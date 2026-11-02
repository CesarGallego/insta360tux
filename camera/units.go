package camera

import "math"

// ArcSecPerDegree is the wire unit of pan/tilt: 1 degree = 3600 arc-seconds.
const ArcSecPerDegree = 3600

// DegToArcSec converts degrees to arc-seconds, rounding to the nearest unit.
func DegToArcSec(deg float64) int32 { return int32(math.Round(deg * ArcSecPerDegree)) }

// ArcSecToDeg converts arc-seconds to degrees.
func ArcSecToDeg(as int32) float64 { return float64(as) / ArcSecPerDegree }

// Range is a closed interval.
type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// Contains reports whether v is inside the range.
func (r Range) Contains(v float64) bool { return v >= r.Min && v <= r.Max }

// Clamp limits v to the range.
func (r Range) Clamp(v float64) float64 { return math.Min(math.Max(v, r.Min), r.Max) }

// check validates v against r; with clamp it returns the clamped value
// instead of a RangeError. The bool reports whether clamping changed v.
func (r Range) check(name string, v float64, clamp bool) (float64, bool, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false, ErrInvalidValue
	}
	if r.Contains(v) {
		return v, false, nil
	}
	if clamp {
		return r.Clamp(v), true, nil
	}
	return 0, false, &RangeError{Name: name, Value: v, Min: r.Min, Max: r.Max}
}

// Insta360 Link ranges (see PROTOCOL.md). Backends may report others in Info.
var (
	DefaultPanRange   = Range{Min: -522000.0 / ArcSecPerDegree, Max: 522000.0 / ArcSecPerDegree} // +-145 deg
	DefaultTiltRange  = Range{Min: -324000.0 / ArcSecPerDegree, Max: 360000.0 / ArcSecPerDegree} // -90..+100 deg
	DefaultZoomRange  = Range{Min: 100, Max: 400}                                                // 100 = 1.0x
	DefaultFocusRange = Range{Min: 0, Max: 100}
	DefaultRollRange  = Range{Min: -100, Max: 100}
)

// ZoomFactor converts the raw zoom value (100..400) into a magnification (1.0..4.0).
func ZoomFactor(raw int) float64 { return float64(raw) / 100 }
