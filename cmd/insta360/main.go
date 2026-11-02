// Command insta360 controls the gimbal of an Insta360 Link camera.
// See 'insta360 --help' or 'insta360 help-agent'.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/CesarGallego/insta360tux/camera"
	_ "github.com/CesarGallego/insta360tux/camera/fake" // backend "fake" (explicit only)
)

var version = "dev"

type globals struct {
	json    bool
	jsonSet bool
	device  string
	backend string
	dryRun  bool
	timeout time.Duration
	clamp   bool

	keepStream bool
	resolution string
	fps        int
	hold       time.Duration
}

// boolFlag records whether it was set explicitly.
type jsonFlag struct{ g *globals }

func (j jsonFlag) String() string   { return "" }
func (j jsonFlag) IsBoolFlag() bool { return true }
func (j jsonFlag) Set(s string) error {
	v := s != "false" && s != "0"
	j.g.json, j.g.jsonSet = v, true
	return nil
}

func (g *globals) register(fs *flag.FlagSet) {
	fs.Var(jsonFlag{g}, "json", "")
	fs.StringVar(&g.device, "device", g.device, "")
	fs.StringVar(&g.backend, "backend", g.backend, "")
	fs.BoolVar(&g.dryRun, "dry-run", g.dryRun, "")
	fs.DurationVar(&g.timeout, "timeout", g.timeout, "")
	fs.BoolVar(&g.clamp, "clamp", g.clamp, "")
	fs.BoolVar(&g.keepStream, "keep-stream", g.keepStream, "")
	fs.StringVar(&g.resolution, "resolution", g.resolution, "")
	fs.IntVar(&g.fps, "fps", g.fps, "")
	fs.DurationVar(&g.hold, "stream-hold", g.hold, "")
}

func (g *globals) options() camera.Options {
	return camera.Options{Backend: g.backend, Device: g.device, Timeout: g.timeout, Clamp: g.clamp, DryRun: g.dryRun}
}

type app struct {
	g        globals
	out, err io.Writer
	isTTY    bool
}

func main() {
	fi, _ := os.Stdout.Stat()
	tty := fi != nil && fi.Mode()&os.ModeCharDevice != 0
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, tty))
}

func newFS(name string, a *app) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	a.g.register(fs)
	return fs
}

// globalValueFlags take a separate value argument ("--device X").
var globalValueFlags = map[string]bool{"device": true, "backend": true, "timeout": true, "resolution": true, "fps": true, "stream-hold": true}

// splitCommand finds the command word: the first argument that is not a flag
// (nor the value of a global value flag). It returns the index, or -1.
func splitCommand(args []string) int {
	for i := 0; i < len(args); i++ {
		s := args[i]
		if s == "--" {
			return -1
		}
		if !strings.HasPrefix(s, "-") || s == "-" {
			return i
		}
		name := strings.TrimLeft(s, "-")
		if !strings.Contains(name, "=") && globalValueFlags[name] {
			i++
		}
	}
	return -1
}

