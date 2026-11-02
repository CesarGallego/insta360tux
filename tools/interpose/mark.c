// mark <etiqueta...>: añade una línea "ts MARK etiqueta" al log del interceptor.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
int main(int argc, char **argv) {
    if (argc < 2) { fprintf(stderr, "uso: mark <etiqueta>\n"); return 2; }
    const char *p = getenv("INSTA360_XU_LOG");
    if (!p) p = "work/logs/xu.log";
    FILE *f = fopen(p, "a");
    if (!f) { perror(p); return 1; }
    struct timespec ts; clock_gettime(CLOCK_REALTIME, &ts);
    fprintf(f, "%ld.%06ld MARK", (long)ts.tv_sec, ts.tv_nsec / 1000);
    for (int i = 1; i < argc; i++) fprintf(f, " %s", argv[i]);
    fputc('\n', f); fclose(f);
    return 0;
}
