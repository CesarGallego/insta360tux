package main

import (
	"fmt"
	"strings"
)

// Exit codes. Documented in --help and help-agent.
const (
	exitOK          = 0
	exitInternal    = 1 // unexpected error
	exitUsage       = 2 // bad flags/arguments
	exitNoDevice    = 3 // no camera found
	exitRange       = 4 // value out of range / not a number
	exitTransfer    = 5 // USB transfer failed or timed out
	exitUnavailable = 6 // backend not available / feature unsupported
	exitPermission  = 7 // permission denied or device busy
)

const exitCodesTable = `| Code | Meaning |
|---|---|
| 0 | success (also: ` + "`list`" + ` found no devices) |
| 1 | unexpected internal error |
| 2 | usage error (unknown command/flag, missing or malformed argument) |
| 3 | no matching camera found (not plugged in, or --device matches nothing) |
| 4 | value out of range or not a finite number (use --clamp to clamp instead) |
| 5 | USB transfer failed or timed out |
| 6 | backend not available / feature not supported by this backend or camera |
| 7 | permission denied or device busy (another process owns it) |`

const overview = `insta360 - control the pan/tilt/zoom gimbal of an Insta360 Link camera (USB 2e1a:4c01).

USAGE
  insta360 [global flags] <command> [command flags]

COMMANDS
  list        list attached cameras
  info        show device identity, backend and valid ranges
  get         read the current pan/tilt/zoom
  set         move to absolute pan/tilt/zoom
  move        move relative to the current position
  center      return to pan=0 tilt=0 (alias: home)
  zoom        read or set zoom only
  help-agent  print the complete reference as one Markdown document
  version     print version

GLOBAL FLAGS (accepted before or after the command)
  --json[=bool]      JSON output on stdout. Default: true when stdout is not a terminal.
                     Use --json=false to force human-readable text.
  --device STR       select camera: "VID:PID" hex (2e1a:4c01), "BUS.ADDR" decimal
                     (see 'list'), or a serial number. Default: first Insta360 Link.
  --backend NAME     libusb | v4l2 | fake. Default: auto ($INSTA360_BACKEND overrides auto).
  --dry-run          validate and report what would be sent, but write nothing to the camera.
  --timeout DUR      per-transfer timeout, Go duration (default 2s).
  --clamp            clamp out-of-range values instead of failing (exit 4).

UNITS
  pan, tilt  degrees (float). Wire format is arc-seconds (3600 = 1 deg); resolution 1/3600 deg.
  zoom       raw integer 100..400 where 100 = 1.0x and 400 = 4.0x.
  focus      raw integer 0..100.   roll  raw integer -100..100.
  Pan range -145..+145, tilt range -90..+100 (check 'info' for the actual device values).

IMPORTANT CAVEATS (read before automating)
  1. The gimbal has only been observed to MOVE while a video stream is active (another app,
     e.g. a camera viewer or video call, is using the camera). Without a stream the commands
     succeed but the gimbal may stay still. This is a hypothesis, not proven.
  2. 'get' returns the LAST COMMANDED value held by the camera, NOT the measured position.
     A successful 'set' followed by 'get' proves the command was accepted, not that the
     gimbal arrived. Do not use 'get' as feedback that movement completed.
  3. Relative moves ('move') are computed client-side from the commanded value in (2).
  4. Ranges (pan -145..145, tilt -90..100, zoom 100..400, ...) are built-in DEFAULTS for the
     Insta360 Link, not queried from the camera. 'info' prints these defaults.
  5. Native UVC PANTILT_RELATIVE is not supported by the camera; focus/roll are listed in
     the descriptor but unverified.

EXIT CODES
` + exitCodesTable + `

OUTPUT
  Data goes to stdout (JSON object when --json). Errors go to stderr; with --json they are
  a JSON object on one line, for example (exit code 4):
    {"error":{"code":"out_of_range","message":"pan 500 out of range [-145, 145]","exit_code":4}}
  "code" is one of: usage, no_device, out_of_range, invalid_value, transfer, timeout,
  unavailable, unsupported, permission, internal. Always branch on the process exit code
  or on "code", never on "message".
  Flags and positionals may appear in any order: 'insta360 zoom 200 --json' is valid.
  A '--' argument ends flag parsing; everything after it is positional.

Run 'insta360 <command> --help' for details, or 'insta360 help-agent' for the full reference.
`

// cmdDoc is the per-command reference, used by '<cmd> --help' and help-agent.
type cmdDoc struct {
	Name, Summary, Usage, Flags, Output, Examples string
}

