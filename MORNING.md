# MORNING: 2026-11-12 23:23 UTC (campaign 1, milestone M2)

## The oracle number

| | now | at the campaign's start |
|---|---|---|
| standard-library packages compiled entirely by fc whose own tests pass | **205** of 262 with tests (tests/fc/passing.txt) | 0 |
| compile but fail a test | 53 (Codex is triaging: reviews/inbox/fc-triage-1.md when it lands) | |
| not compiled | 4 | |
| language corpus (tests/fc/lang: 21 packages, differential against stock) | **21 of 21** | did not exist |
| MinIO | builds and links, 1,283 packages by fc; **the binary does not run yet** (dies in encoding/gob's init) | 0 |
| other corpus projects | not tried | |

Core-seconds per MB of source, the 205 packages (8.23 MB), `tools/fc/cost.sh`, three rounds, medians:

| | core-s | per MB |
|---|---|---|
| stock baseline mode (build/compile-sb, SB_BASELINE=1, GOGC=off, -dwarf=false -l) | 6.18 (6.00-7.08) | 0.75 |
| fc, attributable to these packages (parse + check + compile) | 2.80 (2.65-3.23) | 0.34 |
| fc, whole process (also checks the closure, runs the assembler) | 3.48 (3.26-3.89) | 0.42 |

**Kill test: 0.45, above the one-third line.** I did not stop: PLAN.md stops the campaign only with no
measured path, and there is one (JOURNAL, M2 entry: the profile's named items, and 0.60 -> 0.45 in the first
hour of looking). It is the number to watch at M3.

## The trace and the scoreboard

TRACE.md is unchanged: it describes the frozen stock path (MinIO cold 11.2 s median, fair stock 32.5 s).
SCOREBOARD.md is new (M0): every corpus project cold at 1, 2, 4 and 8 cores, stock and fair stock, Bazel
projects from their own DAG. One run per cell; fc builds overlapped on cores 4-7 during part of the 8-core
pass, so rerun `tools/scoreboard/run.sh . 8` on a quiet machine before quoting an 8-core cell.

MinIO through fc, whole program, one process (`fc build -link`): 5.4-6.3 s wall, 26-27 core-s including
the assembler and the stock link, peak memory 8 GB. **Not comparable to the trace yet**: the runtime's 30
packages come prebuilt from the stock compiler, the page cache is warm, and the binary does not pass the
smoke test. Materialization: one object per package is still written and read back by the stock linker
(M5 removes both).

## What fc is

go/types over source for the whole dependency closure in one address space; one walk over the typed syntax
tree straight to x86 bytes (no IR, no SSA, no assembler pass); its own type descriptors, itabs, stack maps
and pc tables; objects through cmd/internal/obj's writer until M5. Generics are compiled per instantiation.
About 12,000 lines in driver/fc.

## Behaviour differences of the dev build (every one)

- No escape analysis: every address-taken or captured variable is on the heap, every closure and boxed
  value is allocated. Tests asserting allocation counts are skipped and listed (tests/fc/skips.txt: 1 so
  far; the triage will add the bytes/strings family).
- Every local is live until its function returns (one stack map per function).
- No inlining, no optimization: every variable lives in memory.
- Panic order: where the spec leaves the order of two index panics open, fc matches the stock compiler in
  the cases the corpus covers (right-hand sides last to first), not provably everywhere.
- Method tables' `ptrToThis` for unnamed pointer types is never set in whole-program mode (reflect falls back).
- No DWARF.

## Reviews

Codex skeptic fired on the M2 commit; nothing in the inbox yet.

## Decisions that are Dan's

See STATUS.md (three: the skip list, the escape-all export data in the test harness, continuing past the
kill test's threshold on a measured path).
