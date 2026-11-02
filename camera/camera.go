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
	applyQueriedLimits(b, &info)
	return &Camera{b: b, info: info, opts: opts}, nil
}

// applyQueriedLimits replaces the ranges in info by the device's own when the
// backend can query them, and records where the ranges come from.
func applyQueriedLimits(b Backend, info *Info) {
	if info.RangesSource == RangesFromDevice {
		return // backend already filled real ranges
	}
	info.RangesSource = RangesDefault
	q, ok := b.(RangeQuerier)
	if !ok {
		return
	}
	l, err := q.QueryLimits()
	if err != nil {
		info.RangesWarning = "querying device ranges failed, using defaults: " + err.Error()
		return
	}
	info.PanRange, info.TiltRange, info.ZoomRange = l.Pan.Range(), l.Tilt.Range(), l.Zoom.Range()
	info.Controls = map[string]ControlLimits{"pan": l.Pan, "tilt": l.Tilt, "zoom": l.Zoom}
	if l.Focus != nil {
		r := l.Focus.Range()
		info.Focus = &r
		info.Controls["focus"] = *l.Focus
	}
	if l.Roll != nil {
		r := l.Roll.Range()
		info.Roll = &r
		info.Controls["roll"] = *l.Roll
	}
	info.RangesSource = RangesFromDevice
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
	// Quantize to the wire unit (1/3600 deg) so results do not carry float noise.
	p, t = ArcSecToDeg(DegToArcSec(p)), ArcSecToDeg(DegToArcSec(t))
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

// GetAutoFocus reports whether autofocus is on; ErrUnsupported if unavailable.
func (c *Camera) GetAutoFocus() (bool, error) {
	f, ok := c.b.(AutoFocusController)
	if !ok {
		return false, ErrUnsupported
	}
	return f.GetAutoFocus()
}

// SetAutoFocus switches autofocus on or off (nothing is sent in dry-run).
func (c *Camera) SetAutoFocus(on bool) (Result[bool], error) {
	res := Result[bool]{Requested: on, Applied: on, DryRun: c.opts.DryRun}
	f, ok := c.b.(AutoFocusController)
	if !ok {
		return res, ErrUnsupported
	}
	if c.opts.DryRun {
		return res, nil
	}
	return res, f.SetAutoFocus(on)
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
