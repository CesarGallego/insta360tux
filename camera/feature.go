package camera

import (
	"fmt"
	"strconv"
	"strings"
)

// Feature is a named on/off function stored as one bit of the unit 9
// selector 0x1b mask (u16 little endian). The mask is always modified with
// read-modify-write so unknown bits (0x0020 is always set) are preserved.
// Bits were confirmed by the user in the official app with a guided capture
// session (see PROTOCOL.md), except where noted there.
type Feature string

const (
	FeatureHDR          Feature = "hdr"               // 0x004
	FeatureGestures     Feature = "gestures"          // 0x010 master switch (HYPOTHESIS, consistent with the capture)
	FeatureOneTouch     Feature = "onetouch-tracking" // 0x080 one-touch AI tracking toggle
	FeatureAutoTracking Feature = "auto-tracking"     // 0x100 tracks when the gimbal is aimed manually
	FeatureSmartFraming Feature = "smart-framing"     // 0x200 on by default
	FeatureVerticalLock Feature = "vertical-lock"     // 0x400 gimbal fixed at 90 deg, resolution unchanged
)

// FeatureInfo documents a feature.
type FeatureInfo struct {
	Name   Feature `json:"name"`
	Bit    uint16  `json:"bit"`
	Caveat string  `json:"caveat,omitempty"`
	Unsure bool    `json:"hypothesis,omitempty"`
}

// Features lists the settable features in CLI order.
var Features = []FeatureInfo{
	{FeatureHDR, 0x004, "", false},
	{FeatureGestures, 0x010, "master switch; individual gestures are in the gesture mask (see Gestures)", true},
	{FeatureOneTouch, 0x080, "makes the camera follow a person by itself", false},
	{FeatureAutoTracking, 0x100, "the camera moves by itself when the gimbal is aimed manually", false},
	{FeatureSmartFraming, 0x200, "", false},
	{FeatureVerticalLock, 0x400, "moves the gimbal to 90 degrees", false},
}

// ParseFeature resolves a user supplied name (case-insensitive).
func ParseFeature(s string) (FeatureInfo, error) {
	n := Feature(strings.ToLower(strings.TrimSpace(s)))
	for _, f := range Features {
		if f.Name == n {
			return f, nil
		}
	}
	names := make([]Feature, len(Features))
	for i, f := range Features {
		names[i] = f.Name
	}
	return FeatureInfo{}, fmt.Errorf("%w: unknown feature %q (valid: %v)", ErrInvalidValue, s, names)
}

// FeatureController is optionally implemented by backends that can read and
// write the unit 9 selector 0x1b mask. SetFeatureMask writes the whole mask:
// use Camera.SetFeature, which does the read-modify-write.
type FeatureController interface {
	GetFeatureMask() (uint16, error)
	SetFeatureMask(mask uint16) error
}

// GestureController reads and writes the unit 9 selector 0x05 gesture mask.
type GestureController interface {
	GetGestureMask() (byte, error)
	SetGestureMask(mask byte) error
}

// Gesture mask bits (unit 9 selector 0x05).
const (
	GestureTracking   byte = 0x02
	GestureZoom       byte = 0x04
	GestureWhiteboard byte = 0x08
)

// TrackingSpeed is the unit 9 selector 0x12 value.
type TrackingSpeed string

const (
	SpeedQuick  TrackingSpeed = "quick"  // 0
	SpeedNormal TrackingSpeed = "normal" // 1
	SpeedSlow   TrackingSpeed = "slow"   // 2
)

// TrackingSpeeds lists the settable speeds.
var TrackingSpeeds = []TrackingSpeed{SpeedQuick, SpeedNormal, SpeedSlow}

