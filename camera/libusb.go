//go:build cgo && !nolibusb

package camera

import (
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/gousb"
)

// UVC constants for the Insta360 Link (see PROTOCOL.md).
const (
	vidInsta = 0x2e1a
	pidLink  = 0x4c01

	reqSetCur   = 0x01
	reqGetCur   = 0x81
	typeSetCls  = 0x21 // host->device, class, interface
	typeGetCls  = 0xA1 // device->host, class, interface
	unitCamera  = 1    // Camera Terminal
	ifaceCtl    = 0    // VideoControl interface
	selFocusAbs = 0x06
	selZoomAbs  = 0x0B
	selPanTiltA = 0x0D
	selRollAbs  = 0x0F
	selFocusAut = 0x08 // FOCUS_AUTO_CONTROL, 1 byte
	unitXU9     = 9    // proprietary extension unit 9
	selXUMode   = 0x02 // unit 9: 52 byte status/mode block
)

func init() {
	Register(Factory{
		Name:     "libusb",
		Priority: 50,
		New:      func() Backend { return &libusbBackend{} },
		List:     libusbList,
	})
}

type libusbBackend struct {
	ctx  *gousb.Context
	dev  *gousb.Device
	desc *gousb.DeviceDesc
}

func (b *libusbBackend) Name() string { return "libusb" }

var busAddrRe = regexp.MustCompile(`^(\d+)\.(\d+)$`)
var vidPidRe = regexp.MustCompile(`^([0-9a-fA-F]{4}):([0-9a-fA-F]{4})$`)

// matcher builds a descriptor filter from Options.Device. Serial selectors
// return needSerial = the serial to compare after opening.
func matcher(dev string) (m func(*gousb.DeviceDesc) bool, serial string) {
	dev = strings.TrimSpace(dev)
	vid, pid := gousb.ID(vidInsta), gousb.ID(pidLink)
	switch {
	case dev == "":
	case vidPidRe.MatchString(dev):
		g := vidPidRe.FindStringSubmatch(dev)
		v, _ := strconv.ParseUint(g[1], 16, 16)
		p, _ := strconv.ParseUint(g[2], 16, 16)
		vid, pid = gousb.ID(v), gousb.ID(p)
	case busAddrRe.MatchString(dev):
		g := busAddrRe.FindStringSubmatch(dev)
		bus, _ := strconv.Atoi(g[1])
		addr, _ := strconv.Atoi(g[2])
		return func(d *gousb.DeviceDesc) bool {
			return d.Vendor == vid && d.Product == pid && d.Bus == bus && d.Address == addr
		}, ""
	default:
		serial = dev
	}
	return func(d *gousb.DeviceDesc) bool { return d.Vendor == vid && d.Product == pid }, serial
}

func mapErr(op string, err error) error {
	var ue gousb.Error
	if errors.As(err, &ue) {
		switch ue {
		case gousb.ErrorAccess, gousb.ErrorBusy:
			return fmt.Errorf("%w: %s: %v", ErrPermission, op, err)
		case gousb.ErrorTimeout:
			return fmt.Errorf("%w: %s", ErrTimeout, op)
		case gousb.ErrorNoDevice, gousb.ErrorNotFound:
			return fmt.Errorf("%w: %s: %v", ErrNoDevice, op, err)
		}
	}
	return &TransferError{Op: op, Err: err}
}

func (b *libusbBackend) Open(opts Options) error {
	b.ctx = gousb.NewContext()
	match, serial := matcher(opts.Device)
	devs, err := b.ctx.OpenDevices(match)
	// Pick the first device that satisfies the serial filter; close the rest.
	var picked *gousb.Device
	for _, d := range devs {
		if picked == nil {
			ok := true
			if serial != "" {
				s, _ := d.SerialNumber()
				ok = s == serial
			}
			if ok {
				picked = d
				continue
			}
		}
		d.Close()
	}
	if picked == nil {
		b.ctx.Close()
		b.ctx = nil
		if err != nil {
			return mapErr("open", err)
		}
		return ErrNoDevice
	}
	b.dev, b.desc = picked, picked.Desc
	b.dev.ControlTimeout = opts.timeout()
	return nil
}

func (b *libusbBackend) Close() error {
	var err error
	if b.dev != nil {
		err = b.dev.Close()
		b.dev = nil
	}
	if b.ctx != nil {
		b.ctx.Close()
		b.ctx = nil
	}
	return err
}

func (b *libusbBackend) Info() (Info, error) {
	if b.dev == nil {
		return Info{}, ErrNoDevice
	}
	in := Info{
		Backend: "libusb", VendorID: uint16(b.desc.Vendor), ProductID: uint16(b.desc.Product),
		Bus: b.desc.Bus, Address: b.desc.Address,
		PanRange: DefaultPanRange, TiltRange: DefaultTiltRange, ZoomRange: DefaultZoomRange,
	}
	in.Name, _ = b.dev.Product()
	in.Manufacturer, _ = b.dev.Manufacturer()
	in.Serial, _ = b.dev.SerialNumber()
	f, r := DefaultFocusRange, DefaultRollRange
	in.Focus, in.Roll = &f, &r
	return in, nil
}

func (b *libusbBackend) get(op string, sel uint16, n int) ([]byte, error) {
	return b.getReq(op, reqGetCur, sel, n)
}

func (b *libusbBackend) getReq(op string, req uint8, sel uint16, n int) ([]byte, error) {
	return b.getUnit(op, unitCamera, req, sel, n)
}

