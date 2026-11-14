#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/syscall.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

// Wine obtains Windows wall-clock time through these Linux calls. Keep Proton
// startup and every other process on the real clock; activate only after the
// main D2R PE image is mapped in the D2R process.
static time_t game_offset;
static int game_clock_active;

static void report(const char *message) {
    const char *path = getenv("D2R_ELF_PATCH_LOG");
    if (!path || !*path) return;
    FILE *out = fopen(path, "a");
    if (!out) return;
    fprintf(out, "pid=%ld %s\n", (long)getpid(), message);
    fclose(out);
}

int clock_gettime(clockid_t id, struct timespec *out) {
    int result = (int)syscall(SYS_clock_gettime, id, out);
    if (!result && (id == CLOCK_REALTIME || id == CLOCK_REALTIME_COARSE) &&
        __atomic_load_n(&game_clock_active, __ATOMIC_ACQUIRE)) {
        out->tv_sec += game_offset;
    }
    return result;
}

int gettimeofday(struct timeval *out, void *tz) {
    int result = (int)syscall(SYS_gettimeofday, out, tz);
    if (!result && __atomic_load_n(&game_clock_active, __ATOMIC_ACQUIRE)) {
        out->tv_sec += game_offset;
    }
    return result;
}

time_t time(time_t *out) {
    struct timespec now;
    if (syscall(SYS_clock_gettime, CLOCK_REALTIME, &now)) return (time_t)-1;
    time_t result = now.tv_sec;
    if (__atomic_load_n(&game_clock_active, __ATOMIC_ACQUIRE)) result += game_offset;
    if (out) *out = result;
    return result;
}

static bool is_primary_game_process(void) {
    FILE *cmdline = fopen("/proc/self/cmdline", "r");
    if (!cmdline) return false;
    char name[4096] = {};
    bool read = fread(name, 1, sizeof(name) - 1, cmdline) > 0;
    fclose(cmdline);
    if (!read) return false;
    const char *base = name;
    const char *slash = strrchr(name, '/');
    const char *backslash = strrchr(name, '\\');
    if (slash) base = slash + 1;
    if (backslash && backslash + 1 > base) base = backslash + 1;
    return strcasecmp(base, "D2R.exe") == 0;
}

static uint16_t u16(const unsigned char *p) {
    return (uint16_t)p[0] | ((uint16_t)p[1] << 8);
}
static uint32_t u32(const unsigned char *p) {
    return (uint32_t)u16(p) | ((uint32_t)u16(p + 2) << 16);
}

// Wine can remap D2R.exe through a memfd. Identify the primary x64 PE image
// from its mapped header instead of relying on the /proc/maps filename.
static uintptr_t game_base(void) {
    FILE *maps = fopen("/proc/self/maps", "r");
    if (!maps) return 0;
    int memory = open("/proc/self/mem", O_RDONLY | O_CLOEXEC);
    if (memory < 0) {
        fclose(maps);
        return 0;
    }
    char line[1024], perms[5];
    uintptr_t found = 0;
    while (fgets(line, sizeof(line), maps)) {
        unsigned long start = 0, end = 0, offset = 0;
        if (sscanf(line, "%lx-%lx %4s %lx", &start, &end, perms, &offset) != 4 ||
            perms[0] != 'r' || offset != 0 || end - start < 4096 || start < 0x100000000UL)
            continue;
        unsigned char header[4096];
        if (pread(memory, header, sizeof(header), (off_t)start) != sizeof(header))
            continue;
        const unsigned char *p = header;
        if (u16(p) != 0x5a4d) continue;
        uint32_t nt = u32(p + 0x3c);
        if (nt > 0x400 || nt + 0x80 > 4096 || u32(p + nt) != 0x4550 ||
            u16(p + nt + 4) != 0x8664 ||
            (u16(p + nt + 22) & 0x2000) != 0 ||
            u16(p + nt + 24) != 0x20b ||
            u32(p + nt + 80) < 0x1000000)
            continue;
        if (found && found != (uintptr_t)start) {
            found = 0;
            break;
        }
        found = (uintptr_t)start;
    }
    close(memory);
    fclose(maps);
    return found;
}

static void *worker(void *unused) {
    (void)unused;
    const char *value = getenv("D2R_CAMPING_CLOCK_OFFSET_SECONDS");
    if (!value || !*value) return NULL;
    errno = 0;
    char *end = NULL;
    long long seconds = strtoll(value, &end, 10);
    if (errno || !end || *end || seconds > 0 || seconds < -315360000LL) {
        if (is_primary_game_process()) report("invalid offline clock offset; no adjustment applied");
        return NULL;
    }
    for (int i = 0; i < 600; ++i) {
        if (is_primary_game_process() && game_base()) {
            game_offset = (time_t)seconds;
            __atomic_store_n(&game_clock_active, 1, __ATOMIC_RELEASE);
            char message[120];
            snprintf(message, sizeof(message), "clock shift active; seconds=%lld", seconds);
            report(message);
            return NULL;
        }
        usleep(100000);
    }
    if (is_primary_game_process()) report("unsupported D2R PE image; no clock shift applied");
    return NULL;
}

__attribute__((constructor)) static void start_worker(void) {
    pthread_t thread;
    if (!pthread_create(&thread, NULL, worker, NULL)) pthread_detach(thread);
}
