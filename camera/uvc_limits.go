package camera

import (
	"encoding/binary"
	"fmt"
)

// UVC class-specific request codes used to query control limits.
const (
	UVCGetMin  = 0x82
	UVCGetMax  = 0x83
	UVCGetRes  = 0x84
	UVCGetInfo = 0x86
	UVCGetDef  = 0x87
)

// ControlLimits are the limits a device reports for one control through
// GET_MIN / GET_MAX / GET_RES / GET_DEF. Pan and tilt are in degrees, the
// rest in raw device units. Res and Def are 0 when the device did not answer.
type ControlLimits struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
	Res float64 `json:"res"`
	Def float64 `json:"def"`
}

// Range returns the [Min, Max] interval.
func (l ControlLimits) Range() Range { return Range{Min: l.Min, Max: l.Max} }

// Limits groups the limits of every control a backend could query. Focus and
// Roll are nil when the device did not answer for them.
type Limits struct {
	Pan, Tilt, Zoom ControlLimits
	Focus, Roll     *ControlLimits
}

// RangeQuerier is optionally implemented by backends that can ask the device
// for its real ranges. Wrap uses it to fill Info; on error the built-in
// defaults stay and Info.RangesWarning explains why.
type RangeQuerier interface {
	QueryLimits() (Limits, error)
}

// Values of Info.RangesSource.
const (
	RangesFromDevice = "device"
	RangesDefault    = "default"
)

// UVCGetter performs a class GET request on the Camera Terminal: request
// code, control selector and expected length.
type UVCGetter func(req uint8, sel uint16, n int) ([]byte, error)

// QueryUVCLimits reads limits of the standard PTZ controls (pan/tilt 0x0D,
// zoom 0x0B, focus 0x06, roll 0x0F) through get. Pan/tilt, zoom MIN/MAX are
// mandatory; everything else is best effort (focus and roll stay nil when
// they cannot be read). It is exported so any UVC transport can reuse it.
func QueryUVCLimits(get UVCGetter) (Limits, error) {
	var l Limits
	var err error
	if l.Pan, l.Tilt, err = queryPanTilt(get); err != nil {
		return Limits{}, err
	}
	if l.Zoom, err = queryScalar(get, 0x0B, false); err != nil {
		return Limits{}, fmt.Errorf("zoom: %w", err)
	}
	if f, err := queryScalar(get, 0x06, false); err == nil {
		l.Focus = &f
	}
	if r, err := queryScalar(get, 0x0F, true); err == nil {
		l.Roll = &r
	}
	return l, nil
}

func queryPanTilt(get UVCGetter) (pan, tilt ControlLimits, err error) {
	read := func(req uint8, optional bool) (p, t float64, ok bool, err error) {
		d, err := get(req, 0x0D, 8)
		if err != nil || len(d) != 8 {
			if optional {
				return 0, 0, false, nil
			}
			if err == nil {
				err = fmt.Errorf("short read: %d of 8 bytes", len(d))
			}
			return 0, 0, false, err
		}
		return ArcSecToDeg(int32(binary.LittleEndian.Uint32(d[0:4]))),
			ArcSecToDeg(int32(binary.LittleEndian.Uint32(d[4:8]))), true, nil
	}
	var e error
	if pan.Min, tilt.Min, _, e = read(UVCGetMin, false); e != nil {
		return pan, tilt, fmt.Errorf("pan/tilt GET_MIN: %w", e)
	}
	if pan.Max, tilt.Max, _, e = read(UVCGetMax, false); e != nil {
		return pan, tilt, fmt.Errorf("pan/tilt GET_MAX: %w", e)
	}
	pan.Res, tilt.Res, _, _ = read(UVCGetRes, true)
	pan.Def, tilt.Def, _, _ = read(UVCGetDef, true)
	if !(pan.Min < pan.Max) || !(tilt.Min < tilt.Max) {
		return pan, tilt, fmt.Errorf("pan/tilt: implausible limits pan [%g,%g] tilt [%g,%g]", pan.Min, pan.Max, tilt.Min, tilt.Max)
	}
	return pan, tilt, nil
}

func queryScalar(get UVCGetter, sel uint16, signed bool) (ControlLimits, error) {
	read := func(req uint8) (float64, error) {
		d, err := get(req, sel, 2)
		if err != nil {
			return 0, err
		}
		if len(d) != 2 {
			return 0, fmt.Errorf("short read: %d of 2 bytes", len(d))
		}
		u := binary.LittleEndian.Uint16(d)
		if signed {
			return float64(int16(u)), nil
		}
		return float64(u), nil
	}
	var l ControlLimits
	var err error
	if l.Min, err = read(UVCGetMin); err != nil {
		return l, fmt.Errorf("sel 0x%02X GET_MIN: %w", sel, err)
	}
	if l.Max, err = read(UVCGetMax); err != nil {
		return l, fmt.Errorf("sel 0x%02X GET_MAX: %w", sel, err)
	}
	if !(l.Min < l.Max) {
		return l, fmt.Errorf("sel 0x%02X: implausible limits [%g,%g]", sel, l.Min, l.Max)
	}
	l.Res, _ = read(UVCGetRes)
	l.Def, _ = read(UVCGetDef)
	return l, nil
}