// parseInterleaved parses flags and positionals in any order. Everything after
// a "--" argument is positional.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var tail []string
	for i, s := range args {
		if s == "--" {
			args, tail = args[:i], args[i+1:]
			break
		}
	}
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return append(pos, tail...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func run(args []string, stdout, stderr io.Writer, tty bool) int {
	a := &app{out: stdout, err: stderr, isTTY: tty}
	a.g.timeout = 2 * time.Second
	a.g.hold = 3 * time.Second

	ci := splitCommand(args)
	if ci < 0 {
		for _, s := range args {
			if s == "--" {
				break
			}
			if s == "-h" || s == "--help" || s == "-help" {
				fmt.Fprint(stdout, overview)
				return exitOK
			}
		}
		// Only flags (or nothing): validate them for a good error, then show usage.
		if len(args) > 0 {
			gfs := newFS("insta360", a)
			if err := gfs.Parse(args); err != nil {
				a.finish()
				return a.fail(exitUsage, "usage", err)
			}
		}
		a.finish()
		fmt.Fprint(stderr, overview)
		return exitUsage
	}
	if hasHelp(args[:ci]) {
		fmt.Fprint(stdout, overview)
		return exitOK
	}
	cmd := args[ci]
	rest := append(append([]string{}, args[:ci]...), args[ci+1:]...)

	switch cmd {
	case "help", "help-agent", "--help-all":
		a.finish()
		if cmd == "help" {
			for _, s := range rest {
				if !strings.HasPrefix(s, "-") {
					if d := findDoc(s); d != nil {
						fmt.Fprint(stdout, d.text())
						return exitOK
					}
				}
			}
			fmt.Fprint(stdout, overview)
		} else {
			fmt.Fprint(stdout, agentReference())
		}
		return exitOK
	case "version":
		fmt.Fprintln(stdout, version)
		return exitOK
	}

	d := findDoc(cmd)
	if d == nil {
		a.finish()
		return a.fail(exitUsage, "usage", fmt.Errorf("unknown command %q (try 'insta360 --help')", cmd))
	}
	fs := newFS(cmd, a)
	f := declareCmdFlags(fs, d.Name)
	if hasHelp(rest) {
		fmt.Fprint(stdout, d.text())
		return exitOK
	}
	pos, err := parseInterleaved(fs, rest)
	a.finish()
	if err != nil {
		return a.fail(exitUsage, "usage", fmt.Errorf("%s: %v (try 'insta360 %s --help')", cmd, err, cmd))
	}
	f.set = map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { f.set[fl.Name] = true })
	f.args = pos
	return a.dispatch(d.Name, f)
}

func hasHelp(args []string) bool {
	for _, s := range args {
		if s == "-h" || s == "--help" || s == "-help" {
			return true
		}
		if s == "--" {
			return false
		}
	}
	return false
}

// finish resolves the --json default after flag parsing.
func (a *app) finish() {
	if !a.g.jsonSet {
		a.g.json = !a.isTTY
	}
}

type cmdFlags struct {
	pan, tilt         float64
	zoom, focus, roll int
	delta             int
	all, zoomReset    bool
	autofocus         bool
	normal            bool
	onoff             map[string]*string // gestures: --enable --tracking --zoom --whiteboard
	set               map[string]bool
	args              []string
}

func declareCmdFlags(fs *flag.FlagSet, name string) *cmdFlags {
	f := &cmdFlags{}
	switch name {
	case "get":
		fs.BoolVar(&f.all, "all", false, "")
	case "set":
		fs.Float64Var(&f.pan, "pan", 0, "")
		fs.Float64Var(&f.tilt, "tilt", 0, "")
		fs.IntVar(&f.zoom, "zoom", 0, "")
		fs.IntVar(&f.focus, "focus", 0, "")
		fs.IntVar(&f.roll, "roll", 0, "")
		fs.BoolVar(&f.autofocus, "autofocus", false, "")
		fs.BoolVar(&f.normal, "normal", false, "")
		fs.BoolVar(&f.normal, "release", false, "")
	case "move":
		fs.Float64Var(&f.pan, "pan", 0, "")
		fs.Float64Var(&f.tilt, "tilt", 0, "")
		fs.IntVar(&f.zoom, "zoom", 0, "")
		fs.BoolVar(&f.normal, "normal", false, "")
		fs.BoolVar(&f.normal, "release", false, "")
	case "center":
		fs.BoolVar(&f.zoomReset, "zoom", false, "")
	case "zoom":
		fs.IntVar(&f.delta, "delta", 0, "")
	case "gestures":
		f.onoff = map[string]*string{}
		for _, n := range []string{"enable", "tracking", "zoom", "whiteboard"} {
			f.onoff[n] = fs.String(n, "", "")
		}
	}
	return f
}

// ---- output helpers ----

func (a *app) emit(v any, human string) {
	if a.g.json {
		enc := json.NewEncoder(a.out)
		enc.Encode(v)
		return
	}
	fmt.Fprintln(a.out, strings.TrimRight(human, "\n"))
}