var docs = []cmdDoc{
	{
		Name:    "list",
		Summary: "List attached cameras. Does not open them for control. Exit 0 even when none are found.",
		Usage:   "insta360 list",
		Flags:   "(only global flags)",
		Output: `JSON: {"devices":[{"backend":"libusb","vendor_id":11802,"product_id":19457,"bus":20,"address":4,"serial":"...","selector":"20.4"}]}
  "selector" can be passed to --device. "devices" is [] when nothing is attached.`,
		Examples: "insta360 list\ninsta360 list --json=false",
	},
	{
		Name:    "info",
		Summary: "Show the selected camera, the backend in use and the valid ranges for every control.",
		Usage:   "insta360 info",
		Flags:   "(only global flags)",
		Output: `JSON: {"backend":"libusb","name":"Insta360 Link","manufacturer":"Insta360","serial":"...",
  "vendor_id":11802,"product_id":19457,"bus":20,"address":4,
  "pan_range_deg":{"min":-145,"max":145},"tilt_range_deg":{"min":-90,"max":100},
  "zoom_range":{"min":100,"max":400},"focus_range":{"min":0,"max":100},"roll_range":{"min":-100,"max":100}}
  focus_range / roll_range are absent when unsupported.`,
		Examples: "insta360 info\ninsta360 --backend libusb --device 2e1a:4c01 info",
	},
	{
		Name:    "get",
		Summary: "Read pan/tilt/zoom. Values are what was last COMMANDED, not the measured position.",
		Usage:   "insta360 get [--all]",
		Flags:   "--all   also read focus and roll (best effort; failures are reported in \"warnings\", not as errors)",
		Output: `JSON: {"pan":10,"tilt":-5,"zoom":150,"zoom_factor":1.5,"commanded_not_measured":true}
  pan/tilt in degrees; zoom raw 100..400. With --all adds "focus" and "roll" (integers) and
  possibly "warnings":["focus: ..."].`,
		Examples: "insta360 get\ninsta360 get --all --json=false",
	},
	{
		Name:    "set",
		Summary: "Move to absolute pan/tilt/zoom. Only the flags you pass are changed; omitted axes keep their current commanded value (passing only --pan keeps tilt).",
		Usage:   "insta360 set [--pan DEG] [--tilt DEG] [--zoom RAW] [--focus RAW] [--roll RAW]",
		Flags: `--pan DEG    absolute pan in degrees, -145..145 (positive/negative direction per camera convention)
--tilt DEG   absolute tilt in degrees, -90..100
--zoom RAW   absolute zoom, integer 100..400 (100 = 1.0x)
--focus RAW  absolute focus, integer 0..100 (unverified on hardware)
--roll RAW   absolute roll, integer -100..100 (unverified on hardware)
At least one of these is required (else exit 2). Values outside the range exit 4 unless --clamp.`,
		Output: `JSON: {"pan_tilt":{"requested":{"pan":20,"tilt":0},"applied":{"pan":20,"tilt":0},"clamped":false,"dry_run":false},
  "zoom":{"requested":150,"applied":150,"clamped":false,"dry_run":false}}
  Only the sections for axes you set are present ("pan_tilt", "zoom", "focus", "roll").
  "applied" is what was (or with --dry-run would be) sent. Nothing is read back after the write.`,
		Examples: "insta360 set --pan 30 --tilt -10\ninsta360 set --zoom 200\ninsta360 set --pan 500 --clamp      # clamped to 145\ninsta360 --dry-run set --pan 10    # validate only",
	},
	{
		Name:    "move",
		Summary: "Move relative to the current commanded position (client-side: reads, adds, writes). Flags are deltas.",
		Usage:   "insta360 move [--pan DEG] [--tilt DEG] [--zoom RAW]",
		Flags: `--pan DEG    delta pan in degrees (may be negative: --pan -5)
--tilt DEG   delta tilt in degrees
--zoom RAW   delta zoom in raw units (integer)
The resulting absolute value must stay in range (exit 4, or clamp with --clamp).`,
		Output:   `Same schema as 'set' (requested/applied are the resulting ABSOLUTE values).`,
		Examples: "insta360 move --pan 5\ninsta360 move --pan -10 --tilt 3 --clamp\ninsta360 move --zoom 25",
	},
	{
		Name:     "center",
		Summary:  "Return to pan=0, tilt=0 (alias: home). With --zoom also resets zoom to 100 (1.0x).",
		Usage:    "insta360 center [--zoom]",
		Flags:    "--zoom   also reset zoom to 100",
		Output:   `Same schema as 'set'.`,
		Examples: "insta360 center\ninsta360 home --zoom",
	},
	{
		Name:    "zoom",
		Summary: "Read or set zoom only. Without arguments it prints the current zoom.",
		Usage:   "insta360 zoom [RAW] | insta360 zoom --delta N",
		Flags:   "RAW        absolute zoom 100..400\n--delta N  relative change in raw units (e.g. --delta -50)",
		Output: `Read: {"zoom":150,"zoom_factor":1.5}
  Write: {"zoom":{"requested":200,"applied":200,"clamped":false,"dry_run":false}}`,
		Examples: "insta360 zoom\ninsta360 zoom 200\ninsta360 zoom --delta -50",
	},
}

