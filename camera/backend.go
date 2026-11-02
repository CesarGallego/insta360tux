package camera

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// EnvBackend is the environment variable that forces a backend by name.
const EnvBackend = "INSTA360_BACKEND"

// Options configure Open.
type Options struct {
	// Backend forces a backend by name ("libusb", "v4l2", ...). Empty means
	// the INSTA360_BACKEND environment variable, then automatic selection.
	Backend string
	// Device selects a device. Forms: "" (first Insta360 Link), "VID:PID" in
	// hex ("2e1a:4c01"), "BUS.ADDR" in decimal ("20.4"), a serial number, or
	// a backend specific path (e.g. "/dev/video0" for v4l2).
	Device string
	// Timeout for each device transfer. Zero means 2s.
	Timeout time.Duration
	// Clamp makes out-of-range values be clamped instead of rejected.
	Clamp bool
	// DryRun makes Set* methods validate but not write to the device.
	DryRun bool
}

func (o Options) timeout() time.Duration {
	if o.Timeout <= 0 {
		return 2 * time.Second
	}
	return o.Timeout
}

// EffectiveTimeout returns the effective transfer timeout (default 2s).
func (o Options) EffectiveTimeout() time.Duration { return o.timeout() }

// Info describes an opened camera and its limits.
type Info struct {
	Backend      string `json:"backend"`
	Name         string `json:"name,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Serial       string `json:"serial,omitempty"`
	VendorID     uint16 `json:"vendor_id"`
	ProductID    uint16 `json:"product_id"`
	Bus          int    `json:"bus,omitempty"`
	Address      int    `json:"address,omitempty"`
	Path         string `json:"path,omitempty"`

	PanRange  Range  `json:"pan_range_deg"`
	TiltRange Range  `json:"tilt_range_deg"`
	ZoomRange Range  `json:"zoom_range"`
	Focus     *Range `json:"focus_range,omitempty"` // nil when unsupported
	Roll      *Range `json:"roll_range,omitempty"`  // nil when unsupported
}

// Backend is the transport interface. Implementations talk to the camera
// (libusb control transfers, V4L2 ioctls, ...) and always speak degrees for
// pan/tilt and raw device units (100..400) for zoom. Validation, clamping and
// dry-run are handled by Camera, so backends just execute.
//
// A Backend value is unopened until Open succeeds. Methods may be called from
// one goroutine at a time.
type Backend interface {
	// Name returns the registry name ("libusb", "v4l2", ...).
	Name() string
	// Open connects to the device selected by opts.
	Open(opts Options) error
	// Close releases the device. It is safe to call on an unopened backend.
	Close() error
	// Info returns device identity and ranges.
	Info() (Info, error)
	// GetPanTilt returns the last commanded pan/tilt in degrees (not a measured position).
	GetPanTilt() (pan, tilt float64, err error)
	// SetPanTilt commands an absolute pan/tilt in degrees.
	SetPanTilt(pan, tilt float64) error
	// GetZoom returns the current zoom, raw units 100..400 (100 = 1.0x).
	GetZoom() (int, error)
	// SetZoom commands an absolute zoom, raw units.
	SetZoom(zoom int) error
}

// FocusController is optionally implemented by backends with focus control
// (raw 0..100). Check with a type assertion or Info.Focus != nil.
type FocusController interface {
	GetFocus() (int, error)
	SetFocus(v int) error
}

// RollController is optionally implemented by backends with roll control
// (raw -100..100). Check with a type assertion or Info.Roll != nil.
type RollController interface {
	GetRoll() (int, error)
	SetRoll(v int) error
}

// DeviceInfo identifies an attached camera, as returned by List.
type DeviceInfo struct {
	Backend   string `json:"backend"`
	VendorID  uint16 `json:"vendor_id"`
	ProductID uint16 `json:"product_id"`
	Bus       int    `json:"bus,omitempty"`
	Address   int    `json:"address,omitempty"`
	Serial    string `json:"serial,omitempty"`
	Path      string `json:"path,omitempty"`
	// Selector is a string valid for Options.Device that selects this device.
	Selector string `json:"selector"`
}

// Factory describes a backend in the registry.
type Factory struct {
	// Name is the backend name.
	Name string
	// Priority orders automatic selection (higher first). Suggested: v4l2 100, libusb 50.
	Priority int
	// Explicit backends (e.g. the fake one) are never chosen automatically.
	Explicit bool
	// New returns an unopened backend.
	New func() Backend
	// List enumerates devices; may be nil.
	List func(opts Options) ([]DeviceInfo, error)
}

var (
	regMu    sync.RWMutex
	registry = map[string]Factory{}
)

// Register adds a backend. It is meant to be called from init functions in
// files guarded by build tags. It panics on duplicate or invalid names.
func Register(f Factory) {
	if f.Name == "" || f.New == nil {
		panic("camera: Register requires Name and New")
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[f.Name]; dup {
		panic("camera: duplicate backend " + f.Name)
	}
	registry[f.Name] = f
}

// Backends returns the registered backend names, best automatic choice first.
func Backends() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	return sortedLocked(false)
}

func sortedLocked(skipExplicit bool) []string {
	var fs []Factory
	for _, f := range registry {
		if skipExplicit && f.Explicit {
			continue
		}
		fs = append(fs, f)
	}
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Priority != fs[j].Priority {
			return fs[i].Priority > fs[j].Priority
		}
		return fs[i].Name < fs[j].Name
	})
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = f.Name
	}
	return names
}

// chosen returns the explicit choice (option, then env), or "" for auto.
func chosen(opts Options) string {
	if opts.Backend != "" {
		return opts.Backend
	}
	return os.Getenv(EnvBackend)
}

func lookup(name string) (Factory, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := registry[name]
	if !ok {
		return Factory{}, fmt.Errorf("%w: %q (available: %v)", ErrBackendUnavailable, name, sortedLocked(false))
	}
	return f, nil
}

// List enumerates cameras. With an explicit backend it lists that one;
// otherwise it merges all automatic backends.
func List(opts Options) ([]DeviceInfo, error) {
	if name := chosen(opts); name != "" {
		f, err := lookup(name)
		if err != nil {
			return nil, err
		}
		if f.List == nil {
			return nil, fmt.Errorf("%w: backend %s cannot list", ErrUnsupported, name)
		}
		return f.List(opts)
	}
	regMu.RLock()
	names := sortedLocked(true)
	regMu.RUnlock()
	if len(names) == 0 {
		return nil, fmt.Errorf("%w: no backend compiled in", ErrBackendUnavailable)
	}
	var out []DeviceInfo
	var firstErr error
	for _, n := range names {
		f, _ := lookup(n)
		if f.List == nil {
			continue
		}
		ds, err := f.List(opts)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, ds...)
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// Open selects a backend (opts.Backend, $INSTA360_BACKEND, or the first
// automatic backend that opens successfully, by priority) and returns a Camera.
func Open(opts Options) (*Camera, error) {
	if name := chosen(opts); name != "" {
		f, err := lookup(name)
		if err != nil {
			return nil, err
		}
		return openWith(f, opts)
	}
	regMu.RLock()
	names := sortedLocked(true)
	regMu.RUnlock()
	if len(names) == 0 {
		return nil, fmt.Errorf("%w: no backend compiled in", ErrBackendUnavailable)
	}
	var errs []error
	for _, n := range names {
		f, _ := lookup(n)
		c, err := openWith(f, opts)
		if err == nil {
			return c, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", n, err))
		if len(names) == 1 {
			return nil, err
		}
	}
	// Report the most informative error: prefer one that is not "no device"/unavailable.
	for _, e := range errs {
		if !isSoft(e) {
			return nil, e
		}
	}
	return nil, errs[0]
}

func isSoft(err error) bool {
	return errorsIs(err, ErrNoDevice) || errorsIs(err, ErrBackendUnavailable)
}

func openWith(f Factory, opts Options) (*Camera, error) {
	b := f.New()
	if err := b.Open(opts); err != nil {
		b.Close()
		return nil, err
	}
	c, err := Wrap(b, opts)
	if err != nil {
		b.Close()
		return nil, err
	}
	return c, nil
}
