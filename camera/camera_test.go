package camera_test

import (
	"errors"
	"math"
	"testing"

	"github.com/CesarGallego/insta360tux/camera"
	"github.com/CesarGallego/insta360tux/camera/fake"
)

func newCam(t *testing.T, o camera.Options) (*camera.Camera, *fake.Backend) {
	t.Helper()
	b := fake.New()
	c, err := camera.Wrap(b, o)
	if err != nil {
		t.Fatal(err)
	}
	return c, b
}

func TestConversion(t *testing.T) {
	if got := camera.DegToArcSec(1); got != 3600 {
		t.Errorf("1deg = %d", got)
	}
	if got := camera.DegToArcSec(-145); got != -522000 {
		t.Errorf("-145deg = %d", got)
	}
	if got := camera.DegToArcSec(0.00014); got != 1 { // 0.504 arcsec rounds to 1
		t.Errorf("rounding = %d", got)
	}
	for _, as := range []int32{0, 1, -1, 522000, -324000, 360000} {
		if back := camera.DegToArcSec(camera.ArcSecToDeg(as)); back != as {
			t.Errorf("roundtrip %d -> %d", as, back)
		}
	}
}

func TestDefaultRangesMatchProtocol(t *testing.T) {
	if camera.DefaultPanRange.Max != 145 || camera.DefaultPanRange.Min != -145 {
		t.Errorf("pan %+v", camera.DefaultPanRange)
	}
	if camera.DefaultTiltRange.Min != -90 || camera.DefaultTiltRange.Max != 100 {
		t.Errorf("tilt %+v", camera.DefaultTiltRange)
	}
}

func TestSetPanTiltValid(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	res, err := c.SetPanTilt(20, -10)
	if err != nil || res.Clamped || b.Pan != 20 || b.Tilt != -10 {
		t.Fatalf("res=%+v err=%v b=%+v", res, err, b)
	}
}

func TestSetPanTiltOutOfRange(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	_, err := c.SetPanTilt(200, 0)
	var re *camera.RangeError
	if !errors.Is(err, camera.ErrOutOfRange) || !errors.As(err, &re) || re.Name != "pan" {
		t.Fatalf("err=%v", err)
	}
	if b.Writes != 0 {
		t.Error("backend written despite error")
	}
	if _, err = c.SetPanTilt(0, -91); !errors.Is(err, camera.ErrOutOfRange) {
		t.Errorf("tilt err=%v", err)
	}
}

func TestClamp(t *testing.T) {
	c, b := newCam(t, camera.Options{Clamp: true})
	res, err := c.SetPanTilt(500, -500)
	if err != nil || !res.Clamped || b.Pan != 145 || b.Tilt != -90 {
		t.Fatalf("res=%+v err=%v b=%+v", res, err, b)
	}
	z, err := c.SetZoom(999)
	if err != nil || !z.Clamped || b.Zoom != 400 {
		t.Fatalf("zoom=%+v err=%v", z, err)
	}
}

func TestInvalidValues(t *testing.T) {
	c, _ := newCam(t, camera.Options{Clamp: true})
	for _, v := range []float64{math.NaN(), math.Inf(1)} {
		if _, err := c.SetPanTilt(v, 0); !errors.Is(err, camera.ErrInvalidValue) {
			t.Errorf("%v: %v", v, err)
		}
	}
}

func TestDryRun(t *testing.T) {
	c, b := newCam(t, camera.Options{DryRun: true})
	res, err := c.SetPanTilt(10, 10)
	if err != nil || !res.DryRun || b.Writes != 0 {
		t.Fatalf("res=%+v writes=%d err=%v", res, b.Writes, err)
	}
	if _, err = c.SetZoom(200); err != nil || b.Writes != 0 {
		t.Fatalf("zoom dry-run wrote: %v", err)
	}
}

func TestMoveRelative(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	b.Pan, b.Tilt, b.Zoom = 10, 5, 150
	if _, err := c.MovePanTilt(-3, 2); err != nil || b.Pan != 7 || b.Tilt != 7 {
		t.Fatalf("move: %+v %v", b, err)
	}
	if _, err := c.MoveZoom(50); err != nil || b.Zoom != 200 {
		t.Fatalf("zoom: %+v %v", b, err)
	}
	if _, err := c.MovePanTilt(1000, 0); !errors.Is(err, camera.ErrOutOfRange) {
		t.Errorf("expected range error, got %v", err)
	}
}

func TestCenter(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	b.Pan, b.Tilt = 30, 30
	if _, err := c.Center(); err != nil || b.Pan != 0 || b.Tilt != 0 {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestZoomRange(t *testing.T) {
	c, _ := newCam(t, camera.Options{})
	for _, z := range []int{99, 401} {
		if _, err := c.SetZoom(z); !errors.Is(err, camera.ErrOutOfRange) {
			t.Errorf("zoom %d: %v", z, err)
		}
	}
	if _, err := c.SetZoom(400); err != nil {
		t.Error(err)
	}
}

func TestFocusRoll(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	if _, err := c.SetFocus(50); err != nil || b.Focus != 50 {
		t.Fatal(err)
	}
	if _, err := c.SetRoll(-101); !errors.Is(err, camera.ErrOutOfRange) {
		t.Error(err)
	}
}

func TestBackendErrorPropagates(t *testing.T) {
	c, b := newCam(t, camera.Options{})
	b.Fail = &camera.TransferError{Op: "x", Err: errors.New("boom")}
	if _, err := c.SetZoom(200); !errors.Is(err, camera.ErrTransfer) {
		t.Errorf("err=%v", err)
	}
}

func TestRegistryExplicitBackend(t *testing.T) {
	c, err := camera.Open(camera.Options{Backend: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	t.Setenv(camera.EnvBackend, "nope")
	if _, err := camera.Open(camera.Options{}); !errors.Is(err, camera.ErrBackendUnavailable) {
		t.Errorf("err=%v", err)
	}
	t.Setenv(camera.EnvBackend, "fake")
	if c, err = camera.Open(camera.Options{}); err != nil {
		t.Errorf("env selection: %v", err)
	} else {
		c.Close()
	}
}
