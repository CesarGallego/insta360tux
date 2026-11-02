// Interceptor de libusb_control_transfer para Insta360 Link Controller (copia re-firmada).
// La app enlaza libusb ESTATICAMENTE, asi que DYLD_INTERPOSE no sirve (las llamadas son
// internas). Parcheamos en memoria la entrada de libusb_control_transfer (arm64): las 4
// primeras instrucciones se ejecutan en un trampolin (asm, en este dylib) y el original
// continua en +16. Solo se parchea si los bytes coinciden con los esperados.
#include <dlfcn.h>
#include <fcntl.h>
#include <mach/mach.h>
#include <mach/mach_vm.h>
#include <mach-o/dyld.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>
#include <time.h>
#include <unistd.h>
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/IOCFPlugIn.h>

#define OFF_FROM_BASE 0x1261d60UL - 0x100000000UL  // offset en el slice arm64 (T _libusb_control_transfer)

static const uint32_t EXPECT[4] = {0xd10203ff, 0xa9026ffc, 0xa90367fa, 0xa9045ff8};
// sub sp,sp,#0x80 ; stp x28,x27,[sp,#0x20] ; stp x26,x25,[sp,#0x30] ; stp x24,x23,[sp,#0x40]

typedef int (*ctl_fn)(void *, uint8_t, uint8_t, uint16_t, uint16_t, unsigned char *, uint16_t, unsigned);
extern int orig_trampoline(void *, uint8_t, uint8_t, uint16_t, uint16_t, unsigned char *, uint16_t, unsigned);
uint64_t orig_continue;  // direccion de entrada original + 16 (usada por el asm)

__asm__(
    ".text\n.align 4\n.globl _orig_trampoline\n_orig_trampoline:\n"
    "  sub sp, sp, #0x80\n"
    "  stp x28, x27, [sp, #0x20]\n"
    "  stp x26, x25, [sp, #0x30]\n"
    "  stp x24, x23, [sp, #0x40]\n"
    "  adrp x16, _orig_continue@PAGE\n"
    "  ldr x16, [x16, _orig_continue@PAGEOFF]\n"
    "  br x16\n");

static int logfd = -1;
static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;

static void stamp(char *out, size_t n) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    snprintf(out, n, "%ld.%06ld", (long)ts.tv_sec, ts.tv_nsec / 1000);
}

static void emit(const char *dir, uint8_t rt, uint8_t req, uint16_t val, uint16_t idx, unsigned char *d,
                 uint16_t len, int ret) {
    if (logfd < 0) return;
    char ts[40], line[4096];
    stamp(ts, sizeof ts);
    int n = snprintf(line, sizeof line, "%s %s rt=%02x req=%02x val=%04x idx=%04x len=%u ret=%d", ts, dir, rt, req,
                     val, idx, len, ret);
    if ((rt & 0x60) == 0x20)  // clase: UVC -> unit/selector
        n += snprintf(line + n, sizeof line - n, " unit=%u sel=0x%02x", idx >> 8, val >> 8);
    int dl = (!strcmp(dir, "IN")) ? (ret > 0 ? ret : 0) : len;
    if (d && dl > 0) {
        n += snprintf(line + n, sizeof line - n, " data=");
        for (int i = 0; i < dl && n < (int)sizeof line - 4; i++) n += snprintf(line + n, sizeof line - n, "%02x", d[i]);
    }
    line[n++] = '\n';
    pthread_mutex_lock(&mu);
    (void)!write(logfd, line, n);
    pthread_mutex_unlock(&mu);
}

static int hook(void *h, uint8_t rt, uint8_t req, uint16_t val, uint16_t idx, unsigned char *d, uint16_t len,
                unsigned to) {
    if (!(rt & 0x80)) emit("OUT", rt, req, val, idx, d, len, 0);  // antes: lo que se envia
    int r = orig_trampoline(h, rt, req, val, idx, d, len, to);
    if (rt & 0x80) emit("IN", rt, req, val, idx, d, len, r);
    else emit("OUT-RET", rt, req, val, idx, NULL, len, r);
    return r;
}

