Reading additional input from stdin...
OpenAI Codex v0.166.0
--------
workdir: /home/exedev/strand-w
model: gpt-6-astra
provider: exe-llm
approval: never
sandbox: read-only
reasoning effort: none
reasoning summaries: none
session id: 01a1d212-1ddc-7fd3-b906-1a4ca161cf9e
--------
user
You are reviewing the Strand repository (current directory), working toward the goal in CHARTER.md:
cold builds of real projects an order of magnitude or more faster on the critical path. Read CHARTER.md, PLAN.md, STATUS.md,
TRACE.md and the tail of JOURNAL.md. Do not edit code or tests.

Answer briefly and concretely, citing files and numbers:
1. What serializes the current critical path that the agent is not attacking?
2. Which phase's "floor" in TRACE.md is wrong or optimistic, and what is the real limit?
3. What is the strongest reason the current direction is a dead end, and what would you build instead?
4. Which intermediate output is still materialized (written and read back) that a fused
   build would never create, and what does it cost in TRACE.md?
5. Quote any recent JOURNAL.md passage that declines or shrinks work because of time, usage or a
   session ending. Did PLAN.md's oracle number move since the last milestone? What is the largest
   blocker in the campaign that is not being attacked?
Skip praise and skip auditing benchmark claims.
codex
I’ll read the campaign documents and relevant implementation to identify the critical-path limits and unaddressed blockers.
exec
/bin/bash -lc "pwd; rg --files -g 'CHARTER.md' -g 'PLAN.md' -g 'STATUS.md' -g 'TRACE.md' -g 'JOURNAL.md' -g 'AGENTS.md'" in /home/exedev/strand-w
 succeeded in 0ms:
/home/exedev/strand-w
CHARTER.md
TRACE.md
STATUS.md
PLAN.md
JOURNAL.md

exec
/bin/bash -lc 'cat CHARTER.md PLAN.md STATUS.md' in /home/exedev/strand-w
 succeeded in 0ms:
# CHARTER

Only the user edits this file. Agents read it at the start of every session. The current
campaign is in `PLAN.md`; the procedure is in `LOOP.md`.

## North star

A cold build of a real project whose stock build has a minutes-long critical path
produces a working binary that passes the project's own tests, in **2 seconds of wall
clock**, by saturating every core and squeezing every instruction and every byte moved
(network, disk, DRAM, L3, L2, L1).

What the user is after: **one to two orders of magnitude off the critical path of cold
builds**, shown across a corpus of real Go projects (plain `go build` and Bazel/rules_go) with
critical paths of different sizes, and with strong evidence that it scales with cores up to a
point of diminishing returns. 2 seconds on this 8-vCPU VM is the concrete target. Prove what is
possible first; make it general second.

**The demo** (the user's, for the BuildBuddy team; every row a cold build, wall clock, critical
path and core-seconds): stock Bazel locally on 8 cores; stock Bazel on BuildBuddy RBE with no
cache hits (`--noremote_accept_cached`; the user runs this row); ours on 8 cores. Fewer
resources is half the point.

### The approach: fuse the build DAG

A build is a DAG of actions (Bazel's, or `go build`'s action graph). Today every action
is its own process: it gathers its inputs, runs, and materializes its outputs (export
data, objects, archives) for the next action to read back. **Compile the DAG instead:**
treat the whole build as one program, executed in one address space across all cores
(later: a few machines, cut at edges where little data crosses). Intermediate outputs are
not materialized: they're handed over in memory, streamed across edges before their
producer finishes, and deduplicated across the graph. These are HPC techniques (fusion,
streaming, work-stealing, locality) applied to a build.

**The speed limit is reading the sources and writing the binary.** Everything between those
is overhead to be fused away or work to be parallelized.

The night of 2026-11-11 measured why this is the direction (JOURNAL iterations 9-14):
parsing and type-checking all of MinIO in one process takes 1.4 s wall and 7.4
core-seconds, against ~30 core-seconds across 1,312 compiler processes (the difference
is export data written and read back, and IR conversions). On top of that come 1,312
process starts, millions of page faults on fresh heaps, ~600 MB of objects written for the
linker to read again, and 8% of code compiled twice by different packages. Early export
(streaming one edge) took the chain from 38.5 s to ~12 s.

### SIMD and Strand

SIMD and data parallelism are means, not the goal. Strand is the tool for phases that are
data-parallel-shaped (scans over source bytes, layout by prefix sums, relocation blends,
histograms, joins over dense IDs). The trace decides where those are; don't force a phase
into Strand that the trace says is elsewhere (lexing+parsing is ~1% of compile CPU). When a
fused design creates a new data-parallel-shaped phase (e.g. one pass over all sources, a
whole-program symbol table, a layout pass instead of a linker), Strand is the default for it,
and leaving it needs an entry in `SCALAR_LEDGER.md`.

## The one number

Every report leads with the **build trace** (`TRACE.md`, regenerated by one script): wall
clock per phase of the end-to-end build, the critical path, and core utilization.

    phase            ms    cores busy   bytes moved   floor ms   % of floor
    fetch (separate)
    read inputs
    lex/parse
    resolve/types
    lower/codegen
    link
    write output
    TOTAL / crit path

A change matters if it moves this table, `SCOREBOARD.md`, or the current campaign's oracle
number (`PLAN.md`). Microbenchmarks
are diagnostics and are never reported as results.

Two more things the trace must show:

- **A fair stock row.** Next to plain stock `go build`, a cold stock `go build` with the same
  relaxations we use: `GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l'
  -ldflags='-s -w'`. Our gain is measured against *that* row, so "new technique" is kept
  apart from "turned things off".
- **The materialization tax.** Per build: bytes of intermediates written and read back (export
  data, objects, archives), process starts, page faults, core-seconds outside the compiler's
  own phases. This is what fusion removes; it should fall rung by rung.

## Physics, not baselines

For each phase, compute the floor from measured machine limits (`HARDWARE.md`): bytes moved
divided by the bandwidth of each level, and instructions per byte divided by (cores × IPC ×
clock). **% of floor** is the score. Stock toolchains are a reference point for the
end-to-end number only. Beating gcc/go on a kernel is not a goal.

State the scale honestly: this VM has 8 vCPUs, so 2 s is 16 core-seconds. Rung 0 computes
whether the target's irreducible work fits in that; if it doesn't, say so, and report
"2 s on N cores" with N derived from the floor.

## The critical path is the enemy

Minutes-long builds come from dependency chains (package/crate graph, header re-parsing,
global type inference, generic instantiation, linking), not from slow inner loops. For
every phase, answer in writing: **what serializes this, and can the problem be reshaped so
it doesn't?** Don't assume; reshape before giving up. Duplicated work counts as a
critical-path bug.

## May relax

Optimization level, debug info, incremental builds, separate compilation, object-file
formats, the linker as a separate step, diagnostic quality beyond "error + location", any
internal format. Whole-program is fine. Changing Strand is fine.

### This is a dev build (decided by the user, 2026-11-12)

Accepted for the 2-second build: unoptimized code, a binary up to ~2x the stock size, no
DWARF, no inlining, and the baseline code generator's longer object liveness (an object
can stay alive until its function returns, so a test waiting for a finalizer may hang;
skip those tests and list them). Huge pages via `sudo sysctl vm.nr_hugepages` are allowed
as machine configuration (put the old value back after). Report every such difference in
`MORNING.md`; never hide one.

## May not relax

- **Cold:** no caches survive between timed runs (drop the page cache too, if a phase reads from disk).
- **Output:** a binary that passes the project's tests.
- **Correctness:** each replaced phase is differential-tested against the stock toolchain
  (same tokens, same AST, same types, same binary behaviour) and fuzzed at chunk
  boundaries. Bootstrap fixed point and the existing suite stay green.

## Ladder (walking skeleton first)

### Targets

- **Target: MinIO** (`github.com/minio/minio`, the `minio` server binary,
  `CGO_ENABLED=0`). Pure Go, one `go build`, a huge dependency tree (cloud SDKs, generated
  code), hand-written Go assembly (`sha256-simd`, `reedsolomon`), a big serial link.
  Tests: start the server and do S3 put/get/list/delete against it, plus the unit tests
  of the packages we touch.
- **Comparison point: `kube-apiserver`** (Kubernetes, `CGO_ENABLED=0`). Profiled at rung 0
  next to MinIO; once the pipeline works on MinIO, run it here too, so we don't overfit to
  one codebase. Smoke test: it starts and answers `/healthz`.
- **Fast inner loop: the Go toolchain** (`cmd/compile`, `cmd/link`, `cmd/go`, and the
  standard library). It's the quick differential-test corpus. Every phase change is validated
  here in seconds and then confirmed on MinIO. It is not the target: never optimize for
  its build time.
- Full MinIO traces run at campaign milestones. During development, time single packages or
  subtrees and say so.

### Corpus (the scoreboard)

`SCOREBOARD.md`, regenerated by one script: every project cold, stock, fair stock and ours, at 1,
2, 4 and 8 cores (CPU-restricted, e.g. `taskset` or a cgroup cpuset), with wall clock, critical
path and core-seconds. Pin every commit. Graph shapes from `go list` on 2026-11-12 (non-std
packages; longest import chain by source bytes):

| project | why it's here | shape |
|---|---|---|
| buildifier (`bazelbuild/buildtools`) | small, rules_go; the standard library is most of it | 51 pkgs; chain 5.3 MB / 31 pkgs |
| gVisor `runsc` | Bazel-native, pure Go, assembly and Linux-specific code | 169 pkgs; 6.4 MB / 23 |
| caddy (`cmd/caddy`) | the deepest package chain | 794 pkgs; 9.3 MB / 50 |
| tailscaled | mid-size plain go | 552 pkgs; 9.3 MB / 32 |
| MinIO | one fat package on the critical path | 1,048 pkgs; 29.1 MB / 15 |
| kube-apiserver | wide | 1,835 pkgs; 14.3 MB / 31 |
| prometheus | the most source; scaling with cores | 1,283 pkgs; 20.5 MB / 32 |
| BuildBuddy executor and `bb` CLI | the demo; Bazel only (generated protos) | 1,701 pkgs; 11.6 MB / 38 |

Bazel projects build from their Bazel DAG (`bazel aquery`), generated code included, from the
start: BuildBuddy can't be built with plain `go build`.

### What "cold" means

**Fresh cold build every time; nothing else counts** (the user, 2026-11-12). Nothing survives
between timed runs: no GOCACHE, no Bazel disk cache, remote cache or output base, no daemon or
warm process from an earlier run. Anything that warms up (a fork-server, a worker pool, the
Bazel server) starts inside the timer.

- An empty `GOCACHE` (fresh temp dir per run), so the standard library and runtime are part
  of the build.
- Modules are downloaded beforehand (`go mod download`) into a `GOMODCACHE` that is kept
  between runs. Fetching is timed separately, as its own **fetch** row in the trace.
- The page cache is dropped before timed runs (`sync; echo 3 | sudo tee /proc/sys/vm/drop_caches`),
  so reading inputs from disk is honest. Also report the warm-page-cache number next to it.
- Pin versions: record the MinIO and Kubernetes commits and the Go version in `TRACE.md`.

### Rungs

Rungs 0-5 were climbed on 2026-11-11 (JOURNAL iterations 1-14): MinIO cold 54.2 s ->
12.3-13.3 s, kube-apiserver 86.3 s -> 24.1-25.2 s, via our own driver and planner, early
export, a baseline code generator inside `cmd/compile`, huge pages, and splitting `minio/cmd`
across processes. The record of those rungs, kept for reference:

0. **Profile.** (Done for MinIO and kube-apiserver at the time; the
   1/2/4/8-core matrix is now part of the corpus scoreboard.) Build MinIO and `kube-apiserver` cold with the stock toolchain on 1, 2, 4 and 8
   CPUs. Use `go build -debug-trace=trace.json` (a Chrome trace of the action graph) and
   `-debug-actiongraph` to get per-package compile time, the critical path through the
   package graph, idle cores over time, and the link's share. Add bytes read and the total
   source size (Go, assembly, generated). Compute the floor for each phase. Commit
   `TRACE.md` and the script that regenerates it. This is the gap.
1. **Skeleton.** A driver builds MinIO end to end, with every phase present. Phases we
   haven't built yet delegate to the stock toolchain (e.g. Strand's exact Go lexer runs over
   every file, then `go tool compile`/`asm`/`link` do the rest). The trace exists from day one.
2. **And on.** Replace or reshape the phase with the most wall clock on the critical path, one
   at a time. A rung is done when the MinIO trace shrinks, the toolchain corpus and MinIO
   tests still pass, and the differential tests are green.

### Fusion rungs (the map; PLAN.md's campaign says which one is being built)

A rung is finished when the real MinIO build consumes it, passes its tests, and shows the
materialization tax falling in TRACE.md. Getting there may take days, with the trace flat or
worse in between; the campaign's oracle number measures progress until then.

F0. **The fair stock row and the tax, measured.** Add both to TRACE.md (see "The one
    number"). Attribute the current 80 core-seconds: compiling vs moving data between actions
    vs process and memory setup.
F1. **Persistent workers, intermediates in memory.** Compiler processes stay alive across
    packages, and export data and objects pass through shared memory (memfd/tmpfs mappings)
    instead of being written and re-read. `cmd/compile` keeps global state, so a worker may
    need resetting between packages, or a fork-server (one warm process forked per package)
    may be the cheaper way to get the same effect. Measure both; pick by the trace.
F2. **One address space for the front end.** Whole-program parse and type check in one process
    (measured at 1.4 s wall), handing typed packages to the back end in memory, with no export
    data at all. The second compiler (`driver/sbc`) and the whole-program spike
    (`explore/wholeprog`) are the start of this. Fall back to the stock path per package.
F3. **The DAG as a schedule, not a set of processes.** Function bodies (not packages) are the
    unit of work, on a work-stealing queue across all cores; package boundaries matter only
    for initialization order. Deduplicate shared functions (wrappers, instantiations) across
    the graph.
F4. **No linker as a separate step.** Dense symbol IDs from the compiler; layout by one prefix
    sum over sizes; code emitted straight into the final image; relocation as one data-parallel
    pass. (Strand-shaped: see LINKING.md.)
F5. **From Bazel.** Take the DAG from Bazel (`bazel aquery`, rules_go first) instead of from our
    own planner, so the fused executor isn't Go-specific at the graph level. (Reading the DAG
    is pulled forward into the corpus scoreboard; this rung is the executor consuming it.) Then think about
    cutting the DAG across a few machines at narrow edges (package interfaces are small next
    to objects).

