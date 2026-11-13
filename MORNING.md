# MORNING: 2026-11-13 23:55 UTC (campaign 1, M4 reached, M3 two packages short)

## The oracle number

| | now | this morning (09:20) |
|---|---|---|
| standard-library packages whose own tests pass with every package of the test binary by fc, the runtime included, linked by fclink (`FC_ALL=rt`) | **258 of the 262 with tests** (tests/fc/passing.txt); runtime and runtime/pprof are being finished (9 + 1 tests failed this evening; fixes merged, rerun in progress); 2 need cgo | 228, with the runtime's 30 packages by the stock compiler |
| language corpus (tests/fc/lang, differential against stock), wholly by fc | **23 of 23** | 22 of 22, mixed |
| MinIO's own tests, test binaries wholly by fc | **45 of 46 packages pass; cmd fails one test that stock fails too** (fc-minio-rt2.log; rerun with tonight's fc: fc-minio-rt3.log) | same count, runtime by stock |
| MinIO binary wholly by fc | **smoke test passes** (25 of 25 starts; one "not live" under load right after a cache drop, not reproduced) | passed, runtime by stock |
| kube-apiserver wholly by fc | builds, smoke test passes | passed, runtime by stock |
| core-seconds per MB of source, MinIO (75.5 MB) | **0.27** (20 core-s, everything included) | 0.41 (31 core-s) |

## The build (MinIO, cold, 8 cores)

**3.6-4.1 s wall, 20-23 core-s**, measured at load 4-5 (a builder's test runs shared the machine all evening;
nothing was measured idle). This morning's idle scoreboard figure was 6.8 s / 31 core-s; the frozen stock-compiler
path is 11.2 s / 74; fair stock 29.5 s / 150; stock 51.3 s. The exit test is 3.3 s.
**SCOREBOARD.md's fc column and TRACE.md are still this morning's**: rerun `tools/scoreboard/ours.sh` on an idle
machine before quoting any corpus number.

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
- **Skipped tests: tests/fc/skips.txt, 53 lines**: allocation counts that need escape analysis or inlining (most
  fail with the stock compiler at -l too; each line says which), tests that wait on object liveness, and one
  environment skip (crypto/tls's BoGo suite fetches BoringSSL and fails with stock here).
- An fc-built runtime uses the race detector's stack guard (twice the bound): fc's frames are larger.
- A 10 GB pool of explicit huge pages during the build (machine configuration, restored after).
- Two builds of the same source are not byte-identical (same size; which package carries a shared type descriptor
  or generic instance depends on which asks first). Behaviour is what the tests check.
- Assembly files go through the stock assembler (55 processes for MinIO).

## Ideas tried and killed, or measured and parked (IDEAS.md)

- Scheduler overhead of per-function jobs: not in the profile; nothing to do.
- Graph discovery from import headers: fills the idle start (4 -> 7 cores in the first 200 ms) but the build is
  CPU-bound; no measurable wall gain. Kept for the mechanism.
- Compile only reachable functions: ceiling about 2 core-s (21% of code is dead); parked behind M5.
- Per-function scratch reuse (7.5 GB allocated per build): parked.

## Decisions that are Dan's

See STATUS.md: huge pages on the demo machine; which classes of skipped tests should block; the doubled stack
guard; the kill test that was above its threshold at M2.
