// Package camera controls the gimbal (pan, tilt, zoom and, where supported,
// focus and roll) of an Insta360 Link camera (USB 2e1a:4c01).
//
// The package is split in three layers:
//
//   - Backend: the minimal interface a transport must implement. Backends are
//     registered with Register from init functions, guarded by build tags
//     (the libusb backend needs cgo; a future v4l2 backend is linux only).
//   - Camera: wraps a Backend adding range validation, optional clamping,
//     relative moves and dry-run mode. This is what most callers want.
//   - Units: pan/tilt travel on the wire in arc-seconds (3600 = 1 degree);
//     this package always exposes degrees.
//
// Typical use:
//
//	cam, err := camera.Open(camera.Options{})
//	if err != nil { log.Fatal(err) }
//	defer cam.Close()
//	res, err := cam.SetPanTilt(20, -10)
//
// # Caveats
//
// Movement has only been observed while a video stream is active (another
// application is using the camera); with no stream the gimbal appears to sit
// idle. Also, reading back pan/tilt returns the last value commanded, not the
// measured position of the gimbal.
package camera
