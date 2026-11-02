# insta360tux

Library and CLI to control the gimbal (pan / tilt / zoom, plus focus and roll) of an
**Insta360 Link** (USB `2e1a:4c01`) through the standard UVC PTZ controls.
Protocol notes: [PROTOCOL.md](PROTOCOL.md). Project state: [HANDOFF.md](HANDOFF.md).

## Install

Requires Go 1.27+, `libusb` and `pkg-config` (the libusb backend uses cgo via
[gousb](https://github.com/google/gousb)).

```sh
brew install libusb pkgconf        # macOS
sudo apt install libusb-1.0-0-dev pkg-config   # Debian/Ubuntu
go install github.com/CesarGallego/insta360tux/cmd/insta360@latest
```

Build without cgo/libusb: `CGO_ENABLED=0` or `-tags nolibusb` (then only other backends exist).

## CLI

```sh
insta360 list
insta360 info
insta360 get
insta360 set --pan 30 --tilt -10 --zoom 200
insta360 move --pan -5
insta360 center            # alias: home
insta360 zoom 150
insta360 --dry-run set --pan 10
insta360 mode                # current mode; or: insta360 mode overhead
insta360 set --normal --pan 0 --tilt 0   # leave tracking/desk/... first, then move
```

Output is JSON when stdout is not a terminal (`--json` / `--json=false` to force). Errors go to
stderr (with `--json`: `{"error":{"code":"out_of_range","message":"...","exit_code":4}}`).
Global flags `--json --device --backend --dry-run --timeout --clamp` and command flags may appear in
any position (`insta360 zoom 200 --json`); `--` ends flag parsing.
Exit codes: 0 ok, 1 internal, 2 usage, 3 no device, 4 out of range, 5 transfer/timeout,
6 backend unavailable/unsupported, 7 permission/busy.

`insta360 --help`, `insta360 <cmd> --help` and `insta360 help-agent` (the whole reference as one
Markdown document, written for LLM agents). `--backend fake` gives an in-memory camera to try things.

Units: pan/tilt degrees (pan +-145, tilt -90..100); zoom raw 100..400 (100 = 1.0x);
focus 0..100; roll -100..100.

### Caveats

- The gimbal has only been seen moving while a **video stream is active** (hypothesis).
- `get` returns the **commanded** value, not the real position.
- Ranges are queried from the camera (`info` shows `ranges_source`: `device` or `default`); they
  matched the built-in defaults on a real Link.
- `PANTILT_RELATIVE` (and zoom/roll relative) are unsupported by the camera, so `move` is computed client-side.
- Manual focus is rejected while autofocus is on: `set --focus N` switches autofocus off first;
  `set --autofocus=true` restores it. Roll is signed (-100..100).
- Verified on a real Link under macOS (libusb backend, Photo Booth streaming): absolute pan/tilt/zoom,
  relative `move`, `center`, `--clamp`, `--dry-run`, focus, roll, `--device` selectors. See HANDOFF.md.

### Modes (`insta360 mode`)

`mode [normal|tracking|whiteboard|overhead|desk]` (no argument: print it; `get --all` includes it).
Libusb backend only on macOS/Linux; the v4l2 backend has an untested implementation through
`UVCIOC_CTRL_QUERY`. Always finish with `mode normal`.

| Mode | Wire | Effect (confirmed by the user in the official app, reproduced here without it) |
|---|---|---|
| normal | 0 | default; pan/tilt/zoom are yours |
| tracking | 1 | AI tracking: the camera moves **by itself** following a face; `set --pan` is overridden |
| whiteboard | 4 | zooms out (zoom 100) looking for **QR markers** on a whiteboard; returns to normal on its own after ~10 s if none |
| overhead | 5 | tilts straight down (-90 deg), zoom 100 |
| desk | 6 | desk view: tilts to about -45 deg, frames the hands, zoom 100 |

`set`/`move` print a warning if the camera is not in `normal`; `--normal` (alias `--release`) switches to
`normal` first and waits 1.5 s. Modes `0x02` and `0x03` were never seen and are not settable.
The official app's extra handshake (unit 9 selectors `0x14`/`0x15`) is **not** needed.
Details and verified/unverified lists: PROTOCOL.md.

## Library

```go
import "github.com/CesarGallego/insta360tux/camera"

cam, err := camera.Open(camera.Options{Clamp: true}) // backend auto, or INSTA360_BACKEND
if err != nil { log.Fatal(err) }
defer cam.Close()

res, err := cam.SetPanTilt(20, -10)   // degrees; errors.Is(err, camera.ErrOutOfRange) etc.
_, _ = cam.SetZoom(200)
pt, _ := cam.GetPanTilt()
_, _ = cam.SetMode(camera.ModeOverhead) // optional: backend implements camera.ModeController
```

Backends implement `camera.Backend` and register with `camera.Register` from files guarded by build
tags (`libusb.go` needs cgo; `v4l2_linux.go` is pure Go, Priority 100). `INSTA360_BACKEND` or `Options.Backend` overrides automatic selection.
Tests can use `camera/fake`.

## Linux (v4l2 backend)

On Linux the pure-Go `v4l2` backend (no cgo, no libusb) is preferred automatically. It uses V4L2 ioctls on
`/dev/video*` (pan/tilt in arc-seconds, zoom, focus; ranges come from the driver; no roll control exists in V4L2).
Select a device with `/dev/videoN`, `2e1a:4c01`, `BUS.ADDR` or the serial; force it with `INSTA360_BACKEND=v4l2`.

- Your user needs access to `/dev/videoN` (usually the `video` group). EBUSY/EACCES map to the permission error.
- Movement generally needs an active video stream. If a set fails or has no effect, open a stream first:
  `ffplay /dev/video0` or `v4l2-ctl -d /dev/video0 --stream-mmap --stream-count=0`.
- Modes (`insta360 mode`) use `UVCIOC_CTRL_QUERY` on the unit 9 extension unit (no uvcvideo mapping needed);
  implemented but untested on real Linux. If it fails the command exits 5 or 7.
- Build check: `GOOS=linux CGO_ENABLED=0 go build ./...`. Not yet tested on real Linux hardware.

## Development

```sh
go vet ./... && go test ./...
```
`tools/` has the original C probes (`dump_desc`, `probe_uvc`, `ptz_test`, `ptz_diag`).
