package camera

// Platform independent pieces of the V4L2 backend (ioctl numbers, struct
// layouts, sysfs parsing). Kept build-tag free so they are unit-tested on any OS.
// Struct layouts and request numbers assume the 64-bit and 32-bit Linux ABIs,
// where all structs used here have identical sizes.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ioctl request encoding (asm-generic/ioctl.h, valid on x86 and arm).
const (
	iocNone  = 0
	iocWrite = 1
	iocRead  = 2
)

func ioc(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | typ<<8 | nr
}

// v4l2Capability is struct v4l2_capability (104 bytes).
type v4l2Capability struct {
	Driver     [16]byte
	Card       [32]byte
	BusInfo    [32]byte
	Version    uint32
	Caps       uint32
	DeviceCaps uint32
	Reserved   [3]uint32
}

// v4l2Control is struct v4l2_control (8 bytes).
type v4l2Control struct {
	ID    uint32
	Value int32
}

// v4l2QueryCtrl is struct v4l2_queryctrl (68 bytes).
type v4l2QueryCtrl struct {
	ID           uint32
	Type         uint32
	Name         [32]byte
	Minimum      int32
	Maximum      int32
	Step         int32
	DefaultValue int32
	Flags        uint32
	Reserved     [2]uint32
}

var (
	vidiocQueryCap  = ioc(iocRead, 'V', 0, 104)
	vidiocGCtrl     = ioc(iocRead|iocWrite, 'V', 27, 8)
	vidiocSCtrl     = ioc(iocRead|iocWrite, 'V', 28, 8)
	vidiocQueryCtrl = ioc(iocRead|iocWrite, 'V', 36, 68)
)

// Camera-class control ids (linux/v4l2-controls.h).
const (
	v4l2CIDFocusAbsolute = 0x009a090a
	v4l2CIDZoomAbsolute  = 0x009a090d
	v4l2CIDPanAbsolute   = 0x009a0908
	v4l2CIDTiltAbsolute  = 0x009a0909

	v4l2CtrlFlagDisabled = 0x0001
	v4l2CtrlFlagInactive = 0x0010

	v4l2CapVideoCapture      = 0x00000001
	v4l2CapVideoCaptureMplan = 0x00001000
	v4l2CapMetaCapture       = 0x00800000
	v4l2CapDeviceCaps        = 0x80000000
)

// cString converts a NUL-terminated byte array to a string.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// isCaptureNode reports whether the capabilities describe a video capture
// node (not a metadata node or an output-only node).
func isCaptureNode(c v4l2Capability) bool {
	caps := c.Caps
	if caps&v4l2CapDeviceCaps != 0 {
		caps = c.DeviceCaps
	}
	if caps&v4l2CapMetaCapture != 0 {
		return false
	}
	return caps&(v4l2CapVideoCapture|v4l2CapVideoCaptureMplan) != 0
}

// usbIdent identifies the USB device behind a video node (from sysfs).
type usbIdent struct {
	Vendor, Product uint16
	Serial          string
	Bus, Addr       int
}

// readUSBIdent reads idVendor/idProduct/serial/busnum/devnum from a sysfs USB
// device directory (e.g. /sys/class/video4linux/video0/device/..).
func readUSBIdent(dir string) (usbIdent, error) {
	rd := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	v, err1 := strconv.ParseUint(rd("idVendor"), 16, 16)
	p, err2 := strconv.ParseUint(rd("idProduct"), 16, 16)
	if err1 != nil || err2 != nil {
		return usbIdent{}, fmt.Errorf("no USB ids in %s", dir)
	}
	bus, _ := strconv.Atoi(rd("busnum"))
	addr, _ := strconv.Atoi(rd("devnum"))
	return usbIdent{Vendor: uint16(v), Product: uint16(p), Serial: rd("serial"), Bus: bus, Addr: addr}, nil
}

// videoNodeNumber returns N for "videoN" (or "/dev/videoN"), or -1.
func videoNodeNumber(name string) int {
	name = filepath.Base(name)
	if !strings.HasPrefix(name, "video") {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, "video"))
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// v4l2Selector is a parsed Options.Device.
type v4l2Selector struct {
	Path            string // /dev/videoN
	Vendor, Product uint16
	Bus, Addr       int
	Serial          string
}

func parseV4L2Selector(dev string) v4l2Selector {
	dev = strings.TrimSpace(dev)
	s := v4l2Selector{Vendor: 0x2e1a, Product: 0x4c01}
	if g := v4l2VidPidRe.FindStringSubmatch(dev); g != nil {
		v, _ := strconv.ParseUint(g[1], 16, 16)
		p, _ := strconv.ParseUint(g[2], 16, 16)
		s.Vendor, s.Product = uint16(v), uint16(p)
		return s
	}
	if g := v4l2BusAddrRe.FindStringSubmatch(dev); g != nil {
		s.Bus, _ = strconv.Atoi(g[1])
		s.Addr, _ = strconv.Atoi(g[2])
		return s
	}
	switch {
	case dev == "":
	case strings.HasPrefix(dev, "/dev/"):
		s.Path = dev
	default:
		s.Serial = dev
	}
	return s
}

// matches reports whether a node with the given identity satisfies s.
func (s v4l2Selector) matches(path string, id usbIdent) bool {
	if s.Path != "" {
		return path == s.Path
	}
	if id.Vendor != s.Vendor || id.Product != s.Product {
		return false
	}
	if s.Bus != 0 || s.Addr != 0 {
		return id.Bus == s.Bus && id.Addr == s.Addr
	}
	return s.Serial == "" || id.Serial == s.Serial
}

const v4l2StreamHint = "pan/tilt/zoom on UVC cameras often only take effect while a video stream is open; " +
	"start one (e.g. `ffplay /dev/videoN` or `v4l2-ctl -d /dev/videoN --stream-mmap --stream-count=0`) and retry"

var (
	v4l2BusAddrRe = regexp.MustCompile(`^(\d+)\.(\d+)$`)
	v4l2VidPidRe  = regexp.MustCompile(`^([0-9a-fA-F]{4}):([0-9a-fA-F]{4})$`)
)
