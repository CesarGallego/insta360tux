package camera

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestV4L2IoctlNumbers(t *testing.T) {
	for name, c := range map[string]struct{ got, want uintptr }{
		"QUERYCAP":  {vidiocQueryCap, 0x80685600},
		"G_CTRL":    {vidiocGCtrl, 0xc008561b},
		"S_CTRL":    {vidiocSCtrl, 0xc008561c},
		"QUERYCTRL": {vidiocQueryCtrl, 0xc0445624},
	} {
		if c.got != c.want {
			t.Errorf("%s = %#x, want %#x", name, c.got, c.want)
		}
	}
}

func TestV4L2StructSizes(t *testing.T) {
	if s := unsafe.Sizeof(v4l2Capability{}); s != 104 {
		t.Errorf("capability %d", s)
	}
	if s := unsafe.Sizeof(v4l2Control{}); s != 8 {
		t.Errorf("control %d", s)
	}
	if s := unsafe.Sizeof(v4l2QueryCtrl{}); s != 68 {
		t.Errorf("queryctrl %d", s)
	}
}

func TestIsCaptureNode(t *testing.T) {
	cases := []struct {
		c    v4l2Capability
		want bool
	}{
		{v4l2Capability{Caps: 0x84a00001, DeviceCaps: 0x04200001}, true},
		{v4l2Capability{Caps: 0x84a00001, DeviceCaps: 0x04a00000}, false}, // metadata
		{v4l2Capability{Caps: 0x00000001}, true},
		{v4l2Capability{Caps: 0x80000000, DeviceCaps: 0x00000002}, false},
	}
	for i, c := range cases {
		if got := isCaptureNode(c.c); got != c.want {
			t.Errorf("case %d: %v", i, got)
		}
	}
}

func TestReadUSBIdentAndSelector(t *testing.T) {
	d := t.TempDir()
	for n, v := range map[string]string{"idVendor": "2e1a\n", "idProduct": "4c01\n", "serial": "ABC\n", "busnum": "3\n", "devnum": "7\n"} {
		os.WriteFile(filepath.Join(d, n), []byte(v), 0o644)
	}
	id, err := readUSBIdent(d)
	if err != nil || id.Vendor != 0x2e1a || id.Product != 0x4c01 || id.Serial != "ABC" || id.Bus != 3 || id.Addr != 7 {
		t.Fatalf("%+v %v", id, err)
	}
	if _, err := readUSBIdent(t.TempDir()); err == nil {
		t.Error("expected error")
	}
	for dev, want := range map[string]bool{"": true, "2e1a:4c01": true, "1234:4c01": false, "3.7": true, "3.8": false, "ABC": true, "XYZ": false, "/dev/video0": true, "/dev/video2": false} {
		if got := parseV4L2Selector(dev).matches("/dev/video0", id); got != want {
			t.Errorf("selector %q = %v", dev, got)
		}
	}
	if videoNodeNumber("/dev/video12") != 12 || videoNodeNumber("videoX") != -1 || videoNodeNumber("media0") != -1 {
		t.Error("videoNodeNumber")
	}
	if cString([]byte("Link\x00junk")) != "Link" {
		t.Error("cString")
	}
}