Later (record in `IDEAS.md`, don't start yet): a second language through the same fused
executor (Rust or C++ is the boss level); the network/disk end of the pipeline.

## How to work

- **Never stop to ask.** Pick the most ambitious step you can justify, write why in
  `JOURNAL.md`, and do it. Decisions that are the user's go in STATUS.md; keep working.
- **Horizon.** This work runs for days. A session ending (usage limit, context, restart) is not
  an event; the next one continues from `PLAN.md`. Time left, usage left and "the next session"
  are never reasons to decline or shrink a step. The only valid reason is a measurement that
  shows it cannot pay.
- **Calibrate on your own record, not human estimates.** The baseline code generator reached
  97% of MinIO's functions in under an hour; the link spike came within 46 of the stock linker's
  122,860 functions in 33 minutes. A front end or a linker is days of work for you, not months.
- **One campaign at a time**, named in `PLAN.md` by the user. You decide how, not whether. It
  ends when its exit test passes, or when its kill test, written before work began, fails by
  measurement. The trace may stay flat or get worse during a campaign; progress is then the
  campaign's oracle number.
- **Negative results are results.** Write them up and move on.
- **Commit and push often** with real messages. The history is part of the deliverable.
- Report **absolute times** (ms, ns, GB/s), never ratio tables. Take timestamps from `date -u`,
  not from memory.
- **Don't ration usage.** The harness resumes after a limit; committing often means an abrupt end
  costs nothing. Prefer Codex and sol for reviews (separate budgets), cheaper models for
  subagents that only search or summarize, and Fable for the hard parts.
- **The host is noisy.** Compare configurations interleaved in the same window
  (`tools/trace/ab.sh`), not against numbers from hours earlier.

## Anti-goals

- Meta work: "discovering" speedups, frameworks for finding speedups, benchmark harness polish.
- Wins that don't move the trace (e.g. counting declarations faster).
- Defending claims. Make fewer claims; show the trace.
- Waiting on reviews.
- Declining or shrinking work because of time left, usage, or a session ending.
- Polishing a frozen path: once a campaign freezes a baseline, it stays frozen.
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

Now: M2. `tools/fc/survey.sh std` (last run: 81 of 374 std packages compile; reasons most frequent first: type
descriptors/itabs, function literals, type assertions, map index, defer, interface conversion, type switch, map
literal, range over map, method values, clear, select, print, complex, ABI wrappers for assembly functions).
Implement down that list in driver/fc (stubs that give up are in misc.go), `tools/fc/test.sh <pkg>` per package,
record passes in tests/fc/passing.txt. Two builders were working in worktrees under .claude/worktrees when the
session hit its usage limit (type descriptors in rtype.go checked against stock objects; tests/fc/lang feature
tests): look at `git worktree list` and their branches' logs, merge what is committed, and re-brief if unfinished.
M0: when /home/exedev/targets/scoreboard.log ends with ALLDONE run `tools/scoreboard/mkscoreboard.py > SCOREBOARD.md`
and commit (the 8-core rows of the later projects overlapped with fc builds on cores 4-7: rerun `tools/scoreboard/run.sh . 8`
on a quiet machine before trusting them).
# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)

Campaign 1, M1 reached 2026-11-12 19:57 UTC: fc (driver/fc), our own compiler, compiles `unicode/utf8` whole; its tests pass.
Oracle: 1 standard-library package (of ~190 with tests); corpus 0. Core-seconds per MB: measured at M2.
M0 scoreboard run in progress in the background (SCOREBOARD.md when it ends). Trace unchanged (frozen path).

## Decisions for Dan

1. Tests that assert an optimization (zero allocations that need escape analysis) are skipped and listed in
   tests/fc/skips.txt, like the charter's finalizer tests. Say so if they should instead block a package.

exec
/bin/bash -lc 'cat TRACE.md; tail -n 240 JOURNAL.md' in /home/exedev/strand-w
 succeeded in 0ms:
# TRACE

Generated by `tools/trace/mktrace.py` on 2026-11-12 15:49 UTC from the instrumented builds made by `tools/trace/stock_all.sh`. Do not edit by hand.

Machine: 8 vCPUs (AMD EPYC 9554P under KVM), 31 GiB RAM; 2 s of wall clock is 16 core-seconds.

Measured limits (`hw/limits.c`, `hw/limits.out.txt`): memcpy 1t 9.88 GB/s, memcpy 8t 43.09 GB/s, disk write 0.90 GB/s, disk read cold 3.11 GB/s, pagecache read 1t 4.99 GB/s, smallfiles cold 8t 0.27 GB/s, smallfiles warm 8t 2.11 GB/s.

## minio: the build trace (driver `sb`, 8 CPUs, cold)

Run `13-noweakptr` (driver at `5e509a4`, flags `GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout`). Result: PASS minio smoke (put/get/list/delete, 3 MB object round trip). Stock `go build` on the same machine, cold: 54,233 ms.

Wall clock of 4 cold runs of this configuration, ms: 11,009, 11,344, 10,916, 10,794. The table is the first of them.

| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |
|---|---|---|---|---|---|---|---|
| fetch (separate, not in total) | 6,675 | | | 1,120 MB | | | `go mod download` |
| plan (package graph) + read inputs | 1,092 | 3.9 | 0.0 | 76 MB | 374 | 3x | driver; go.mod, build constraints |
| &nbsp;&nbsp;of which lex + package-level declarations | 372 | 3.1 | 1.1 | 76 MB | 6 | 61x | Strand godecl |
| head start | 1,409 | | | 0 kB | | | runtime closure compiled during the plan |
| parse + resolve/types + lower/codegen | 8,470 | 7.6 | 62.9 | 526 MB | 268 | 32x | go tool compile/asm |
| link | 1,071 | 1.9 | 1.7 | 159 MB | 16 | 67x | go tool link |
| write output | 111 | 4.1 | 0.0 | 159 MB | 176 | 1x | driver |
| **TOTAL** | **11,009** | **6.25** | **69** | read 223 MB, wrote 160 MB (disk) | **841** | **13x** | |
| critical path (13 actions) | 3,429 | | | | | | |

**Inside the compile row, by profile** (`tools/trace/phases.sh`: a CPU profile of every compiler process of run `08-huge-shard`, merged; 42 s of samples. Read the shares, not the seconds: the sampler misses about a third of the row's CPU. The exact per-phase table is further down):

| compiler phase | core-s | share | the charter's row |
|---|---|---|---|
| lex + parse (syntax.Parse) | 0.3 | 0.8% | lex/parse |
| type check (types2) | 4.8 | 11.4% | resolve/types |
| typed tree -> export IR (write) | 3.0 | 7.2% | resolve/types |
| export IR -> compiler IR (read), imports | 6.0 | 14.3% | resolve/types |
| escape analysis, wrappers, runtime types, init | 5.6 | 13.3% | lower/codegen |
| walk + build SSA | 4.5 | 10.8% | lower/codegen |
| SSA passes (regalloc 0.5) | 2.2 | 5.2% | lower/codegen |
| emit machine code (genssa, assembler) | 8.0 | 19.2% | lower/codegen |
| write objects and export data | 3.6 | 8.5% | write objects |
| not under any of these (scheduler, runtime start, flag parsing) | 3.9 | 9.2% | |

Across all phases, 5.0 s is the allocator and 1.4 s is clearing memory. Lexing and parsing all of the build's Go is 0.34 core-s: 0.8% of the compiler's work.

**The fair stock row.** Stock `go build`, cold, with the relaxations this build uses (`GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`): **32,512 ms** (169 core-s). Plain stock in the same hour: 49,953 ms. This build: 11,009 ms. The gain to claim is against the fair row.

**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):

| | stock | fair stock | this build |
|---|---|---|---|
| wall ms, cold | 49,953 | 32,512 | 11,009 |
| core-s | 257 | 169 | 69 |
| tasks created, whole machine (processes + threads) | 11,404 | 10,585 | 6,380 |
| tool processes started (exec) | 1,483 | 1,483 | 172 + 1,315 forked from one warm compiler |
| page faults, whole machine | 11,923,241 | 11,007,650 | 1,343,950 |
| intermediates written (archives, export data) | 1,899 MB | 1,383 MB | 618 MB |
| intermediates read back (importers, linker, cache copy) | 2,731 MB | 1,996 MB | 1,143 MB |
| of which export data | 107 MB | 91 MB |  |

Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.

**Where the compile row's CPU goes, exactly** (`tools/trace/cpuphases.sh`: every compiler process logs its own CPU time and page faults at each phase boundary; warm run, 1,315 compiler processes, 68.1 core-s in the row):

| compiler phase | user s | sys s | page faults | what it is |
|---|---|---|---|---|
| be:compilefuncs | 24.3 | 1.7 | 111,536 | walk, baseline code generation (SSA for the rest), assembler: **compiling** |
| fe:irread | 9.1 | 1.2 | 74,184 | build the compiler's IR by reading that back, plus imported bodies and types: **moving data** |
| fe:typecheck | 7.7 | 1.6 | 132,859 | types2, including reading imported packages' export data on demand: compiling (+ moving data) |
| fe:irwrite | 4.6 | 0.7 | 57,626 | serialize the checked package as export IR: **moving data** |
| be:dumpobj | 2.8 | 0.6 | 6,788 | write the object file: **moving data** |
| fe:earlyexport | 2.4 | 0.4 | 29,548 | write export data for importers: **moving data** |
| fe:devirtualize-and-inline | 2.2 | 0.2 | 11,067 | inlining is off; what is left under this timer is method wrappers and small per-function passes: compiling |
| fe:escapes | 2.1 | 0.2 | 7,394 | escape analysis: compiling |
| fe:parse | 1.4 | 0.6 | 71,704 | read and parse the source files: compiling |
| (before main) | 0.9 | 0.2 | 126,091 | exec, runtime start, package init: **process setup** |
| fe:init | 0.8 | 0.3 | 134,437 | flags, universe: process setup |
| fe:loadsys | 0.5 | 0.2 | 26,153 | declare the runtime's functions: process setup |
| (exit, asm, pack) | 1.6 | 0.0 | 0 | process exit, the assembler, joining shard objects |

Notes: plan + read: 1312 packages, 7531 Go files, 10 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.

```
8 |▁▃▅▄▄▆▇█████████████████████████████████████▇█▇▅▄▄▇█▃▁▁▁▁▂▄▁|   cores busy; one column = 183 ms
0 +------------------------------------------------------------+
```

The chain that ends the build (each action was the last thing the next one waited for):

| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
|---|---|---|---|---|---|
| `github.com/minio/minio/cmd` | compile | 7,842 | 1,716 | 0 | 5 MB |
| `github.com/minio/minio` | link | 9,567 | 1,071 | 9,567 | 0 kB |
| `github.com/prometheus/procfs` | compile | 2,003 | 167 | 12 | 297 kB |
| `syscall` | compile+asm | 1,161 | 102 | 3 | 244 kB |
| `os` | compile | 1,399 | 80 | 0 | 179 kB |
| `internal/poll` | compile | 1,319 | 80 | 0 | 66 kB |
| `fmt` | compile | 1,480 | 67 | 1 | 99 kB |
| `time` | compile | 1,263 | 55 | 0 | 168 kB |

### minio: every rung so far (8 CPUs, cold)

Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.

| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
|---|---|---|---|---|---|---|---|---|---|---|
| stock `go build` | 54,233 | 282 | 5.21 | | | | | 3,251 | | |
| `01-skeleton`  | 47,553 | 265 | 5.58 | 428 | 216 | 213 | 43,083 | 3,077 | 272 | pass |
| `02-pipeline` (-compiler build/compile-sb -pipeline -backends 2000) | 39,885 | 273 | 6.84 | 623 | 299 | 207 | 35,221 | 2,925 | 283 | pass |
| `03-relaxed` (GOGC=off -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 21,957 | 144 | 6.58 | 509 | 202 | 225 | 19,053 | 1,532 | 192 | pass |
| `04-selfplan` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 22,111 | 147 | 6.65 | 663 | (in plan) | 210 | 19,365 | 1,634 | 183 | pass |
| `05-sbc` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -sbc build/sbc -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,016 | 156 | 6.48 | 924 | (in plan) | 245 | 19,976 | 1,618 | 202 | pass |
| `06-baseline` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,259 | 92 | 5.65 | 814 | (in plan) | 245 | 13,459 | 1,425 | 297 | pass |
| `07-baseline-float` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,403 | 91 | 5.58 | 743 | (in plan) | 276 | 13,684 | 1,404 | 302 | pass |
| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 13,262 | 80 | 6.06 | 932 | (in plan) | 256 | 10,438 | 1,313 | 282 | pass |
| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 15,616 | 97 | 6.18 | 686 | (in plan) | 233 | 12,911 | 1,397 | 277 | pass |
| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 11,699 | 67 | 5.74 | 945 | (in plan) | 263 | 8,938 | 1,194 | 310 | pass |
| `11-headstart` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart) | 13,813 | 86 | 6.22 | 1,013 | (in plan) | 442 | 11,069 | 1,191 | 276 | pass |
| `12-directout` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout) | 11,531 | 71 | 6.16 | 1,048 | (in plan) | 367 | 8,853 | 1,227 | 94 | pass |
| `13-noweakptr` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout) | 11,009 | 69 | 6.25 | 1,092 | (in plan) | 372 | 8,470 | 1,071 | 111 | pass |

## kube-apiserver: the build trace (driver `sb`, 8 CPUs, cold)

Run `13-noweakptr` (driver at `5e509a4`, flags `GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout`). Result: PASS kube-apiserver smoke (/healthz ok; ConfigMap create/get/delete). Stock `go build` on the same machine, cold: 86,260 ms.

Wall clock of 2 cold runs of this configuration, ms: 23,634, 20,536. The table is the first of them.

| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |
|---|---|---|---|---|---|---|---|
| fetch (separate, not in total) | 0 (vendored) | | | | | | |
| plan (package graph) + read inputs | 807 | 4.2 | 0.0 | 89 MB | 334 | 2x | driver; go.mod, build constraints |
| &nbsp;&nbsp;of which lex + package-level declarations | 402 | 4.4 | 1.3 | 89 MB | 7 | 56x | Strand godecl |
| head start | 5,570 | | | 0 kB | | | runtime closure compiled during the plan |
| parse + resolve/types + lower/codegen | 20,591 | 7.7 | 151.9 | 1,043 MB | 318 | 65x | go tool compile/asm |
| link | 1,666 | 1.6 | 2.5 | 182 MB | 28 | 59x | go tool link |
| write output | 95 | 1.0 | 0.0 | 182 MB | 201 | 0x | driver |
| **TOTAL** | **23,634** | **6.82** | **161** | read 201 MB, wrote 184 MB (disk) | **888** | **27x** | |
| critical path (36 actions) | 8,984 | | | | | | |

**The fair stock row.** Stock `go build`, cold, with the relaxations this build uses (`GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`): **51,151 ms** (329 core-s). Plain stock in the same hour: 77,091 ms. This build: 23,634 ms. The gain to claim is against the fair row.

**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):

| | stock | fair stock | this build |
|---|---|---|---|
| wall ms, cold | 77,091 | 51,151 | 23,634 |
| core-s | 511 | 329 | 161 |
| tasks created, whole machine (processes + threads) | 18,018 | 16,510 | 9,278 |
| tool processes started (exec) | 2,180 | 2,180 | 102 + 2,081 forked from one warm compiler |
| page faults, whole machine | 26,907,866 | 26,418,964 | 2,299,864 |
| intermediates written (archives, export data) | 5,466 MB | 4,283 MB | 1,762 MB |
| intermediates read back (importers, linker, cache copy) | 9,909 MB | 7,993 MB | 4,781 MB |
| of which export data | 764 MB | 713 MB |  |

Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.

Notes: plan + read: 2079 packages, 8940 Go files, 11 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.

```
8 |▄▄▆█████████████████████████████████████████████████▅▂▁▁▁▁▃▁|   cores busy; one column = 394 ms
0 +------------------------------------------------------------+
```

The chain that ends the build (each action was the last thing the next one waited for):

| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
|---|---|---|---|---|---|
| `k8s.io/kubernetes/cmd/kube-apiserver` | link | 21,408 | 1,666 | 21,408 | 0 kB |
| `k8s.io/kubernetes/pkg/controlplane/apiserver` | compile | 19,887 | 757 | 4 | 68 kB |
| `net/http/internal/http2` | compile | 2,488 | 447 | 0 | 370 kB |
| `k8s.io/kubernetes/cmd/kube-apiserver/app` | compile | 20,953 | 440 | 0 | 20 kB |
| `net/http` | compile | 2,935 | 439 | 0 | 562 kB |
| `k8s.io/client-go/informers/core/v1` | compile | 8,380 | 434 | 0 | 180 kB |
| `crypto/internal/fips140/nistec` | compile+asm | 1,318 | 396 | 0 | 624 kB |
| `k8s.io/client-go/kubernetes` | compile | 7,626 | 387 | 2 | 35 kB |

### kube-apiserver: every rung so far (8 CPUs, cold)

Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.

| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
|---|---|---|---|---|---|---|---|---|---|---|
| stock `go build` | 86,260 | 554 | 6.43 | | | | | 4,974 | | |
| `03-relaxed` (GOGC=1000 -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 53,910 | 346 | 6.42 | 1,114 | 176 | 243 | 49,219 | 2,453 | 203 | pass |
| `04-selfplan` (GOGC=1000 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 44,406 | 320 | 7.22 | 601 | (in plan) | 228 | 40,657 | 2,465 | 194 | pass |
| `06-baseline` (GOGC=1000 SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 28,974 | 196 | 6.77 | 723 | (in plan) | 247 | 24,974 | 2,404 | 373 | pass |
| `07-baseline-float` (GOGC=1000 SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 29,561 | 201 | 6.80 | 739 | (in plan) | 354 | 25,523 | 2,429 | 366 | pass |
| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,090 | 158 | 6.56 | 609 | (in plan) | 260 | 20,521 | 2,119 | 334 | pass |
| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote) | 24,517 | 162 | 6.60 | 622 | (in plan) | 224 | 21,031 | 2,031 | 330 | pass |
| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote) | 21,130 | 137 | 6.49 | 628 | (in plan) | 244 | 17,882 | 1,822 | 308 | pass |
| `11-headstart` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart) | 23,800 | 154 | 6.46 | 827 | (in plan) | 420 | 20,110 | 1,983 | 308 | pass |
| `12-directout` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout) | 20,097 | 135 | 6.70 | 758 | (in plan) | 378 | 16,998 | 1,810 | 104 | pass |
| `13-noweakptr` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout) | 23,634 | 161 | 6.82 | 807 | (in plan) | 402 | 20,591 | 1,666 | 95 | pass |

# The stock toolchain (rung 0: the gap)

## minio

Commit `7aac2a2c5b7c`, `go1.27.3 linux/amd64`, `CGO_ENABLED=0`, `-trimpath`. 1,312 packages (264 from the standard library), 7,531 Go files, 75.5 MB and 2,379,958 lines of Go, 109 assembly files (5.4 MB), 20.1 MB embedded. Output binary: 146.8 MB.

### Stock toolchain, cold, by CPU count

| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |
|---|---|---|---|---|---|---|
| 1 | 234,039 | 229 | 0.98 | 4,195 | 544 MB | 2,074 MB |
| 2 | 121,704 | 220 | 1.80 | 3,424 | 634 MB | 1,938 MB |
| 4 | 72,561 | 237 | 3.27 | 3,280 | 598 MB | 1,835 MB |
| 8 | 54,233 | 282 | 5.21 | 3,251 | 504 MB | 1,651 MB |
| 8, warm page cache | 49,937 | 262 | 5.25 | 3,128 | 293 MB | 1,598 MB |

### The build trace (8 CPUs, cold)

Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the phases interleave across 1,300+ processes, so they are given in core-seconds (the compiler's own `-bench` timers, summed over all packages, scaled to the cold run's compile CPU).

| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |
|---|---|---|---|---|---|---|
| fetch (separate, not in total) | 6,675 | | | 1,120 MB on disk | | |
| read inputs | 4,296 (cold minus warm) | | | 504 MB read from disk; sources are 101 MB | 374 | 11x |
| load package graph (`go build` before the first compile) | 3,255 | | 1.8 | | | |
| **compile stretch** (first compile to link start) | 47,383 | 267 | 5.9 | 81 MB source in, 957 MB of objects out | | |
| &nbsp;&nbsp;process start (init, load runtime decls) | | 2.1 | | | 0 (not needed) | |
| &nbsp;&nbsp;lex/parse + resolve/types | | 50.4 | | 76 MB of Go | 152 | 41x (core-s / 8 against the floor) |
| &nbsp;&nbsp;inline + escape analysis | | 26.4 | | | 0 (not needed) | |
| &nbsp;&nbsp;lower/codegen | | 169.0 | | 76 MB of Go | 122 | 173x (core-s / 8 against the floor) |
| &nbsp;&nbsp;write objects | | 18.8 | | | 0 (not needed) | |
| link | 3,251 | 4.8 | 1.7 | 147 MB binary | 3 | 954x |
| write output | (inside link) | | | 147 MB; the build wrote 1,651 MB in all | 163 | |
| after link (install, exit) | 344 | | | | | |
| **TOTAL** | **54,233** | **282** | **5.21** | | **815** | **67x** |
| critical path (longest dependency chain, 30 actions) | 38,503 | | | | | |

The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages (`-bench` run, 8 CPUs, warm page cache): be:compilefuncs 92.1, fe:parse 50.4, fe:devirtualize-and-inline 19.3, be:dumpobj 18.8, fe:escapes 7.0, fe:loadsys 1.2, fe:init 0.9, fe:pgo-load-profile 0.0. Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.

### Cores busy over time (8 CPUs, cold)

```
8 |▂▂▂▃▃▄▅██████▇▇█▇█▇▇█████▇█▇██████▇▆▇▇▆▄▂▄▅▆▄▄▃▂▂▂▂▂██▄▁▁▁▂▂|   one column = 904 ms
0 +------------------------------------------------------------+   load graph ends 3,255 ms, link 50,638-53,889 ms
```
Share of wall clock by number of busy cores: 1: 19%, 2: 11%, 3: 3%, 4: 4%, 5: 3%, 6: 4%, 7: 21%, 8: 35%. Idle core-seconds: 151 of 434.

### Critical path through the package graph (8 CPUs, cold)

The longest chain of actions, each waiting for the one before: 38,503 ms of the 54,233 ms wall clock. With unlimited cores and this toolchain the build could not be shorter than this.

| # | package | action | start ms | ms | waited for a core, ms | Go source |
|---|---|---|---|---|---|---|
| 5 | `runtime` | build | 3,783 | 1,853 | 0 | 3 MB |
| 9 | `syscall` | build | 5,829 | 403 | 0 | 244 kB |
| 13 | `os` | build | 6,806 | 474 | 0 | 179 kB |
| 16 | `encoding/json/v2` | build | 7,683 | 1,173 | 0 | 237 kB |
| 18 | `github.com/goccy/go-json/internal/encoder` | build | 21,205 | 602 | 0 | 161 kB |
| 19 | `github.com/goccy/go-json/internal/encoder/vm_color_indent` | build | 21,816 | 2,599 | 0 | 147 kB |
| 20 | `github.com/goccy/go-json/internal/encoder/vm_color` | build | 24,416 | 2,806 | 0 | 147 kB |
| 21 | `github.com/goccy/go-json/internal/encoder/vm_indent` | build | 27,226 | 4,985 | 0 | 145 kB |
| 22 | `github.com/goccy/go-json/internal/encoder/vm` | build | 32,213 | 4,751 | 0 | 145 kB |
| 25 | `github.com/minio/madmin-go/v3` | build | 37,218 | 1,419 | 0 | 1 MB |
| 26 | `github.com/minio/mc/cmd` | build | 38,666 | 2,901 | 0 | 2 MB |
| 27 | `github.com/minio/console/api` | build | 41,570 | 424 | 0 | 285 kB |
| 28 | `github.com/minio/minio/cmd` | build | 42,040 | 8,546 | 0 | 5 MB |
| 30 | `github.com/minio/minio` | link | 50,638 | 3,251 | 0 | 1 kB |

(30 actions on the path; the 14 longest shown.)

### The ten most expensive actions (8 CPUs, cold)

| package | action | wall ms | core-s | Go source |
|---|---|---|---|---|
| `github.com/minio/minio/cmd` | build | 8,546 | 26.5 | 5 MB |
| `github.com/elastic/go-elasticsearch/v7/esapi` | build | 8,460 | 17.6 | 3 MB |
| `github.com/goccy/go-json/internal/encoder/vm` | build | 4,751 | 6.7 | 145 kB |
| `github.com/minio/mc/cmd` | build | 2,901 | 6.3 | 2 MB |
| `github.com/minio/minio` | link | 3,251 | 4.8 | 1 kB |
| `runtime` | build | 1,853 | 4.5 | 3 MB |
| `github.com/minio/madmin-go/v3` | build | 1,419 | 3.9 | 1 MB |
| `github.com/goccy/go-json/internal/encoder/vm_indent` | build | 4,985 | 3.8 | 145 kB |
| `github.com/IBM/sarama` | build | 2,424 | 3.1 | 880 kB |
| `github.com/envoyproxy/go-control-plane/envoy/config/core/v3` | build | 2,455 | 2.9 | 1 MB |

## kube-apiserver

Commit `2cb26b34ab73`, `go1.27.3 linux/amd64`, `CGO_ENABLED=0`, `-trimpath`. 2,079 packages (244 from the standard library), 8,940 Go files, 89.3 MB and 2,500,778 lines of Go, 62 assembly files (0.8 MB), 0.0 MB embedded. Output binary: 172.8 MB.

### Stock toolchain, cold, by CPU count

| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |
|---|---|---|---|---|---|---|
| 1 | 451,576 | 448 | 0.99 | 6,101 | 457 MB | 5,703 MB |
| 2 | 233,876 | 441 | 1.89 | 5,466 | 768 MB | 5,513 MB |
| 4 | 128,596 | 461 | 3.59 | 4,762 | 434 MB | 5,228 MB |
| 8 | 86,260 | 554 | 6.43 | 4,974 | 813 MB | 4,863 MB |
| 8, warm page cache | 73,522 | 485 | 6.60 | 4,898 | 74 kB | 4,677 MB |

### The build trace (8 CPUs, cold)

Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the phases interleave across 1,300+ processes, so they are given in core-seconds (the compiler's own `-bench` timers, summed over all packages, scaled to the cold run's compile CPU).

| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |
|---|---|---|---|---|---|---|
| fetch (separate, not in total) | 0 (vendored in the repository) | | | | | |
| read inputs | 12,738 (cold minus warm) | | | 813 MB read from disk; sources are 90 MB | 334 | 38x |
| load package graph (`go build` before the first compile) | 4,037 | | 1.5 | | | |
| **compile stretch** (first compile to link start) | 76,515 | 528 | 7.3 | 90 MB source in, 2,739 MB of objects out | | |
| &nbsp;&nbsp;process start (init, load runtime decls) | | 3.5 | | | 0 (not needed) | |
| &nbsp;&nbsp;lex/parse + resolve/types | | 148.6 | | 89 MB of Go | 180 | 103x (core-s / 8 against the floor) |
| &nbsp;&nbsp;inline + escape analysis | | 61.2 | | | 0 (not needed) | |
| &nbsp;&nbsp;lower/codegen | | 226.3 | | 89 MB of Go | 144 | 196x (core-s / 8 against the floor) |
| &nbsp;&nbsp;write objects | | 88.0 | | | 0 (not needed) | |
| link | 4,974 | 7.2 | 1.6 | 173 MB binary | 4 | 1,240x |
| write output | (inside link) | | | 173 MB; the build wrote 4,863 MB in all | 192 | |
| after link (install, exit) | 734 | | | | | |
| **TOTAL** | **86,260** | **554** | **6.43** | | **854** | **101x** |
| critical path (longest dependency chain, 54 actions) | 62,744 | | | | | |

The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages (`-bench` run, 8 CPUs, warm page cache): be:compilefuncs 192.9, fe:parse 148.6, be:dumpobj 88.0, fe:devirtualize-and-inline 47.2, fe:escapes 14.0, fe:loadsys 2.1, fe:init 1.5, fe:pgo-load-profile 0.0. Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.

### Cores busy over time (8 CPUs, cold)

```
8 |▃▁▂▃▆▇███████████████████▇███▇█████████████████▇████▇▅▅▃▁▁▃▁|   one column = 1,438 ms
0 +------------------------------------------------------------+   load graph ends 4,037 ms, link 80,551-85,526 ms
```
Share of wall clock by number of busy cores: 0: 0%, 1: 10%, 2: 3%, 3: 2%, 4: 1%, 5: 1%, 6: 1%, 7: 26%, 8: 56%. Idle core-seconds: 136 of 690.

### Critical path through the package graph (8 CPUs, cold)

The longest chain of actions, each waiting for the one before: 62,744 ms of the 86,260 ms wall clock. With unlimited cores and this toolchain the build could not be shorter than this.

| # | package | action | start ms | ms | waited for a core, ms | Go source |
|---|---|---|---|---|---|---|
| 27 | `k8s.io/apimachinery/pkg/apis/meta/v1` | build | 16,532 | 2,565 | 0 | 527 kB |
| 28 | `k8s.io/api/core/v1` | build | 19,675 | 6,910 | 66 | 3 MB |
| 29 | `k8s.io/api/resource/v1` | build | 27,493 | 2,015 | 0 | 703 kB |
| 37 | `k8s.io/client-go/informers/core/v1` | build | 37,139 | 1,897 | 0 | 180 kB |
| 39 | `k8s.io/client-go/informers` | build | 43,240 | 3,141 | 0 | 53 kB |
| 42 | `k8s.io/apiserver/pkg/admission/plugin/policy/generic` | build | 50,300 | 2,075 | 0 | 81 kB |
| 44 | `k8s.io/apiserver/pkg/admission/plugin/policy/validating` | build | 52,436 | 2,476 | 0 | 61 kB |
| 45 | `k8s.io/apiserver/pkg/admission/plugin/policy/mutating` | build | 54,912 | 2,551 | 0 | 28 kB |
| 46 | `k8s.io/apiserver/pkg/server` | build | 57,465 | 2,913 | 0 | 175 kB |
| 47 | `k8s.io/apiserver/pkg/server/options` | build | 60,450 | 2,800 | 0 | 164 kB |
| 48 | `k8s.io/kubernetes/pkg/kubeapiserver/options` | build | 67,895 | 1,954 | 0 | 67 kB |
| 49 | `k8s.io/kubernetes/pkg/controlplane/apiserver/options` | build | 69,849 | 1,970 | 0 | 26 kB |
| 50 | `k8s.io/kubernetes/pkg/controlplane/apiserver` | build | 74,875 | 1,847 | 0 | 68 kB |
| 54 | `k8s.io/kubernetes/cmd/kube-apiserver` | link | 80,551 | 4,974 | 0 | 1 kB |

(54 actions on the path; the 14 longest shown.)

### The ten most expensive actions (8 CPUs, cold)

| package | action | wall ms | core-s | Go source |
|---|---|---|---|---|
| `k8s.io/api/core/v1` | build | 6,910 | 7.9 | 3 MB |
| `k8s.io/kubernetes/cmd/kube-apiserver` | link | 4,974 | 7.2 | 1 kB |
| `k8s.io/kubernetes/pkg/generated/openapi` | build | 2,360 | 5.5 | 4 MB |
| `cel.dev/cel-go/common/types` | build | 4,375 | 4.8 | 330 kB |
| `k8s.io/kubernetes/pkg/apis/core/validation` | build | 3,515 | 4.5 | 486 kB |
| `runtime` | build | 1,720 | 4.2 | 3 MB |
| `k8s.io/client-go/kubernetes/typed/core/v1/fake` | build | 3,035 | 3.3 | 48 kB |
| `k8s.io/apiserver/pkg/server` | build | 2,913 | 3.1 | 175 kB |
| `k8s.io/apiextensions-apiserver/pkg/apiserver` | build | 3,080 | 3.0 | 91 kB |
| `k8s.io/kubernetes/pkg/apis/core/v1` | build | 2,518 | 2.9 | 655 kB |

## How the floors are computed

A floor is the larger of (bytes moved / measured bandwidth of the level they move through) and (bytes x modelled ticks per byte / (8 cores x 3.095e9 ticks/s)). The bandwidths are measured. The ticks per byte are budgets for a data-parallel implementation, set in `tools/trace/mktrace.py`: lex 2, parse 8, types 40, codegen 40. They are the weakest numbers in this file. The link floor is one pass over the objects in and the binary out at memory bandwidth: a copy floor, which says nothing about symbol resolution or relocation (no model for those yet). For scale: the Strand Go lexer measures 15 ticks/byte today (GOLEX.md), and the stock toolchain spends about 11,578 ticks per source byte end to end on MinIO.

## Does 2 seconds fit on this machine?

By the floors above, MinIO's irreducible work is about 6.5 core-seconds (815 ms on 8 cores), inside the 16 core-seconds that 2 s on this VM allows, so physics does not rule the target out here. The stock toolchain does 282 core-seconds of work for the same build: with it doing the work, 2 s would need 141 perfectly used cores, and its dependency chain (38,503 ms) would still be in the way. So the gap is 18x in work done and 19x in the critical path; both have to go.

  program; MinIO smoke and stress pass.
- Link: 1,113 -> **1,189 ms** (four builds each, order-balanced; link CPU 1,816 -> 1,946 ms). Slower.
  Without sharing there is more pclntab to build and write, and that costs more than the hashing saved.
- Binary 159.1 -> 162.7 MB (+2.3%); objects 526 -> 504 MB.
- Compile: one package in strict alternation, twice: -0.8% and +1.2%, i.e. nothing.
So in the stock loader, hashing content-addressed pc tables is not where the 390 ms goes; the 200 ms
of "claims" in my spike is the cost of *my* table (Go maps with string keys), not a property of the
problem. That corrects iteration 25's reading: F4's first step is not "stop emitting those symbols".
What the stock loader's resolution time is made of is still unmeasured below the function level
(`addSym` and map operations, by its profile), and the spike needs a better table before its
resolution number means anything. The layout and relocation numbers (40 ms) stand.

**A second warning about whole-build CPU comparisons.** The same eight order-balanced builds showed
compile CPU of 67.5 core-s hashed against 61.6 plain, the "right" sign in all four adjacent pairs, by
5.0 to 6.6 core-s each. There is no mechanism for it (a million SHA-256s of tiny buffers is 0.3
core-s) and the single-package measurement says zero. So order balancing with four or six builds per
arm is *not* enough on this host for compile core-seconds: the noise is large (sigma about 5 core-s per
build) and correlated over minutes in ways ABBA does not cancel. What I trust, in order: (1) a
mechanism plus a count (bytes assembled, descriptors emitted); (2) one package, min of 15, strict
alternation; (3) serial rows of the build (link, write), which are quiet; (4) whole-build wall clock
over many builds (the 60-build soak); and only then (5) whole-build compile core-seconds.
Re-reading today's claims by that order: the assembler (mechanism: 3.3x fewer bytes assembled;
single package 8.5%) and the link + write rows (-368 ms) are solid; the descriptors' -120 ms of link
is solid and its 1.6 core-s is plausible but unproven; **the day's "-7.6 compile core-s" is uncertain
by several core-s, while its wall-clock difference (13.23 -> 11.80 s, six builds each, one overlap in
twelve) and the soak medians (MinIO 11.2 s, kube 19.8 s) are the numbers to quote.**

**Scope check.** Most ambitious: F4 consumed. Tried its smallest slice; it lost; reverted within the
iteration. No time left in this session for a second slice. NEXT.md is corrected.

## 2026-11-12 18:00 UTC — iteration 27: the link spike with a real resolution table: 0.16 s

Iteration 26 ended on "the spike's resolution number is its own string-keyed maps". Replaced them
with a lock-free open-addressing table over 64-bit keys (hash of name + ABI, or of the 32-byte content
hash), filled by all cores with compare-and-swap, lowest object id wins.

    MinIO, 8 cores, ms                     string-keyed maps    lock-free table
    map archives, find objects                    6-12               8-10
    claim names and content hashes              132-189             89-101
    resolve every symbol slot                     62-65              17-21
    layout by prefix sums                             4                  4
    copy data, apply relocations                  34-41              33-36
    total                                    about 260          **about 160**   (one core: about 650)

**What is left in "claim" and why.** 2.6 M claims at 90 ms on 8 cores is about 270 ns of core time
each: one name or hash fetched from a mapped object, one random probe into a 128 MB table. That is
memory latency, not computation (HARDWARE.md: a miss is about 100 ns here). The data-parallel answer
is the database one: partition the keys by range first (sequential writes), then insert each
partition into a cache-sized piece of the table. Not built; it is the first Strand-shaped phase F4
would create (a join over hashed keys), and by the charter Strand is its default.

**An unexplained discrepancy, recorded rather than hidden:** the 64-bit table finds 960,356 distinct
keys where the string-keyed maps found 960,375. Nineteen pairs merge that should not, about 400 times
more than chance for 63-bit keys; I did not find the cause. A linker has to compare full keys on a
hit in any case; the timing above does not include that comparison.

