// Audit-only stronger baselines; never modifies project implementation.
// Usage: variants {full|stream|parallel|fingerprint|checked}; file stdin, image stdout.
// Same ordinal semantics as linkjob ref. Like that reference this trusts the job
// and assumes relocations grouped by unit. Overlaps within a unit preserve order.
// full = original whole-image memcpy + patch shape.
// stream = one reusable max-unit buffer, emit each aligned unit; no giant image.
// parallel = independent units, direct image output, with binary-searched ranges.
// fingerprint = full's output path, resolving the same 64-bit fingerprints by
// scalar open addressing (collision probing, not fixed rounds; no string work).
// checked = full, plus explicit in-unit bounds check at each dword store.
#define _GNU_SOURCE
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <unistd.h>
#include <omp.h>

static void put(const void *p, size_t n) {
    const unsigned char *q = p;
    while (n) { ssize_t w = write(1, q, n); if (w <= 0) _exit(1); q += w; n -= w; }
}
static uint32_t lower(const uint32_t *a, uint32_t n, uint32_t key) {
    uint32_t lo = 0, hi = n;
    while (lo < hi) { uint32_t mid = lo + (hi-lo)/2; if (a[mid] < key) lo=mid+1; else hi=mid; }
    return lo;
}
static uint64_t mix(uint64_t v) {
    v ^= v >> 30; v *= UINT64_C(0xbf58476d1ce4e5b9);
    v ^= v >> 27; v *= UINT64_C(0x94d049bb133111eb);
    return v ^ (v >> 31);
}
static void patch(unsigned char *out, uint32_t base, uint32_t start, uint32_t end,
                  const uint32_t *roff, const uint32_t *rimp, const uint32_t *addr) {
    for (uint32_t k = start; k < end; ++k) {
        uint32_t v = addr[rimp[k]] - (base + roff[k]);
        memcpy(out + roff[k], &v, 4);
    }
}
int main(int argc, char **argv) {
    if (argc != 2) return 2;
    int stream = !strcmp(argv[1], "stream"), parallel = !strcmp(argv[1], "parallel");
    int fingerprint = !strcmp(argv[1], "fingerprint");
    int checked = !strcmp(argv[1], "checked");
    if (!stream && !parallel && !fingerprint && !checked && strcmp(argv[1], "full")) return 2;
    struct stat st;
    if (fstat(0, &st) || st.st_size < 20) return 1;
    const uint32_t *f = mmap(0, st.st_size, PROT_READ, MAP_PRIVATE, 0, 0);
    if (f == MAP_FAILED) return 1;
    uint32_t U=f[0], NE=f[1], NI=f[2], NR=f[3];
    const uint32_t *size=f+5, *uexp=size+U, *exunit=uexp+U, *exoff=exunit+NE;
    const uint32_t *imu=exoff+3ull*NE, *imo=imu+NI, *ru=imu+4ull*NI, *ro=ru+NR, *ri=ro+NR;
    const unsigned char *code=(const unsigned char *)(ri+NR);
    uint32_t *base=calloc((size_t)U+1,4), *in=calloc((size_t)U+1,4), *ex=calloc((size_t)U+1,4);
    uint32_t max=0;
    for (uint32_t u=0;u<U;++u) {
        uint32_t aligned=(size[u]+63)&~63u;
        base[u+1]=base[u]+aligned; in[u+1]=in[u]+size[u]; ex[u+1]=ex[u]+uexp[u];
        if (aligned>max) max=aligned;
    }
    uint32_t *addr=malloc(4ull*NI+4);
    if (!base || !in || !ex || !addr) return 1;
    if (fingerprint) {
        const uint32_t *elo=exoff+NE, *ehi=elo+NE, *ilo=imo+NI, *ihi=ilo+NI;
        size_t capacity=1;
        while (capacity < 2ull*NE) capacity*=2;
        uint32_t *table=calloc(capacity,sizeof *table);
        if (!table) return 1;
        for (uint32_t e=0;e<NE;++e) {
            uint64_t fp=(uint64_t)ehi[e]<<32 | elo[e];
            size_t k=mix(fp)&(capacity-1);
            while (table[k]) {
                uint32_t prev=table[k]-1;
                // Genuine fingerprint identity is ambiguous, unlike a table collision.
                if (elo[prev]==elo[e] && ehi[prev]==ehi[e]) return 9;
                k=(k+1)&(capacity-1);
            }
            table[k]=e+1;
        }
        for (uint32_t i=0;i<NI;++i) {
            uint64_t fp=(uint64_t)ihi[i]<<32 | ilo[i];
            size_t k=mix(fp)&(capacity-1);
            uint32_t e;
            for (;;) {
                if (!table[k]) return 9;
                e=table[k]-1;
                if (elo[e]==ilo[i] && ehi[e]==ihi[i]) break;
                k=(k+1)&(capacity-1);
            }
            addr[i]=base[exunit[e]]+exoff[e];
        }
        free(table);
    } else {
        #pragma omp parallel for if(parallel) schedule(static)
        for (uint32_t i=0;i<NI;++i) {
            uint32_t e=ex[imu[i]]+imo[i]; addr[i]=base[exunit[e]]+exoff[e];
        }
    }
    if (stream) {
        unsigned char *out=malloc((size_t)max+64);
        if (!out) return 1;
        uint32_t k=0;
        for (uint32_t u=0;u<U;++u) {
            memcpy(out,code+in[u],size[u]);
            memset(out+size[u],0,base[u+1]-base[u]-size[u]);
            uint32_t start=k;
            while (k<NR && ru[k]==u) ++k;
            patch(out,base[u],start,k,ro,ri,addr);
            put(out,base[u+1]-base[u]);
        }
    } else {
        unsigned char *out=calloc((size_t)base[U]+64,1);
        if (!out) return 1;
        if (parallel) {
            #pragma omp parallel for schedule(static)
            for (uint32_t u=0;u<U;++u) {
                unsigned char *o=out+base[u];
                memcpy(o,code+in[u],size[u]);
                patch(o,base[u],lower(ru,NR,u),lower(ru,NR,u+1),ro,ri,addr);
            }
        } else {
            uint32_t k=0;
            for (uint32_t u=0;u<U;++u) {
                unsigned char *o=out+base[u];
                memcpy(o,code+in[u],size[u]);
                // Keep original's one pass over runit, without range preprocessing.
                for (;k<NR && ru[k]==u;++k) {
                    if (checked && (size[u]<4 || ro[k]>size[u]-4)) return 9;
                    uint32_t v=addr[ri[k]]-(base[u]+ro[k]); memcpy(o+ro[k],&v,4);
                }
            }
        }
        put(out,base[U]);
    }
    return 0;
}
