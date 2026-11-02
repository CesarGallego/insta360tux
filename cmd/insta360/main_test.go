package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	for _, s := range []string{"video stream", "COMMANDED", "DEFAULTS", "any order", `"error"`} {
		if !strings.Contains(top, s) {
			t.Errorf("--help missing %q", s)
		}
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
		if !strings.Contains(top, fmt.Sprintf("| %d |", code)) || !strings.Contains(agent, fmt.Sprintf("| %d |", code)) {
			t.Errorf("exit code %d not documented", code)
		}
	}
	for _, c := range []string{"usage", "no_device", "out_of_range", "invalid_value", "transfer", "timeout", "unavailable", "unsupported", "permission", "internal"} {
		if !strings.Contains(agent, "`"+c+"`") {
			t.Errorf("error code %s not documented", c)
		}
	}
}