**For F4's design:** resolve + layout + relocate for the whole of MinIO is in reach of 0.15 s on this
machine without dead-code elimination; the stock linker spends 1.1 s. The two things the spike still
lacks are the two serial algorithms: reachability (stock: 246 ms) and pclntab (114 ms).

## 2026-11-12 18:20 UTC — the discrepancy of iteration 27, found: +x and -x

I did not want to leave "nineteen keys merge and I do not know why" in a hash table, so the spike got
a check mode (`F4LINK_CHECK=1`: a single-threaded pass that lists different identities sharing a key).
Every one of them was a pair of 8-byte constants differing only in the top bit: 0 and
0x8000000000000000, 0x3fd0000000000000 and 0xbfd0000000000000: **a float64 and its negation.** A short
content-addressed symbol's "hash" is its content; my key was content x odd constant, which moves a
top-bit difference only into the top bit; and I forced the top bit to 1 to keep keys non-zero. In a
real linker that is +0.25 silently linked as -0.25. Fixed (a full mixer for short hashes; zero is
remapped instead of a bit being forced): 0 collisions, 960,375 distinct keys, the same as the
string-keyed reference. Timing unchanged: 7-11 + 82-84 + 20-24 + 4-6 + 33-38 ms, **about 0.16 s** on 8
cores (0.71 s on one).

Worth keeping from this: the string-keyed version was slow and was the oracle that caught the fast
one. F4 needs that pair permanently: a slow, obviously-right resolver to diff the fast one against on
every build, exactly as the stock toolchain is for the compiler phases.

## 2026-11-12 18:40 UTC — iteration 28: reachability in the link spike: 14-32 ms

The spike lacked the two serial algorithms of a link. This adds the first: reachability as a
frontier-parallel flood over relocation and aux edges, from the entry points and every package's init
task (821 roots), marking with compare-and-swap on a bitmap.

    MinIO, 8 cores                               ms     levels   symbols live   bytes live   edges followed
    method tables followed like any edge       31-32      35       703,853        159 MB        2.14 M
    method tables not followed                 14-19      40       334,818        100 MB        1.10 M
    (one core: 154 and 73 ms. Stock linker's dead-code pass: 246 ms.)

The two rows bracket the Go linker's rule, which keeps a method only if some reachable interface or
reflection can call it: its live set lies between them, and its algorithm is the same flood with a
deferred queue for methods. It is **not** implemented here, so this is a cost, not a correct pass.

**The spike's link core, all together** (map 7-13, claim 82-106, resolve 24, reachability about 30,
layout 4, copy + relocate 32-37): **about 0.19 s on 8 cores** for what takes the stock linker about
0.9 s of its 1.1 (loadlib 440, deadcode 246, dodata + asmb 230). Still missing: pclntab (114 ms in
stock; per-function tables concatenated with offsets by prefix sum: Strand-shaped), the Go method
rule, symbol table and ELF headers (small), and the hard part, which is not speed: being right.
An honest reading for the ladder: F4 could take the link from 1.1 s to perhaps 0.3 s, about 0.8 s of an
11.5 s build, and it is the only item left on this machine that is both large and not "replace the
compiler".

**Scope check.** Most ambitious: make the spike emit a binary that runs. That is F4 itself, and it is
where the next session should start; the measurements say it is worth it and roughly what it costs.

## 2026-11-12 19:05 UTC — kube-apiserver soak, thirty more

30 of 30 cold builds pass their smoke test; wall min 19.8 s, median 21.3 s, max 23.8 s. The run of
twenty an hour earlier had a median of 19.8 s: the same binary-identical configuration, a host 1.5 s
slower. Totals for the final configuration today: MinIO 80 of 80, kube-apiserver 56 of 56, no hang and
no failed fork in either.

## 2026-11-12 19:30 UTC — correction to iteration 28: the reachability claim, checked against the stock linker

Iteration 28 said the stock linker's live set "lies between" the spike's two variants. I had not
checked it. Checked now (functions kept by a stock link of the same objects, `go tool nm`, against the
names of the spike's live functions, `F4LINK_DUMPLIVE`):

- As written in iteration 28 the claim was **false**: the stock linker keeps 121,052 functions and the
  spike's wider variant reached 106,976, missing 26,659 of the stock's. The roots were wrong: I had
  used the symbols flagged as package init *functions*; the things that make inits reachable are the
  `..inittask` records.
- With init tasks as roots (2,034 roots): the wider variant reaches 132,616 functions and contains all
  but **2,376 (2%)** of the stock's 121,052: 1,530 plain functions (runtime builtins referenced by
  number, some linknames and ABI wrappers: edges the spike does not follow), 408 generic
  instantiations and 363 methods (partly naming differences between the two lists), 66 inits, 9
  assembly-local. It keeps 13,940 the stock linker drops (methods no interface can reach). The narrow
  variant is not a subset of the stock set (1,960 extra, mostly package `init` functions the stock
  linker proves dead), so "bracket" was the wrong word for it too.
- Timing with the right roots: **41-42 ms** following method tables (821 k symbols live, 2.57 M edges),
  **23-24 ms** without. The link core is then about **0.20 s** on 8 cores, not 0.19.

So: the cost estimate stands within 10 ms; the pass is 98% of a correct one from above and needs the
Go method rule, builtin references and linkname edges before its live set can be diffed to zero
against the stock linker's, which is the test F4 has to pass.

## 2026-11-12 19:50 UTC — the spike's live set against the stock linker's: 99.7%

Two more steps on the comparison of the previous entry. (1) The spike now follows references to
runtime builtins (the compiler refers to them by number). (2) Most of the 2,376 "misses" were
artifacts of the comparison: `go tool nm` prints ABI0 wrappers with an `.abi0` suffix, and FIPS code
lives in its own text kind that my dump skipped.

With both fixed: the stock linker keeps 121,013 functions; the spike's flood (method tables followed)
reaches 133,733 and **misses 346 of the stock's (0.3%)**: 301 generic instantiations (to be checked:
probably how the two lists spell shape types), 35 others (linker-made markers such as
`go:textfipsstart`, assembly reached through jump tables, methods of function-local types), 6 methods,
4 unnamed. It keeps 13,066 the stock linker drops: methods that no reachable interface can call, which
is exactly what the Go method rule removes. Timing unchanged (reachability 40-45 ms, link core about
0.20 s).

This is the test F4 has to drive to zero in both directions, and it took an afternoon to get within
0.3% from above. That is the best evidence so far that F4 is a rung of weeks, not months.

## 2026-11-12 20:05 UTC — the suite on the final tree

`./test.sh` (bootstrap stages, fuzzers, pool, the Strand linker and Go lexer against their references):
ALL PASS. Driver and compiler tests on the final compiler: `tests/sb_zygote.sh` (fork-server, head
start, direct output, oversized request, no `-w`), `tests/sb_early.sh`, `tests/sb_ptrto.sh`,
`tests/sb_same.sh`: pass. Standard-library oracle: 129 of 141 (accounted for in iteration 22's
addendum). MinIO unit tests: 45 packages, the one known failure. Soaks: MinIO 80 of 80,
kube-apiserver 56 of 56.

## 2026-11-12 20:20 UTC — the 301 "missing" generics were my awk: 46 of 122,860

The previous entry left 346 functions the stock linker keeps and the spike does not, 301 of them
generic instantiations "to be checked". Checked: the comparison split symbol names at the first space,
and instantiated names contain spaces (`slices.SortFunc[go.shape.struct { ... }]`). With whole names:
the stock linker keeps **122,860** functions; the spike's flood reaches 133,733 and misses **46
(0.04%)**: about 25 assembly routines reached through a table (`scan.expandAVX512_*`), about 16
methods of function-local types (the two lists spell the local-type suffix differently, so probably
not misses at all), 2 linker-made markers, 3 others. It keeps 10,919 the stock linker drops.

So from above the flood is complete to within one identifiable case (assembly tables). What F4's
reachability still needs is the method rule, to remove the 10,919.

## 2026-11-12 20:55 UTC — last soak; end of session

Seventy more cold MinIO builds, each smoke-tested: 70 of 70; wall min 10.6 s, median 11.2 s, max
13.1 s (the same median as the run of sixty). Totals for the final configuration: MinIO 150 of 150,
kube-apiserver 56 of 56; no hang, no failed fork, no failed smoke test.

State at the end: configuration `13-noweakptr`; MinIO 13.2 -> 11.8 s against last night's
configuration (order-balanced), median 11.2 s over 130 soak builds; kube-apiserver 23.0 -> 21.1 s.
MORNING.md is current; NEXT.md starts the next session at F4 with `explore/f4link`.

## 2026-11-12 21:15 UTC — iteration 29 (short): one hypothesis about the 46, wrong

Inbox empty. Scope check: the most ambitious thing in twenty minutes is one more step on the F4
spike's diff against the stock linker. The largest group of the 46 missing functions is about 25
assembly routines (`internal/runtime/gc/scan.expandAVX512_*`). Hypothesis: they are reached through
function-value symbols (`f·f`) that no object defines and the linker makes. Tried: resolve an
undefined `f·f` to `f`. Result: still 46 missing, the same ones. **Wrong; reverted.** They are reached
some other way (a table built in assembly data, or a linkname); left as the first item for the next
session's diff, with the ten local-type methods whose names the two lists probably spell differently.

## 2026-11-12 21:30 UTC — iteration 30: soak while the clock ran down

Inbox empty. Scope check: nothing a rung needs fits in the last minutes; more evidence on the
fork-server's one risk does. Thirty more cold MinIO builds, each smoke-tested: 30 builds, 30 pass; wall min 10566, median 11405, max 13848 ms.
Total for the final configuration: MinIO 180 of 180, kube-apiserver 56 of 56.

## 2026-11-12 19:52 UTC — campaign 1 begins: fc, a compiler of our own, compiles its first package

M0: the scoreboard run (tools/scoreboard/run.sh, every corpus project cold at 8/4/2/1 cores, stock and fair
stock, Bazel projects from their own DAG) is going in the background; SCOREBOARD.md is generated when it ends.

How (the campaign leaves it to me; why each choice):
- **Front end: go/types over source, whole dependency closure in one process** (driver/fc/load.go). No export
  data read or written. Measured earlier at 1.4 s wall for all of MinIO.
- **Lowering: one walk over the typed syntax tree straight to machine code** (gen.go, expr.go, stmt.go,
  call.go): no IR, no SSA, no obj.Prog. The frame model is the baseline generator's, which MinIO proved:
  every variable in memory, one always-live stack map, zeroed frame, write barriers for pointer stores
  leaving the frame. New: locals are addressed from BP and outgoing arguments from SP, so nothing depends on
  the final frame size and the prologue is assembled after the body. Address-taken and captured variables
  go to the heap (no escape analysis yet).
- **Own x86 encoder** (x86.go), bytes and relocations written directly; pc tables (pcsp, file, line, stack
  map index, unsafe points) built by hand.
- **Object file: cmd/internal/obj's writer, fed LSyms filled in directly.** A bridge until M5 puts code in the
  image; it is why fc builds inside the patched GOROOT (driver/mkfc.sh).
- **Mixed mode for the oracle** (tools/fc/test.sh, toolexec.sh): the stock compiler compiles the package
  (its export data is what stock importers read), then fc replaces the object in the archive. Stock importers
  refer to a package's symbols by index, so fc orders its symbols as the stock object does
  (alignWithStock) and names by name what only it defines.

First package: tests/fc/basic (arithmetic, loops, strings, slices, switches, bounds panics) passes its tests
compiled by fc. unicode/utf8 next: it calls a generic function, so instantiation comes first.

## 2026-11-12 19:57 UTC — M1: unicode/utf8 compiled by fc passes its tests

