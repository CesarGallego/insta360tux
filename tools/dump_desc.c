// Vuelca descriptores USB y parsea las unidades UVC (VC) de una cámara.
// Uso: dump_desc [vid pid]   (por defecto 2e1a:4c01, Insta360 Link)
#include <libusb-1.0/libusb.h>
#include <stdio.h>
#include <stdlib.h>

static void hex(const uint8_t *p, int n) { for (int i = 0; i < n; i++) printf("%02x ", p[i]); }

static void guid(const uint8_t *g) {
    printf("{%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x}",
        g[3],g[2],g[1],g[0],g[5],g[4],g[7],g[6],g[8],g[9],g[10],g[11],g[12],g[13],g[14],g[15]);
}

static void parse_cs(const uint8_t *p, int len, int ifnum) {
    while (len >= 3) {
        int l = p[0];
        if (l < 3 || l > len) break;
        if (p[1] == 0x24) {  // CS_INTERFACE
            int st = p[2];
            printf("    CS_INTERFACE if%d subtype=0x%02x len=%d: ", ifnum, st, l); hex(p, l); printf("\n");
            if (st == 0x06 && l >= 24) {  // VC_EXTENSION_UNIT
                printf("      -> EXTENSION UNIT id=%d guid=", p[3]); guid(p + 4);
                int ncontrols = p[20], npins = p[21];
                int off = 22 + npins;
                int ctl = p[off];
                printf(" numControls=%d bmControls=", ncontrols);
                hex(p + off + 1, ctl);
                printf("\n");
            } else if (st == 0x05) printf("      -> PROCESSING UNIT id=%d\n", p[3]);
            else if (st == 0x02) printf("      -> INPUT TERMINAL id=%d type=0x%02x%02x\n", p[3], p[5], p[4]);
        }
        p += l; len -= l;
    }
}

int main(int argc, char **argv) {
    uint16_t vid = argc > 2 ? strtol(argv[1], 0, 16) : 0x2e1a;
    uint16_t pid = argc > 2 ? strtol(argv[2], 0, 16) : 0x4c01;
    libusb_context *ctx; libusb_init(&ctx);
    libusb_device **list; ssize_t n = libusb_get_device_list(ctx, &list);
    for (ssize_t i = 0; i < n; i++) {
        struct libusb_device_descriptor d; libusb_get_device_descriptor(list[i], &d);
        if (d.idVendor != vid || d.idProduct != pid) continue;
        printf("Dispositivo %04x:%04x class=%d bcdUSB=%x\n", d.idVendor, d.idProduct, d.bDeviceClass, d.bcdUSB);
        struct libusb_config_descriptor *c;
        if (libusb_get_active_config_descriptor(list[i], &c)) { puts("sin config"); continue; }
        for (int a = 0; a < c->bNumInterfaces; a++) {
            const struct libusb_interface *itf = &c->interface[a];
            for (int s = 0; s < itf->num_altsetting; s++) {
                const struct libusb_interface_descriptor *id = &itf->altsetting[s];
                printf("  Interface %d alt %d class=%d sub=%d proto=%d endpoints=%d\n",
                    id->bInterfaceNumber, id->bAlternateSetting, id->bInterfaceClass,
                    id->bInterfaceSubClass, id->bInterfaceProtocol, id->bNumEndpoints);
                for (int e = 0; e < id->bNumEndpoints; e++)
                    printf("    EP 0x%02x attrs=0x%02x maxpkt=%d\n", id->endpoint[e].bEndpointAddress,
                        id->endpoint[e].bmAttributes, id->endpoint[e].wMaxPacketSize);
                if (id->bInterfaceClass == 14 && id->bInterfaceSubClass == 1)
                    parse_cs(id->extra, id->extra_length, id->bInterfaceNumber);
            }
        }
        libusb_free_config_descriptor(c);
    }
    libusb_free_device_list(list, 1); libusb_exit(ctx);
    return 0;
}
