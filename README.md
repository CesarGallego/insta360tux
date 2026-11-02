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
```

Output is JSON when stdout is not a terminal (`--json` / `--json=false` to force). Errors go to
stderr. Global flags: `--json --device --backend --dry-run --timeout --clamp`.
Exit codes: 0 ok, 1 internal, 2 usage, 3 no device, 4 out of range, 5 transfer/timeout,
6 backend unavailable/unsupported, 7 permission/busy.

`insta360 --help`, `insta360 <cmd> --help` and `insta360 help-agent` (the whole reference as one
Markdown document, written for LLM agents). `--backend fake` gives an in-memory camera to try things.

Units: pan/tilt degrees (pan +-145, tilt -90..100); zoom raw 100..400 (100 = 1.0x);
focus 0..100; roll -100..100.

### Caveats

- The gimbal has only been seen moving while a **video stream is active** (hypothesis).
- `get` returns the **commanded** value, not the real position.
- Focus and roll controls are in the descriptor but unverified; `PANTILT_RELATIVE` is unsupported
  by the camera, so `move` is computed client-side.

## Library

```go
import "github.com/CesarGallego/insta360tux/camera"

cam, err := camera.Open(camera.Options{Clamp: true}) // backend auto, or INSTA360_BACKEND
if err != nil { log.Fatal(err) }
defer cam.Close()

res, err := cam.SetPanTilt(20, -10)   // degrees; errors.Is(err, camera.ErrOutOfRange) etc.
_, _ = cam.SetZoom(200)
pt, _ := cam.GetPanTilt()
```

Backends implement `camera.Backend` and register with `camera.Register` from files guarded by build
tags (`libusb.go` needs cgo; a pure-Go linux `v4l2` backend can be added the same way with a higher
`Priority`). `INSTA360_BACKEND` or `Options.Backend` overrides automatic selection.
Tests can use `camera/fake`.

## Development

```sh
go vet ./... && go test ./...
```
`tools/` has the original C probes (`dump_desc`, `probe_uvc`, `ptz_test`, `ptz_diag`).