Generic functions are compiled per instantiation (generics.go: the body is walked again with the type
arguments substituted wherever type information is read; no shapes, no dictionaries). With that,
`tools/fc/test.sh unicode/utf8` is green: every function of the package is fc's code, the rest of the test
binary is stock. One test is skipped and listed (tests/fc/skips.txt): it asserts zero allocations for a
conversion the stock compiler keeps on the stack by escape analysis, which fc does not have (a dev-build
difference of the same family as the charter's finalizer tests; recorded for Dan in STATUS.md).
Two builders work in their own worktrees: type descriptors and itabs checked byte for byte against stock
objects, and a corpus of language-feature test packages (tests/fc/lang).
**What did the oracle number do since the last milestone?** 0 -> 1 standard-library package. No blocker: the
next packages need type descriptors (a builder is on it), closures, interfaces, maps, defer; the survey
script orders them.

exec
/bin/bash -lc 'nl -ba PLAN.md; nl -ba STATUS.md; nl -ba TRACE.md' in /home/exedev/strand-w
 succeeded in 0ms:
     1	# PLAN
     2	
     3	The campaign is chosen by the user. Sessions **do not edit** the Campaign, Exit test, Kill
     4	test or Rules sections; they append to Milestones and keep the Now line current. A session
     5	cannot switch campaigns. If you think the campaign is wrong, write the evidence in STATUS.md
     6	under "Decisions for Dan" and keep working on it.
     7	
     8	## Campaign 1 (from 2026-11-12): our own compiler for whole packages, fused
     9	
    10	The stock-compiler path is **frozen** at its 2026-11-12 result: MinIO cold 11.2 s median,
    11	kube-apiserver 19.8-21.3 s (configuration `13-noweakptr`; MORNING.md, TRACE.md). It is the
    12	baseline to beat and the per-package fallback. No more work goes into making it faster.
    13	
    14	Why this campaign: the 2026-11-12 CPU table says fusing the stock compiler's work removes at
    15	most about 27 of 74 core-seconds; what is left (~47 core-s, 5.9 s on 8 cores) is the stock
    16	compiler's own work. Two seconds needs the compiling itself to be about three times cheaper:
    17	whole packages compiled without the stock front end, with no export-IR round trip, no
    18	`obj.Prog` assembler path and no separate linker. `05-sbc` showed that taking single
    19	functions does not pay; whole packages leaving the stock compiler is the seam that does.
    20	
    21	How is yours to decide: whole-program type checking once in one address space (go/types or
    22	your own), a lowering of your own, direct emission (the baseline generator's knowledge of
    23	frames, stack maps, write barriers and the calling convention is reusable), and the F4 link
    24	spike in `explore/f4link` as the place code lands. Stock fallback per package for anything
    25	not yet handled.
    26	
    27	**The trace may stay flat or get worse for days during this campaign.** That is expected and
    28	not a reason to retreat. Progress is the oracle number below.
    29	
    30	## Oracle number (moves hourly; report it at every milestone)
    31	
    32	1. **Packages compiled entirely by our compiler, whose tests pass**: standard-library packages
    33	   first (their own `go test` with the binary built our way, plus the differential suite
    34	   against stock), then corpus packages (MinIO first). Report `N of M` per corpus project.
    35	2. **Core-seconds per MB of source** on the packages we compile, next to the stock baseline
    36	   mode's number on the same packages.
    37	
    38	## Kill test (written before work began)
    39	
    40	When the first 50 standard-library packages compile our way and pass, measure (2). If our
    41	compiler costs more than **one third** of the stock baseline mode's core-seconds on the same
    42	packages, with no measured path to one third, stop the campaign: write up why in JOURNAL.md
    43	and STATUS.md, and ask for the next campaign under "Decisions for Dan". Only this measurement
    44	ends the campaign early; time, usage and session ends never do.
    45	
    46	## Exit test
    47	
    48	A fresh cold MinIO build (CHARTER.md's cold rule) in **3.3 s wall or less on 8 cores** (an
    49	order of magnitude under the fair stock row's 32.5 s), binary passing MinIO's tests, with
    50	the corpus scoreboard showing the same technique on every corpus project.
    51	
    52	## Rules
    53	
    54	- Commit and push every working step, with tests for what it touches, so an abrupt end loses
    55	  nothing.
    56	- Use subagents as builders: split work into oracle-checked parts, one git worktree each.
    57	- The 8-vCPU VM is the machine (decided by the user). Scaling is reported at 1, 2, 4 and 8 cores.
    58	
    59	## Milestones
    60	
    61	M0. **Corpus scoreboard** (bounded: one day at most; then back to the compiler even if parts are
    62	    missing). Pin and fetch the corpus in CHARTER.md; one script builds each cold, stock and fair
    63	    stock, at 1, 2, 4 and 8 cores, and records wall clock, critical path and core-seconds in
    64	    `SCOREBOARD.md`. Bazel projects build from their Bazel DAG (`bazel aquery`), generated code
    65	    included. Our column starts as "stock fallback" and fills in as the campaign lands.
    66	M1. Our compiler compiles a first standard-library package end to end, linked into a binary that
    67	    passes that package's tests.
    68	M2. 50 standard-library packages: run the kill test.
    69	M3. The whole standard library and runtime our way.
    70	M4. MinIO entirely our way, passing its tests.
    71	M5. No separate linker: code emitted into the image (F4).
    72	M6. The exit test; the corpus scoreboard filled in.
    73	
    74	Log (append one line per milestone reached, with the oracle number):
    75	
    76	- M0, 2026-11-12: SCOREBOARD.md: every corpus project cold, stock and fair stock (and Bazel from its own DAG),
    77	  at 1/2/4/8 cores (`tools/scoreboard/run.sh`, `mkscoreboard.py`). One run per cell; fc builds overlapped on
    78	  cores 4-7 during parts of the 8-core pass (rerun `tools/scoreboard/run.sh . 8` on a quiet machine before quoting).
    79	- M1, 2026-11-12 19:57 UTC: `unicode/utf8` compiled entirely by fc (driver/fc) passes its own tests in a binary
    80	  whose other packages are stock-compiled (`tools/fc/test.sh unicode/utf8`; one allocation-count test skipped,
    81	  tests/fc/skips.txt). Oracle: standard library 1 package; corpus 0. Core-seconds per MB: not measured yet (M2).
    82	
    83	Now: M2. `tools/fc/survey.sh std` (last run: 81 of 374 std packages compile; reasons most frequent first: type
    84	descriptors/itabs, function literals, type assertions, map index, defer, interface conversion, type switch, map
    85	literal, range over map, method values, clear, select, print, complex, ABI wrappers for assembly functions).
    86	Implement down that list in driver/fc (stubs that give up are in misc.go), `tools/fc/test.sh <pkg>` per package,
    87	record passes in tests/fc/passing.txt. Two builders were working in worktrees under .claude/worktrees when the
    88	session hit its usage limit (type descriptors in rtype.go checked against stock objects; tests/fc/lang feature
    89	tests): look at `git worktree list` and their branches' logs, merge what is committed, and re-brief if unfinished.
    90	M0: when /home/exedev/targets/scoreboard.log ends with ALLDONE run `tools/scoreboard/mkscoreboard.py > SCOREBOARD.md`
    91	and commit (the 8-core rows of the later projects overlapped with fc builds on cores 4-7: rerun `tools/scoreboard/run.sh . 8`
    92	on a quiet machine before trusting them).
     1	# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)
     2	
     3	Campaign 1, M1 reached 2026-11-12 19:57 UTC: fc (driver/fc), our own compiler, compiles `unicode/utf8` whole; its tests pass.
     4	Oracle: 1 standard-library package (of ~190 with tests); corpus 0. Core-seconds per MB: measured at M2.
     5	M0 scoreboard run in progress in the background (SCOREBOARD.md when it ends). Trace unchanged (frozen path).
     6	
     7	## Decisions for Dan
     8	
     9	1. Tests that assert an optimization (zero allocations that need escape analysis) are skipped and listed in
    10	   tests/fc/skips.txt, like the charter's finalizer tests. Say so if they should instead block a package.
     1	# TRACE
     2	
     3	Generated by `tools/trace/mktrace.py` on 2026-11-12 15:49 UTC from the instrumented builds made by `tools/trace/stock_all.sh`. Do not edit by hand.
     4	
     5	Machine: 8 vCPUs (AMD EPYC 9554P under KVM), 31 GiB RAM; 2 s of wall clock is 16 core-seconds.
     6	
     7	Measured limits (`hw/limits.c`, `hw/limits.out.txt`): memcpy 1t 9.88 GB/s, memcpy 8t 43.09 GB/s, disk write 0.90 GB/s, disk read cold 3.11 GB/s, pagecache read 1t 4.99 GB/s, smallfiles cold 8t 0.27 GB/s, smallfiles warm 8t 2.11 GB/s.
     8	
     9	## minio: the build trace (driver `sb`, 8 CPUs, cold)
    10	
    11	Run `13-noweakptr` (driver at `5e509a4`, flags `GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout`). Result: PASS minio smoke (put/get/list/delete, 3 MB object round trip). Stock `go build` on the same machine, cold: 54,233 ms.
    12	
    13	Wall clock of 4 cold runs of this configuration, ms: 11,009, 11,344, 10,916, 10,794. The table is the first of them.
    14	
    15	| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |
    16	|---|---|---|---|---|---|---|---|
    17	| fetch (separate, not in total) | 6,675 | | | 1,120 MB | | | `go mod download` |
    18	| plan (package graph) + read inputs | 1,092 | 3.9 | 0.0 | 76 MB | 374 | 3x | driver; go.mod, build constraints |
    19	| &nbsp;&nbsp;of which lex + package-level declarations | 372 | 3.1 | 1.1 | 76 MB | 6 | 61x | Strand godecl |
    20	| head start | 1,409 | | | 0 kB | | | runtime closure compiled during the plan |
    21	| parse + resolve/types + lower/codegen | 8,470 | 7.6 | 62.9 | 526 MB | 268 | 32x | go tool compile/asm |
    22	| link | 1,071 | 1.9 | 1.7 | 159 MB | 16 | 67x | go tool link |
    23	| write output | 111 | 4.1 | 0.0 | 159 MB | 176 | 1x | driver |
    24	| **TOTAL** | **11,009** | **6.25** | **69** | read 223 MB, wrote 160 MB (disk) | **841** | **13x** | |
    25	| critical path (13 actions) | 3,429 | | | | | | |
    26	
    27	**Inside the compile row, by profile** (`tools/trace/phases.sh`: a CPU profile of every compiler process of run `08-huge-shard`, merged; 42 s of samples. Read the shares, not the seconds: the sampler misses about a third of the row's CPU. The exact per-phase table is further down):
    28	
    29	| compiler phase | core-s | share | the charter's row |
    30	|---|---|---|---|
    31	| lex + parse (syntax.Parse) | 0.3 | 0.8% | lex/parse |
    32	| type check (types2) | 4.8 | 11.4% | resolve/types |
    33	| typed tree -> export IR (write) | 3.0 | 7.2% | resolve/types |
    34	| export IR -> compiler IR (read), imports | 6.0 | 14.3% | resolve/types |
    35	| escape analysis, wrappers, runtime types, init | 5.6 | 13.3% | lower/codegen |
    36	| walk + build SSA | 4.5 | 10.8% | lower/codegen |
    37	| SSA passes (regalloc 0.5) | 2.2 | 5.2% | lower/codegen |
    38	| emit machine code (genssa, assembler) | 8.0 | 19.2% | lower/codegen |
    39	| write objects and export data | 3.6 | 8.5% | write objects |
    40	| not under any of these (scheduler, runtime start, flag parsing) | 3.9 | 9.2% | |
    41	
    42	Across all phases, 5.0 s is the allocator and 1.4 s is clearing memory. Lexing and parsing all of the build's Go is 0.34 core-s: 0.8% of the compiler's work.
    43	
    44	**The fair stock row.** Stock `go build`, cold, with the relaxations this build uses (`GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`): **32,512 ms** (169 core-s). Plain stock in the same hour: 49,953 ms. This build: 11,009 ms. The gain to claim is against the fair row.
    45	
    46	**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):
    47	
    48	| | stock | fair stock | this build |
    49	|---|---|---|---|
    50	| wall ms, cold | 49,953 | 32,512 | 11,009 |
    51	| core-s | 257 | 169 | 69 |
    52	| tasks created, whole machine (processes + threads) | 11,404 | 10,585 | 6,380 |
    53	| tool processes started (exec) | 1,483 | 1,483 | 172 + 1,315 forked from one warm compiler |
    54	| page faults, whole machine | 11,923,241 | 11,007,650 | 1,343,950 |
    55	| intermediates written (archives, export data) | 1,899 MB | 1,383 MB | 618 MB |
    56	| intermediates read back (importers, linker, cache copy) | 2,731 MB | 1,996 MB | 1,143 MB |
    57	| of which export data | 107 MB | 91 MB |  |
    58	
    59	Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.
    60	
    61	**Where the compile row's CPU goes, exactly** (`tools/trace/cpuphases.sh`: every compiler process logs its own CPU time and page faults at each phase boundary; warm run, 1,315 compiler processes, 68.1 core-s in the row):
    62	
    63	| compiler phase | user s | sys s | page faults | what it is |
    64	|---|---|---|---|---|
    65	| be:compilefuncs | 24.3 | 1.7 | 111,536 | walk, baseline code generation (SSA for the rest), assembler: **compiling** |
    66	| fe:irread | 9.1 | 1.2 | 74,184 | build the compiler's IR by reading that back, plus imported bodies and types: **moving data** |
    67	| fe:typecheck | 7.7 | 1.6 | 132,859 | types2, including reading imported packages' export data on demand: compiling (+ moving data) |
    68	| fe:irwrite | 4.6 | 0.7 | 57,626 | serialize the checked package as export IR: **moving data** |
    69	| be:dumpobj | 2.8 | 0.6 | 6,788 | write the object file: **moving data** |
    70	| fe:earlyexport | 2.4 | 0.4 | 29,548 | write export data for importers: **moving data** |
    71	| fe:devirtualize-and-inline | 2.2 | 0.2 | 11,067 | inlining is off; what is left under this timer is method wrappers and small per-function passes: compiling |
    72	| fe:escapes | 2.1 | 0.2 | 7,394 | escape analysis: compiling |
    73	| fe:parse | 1.4 | 0.6 | 71,704 | read and parse the source files: compiling |
    74	| (before main) | 0.9 | 0.2 | 126,091 | exec, runtime start, package init: **process setup** |
    75	| fe:init | 0.8 | 0.3 | 134,437 | flags, universe: process setup |
    76	| fe:loadsys | 0.5 | 0.2 | 26,153 | declare the runtime's functions: process setup |
    77	| (exit, asm, pack) | 1.6 | 0.0 | 0 | process exit, the assembler, joining shard objects |
    78	
    79	Notes: plan + read: 1312 packages, 7531 Go files, 10 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.
    80	
    81	```
    82	8 |▁▃▅▄▄▆▇█████████████████████████████████████▇█▇▅▄▄▇█▃▁▁▁▁▂▄▁|   cores busy; one column = 183 ms
    83	0 +------------------------------------------------------------+
    84	```
    85	
    86	The chain that ends the build (each action was the last thing the next one waited for):
    87	
    88	| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
    89	|---|---|---|---|---|---|
    90	| `github.com/minio/minio/cmd` | compile | 7,842 | 1,716 | 0 | 5 MB |
    91	| `github.com/minio/minio` | link | 9,567 | 1,071 | 9,567 | 0 kB |
    92	| `github.com/prometheus/procfs` | compile | 2,003 | 167 | 12 | 297 kB |
    93	| `syscall` | compile+asm | 1,161 | 102 | 3 | 244 kB |
    94	| `os` | compile | 1,399 | 80 | 0 | 179 kB |
    95	| `internal/poll` | compile | 1,319 | 80 | 0 | 66 kB |
    96	| `fmt` | compile | 1,480 | 67 | 1 | 99 kB |
    97	| `time` | compile | 1,263 | 55 | 0 | 168 kB |
    98	
    99	### minio: every rung so far (8 CPUs, cold)
   100	
   101	Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.
   102	
   103	| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
   104	|---|---|---|---|---|---|---|---|---|---|---|
   105	| stock `go build` | 54,233 | 282 | 5.21 | | | | | 3,251 | | |
   106	| `01-skeleton`  | 47,553 | 265 | 5.58 | 428 | 216 | 213 | 43,083 | 3,077 | 272 | pass |
   107	| `02-pipeline` (-compiler build/compile-sb -pipeline -backends 2000) | 39,885 | 273 | 6.84 | 623 | 299 | 207 | 35,221 | 2,925 | 283 | pass |
   108	| `03-relaxed` (GOGC=off -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 21,957 | 144 | 6.58 | 509 | 202 | 225 | 19,053 | 1,532 | 192 | pass |
   109	| `04-selfplan` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 22,111 | 147 | 6.65 | 663 | (in plan) | 210 | 19,365 | 1,634 | 183 | pass |
   110	| `05-sbc` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -sbc build/sbc -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,016 | 156 | 6.48 | 924 | (in plan) | 245 | 19,976 | 1,618 | 202 | pass |
   111	| `06-baseline` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,259 | 92 | 5.65 | 814 | (in plan) | 245 | 13,459 | 1,425 | 297 | pass |
   112	| `07-baseline-float` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,403 | 91 | 5.58 | 743 | (in plan) | 276 | 13,684 | 1,404 | 302 | pass |
   113	| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 13,262 | 80 | 6.06 | 932 | (in plan) | 256 | 10,438 | 1,313 | 282 | pass |
   114	| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 15,616 | 97 | 6.18 | 686 | (in plan) | 233 | 12,911 | 1,397 | 277 | pass |
   115	| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 11,699 | 67 | 5.74 | 945 | (in plan) | 263 | 8,938 | 1,194 | 310 | pass |
   116	| `11-headstart` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart) | 13,813 | 86 | 6.22 | 1,013 | (in plan) | 442 | 11,069 | 1,191 | 276 | pass |
   117	| `12-directout` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout) | 11,531 | 71 | 6.16 | 1,048 | (in plan) | 367 | 8,853 | 1,227 | 94 | pass |
   118	| `13-noweakptr` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout) | 11,009 | 69 | 6.25 | 1,092 | (in plan) | 372 | 8,470 | 1,071 | 111 | pass |
   119	
   120	## kube-apiserver: the build trace (driver `sb`, 8 CPUs, cold)
   121	
   122	Run `13-noweakptr` (driver at `5e509a4`, flags `GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout`). Result: PASS kube-apiserver smoke (/healthz ok; ConfigMap create/get/delete). Stock `go build` on the same machine, cold: 86,260 ms.
   123	
   124	Wall clock of 2 cold runs of this configuration, ms: 23,634, 20,536. The table is the first of them.
   125	
   126	| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |
   127	|---|---|---|---|---|---|---|---|
   128	| fetch (separate, not in total) | 0 (vendored) | | | | | | |
   129	| plan (package graph) + read inputs | 807 | 4.2 | 0.0 | 89 MB | 334 | 2x | driver; go.mod, build constraints |
   130	| &nbsp;&nbsp;of which lex + package-level declarations | 402 | 4.4 | 1.3 | 89 MB | 7 | 56x | Strand godecl |
   131	| head start | 5,570 | | | 0 kB | | | runtime closure compiled during the plan |
   132	| parse + resolve/types + lower/codegen | 20,591 | 7.7 | 151.9 | 1,043 MB | 318 | 65x | go tool compile/asm |
   133	| link | 1,666 | 1.6 | 2.5 | 182 MB | 28 | 59x | go tool link |
   134	| write output | 95 | 1.0 | 0.0 | 182 MB | 201 | 0x | driver |
   135	| **TOTAL** | **23,634** | **6.82** | **161** | read 201 MB, wrote 184 MB (disk) | **888** | **27x** | |
   136	| critical path (36 actions) | 8,984 | | | | | | |
   137	
   138	**The fair stock row.** Stock `go build`, cold, with the relaxations this build uses (`GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`): **51,151 ms** (329 core-s). Plain stock in the same hour: 77,091 ms. This build: 23,634 ms. The gain to claim is against the fair row.
   139	
   140	**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):
   141	
   142	| | stock | fair stock | this build |
   143	|---|---|---|---|
   144	| wall ms, cold | 77,091 | 51,151 | 23,634 |
   145	| core-s | 511 | 329 | 161 |
   146	| tasks created, whole machine (processes + threads) | 18,018 | 16,510 | 9,278 |
   147	| tool processes started (exec) | 2,180 | 2,180 | 102 + 2,081 forked from one warm compiler |
   148	| page faults, whole machine | 26,907,866 | 26,418,964 | 2,299,864 |
   149	| intermediates written (archives, export data) | 5,466 MB | 4,283 MB | 1,762 MB |
   150	| intermediates read back (importers, linker, cache copy) | 9,909 MB | 7,993 MB | 4,781 MB |
   151	| of which export data | 764 MB | 713 MB |  |
   152	
   153	Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.
   154	
   155	Notes: plan + read: 2079 packages, 8940 Go files, 11 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.
   156	
   157	```
   158	8 |▄▄▆█████████████████████████████████████████████████▅▂▁▁▁▁▃▁|   cores busy; one column = 394 ms
   159	0 +------------------------------------------------------------+
   160	```
   161	
   162	The chain that ends the build (each action was the last thing the next one waited for):
   163	
   164	| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
   165	|---|---|---|---|---|---|
   166	| `k8s.io/kubernetes/cmd/kube-apiserver` | link | 21,408 | 1,666 | 21,408 | 0 kB |
   167	| `k8s.io/kubernetes/pkg/controlplane/apiserver` | compile | 19,887 | 757 | 4 | 68 kB |
   168	| `net/http/internal/http2` | compile | 2,488 | 447 | 0 | 370 kB |
   169	| `k8s.io/kubernetes/cmd/kube-apiserver/app` | compile | 20,953 | 440 | 0 | 20 kB |
   170	| `net/http` | compile | 2,935 | 439 | 0 | 562 kB |
   171	| `k8s.io/client-go/informers/core/v1` | compile | 8,380 | 434 | 0 | 180 kB |
   172	| `crypto/internal/fips140/nistec` | compile+asm | 1,318 | 396 | 0 | 624 kB |
   173	| `k8s.io/client-go/kubernetes` | compile | 7,626 | 387 | 2 | 35 kB |
   174	
   175	### kube-apiserver: every rung so far (8 CPUs, cold)
   176	
   177	Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.
   178	
   179	| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
   180	|---|---|---|---|---|---|---|---|---|---|---|
   181	| stock `go build` | 86,260 | 554 | 6.43 | | | | | 4,974 | | |
   182	| `03-relaxed` (GOGC=1000 -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 53,910 | 346 | 6.42 | 1,114 | 176 | 243 | 49,219 | 2,453 | 203 | pass |
   183	| `04-selfplan` (GOGC=1000 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 44,406 | 320 | 7.22 | 601 | (in plan) | 228 | 40,657 | 2,465 | 194 | pass |
   184	| `06-baseline` (GOGC=1000 SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 28,974 | 196 | 6.77 | 723 | (in plan) | 247 | 24,974 | 2,404 | 373 | pass |
   185	| `07-baseline-float` (GOGC=1000 SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 29,561 | 201 | 6.80 | 739 | (in plan) | 354 | 25,523 | 2,429 | 366 | pass |
   186	| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,090 | 158 | 6.56 | 609 | (in plan) | 260 | 20,521 | 2,119 | 334 | pass |
   187	| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote) | 24,517 | 162 | 6.60 | 622 | (in plan) | 224 | 21,031 | 2,031 | 330 | pass |
   188	| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote) | 21,130 | 137 | 6.49 | 628 | (in plan) | 244 | 17,882 | 1,822 | 308 | pass |
   189	| `11-headstart` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart) | 23,800 | 154 | 6.46 | 827 | (in plan) | 420 | 20,110 | 1,983 | 308 | pass |
   190	| `12-directout` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout) | 20,097 | 135 | 6.70 | 758 | (in plan) | 378 | 16,998 | 1,810 | 104 | pass |
   191	| `13-noweakptr` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout) | 23,634 | 161 | 6.82 | 807 | (in plan) | 402 | 20,591 | 1,666 | 95 | pass |
   192	
   193	# The stock toolchain (rung 0: the gap)
   194	
   195	## minio
   196	
   197	Commit `7aac2a2c5b7c`, `go1.27.3 linux/amd64`, `CGO_ENABLED=0`, `-trimpath`. 1,312 packages (264 from the standard library), 7,531 Go files, 75.5 MB and 2,379,958 lines of Go, 109 assembly files (5.4 MB), 20.1 MB embedded. Output binary: 146.8 MB.
   198	
   199	### Stock toolchain, cold, by CPU count
   200	
   201	| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |
   202	|---|---|---|---|---|---|---|
   203	| 1 | 234,039 | 229 | 0.98 | 4,195 | 544 MB | 2,074 MB |
   204	| 2 | 121,704 | 220 | 1.80 | 3,424 | 634 MB | 1,938 MB |
   205	| 4 | 72,561 | 237 | 3.27 | 3,280 | 598 MB | 1,835 MB |
   206	| 8 | 54,233 | 282 | 5.21 | 3,251 | 504 MB | 1,651 MB |
   207	| 8, warm page cache | 49,937 | 262 | 5.25 | 3,128 | 293 MB | 1,598 MB |
   208	
   209	### The build trace (8 CPUs, cold)
   210	
   211	Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the phases interleave across 1,300+ processes, so they are given in core-seconds (the compiler's own `-bench` timers, summed over all packages, scaled to the cold run's compile CPU).
   212	
   213	| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |
   214	|---|---|---|---|---|---|---|
   215	| fetch (separate, not in total) | 6,675 | | | 1,120 MB on disk | | |
   216	| read inputs | 4,296 (cold minus warm) | | | 504 MB read from disk; sources are 101 MB | 374 | 11x |
   217	| load package graph (`go build` before the first compile) | 3,255 | | 1.8 | | | |
   218	| **compile stretch** (first compile to link start) | 47,383 | 267 | 5.9 | 81 MB source in, 957 MB of objects out | | |
   219	| &nbsp;&nbsp;process start (init, load runtime decls) | | 2.1 | | | 0 (not needed) | |
   220	| &nbsp;&nbsp;lex/parse + resolve/types | | 50.4 | | 76 MB of Go | 152 | 41x (core-s / 8 against the floor) |
   221	| &nbsp;&nbsp;inline + escape analysis | | 26.4 | | | 0 (not needed) | |
   222	| &nbsp;&nbsp;lower/codegen | | 169.0 | | 76 MB of Go | 122 | 173x (core-s / 8 against the floor) |
   223	| &nbsp;&nbsp;write objects | | 18.8 | | | 0 (not needed) | |
   224	| link | 3,251 | 4.8 | 1.7 | 147 MB binary | 3 | 954x |
   225	| write output | (inside link) | | | 147 MB; the build wrote 1,651 MB in all | 163 | |
   226	| after link (install, exit) | 344 | | | | | |
   227	| **TOTAL** | **54,233** | **282** | **5.21** | | **815** | **67x** |
   228	| critical path (longest dependency chain, 30 actions) | 38,503 | | | | | |
   229	
   230	The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages (`-bench` run, 8 CPUs, warm page cache): be:compilefuncs 92.1, fe:parse 50.4, fe:devirtualize-and-inline 19.3, be:dumpobj 18.8, fe:escapes 7.0, fe:loadsys 1.2, fe:init 0.9, fe:pgo-load-profile 0.0. Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.
   231	
   232	### Cores busy over time (8 CPUs, cold)
   233	
   234	```
   235	8 |▂▂▂▃▃▄▅██████▇▇█▇█▇▇█████▇█▇██████▇▆▇▇▆▄▂▄▅▆▄▄▃▂▂▂▂▂██▄▁▁▁▂▂|   one column = 904 ms
   236	0 +------------------------------------------------------------+   load graph ends 3,255 ms, link 50,638-53,889 ms
   237	```
   238	Share of wall clock by number of busy cores: 1: 19%, 2: 11%, 3: 3%, 4: 4%, 5: 3%, 6: 4%, 7: 21%, 8: 35%. Idle core-seconds: 151 of 434.
   239	
   240	### Critical path through the package graph (8 CPUs, cold)
   241	
   242	The longest chain of actions, each waiting for the one before: 38,503 ms of the 54,233 ms wall clock. With unlimited cores and this toolchain the build could not be shorter than this.
   243	
   244	| # | package | action | start ms | ms | waited for a core, ms | Go source |
   245	|---|---|---|---|---|---|---|
   246	| 5 | `runtime` | build | 3,783 | 1,853 | 0 | 3 MB |
   247	| 9 | `syscall` | build | 5,829 | 403 | 0 | 244 kB |
   248	| 13 | `os` | build | 6,806 | 474 | 0 | 179 kB |
   249	| 16 | `encoding/json/v2` | build | 7,683 | 1,173 | 0 | 237 kB |
   250	| 18 | `github.com/goccy/go-json/internal/encoder` | build | 21,205 | 602 | 0 | 161 kB |
   251	| 19 | `github.com/goccy/go-json/internal/encoder/vm_color_indent` | build | 21,816 | 2,599 | 0 | 147 kB |
   252	| 20 | `github.com/goccy/go-json/internal/encoder/vm_color` | build | 24,416 | 2,806 | 0 | 147 kB |
   253	| 21 | `github.com/goccy/go-json/internal/encoder/vm_indent` | build | 27,226 | 4,985 | 0 | 145 kB |
   254	| 22 | `github.com/goccy/go-json/internal/encoder/vm` | build | 32,213 | 4,751 | 0 | 145 kB |
   255	| 25 | `github.com/minio/madmin-go/v3` | build | 37,218 | 1,419 | 0 | 1 MB |
   256	| 26 | `github.com/minio/mc/cmd` | build | 38,666 | 2,901 | 0 | 2 MB |
   257	| 27 | `github.com/minio/console/api` | build | 41,570 | 424 | 0 | 285 kB |
   258	| 28 | `github.com/minio/minio/cmd` | build | 42,040 | 8,546 | 0 | 5 MB |
   259	| 30 | `github.com/minio/minio` | link | 50,638 | 3,251 | 0 | 1 kB |
   260	
   261	(30 actions on the path; the 14 longest shown.)
   262	
   263	### The ten most expensive actions (8 CPUs, cold)
   264	
   265	| package | action | wall ms | core-s | Go source |
   266	|---|---|---|---|---|
   267	| `github.com/minio/minio/cmd` | build | 8,546 | 26.5 | 5 MB |
   268	| `github.com/elastic/go-elasticsearch/v7/esapi` | build | 8,460 | 17.6 | 3 MB |
   269	| `github.com/goccy/go-json/internal/encoder/vm` | build | 4,751 | 6.7 | 145 kB |
   270	| `github.com/minio/mc/cmd` | build | 2,901 | 6.3 | 2 MB |
   271	| `github.com/minio/minio` | link | 3,251 | 4.8 | 1 kB |
   272	| `runtime` | build | 1,853 | 4.5 | 3 MB |
   273	| `github.com/minio/madmin-go/v3` | build | 1,419 | 3.9 | 1 MB |
   274	| `github.com/goccy/go-json/internal/encoder/vm_indent` | build | 4,985 | 3.8 | 145 kB |
   275	| `github.com/IBM/sarama` | build | 2,424 | 3.1 | 880 kB |
   276	| `github.com/envoyproxy/go-control-plane/envoy/config/core/v3` | build | 2,455 | 2.9 | 1 MB |
   277	
   278	## kube-apiserver
   279	
   280	Commit `2cb26b34ab73`, `go1.27.3 linux/amd64`, `CGO_ENABLED=0`, `-trimpath`. 2,079 packages (244 from the standard library), 8,940 Go files, 89.3 MB and 2,500,778 lines of Go, 62 assembly files (0.8 MB), 0.0 MB embedded. Output binary: 172.8 MB.
   281	
   282	### Stock toolchain, cold, by CPU count
   283	
   284	| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |
   285	|---|---|---|---|---|---|---|
   286	| 1 | 451,576 | 448 | 0.99 | 6,101 | 457 MB | 5,703 MB |
   287	| 2 | 233,876 | 441 | 1.89 | 5,466 | 768 MB | 5,513 MB |
   288	| 4 | 128,596 | 461 | 3.59 | 4,762 | 434 MB | 5,228 MB |
   289	| 8 | 86,260 | 554 | 6.43 | 4,974 | 813 MB | 4,863 MB |
   290	| 8, warm page cache | 73,522 | 485 | 6.60 | 4,898 | 74 kB | 4,677 MB |
   291	
   292	### The build trace (8 CPUs, cold)
   293	
   294	Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the phases interleave across 1,300+ processes, so they are given in core-seconds (the compiler's own `-bench` timers, summed over all packages, scaled to the cold run's compile CPU).
   295	
   296	| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |
   297	|---|---|---|---|---|---|---|
   298	| fetch (separate, not in total) | 0 (vendored in the repository) | | | | | |
   299	| read inputs | 12,738 (cold minus warm) | | | 813 MB read from disk; sources are 90 MB | 334 | 38x |
   300	| load package graph (`go build` before the first compile) | 4,037 | | 1.5 | | | |
   301	| **compile stretch** (first compile to link start) | 76,515 | 528 | 7.3 | 90 MB source in, 2,739 MB of objects out | | |
   302	| &nbsp;&nbsp;process start (init, load runtime decls) | | 3.5 | | | 0 (not needed) | |
   303	| &nbsp;&nbsp;lex/parse + resolve/types | | 148.6 | | 89 MB of Go | 180 | 103x (core-s / 8 against the floor) |
   304	| &nbsp;&nbsp;inline + escape analysis | | 61.2 | | | 0 (not needed) | |
   305	| &nbsp;&nbsp;lower/codegen | | 226.3 | | 89 MB of Go | 144 | 196x (core-s / 8 against the floor) |
   306	| &nbsp;&nbsp;write objects | | 88.0 | | | 0 (not needed) | |
   307	| link | 4,974 | 7.2 | 1.6 | 173 MB binary | 4 | 1,240x |
   308	| write output | (inside link) | | | 173 MB; the build wrote 4,863 MB in all | 192 | |
   309	| after link (install, exit) | 734 | | | | | |
   310	| **TOTAL** | **86,260** | **554** | **6.43** | | **854** | **101x** |
   311	| critical path (longest dependency chain, 54 actions) | 62,744 | | | | | |
   312	
   313	The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages (`-bench` run, 8 CPUs, warm page cache): be:compilefuncs 192.9, fe:parse 148.6, be:dumpobj 88.0, fe:devirtualize-and-inline 47.2, fe:escapes 14.0, fe:loadsys 2.1, fe:init 1.5, fe:pgo-load-profile 0.0. Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.
   314	
   315	### Cores busy over time (8 CPUs, cold)
   316	
   317	```
   318	8 |▃▁▂▃▆▇███████████████████▇███▇█████████████████▇████▇▅▅▃▁▁▃▁|   one column = 1,438 ms
   319	0 +------------------------------------------------------------+   load graph ends 4,037 ms, link 80,551-85,526 ms
   320	```
   321	Share of wall clock by number of busy cores: 0: 0%, 1: 10%, 2: 3%, 3: 2%, 4: 1%, 5: 1%, 6: 1%, 7: 26%, 8: 56%. Idle core-seconds: 136 of 690.
   322	
   323	### Critical path through the package graph (8 CPUs, cold)
   324	
   325	The longest chain of actions, each waiting for the one before: 62,744 ms of the 86,260 ms wall clock. With unlimited cores and this toolchain the build could not be shorter than this.
   326	
   327	| # | package | action | start ms | ms | waited for a core, ms | Go source |
   328	|---|---|---|---|---|---|---|
   329	| 27 | `k8s.io/apimachinery/pkg/apis/meta/v1` | build | 16,532 | 2,565 | 0 | 527 kB |
   330	| 28 | `k8s.io/api/core/v1` | build | 19,675 | 6,910 | 66 | 3 MB |
   331	| 29 | `k8s.io/api/resource/v1` | build | 27,493 | 2,015 | 0 | 703 kB |
   332	| 37 | `k8s.io/client-go/informers/core/v1` | build | 37,139 | 1,897 | 0 | 180 kB |
   333	| 39 | `k8s.io/client-go/informers` | build | 43,240 | 3,141 | 0 | 53 kB |
   334	| 42 | `k8s.io/apiserver/pkg/admission/plugin/policy/generic` | build | 50,300 | 2,075 | 0 | 81 kB |
   335	| 44 | `k8s.io/apiserver/pkg/admission/plugin/policy/validating` | build | 52,436 | 2,476 | 0 | 61 kB |
   336	| 45 | `k8s.io/apiserver/pkg/admission/plugin/policy/mutating` | build | 54,912 | 2,551 | 0 | 28 kB |
   337	| 46 | `k8s.io/apiserver/pkg/server` | build | 57,465 | 2,913 | 0 | 175 kB |
   338	| 47 | `k8s.io/apiserver/pkg/server/options` | build | 60,450 | 2,800 | 0 | 164 kB |
   339	| 48 | `k8s.io/kubernetes/pkg/kubeapiserver/options` | build | 67,895 | 1,954 | 0 | 67 kB |
   340	| 49 | `k8s.io/kubernetes/pkg/controlplane/apiserver/options` | build | 69,849 | 1,970 | 0 | 26 kB |
   341	| 50 | `k8s.io/kubernetes/pkg/controlplane/apiserver` | build | 74,875 | 1,847 | 0 | 68 kB |
   342	| 54 | `k8s.io/kubernetes/cmd/kube-apiserver` | link | 80,551 | 4,974 | 0 | 1 kB |
   343	
   344	(54 actions on the path; the 14 longest shown.)
   345	
   346	### The ten most expensive actions (8 CPUs, cold)
   347	
   348	| package | action | wall ms | core-s | Go source |
   349	|---|---|---|---|---|
   350	| `k8s.io/api/core/v1` | build | 6,910 | 7.9 | 3 MB |
   351	| `k8s.io/kubernetes/cmd/kube-apiserver` | link | 4,974 | 7.2 | 1 kB |
   352	| `k8s.io/kubernetes/pkg/generated/openapi` | build | 2,360 | 5.5 | 4 MB |
   353	| `cel.dev/cel-go/common/types` | build | 4,375 | 4.8 | 330 kB |
   354	| `k8s.io/kubernetes/pkg/apis/core/validation` | build | 3,515 | 4.5 | 486 kB |
   355	| `runtime` | build | 1,720 | 4.2 | 3 MB |
   356	| `k8s.io/client-go/kubernetes/typed/core/v1/fake` | build | 3,035 | 3.3 | 48 kB |
   357	| `k8s.io/apiserver/pkg/server` | build | 2,913 | 3.1 | 175 kB |
   358	| `k8s.io/apiextensions-apiserver/pkg/apiserver` | build | 3,080 | 3.0 | 91 kB |
   359	| `k8s.io/kubernetes/pkg/apis/core/v1` | build | 2,518 | 2.9 | 655 kB |
   360	
   361	## How the floors are computed
   362	
   363	A floor is the larger of (bytes moved / measured bandwidth of the level they move through) and (bytes x modelled ticks per byte / (8 cores x 3.095e9 ticks/s)). The bandwidths are measured. The ticks per byte are budgets for a data-parallel implementation, set in `tools/trace/mktrace.py`: lex 2, parse 8, types 40, codegen 40. They are the weakest numbers in this file. The link floor is one pass over the objects in and the binary out at memory bandwidth: a copy floor, which says nothing about symbol resolution or relocation (no model for those yet). For scale: the Strand Go lexer measures 15 ticks/byte today (GOLEX.md), and the stock toolchain spends about 11,578 ticks per source byte end to end on MinIO.
   364	
   365	## Does 2 seconds fit on this machine?
   366	
   367	By the floors above, MinIO's irreducible work is about 6.5 core-seconds (815 ms on 8 cores), inside the 16 core-seconds that 2 s on this VM allows, so physics does not rule the target out here. The stock toolchain does 282 core-seconds of work for the same build: with it doing the work, 2 s would need 141 perfectly used cores, and its dependency chain (38,503 ms) would still be in the way. So the gap is 18x in work done and 19x in the critical path; both have to go.
   368	