func (b *libusbBackend) getUnit(op string, unit uint16, req uint8, sel uint16, n int) ([]byte, error) {
	if b.dev == nil {
		return nil, ErrNoDevice
	}
	buf := make([]byte, n)
	got, err := b.dev.Control(typeGetCls, req, sel<<8, unit<<8|ifaceCtl, buf)
	if err != nil {
		return nil, mapErr(op, err)
	}
	if got != n {
		return nil, &TransferError{Op: op, Err: fmt.Errorf("short read: %d of %d bytes", got, n)}
	}
	return buf, nil
}

func (b *libusbBackend) set(op string, sel uint16, data []byte) error {
	return b.setUnit(op, unitCamera, sel, data)
}

func (b *libusbBackend) setUnit(op string, unit uint16, sel uint16, data []byte) error {
	if b.dev == nil {
		return ErrNoDevice
	}
	got, err := b.dev.Control(typeSetCls, reqSetCur, sel<<8, unit<<8|ifaceCtl, data)
	if err != nil {
		return mapErr(op, err)
	}
	if got != len(data) {
		return &TransferError{Op: op, Err: fmt.Errorf("short write: %d of %d bytes", got, len(data))}
	}
	return nil
}

func (b *libusbBackend) GetPanTilt() (float64, float64, error) {
	d, err := b.get("GET pan/tilt", selPanTiltA, 8)
	if err != nil {
		return 0, 0, err
	}
	p := int32(binary.LittleEndian.Uint32(d[0:4]))
	t := int32(binary.LittleEndian.Uint32(d[4:8]))
	return ArcSecToDeg(p), ArcSecToDeg(t), nil
}

func (b *libusbBackend) SetPanTilt(pan, tilt float64) error {
	d := make([]byte, 8)
	binary.LittleEndian.PutUint32(d[0:4], uint32(DegToArcSec(pan)))
	binary.LittleEndian.PutUint32(d[4:8], uint32(DegToArcSec(tilt)))
	return b.set("SET pan/tilt", selPanTiltA, d)
}

func (b *libusbBackend) getU16(op string, sel uint16) (int, error) {
	d, err := b.get(op, sel, 2)
	if err != nil {
		return 0, err
	}
	return int(binary.LittleEndian.Uint16(d)), nil
}

func (b *libusbBackend) setU16(op string, sel uint16, v int) error {
	d := make([]byte, 2)
	binary.LittleEndian.PutUint16(d, uint16(v))
	return b.set(op, sel, d)
}

func (b *libusbBackend) GetZoom() (int, error) { return b.getU16("GET zoom", selZoomAbs) }
func (b *libusbBackend) SetZoom(z int) error   { return b.setU16("SET zoom", selZoomAbs, z) }
func (b *libusbBackend) GetFocus() (int, error) {
	return b.getU16("GET focus", selFocusAbs)
}

// SetFocus writes the focus. The camera stalls manual focus writes while
// autofocus is on (verified on hardware), so autofocus is switched off first;
// it stays off until SetAutoFocus(true).
func (b *libusbBackend) SetFocus(v int) error {
	if on, err := b.GetAutoFocus(); err == nil && on {
		if err := b.SetAutoFocus(false); err != nil {
			return err
		}
	}
	return b.setU16("SET focus", selFocusAbs, v)
}

func (b *libusbBackend) GetAutoFocus() (bool, error) {
	d, err := b.get("GET autofocus", selFocusAut, 1)
	if err != nil {
		return false, err
	}
	return d[0] != 0, nil
}

func (b *libusbBackend) SetAutoFocus(on bool) error {
	v := byte(0)
	if on {
		v = 1
	}
	return b.set("SET autofocus", selFocusAut, []byte{v})
}

// QueryLimits asks the camera for GET_MIN/MAX/RES/DEF of the PTZ controls.
func (b *libusbBackend) QueryLimits() (Limits, error) {
	return QueryUVCLimits(func(req uint8, sel uint16, n int) ([]byte, error) {
		return b.getReq(fmt.Sprintf("GET 0x%02X sel 0x%02X", req, sel), req, sel, n)
	})
}

// GetMode reads the camera mode (byte 0 of unit 9 selector 0x02).
func (b *libusbBackend) GetMode() (Mode, error) {
	d, err := b.getUnit("GET mode", unitXU9, reqGetCur, selXUMode, ModeBlockLen)
	if err != nil {
		return "", err
	}
	return DecodeModeBlock(d)
}

// SetMode writes the 52 byte block the official app writes. No handshake
// (unit 9 selectors 0x14/0x15) is needed: verified on hardware.
func (b *libusbBackend) SetMode(m Mode) error {
	d, err := EncodeModeBlock(m)
	if err != nil {
		return err
	}
	return b.setUnit("SET mode", unitXU9, selXUMode, d)
}

// Roll is a signed 16-bit value on the wire.
func (b *libusbBackend) GetRoll() (int, error) {
	v, err := b.getU16("GET roll", selRollAbs)
	return int(int16(v)), err
}
func (b *libusbBackend) SetRoll(v int) error {
	return b.setU16("SET roll", selRollAbs, int(uint16(int16(v))))
}

func libusbList(opts Options) ([]DeviceInfo, error) {
	ctx := gousb.NewContext()
	defer ctx.Close()
	match, serial := matcher(opts.Device)
	devs, err := ctx.OpenDevices(match)
	var out []DeviceInfo
	for _, d := range devs {
		s, _ := d.SerialNumber()
		if serial == "" || s == serial {
			out = append(out, DeviceInfo{
				Backend: "libusb", VendorID: uint16(d.Desc.Vendor), ProductID: uint16(d.Desc.Product),
				Bus: d.Desc.Bus, Address: d.Desc.Address, Serial: s,
				Selector: fmt.Sprintf("%d.%d", d.Desc.Bus, d.Desc.Address),
			})
		}
		d.Close()
	}
	if len(out) == 0 && err != nil {
		return nil, mapErr("list", err)
	}
	return out, nil
}