func classify(err error) (int, string) {
	var re *camera.RangeError
	switch {
	case errors.As(err, &re), errors.Is(err, camera.ErrOutOfRange):
		return exitRange, "out_of_range"
	case errors.Is(err, camera.ErrInvalidValue):
		return exitRange, "invalid_value"
	case errors.Is(err, camera.ErrNoDevice):
		return exitNoDevice, "no_device"
	case errors.Is(err, camera.ErrPermission):
		return exitPermission, "permission"
	case errors.Is(err, camera.ErrTimeout):
		return exitTransfer, "timeout"
	case errors.Is(err, camera.ErrTransfer):
		return exitTransfer, "transfer"
	case errors.Is(err, camera.ErrBackendUnavailable):
		return exitUnavailable, "unavailable"
	case errors.Is(err, camera.ErrUnsupported):
		return exitUnavailable, "unsupported"
	}
	return exitInternal, "internal"
}

func (a *app) fail(code int, kind string, err error) int {
	if code == 0 {
		code, kind = classify(err)
	}
	if a.g.json {
		json.NewEncoder(a.err).Encode(map[string]any{"error": map[string]any{
			"code": kind, "message": err.Error(), "exit_code": code}})
	} else {
		fmt.Fprintf(a.err, "error: %s\n", err)
	}
	return code
}

func (a *app) failErr(err error) int { return a.fail(0, "", err) }

func (a *app) open() (*camera.Camera, error) { return camera.Open(a.g.options()) }

// ---- commands ----

func (a *app) dispatch(name string, f *cmdFlags) int {
	switch name {
	case "list":
		return a.cmdList()
	}
	cam, err := a.open()
	if err != nil {
		return a.failErr(err)
	}
	defer cam.Close()
	if a.g.keepStream {
		release, code := a.startStream(cam)
		if code != exitOK {
			return code
		}
		defer release()
	}
	return a.exec(cam, name, f)
}

func (a *app) exec(cam *camera.Camera, name string, f *cmdFlags) int {
	switch name {
	case "info":
		return a.cmdInfo(cam)
	case "get":
		return a.cmdGet(cam, f)
	case "set":
		return a.cmdSet(cam, f, false)
	case "move":
		return a.cmdSet(cam, f, true)
	case "center":
		return a.cmdCenter(cam, f)
	case "zoom":
		return a.cmdZoom(cam, f)
	case "mode":
		return a.cmdMode(cam, f)
	case "feature":
		return a.cmdFeature(cam, f)
	case "gestures":
		return a.cmdGestures(cam, f)
	case "tracking-speed":
		return a.cmdTrackingSpeed(cam, f)
	case "flicker":
		return a.cmdFlicker(cam, f)
	}
	return a.fail(exitInternal, "internal", fmt.Errorf("unhandled command %s", name))
}

func (a *app) cmdList() int {
	devs, err := camera.List(a.g.options())
	if err != nil && !errors.Is(err, camera.ErrNoDevice) {
		return a.failErr(err)
	}
	if devs == nil {
		devs = []camera.DeviceInfo{}
	}
	var sb strings.Builder
	if len(devs) == 0 {
		sb.WriteString("no devices found")
	}
	for _, d := range devs {
		fmt.Fprintf(&sb, "%s  %04x:%04x  selector=%s  serial=%s\n", d.Backend, d.VendorID, d.ProductID, d.Selector, d.Serial)
	}
	a.emit(map[string]any{"devices": devs}, sb.String())
	return exitOK
}

func (a *app) cmdInfo(cam *camera.Camera) int {
	in := cam.Info()
	human := fmt.Sprintf("%s (%s) backend=%s serial=%s\nid %04x:%04x bus %d addr %d\npan  %g..%g deg\ntilt %g..%g deg\nzoom %g..%g (100 = 1.0x)\nranges: %s",
		in.Name, in.Manufacturer, in.Backend, in.Serial, in.VendorID, in.ProductID, in.Bus, in.Address,
		in.PanRange.Min, in.PanRange.Max, in.TiltRange.Min, in.TiltRange.Max, in.ZoomRange.Min, in.ZoomRange.Max, in.RangesSource)
	if in.RangesWarning != "" {
		human += "\nwarning: " + in.RangesWarning
	}
	a.emit(in, human)
	return exitOK
}

