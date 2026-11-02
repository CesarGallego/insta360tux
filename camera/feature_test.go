package camera_test

import (
	"errors"
	"testing"

	"github.com/CesarGallego/insta360tux/camera"
)

func ptr(b bool) *bool { return &b }

func TestFeatureReadModifyWrite(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	b.FeatureMask = 0x0220 | 0x8000 // an unknown bit must survive
	ch, err := c.SetFeature(camera.FeatureHDR, true)
	if err != nil || b.FeatureMask != 0x8224 || ch.MaskBefore != 0x8220 || ch.MaskAfter != 0x8224 {
		t.Fatalf("hdr on: %+v %v mask=%#x", ch, err, b.FeatureMask)
	}
	if _, err := c.SetFeature(camera.FeatureSmartFraming, false); err != nil || b.FeatureMask != 0x8024 {
		t.Fatalf("smart off: mask=%#x %v", b.FeatureMask, err)
	}
	if _, err := c.SetFeature(camera.FeatureVerticalLock, true); err != nil || b.FeatureMask&0x400 == 0 {
		t.Fatalf("vertical: %#x", b.FeatureMask)
	}
	fs, m, err := c.GetFeatures()
	if err != nil || m != b.FeatureMask || len(fs) != len(camera.Features) {
		t.Fatalf("get: %v %#x %v", fs, m, err)
	}
	for _, f := range fs {
		want := b.FeatureMask&bitOf(f.Name) != 0
		if f.On != want {
			t.Errorf("%s: %v", f.Name, f.On)
		}
	}
	if _, err := c.SetFeature("bogus", true); !errors.Is(err, camera.ErrInvalidValue) {
		t.Errorf("bogus: %v", err)
	}
}

func bitOf(n camera.Feature) uint16 {
	for _, f := range camera.Features {
		if f.Name == n {
			return f.Bit
		}
	}
	return 0
}

func TestFeatureDryRun(t *testing.T) {
	c, b := newCam(t, camera.Options{DryRun: true})
	ch, err := c.SetFeature(camera.FeatureOneTouch, true)
	if err != nil || !ch.DryRun || ch.MaskAfter != 0x02a0 || b.Writes != 0 || b.FeatureMask != 0x0220 {
		t.Errorf("%+v %v writes=%d", ch, err, b.Writes)
	}
}

func TestGestures(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	b.GestureMask = 0x01 | 0x20 // unknown bits preserved
	ch, err := c.SetGestures(camera.GestureUpdate{Enabled: ptr(true), Tracking: ptr(true), Zoom: ptr(true)})
	if err != nil || b.FeatureMask != 0x0230 || b.GestureMask != 0x27 || !ch.After.Enabled || !ch.After.Zoom || ch.Before.Enabled {
		t.Fatalf("%+v %v f=%#x g=%#x", ch, err, b.FeatureMask, b.GestureMask)
	}
	if _, err := c.SetGestures(camera.GestureUpdate{Tracking: ptr(false)}); err != nil || b.GestureMask != 0x25 || b.FeatureMask != 0x0230 {
		t.Fatalf("tracking off: g=%#x", b.GestureMask)
	}
	w := b.Writes
	if _, err := c.SetGestures(camera.GestureUpdate{Tracking: ptr(false)}); err != nil || b.Writes != w {
		t.Errorf("unchanged must not write")
	}
	if _, err := c.SetGestures(camera.GestureUpdate{Enabled: ptr(false), Zoom: ptr(false)}); err != nil || b.FeatureMask != 0x0220 || b.GestureMask != 0x21 {
		t.Fatalf("off: f=%#x g=%#x", b.FeatureMask, b.GestureMask)
	}
	g, err := c.GetGestures()
	if err != nil || g.Enabled || g.Zoom {
		t.Errorf("%+v %v", g, err)
	}
}

func TestTrackingSpeedAndPowerLine(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	if s, _ := c.GetTrackingSpeed(); s != camera.SpeedNormal {
		t.Errorf("default %s", s)
	}
	if _, err := c.SetTrackingSpeed(camera.SpeedSlow); err != nil || b.Speed != camera.SpeedSlow {
		t.Errorf("slow: %v", err)
	}
	if _, err := c.SetTrackingSpeed("warp"); !errors.Is(err, camera.ErrInvalidValue) {
		t.Errorf("warp: %v", err)
	}
	for i, s := range camera.TrackingSpeeds {
		v, _ := camera.TrackingSpeedByte(s)
		if int(v) != i || camera.TrackingSpeedFromByte(v) != s {
			t.Errorf("wire %s", s)
		}
	}
	if camera.TrackingSpeedFromByte(7) != "unknown(0x07)" {
		t.Error("unknown speed")
	}
	if f, _ := c.GetPowerLine(); f != camera.PowerLine50 {
		t.Errorf("default %d", f)
	}
	if _, err := c.SetPowerLine(camera.PowerLine60); err != nil || b.PowerLine != 60 {
		t.Errorf("60: %v", err)
	}
	if _, err := c.SetPowerLine(55); !errors.Is(err, camera.ErrInvalidValue) {
		t.Errorf("55: %v", err)
	}
	for _, s := range []string{"50", "60Hz", " 50 hz"} {
		if _, err := camera.ParsePowerLine(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	if _, err := camera.ParsePowerLine("0"); err == nil {
		t.Error("0 must be rejected")
	}
	if b1, _ := camera.PowerLineByte(60); b1 != 2 || camera.PowerLineFromByte(1) != 50 || camera.PowerLineFromByte(3) != camera.PowerLineOther {
		t.Error("power line wire")
	}
}
