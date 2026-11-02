package main

import (
	"bytes"
	"encoding/json"
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