func (a *app) cmdGet(cam *camera.Camera, f *cmdFlags) int {
	pt, err := cam.GetPanTilt()
	if err != nil {
		return a.failErr(err)
	}
	z, err := cam.GetZoom()
	if err != nil {
		return a.failErr(err)
	}
	out := map[string]any{"pan": pt.Pan, "tilt": pt.Tilt, "zoom": z, "zoom_factor": camera.ZoomFactor(z), "commanded_not_measured": true}
	human := fmt.Sprintf("pan=%.4f tilt=%.4f zoom=%d (%.2fx)  [commanded values, not measured]", pt.Pan, pt.Tilt, z, camera.ZoomFactor(z))
	if f.all {
		var warns []string
		if v, err := cam.GetFocus(); err != nil {
			warns = append(warns, "focus: "+err.Error())
		} else {
			out["focus"] = v
			human += fmt.Sprintf("\nfocus=%d", v)
		}
		if v, err := cam.GetAutoFocus(); err == nil {
			out["autofocus"] = v
			human += fmt.Sprintf("\nautofocus=%v", v)
		}
		if v, err := cam.GetRoll(); err != nil {
			warns = append(warns, "roll: "+err.Error())
		} else {
			out["roll"] = v
			human += fmt.Sprintf("\nroll=%d", v)
		}
		if v, err := cam.GetMode(); err == nil {
			out["mode"] = v
			human += fmt.Sprintf("\nmode=%s", v)
		} else if !errors.Is(err, camera.ErrUnsupported) {
			warns = append(warns, "mode: "+err.Error())
		}
		if fs, m, err := cam.GetFeatures(); err == nil {
			fm := map[string]bool{}
			for _, x := range fs {
				fm[string(x.Name)] = x.On
				human += fmt.Sprintf("\n%s=%s", x.Name, onOff(x.On))
			}
			out["features"], out["feature_mask"] = fm, fmt.Sprintf("0x%04x", m)
			human += fmt.Sprintf("\nfeature_mask=0x%04x", m)
		} else if !errors.Is(err, camera.ErrUnsupported) {
			warns = append(warns, "features: "+err.Error())
		}
		if g, err := cam.GetGestures(); err == nil {
			out["gestures"] = g
			human += fmt.Sprintf("\ngesture_mask=0x%02x (tracking=%s zoom=%s whiteboard=%s)", g.Mask, onOff(g.Tracking), onOff(g.Zoom), onOff(g.Whiteboard))
		} else if !errors.Is(err, camera.ErrUnsupported) {
			warns = append(warns, "gestures: "+err.Error())
		}
		if v, err := cam.GetTrackingSpeed(); err == nil {
			out["tracking_speed"] = v
			human += "\ntracking_speed=" + string(v)
		} else if !errors.Is(err, camera.ErrUnsupported) {
			warns = append(warns, "tracking-speed: "+err.Error())
		}
		if v, err := cam.GetPowerLine(); err == nil {
			out["flicker_hz"] = int(v)
			human += fmt.Sprintf("\nflicker_hz=%d", v)
		} else if !errors.Is(err, camera.ErrUnsupported) {
			warns = append(warns, "flicker: "+err.Error())
		}
		if len(warns) > 0 {
			out["warnings"] = warns
			human += "\nwarnings: " + strings.Join(warns, "; ")
		}
	}
	a.emit(out, human)
	return exitOK
}

func fmtPT(r camera.Result[camera.PanTilt]) string {
	return fmt.Sprintf("pan=%.4f tilt=%.4f%s", r.Applied.Pan, r.Applied.Tilt, flags(r.Clamped, r.DryRun))
}
func fmtInt(name string, r camera.Result[int]) string {
	return fmt.Sprintf("%s=%d%s", name, r.Applied, flags(r.Clamped, r.DryRun))
}
func flags(clamped, dry bool) string {
	s := ""
	if clamped {
		s += " (clamped)"
	}
	if dry {
		s += " (dry-run, nothing sent)"
	}
	return s
}

