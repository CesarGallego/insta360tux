// Package fake provides an in-memory camera backend for tests and for trying
// the CLI without hardware. Importing the package registers backend "fake",
// which is only used when selected explicitly (--backend fake).
package fake

import "github.com/CesarGallego/insta360tux/camera"

// Backend is an in-memory camera.
type Backend struct {
	Pan, Tilt         float64
	Zoom, Focus, Roll int
	AutoFocus         bool
	Mode              camera.Mode // zero value means normal
	FeatureMask       uint16      // unit 9 sel 0x1b; New() starts at the app's base state 0x0220
	GestureMask       byte
	Speed             camera.TrackingSpeed // zero value means normal
	PowerLine         camera.PowerLineFrequency
	Opened            bool
	Writes            int // number of Set* calls reaching the backend
	Fail              error
}

// New returns a fake at home position (zoom 100).
func New() *Backend {
	return &Backend{Zoom: 100, FeatureMask: 0x0220, PowerLine: camera.PowerLine50}
}

func (b *Backend) Name() string                          { return "fake" }
func (b *Backend) Open(camera.Options) error             { b.Opened = true; return b.Fail }
func (b *Backend) Close() error                          { b.Opened = false; return nil }
func (b *Backend) GetZoom() (int, error)                 { return b.Zoom, b.Fail }
func (b *Backend) GetFocus() (int, error)                { return b.Focus, b.Fail }
func (b *Backend) GetRoll() (int, error)                 { return b.Roll, b.Fail }
func (b *Backend) GetPanTilt() (float64, float64, error) { return b.Pan, b.Tilt, b.Fail }

func (b *Backend) SetPanTilt(p, t float64) error { b.Writes++; b.Pan, b.Tilt = p, t; return b.Fail }
func (b *Backend) SetZoom(z int) error           { b.Writes++; b.Zoom = z; return b.Fail }
func (b *Backend) SetFocus(v int) error          { b.Writes++; b.Focus = v; return b.Fail }
func (b *Backend) GetAutoFocus() (bool, error)   { return b.AutoFocus, b.Fail }
func (b *Backend) SetAutoFocus(on bool) error    { b.Writes++; b.AutoFocus = on; return b.Fail }
func (b *Backend) SetRoll(v int) error           { b.Writes++; b.Roll = v; return b.Fail }

func (b *Backend) GetMode() (camera.Mode, error) {
	if b.Mode == "" {
		return camera.ModeNormal, b.Fail
	}
	return b.Mode, b.Fail
}
func (b *Backend) SetMode(m camera.Mode) error { b.Writes++; b.Mode = m; return b.Fail }

func (b *Backend) GetFeatureMask() (uint16, error) { return b.FeatureMask, b.Fail }
func (b *Backend) SetFeatureMask(m uint16) error   { b.Writes++; b.FeatureMask = m; return b.Fail }
func (b *Backend) GetGestureMask() (byte, error)   { return b.GestureMask, b.Fail }
func (b *Backend) SetGestureMask(m byte) error     { b.Writes++; b.GestureMask = m; return b.Fail }
func (b *Backend) GetTrackingSpeed() (camera.TrackingSpeed, error) {
	if b.Speed == "" {
		return camera.SpeedNormal, b.Fail
	}
	return b.Speed, b.Fail
}
func (b *Backend) SetTrackingSpeed(s camera.TrackingSpeed) error {
	b.Writes++
	b.Speed = s
	return b.Fail
}
func (b *Backend) GetPowerLine() (camera.PowerLineFrequency, error) {
	return b.PowerLine, b.Fail
}
func (b *Backend) SetPowerLine(f camera.PowerLineFrequency) error {
	b.Writes++
	b.PowerLine = f
	return b.Fail
}

func (b *Backend) Info() (camera.Info, error) {
	f, r := camera.DefaultFocusRange, camera.DefaultRollRange
	return camera.Info{
		Backend: "fake", Name: "Fake Insta360 Link", VendorID: 0x2e1a, ProductID: 0x4c01,
		PanRange: camera.DefaultPanRange, TiltRange: camera.DefaultTiltRange,
		ZoomRange: camera.DefaultZoomRange, Focus: &f, Roll: &r,
	}, nil
}

func init() {
	camera.Register(camera.Factory{
		Name: "fake", Explicit: true,
		New: func() camera.Backend { return New() },
		List: func(camera.Options) ([]camera.DeviceInfo, error) {
			return []camera.DeviceInfo{{Backend: "fake", VendorID: 0x2e1a, ProductID: 0x4c01, Selector: "2e1a:4c01"}}, nil
		},
	})
}
