package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/CesarGallego/insta360tux/camera"
	"github.com/CesarGallego/insta360tux/camera/fake"
	"strings"
	"testing"
)

func do(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	code := run(append([]string{"--backend", "fake"}, args...), &o, &e, false)
	return code, o.String(), e.String()
}

func TestExitCodes(t *testing.T) {
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"get"}, 0},
		{[]string{"set", "--pan", "10"}, 0},
		{[]string{"set", "--pan", "999"}, exitRange},
		{[]string{"set"}, exitUsage},
		{[]string{"bogus"}, exitUsage},
		{[]string{"set", "--nope"}, exitUsage},
		{[]string{"zoom", "abc"}, exitUsage},
		{[]string{"zoom", "401"}, exitRange},
		{[]string{"--clamp", "zoom", "401"}, 0},
		{[]string{"home"}, 0},
		{[]string{"help-agent"}, 0},
		{[]string{"get", "--help"}, 0},
	}
	for _, c := range cases {
		if code, _, _ := do(t, c.args...); code != c.want {
			t.Errorf("%v: exit %d, want %d", c.args, code, c.want)
		}
	}
	var o, e bytes.Buffer
	if code := run([]string{"--backend", "nope", "get"}, &o, &e, false); code != exitUnavailable {
		t.Errorf("bad backend: %d", code)
	}
}

func TestJSONOutputAndErrors(t *testing.T) {
	_, out, _ := do(t, "get")
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil || m["zoom"].(float64) != 100 {
		t.Fatalf("out=%q err=%v", out, err)
	}
	_, _, errs := do(t, "set", "--tilt", "500")
	var e struct {
		Error struct {
			Code     string
			ExitCode int `json:"exit_code"`
		}
	}
	if err := json.Unmarshal([]byte(errs), &e); err != nil || e.Error.Code != "out_of_range" || e.Error.ExitCode != 4 {
		t.Fatalf("stderr=%q err=%v", errs, err)
	}
}

func TestHumanOutputOnTTY(t *testing.T) {
	var o, e bytes.Buffer
	run([]string{"--backend", "fake", "get"}, &o, &e, true)
	if !strings.HasPrefix(o.String(), "pan=") {
		t.Errorf("tty output: %q", o.String())
	}
}

func TestHelpMentionsCaveats(t *testing.T) {
	_, out, _ := do(t, "help-agent")
	for _, s := range []string{"video stream", "COMMANDED", "Exit codes", "### `set`", "arc-seconds"} {
		if !strings.Contains(out, s) {
			t.Errorf("help-agent missing %q", s)
		}
	}
}

func TestFlagsAnyPosition(t *testing.T) {
	cases := [][]string{
		{"zoom", "200", "--json"},
		{"zoom", "--json", "200"},
		{"--json", "zoom", "200"},
		{"set", "--pan", "10", "--json"},
		{"set", "--clamp", "--pan", "500"},
		{"set", "--pan", "500", "--clamp"},
	}
	for _, c := range cases {
		code, out, e := do(t, c...)
		if code != 0 {
			t.Errorf("%v: exit %d stderr=%q", c, code, e)
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Errorf("%v: not json: %q", c, out)
		}
	}
	// value flag after the command
	if code, _, e := run2(t, "get", "--backend", "fake", "--dry-run"); code != 0 {
		t.Errorf("backend after cmd: %d %q", code, e)
	}
	// --json=false after positional gives text
	if _, out, _ := do(t, "zoom", "150", "--json=false"); !strings.HasPrefix(out, "zoom=150") {
		t.Errorf("text out: %q", out)
	}
	// dry-run after command applies
	if _, out, _ := do(t, "set", "--pan", "10", "--dry-run"); !strings.Contains(out, `"dry_run":true`) {
		t.Errorf("dry-run not applied: %q", out)
	}
	// device value is not mistaken for the command (fake ignores the selector)
	if code, _, _ := do(t, "--device", "9.9", "get"); code != 0 {
		t.Errorf("--device 9.9 get: %d", code)
	}
	// "--" makes the rest positional
	if code, _, _ := do(t, "zoom", "--", "abc"); code != exitUsage {
		t.Errorf("zoom -- abc: %d", code)
	}
	if code, _, _ := do(t, "--clamp", "zoom", "--", "-5"); code != 0 {
		t.Errorf("zoom -- -5 with clamp: %d", code)
	}
}