func (a *app) cmdSet(cam *camera.Camera, f *cmdFlags, relative bool) int {
	out := map[string]any{}
	var lines []string
	if len(f.args) > 0 {
		return a.fail(exitUsage, "usage", fmt.Errorf("unexpected argument %q", f.args[0]))
	}
	if !(f.set["pan"] || f.set["tilt"] || f.set["zoom"] || f.set["focus"] || f.set["roll"] || f.set["autofocus"]) {
		return a.fail(exitUsage, "usage", errors.New("nothing to do: pass at least one of --pan --tilt --zoom (--focus --roll --autofocus for set)"))
	}
	if f.set["pan"] || f.set["tilt"] || f.set["zoom"] {
		cur, err := cam.GetMode()
		switch {
		case err != nil: // unsupported or unreadable: carry on without mode handling
		case cur == camera.ModeNormal:
		case f.normal:
			r, err := cam.SetMode(camera.ModeNormal)
			if err != nil {
				return a.failErr(err)
			}
			out["mode"] = r
			lines = append(lines, "mode="+string(r.Applied)+" (was "+string(cur)+")"+flags(false, r.DryRun))
			if !r.DryRun {
				time.Sleep(modeSettle)
			}
		default:
			w := fmt.Sprintf("camera is in mode %q: it may move by itself or ignore pan/tilt/zoom; use --normal to switch to 'normal' first", cur)
			out["warnings"] = []string{w}
			fmt.Fprintln(a.err, "warning: "+w)
		}
	}
	if f.set["pan"] || f.set["tilt"] {
		cur, err := cam.GetPanTilt()
		if err != nil {
			return a.failErr(err)
		}
		pan, tilt := cur.Pan, cur.Tilt
		if relative {
			pan += f.pan * b2f(f.set["pan"])
			tilt += f.tilt * b2f(f.set["tilt"])
		} else {
			if f.set["pan"] {
				pan = f.pan
			}
			if f.set["tilt"] {
				tilt = f.tilt
			}
		}
		if math.IsNaN(pan) || math.IsNaN(tilt) {
			return a.fail(exitRange, "invalid_value", camera.ErrInvalidValue)
		}
		r, err := cam.SetPanTilt(pan, tilt)
		if err != nil {
			return a.failErr(err)
		}
		out["pan_tilt"] = r
		lines = append(lines, fmtPT(r))
	}
	if f.set["zoom"] {
		var r camera.Result[int]
		var err error
		if relative {
			r, err = cam.MoveZoom(f.zoom)
		} else {
			r, err = cam.SetZoom(f.zoom)
		}
		if err != nil {
			return a.failErr(err)
		}
		out["zoom"] = r
		lines = append(lines, fmtInt("zoom", r))
	}
	if f.set["autofocus"] {
		r, err := cam.SetAutoFocus(f.autofocus)
		if err != nil {
			return a.failErr(err)
		}
		out["autofocus"] = r
		lines = append(lines, fmt.Sprintf("autofocus=%v%s", r.Applied, flags(false, r.DryRun)))
	}
	if f.set["focus"] {
		r, err := cam.SetFocus(f.focus)
		if err != nil {
			return a.failErr(err)
		}
		out["focus"] = r
		lines = append(lines, fmtInt("focus", r))
	}
	if f.set["roll"] {
		r, err := cam.SetRoll(f.roll)
		if err != nil {
			return a.failErr(err)
		}
		out["roll"] = r
		lines = append(lines, fmtInt("roll", r))
	}
	a.emit(out, strings.Join(lines, "\n"))
	return exitOK
}

// modeSettle is how long to wait after leaving a mode before moving the gimbal.
const modeSettle = 1500 * time.Millisecond