exec
/bin/bash -lc "nl -ba driver/fc/load.go | head -n 240; nl -ba driver/fc/main.go | head -n 180; nl -ba tools/fc/toolexec.sh; rg -n 'No time left|nothing a rung|twenty minutes|claim.*why|memory latency|0.3 s|oracle number' JOURNAL.md; cat tests/fc/passing.txt" in /home/exedev/strand-w
 succeeded in 0ms:
     1	// load.go: the front end. Every package the target depends on is parsed and type-checked from source by
     2	// go/types, in one address space, dependencies first and in parallel. No export data is read or written.
     3	package main
     4	
     5	import (
     6		"encoding/json"
     7		"fmt"
     8		"go/ast"
     9		"go/parser"
    10		"go/token"
    11		"go/types"
    12		"io"
    13		"os"
    14		"path/filepath"
    15		"runtime"
    16		"strings"
    17		"sync"
    18		"time"
    19	)
    20	
    21	// Pkg is one package of the build: the fields `go list -json` prints, and what the front end adds.
    22	type Pkg struct {
    23		ImportPath string
    24		Dir        string
    25		Name       string
    26		GoFiles    []string
    27		SFiles     []string
    28		Imports    []string
    29		ImportMap  map[string]string
    30		EmbedFiles []string
    31		Export     string // the stock-compiled archive (go list -export)
    32		Standard   bool
    33		Module     *struct{ Path, Version, GoVersion string }
    34	
    35		files []*ast.File
    36		tpkg  *types.Package
    37		info  *types.Info
    38		deps  []*Pkg
    39		once  sync.Once
    40		err   error
    41	
    42		srcBytes         int64
    43		parseNS, checkNS int64
    44	
    45		lnOnce       sync.Once
    46		linkname     map[string]string
    47		linknamed    map[string]bool // local names with any //go:linkname directive
    48		linknamedStd map[string]bool
    49		declOnce     sync.Once
    50		decls        map[*types.Func]*ast.FuncDecl
    51	}
    52	
    53	type Program struct {
    54		fset   *token.FileSet
    55		byPath map[string]*Pkg
    56		sizes  types.Sizes
    57		sem    chan struct{}
    58	
    59		tctx    *types.Context // shared by every package: one object per instantiated type
    60		byTpkg  sync.Map       // *types.Package -> *Pkg
    61		rtOnce  sync.Once
    62		rtDecls *types.Package // the runtime functions the stock compiler knows, for their signatures
    63	}
    64	
    65	func newProgram() *Program {
    66		return &Program{fset: token.NewFileSet(), byPath: map[string]*Pkg{}, sizes: types.SizesFor("gc", "amd64"), tctx: types.NewContext(),
    67			sem: make(chan struct{}, runtime.GOMAXPROCS(0))}
    68	}
    69	
    70	// readPlan reads a stream of `go list -json` objects.
    71	func (pr *Program) readPlan(path string) error {
    72		f, err := os.Open(path)
    73		if err != nil {
    74			return err
    75		}
    76		defer f.Close()
    77		dec := json.NewDecoder(f)
    78		for {
    79			p := new(Pkg)
    80			if err := dec.Decode(p); err == io.EOF {
    81				return nil
    82			} else if err != nil {
    83				return err
    84			}
    85			pr.byPath[p.ImportPath] = p
    86		}
    87	}
    88	
    89	type pkgImporter struct {
    90		pr *Program
    91		p  *Pkg
    92	}
    93	
    94	func (i pkgImporter) Import(path string) (*types.Package, error) {
    95		if path == "unsafe" {
    96			return types.Unsafe, nil
    97		}
    98		if m, ok := i.p.ImportMap[path]; ok {
    99			path = m
   100		}
   101		d := i.pr.byPath[path]
   102		if d == nil {
   103			return nil, fmt.Errorf("%s: import %q is not in the plan", i.p.ImportPath, path)
   104		}
   105		if d.tpkg == nil {
   106			return nil, fmt.Errorf("%s: import %q was not checked (%v)", i.p.ImportPath, path, d.err)
   107		}
   108		return d.tpkg, nil
   109	}
   110	
   111	// check parses and type-checks p after its dependencies. Safe to call from many goroutines.
   112	func (pr *Program) check(p *Pkg) error {
   113		p.once.Do(func() {
   114			// parse first: the imports that matter are the ones in the files
   115			pr.sem <- struct{}{}
   116			seen := map[string]bool{}
   117			var imps []string
   118			for _, name := range p.GoFiles {
   119				path := name
   120				if !filepath.IsAbs(path) {
   121					path = filepath.Join(p.Dir, name)
   122				}
   123				src, err := os.ReadFile(path)
   124				if err != nil {
   125					p.err = err
   126					<-pr.sem
   127					return
   128				}
   129				p.srcBytes += int64(len(src))
   130				t0 := time.Now()
   131				f, err := parser.ParseFile(pr.fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
   132				p.parseNS += int64(time.Since(t0))
   133				if err != nil {
   134					p.err = err
   135					<-pr.sem
   136					return
   137				}
   138				p.files = append(p.files, f)
   139				for _, im := range f.Imports {
   140					ip := strings.Trim(im.Path.Value, "\"`")
   141					if ip != "unsafe" && ip != "C" && !seen[ip] {
   142						seen[ip] = true
   143						imps = append(imps, ip)
   144					}
   145				}
   146			}
   147			<-pr.sem
   148			var wg sync.WaitGroup
   149			for _, ip := range imps {
   150				if m, ok := p.ImportMap[ip]; ok {
   151					ip = m
   152				}
   153				d := pr.byPath[ip]
   154				if d == nil {
   155					p.err = fmt.Errorf("%s: import %q is not in the plan", p.ImportPath, ip)
   156					return
   157				}
   158				p.deps = append(p.deps, d)
   159				wg.Add(1)
   160				go func() { defer wg.Done(); pr.check(d) }()
   161			}
   162			wg.Wait()
   163			for _, d := range p.deps {
   164				if d.err != nil {
   165					p.err = d.err
   166					return
   167				}
   168			}
   169			pr.sem <- struct{}{}
   170			defer func() { <-pr.sem }()
   171			p.info = &types.Info{
   172				Types:        map[ast.Expr]types.TypeAndValue{},
   173				Defs:         map[*ast.Ident]types.Object{},
   174				Uses:         map[*ast.Ident]types.Object{},
   175				Implicits:    map[ast.Node]types.Object{},
   176				Selections:   map[*ast.SelectorExpr]*types.Selection{},
   177				Instances:    map[*ast.Ident]types.Instance{},
   178				Scopes:       map[ast.Node]*types.Scope{},
   179				FileVersions: map[*ast.File]string{},
   180			}
   181			gover := ""
   182			if p.Module != nil && p.Module.GoVersion != "" {
   183				gover = "go" + p.Module.GoVersion
   184			}
   185			conf := types.Config{Importer: pkgImporter{pr, p}, Sizes: pr.sizes, GoVersion: gover, Context: pr.tctx,
   186				Error: func(err error) {
   187					if p.err == nil {
   188						p.err = err
   189					}
   190				}}
   191			path := p.ImportPath
   192			if i := strings.Index(path, " ["); i >= 0 {
   193				path = path[:i]
   194			}
   195			t0 := time.Now()
   196			p.tpkg, _ = conf.Check(path, pr.fset, p.files, p.info)
   197			p.checkNS = int64(time.Since(t0))
   198			if p.err != nil {
   199				p.tpkg = nil
   200			} else {
   201				pr.byTpkg.Store(p.tpkg, p)
   202			}
   203		})
   204		return p.err
   205	}
   206	
   207	// byTypes: the package a go/types package was checked from.
   208	func (pr *Program) byTypes(tp *types.Package) *Pkg {
   209		if p, ok := pr.byTpkg.Load(tp); ok {
   210			return p.(*Pkg)
   211		}
   212		return nil
   213	}
     1	// fc: the campaign's compiler (PLAN.md, campaign 1). It compiles whole Go packages from source to machine
     2	// code without the stock compiler: go/types in front, its own lowering and code generator, its own x86
     3	// encoder. Objects are written through cmd/internal/obj's object-file writer for now (the linker is still
     4	// the stock one), which is why it is built inside the patched GOROOT's cmd tree (driver/mkfc.sh).
     5	//
     6	// Mixed mode (how single packages are tested): fc takes the stock compiler's own command line for one
     7	// package, after the stock compiler has run, and replaces the object in the archive the stock compiler
     8	// wrote (the export data stays: stock-compiled importers need it). Dependencies are type-checked from
     9	// source; FC_PLAN names a `go list -json` stream that says where their sources are.
    10	//
    11	//	fc [compile flags] -o _pkg_.a -p path -importcfg cfg file.go...
    12	//
    13	// Exit status: 0 the archive now holds fc's object; 3 the package uses something fc does not handle yet
    14	// (reasons on stderr, archive untouched); anything else is a failure.
    15	package main
    16	
    17	import (
    18		"bytes"
    19		"fmt"
    20		"go/types"
    21		"log"
    22		"os"
    23		"path/filepath"
    24		"strings"
    25	
    26		"cmd/internal/goobj"
    27		"cmd/internal/obj"
    28		"cmd/internal/objabi"
    29	)
    30	
    31	var valueFlags = map[string]bool{"o": true, "p": true, "importcfg": true, "trimpath": true, "symabis": true,
    32		"asmhdr": true, "lang": true, "goversion": true, "embedcfg": true, "c": true, "buildid": true, "D": true,
    33		"coveragecfg": true, "pgoprofile": true, "installsuffix": true, "spectre": true, "d": true, "linkobj": true,
    34		"json": true, "env": true, "memprofile": true, "cpuprofile": true, "I": true}
    35	
    36	type options struct {
    37		out, path, importcfg, trimpath, symabis, lang, embedcfg string
    38		std                                                     bool
    39		files                                                   []string
    40	}
    41	
    42	func parseArgs(args []string) options {
    43		var o options
    44		for i := 0; i < len(args); i++ {
    45			a := args[i]
    46			if !strings.HasPrefix(a, "-") {
    47				o.files = append(o.files, a)
    48				continue
    49			}
    50			name := strings.TrimLeft(a, "-")
    51			val := ""
    52			hasVal := false
    53			if j := strings.Index(name, "="); j >= 0 {
    54				name, val, hasVal = name[:j], name[j+1:], true
    55			}
    56			if valueFlags[name] && !hasVal {
    57				i++
    58				if i < len(args) {
    59					val = args[i]
    60				}
    61			}
    62			switch name {
    63			case "o":
    64				o.out = val
    65			case "p":
    66				o.path = val
    67			case "importcfg":
    68				o.importcfg = val
    69			case "trimpath":
    70				o.trimpath = val
    71			case "symabis":
    72				o.symabis = val
    73			case "embedcfg":
    74				o.embedcfg = val
    75			case "lang":
    76				o.lang = val
    77			case "std":
    78				o.std = true
    79			}
    80		}
    81		return o
    82	}
    83	
    84	// readImportcfg returns the import map and the archive of each package.
    85	func readImportcfg(path string) (importMap, files map[string]string) {
    86		importMap, files = map[string]string{}, map[string]string{}
    87		data, err := os.ReadFile(path)
    88		if err != nil {
    89			log.Fatal(err)
    90		}
    91		for _, line := range strings.Split(string(data), "\n") {
    92			verb, rest, _ := strings.Cut(line, " ")
    93			a, b, ok := strings.Cut(rest, "=")
    94			if !ok {
    95				continue
    96			}
    97			switch verb {
    98			case "importmap":
    99				importMap[a] = b
   100			case "packagefile":
   101				files[a] = b
   102			}
   103		}
   104		return
   105	}
   106	
   107	// ---- archives
   108	
   109	type arMember struct {
   110		name string
   111		hdr  []byte
   112		data []byte
   113	}
   114	
   115	func readArchive(path string) []arMember {
   116		data, err := os.ReadFile(path)
   117		if err != nil {
   118			log.Fatal(err)
   119		}
   120		if !bytes.HasPrefix(data, []byte("!<arch>\n")) {
   121			log.Fatalf("fc: %s is not an archive", path)
   122		}
   123		var ms []arMember
   124		off := 8
   125		for off+60 <= len(data) {
   126			hdr := data[off : off+60]
   127			name := strings.TrimSpace(string(hdr[:16]))
   128			var size int
   129			fmt.Sscanf(strings.TrimSpace(string(hdr[48:58])), "%d", &size)
   130			ms = append(ms, arMember{name, hdr, data[off+60 : off+60+size]})
   131			off += 60 + size
   132			if off&1 != 0 {
   133				off++
   134			}
   135		}
   136		return ms
   137	}
   138	
   139	func writeArchive(path string, ms []arMember) {
   140		var b bytes.Buffer
   141		b.WriteString("!<arch>\n")
   142		for _, m := range ms {
   143			hdr := append([]byte(nil), m.hdr...)
   144			copy(hdr[48:58], fmt.Sprintf("%-10d", len(m.data)))
   145			b.Write(hdr)
   146			b.Write(m.data)
   147			if b.Len()&1 != 0 {
   148				b.WriteByte(0)
   149			}
   150		}
   151		if err := os.WriteFile(path, b.Bytes(), 0o666); err != nil {
   152			log.Fatal(err)
   153		}
   154	}
   155	
   156	// stockDef is a symbol the stock object defines in its indexed block: importers compiled by the stock
   157	// compiler refer to it by that index, not by name.
   158	type stockDef struct {
   159		name string
   160		abi  uint16
   161		text bool
   162	}
   163	
   164	// definedSyms lists the symbols an archive's Go object defines.
   165	func definedSyms(path string) map[string]bool {
   166		out, _ := definedSymsOrder(path)
   167		return out
   168	}
   169	
   170	func definedSymsOrder(path string) (map[string]bool, []stockDef) {
   171		out := map[string]bool{}
   172		var defs []stockDef
   173		for _, m := range readArchive(path) {
   174			if m.name != "_go_.o" {
   175				continue
   176			}
   177			data := m.data
   178			// the object proper starts after the "go object ...\n!\n" header
   179			i := bytes.Index(data, []byte("\n!\n"))
   180			if i < 0 {
nl: write error: Broken pipe
     1	#!/usr/bin/env bash
     2	# go build/test -toolexec wrapper for fc's mixed mode. A package whose compile flags contain -d=fc=<anything>
     3	# (give it with -gcflags='<pkg>=-l -d=fc=<hash of build/fc>', so the go command recompiles the package when
     4	# fc changes) is compiled by the stock compiler and then by fc, which replaces the object in the archive.
     5	# FC_ONLY: the import paths (space-separated) fc compiles, or '*'. FC_REASONS: a file that collects fc's stderr. FC_BIN: the fc binary. FC_PLAN: `go list -json` of the dependencies. FC_LOG: a line per package.
     6	# FC_STRICT=1: a package fc cannot compile fails the build (otherwise the stock object stays and the log says so).
     7	tool=$1; shift
     8	if [ "$(basename "$tool")" != compile ]; then exec "$tool" "$@"; fi
     9	fc=0; args=()
    10	for a in "$@"; do case "$a" in -d=fc=*) fc=1;; *) args+=("$a");; esac; done
    11	pkg=; prev=
    12	for a in "${args[@]}"; do [ "$prev" = -p ] && pkg=$a; prev=$a; done
    13	# the flag also reaches the package's external test package (<pkg>_test): that one stays with the stock compiler
    14	mine=$fc
    15	[ "$FC_ONLY" = '*' ] || case " $FC_ONLY " in *" $pkg "*) ;; *) mine=0;; esac
    16	if [ $mine = 1 ] && [ -n "$FC_ESC_COMPILE" ]; then
    17	  # the export data importers read says every pointer parameter escapes (tools/fc/mkcompile-esc.sh)
    18	  "$FC_ESC_COMPILE" "${args[@]}" || exit $?
    19	else
    20	  "$tool" "${args[@]}" || exit $?
    21	fi
    22	[ $mine = 1 ] || exit 0
    23	if [ -n "$FC_REASONS" ]; then "$FC_BIN" "${args[@]}" 2>> "$FC_REASONS"; rc=$?; else "$FC_BIN" "${args[@]}"; rc=$?; fi
    24	case $rc in
    25	  0) [ -z "$FC_LOG" ] || echo "$pkg	FC" >> "$FC_LOG"; exit 0;;
    26	  3) [ -z "$FC_LOG" ] || echo "$pkg	STOCK" >> "$FC_LOG"; [ -n "$FC_STRICT" ] && exit 1; exit 0;;
    27	  *) [ -n "$FC_STRICT" ] && exit $rc
    28	     [ -z "$FC_LOG" ] || echo "$pkg	CRASH" >> "$FC_LOG"; exit 0;;
    29	esac