func run2(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	code := run(args, &o, &e, false)
	return code, o.String(), e.String()
}

func TestHelpCoversCommandsAndExitCodes(t *testing.T) {
	_, top, _ := do(t, "--help")
	_, agent, _ := do(t, "help-agent")
	for _, s := range []string{"video stream", "COMMANDED", "TOPICS", "help caveats"} {
		if !strings.Contains(top, s) {
			t.Errorf("--help missing %q", s)
		}
	}
	for _, tp := range topics {
		_, h, _ := do(t, "help", tp.Name)
		if h != tp.Text {
			t.Errorf("help %s mismatch: %q", tp.Name, h)
		}
		if !strings.Contains(agent, tp.Text) {
			t.Errorf("help-agent missing topic %s", tp.Name)
		}
	}
	for _, s := range []string{"GET_MIN", "any order", `"error"`} {
		if !strings.Contains(agent, s) {
			t.Errorf("help-agent missing %q", s)
		}
	}
	if c, _, e := do(t, "help", "nope"); c != 2 || !strings.Contains(e, "unknown help topic") {
		t.Errorf("help nope: %d %q", c, e)
	}
	for _, d := range docs {
		if !strings.Contains(top, "  "+d.Name) {
			t.Errorf("overview missing command %s", d.Name)
		}
		_, h, _ := do(t, d.Name, "--help")
		if h == "" || cmdExits[d.Name] == "" {
			t.Errorf("%s: empty help or exits", d.Name)
		}
		for _, s := range []string{"USAGE", "EXAMPLES", "insta360 " + d.Name, "OUTPUT", "EXIT CODES", "ERRORS", "exit_code"} {
			if !strings.Contains(h, s) {
				t.Errorf("%s --help missing %q", d.Name, s)
			}
		}
		if !strings.Contains(agent, "### `"+d.Name+"`") {
			t.Errorf("help-agent missing %s", d.Name)
		}
	}
	for code := 0; code <= 7; code++ {
		if !strings.Contains(topicExitCodes, fmt.Sprintf("| %d |", code)) || !strings.Contains(agent, fmt.Sprintf("| %d |", code)) {
			t.Errorf("exit code %d not documented", code)
		}
	}
	for _, c := range []string{"usage", "no_device", "out_of_range", "invalid_value", "transfer", "timeout", "unavailable", "unsupported", "permission", "internal"} {
		if !strings.Contains(agent, "`"+c+"`") {
			t.Errorf("error code %s not documented", c)
		}
	}
}

func TestModeCommand(t *testing.T) {
	code, out, _ := do(t, "mode")
	if code != 0 || !strings.Contains(out, `"mode":"normal"`) {
		t.Fatalf("get: %d %q", code, out)
	}
	if code, out, _ := do(t, "mode", "overhead"); code != 0 || !strings.Contains(out, `"applied":"overhead"`) {
		t.Fatalf("set: %d %q", code, out)
	}
	if code, _, _ := do(t, "mode", "disco"); code != exitUsage {
		t.Errorf("unknown mode: %d", code)
	}
	if code, _, _ := do(t, "mode", "a", "b"); code != exitUsage {
		t.Errorf("two args: %d", code)
	}
	if code, out, _ := do(t, "--dry-run", "mode", "tracking"); code != 0 || !strings.Contains(out, `"dry_run":true`) {
		t.Errorf("dry-run: %d %q", code, out)
	}
	if code, out, _ := do(t, "mode", "--help"); code != 0 || !strings.Contains(out, "QR") {
		t.Errorf("help: %d", code)
	}
}

