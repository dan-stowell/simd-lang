# PLAN

The campaign is chosen by the user. Sessions **do not edit** the Campaign, Exit test, Kill
test or Rules sections; they append to Milestones and keep the Now line current. A session
cannot switch campaigns. If you think the campaign is wrong, write the evidence in STATUS.md
under "Decisions for Dan" and keep working on it.

## Campaign 1 (from 2026-11-12): our own compiler for whole packages, fused

The stock-compiler path is **frozen** at its 2026-11-12 result: MinIO cold 11.2 s median,
kube-apiserver 19.8-21.3 s (configuration `13-noweakptr`; MORNING.md, TRACE.md). It is the
baseline to beat and the per-package fallback. No more work goes into making it faster.

Why this campaign: the 2026-11-12 CPU table says fusing the stock compiler's work removes at
most about 27 of 74 core-seconds; what is left (~47 core-s, 5.9 s on 8 cores) is the stock
compiler's own work. Two seconds needs the compiling itself to be about three times cheaper:
whole packages compiled without the stock front end, with no export-IR round trip, no
`obj.Prog` assembler path and no separate linker. `05-sbc` showed that taking single
functions does not pay; whole packages leaving the stock compiler is the seam that does.

How is yours to decide: whole-program type checking once in one address space (go/types or
your own), a lowering of your own, direct emission (the baseline generator's knowledge of
frames, stack maps, write barriers and the calling convention is reusable), and the F4 link
spike in `explore/f4link` as the place code lands. Stock fallback per package for anything
not yet handled.

**The trace may stay flat or get worse for days during this campaign.** That is expected and
not a reason to retreat. Progress is the oracle number below.

## Oracle number (moves hourly; report it at every milestone)

1. **Packages compiled entirely by our compiler, whose tests pass**: standard-library packages
   first (their own `go test` with the binary built our way, plus the differential suite
   against stock), then corpus packages (MinIO first). Report `N of M` per corpus project.
2. **Core-seconds per MB of source** on the packages we compile, next to the stock baseline
   mode's number on the same packages.

## Kill test (written before work began)

When the first 50 standard-library packages compile our way and pass, measure (2). If our
compiler costs more than **one third** of the stock baseline mode's core-seconds on the same
packages, with no measured path to one third, stop the campaign: write up why in JOURNAL.md
and STATUS.md, and ask for the next campaign under "Decisions for Dan". Only this measurement
ends the campaign early; time, usage and session ends never do.

## Exit test

