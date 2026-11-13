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

Now: (1) read /home/exedev/targets/fc-minio-rt.log (MinIO's own tests, wholly-fc binaries; it was started with an
earlier fc, so rerun what failed with the current one): that is M4's oracle. (2) The 5 std packages left in
FC_ALL=rt mode: crypto/cipher, crypto/fips140, crypto/internal/fips140only (FIPS tests, not looked at),
encoding/binary (allocation counts: needs FC_ESC=1 for all packages), debug/buildinfo (its test's git call fails
here). Then rerun the whole list once in that mode with one fc binary. (3) Make FC_RUNTIME the default in
`fc build` (the cold build then needs no `go` subprocess: 4.9 core-s of MinIO's 34), rerun
`tools/scoreboard/ours.sh` on an idle machine, update STATUS and MORNING (they predate today's M3 work). (4) Speed,
measured candidates in order: memory (8.1 GB peak costs 6 s of system time and 0.7 s of exit teardown), object
files handed to fclink in process (2.6 core-s), type checking (9 + 2.4 core-s).