__attribute__((constructor)) static void init(void) {
    const char *path = getenv("INSTA360_XU_LOG");
    if (!path) path = "xu.log";
    logfd = open(path, O_WRONLY | O_CREAT | O_APPEND, 0644);
    char ts[40];
    stamp(ts, sizeof ts);
    char m[256];
    // La dirección: preferir el simbolo; si no, base del ejecutable + offset.
    uint8_t *fn = dlsym(RTLD_DEFAULT, "libusb_control_transfer");
    const char *how = "dlsym";
    if (!fn) {
        const char *exe = _dyld_get_image_name(0);
        if (!exe || !strstr(exe, "Webcam-desktop")) return;  // solo la app objetivo
        fn = (uint8_t *)_dyld_get_image_header(0) + (OFF_FROM_BASE);
        how = "offset";
    }
    if (memcmp(fn, EXPECT, 16) != 0) {
        int n = snprintf(m, sizeof m, "%s INFO xulog: bytes inesperados en %p (%s); NO parcheado\n", ts, fn, how);
        if (logfd >= 0) (void)!write(logfd, m, n);
        return;
    }
    orig_continue = (uint64_t)(fn + 16);
    uint32_t patch[4] = {0x58000050,  // ldr x16, #8
                         0xd61f0200,  // br x16
                         (uint32_t)(uintptr_t)hook, (uint32_t)((uint64_t)(uintptr_t)hook >> 32)};
    kern_return_t kr = mach_vm_protect(mach_task_self(), (mach_vm_address_t)fn, 16, 0,
                                       VM_PROT_READ | VM_PROT_WRITE | VM_PROT_COPY);
    if (kr == KERN_SUCCESS) {
        memcpy(fn, patch, 16);
        mach_vm_protect(mach_task_self(), (mach_vm_address_t)fn, 16, 0, VM_PROT_READ | VM_PROT_EXECUTE);
        __builtin___clear_cache((char *)fn, (char *)fn + 16);
    }
    int n = snprintf(m, sizeof m, "%s INFO xulog: hook en %p (%s) kr=%d pid=%d\n", ts, fn, how, kr, getpid());
    if (logfd >= 0) (void)!write(logfd, m, n);
}

// ---------------------------------------------------------------------------------------------
// Ruta real de la app para UVC: IOKit (IOCreatePlugInInterfaceForService -> QueryInterface ->
// IOUSBInterfaceInterface/IOUSBDeviceInterface). Se envuelven las tablas de funciones COM
// (copia de la vtable con ControlRequest/DeviceRequest y variantes TO/Async enganchadas).
// Los indices de slot son los del ABI historico de IOUSBLib (ver PROTOCOL.md, "Interceptor").
typedef struct { uint8_t bmRequestType, bRequest; uint16_t wValue, wIndex, wLength; void *pData; uint32_t wLenDone; uint32_t noDataTimeout, completionTimeout; } DevReq;
#define NSLOT 64
typedef struct { void **orig; void **copy; } VT;
static VT vts[32];
static int nvt;
static pthread_mutex_t vmu = PTHREAD_MUTEX_INITIALIZER;
static void *origslot[NSLOT];  // por slot: el original (las dos familias usan slots distintos; ver envoltorios)

enum { I_CTRL = 24, I_CTRL_ASYNC = 25, I_CTRL_TO = 37, I_CTRL_ASYNC_TO = 38, D_REQ = 26, D_REQ_ASYNC = 27, D_REQ_TO = 30, D_REQ_ASYNC_TO = 31 };
static int is_iface;  // la familia se decide por wrapper: usamos dos tablas de originales
static void *orig_if[NSLOT], *orig_dev[NSLOT];

