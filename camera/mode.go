package camera

import (
	"fmt"
	"strings"
)

// Mode is a camera "mode" of the Insta360 Link: the byte 0 of the unit 9
// selector 0x02 block (see PROTOCOL.md). Verified on hardware (macOS, libusb,
// firmware v1.4.8.8_build2) by writing the block without the official app.
type Mode string

const (
	ModeNormal     Mode = "normal"     // 0x00: no special behaviour, pan/tilt/zoom are yours
	ModeTracking   Mode = "tracking"   // 0x01: AI tracking, the camera searches for a face and moves by itself
	ModeWhiteboard Mode = "whiteboard" // 0x04: zooms out looking for QR markers; cancels itself if none are found
	ModeOverhead   Mode = "overhead"   // 0x05: tilts straight down (-90 deg)
	ModeDesk       Mode = "desk"       // 0x06: desk view, frames the hands (tilt about -45 deg)
)

// Modes lists the modes that can be set, in CLI order. Wire values 0x02 and
// 0x03 were never seen and are deliberately not settable.
var Modes = []Mode{ModeNormal, ModeTracking, ModeWhiteboard, ModeOverhead, ModeDesk}

var modeBytes = map[Mode]byte{ModeNormal: 0, ModeTracking: 1, ModeWhiteboard: 4, ModeOverhead: 5, ModeDesk: 6}

// ParseMode resolves a user supplied name (case-insensitive).
func ParseMode(s string) (Mode, error) {
	m := Mode(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := modeBytes[m]; !ok {
		return "", fmt.Errorf("%w: unknown mode %q (valid: %v)", ErrInvalidValue, s, Modes)
	}
	return m, nil
}

// ModeByte returns the wire value of a settable mode.
func ModeByte(m Mode) (byte, bool) { b, ok := modeBytes[m]; return b, ok }

// ModeFromByte names a wire value. Unseen values are reported as "unknown(0xNN)".
func ModeFromByte(b byte) Mode {
	for m, v := range modeBytes {
		if v == b {
			return m
		}
	}
	return Mode(fmt.Sprintf("unknown(0x%02x)", b))
}

// Layout of the 52 byte unit 9 selector 0x02 block.
const (
	ModeBlockLen = 52
	// The app writes 0x0e1a (3610) as int32 LE at offsets 38, 42 and 46 on every
	// mode change. Meaning unknown; replicated verbatim (verified to work).
	modeBlockConst = 3610
)

// EncodeModeBlock builds the 52 byte block written to select m, byte for
// byte what the official app sends.
func EncodeModeBlock(m Mode) ([]byte, error) {
	v, ok := modeBytes[m]
	if !ok {
		return nil, fmt.Errorf("%w: mode %q is not settable", ErrInvalidValue, m)
	}
	b := make([]byte, ModeBlockLen)
	b[0] = v
	for _, off := range []int{38, 42, 46} {
		b[off], b[off+1] = byte(modeBlockConst&0xff), byte(modeBlockConst>>8)
	}
	return b, nil
}

// DecodeModeBlock extracts the mode from a block read with GET_CUR. On the
// read side byte 1 is a status field (0x10, 0x03, 0x01 ... meaning unknown)
// and pan/tilt (int32, 0.1 deg) / zoom (u16) sit at offsets 38, 42 and 50.
func DecodeModeBlock(b []byte) (Mode, error) {
	if len(b) < 1 {
		return "", fmt.Errorf("%w: empty mode block", ErrTransfer)
	}
	return ModeFromByte(b[0]), nil
}

// ModeController is optionally implemented by backends that can read and set
// the camera mode. Today only the libusb backend does (and the fake one). On
// Linux the v4l2 backend can implement it through UVCIOC_CTRL_QUERY (unverified).
type ModeController interface {
	GetMode() (Mode, error)
	SetMode(Mode) error
}

// GetMode returns the current mode. ErrUnsupported if the backend cannot.
// Unseen wire values are returned as "unknown(0xNN)", not as an error.
func (c *Camera) GetMode() (Mode, error) {
	m, ok := c.b.(ModeController)
	if !ok {
		return "", ErrUnsupported
	}
	return m.GetMode()
}

// SetMode switches the camera mode (nothing is sent in dry-run).
// Tracking makes the camera move on its own and fights SetPanTilt; use
// ModeNormal to take control back.
func (c *Camera) SetMode(mode Mode) (Result[Mode], error) {
	res := Result[Mode]{Requested: mode, Applied: mode, DryRun: c.opts.DryRun}
	if _, ok := modeBytes[mode]; !ok {
		return res, fmt.Errorf("%w: mode %q is not settable", ErrInvalidValue, mode)
	}
	m, ok := c.b.(ModeController)
	if !ok {
		return res, ErrUnsupported
	}
	if c.opts.DryRun {
		return res, nil
	}
	return res, m.SetMode(mode)
}
