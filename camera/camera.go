package camera

import (
	"errors"
	"math"
)

func errorsIs(err, target error) bool { return errors.Is(err, target) }

// Camera is a validated, high level view of an opened Backend.
type Camera struct {
	b    Backend
	info Info
	opts Options
}

// Wrap builds a Camera on an already opened Backend (useful for tests and
// custom backends). Camera.Close closes the backend.
func Wrap(b Backend, opts Options) (*Camera, error) {
	info, err := b.Info()
	if err != nil {
		return nil, err
	}
	return &Camera{b: b, info: info, opts: opts}, nil
}

// Close releases the device.
func (c *Camera) Close() error { return c.b.Close() }

// Info returns device identity and ranges.
func (c *Camera) Info() Info { return c.info }

// DryRun reports whether writes are suppressed.
func (c *Camera) DryRun() bool { return c.opts.DryRun }

// PanTilt is an absolute pan/tilt pair in degrees.
type PanTilt struct {
	Pan  float64 `json:"pan"`
	Tilt float64 `json:"tilt"`
}

// Result describes a Set operation.
type Result[T any] struct {
	Requested T    `json:"requested"`
	Applied   T    `json:"applied"` // value sent (or that would be sent in dry-run)
	Clamped   bool `json:"clamped"`
	DryRun    bool `json:"dry_run"`
}

// GetPanTilt returns the last commanded pan/tilt in degrees (not measured).
func (c *Camera) GetPanTilt() (PanTilt, error) {
	p, t, err := c.b.GetPanTilt()
	return PanTilt{p, t}, err
}

// SetPanTilt moves to the absolute pan/tilt in degrees, validating ranges
// (or clamping when Options.Clamp is set).
func (c *Camera) SetPanTilt(pan, tilt float64) (Result[PanTilt], error) {
	res := Result[PanTilt]{Requested: PanTilt{pan, tilt}, DryRun: c.opts.DryRun}
	p, cp, err := c.info.PanRange.check("pan", pan, c.opts.Clamp)
	if err != nil {
		return res, err
	}
	t, ct, err := c.info.TiltRange.check("tilt", tilt, c.opts.Clamp)
	if err != nil {
		return res, err
	}
	res.Applied, res.Clamped = PanTilt{p, t}, cp || ct
	if c.opts.DryRun {
		return res, nil
	}
	return res, c.b.SetPanTilt(p, t)
}

// MovePanTilt moves relative to the current commanded pan/tilt.
func (c *Camera) MovePanTilt(dPan, dTilt float64) (Result[PanTilt], error) {
	cur, err := c.GetPanTilt()
	if err != nil {
		return Result[PanTilt]{}, err
	}
	if math.IsNaN(dPan) || math.IsNaN(dTilt) {
		return Result[PanTilt]{}, ErrInvalidValue
	}
	return c.SetPanTilt(cur.Pan+dPan, cur.Tilt+dTilt)
}

// Center points the camera at pan=0, tilt=0.
func (c *Camera) Center() (Result[PanTilt], error) { return c.SetPanTilt(0, 0) }

// GetZoom returns the raw zoom (100 = 1.0x).
func (c *Camera) GetZoom() (int, error) { return c.b.GetZoom() }

// SetZoom sets the raw zoom (100..400), validating or clamping.
func (c *Camera) SetZoom(zoom int) (Result[int], error) {
	return setInt(c, "zoom", c.info.ZoomRange, zoom, c.b.SetZoom)
}

// MoveZoom changes the zoom relative to the current value.
func (c *Camera) MoveZoom(delta int) (Result[int], error) {
	cur, err := c.b.GetZoom()
	if err != nil {
		return Result[int]{}, err
	}
	return c.SetZoom(cur + delta)
}

// GetFocus returns the raw focus value; ErrUnsupported if unavailable.
func (c *Camera) GetFocus() (int, error) {
	f, ok := c.b.(FocusController)
	if !ok || c.info.Focus == nil {
		return 0, ErrUnsupported
	}
	return f.GetFocus()
}

// SetFocus sets focus (0..100).
func (c *Camera) SetFocus(v int) (Result[int], error) {
	f, ok := c.b.(FocusController)
	if !ok || c.info.Focus == nil {
		return Result[int]{}, ErrUnsupported
	}
	return setInt(c, "focus", *c.info.Focus, v, f.SetFocus)
}

// GetRoll returns the raw roll value; ErrUnsupported if unavailable.
func (c *Camera) GetRoll() (int, error) {
	r, ok := c.b.(RollController)
	if !ok || c.info.Roll == nil {
		return 0, ErrUnsupported
	}
	return r.GetRoll()
}

// SetRoll sets roll (-100..100).
func (c *Camera) SetRoll(v int) (Result[int], error) {
	r, ok := c.b.(RollController)
	if !ok || c.info.Roll == nil {
		return Result[int]{}, ErrUnsupported
	}
	return setInt(c, "roll", *c.info.Roll, v, r.SetRoll)
}

func setInt(c *Camera, name string, rg Range, v int, write func(int) error) (Result[int], error) {
	res := Result[int]{Requested: v, DryRun: c.opts.DryRun}
	f, cl, err := rg.check(name, float64(v), c.opts.Clamp)
	if err != nil {
		return res, err
	}
	res.Applied, res.Clamped = int(f), cl
	if c.opts.DryRun {
		return res, nil
	}
	return res, write(res.Applied)
}
