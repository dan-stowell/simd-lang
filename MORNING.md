# MORNING: 2026-11-14 00:08 UTC (campaign 1, M3 and M4 reached)

## The oracle number

| | now | this morning (09:20) |
|---|---|---|
| standard-library packages whose own tests pass with every package of the test binary by fc, the runtime included, linked by fclink (`FC_ALL=rt`) | **260 of the 262 with tests** (tests/fc/passing.txt), the runtime's own suite included; the other 2 need cgo | 228, with the runtime's 30 packages by the stock compiler |
| language corpus (tests/fc/lang, differential against stock), wholly by fc | **23 of 23** | 22 of 22, mixed |
| MinIO's own tests, test binaries wholly by fc | **45 of 46 packages pass; cmd fails one test that stock fails too** (fc-minio-rt3.log, tonight's fc) | same count, runtime by stock |
| MinIO binary wholly by fc | **smoke test passes** (25 of 25 starts; one "not live" under load right after a cache drop, not reproduced) | passed, runtime by stock |
| kube-apiserver wholly by fc | builds, smoke test passes | passed, runtime by stock |
| core-seconds per MB of source, MinIO (75.5 MB) | **0.30** on 8 cores (22.8 core-s, cold, everything included); 0.22 on one core (16.4) | 0.41 (31 core-s) |

## The scoreboard (SCOREBOARD.md, regenerated 2026-11-14 00:26-00:34 UTC, idle machine)

Cold builds, one run per cell, fc + fclink with the runtime by fc, every binary checked (smoke test or version
command). Seconds of wall clock (core-seconds):

| project | stock, 8 cores | fair stock, 8 | **fc, 8 cores** | fc, 4 | fc, 2 | fc, 1 | fc this morning, 8 |
|---|---|---|---|---|---|---|---|
| buildifier | 6.7 | 4.9 | **0.8** (3) | 0.8 | 1.2 | 2.0 | 2.4 |
| runsc | 10.9 | 7.3 | **1.2** (6) | 1.4 | 2.4 | 4.0 | 2.8 |
| tailscaled | 48.4 | 26.9 | **2.1** (12) | 2.8 | 4.9 | 8.7 | 4.3 |
| caddy | 33.3 | 20.6 | **2.3** (14) | 3.6 | 6.5 | 12.1 | 4.6 |
| minio | 51.3 | 29.5 | **3.9** (23) | 5.6 | 10.2 | 17.4 | 6.8 |
| kube-apiserver | 74.1 | 52.2 | **4.3** (26) | 6.4 | 11.4 | 21.1 | 7.6 |
| prometheus | 94.2 | 56.5 | **9.3** (50) | 13.1 | 21.2 | 35.8 | 11.1 |

MinIO again, three more cold runs: 3.7, 3.9, 4.2 s; warm page cache 3.4-3.5 s. **The exit test is 3.3 s.**
Against the fair stock row MinIO is 7.6x and kube-apiserver 12x; against stock 13x and 17x.

Two things the table says. (1) **Core-seconds grow with cores**: MinIO costs 16.4 core-s on one core and 22.8 on
eight (kube 20 -> 26, prometheus 35 -> 50). lscpu shows 8 single-thread cores, so it is not hyperthreading as
presented; shared memory bandwidth, the L3 (32 MB for all eight) and lock waits are the candidates, not yet
separated. One core does the whole of MinIO in 17.4 s. (2) prometheus is the outlier: its heap outgrows the
10 GB pool and falls back to 4 kB pages (50 core-s).
TRACE.md still describes the frozen stock-compiler path; fc's phases are in the profile shares below.

## The build (MinIO, cold, 8 cores)

**3.7-4.2 s wall, 21-24 core-s on an idle machine** (3.6-4.1 s was also what the evening's loaded runs gave).
This morning 6.8 s / 31 core-s; the frozen stock-compiler path 11.2 s / 74; fair stock 29.5 s / 150; stock 51.3 s.
A goroutine census during an idle build (stacks every 50 ms): of 8 compile slots about 6 are running fc's own
code; the rest wait for the assembler's child processes (which use the core themselves), for one per-package
lock around type-descriptor generation (about 0.4 of a slot), and for imported packages' checks.

Order-balanced A/B of the evening's changes (cold, 4 runs each, load 4-9, mean core-s; fc-ab-cold-1113.log):
everything on 21.1; arrival-order slots 23.5; two-pass check 24.0; one type-information map per package 22.7;
no huge pages 29.0 (and +1.8 s wall).

What changed since the morning, largest first:
1. **Huge pages for fc's heap** (10 GB pool reserved for the run and put back): kernel time 6.3-7.3 -> 2.4-2.8 s,
   exit 0.7 -> 0.04 s. Without the pool the same build is 6.4-6.8 s.
2. **The runtime by fc, fclink by default**: no go command and no stock compiler in a build (processes: fc, fclink,
   55 runs of the stock assembler).
3. **Biggest-first scheduling**: minio/cmd's check and back end no longer wait in arrival order (it used to finish
   alone, on one or two cores).