func (a *app) cmdMode(cam *camera.Camera, f *cmdFlags) int {
	switch {
	case len(f.args) > 1:
		return a.fail(exitUsage, "usage", errors.New("mode takes at most one name"))
	case len(f.args) == 1:
		m, err := camera.ParseMode(f.args[0])
		if err != nil {
			return a.fail(exitUsage, "usage", err)
		}
		r, err := cam.SetMode(m)
		if err != nil {
			return a.failErr(err)
		}
		a.emit(map[string]any{"mode": r}, "mode="+string(r.Applied)+flags(false, r.DryRun))
		return exitOK
	}
	m, err := cam.GetMode()
	if err != nil {
		return a.failErr(err)
	}
	a.emit(map[string]any{"mode": m}, "mode="+string(m))
	return exitOK
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func (a *app) cmdCenter(cam *camera.Camera, f *cmdFlags) int {
	out := map[string]any{}
	r, err := cam.Center()
	if err != nil {
		return a.failErr(err)
	}
	out["pan_tilt"] = r
	lines := []string{fmtPT(r)}
	if f.zoomReset {
		z, err := cam.SetZoom(100)
		if err != nil {
			return a.failErr(err)
		}
		out["zoom"] = z
		lines = append(lines, fmtInt("zoom", z))
	}
	a.emit(out, strings.Join(lines, "\n"))
	return exitOK
}

func (a *app) cmdZoom(cam *camera.Camera, f *cmdFlags) int {
	var r camera.Result[int]
	var err error
	switch {
	case len(f.args) > 1:
		return a.fail(exitUsage, "usage", errors.New("zoom takes at most one value"))
	case len(f.args) == 1:
		var v int
		if _, e := fmt.Sscanf(f.args[0], "%d", &v); e != nil || fmt.Sprint(v) != f.args[0] {
			return a.fail(exitUsage, "usage", fmt.Errorf("zoom value %q is not an integer", f.args[0]))
		}
		r, err = cam.SetZoom(v)
	case f.set["delta"]:
		r, err = cam.MoveZoom(f.delta)
	default:
		z, err := cam.GetZoom()
		if err != nil {
			return a.failErr(err)
		}
		a.emit(map[string]any{"zoom": z, "zoom_factor": camera.ZoomFactor(z)}, fmt.Sprintf("zoom=%d (%.2fx)", z, camera.ZoomFactor(z)))
		return exitOK
	}
	if err != nil {
		return a.failErr(err)
	}
	a.emit(map[string]any{"zoom": r}, fmtInt("zoom", r))
	return exitOK
}

// ---- features, gestures, tracking speed, anti-flicker, stream ----

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	}
	return false, fmt.Errorf("%q is not on|off", s)
}

func (a *app) cmdFeature(cam *camera.Camera, f *cmdFlags) int {
	switch {
	case len(f.args) > 2:
		return a.fail(exitUsage, "usage", errors.New("feature takes NAME [on|off]"))
	case len(f.args) == 0:
		fs, m, err := cam.GetFeatures()
		if err != nil {
			return a.failErr(err)
		}
		var sb strings.Builder
		for _, x := range fs {
			fmt.Fprintf(&sb, "%-18s %s\n", x.Name, onOff(x.On))
		}
		fmt.Fprintf(&sb, "mask=0x%04x", m)
		a.emit(map[string]any{"features": fs, "mask": m}, sb.String())
		return exitOK
	}
	fi, err := camera.ParseFeature(f.args[0])
	if err != nil {
		return a.fail(exitUsage, "usage", err)
	}
	if len(f.args) == 1 {
		fs, _, err := cam.GetFeatures()
		if err != nil {
			return a.failErr(err)
		}
		for _, x := range fs {
			if x.Name == fi.Name {
				a.emit(map[string]any{"feature": x}, fmt.Sprintf("%s=%s", x.Name, onOff(x.On)))
			}
		}
		return exitOK
	}
	on, err := parseOnOff(f.args[1])
	if err != nil {
		return a.fail(exitUsage, "usage", err)
	}
	ch, err := cam.SetFeature(fi.Name, on)
	if err != nil {
		return a.failErr(err)
	}
	a.emit(map[string]any{"feature": ch}, fmt.Sprintf("%s=%s mask 0x%04x -> 0x%04x%s", ch.Feature, onOff(ch.On), ch.MaskBefore, ch.MaskAfter, flags(false, ch.DryRun)))
	return exitOK
}

func fmtGestures(g camera.Gestures) string {
	return fmt.Sprintf("enabled=%s tracking=%s zoom=%s whiteboard=%s (mask 0x%02x)", onOff(g.Enabled), onOff(g.Tracking), onOff(g.Zoom), onOff(g.Whiteboard), g.Mask)
}

