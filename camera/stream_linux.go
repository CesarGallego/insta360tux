//go:build linux

package camera

import (
	"encoding/binary"
	"syscall"
	"time"
	"unsafe"
)

// Minimal V4L2 mmap capture so the camera sees an open stream while the CLI
// moves it. Pure Go, no cgo. UNVERIFIED on real hardware: only cross-compiled.

const (
	v4l2BufTypeCapture = 1
	v4l2MemoryMmap     = 1
	pixMJPG            = 'M' | 'J'<<8 | 'P'<<16 | 'G'<<24
	pixYUYV            = 'Y' | 'U'<<8 | 'Y'<<16 | 'V'<<24
	bufCount           = 4
)

// v4l2Buffer is struct v4l2_buffer; Go alignment matches C on 32 and 64-bit
// (88 and 68 bytes). m is the union's offset/userptr word.
type v4l2Buffer struct {
	Index, Type, BytesUsed, Flags, Field uint32
	Timestamp                            syscall.Timeval
	Timecode                             [16]byte
	Sequence, Memory                     uint32
	M                                    uintptr
	Length, Reserved2, RequestFD         uint32
}

type v4l2ReqBufs struct {
	Count, Type, Memory uint32
	Capabilities        uint32
	Flags               uint8
	Reserved            [3]uint8
}

// struct v4l2_format / v4l2_streamparm as raw bytes: the union is 8-aligned on
// 64-bit (it holds pointers), so the pixel format starts at 8 there and at 4 on 32-bit.
var (
	fmtUnion   = int(unsafe.Sizeof(uintptr(0)))
	fmtSize    = fmtUnion + 200
	vidiocSFmt = ioc(iocRead|iocWrite, 'V', 5, uintptr(fmtSize))
	vidiocSPar = ioc(iocRead|iocWrite, 'V', 22, 204)
	vidiocReq  = ioc(iocRead|iocWrite, 'V', 8, unsafe.Sizeof(v4l2ReqBufs{}))
	vidiocQBuf = ioc(iocRead|iocWrite, 'V', 15, unsafe.Sizeof(v4l2Buffer{}))
	vidiocDQ   = ioc(iocRead|iocWrite, 'V', 17, unsafe.Sizeof(v4l2Buffer{}))
	vidiocQryB = ioc(iocRead|iocWrite, 'V', 9, unsafe.Sizeof(v4l2Buffer{}))
	vidiocOn   = ioc(iocWrite, 'V', 18, 4)
	vidiocOff  = ioc(iocWrite, 'V', 19, 4)
)

// KeepStream negotiates a format (MJPEG, then YUYV), maps buffers, starts
// streaming and drains frames in a goroutine until release is called.
func (b *v4l2Backend) KeepStream(o StreamOptions) (int, int, int, func() error, error) {
	if b.fd < 0 {
		return 0, 0, 0, nil, ErrNoDevice
	}
	if o.Width == 0 || o.Height == 0 {
		o.Width, o.Height = 1920, 1080
	}
	if o.FPS == 0 {
		o.FPS = 30
	}
	var f []byte
	var err error
	for _, pf := range []uint32{pixMJPG, pixYUYV} {
		f = make([]byte, fmtSize)
		binary.LittleEndian.PutUint32(f[0:], v4l2BufTypeCapture)
		u := f[fmtUnion:]
		binary.LittleEndian.PutUint32(u[0:], uint32(o.Width))
		binary.LittleEndian.PutUint32(u[4:], uint32(o.Height))
		binary.LittleEndian.PutUint32(u[8:], pf)
		binary.LittleEndian.PutUint32(u[12:], 1) // V4L2_FIELD_NONE
		if err = ioctl(b.fd, vidiocSFmt, unsafe.Pointer(&f[0])); err == nil {
			break
		}
	}
	if err != nil {
		return 0, 0, 0, nil, mapV4L2Err("S_FMT", err, true)
	}
	w, h := int(binary.LittleEndian.Uint32(f[fmtUnion:])), int(binary.LittleEndian.Uint32(f[fmtUnion+4:]))

	fps := o.FPS
	p := make([]byte, 204)
	binary.LittleEndian.PutUint32(p[0:], v4l2BufTypeCapture)
	binary.LittleEndian.PutUint32(p[12:], 1) // timeperframe numerator
	binary.LittleEndian.PutUint32(p[16:], uint32(o.FPS))
	if err := ioctl(b.fd, vidiocSPar, unsafe.Pointer(&p[0])); err == nil {
		if n, d := binary.LittleEndian.Uint32(p[12:]), binary.LittleEndian.Uint32(p[16:]); n != 0 {
			fps = int(d / n)
		}
	} // a failed S_PARM is not fatal: the driver keeps its own rate

	rb := v4l2ReqBufs{Count: bufCount, Type: v4l2BufTypeCapture, Memory: v4l2MemoryMmap}
	if err := ioctl(b.fd, vidiocReq, unsafe.Pointer(&rb)); err != nil {
		return 0, 0, 0, nil, mapV4L2Err("REQBUFS", err, true)
	}
	var maps [][]byte
	cleanup := func() {
		for _, m := range maps {
			syscall.Munmap(m)
		}
		rb := v4l2ReqBufs{Type: v4l2BufTypeCapture, Memory: v4l2MemoryMmap}
		ioctl(b.fd, vidiocReq, unsafe.Pointer(&rb)) // free the buffers
	}
	for i := uint32(0); i < rb.Count; i++ {
		buf := v4l2Buffer{Index: i, Type: v4l2BufTypeCapture, Memory: v4l2MemoryMmap}
		if err := ioctl(b.fd, vidiocQryB, unsafe.Pointer(&buf)); err != nil {
			cleanup()
			return 0, 0, 0, nil, mapV4L2Err("QUERYBUF", err, true)
		}
		m, err := syscall.Mmap(b.fd, int64(buf.M), int(buf.Length), syscall.PROT_READ, syscall.MAP_SHARED)
		if err != nil {
			cleanup()
			return 0, 0, 0, nil, &TransferError{Op: "mmap", Err: err}
		}
		maps = append(maps, m)
		if err := ioctl(b.fd, vidiocQBuf, unsafe.Pointer(&buf)); err != nil {
			cleanup()
			return 0, 0, 0, nil, mapV4L2Err("QBUF", err, true)
		}
	}
	typ := int32(v4l2BufTypeCapture)
	if err := ioctl(b.fd, vidiocOn, unsafe.Pointer(&typ)); err != nil {
		cleanup()
		return 0, 0, 0, nil, mapV4L2Err("STREAMON", err, true)
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() { // drain: dequeue, then give the buffer back
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// The fd is O_NONBLOCK: EAGAIN means no frame yet.
			buf := v4l2Buffer{Type: v4l2BufTypeCapture, Memory: v4l2MemoryMmap}
			if ioctl(b.fd, vidiocDQ, unsafe.Pointer(&buf)) == nil {
				ioctl(b.fd, vidiocQBuf, unsafe.Pointer(&buf))
			} else {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()
	release := func() error {
		close(stop)
		<-done
		err := ioctl(b.fd, vidiocOff, unsafe.Pointer(&typ))
		cleanup()
		return err
	}
	return w, h, fps, release, nil
}