var cmdExits = map[string]string{
	"list":   "0 ok (including no devices); 2 usage; 6 backend unavailable",
	"info":   "0 ok; 2 usage; 3 no camera; 5 transfer/timeout; 6 backend unavailable; 7 permission/busy",
	"get":    "0 ok; 2 usage; 3 no camera; 5 transfer/timeout; 6 backend unavailable; 7 permission/busy",
	"set":    "0 ok; 2 usage (no axis flag, bad number); 3 no camera; 4 out of range or NaN (unless --clamp); 5 transfer/timeout; 6 unavailable/unsupported; 7 permission/busy",
	"move":   "0 ok; 2 usage; 3 no camera; 4 resulting value out of range (unless --clamp); 5 transfer/timeout; 6 unavailable; 7 permission/busy",
	"center": "0 ok; 2 usage; 3 no camera; 5 transfer/timeout; 6 unavailable; 7 permission/busy",
	"zoom":   "0 ok; 2 usage (not an integer, extra args); 3 no camera; 4 out of range (unless --clamp); 5 transfer/timeout; 6 unavailable; 7 permission/busy",
}

func findDoc(name string) *cmdDoc {
	if name == "home" {
		name = "center"
	}
	for i := range docs {
		if docs[i].Name == name {
			return &docs[i]
		}
	}
	return nil
}

func (d cmdDoc) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nUSAGE\n  %s\n\nFLAGS\n%s\n\nOUTPUT\n  %s\n\nEXAMPLES\n%s\n\nEXIT CODES\n  %s\n  (full table: 'insta360 --help')\n\nERRORS\n  stderr, with --json: {\"error\":{\"code\":\"out_of_range\",\"message\":\"...\",\"exit_code\":4}}\n\nGlobal flags (valid before or after the command): --json --device --backend --dry-run --timeout --clamp.\nUnits and caveats (movement needs an active video stream; get returns the commanded value): 'insta360 --help'.\n",
		d.Summary, d.Usage, indent(d.Flags), indent(d.Output), indent(d.Examples), cmdExits[d.Name])
	return b.String()
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n  ")
}

// agentReference renders everything as one Markdown document.
func agentReference() string {
	var b strings.Builder
	b.WriteString("# insta360 CLI reference\n\n")
	b.WriteString("```text\n" + overview + "```\n\n")
	b.WriteString("## Typical agent workflow\n\n")
	b.WriteString("1. `insta360 list` - confirm the camera is attached (exit 3 from other commands means it is not).\n")
	b.WriteString("2. Make sure a video stream is active in another app, otherwise the gimbal may not move.\n")
	b.WriteString("3. `insta360 info` - read the valid ranges.\n")
	b.WriteString("4. `insta360 --dry-run set --pan 20` - validate; then `insta360 set --pan 20`.\n")
	b.WriteString("5. Do not poll `get` expecting movement feedback; it echoes the commanded value.\n\n")
	b.WriteString("## Exit codes\n\n" + exitCodesTable + "\n\n")
	b.WriteString("## Errors\n\nErrors are written to stderr. With `--json` they are `{\"error\":{\"code\":\"...\",\"message\":\"...\",\"exit_code\":N}}`. ")
	b.WriteString("Codes: `usage`, `no_device`, `out_of_range`, `invalid_value`, `transfer`, `timeout`, `unavailable`, `unsupported`, `permission`, `internal`.\n\n")
	b.WriteString("## Commands\n\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "### `%s`\n\n%s\n\n**Usage:** `%s`\n\n**Flags:**\n\n```text\n%s\n```\n\n**Output:**\n\n```text\n%s\n```\n\n**Examples:**\n\n```sh\n%s\n```\n\n**Exit codes:** %s\n\n",
			d.Name, d.Summary, d.Usage, d.Flags, d.Output, d.Examples, cmdExits[d.Name])
	}
	b.WriteString("### `help-agent`\n\nPrints this document (exit 0).\n\n### `version`\n\nPrints the version string (exit 0).\n")
	return b.String()
}
