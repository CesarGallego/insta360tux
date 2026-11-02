// Sondeo SOLO LECTURA de controles UVC: estándar (Camera Terminal) y extension units.
// Solo emite GET_* (bmRequestType 0xA1). No envía SET_CUR.
#include <libusb-1.0/libusb.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define GET_CUR 0x81
#define GET_MIN 0x82
#define GET_MAX 0x83
#define GET_RES 0x84
#define GET_LEN 0x85
#define GET_INFO 0x86
#define GET_DEF 0x87

static libusb_device_handle *h;

static int get(uint8_t req, uint8_t unit, uint8_t sel, uint8_t *buf, int len) {
    return libusb_control_transfer(h, 0xA1, req, sel << 8, (unit << 8) | 0, buf, len, 500);
}

static void show(const char *name, uint8_t req, uint8_t unit, uint8_t sel, int len) {
    uint8_t b[64] = {0};
    int r = get(req, unit, sel, b, len);
    printf("    %-8s ", name);
    if (r < 0) { printf("error %s\n", libusb_error_name(r)); return; }
    for (int i = 0; i < r; i++) printf("%02x ", b[i]);
    printf("(%d bytes)\n", r);
}

static void probe_std(const char *name, uint8_t sel, int len) {
    uint8_t info = 0;
    int r = get(GET_INFO, 1, sel, &info, 1);
    printf("  [CT sel 0x%02x] %s  GET_INFO=%s0x%02x\n", sel, name, r < 0 ? "err " : "", r < 0 ? 0 : info);
    if (r < 0) return;
    show("CUR", GET_CUR, 1, sel, len); show("MIN", GET_MIN, 1, sel, len);
    show("MAX", GET_MAX, 1, sel, len); show("RES", GET_RES, 1, sel, len); show("DEF", GET_DEF, 1, sel, len);
}

static void probe_xu(uint8_t unit) {
    printf("\n== Extension unit %d ==\n", unit);
    for (uint8_t sel = 1; sel <= 32; sel++) {
        uint8_t info = 0, l[2] = {0};
        if (get(GET_INFO, unit, sel, &info, 1) < 0) continue;
        int rl = get(GET_LEN, unit, sel, l, 2);
        int len = rl == 2 ? l[0] | (l[1] << 8) : 0;
        printf("  [sel 0x%02x] GET_INFO=0x%02x (get=%d set=%d) len=%d\n", sel, info, info & 1, (info >> 1) & 1, len);
        if (len > 0 && len <= 64 && (info & 1)) {
            show("CUR", GET_CUR, unit, sel, len); show("MIN", GET_MIN, unit, sel, len);
            show("MAX", GET_MAX, unit, sel, len); show("DEF", GET_DEF, unit, sel, len);
        }
    }
}

int main(void) {
    libusb_init(NULL);
    h = libusb_open_device_with_vid_pid(NULL, 0x2e1a, 0x4c01);
    if (!h) { puts("No se pudo abrir la cámara (permisos / ocupada)"); return 1; }
    puts("== Controles estándar (Camera Terminal, unit 1) ==");
    probe_std("FOCUS_ABS", 0x06, 2);
    probe_std("ZOOM_ABS", 0x0B, 2);
    probe_std("PANTILT_ABS", 0x0D, 8);
    probe_std("PANTILT_REL", 0x0E, 4);
    probe_std("ROLL_ABS", 0x0F, 2);
    probe_xu(9); probe_xu(10); probe_xu(11);
    libusb_close(h); libusb_exit(NULL);
    return 0;
}
