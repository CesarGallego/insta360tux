//go:build linux

package camera

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// The v4l2 backend is pure Go (raw ioctls through package syscall), so it
// works with CGO_ENABLED=0. Pan/tilt are in arc-seconds (like UVC), zoom and
// focus are raw UVC values. There is no standard V4L2 roll control, so Roll
// is not offered. Movement generally needs an active video stream.

const sysVideoDir = "/sys/class/video4linux"

func init() {
	Register(Factory{
		Name:     "v4l2",
		Priority: 100,
		New:      func() Backend { return &v4l2Backend{fd: -1} },
		List:     v4l2List,
	})
}

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
		if e == 0 {
			return nil
		}
		if e == syscall.EINTR {
			continue
		}
		return e
	}
}

func mapV4L2Err(op string, err error, isSet bool) error {
	var en syscall.Errno
	if errors.As(err, &en) {
		switch en {
		case syscall.EACCES, syscall.EPERM:
			return fmt.Errorf("%w: %s: %v (add your user to the 'video' group)", ErrPermission, op, err)
		case syscall.EBUSY:
			return fmt.Errorf("%w: %s: %v", ErrPermission, op, err)
		case syscall.ENODEV, syscall.ENOENT, syscall.ENXIO:
			return fmt.Errorf("%w: %s: %v", ErrNoDevice, op, err)
		case syscall.ETIMEDOUT:
			return fmt.Errorf("%w: %s", ErrTimeout, op)
		}
	}
	if isSet {
		return &TransferError{Op: op, Err: fmt.Errorf("%v (%s)", err, v4l2StreamHint)}
	}
	return &TransferError{Op: op, Err: err}
}

type v4l2Node struct {
	path string
	card string
	id   usbIdent
}

// scanNodes returns capture nodes of USB devices, ordered by node number.
func scanNodes() ([]v4l2Node, error) {
	ents, err := filepath.Glob(filepath.Join(sysVideoDir, "video*"))
	if err != nil {
		return nil, err
	}
	sort.Slice(ents, func(i, j int) bool { return videoNodeNumber(ents[i]) < videoNodeNumber(ents[j]) })
	var out []v4l2Node
	for _, e := range ents {
		if videoNodeNumber(e) < 0 {
			continue
		}
		id, err := readUSBIdent(filepath.Join(e, "device", ".."))
		if err != nil {
			continue
		}
		path := "/dev/" + filepath.Base(e)
		fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			// Cannot probe capabilities; still list it if it is the sole node kind we can see.
			out = append(out, v4l2Node{path: path, id: id})
			continue
		}
		var c v4l2Capability
		qerr := ioctl(fd, vidiocQueryCap, unsafe.Pointer(&c))
		syscall.Close(fd)
		if qerr != nil || !isCaptureNode(c) {
			continue
		}
		out = append(out, v4l2Node{path: path, card: cString(c.Card[:]), id: id})
	}
	return out, nil
}

func v4l2List(opts Options) ([]DeviceInfo, error) {
	nodes, err := scanNodes()
	if err != nil {
		return nil, err
	}
	sel := parseV4L2Selector(opts.Device)
	var out []DeviceInfo
	seen := map[[2]int]bool{}
	for _, n := range nodes {
		if !sel.matches(n.path, n.id) {
			continue
		}
		k := [2]int{n.id.Bus, n.id.Addr}
		if sel.Path == "" && seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, DeviceInfo{
			Backend: "v4l2", VendorID: n.id.Vendor, ProductID: n.id.Product,
			Bus: n.id.Bus, Address: n.id.Addr, Serial: n.id.Serial, Path: n.path,
			Selector: n.path,
		})
	}
	return out, nil
}

type v4l2Backend struct {
	fd   int
	node v4l2Node
	rng  map[uint32]Range // by control id
}

func (b *v4l2Backend) Name() string { return "v4l2" }

func (b *v4l2Backend) query(id uint32) (Range, bool) {
	q := v4l2QueryCtrl{ID: id}
	if err := ioctl(b.fd, vidiocQueryCtrl, unsafe.Pointer(&q)); err != nil || q.Flags&v4l2CtrlFlagDisabled != 0 {
		return Range{}, false
	}
	return Range{Min: float64(q.Minimum), Max: float64(q.Maximum)}, true
}