func (a *app) cmdGestures(cam *camera.Camera, f *cmdFlags) int {
	if len(f.args) > 0 {
		return a.fail(exitUsage, "usage", fmt.Errorf("unexpected argument %q", f.args[0]))
	}
	var u camera.GestureUpdate
	given := false
	for _, x := range []struct {
		n string
		p **bool
	}{{"enable", &u.Enabled}, {"tracking", &u.Tracking}, {"zoom", &u.Zoom}, {"whiteboard", &u.Whiteboard}} {
		if !f.set[x.n] {
			continue
		}
		v, err := parseOnOff(*f.onoff[x.n])
		if err != nil {
			return a.fail(exitUsage, "usage", fmt.Errorf("--%s: %v", x.n, err))
		}
		*x.p, given = &v, true
	}
	if !given {
		g, err := cam.GetGestures()
		if err != nil {
			return a.failErr(err)
		}
		a.emit(map[string]any{"gestures": g}, fmtGestures(g))
		return exitOK
	}
	ch, err := cam.SetGestures(u)
	if err != nil {
		return a.failErr(err)
	}
	a.emit(map[string]any{"gestures": ch}, fmtGestures(ch.After)+flags(false, ch.DryRun))
	return exitOK
}

func (a *app) cmdTrackingSpeed(cam *camera.Camera, f *cmdFlags) int {
	switch {
	case len(f.args) > 1:
		return a.fail(exitUsage, "usage", errors.New("tracking-speed takes at most one value"))
	case len(f.args) == 1:
		s, err := camera.ParseTrackingSpeed(f.args[0])
		if err != nil {
			return a.fail(exitUsage, "usage", err)
		}
		r, err := cam.SetTrackingSpeed(s)
		if err != nil {
			return a.failErr(err)
		}
		a.emit(map[string]any{"tracking_speed": r}, "tracking_speed="+string(r.Applied)+flags(false, r.DryRun))
		return exitOK
	}
	s, err := cam.GetTrackingSpeed()
	if err != nil {
		return a.failErr(err)
	}
	a.emit(map[string]any{"tracking_speed": s}, "tracking_speed="+string(s))
	return exitOK
}

func (a *app) cmdFlicker(cam *camera.Camera, f *cmdFlags) int {
	switch {
	case len(f.args) > 1:
		return a.fail(exitUsage, "usage", errors.New("flicker takes at most one value (50|60)"))
	case len(f.args) == 1:
		hz, err := camera.ParsePowerLine(f.args[0])
		if err != nil {
			return a.fail(exitUsage, "usage", err)
		}
		r, err := cam.SetPowerLine(hz)
		if err != nil {
			return a.failErr(err)
		}
		a.emit(map[string]any{"flicker_hz": r}, fmt.Sprintf("flicker=%d Hz%s", r.Applied, flags(false, r.DryRun)))
		return exitOK
	}
	hz, err := cam.GetPowerLine()
	if err != nil {
		return a.failErr(err)
	}
	a.emit(map[string]any{"flicker_hz": int(hz)}, fmt.Sprintf("flicker=%d Hz", hz))
	return exitOK
}

// startStream implements --keep-stream. The release function waits
// --stream-hold first so the gimbal has time to move before the stream closes
// (the camera drops to privacy when the last stream closes).
func (a *app) startStream(cam *camera.Camera) (func(), int) {
	o := camera.StreamOptions{FPS: a.g.fps}
	if a.g.resolution != "" {
		if _, err := fmt.Sscanf(a.g.resolution, "%dx%d", &o.Width, &o.Height); err != nil || o.Width <= 0 || o.Height <= 0 {
			return nil, a.fail(exitUsage, "usage", fmt.Errorf("--resolution %q is not WxH", a.g.resolution))
		}
	}
	w, h, fps, rel, err := cam.KeepStream(o)
	if err != nil {
		return nil, a.failErr(err)
	}
	fmt.Fprintf(a.err, "stream: %dx%d @%d fps open (unverified on real hardware), held %s after the command\n", w, h, fps, a.g.hold)
	return func() {
		if !a.g.dryRun {
			time.Sleep(a.g.hold)
		}
		rel()
	}, exitOK
}