A fresh cold MinIO build (CHARTER.md's cold rule) in **3.3 s wall or less on 8 cores** (an
order of magnitude under the fair stock row's 32.5 s), binary passing MinIO's tests, with
the corpus scoreboard showing the same technique on every corpus project.

## Rules

- Commit and push every working step, with tests for what it touches, so an abrupt end loses
  nothing.
- Use subagents as builders: split work into oracle-checked parts, one git worktree each.
- The 8-vCPU VM is the machine (decided by the user). Scaling is reported at 1, 2, 4 and 8 cores.

## Milestones

M0. **Corpus scoreboard** (bounded: one day at most; then back to the compiler even if parts are
    missing). Pin and fetch the corpus in CHARTER.md; one script builds each cold, stock and fair
    stock, at 1, 2, 4 and 8 cores, and records wall clock, critical path and core-seconds in
    `SCOREBOARD.md`. Bazel projects build from their Bazel DAG (`bazel aquery`), generated code
    included. Our column starts as "stock fallback" and fills in as the campaign lands.
M1. Our compiler compiles a first standard-library package end to end, linked into a binary that
    passes that package's tests.
M2. 50 standard-library packages: run the kill test.
M3. The whole standard library and runtime our way.
M4. MinIO entirely our way, passing its tests.
M5. No separate linker: code emitted into the image (F4).
M6. The exit test; the corpus scoreboard filled in.

Log (append one line per milestone reached, with the oracle number):

- M0, 2026-11-12: SCOREBOARD.md: every corpus project cold, stock and fair stock (and Bazel from its own DAG),
  at 1/2/4/8 cores (`tools/scoreboard/run.sh`, `mkscoreboard.py`). One run per cell; fc builds overlapped on
  cores 4-7 during parts of the 8-core pass (rerun `tools/scoreboard/run.sh . 8` on a quiet machine before quoting).
- M1, 2026-11-12 19:57 UTC: `unicode/utf8` compiled entirely by fc (driver/fc) passes its own tests in a binary
  whose other packages are stock-compiled (`tools/fc/test.sh unicode/utf8`; one allocation-count test skipped,
  tests/fc/skips.txt). Oracle: standard library 1 package; corpus 0. Core-seconds per MB: not measured yet (M2).
- M2, 2026-11-12 23:23 UTC: **205 standard-library packages** compiled entirely by fc pass their own tests in mixed mode
  (tests/fc/passing.txt; 53 compile but fail, with Codex triaging; 91 have no tests), and all 21 packages of the
  language corpus (tests/fc/lang). Corpus: MinIO builds and links in whole-program mode (1,283 packages by fc, the
  runtime's 30 from stock) but does not run yet. **Kill test measured** (`tools/fc/cost.sh`, the 205 packages, 8.23 MB):
  stock baseline mode 6.18 core-s (0.75 per MB); fc 2.80 core-s attributable (0.34 per MB), ratio **0.45**: above one
  third. Not stopped, because the path is measured (JOURNAL): the profile is flat with named items, and on MinIO the
  whole fc build is 26-27 core-s against the frozen path's 74.
- M4 in part, 2026-11-13 03:43 UTC: **MinIO built by fc passes its smoke test, cold**: 1,283 packages by fc in one process,
  the runtime's 29-30 packages by the stock compiler inside the timer, planning by the driver's own planner,
  stock link: 7.7-8.7 s wall, 34-36 core-s on a machine two other agents were using (`tools/fc/cold.sh minio`;
  frozen stock path 11.2 s, fair stock 29.5 s / 150 core-s). kube-apiserver the same way: 8.7-9.1 s, 42-43 core-s,
  smoke passes (frozen path 19.8-21.3 s). buildifier, runsc, caddy and tailscaled build, link and run `--version`
  (`tools/fc/corpus.sh`); prometheus stops in one generic instance. Not M4 yet: MinIO's unit tests have not been
  run against fc-built packages, and the runtime is stock (M3).
- M4 in part, 2026-11-13 05:00 UTC: **MinIO's own tests under fc: 45 of 46 packages pass; the 46th (cmd) fails one
  test that fails with the stock compiler too** (package under test by fc; the all-packages-by-fc run,
  `FC_ALL=1 tools/fc/test.sh`, is in /home/exedev/targets/fc-minio-all.log). std: 228 packages pass. Escape analysis
  (driver/fc/escape*.go) and fclink (driver/fclink) are merged into main, not yet used by `fc build`.
- M3 in part, 2026-11-13 10:40 UTC: **a program compiled entirely by fc, the runtime included, and linked by
  fclink runs**: hello and three of tools/fclink/progs match stock output; MinIO built that way (1,313 fc objects,
  no stock archive) passes its smoke test (`FC_RUNTIME=1`, off by default). Not M3: with an fc-built runtime the
  collector loses live objects when it marks concurrently on more than two CPUs (deterministic reproduction:
  reflect calls under allocation pressure); `FC_ALL=rt tools/fc/lang.sh` (test binaries wholly by fc) is 17-19 of 23.
  SCOREBOARD.md's fc column is filled (cold, idle machine, fc + fclink, stock-compiled runtime): MinIO 6.8 s on 8
  cores, before the two-pass front end (warm 5.5 -> 4.8 s).
- M3 nearly, 2026-11-13 13:30 UTC: **std with test binaries built wholly by fc, runtime included, linked by fclink
  (`FC_ALL=rt tools/fc/test.sh`): 221 of 226 packages pass their own tests**; the language corpus 23 of 23 in that
  mode; MinIO and kube-apiserver built that way pass their smoke tests. The lost-object bug had two causes (JOURNAL
  12:10): stack checks in helpers the stock compiler inlines, and BP after the runtime's context switches.
- **M4, 2026-11-13 23:45 UTC: MinIO entirely our way passes its tests.** Every Go package by fc, the runtime included,
  linked by fclink (`fc build`'s defaults now; no go command in the build; assembly by the stock assembler): MinIO's own
  tests with wholly-fc test binaries: 45 of 46 packages pass and cmd fails one test that stock fails too
  (fc-minio-rt2.log, fc-minio-cmd-flake.log); smoke test passes (25 of 25 starts). Cold, 8 cores, machine shared with a
  builder (load 4-5): **3.6-4.1 s wall, 20-23 core-s** (this morning 6.8 s; frozen stock path 11.2 s; exit test 3.3 s).
  What moved it: huge pages for fc's heap (-8 core-s), biggest-first slots, declarations checked once with bodies in
  shards (go/types CheckDecls/Bodies), a function's type information in its own maps. The build is CPU-bound now
  (6-7 of 8 cores busy from 0.2 s to the link).
- M3 nearly, 2026-11-13 23:45 UTC: **std wholly by fc: 258 of the 262 packages with tests pass** (tests/fc/passing.txt;
  skips with evidence in tests/fc/skips.txt). Left: runtime (9 tests) and runtime/pprof with a builder; runtime/cgo and
  internal/runtime/cgobench need cgo (not buildable with stock either at CGO_ENABLED=0). Bugs found by the last 36:
  one-case select through selectgo (tickers deadlocked), fclink's function table order with mixed alignment,
  go:fipsinfo, two data races in fc.
- **M3, 2026-11-14 00:08 UTC: the standard library and the runtime our way.** With every package of the test binary
  compiled by fc, the runtime included, and linked by fclink (`FC_ALL=rt tools/fc/test.sh`): **260 of the 262 std
  packages that have tests pass their own tests** (tests/fc/passing.txt), the runtime's own suite among them (284 s).
  The two left need cgo (runtime/cgo, internal/runtime/cgobench: not buildable by stock either at CGO_ENABLED=0).
  54 skip lines (tests/fc/skips.txt): allocation counts and inlining assertions (most fail with stock at -l too),
  liveness waits, one environment skip. Language corpus 23 of 23. MinIO's own tests rerun with this fc: 45 of 46,
  cmd as stock (fc-minio-rt3.log). Oracle 2: MinIO 20-23 core-s for 75.5 MB (0.27-0.30 per MB).

Now (2026-11-16 01:12 UTC): **MinIO warm 2.1-2.3 s on 8 cores with the same bytes from every build (one core of the
machine taken by another agent's browser); cold not measured on this HEAD yet (11-15 morning: 2.4-2.7 s; HEAD before
tonight's fixes: 3.0-3.6 s, over the exit test's 3.3 s twice in five).** Compilers on disk: **build/fc = HEAD
(5acfc98: functions by demand, shards, built 01:06)**; build/fc-5b23b5d (11-15 21:44), fc-part1 (before the shards),
fc-part2a: for A/B, deletable; build/fcw, fcx: scratch (`FC_TAG=w driver/mkfc.sh`; a build piped into tail needs
`set -o pipefail`). The session of 11-15 died in the kernel's OOM killer (JOURNAL 11-16 00:40): **every heavy run goes
through tools/memlimit.py** (LOOP "VM safety"). In order:
(1) **Correctness on HEAD.** Running since 01:07 on build/fc: /home/exedev/targets/fc-verify-1116a.sh (log
fc-verify-1116a.log there; every stage in its own memory-limited service): wbcheck, safecheck, zipcheck, cgotest, gotest
(all recipes; then 16 at once), lang in four ways, std 260 through wpbatch, MinIO's packages, the corpus builds with
their smoke tests, samebytes on three projects, gotest three more rounds. **While it runs do not rebuild build/fc and
do not edit tools/fc/{wptest,wpbatch,lang,test,corpus,cgotest,samebytes}.sh or gotest.py.** Expect: gotest 1,251 tests:
PASS 1209 FAIL 0 NOFC 0; lang 26 of 26 but FC_ESC=1: 25 (closures/TestCleanupSeesCapture: JOURNAL 00:40); std 260 of
260; MinIO 45 of 46 (cmd: TestTrackingResponseWriter, as stock); samebytes equal three times. A stage that prints
"rc N" without a FAIL line was killed (OOM or timeout): rerun it alone. It is the first pass in a service's
environment (soft open-file limit 1024, no terminal) and the first with functions by demand in every test binary:
new failures are likely one of those two; FC_EAGERFUNCS=1 tells them apart. When the whole pass is clean: the M-log
line (std, MinIO, gotest counts) and tests/fc/passing.txt's header. Then `tools/fc/soak.sh 1500 4` (0 bad of 6,000 on
de605c5; today's fixes add stores and nil checks everywhere), through memlimit.
(2) **Cold numbers of HEAD** (after (1): nothing else running; another agent's headless-shell may still hold a core:
say so): 5 cold runs (`tools/fc/cold.sh minio 8` + tests/minio_smoke.sh), and order-balanced pairs against
build/fc-5b23b5d as /home/exedev/targets/quiet-1116/run.sh does them (copy it; its `pairs` function). Then
`tools/scoreboard/ours.sh . 8 4 2 1` and `tools/scoreboard/mkscoreboard.py > SCOREBOARD.md` (the Bazel rows of ours are
not in SCOREBOARD.md yet), and TRACE.md: an fc section is owed (`fc build`'s summary lines: phases, read:, shards:, the
link's table from FC_LINKFLAGS=-stats, tax). Then the milestone steps of LOOP.md (STATUS, MORNING, reviewers).
(3) **The shards' remaining cost: +0.19 s wall, +1.1 core-s on MinIO** (JOURNAL 01:10). Measured ways in: the tail
(`shards:` line: 70-90 ms after the last package: equality and hash functions compiled, then 64 NewUnit calls of
0.5 core-s together: overlap them with the last package's back end by finishing a shard as soon as no package that
can still ask is running, or split minio/cmd's tail); symbols looked up in two compilers (LookupABIInit 0.39 -> 0.92
core-s in profiles, claim 0.11 -> 0.31): hand the shard the asker's name string and hash, or give each asker a
per-compiler map from types.Type to the shard's symbol. A/B with FC_SHARDS=0, strict rotation, 6 rounds.
(4) **Gotest beyond this GOROOT** (one Opus builder): the same harness over the regression programs of older Go
releases that this GOROOT dropped, and a random-program differential (csmith-style) if those dry up. And the class the
vfork bug belongs to: code that only works because the stock compiler keeps nothing in the frame across a call
(runtime and syscall functions marked nosplit/norace/"must not split": audit fc's temporaries across calls there).
(5) **The demo rows (BuildBuddy) are only 3x under stock** (executor 111-141 s against 335; bb-cli 38-52 against 124),
none of it fc's own time (fc: 9.4 s for the executor's 1,065 packages). The DAG from `bazel query` (loading only) is
merged (`BZ_QUERY=1 tools/bazel/cold.sh <row> 8`; same plans as the aquery path on all four rows; no timings yet).
Owed on a quiet machine: `tools/bazel/cold.sh bb-cli 8`, `bb-executor 8`, then both with BZ_QUERY=1, cold, HUGEPAGES
on; the stages (Bazel, read, generate, fc), and what is left. Make BZ_QUERY=1 the default if it is faster on every
row. Smaller: `--host_jvm_args=-XX:TieredStopAtLevel=1`, A/B on buildifier first.
(6) Toward 2.0 s cold: what is left of cold reading after the zips (2,495 files from directories; the graph is known
at 173-184 ms warm, 370 ms cold), allocation volume (6.4 GB mapped per MinIO build), type descriptors written while
generating code, one assembler pass per package (assembler: 170 runs, 666-691 ms of CPU, 227-268 of it symbol ABI
passes), bodies by demand (FC_LAZYBODIES=1), the link (163-193 ms: resolve 39, deadcode 44, layout 20, write 52).
Housekeeping: `df -h /` before long runs (25 GB free at 01:07 after `go clean -cache` of targets/fc-gocache, which
grows by gigabytes a day); nr_hugepages must be 0 between runs (a killed build leaves it set); no builder worktrees
are live; never `pkill -f`; the escape analysis everywhere (FC_ESC=1) stays off (+1.1 core-s, code no smaller).