4. **Declarations checked once; bodies later, in parallel shards, against the same package** (types.CheckDecls and
   Bodies, 150 lines added to go/types in our GOROOT, driver/gotypes). cmd's 1.3 s serial check is its declarations
   plus an eighth of its bodies; importers and the package see the same objects.
5. **A function's type information in its own small maps** (the package-wide maps were 0.9 GB and the top line of
   the CPU profile): -0.8 s of user CPU.
6. The package graph from import headers, a 128 kB object-writer buffer, sizes of word-shaped types without a map.

Where the 20 core-s go now (profile shares): code generation 42%, bodies check 16%, type descriptors 14%, object
writing 12%, parsing 8%, declarations 8%; kernel 2.7 s of the total. The cores-busy profile is flat from 0.2 s to
the link: the build is CPU-bound, so the remaining distance to 3.3 s is core-seconds.

**Materialization tax** (what fusion still has to remove): 420 MB of objects written and read back for a 126 MB
binary; 57 process starts; 160,710 functions compiled of which the linker keeps 121,936 (21% of the code is dead).

## Bugs found today (each by an oracle, each fixed with a test that now passes)

- An fc-built runtime lost live objects under concurrent marking: stack checks in helpers the stock compiler
  inlines; BP after the runtime's context switches (JOURNAL 12:10).
- `select { case c <- v: default: }` went through selectgo; the runtime's timer send depends on selectnbsend:
  every ticker with a short period deadlocked (found by the time package's tests hanging).
- fclink: go:fipsinfo never written (FIPS self-check); function table out of address order when a unit mixes
  alignments.
- Two data races in fc itself (race-detector build of fc; caddy and buildifier now report none).
- Runtime print of hex and quoted types, stack check of self-recursive runtime functions, FuncPCABIInternal of an
  interface, closure names (the builder's work on the runtime's own tests).

## Every difference of the dev build

- Unoptimized code, no inlining, no DWARF; the binary is 126 MB against stock's 100 MB. In tight loops fc's code is
  about 8x slower than the stock compiler's at -l (index/suffixarray's tests: 387 s against 51 s).
- Escape analysis only in the runtime's packages: everything whose address is taken is on the heap elsewhere.
- Objects can stay alive until their function returns.
- **Skipped tests: tests/fc/skips.txt, 54 lines**: allocation counts that need escape analysis or inlining (most
  fail with the stock compiler at -l too; each line says which), tests that wait on object liveness, and one
  environment skip (crypto/tls's BoGo suite fetches BoringSSL and fails with stock here).
- An fc-built runtime uses the race detector's stack guard (twice the bound): fc's frames are larger.
- A 10 GB pool of explicit huge pages during the build (machine configuration, restored after).
- Two builds of the same source are not byte-identical (same size; which package carries a shared type descriptor
  or generic instance depends on which asks first). Behaviour is what the tests check.
- Assembly files go through the stock assembler (55 processes for MinIO).
- Generic functions are compiled once per type argument and named by it (`f[uint64]`), where the stock compiler
  compiles per shape (`f[go.shape.uint64]`): tracebacks and profiles show different names (one runtime/pprof test
  is skipped partly for this). Package-level closures are named `pkg.glob..funcN`, stock's are `pkg.init.funcN`.

## Ideas tried and killed, or measured and parked (IDEAS.md)

- Scheduler overhead of per-function jobs: not in the profile; nothing to do.
- Graph discovery from import headers: fills the idle start (4 -> 7 cores in the first 200 ms) but the build is
  CPU-bound; no measurable wall gain. Kept for the mechanism.
- Compile only reachable functions: ceiling about 2 core-s (21% of code is dead); parked behind M5.
- Per-function scratch reuse (7.5 GB allocated per build): parked.

## Decisions that are Dan's

See STATUS.md: huge pages on the demo machine; which classes of skipped tests should block; the doubled stack
guard; the kill test that was above its threshold at M2.