func (b *v4l2Backend) Open(opts Options) error {
	nodes, err := scanNodes()
	if err != nil {
		return err
	}
	sel := parseV4L2Selector(opts.Device)
	var pick *v4l2Node
	for i := range nodes {
		if sel.matches(nodes[i].path, nodes[i].id) {
			pick = &nodes[i]
			break
		}
	}
	if pick == nil {
		return ErrNoDevice
	}
	fd, err := syscall.Open(pick.path, syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return mapV4L2Err("open "+pick.path, err, false)
	}
	b.fd, b.node = fd, *pick
	b.rng = map[uint32]Range{}
	for _, id := range []uint32{v4l2CIDPanAbsolute, v4l2CIDTiltAbsolute, v4l2CIDZoomAbsolute, v4l2CIDFocusAbsolute} {
		if r, ok := b.query(id); ok {
			b.rng[id] = r
		}
	}
	for _, id := range []uint32{v4l2CIDPanAbsolute, v4l2CIDTiltAbsolute, v4l2CIDZoomAbsolute} {
		if _, ok := b.rng[id]; !ok {
			b.Close()
			return fmt.Errorf("%w: %s lacks pan/tilt/zoom controls (not an Insta360 Link or driver too old)", ErrUnsupported, pick.path)
		}
	}
	return nil
}

func (b *v4l2Backend) Close() error {
	if b.fd >= 0 {
		err := syscall.Close(b.fd)
		b.fd = -1
		return err
	}
	return nil
}

func (b *v4l2Backend) Info() (Info, error) {
	if b.fd < 0 {
		return Info{}, ErrNoDevice
	}
	arc := func(id uint32) Range {
		r := b.rng[id]
		return Range{Min: r.Min / ArcSecPerDegree, Max: r.Max / ArcSecPerDegree}
	}
	in := Info{
		Backend: "v4l2", Name: b.node.card, Manufacturer: "",
		Serial: b.node.id.Serial, VendorID: b.node.id.Vendor, ProductID: b.node.id.Product,
		Bus: b.node.id.Bus, Address: b.node.id.Addr, Path: b.node.path,
		PanRange: arc(v4l2CIDPanAbsolute), TiltRange: arc(v4l2CIDTiltAbsolute),
		ZoomRange: b.rng[v4l2CIDZoomAbsolute],
	}
	if r, ok := b.rng[v4l2CIDFocusAbsolute]; ok {
		in.Focus = &r
	}
	in.RangesSource = RangesFromDevice // VIDIOC_QUERYCTRL at Open
	return in, nil
}

func (b *v4l2Backend) getCtrl(op string, id uint32) (int32, error) {
	if b.fd < 0 {
		return 0, ErrNoDevice
	}
	c := v4l2Control{ID: id}
	if err := ioctl(b.fd, vidiocGCtrl, unsafe.Pointer(&c)); err != nil {
		return 0, mapV4L2Err(op, err, false)
	}
	return c.Value, nil
}

func (b *v4l2Backend) setCtrl(op string, id uint32, v int32) error {
	if b.fd < 0 {
		return ErrNoDevice
	}
	c := v4l2Control{ID: id, Value: v}
	if err := ioctl(b.fd, vidiocSCtrl, unsafe.Pointer(&c)); err != nil {
		return mapV4L2Err(op, err, true)
	}
	return nil
}

func (b *v4l2Backend) GetPanTilt() (float64, float64, error) {
	p, err := b.getCtrl("GET pan", v4l2CIDPanAbsolute)
	if err != nil {
		return 0, 0, err
	}
	t, err := b.getCtrl("GET tilt", v4l2CIDTiltAbsolute)
	if err != nil {
		return 0, 0, err
	}
	return ArcSecToDeg(p), ArcSecToDeg(t), nil
}

func (b *v4l2Backend) SetPanTilt(pan, tilt float64) error {
	if err := b.setCtrl("SET pan", v4l2CIDPanAbsolute, DegToArcSec(pan)); err != nil {
		return err
	}
	return b.setCtrl("SET tilt", v4l2CIDTiltAbsolute, DegToArcSec(tilt))
}

func (b *v4l2Backend) GetZoom() (int, error) {
	v, err := b.getCtrl("GET zoom", v4l2CIDZoomAbsolute)
	return int(v), err
}
func (b *v4l2Backend) SetZoom(z int) error {
	return b.setCtrl("SET zoom", v4l2CIDZoomAbsolute, int32(z))
}

func (b *v4l2Backend) hasFocus() bool { _, ok := b.rng[v4l2CIDFocusAbsolute]; return ok }

func (b *v4l2Backend) GetFocus() (int, error) {
	if !b.hasFocus() {
		return 0, ErrUnsupported
	}
	v, err := b.getCtrl("GET focus", v4l2CIDFocusAbsolute)
	return int(v), err
}

func (b *v4l2Backend) SetFocus(v int) error {
	if !b.hasFocus() {
		return ErrUnsupported
	}
	return b.setCtrl("SET focus", v4l2CIDFocusAbsolute, int32(v))
}

var _ = strings.TrimSpace