// ParseTrackingSpeed resolves a user supplied name (case-insensitive).
func ParseTrackingSpeed(s string) (TrackingSpeed, error) {
	v := TrackingSpeed(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := TrackingSpeedByte(v); !ok {
		return "", fmt.Errorf("%w: unknown tracking speed %q (valid: %v)", ErrInvalidValue, s, TrackingSpeeds)
	}
	return v, nil
}

// TrackingSpeedByte returns the wire value of a speed.
func TrackingSpeedByte(s TrackingSpeed) (byte, bool) {
	for i, v := range TrackingSpeeds {
		if v == s {
			b := [...]byte{0, 1, 2}[i]
			return b, true
		}
	}
	return 0, false
}

// TrackingSpeedFromByte names a wire value ("unknown(0xNN)" when unseen).
func TrackingSpeedFromByte(b byte) TrackingSpeed {
	if int(b) < len(TrackingSpeeds) {
		return TrackingSpeeds[b]
	}
	return TrackingSpeed(fmt.Sprintf("unknown(0x%02x)", b))
}

// TrackingSpeedController is optionally implemented by backends.
type TrackingSpeedController interface {
	GetTrackingSpeed() (TrackingSpeed, error)
	SetTrackingSpeed(TrackingSpeed) error
}

// PowerLineFrequency is the anti-flicker setting in Hz: 50 or 60. When
// reading, PowerLineOther is returned for the UVC values "disabled" (0) and
// "auto" (3) or anything unexpected.
type PowerLineFrequency int

const (
	PowerLine50    PowerLineFrequency = 50
	PowerLine60    PowerLineFrequency = 60
	PowerLineOther PowerLineFrequency = 0
)

// ParsePowerLine accepts "50", "60", "50hz", "60Hz".
func ParsePowerLine(s string) (PowerLineFrequency, error) {
	t := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "hz")
	n, err := strconv.Atoi(strings.TrimSpace(t))
	if err != nil || (n != 50 && n != 60) {
		return 0, fmt.Errorf("%w: power line frequency %q must be 50 or 60", ErrInvalidValue, s)
	}
	return PowerLineFrequency(n), nil
}

// PowerLineByte is the standard UVC / V4L2 value: 1 = 50 Hz, 2 = 60 Hz.
func PowerLineByte(f PowerLineFrequency) (byte, bool) {
	switch f {
	case PowerLine50:
		return 1, true
	case PowerLine60:
		return 2, true
	}
	return 0, false
}

// PowerLineFromByte maps the UVC value back; 0 and 3 give PowerLineOther.
func PowerLineFromByte(b byte) PowerLineFrequency {
	switch b {
	case 1:
		return PowerLine50
	case 2:
		return PowerLine60
	}
	return PowerLineOther
}

// PowerLineController is optionally implemented by backends.
type PowerLineController interface {
	GetPowerLine() (PowerLineFrequency, error)
	SetPowerLine(PowerLineFrequency) error
}

// ---- Camera level API ----

// FeatureState is the state of one feature.
type FeatureState struct {
	Name Feature `json:"name"`
	On   bool    `json:"on"`
}

// FeatureChange describes a SetFeature.
type FeatureChange struct {
	Feature    Feature `json:"feature"`
	On         bool    `json:"on"`
	MaskBefore uint16  `json:"mask_before"`
	MaskAfter  uint16  `json:"mask_after"`
	DryRun     bool    `json:"dry_run"`
}

// GetFeatureMask returns the raw mask. ErrUnsupported if the backend cannot.
func (c *Camera) GetFeatureMask() (uint16, error) {
	f, ok := c.b.(FeatureController)
	if !ok {
		return 0, ErrUnsupported
	}
	return f.GetFeatureMask()
}

// GetFeatures reads the mask once and reports every feature.
func (c *Camera) GetFeatures() ([]FeatureState, uint16, error) {
	m, err := c.GetFeatureMask()
	if err != nil {
		return nil, 0, err
	}
	out := make([]FeatureState, len(Features))
	for i, f := range Features {
		out[i] = FeatureState{f.Name, m&f.Bit != 0}
	}
	return out, m, nil
}

// SetFeature switches one feature with read-modify-write of the mask, so
// every other bit (known or not) is preserved. Nothing is written in dry-run.
func (c *Camera) SetFeature(name Feature, on bool) (FeatureChange, error) {
	res := FeatureChange{Feature: name, On: on, DryRun: c.opts.DryRun}
	fi, err := ParseFeature(string(name))
	if err != nil {
		return res, err
	}
	fc, ok := c.b.(FeatureController)
	if !ok {
		return res, ErrUnsupported
	}
	old, err := fc.GetFeatureMask()
	if err != nil {
		return res, err
	}
	res.MaskBefore, res.MaskAfter = old, old&^fi.Bit
	if on {
		res.MaskAfter = old | fi.Bit
	}
	if c.opts.DryRun {
		return res, nil
	}
	return res, fc.SetFeatureMask(res.MaskAfter)
}

// Gestures is the state of the gesture controls.
type Gestures struct {
	Enabled    bool `json:"enabled"` // master switch: feature mask bit 0x010 (hypothesis)
	Tracking   bool `json:"tracking"`
	Zoom       bool `json:"zoom"`
	Whiteboard bool `json:"whiteboard"`
	Mask       byte `json:"mask"` // raw selector 0x05 value
}

// GestureUpdate selects what SetGestures changes; nil fields are left alone.
type GestureUpdate struct {
	Enabled, Tracking, Zoom, Whiteboard *bool
}

// GestureChange describes a SetGestures.
type GestureChange struct {
	Before Gestures `json:"before"`
	After  Gestures `json:"after"`
	DryRun bool     `json:"dry_run"`
}

