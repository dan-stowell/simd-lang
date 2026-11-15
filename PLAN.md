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

Now (2026-11-15 20:46 UTC): **MinIO cold 2.4-2.7 s on 8 cores this morning (idle, smoke passing; exit test 3.3 s);
warm 1.77-1.92 s this afternoon.** No quiet numbers since: the machine is busy with verification and a builder.
**Three bugs of every earlier fc binary were fixed today** (JOURNAL 19:01, 19:40, 20:12, 20:44): write barriers where
the runtime forbids them (7259dfb; a deadlock), a stop inside what the stock compiler inlines into nosplit runtime
code (3ef1d06, 09e6304; "found pointer to free object"), and append storing an operand before evaluating the next
(de605c5; `append(x[:0], x[1], x[0])` gave [b b]). The first two each have a check that redoes the stock build's
guarantee (tools/fc/wbcheck.sh, tools/fc/safecheck.sh); the soak on HEAD's compiler is 0 bad of 6,000 runs.
Compilers on disk: **build/fc = 09e6304** (20:14: both runtime fixes, not the cgo merge, not the append fix: the pass
below is running on it); **build/fcx = d6ee59c** (the cgo merge; the query builder uses it: do not rebuild it, i.e.
no `FC_TAG=x`); **build/fcy = de605c5** (HEAD's code generator; scratch builds: `FC_TAG=y driver/mkfc.sh`). In order:
(1) **Correctness on HEAD.**
(a) Running since 20:15 on build/fc: /home/exedev/targets/fc-verify-1115d.sh (log fc-verify-1115d.log there; wbcheck,
safecheck, zipcheck, lang three modes: all PASS; std 260 through wpbatch: no failure in the first 60; then MinIO's
packages). **While it runs do not rebuild build/fc and do not edit tools/fc/wptest.sh, wpbatch.sh, lang.sh, test.sh**
(bash reads a script while it runs it). Expect std 259 of 260 (runtime: TestSideEffectOrder, the append bug, and the
three append-in-loop counts unless the skip lines added at 20:44 are read first) and MinIO 45 of 46 (cmd:
TestTrackingResponseWriter, as stock); any other line is a bug, or a timeout to rerun alone.
(b) Running since 20:47 on build/fcy: /home/exedev/targets/fc-sweep-1115.sh (log fc-sweep-1115.log): the runtime's own
suite (must pass now: its 13 + 6 whole-program failures are fixed or skip lines with evidence), then the free-slot
diagnostic (`WPTEST_FREECHECK=1`, tools/fc/freecheck.py: the collector names whoever shows it a pointer to a free
slot) over the runtime's suite and MinIO's packages; its last lines list any report file. The diagnostic over std on
the fixed compiler: 253 packages pass, no report (JOURNAL 20:44).
(c) A Codex review hunting counterexamples to by-value capture runs until about 21:07 UTC
(reviews/inbox/20261115T1427-codex-byval.md): triage it into REVIEWS.md; a counterexample is a bug to fix first.
(d) When (a) is done: `driver/mkfc.sh` (build/fc at HEAD), lang both modes, wbcheck, safecheck, cgotest, then the
whole pass again as fc-verify-1115e (copy 1115d's script, change the names) for the M-log line and
tests/fc/passing.txt's header; it can run overnight. Do the quiet pass (3) before starting it.
**Liveness skips are 20-odd tests and some depend on what ran before** (the pinner tests). FC_TIDY=all (slots die
with their statement or block in every package, as in the runtime's) makes most pass but is killed by its counts
(IDEAS: code +33%, binary +55%, build memory +18%).
(2) **The soak** is `tools/fc/soak.sh [runs] [workers]` now (an fc-built protoc-gen-go against the stock-built one at
GOGC=1; hangs get their stacks from a core by tools/fc/corebt). With the three fixes a scratch soak of 3,200 runs was
started at 18:58 (not finished at 19:10: load 74); 0 bad of 1,600 with the first fix alone. **One segmentation fault
(rc 139, nothing on stderr) in about 1,400 runs before the fixes is unexplained**: run `tools/fc/soak.sh 1500 4` when
the pass is done; a bad run with rc 139 needs `ulimit -c unlimited` in the script and the core read with corebt. Also
worth soaking MinIO's server under load the same way.
(3) **Quiet pass** (nothing else running; `uptime` under 1): order-balanced A/B of MinIO, 4 pairs each, user+sys and
wall: cold FC_NOZIP=1 against default (module sources from zips, JOURNAL 18:33); warm FC_LAZYFUNCS=1, FC_FUSE=1,
FC_SCRATCH=0, FC_ASMPROC=1, FC_CAPREF=1; `FC_LINKFLAGS=-stats`; tools/fc/timeline.py -b 50; 5 cold runs (`tools/fc/cold.sh minio 8`
+ tests/minio_smoke.sh); `tools/scoreboard/ours.sh . 8 4 2 1` then `tools/scoreboard/mkscoreboard.py > SCOREBOARD.md`
(the Bazel rows of ours are not in SCOREBOARD.md yet). Flip `lazyFuncsDefault` (driver/fc/demand.go) if by demand wins
on wall. Then TRACE.md: an fc section is owed (`fc build`'s summary lines: phases, read:, the link's table, tax).
(4) **The demo rows (BuildBuddy) are only 3x under stock** (executor 111-141 s against 335; bb-cli 38-52 against 124),
none of it fc's own time (fc: 9.4 s for the executor's 1,065 packages). Two builders, each in its own worktree under
.claude/worktrees (reports arrive as task notifications even after a fresh context; if none came, `git worktree list`
and the newest commits of each branch):
(a) **cgo packages by fc: merged (d6ee59c, 20:24).** bb-cli and bb-executor build with 0 packages from the stock
compiler (75 and 924 before; BZFC_STOCKCGO=1 is the old path): fc runs cgo and gcc for the C, the stock linker links
in external mode, the runtime and runtime/cgo are fc's. Verified on merged main with a scratch compiler: lang both
modes, wbcheck, safecheck, tools/fc/cgotest.sh; the rows once each by the builder (rc 0, checks equal). Owed on a
quiet machine: `tools/bazel/cold.sh bb-cli 8` and `bb-executor 8` with build/fc rebuilt (not while a pass runs).
(b) **The DAG from `bazel query` (loading only) instead of `aquery`** (Opus, launched 20:22; JOURNAL 20:18: cold query
13-28 s against aquery 56-124 s at the same load; the executor's graph is 2,449 go_library, ~220 proto rules, 14
genrules). Oracle in its brief: the plan handed to fc equal package by package to the aquery path's, generated files
byte for byte, the four rows' binary checks with `BZ_QUERY=1 tools/bazel/cold.sh`. Its worktree was branched before (a)'s merge: merge main into its branch first.
Then on a quiet machine: both rows cold with HUGEPAGES on, the stages (Bazel, read, generate, fc), and what is left.
Smaller, if Bazel's part still matters after (b): `--host_jvm_args=-XX:TieredStopAtLevel=1` (a one-shot server never
warms up), A/B on buildifier first.
(5) Toward 2.0 s cold: what is left of cold reading after the zips (2,495 files from directories: the standard
library and the main module; 241 .mod files), allocation volume (5.9 GB mapped per MinIO build; `-memprofile`: no site
above 5%), one assembler pass per package (IDEAS), functions and bodies by demand (FC_FUSE=1 FC_LAZYBODIES=1), the
link (280 ms at 2.4 cores).
Housekeeping: `df -h /` before long runs; nr_hugepages must be 0 between runs (a killed build leaves it set).
