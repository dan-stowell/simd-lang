/* Audit-only, one checked whole-process sample with all wait4 resource counters.
 * Usage: timer INPUT EXECUTABLE [ARGS...]; inherit the caller's CPU affinity. */
#define _GNU_SOURCE
#include <fcntl.h>
#include <stdio.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>
static double now(void) {
    struct timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    return t.tv_sec * 1000.0 + t.tv_nsec / 1e6;
}
int main(int argc, char **argv) {
    if (argc < 3) return 2;
    double start = now();
    pid_t p = fork();
    if (p < 0) return 3;
    if (!p) {
        int in = open(argv[1], O_RDONLY), out = open("/dev/null", O_WRONLY);
        if (in < 0 || out < 0 || dup2(in, 0) < 0 || dup2(out, 1) < 0) _exit(125);
        execv(argv[2], argv + 2);
        _exit(127);
    }
    int status;
    struct rusage r;
    if (wait4(p, &status, 0, &r) != p) return 4;
    printf("{\"wall_ms\":%.6f,\"user_ms\":%.6f,\"system_ms\":%.6f,"
           "\"maxrss_kib\":%ld,\"minor_faults\":%ld,\"major_faults\":%ld,"
           "\"wait_status\":%d}\n", now() - start,
           r.ru_utime.tv_sec * 1000.0 + r.ru_utime.tv_usec / 1000.0,
           r.ru_stime.tv_sec * 1000.0 + r.ru_stime.tv_usec / 1000.0,
           r.ru_maxrss, r.ru_minflt, r.ru_majflt, status);
    return WIFEXITED(status) && WEXITSTATUS(status) == 0 ? 0 : 1;
}