static void logreq(const char *tag, DevReq *r, int ret, int post) {
    if (logfd < 0 || !r) return;
    char ts[40], line[4096];
    stamp(ts, sizeof ts);
    int in = r->bmRequestType & 0x80;
    int n = snprintf(line, sizeof line, "%s %s %s rt=%02x req=%02x val=%04x idx=%04x len=%u ret=0x%x", ts,
                     post ? (in ? "IN" : "OUT-RET") : (in ? "IN-REQ" : "OUT"), tag, r->bmRequestType, r->bRequest,
                     r->wValue, r->wIndex, r->wLength, (unsigned)ret);
    if ((r->bmRequestType & 0x60) == 0x20)
        n += snprintf(line + n, sizeof line - n, " unit=%u sel=0x%02x", r->wIndex >> 8, r->wValue >> 8);
    int dl = -1;
    if (post && in) dl = r->wLenDone;
    if (!post && !in) dl = r->wLength;
    if (dl > 0 && r->pData) {
        n += snprintf(line + n, sizeof line - n, " data=");
        for (int i = 0; i < dl && n < (int)sizeof line - 4; i++) n += snprintf(line + n, sizeof line - n, "%02x", ((uint8_t *)r->pData)[i]);
    }
    line[n++] = '\n';
    pthread_mutex_lock(&mu);
    (void)!write(logfd, line, n);
    pthread_mutex_unlock(&mu);
}

typedef int (*f_ctl)(void *, uint8_t, DevReq *);
typedef int (*f_dev)(void *, DevReq *);
typedef int (*f_ctl_a)(void *, uint8_t, DevReq *, void *, void *);
typedef int (*f_dev_a)(void *, DevReq *, void *, void *);

static int h_ctl(void *s, uint8_t p, DevReq *r) { logreq("if.Ctrl", r, 0, 0); int x = ((f_ctl)orig_if[I_CTRL])(s, p, r); logreq("if.Ctrl", r, x, 1); return x; }
static int h_ctl_to(void *s, uint8_t p, DevReq *r) { logreq("if.CtrlTO", r, 0, 0); int x = ((f_ctl)orig_if[I_CTRL_TO])(s, p, r); logreq("if.CtrlTO", r, x, 1); return x; }
static int h_ctl_a(void *s, uint8_t p, DevReq *r, void *c, void *f) { logreq("if.CtrlAsync", r, 0, 0); return ((f_ctl_a)orig_if[I_CTRL_ASYNC])(s, p, r, c, f); }
static int h_ctl_ato(void *s, uint8_t p, DevReq *r, void *c, void *f) { logreq("if.CtrlAsyncTO", r, 0, 0); return ((f_ctl_a)orig_if[I_CTRL_ASYNC_TO])(s, p, r, c, f); }
static int h_dev(void *s, DevReq *r) { logreq("dev.Req", r, 0, 0); int x = ((f_dev)orig_dev[D_REQ])(s, r); logreq("dev.Req", r, x, 1); return x; }
static int h_dev_to(void *s, DevReq *r) { logreq("dev.ReqTO", r, 0, 0); int x = ((f_dev)orig_dev[D_REQ_TO])(s, r); logreq("dev.ReqTO", r, x, 1); return x; }
static int h_dev_a(void *s, DevReq *r, void *c, void *f) { logreq("dev.ReqAsync", r, 0, 0); return ((f_dev_a)orig_dev[D_REQ_ASYNC])(s, r, c, f); }
static int h_dev_ato(void *s, DevReq *r, void *c, void *f) { logreq("dev.ReqAsyncTO", r, 0, 0); return ((f_dev_a)orig_dev[D_REQ_ASYNC_TO])(s, r, c, f); }

static void note(const char *fmt, ...) __attribute__((format(printf, 1, 2)));
static void note(const char *fmt, ...) {
    if (logfd < 0) return;
    char ts[40], line[1024];
    stamp(ts, sizeof ts);
    int n = snprintf(line, sizeof line, "%s INFO ", ts);
    va_list ap; va_start(ap, fmt); n += vsnprintf(line + n, sizeof line - n, fmt, ap); va_end(ap);
    line[n++] = '\n';
    (void)!write(logfd, line, n);
}

// iid de las familias (bytes UUID). Interfaz: 2D9786C6-9EF3-11D4-AD51-000A27052861 (+ variantes
// que solo cambian el 1er byte de grupo); dispositivo: 5c8187d0-9ef3-11d4-8b45-000a27052861.
static int family_of(CFUUIDBytes b) {
    static const uint8_t tail_if[] = {0xB1, 0xD0, 0x00, 0x0A, 0x27, 0x05, 0x28, 0x61};
    static const uint8_t tail_dev[] = {0x8B, 0x45, 0x00, 0x0A, 0x27, 0x05, 0x28, 0x61};
    const uint8_t *p = (const uint8_t *)&b;
    (void)tail_if; (void)tail_dev;
    // Familias por sufijo del UUID (bytes 8..15) y bytes 4..7 = 9E F3 11 D4.
    if (memcmp(p + 4, "\x9e\xf3\x11\xd4", 4)) return 0;
    if (!memcmp(p + 8, tail_if, 8)) return 1;
    if (!memcmp(p + 8, tail_dev, 8)) return 2;
    return 0;
}

