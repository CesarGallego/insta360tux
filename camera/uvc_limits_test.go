package camera_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/CesarGallego/insta360tux/camera"
	"github.com/CesarGallego/insta360tux/camera/fake"
)

func pt(p, t int32) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, uint32(p))
	binary.LittleEndian.PutUint32(b[4:], uint32(t))
	return b
}
func u16(v int16) []byte { return binary.LittleEndian.AppendUint16(nil, uint16(v)) }

// linkGetter replays the answers captured from a real Insta360 Link.
func linkGetter(req uint8, sel uint16, n int) ([]byte, error) {
	switch sel {
	case 0x0D:
		switch req {
		case camera.UVCGetMin:
			return pt(-522000, -324000), nil
		case camera.UVCGetMax:
			return pt(522000, 360000), nil
		case camera.UVCGetRes:
			return pt(3600, 3600), nil
		case camera.UVCGetDef:
			return pt(0, 0), nil
		}
	case 0x0B:
		return map[uint8][]byte{camera.UVCGetMin: u16(100), camera.UVCGetMax: u16(400), camera.UVCGetRes: u16(1), camera.UVCGetDef: u16(100)}[req], nil
	case 0x06:
		return map[uint8][]byte{camera.UVCGetMin: u16(0), camera.UVCGetMax: u16(100), camera.UVCGetRes: u16(1), camera.UVCGetDef: u16(50)}[req], nil
	case 0x0F:
		return map[uint8][]byte{camera.UVCGetMin: u16(-100), camera.UVCGetMax: u16(100), camera.UVCGetRes: u16(1), camera.UVCGetDef: u16(0)}[req], nil
	}
	return nil, errors.New("pipe")
}

func TestQueryUVCLimitsRealLink(t *testing.T) {
	l, err := camera.QueryUVCLimits(linkGetter)
	if err != nil {
		t.Fatal(err)
	}
	if l.Pan.Min != -145 || l.Pan.Max != 145 || l.Tilt.Min != -90 || l.Tilt.Max != 100 {
		t.Errorf("pan/tilt %+v %+v", l.Pan, l.Tilt)
	}
	if l.Pan.Res != 1 || l.Tilt.Def != 0 {
		t.Errorf("res/def %+v", l.Pan)
	}
	if l.Zoom.Min != 100 || l.Zoom.Max != 400 || l.Zoom.Def != 100 {
		t.Errorf("zoom %+v", l.Zoom)
	}
	if l.Focus == nil || l.Focus.Max != 100 || l.Focus.Def != 50 {
		t.Errorf("focus %+v", l.Focus)
	}
	if l.Roll == nil || l.Roll.Min != -100 { // signed on the wire (0xFF9C)
		t.Errorf("roll %+v", l.Roll)
	}
}

func TestQueryUVCLimitsOptionalAndErrors(t *testing.T) {
	without := func(bad uint16, others ...uint16) camera.UVCGetter {
		return func(req uint8, sel uint16, n int) ([]byte, error) {
			if sel == bad {
				return nil, errors.New("stall")
			}
			for _, o := range others {
				if sel == o {
					return nil, errors.New("stall")
				}
			}
			return linkGetter(req, sel, n)
		}
	}
	l, err := camera.QueryUVCLimits(without(0x06, 0x0F))
	if err != nil || l.Focus != nil || l.Roll != nil {
		t.Fatalf("optional: %+v %v", l, err)
	}
	if _, err := camera.QueryUVCLimits(without(0x0B)); err == nil {
		t.Error("missing zoom must fail")
	}
	if _, err := camera.QueryUVCLimits(without(0x0D)); err == nil {
		t.Error("missing pan/tilt must fail")
	}
	short := func(req uint8, sel uint16, n int) ([]byte, error) { return []byte{1}, nil }
	if _, err := camera.QueryUVCLimits(short); err == nil {
		t.Error("short read must fail")
	}
	inverted := func(req uint8, sel uint16, n int) ([]byte, error) {
		if sel == 0x0D && req == camera.UVCGetMin {
			return pt(600000, 0), nil
		}
		return linkGetter(req, sel, n)
	}
	if _, err := camera.QueryUVCLimits(inverted); err == nil {
		t.Error("min>=max must fail")
	}
}

type queryingFake struct {
	*fake.Backend
	l   camera.Limits
	err error
}

func (q queryingFake) QueryLimits() (camera.Limits, error) { return q.l, q.err }

func TestWrapUsesQueriedLimits(t *testing.T) {
	l, _ := camera.QueryUVCLimits(linkGetter)
	l.Tilt.Max = 50 // pretend the device differs from the defaults
	c, err := camera.Wrap(queryingFake{Backend: fake.New(), l: l}, camera.Options{})
	if err != nil {
		t.Fatal(err)
	}
	in := c.Info()
	if in.RangesSource != camera.RangesFromDevice || in.TiltRange.Max != 50 || in.Controls["tilt"].Max != 50 {
		t.Fatalf("info %+v", in)
	}
	if _, err := c.SetPanTilt(0, 60); !errors.Is(err, camera.ErrOutOfRange) {
		t.Errorf("queried range not enforced: %v", err)
	}
}

func TestWrapFallsBackToDefaults(t *testing.T) {
	c, err := camera.Wrap(queryingFake{Backend: fake.New(), err: errors.New("boom")}, camera.Options{})
	if err != nil {
		t.Fatal(err)
	}
	in := c.Info()
	if in.RangesSource != camera.RangesDefault || in.RangesWarning == "" || in.TiltRange != camera.DefaultTiltRange {
		t.Fatalf("info %+v", in)
	}
	c2, _ := camera.Wrap(fake.New(), camera.Options{})
	if c2.Info().RangesSource != camera.RangesDefault || c2.Info().RangesWarning != "" {
		t.Errorf("plain backend: %+v", c2.Info())
	}
}

func TestPanTiltQuantized(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	b.Pan, b.Tilt = -0.8, -11.6
	res, err := c.MovePanTilt(-5, 3) // -5.800000000000001 without quantization
	if err != nil || res.Applied.Pan != -5.8 || b.Pan != -5.8 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestAutoFocus(t *testing.T) {
	c, b := newCam(t, camera.Options{DryRun: true})
	if r, err := c.SetAutoFocus(true); err != nil || !r.DryRun || b.AutoFocus || b.Writes != 0 {
		t.Fatalf("dry-run wrote: %+v %v", r, err)
	}
	c, b = newCam(t, camera.Options{})
	if _, err := c.SetAutoFocus(true); err != nil || !b.AutoFocus {
		t.Fatal(err)
	}
	if on, err := c.GetAutoFocus(); err != nil || !on {
		t.Fatal(on, err)
	}
}
