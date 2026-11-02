// Diagnóstico: mueve pan +30° y muestra lectura estándar vs. selector 0x1a de la unit 9 mientras tanto.
#include <libusb-1.0/libusb.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <stdint.h>

static libusb_device_handle *h;
static int rd(uint8_t unit, uint8_t sel, uint8_t *b, int n) {
    return libusb_control_transfer(h, 0xA1, 0x81, sel << 8, (unit << 8), b, n, 500);
}
static void snap(const char *tag) {
    uint8_t a[8], x[8]; int32_t p, t, xt, xp;
    rd(1, 0x0D, a, 8); memcpy(&p, a, 4); memcpy(&t, a + 4, 4);
    rd(9, 0x1a, x, 8); memcpy(&xt, x, 4); memcpy(&xp, x + 4, 4);
    printf("%-10s std pan=%7d tilt=%7d | XU9[0x1a] tilt?=%7d pan?=%7d\n", tag, p, t, xt, xp);
}
int main(void) {
    libusb_init(NULL);
    h = libusb_open_device_with_vid_pid(NULL, 0x2e1a, 0x4c01);
    if (!h) { puts("no se pudo abrir"); return 1; }
    uint8_t a[8]; int32_t p0, t0;
    rd(1, 0x0D, a, 8); memcpy(&p0, a, 4); memcpy(&t0, a + 4, 4);
    snap("inicio");
    int32_t np = p0 + 30 * 3600; uint8_t b[8];
    memcpy(b, &np, 4); memcpy(b + 4, &t0, 4);
    printf("SET pan=%d -> %d\n", np, libusb_control_transfer(h, 0x21, 0x01, 0x0D << 8, 1 << 8, b, 8, 500));
    for (int i = 0; i < 6; i++) { usleep(500000); snap("t+0.5s"); }
    memcpy(b, &p0, 4);
    printf("restaurar -> %d\n", libusb_control_transfer(h, 0x21, 0x01, 0x0D << 8, 1 << 8, b, 8, 500));
    for (int i = 0; i < 6; i++) { usleep(500000); snap("t+0.5s"); }
    libusb_close(h); libusb_exit(NULL);
    return 0;
}