func decodeGestures(fm uint16, gm byte) Gestures {
	return Gestures{fm&0x010 != 0, gm&GestureTracking != 0, gm&GestureZoom != 0, gm&GestureWhiteboard != 0, gm}
}

// GetGestures reads the master switch and the per-gesture mask.
func (c *Camera) GetGestures() (Gestures, error) {
	fc, ok1 := c.b.(FeatureController)
	gc, ok2 := c.b.(GestureController)
	if !ok1 || !ok2 {
		return Gestures{}, ErrUnsupported
	}
	fm, err := fc.GetFeatureMask()
	if err != nil {
		return Gestures{}, err
	}
	gm, err := gc.GetGestureMask()
	if err != nil {
		return Gestures{}, err
	}
	return decodeGestures(fm, gm), nil
}

// SetGestures applies u with read-modify-write on both masks (unknown bits
// preserved). A mask is only written when it changes.
func (c *Camera) SetGestures(u GestureUpdate) (GestureChange, error) {
	res := GestureChange{DryRun: c.opts.DryRun}
	fc, ok1 := c.b.(FeatureController)
	gc, ok2 := c.b.(GestureController)
	if !ok1 || !ok2 {
		return res, ErrUnsupported
	}
	fm, err := fc.GetFeatureMask()
	if err != nil {
		return res, err
	}
	gm, err := gc.GetGestureMask()
	if err != nil {
		return res, err
	}
	res.Before = decodeGestures(fm, gm)
	nf, ng := fm, gm
	if u.Enabled != nil {
		nf = setBit16(nf, 0x010, *u.Enabled)
	}
	for _, g := range []struct {
		v   *bool
		bit byte
	}{{u.Tracking, GestureTracking}, {u.Zoom, GestureZoom}, {u.Whiteboard, GestureWhiteboard}} {
		if g.v != nil {
			if *g.v {
				ng |= g.bit
			} else {
				ng &^= g.bit
			}
		}
	}
	res.After = decodeGestures(nf, ng)
	if c.opts.DryRun {
		return res, nil
	}
	// Mask 0x05 first when enabling, last when disabling: mirrors the app's order.
	if ng != gm && nf&0x010 != 0 {
		if err := gc.SetGestureMask(ng); err != nil {
			return res, err
		}
		gm = ng
	}
	if nf != fm {
		if err := fc.SetFeatureMask(nf); err != nil {
			return res, err
		}
	}
	if ng != gm {
		return res, gc.SetGestureMask(ng)
	}
	return res, nil
}

func setBit16(m, bit uint16, on bool) uint16 {
	if on {
		return m | bit
	}
	return m &^ bit
}

// GetTrackingSpeed reads the tracking speed.
func (c *Camera) GetTrackingSpeed() (TrackingSpeed, error) {
	t, ok := c.b.(TrackingSpeedController)
	if !ok {
		return "", ErrUnsupported
	}
	return t.GetTrackingSpeed()
}

// SetTrackingSpeed sets the tracking speed (nothing is sent in dry-run).
func (c *Camera) SetTrackingSpeed(s TrackingSpeed) (Result[TrackingSpeed], error) {
	res := Result[TrackingSpeed]{Requested: s, Applied: s, DryRun: c.opts.DryRun}
	if _, ok := TrackingSpeedByte(s); !ok {
		return res, fmt.Errorf("%w: unknown tracking speed %q", ErrInvalidValue, s)
	}
	t, ok := c.b.(TrackingSpeedController)
	if !ok {
		return res, ErrUnsupported
	}
	if c.opts.DryRun {
		return res, nil
	}
	return res, t.SetTrackingSpeed(s)
}

// GetPowerLine reads the anti-flicker frequency (PowerLineOther if the
// camera reports disabled/auto).
func (c *Camera) GetPowerLine() (PowerLineFrequency, error) {
	p, ok := c.b.(PowerLineController)
	if !ok {
		return 0, ErrUnsupported
	}
	return p.GetPowerLine()
}

// SetPowerLine sets 50 or 60 Hz anti-flicker (nothing is sent in dry-run).
func (c *Camera) SetPowerLine(f PowerLineFrequency) (Result[PowerLineFrequency], error) {
	res := Result[PowerLineFrequency]{Requested: f, Applied: f, DryRun: c.opts.DryRun}
	if _, ok := PowerLineByte(f); !ok {
		return res, fmt.Errorf("%w: power line frequency %d must be 50 or 60", ErrInvalidValue, int(f))
	}
	p, ok := c.b.(PowerLineController)
	if !ok {
		return res, ErrUnsupported
	}
	if c.opts.DryRun {
		return res, nil
	}
	return res, p.SetPowerLine(f)
}
