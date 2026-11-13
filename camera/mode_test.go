package camera_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/CesarGallego/insta360tux/camera"
)

// The block captured from the official app when selecting overhead (see PROTOCOL.md).
const appOverhead = "05000000000000000000000000000000000000000000000000000000000000000000000000001a0e00001a0e00001a0e00000000"

func TestEncodeModeBlockMatchesApp(t *testing.T) {
	b, err := camera.EncodeModeBlock(camera.ModeOverhead)
	if err != nil || len(b) != 52 {
		t.Fatalf("len=%d err=%v", len(b), err)
	}
	want := make([]byte, 52)
	for i := range want {
		fmtHex := appOverhead[2*i : 2*i+2]
		var v byte
		for _, c := range fmtHex {
			v <<= 4
			switch {
			case c >= 'a':
				v |= byte(c-'a') + 10
			default:
				v |= byte(c - '0')
			}
		}
		want[i] = v
	}
	if !bytes.Equal(b, want) {
		t.Errorf("got %x\nwant %x", b, want)
	}
}

func TestModeParsingAndWire(t *testing.T) {
	for _, m := range camera.Modes {
		p, err := camera.ParseMode(string(m))
		if err != nil || p != m {
			t.Errorf("%s: %v %v", m, p, err)
		}
		v, _ := camera.ModeByte(m)
		if camera.ModeFromByte(v) != m {
			t.Errorf("roundtrip %s", m)
		}
	}
	if m, err := camera.ParseMode(" Tracking "); err != nil || m != camera.ModeTracking {
		t.Errorf("case/space: %v %v", m, err)
	}
	if _, err := camera.ParseMode("disco"); !errors.Is(err, camera.ErrInvalidValue) {
		t.Errorf("unknown: %v", err)
	}
	if got := camera.ModeFromByte(2); got != "unknown(0x02)" {
		t.Errorf("0x02 = %q", got)
	}
	if _, err := camera.EncodeModeBlock("unknown(0x02)"); err == nil {
		t.Error("unseen modes must not be settable")
	}
}

func TestCameraSetGetMode(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	if m, err := c.GetMode(); err != nil || m != camera.ModeNormal {
		t.Fatalf("initial %v %v", m, err)
	}
	if _, err := c.SetMode(camera.ModeDesk); err != nil || b.Mode != camera.ModeDesk {
		t.Fatalf("set: %v %v", err, b.Mode)
	}
	if _, err := c.SetMode("bogus"); !errors.Is(err, camera.ErrInvalidValue) {
		t.Errorf("bogus: %v", err)
	}
	d, b2 := newCam(t, camera.Options{DryRun: true})
	r, err := d.SetMode(camera.ModeTracking)
	if err != nil || !r.DryRun || b2.Writes != 0 {
		t.Errorf("dry-run: %+v %v writes=%d", r, err, b2.Writes)
	}
}

func TestModeSettling(t *testing.T) {
	if got := camera.ModeFromByte(0xff); got != camera.ModeSettling {
		t.Errorf("0xff = %q", got)
	}
	if _, err := camera.EncodeModeBlock(camera.ModeSettling); err == nil {
		t.Error("settling must not be settable")
	}
}
