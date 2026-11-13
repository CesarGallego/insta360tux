package camera

// StreamOptions configure KeepStream. Zero values mean 1920x1080 at 30 fps.
type StreamOptions struct {
	Width, Height int
	FPS           int
}

// StreamKeeper is optionally implemented by backends that can hold a video
// capture stream open (today only v4l2 on Linux, verified on hardware).
// The camera only moves, and only keeps its pose, while a stream is open; with
// no stream it enters privacy mode by itself (see PROTOCOL.md).
type StreamKeeper interface {
	// KeepStream starts capturing (frames are read and discarded in a
	// goroutine) and returns the negotiated size and a release function.
	KeepStream(StreamOptions) (w, h, fps int, release func() error, err error)
}

// KeepStream opens a capture stream that stays open until release is called.
// ErrUnsupported if the backend cannot (macOS/libusb: use another app).
func (c *Camera) KeepStream(o StreamOptions) (w, h, fps int, release func() error, err error) {
	s, ok := c.b.(StreamKeeper)
	if !ok {
		return 0, 0, 0, nil, ErrUnsupported
	}
	if c.opts.DryRun {
		return o.Width, o.Height, o.FPS, func() error { return nil }, nil
	}
	return s.KeepStream(o)
}