293:at the line. And the whole-program chain for declarations is 0.3 s where the per-package chain is 12 s.
585:cores): today's rule gives 10.3 s for the compile row, the CPU bound is 9.3 s, and exporting
825:0.3 s write at 1.5 cores, 13% of the build) and nothing built so far touches it. Why I am not building
981:## 2026-11-12 14:25 UTC — correction to iteration 20: the descriptor change is worth 1.6 core-s and 0.3 s, not 7 and 0.9
1001:**-1.6 core-s of compile; -120 ms of link (the two link samples do not overlap at all); about -0.3 s
1130:  were right about different machines: on 8 cores breaking the chain buys 0.3 s (last night's
1286:iteration. No time left in this session for a second slice. NEXT.md is corrected.
1302:**What is left in "claim" and why.** 2.6 M claims at 90 ms on 8 cores is about 270 ns of core time
1304:memory latency, not computation (HARDWARE.md: a miss is about 100 ns here). The data-parallel answer
1355:An honest reading for the ladder: F4 could take the link from 1.1 s to perhaps 0.3 s, about 0.8 s of an
1445:Inbox empty. Scope check: the most ambitious thing in twenty minutes is one more step on the F4
1455:Inbox empty. Scope check: nothing a rung needs fits in the last minutes; more evidence on the
1495:**What did the oracle number do since the last milestone?** 0 -> 1 standard-library package. No blocker: the
archive/tar
archive/zip
cmp
compress/bzip2
compress/flate
compress/gzip
compress/lzw
compress/zlib
container/heap
container/ring
crypto
crypto/aes
crypto/cipher
crypto/des
crypto/dsa
crypto/ecdh
crypto/ecdsa
crypto/elliptic
crypto/fips140
crypto/hkdf
crypto/hmac
crypto/hpke
crypto/internal/boring
crypto/internal/boring/bcache
crypto/internal/cryptotest
crypto/internal/fips140
crypto/internal/fips140/aes
crypto/internal/fips140/aes/gcm
crypto/internal/fips140/bigmod
crypto/internal/fips140deps
crypto/internal/fips140/drbg
crypto/internal/fips140/ecdh
crypto/internal/fips140/ecdsa
crypto/internal/fips140/edwards25519
crypto/internal/fips140/edwards25519/field
crypto/internal/fips140/mldsa
crypto/internal/fips140/mlkem
crypto/internal/fips140/nistec
crypto/internal/fips140/nistec/fiat
crypto/internal/fips140only
crypto/internal/fips140/rsa
crypto/internal/fips140/subtle
crypto/internal/sysrand
crypto/mlkem
crypto/pbkdf2
crypto/rc4
crypto/subtle
database/sql/driver
debug/buildinfo
debug/dwarf
debug/elf
debug/gosym
debug/macho
debug/pe
debug/plan9obj
embed
encoding/ascii85
encoding/asn1
encoding/base32
encoding/base64
encoding/csv
encoding/gob
encoding/hex
encoding/json
encoding/json/internal/jsonflags
encoding/json/internal/jsonopts
encoding/json/internal/jsonwire
encoding/json/v2
encoding/pem
encoding/xml
errors
expvar
flag
go/ast
go/build
go/build/constraint
go/constant
go/doc
go/doc/comment
go/format
go/importer
go/internal/gccgoimporter
go/internal/gcimporter
go/internal/srcimporter
go/parser
go/printer
go/scanner
go/token
go/version
hash
hash/adler32
hash/crc32
hash/crc64
hash/fnv
html
html/template
image
image/color
image/draw
image/gif
image/jpeg
image/png
index/suffixarray
internal/buildcfg
internal/cgrouptest
internal/copyright
internal/coverage/cfile
internal/coverage/cformat
internal/coverage/cmerge
internal/coverage/pods
internal/coverage/slicereader
internal/coverage/slicewriter
internal/coverage/test
internal/cpu
internal/dag
internal/diff
internal/fmtsort
internal/fuzz
internal/gate
internal/godebug
internal/godebugs
internal/gover
internal/nettest
internal/pkgbits
internal/platform
internal/poll
internal/profile
internal/reflectlite
internal/runtime/atomic
internal/runtime/gc/scan
internal/runtime/math
internal/runtime/sys
internal/runtime/syscall/linux
internal/runtime/wasitest
internal/saferio
internal/singleflight
internal/sync
internal/synctest
internal/syscall/unix
internal/sysinfo
internal/testenv
internal/trace
internal/trace/internal/tracev1
internal/trace/testtrace
internal/trace/tracev2
internal/types/errors
internal/unsafeheader
internal/xcoff
io
io/fs
io/ioutil
iter
log/slog/internal/benchmarks
log/syslog
maps
math
math/big/internal/asmgen
math/bits
math/rand
math/rand/v2
mime/multipart
mime/quotedprintable
net/http/cgi
net/http/cookiejar
net/http/fcgi
net/http/httptest
net/http/httptrace
net/http/httputil
net/http/internal/ascii
net/http/internal/http2
net/http/pprof
net/internal/cgotest
net/internal/socktest
net/mail
net/rpc
net/rpc/jsonrpc
net/smtp
net/url
os/exec
os/exec/internal/fdtest
os/signal
os/user
path
path/filepath
plugin
regexp/syntax
runtime/debug
runtime/metrics
runtime/trace
slices
sort
sync/atomic
syscall
testing/cryptotest
testing/fstest
testing/iotest
testing/quick
testing/slogtest
testing/synctest
text/tabwriter
text/template/parse
unicode
unicode/utf8
uuid
weak

codex
The frozen build still ends at a package-completion barrier followed by the stock linker. The new compiler also waits for whole dependencies before type-checking, and its current wrapper runs stock compilation first. PLAN’s oracle remains at one passing package, although the passing list now contains substantially more entries.
exec
/bin/bash -lc "wc -l tests/fc/passing.txt; nl -ba JOURNAL.md | sed -n '1298,1310p;1442,1460p'; rg -n 'parallel|goroutine|for .*range|newProgram' driver/fc/gen.go driver/fc/main.go | tail -n 22; nl -ba tools/fc/test.sh | tail -n 65" in /home/exedev/strand-w
 succeeded in 0ms:
205 tests/fc/passing.txt
  1298	    layout by prefix sums                             4                  4
  1299	    copy data, apply relocations                  34-41              33-36
  1300	    total                                    about 260          **about 160**   (one core: about 650)
  1301	
  1302	**What is left in "claim" and why.** 2.6 M claims at 90 ms on 8 cores is about 270 ns of core time
  1303	each: one name or hash fetched from a mapped object, one random probe into a 128 MB table. That is
  1304	memory latency, not computation (HARDWARE.md: a miss is about 100 ns here). The data-parallel answer
  1305	is the database one: partition the keys by range first (sequential writes), then insert each
  1306	partition into a cache-sized piece of the table. Not built; it is the first Strand-shaped phase F4
  1307	would create (a join over hashed keys), and by the charter Strand is its default.
  1308	
  1309	**An unexplained discrepancy, recorded rather than hidden:** the 64-bit table finds 960,356 distinct
  1310	keys where the string-keyed maps found 960,375. Nineteen pairs merge that should not, about 400 times
  1442	
  1443	## 2026-11-12 21:15 UTC — iteration 29 (short): one hypothesis about the 46, wrong
  1444	
  1445	Inbox empty. Scope check: the most ambitious thing in twenty minutes is one more step on the F4
  1446	spike's diff against the stock linker. The largest group of the 46 missing functions is about 25
  1447	assembly routines (`internal/runtime/gc/scan.expandAVX512_*`). Hypothesis: they are reached through
  1448	function-value symbols (`f·f`) that no object defines and the linker makes. Tried: resolve an
  1449	undefined `f·f` to `f`. Result: still 46 missing, the same ones. **Wrong; reverted.** They are reached
  1450	some other way (a table built in assembly data, or a linkname); left as the first item for the next
  1451	session's diff, with the ten local-type methods whose names the two lists probably spell differently.
  1452	
  1453	## 2026-11-12 21:30 UTC — iteration 30: soak while the clock ran down
  1454	
  1455	Inbox empty. Scope check: nothing a rung needs fits in the last minutes; more evidence on the
  1456	fork-server's one risk does. Thirty more cold MinIO builds, each smoke-tested: 30 builds, 30 pass; wall min 10566, median 11405, max 13848 ms.
  1457	Total for the final configuration: MinIO 180 of 180, kube-apiserver 56 of 56.
  1458	
  1459	## 2026-11-12 19:52 UTC — campaign 1 begins: fc, a compiler of our own, compiles its first package
  1460	
driver/fc/gen.go:663:	for _, n := range []int64{4, 2, 1} {
driver/fc/gen.go:771:	for i, p := range g.abi.in {
driver/fc/gen.go:786:	for i, p := range g.abi.out {
driver/fc/gen.go:823:	for i, p := range g.abi.out {
driver/fc/gen.go:829:	for i, p := range g.abi.out {
driver/fc/gen.go:830:		for _, q := range p.parts {
driver/fc/gen.go:890:	for _, in := range g.abi.in {
driver/fc/gen.go:891:		for _, q := range in.parts {
driver/fc/gen.go:916:	for _, o := range g.abi.out {
driver/fc/gen.go:944:	for _, e := range out.unsafe {
driver/fc/gen.go:947:	for _, e := range out.lines {
driver/fc/gen.go:950:	for _, e := range out.sp {
driver/fc/gen.go:958:	for i := range relocs {
driver/fc/gen.go:965:		for _, in := range g.abi.in {
driver/fc/gen.go:966:			for _, q := range in.parts {
driver/fc/gen.go:982:		for _, in := range g.abi.in {
driver/fc/gen.go:983:			for _, q := range in.parts {
driver/fc/gen.go:996:		for _, mj := range moreJumps {
driver/fc/gen.go:1021:	for _, in := range g.abi.in {
driver/fc/gen.go:1027:	for _, o := range g.abi.out {
driver/fc/gen.go:1034:	for _, s := range g.slots {
driver/fc/gen.go:1036:		for i, b := range s.ptrs {
     1	#!/usr/bin/env bash
     2	# Run a package's own tests with the package compiled by fc (everything else by the stock compiler).
     3	#   tools/fc/test.sh [-C dir] <import path> [go test flags...]
     4	# Exit 0: fc compiled the package and its tests pass.
     5	here=$(cd "$(dirname "$0")/../.." && pwd)
     6	dir=.
     7	if [ "$1" = -C ]; then dir=$2; shift 2; fi
     8	pkg=$1; shift
     9	export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS= GOCACHE=${FC_GOCACHE:-/home/exedev/targets/fc-gocache}
    10	export FC_BIN=${FC_BIN:-$here/build/fc} FC_STRICT=${FC_STRICT-1}
    11	[ -x "$here/build/compile-esc" ] && export FC_ESC_COMPILE=${FC_ESC_COMPILE-$here/build/compile-esc}
    12	cd "$dir" || exit 2
    13	plan=$(mktemp /tmp/fcplan.XXXXXX); trap 'rm -f $plan' EXIT
    14	go list -deps -test -json "$pkg" runtime > "$plan" || exit 2
    15	export FC_PLAN=$plan FC_ONLY=$pkg
    16	hash=$(cat "$FC_BIN" "${FC_ESC_COMPILE:-/dev/null}" | sha1sum | cut -c1-12)
    17	# tests the dev build is excused from (tests/fc/skips.txt: package, test name pattern, reason)
    18	skip=$(awk -v p="$pkg" '$1==p{printf "%s%s", sep, $2; sep="|"}' "$here/tests/fc/skips.txt" 2>/dev/null)
    19	[ -z "$skip" ] || set -- -skip "$skip" "$@"
    20	go test -vet=off -toolexec="$here/tools/fc/toolexec.sh" -ldflags=-w -gcflags=all=-l "-gcflags=$pkg=-l -d=fc=$hash" "$@" "$pkg"

codex
1. **Whole-package barriers and a terminal linker still serialize the build.** MinIO’s final `cmd` compile releases its successor after **1,716 ms**, followed by **1,071 ms** linking ([TRACE.md:86](/home/exedev/strand-w/TRACE.md:86)). Even fc waits for every dependency’s complete type check before checking the importer ([load.go:162](/home/exedev/strand-w/driver/fc/load.go:162)). Missing: declaration readiness separated from body checking, function-level scheduling, and integrated image construction.

2. **The link floor—16 ms—is a copy floor, not a linking floor.** TRACE explicitly excludes symbol resolution and relocation from its model ([TRACE.md:363](/home/exedev/strand-w/TRACE.md:363)). The spike’s **2.6 million claims** alone take **90 ms**, probing a **128 MB** table with roughly **100 ns** memory misses ([JOURNAL.md:1302](/home/exedev/strand-w/JOURNAL.md:1302)). The actual constraint is irregular memory access plus reachability and metadata dependencies. The measured incomplete link core is about **200 ms**; roughly **300 ms** is a proposed complete-link target, not an established physical floor.

3. **Keeping the stock compiler underneath cannot reach the target.** Even removing its fusion tax leaves approximately **47 core-seconds / 8 = 5.9 seconds**, before other costs ([PLAN.md:14](/home/exedev/strand-w/PLAN.md:14)). Today’s fc wrapper actually runs stock compilation **before** fc ([toolexec.sh:16](/home/exedev/strand-w/tools/fc/toolexec.sh:16)). That is useful oracle scaffolding but a dead end as the execution architecture. Build a persistent whole-program compiler: check each dependency once, schedule bodies across cores, and emit into a shared final-image representation. The replacement-compiler campaign itself is not disproven.

4. **Serialized export IR is the clearest unnecessary intermediate.** Writing it costs **5.3 core-seconds**; reading it into compiler IR, including imports, costs **10.3 core-seconds**—**15.6 combined**. Early export adds **2.8**, object writing **3.4** ([TRACE.md:65](/home/exedev/strand-w/TRACE.md:65)). Archives/export files still account for **618 MB written and 1,143 MB read back**; tmpfs retains that materialization ([TRACE.md:55](/home/exedev/strand-w/TRACE.md:55)).

5. **The journal explicitly shrank work for session time:** “Scope check: nothing a rung needs fits in the last minutes; more evidence on the fork-server’s one risk does.” It then ran **30 more soaks** ([JOURNAL.md:1455](/home/exedev/strand-w/JOURNAL.md:1455)).

   **PLAN’s recorded oracle has not moved beyond M1:** **1 standard-library package, 0 corpus packages**, performance unmeasured ([PLAN.md:79](/home/exedev/strand-w/PLAN.md:79)). The working [passing.txt](/home/exedev/strand-w/tests/fc/passing.txt) contains **205 entries**, so that record is stale; entries alone do not establish the full oracle. The largest unattended campaign blocker is **turning isolated package replacement into an actual fused build that eliminates stock work and repeated dependency checking**. The missing core-seconds/MB measurement also leaves the **50-package, one-third-cost kill test** unanswered.
tokens used
38,189
1. **Whole-package barriers and a terminal linker still serialize the build.** MinIO’s final `cmd` compile releases its successor after **1,716 ms**, followed by **1,071 ms** linking ([TRACE.md:86](/home/exedev/strand-w/TRACE.md:86)). Even fc waits for every dependency’s complete type check before checking the importer ([load.go:162](/home/exedev/strand-w/driver/fc/load.go:162)). Missing: declaration readiness separated from body checking, function-level scheduling, and integrated image construction.

2. **The link floor—16 ms—is a copy floor, not a linking floor.** TRACE explicitly excludes symbol resolution and relocation from its model ([TRACE.md:363](/home/exedev/strand-w/TRACE.md:363)). The spike’s **2.6 million claims** alone take **90 ms**, probing a **128 MB** table with roughly **100 ns** memory misses ([JOURNAL.md:1302](/home/exedev/strand-w/JOURNAL.md:1302)). The actual constraint is irregular memory access plus reachability and metadata dependencies. The measured incomplete link core is about **200 ms**; roughly **300 ms** is a proposed complete-link target, not an established physical floor.

3. **Keeping the stock compiler underneath cannot reach the target.** Even removing its fusion tax leaves approximately **47 core-seconds / 8 = 5.9 seconds**, before other costs ([PLAN.md:14](/home/exedev/strand-w/PLAN.md:14)). Today’s fc wrapper actually runs stock compilation **before** fc ([toolexec.sh:16](/home/exedev/strand-w/tools/fc/toolexec.sh:16)). That is useful oracle scaffolding but a dead end as the execution architecture. Build a persistent whole-program compiler: check each dependency once, schedule bodies across cores, and emit into a shared final-image representation. The replacement-compiler campaign itself is not disproven.

4. **Serialized export IR is the clearest unnecessary intermediate.** Writing it costs **5.3 core-seconds**; reading it into compiler IR, including imports, costs **10.3 core-seconds**—**15.6 combined**. Early export adds **2.8**, object writing **3.4** ([TRACE.md:65](/home/exedev/strand-w/TRACE.md:65)). Archives/export files still account for **618 MB written and 1,143 MB read back**; tmpfs retains that materialization ([TRACE.md:55](/home/exedev/strand-w/TRACE.md:55)).

5. **The journal explicitly shrank work for session time:** “Scope check: nothing a rung needs fits in the last minutes; more evidence on the fork-server’s one risk does.” It then ran **30 more soaks** ([JOURNAL.md:1455](/home/exedev/strand-w/JOURNAL.md:1455)).

   **PLAN’s recorded oracle has not moved beyond M1:** **1 standard-library package, 0 corpus packages**, performance unmeasured ([PLAN.md:79](/home/exedev/strand-w/PLAN.md:79)). The working [passing.txt](/home/exedev/strand-w/tests/fc/passing.txt) contains **205 entries**, so that record is stale; entries alone do not establish the full oracle. The largest unattended campaign blocker is **turning isolated package replacement into an actual fused build that eliminates stock work and repeated dependency checking**. The missing core-seconds/MB measurement also leaves the **50-package, one-third-cost kill test** unanswered.
