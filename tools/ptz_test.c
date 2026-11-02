// Prueba mínima de escritura: mueve pan +5° respecto al valor actual, lee, y restaura.
#include <libusb-1.0/libusb.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <stdint.h>

static libusb_device_handle *h;

static int get_pt(int32_t *pan, int32_t *tilt) {
    uint8_t b[8];
    int r = libusb_control_transfer(h, 0xA1, 0x81, 0x0D << 8, (1 << 8), b, 8, 500);
    if (r != 8) return -1;
    memcpy(pan, b, 4); memcpy(tilt, b + 4, 4);
    return 0;
}
static int set_pt(int32_t pan, int32_t tilt) {
    uint8_t b[8];
    memcpy(b, &pan, 4); memcpy(b + 4, &tilt, 4);
    return libusb_control_transfer(h, 0x21, 0x01, 0x0D << 8, (1 << 8), b, 8, 500);
}

int main(void) {
    libusb_init(NULL);
    h = libusb_open_device_with_vid_pid(NULL, 0x2e1a, 0x4c01);
    if (!h) { puts("no se pudo abrir"); return 1; }
    int32_t pan0, tilt0, pan1, tilt1;
    if (get_pt(&pan0, &tilt0)) { puts("fallo leyendo"); return 1; }
    printf("inicio:   pan=%d tilt=%d\n", pan0, tilt0);
    int r = set_pt(pan0 + 5 * 3600, tilt0);
    printf("SET_CUR pan+5deg -> %d\n", r);
    sleep(2);
    get_pt(&pan1, &tilt1);
    printf("tras set: pan=%d tilt=%d\n", pan1, tilt1);
    r = set_pt(pan0, tilt0);
    printf("restaurar -> %d\n", r);
    sleep(2);
    get_pt(&pan1, &tilt1);
    printf("final:    pan=%d tilt=%d\n", pan1, tilt1);
    libusb_close(h); libusb_exit(NULL);
    return 0;
}
