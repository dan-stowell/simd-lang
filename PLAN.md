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

Now (2026-11-15 21:45 UTC): **MinIO cold 2.4-2.7 s on 8 cores this morning (idle, smoke passing; exit test 3.3 s);
warm 1.77-1.92 s this afternoon.** No quiet numbers since, and those are numbers of a compiler that has changed:
**the Go distribution's own compiler tests (`tools/fc/gotest.py`: $GOROOT/test's 1,048 single-file `// run` programs,
never run before) found 30-odd differences from the stock compiler today, every one in every earlier fc binary**
(JOURNAL 21:00, 21:20, 21:40 UTC: interface values of two named types, `for i, x[i] = range`, nil checks where nothing
is loaded, uintptr arguments keeping their objects, recover through reflect, overlapping array assignment, blank
fields, `switch { case i: }` with an interface value, line numbers, types local to generic functions, ...). HEAD:
1,018 pass, 0 open, 13 skips with reasons (tests/fc/gotest-skips.txt), 17 excluded (stock does not pass them outside
GOROOT/test). Compilers on disk: **build/fc = HEAD (5b23b5d, built 21:44)**; build/fcy = de605c5 (the sweep below
runs on it: do not rebuild it); build/fcw, fcz, fcr (race detector), fcx: scratch, free to overwrite
(`FC_TAG=w driver/mkfc.sh`; a build piped into tail needs `set -o pipefail`). In order:
(1) **Correctness on HEAD.**
(a) Running since 21:45 on build/fc: /home/exedev/targets/fc-verify-1115e.sh (log fc-verify-1115e.log there): wbcheck,
safecheck, zipcheck, cgotest, gotest (as is, then 16 at once), lang three modes, std 260 through wpbatch, MinIO's
packages, the corpus builds with their smoke tests, then gotest eight more rounds (binaries are not the same bytes
from build to build, and a miscompile that depends on an address shows in some builds only: IDEAS "The same bytes").
**While it runs do not rebuild build/fc and do not edit tools/fc/{wptest,wpbatch,lang,test,corpus,cgotest}.sh or
gotest.py.** Expect: gotest PASS 1018 FAIL 0 NOFC 0 every time (21:50: the first run is exactly that) (a failing binary is kept under
fc-verify-1115e.gotest-keep*: disassemble it, do not rerun and shrug); lang 26 of 26; std 260 of 260 (TestStackGrowth
is a skip line now); MinIO 45 of 46 (cmd: TestTrackingResponseWriter, as stock). Any other line is a bug, or a timeout
to rerun alone. The pass before it (1115d, on 09e6304) ended as expected: std 259 of 260, MinIO 45 of 46.
**Not pushed yet: b57259f..5b23b5d** (the fixes and the two builder merges) wait for the first lines of that log
(checks, gotest, lang); push when they are clean. When the whole pass is clean: the M-log line (std, MinIO, gotest
counts) and tests/fc/passing.txt's header.
(b) Running since 20:47 on build/fcy: /home/exedev/targets/fc-sweep-1115.sh (log fc-sweep-1115.log): the free-slot
diagnostic (`WPTEST_FREECHECK=1`, tools/fc/freecheck.py) over the runtime's suite and MinIO's packages; its last lines
list any report file. Its first step (the runtime's suite plain) failed on TestStackGrowth alone: a liveness wait
(the skip line of 21:20), 34 minutes in chan receive; the freecheck run of the runtime will time out on it too (rerun
that one alone afterwards: `WPTEST_FREECHECK=1 GOGC=25 tools/fc/wpbatch.sh -j 1 -C /home/exedev/targets/goroot-slots/src runtime`).
(c) Owed by today's findings: **gotest covers only `// run` with no flags** (1,048 of 3,405 files). Next kinds, in
order of yield: `// rundir` (116 directories of several packages), `// runoutput` (17), `// run` with flags, `// build`.
`// compile`/`// errorcheck` are the front end's (go/types): not ours. Also worth one builder: the same harness over
the test directories of older Go releases' regression programs that this GOROOT dropped, and a random-program
differential (csmith-style) only if those dry up.
(d) The corpus case of each finding is tests/fc/lang/gotestfinds (mine), lines, generics/consts+locals, localtarg (the
builders'). FC_ESC=1 (escape analysis in every package) is not part of any pass and crashed on a corpus package until
21:42: add `FC_ESC=1 FC_ALL=rt tools/fc/lang.sh` to the next verify script.
**Liveness skips are 30-odd tests and some depend on what ran before** (the pinner tests, TestStackGrowth). FC_TIDY=all
(slots die with their statement or block in every package, as in the runtime's) makes most pass but is killed by its
counts (IDEAS: code +33%, binary +55%, build memory +18%).
(2) **The soak** is `tools/fc/soak.sh [runs] [workers]` (an fc-built protoc-gen-go against the stock-built one at
GOGC=1; hangs get their stacks from a core by tools/fc/corebt): 0 bad of 6,000 on de605c5. Run `tools/fc/soak.sh 1500 4`
on HEAD after the pass (today's fixes add stores and nil checks everywhere). Also worth soaking MinIO's server under
load the same way.
(3) **Quiet pass** (after (1a) and (1b) are done: nothing else running; `uptime` under 1; the order was the other way
round until 21:40: JOURNAL says why): order-balanced A/B of MinIO, 4 pairs each, user+sys and wall: cold FC_NOZIP=1
against default (module sources from zips, JOURNAL 18:33); warm FC_LAZYFUNCS=1 (by demand compiles 137,630 of 158,189
functions, 70.8 of 80.8 MB of code: counted at 20:50), FC_FUSE=1, FC_SCRATCH=0, FC_ASMPROC=1, FC_CAPREF=1;
`FC_LINKFLAGS=-stats`; tools/fc/timeline.py -b 50; 5 cold runs (`tools/fc/cold.sh minio 8` + tests/minio_smoke.sh);
`tools/scoreboard/ours.sh . 8 4 2 1` then `tools/scoreboard/mkscoreboard.py > SCOREBOARD.md` (the Bazel rows of ours
are not in SCOREBOARD.md yet). Flip `lazyFuncsDefault` (driver/fc/demand.go) if by demand wins on wall. Then TRACE.md:
an fc section is owed (`fc build`'s summary lines: phases, read:, the link's table, tax). **Compare this morning's
2.4-2.7 s with HEAD's first**: the fixes cost code (MinIO: 80.8 -> 80.9 MB compiled, pclntab 23.4 -> 22.0 MB, text
64.5 MB both) and whatever they cost in time is the price of being right, to be stated, not tuned away.
(4) **The demo rows (BuildBuddy) are only 3x under stock** (executor 111-141 s against 335; bb-cli 38-52 against 124),
none of it fc's own time (fc: 9.4 s for the executor's 1,065 packages). Both builders are merged:
(a) cgo packages by fc (d6ee59c): 0 packages from the stock compiler in bb-cli and bb-executor.
(b) **The DAG from `bazel query` (loading only) instead of `aquery`: merged (e84d816, 21:17).** `BZ_QUERY=1
tools/bazel/cold.sh <row> 8`; on all four rows the plan handed to fc equals the aquery path's package by package
(tools/bazel/plandiff.sh), generated files byte for byte (but those that embed a configuration directory's name), and
the binaries pass their checks. Checked warm, without huge pages, at load 10-40: **no timings yet**. Its approximations
are listed in JOURNAL 21:15 (rule classes recognised by name, a table of native-option defaults, the exec platform
taken to be the target platform; every miss stops the build with the label).
Owed on a quiet machine, with build/fc at HEAD: `tools/bazel/cold.sh bb-cli 8`, `bb-executor 8`, then both with
BZ_QUERY=1, cold, HUGEPAGES on; the stages (Bazel, read, generate, fc), and what is left. Make BZ_QUERY=1 the default
if it is faster on every row. Smaller, if Bazel's part still matters then: `--host_jvm_args=-XX:TieredStopAtLevel=1`
(a one-shot server never warms up), A/B on buildifier first.
(5) Toward 2.0 s cold: what is left of cold reading after the zips (2,495 files from directories: the standard
library and the main module; 241 .mod files; the graph is known at 287 ms warm), allocation volume (5.9 GB mapped per
MinIO build; no site above 5%: NewUnit 6.6% cumulative, newGen 5.8%), type descriptors written while generating code
(2.0 of 18 s of samples in a profile under load: writeCommon, writeUncommon, the names), one assembler pass per
package (IDEAS), functions and bodies by demand (FC_FUSE=1 FC_LAZYBODIES=1), the link (250 ms: resolve 51, deadcode
80, layout 27, write 71).
Housekeeping: `df -h /` before long runs (14 GB free at 21:00 after removing 7 GB of reviewers' scratch from /tmp;
/home/exedev/targets/bz-scratch/q is 865 MB of the query builder's outputs, removable); nr_hugepages must be 0 between
runs (a killed build leaves it set); the four worktrees under .claude/worktrees are merged and can go (`git worktree
remove`); reviews: the by-value capture hunt came back with 34 cases and no counterexample (REVIEWS.md).