static void wrap_iface(void ***obj, int fam) {
    void **vt = *obj;
    pthread_mutex_lock(&vmu);
    for (int i = 0; i < nvt; i++) {
        if (vts[i].copy == vt) { pthread_mutex_unlock(&vmu); return; }
        if (vts[i].orig == vt) { *obj = vts[i].copy; pthread_mutex_unlock(&vmu); return; }
    }
    void **cp = calloc(NSLOT, sizeof(void *));
    memcpy(cp, vt, NSLOT * sizeof(void *));
    void **o = fam == 1 ? orig_if : orig_dev;
    memcpy(o, vt, NSLOT * sizeof(void *));
    if (fam == 1) { cp[I_CTRL] = h_ctl; cp[I_CTRL_TO] = h_ctl_to; cp[I_CTRL_ASYNC] = h_ctl_a; cp[I_CTRL_ASYNC_TO] = h_ctl_ato; }
    else { cp[D_REQ] = h_dev; cp[D_REQ_TO] = h_dev_to; cp[D_REQ_ASYNC] = h_dev_a; cp[D_REQ_ASYNC_TO] = h_dev_ato; }
    vts[nvt].orig = vt; vts[nvt].copy = cp; nvt++;
    pthread_mutex_unlock(&vmu);
    *obj = cp;
    note("wrap %s vtable orig=%p", fam == 1 ? "IOUSBInterface" : "IOUSBDevice", (void *)vt);
}

typedef int (*f_qi)(void *, CFUUIDBytes, void **);
typedef struct { void **copy, **orig; } QP;
static QP qps[256];
static int nqp;
static int h_qi(void *self, CFUUIDBytes iid, void **out) {
    void **cur = *(void ***)self, **orig = NULL;
    pthread_mutex_lock(&vmu);
    for (int i = 0; i < nqp; i++) if (qps[i].copy == cur) orig = qps[i].orig;
    pthread_mutex_unlock(&vmu);
    if (!orig) return -1;
    int r = ((f_qi)orig[1])(self, iid, out);
    int fam = family_of(iid);
    CFUUIDRef u = CFUUIDCreateFromUUIDBytes(NULL, iid);
    CFStringRef str = CFUUIDCreateString(NULL, u);
    char b[64] = "?";
    CFStringGetCString(str, b, sizeof b, kCFStringEncodingUTF8);
    note("QueryInterface %s -> r=%d fam=%d", b, r, fam);
    CFRelease(str); CFRelease(u);
    if (r == 0 && fam && out && *out) wrap_iface((void ***)*out, fam);
    return r;
}

static kern_return_t my_IOCreatePlugIn(io_service_t svc, CFUUIDRef pt, CFUUIDRef it, IOCFPlugInInterface ***ti, SInt32 *score) {
    kern_return_t kr = IOCreatePlugInInterfaceForService(svc, pt, it, ti, score);
    if (kr == 0 && ti && *ti) {
        void ***obj = (void ***)*ti;
        void **vt = *obj;
        pthread_mutex_lock(&vmu);
        if (nqp < 256) {
            void **cp = calloc(16, sizeof(void *));
            memcpy(cp, vt, 16 * sizeof(void *));
            cp[1] = h_qi;
            qps[nqp].copy = cp; qps[nqp].orig = vt; nqp++;
            *obj = cp;
        }
        pthread_mutex_unlock(&vmu);
        note("IOCreatePlugInInterfaceForService ok");
    }
    return kr;
}
__attribute__((used, section("__DATA,__interpose"))) static struct { void *n, *o; } interposers[] = {
    {(void *)my_IOCreatePlugIn, (void *)IOCreatePlugInInterfaceForService},
};