func TestSetWarnsOrReleasesMode(t *testing.T) {
	shared := fake.New()
	shared.Mode = camera.ModeTracking
	camera.Register(camera.Factory{Name: "fakemode", Explicit: true, New: func() camera.Backend { return shared }})
	runIt := func(args ...string) (int, string, string) {
		var o, e bytes.Buffer
		code := run(append([]string{"--backend", "fakemode"}, args...), &o, &e, false)
		return code, o.String(), e.String()
	}
	code, out, errs := runIt("set", "--pan", "10")
	if code != 0 || !strings.Contains(errs, "warning") || !strings.Contains(out, `"warnings"`) || shared.Mode != camera.ModeTracking {
		t.Errorf("warn: %d out=%q err=%q mode=%s", code, out, errs, shared.Mode)
	}
	code, out, _ = runIt("set", "--pan", "10", "--normal")
	if code != 0 || shared.Mode != camera.ModeNormal || shared.Pan != 10 || !strings.Contains(out, `"mode"`) {
		t.Errorf("release: %d out=%q mode=%s pan=%v", code, out, shared.Mode, shared.Pan)
	}
	shared.Mode = camera.ModeDesk
	if _, out, _ := runIt("get", "--all"); !strings.Contains(out, `"mode":"desk"`) {
		t.Errorf("get --all lacks mode: %q", out)
	}
}

func TestFeatureGesturesSpeedFlicker(t *testing.T) {
	shared := fake.New()
	camera.Register(camera.Factory{Name: "fakefeat", Explicit: true, New: func() camera.Backend { return shared }})
	runIt := func(args ...string) (int, string, string) {
		var o, e bytes.Buffer
		code := run(append([]string{"--backend", "fakefeat"}, args...), &o, &e, false)
		return code, o.String(), e.String()
	}
	if code, out, _ := runIt("feature"); code != 0 || !strings.Contains(out, `"name":"smart-framing","on":true`) || !strings.Contains(out, `"mask":544`) {
		t.Errorf("list: %d %q", code, out)
	}
	if code, out, _ := runIt("feature", "hdr", "on"); code != 0 || shared.FeatureMask != 0x0224 || !strings.Contains(out, `"mask_after":548`) {
		t.Errorf("hdr on: %d %q %#x", code, out, shared.FeatureMask)
	}
	if code, _, _ := runIt("feature", "hdr", "maybe"); code != exitUsage {
		t.Errorf("maybe: %d", code)
	}
	if code, _, _ := runIt("feature", "bogus", "on"); code != exitUsage {
		t.Errorf("bogus: %d", code)
	}
	if code, _, _ := runIt("--dry-run", "feature", "vertical-lock", "on"); code != 0 || shared.FeatureMask != 0x0224 {
		t.Errorf("dry-run wrote: %#x", shared.FeatureMask)
	}
	if code, _, _ := runIt("gestures", "--enable", "on", "--tracking", "on", "--zoom", "on"); code != 0 || shared.GestureMask != 0x06 || shared.FeatureMask != 0x0234 {
		t.Errorf("gestures: %#x %#x", shared.GestureMask, shared.FeatureMask)
	}
	if code, out, _ := runIt("gestures"); code != 0 || !strings.Contains(out, `"zoom":true`) {
		t.Errorf("gestures read: %d %q", code, out)
	}
	if code, _, _ := runIt("gestures", "--zoom", "x"); code != exitUsage {
		t.Errorf("gestures bad: %d", code)
	}
	if code, _, _ := runIt("tracking-speed", "slow"); code != 0 || shared.Speed != camera.SpeedSlow {
		t.Errorf("speed: %v", shared.Speed)
	}
	if code, _, _ := runIt("tracking-speed", "warp"); code != exitUsage {
		t.Errorf("warp: %d", code)
	}
	if code, _, _ := runIt("flicker", "60"); code != 0 || shared.PowerLine != 60 {
		t.Errorf("flicker: %v", shared.PowerLine)
	}
	if code, _, _ := runIt("flicker", "55"); code != exitUsage {
		t.Errorf("flicker 55: %d", code)
	}
	code, out, _ := runIt("get", "--all")
	for _, s := range []string{`"features"`, `"feature_mask":"0x0234"`, `"gestures"`, `"tracking_speed":"slow"`, `"flicker_hz":60`} {
		if code != 0 || !strings.Contains(out, s) {
			t.Errorf("get --all lacks %s: %q", s, out)
		}
	}
	if code, _, e := runIt("--keep-stream", "get"); code != exitUnavailable {
		t.Errorf("keep-stream on fake: %d %q", code, e)
	}
	if code, _, _ := runIt("--keep-stream", "--resolution", "bad", "get"); code != exitUsage {
		t.Errorf("bad resolution: %d", code)
	}
}
