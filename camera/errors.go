package camera

import (
	"errors"
	"fmt"
)

// Sentinel errors. Use errors.Is to test for them.
var (
	// ErrNoDevice: no matching camera was found.
	ErrNoDevice = errors.New("camera: no matching device found")
	// ErrBackendUnavailable: the requested backend is not compiled in or
	// cannot run on this system.
	ErrBackendUnavailable = errors.New("camera: backend unavailable")
	// ErrUnsupported: the backend or device lacks the requested feature.
	ErrUnsupported = errors.New("camera: operation not supported")
	// ErrOutOfRange: a value is outside the device range (see RangeError).
	ErrOutOfRange = errors.New("camera: value out of range")
	// ErrInvalidValue: a value is not a usable number (NaN, Inf, ...).
	ErrInvalidValue = errors.New("camera: invalid value")
	// ErrPermission: the OS denied access to the device, or it is busy.
	ErrPermission = errors.New("camera: permission denied or device busy")
	// ErrTimeout: the device did not answer in time.
	ErrTimeout = errors.New("camera: timeout")
	// ErrTransfer: a USB/driver transfer failed (see TransferError).
	ErrTransfer = errors.New("camera: transfer failed")
)

// RangeError reports a value outside [Min, Max]. It matches ErrOutOfRange.
type RangeError struct {
	Name     string // "pan", "tilt", "zoom", ...
	Value    float64
	Min, Max float64
}

func (e *RangeError) Error() string {
	return fmt.Sprintf("%s %g out of range [%g, %g]", e.Name, e.Value, e.Min, e.Max)
}

// Is makes errors.Is(err, ErrOutOfRange) true.
func (e *RangeError) Is(target error) bool { return target == ErrOutOfRange }

// TransferError wraps a low level failure of operation Op (e.g. "GET pan/tilt").
// It matches ErrTransfer, and the wrapped error is available through Unwrap.
type TransferError struct {
	Op  string
	Err error
}

func (e *TransferError) Error() string { return fmt.Sprintf("%s: %v", e.Op, e.Err) }
func (e *TransferError) Unwrap() error { return e.Err }
func (e *TransferError) Is(target error) bool {
	return target == ErrTransfer
}
