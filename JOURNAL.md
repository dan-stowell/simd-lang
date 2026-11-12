# JOURNAL

Append-only (one correction: on 2026-11-12 00:01 the headings of iterations 1-9 were reset to the times of
their commits; I had been writing estimated times and they ran up to 70 minutes fast). Each entry: timestamp (absolute, UTC), what I did, what moved in TRACE.md,
scope check ("most ambitious next thing; why not that").

## 2026-11-11 22:25 UTC — iteration 1: rung 0, the profile

**Did.** Cloned MinIO (`7aac2a2c`) and Kubernetes (`2cb26b34`) under `/home/exedev/targets/`, Go 1.27.3
(`GOTOOLCHAIN=local`). `tools/trace/stock.sh` does one instrumented cold build (fresh GOCACHE, dropped
page cache, `-debug-trace`, `-debug-actiongraph`, /proc/stat sampled every 100 ms, disk bytes from
/proc/diskstats); `stock_all.sh` runs both targets on 8, 4, 2, 1 CPUs plus a warm run and a run with the
compiler's `-bench` timers; `census.py` counts the source; `hw/limits.sh` measures the bandwidths;
`mktrace.py` turns all of it into TRACE.md.

**The gap (MinIO, 8 CPUs, cold):** 54.2 s wall, 282 core-seconds, 5.2 cores busy on average. Compile
stretch 47.4 s; link 3.3 s; `go build` takes 3.3 s before the first compile starts. The longest
dependency chain is 38.5 s. `kube-apiserver`: 86.3 s, 554 core-seconds, chain 62.7 s. Module fetch for
MinIO: 6.7 s (1.1 GB).

**What serializes it.**
1. *Work, not waiting, is the first wall on this VM.* 282 core-seconds over 8 cores is 35 s even with
   every core busy all the time. Stock gets 54 s. So perfect scheduling is worth 19 s and no more;
   everything after that has to come from doing less work.
2. *The chain.* A package cannot start until every dependency's compiler has exited, because the export
   data and the object code come out of the same process at the same time. 30 actions deep, 38.5 s. The
   tail is `minio/cmd` (5 MB of Go, 8.5 s, 26.5 core-s) waiting for `mc/cmd`, `madmin-go`, four
   `goccy/go-json` VM packages in a row (2.6-5 s each for 145 kB of Go: one giant function each, which the
   back end compiles on one core) and then the 3.3 s link, at 1.7 cores busy.
3. *Where the work is* (compiler's own timers, MinIO): back end 169 core-s, front end (lex, parse, types,
   IR) 50, inlining and escape analysis 26, writing objects 19. Kubernetes is different: front end 149,
   writing objects 88 (2.7 GB of objects for 89 MB of source, export data copied down the graph).
4. *The first 3.3 s* is `go build` loading the package graph on about 1.8 cores: by itself over the 2 s budget.
5. *Small files.* Reading the sources cold costs 300 ms as 7,640 files, 26 ms as one sequential read.

**Floors.** TRACE.md's model puts MinIO's irreducible work at 6.5 core-seconds, so 2 s on 8 cores is not
ruled out by physics. The ticks-per-byte budgets in that model (lex 2, parse 8, types 40, codegen 40) are
guesses and are labelled as such; the bandwidths are measured.

**Caveat on the numbers.** During the 2-CPU MinIO and the 8-CPU and 2-CPU kube-apiserver stock runs I ran
`go list` / a small `go build` on the same machine (a second or two of one core). The 8-CPU MinIO run,
which the trace is built on, was clean.

**Scope check.** Most ambitious next thing: replace the compiler's back end (169 of 282 core-seconds)
with a Strand code generator. Why not now: there is no driver yet in which a replaced phase could run and
be tested against MinIO's own behaviour; without the skeleton a new back end has nowhere to stand. Rung 1
first (hours, not days), then the chain (item 2), which a patch to the compiler can cut without a new
back end.

## 2026-11-11 22:36 UTC — iteration 2: rung 1, the skeleton

**Did.** `build/sb` (driver/main.go) builds MinIO end to end, cold: plan (`go list`) -> read every Go
file in parallel -> Strand golex over the 75.5 MB concatenation -> `go tool compile`/`asm` per package
from a priority queue (priority = bytes of Go on the longest chain above) with CPU tokens that become the
compiler's `-c` -> `go tool link` -> write the binary. Objects live in tmpfs and there is no build cache.
`tools/trace/sb.sh` runs it cold with the same instrumentation as the stock runs, then the smoke test.

**Checked.**
- `tests/minio_smoke.sh`: the binary serves S3 put/get/list/delete (3 MB object round trip). Passes for
  the stock binary and the driver's.
- `tests/sb_plan.sh`: all 1,483 compile/asm/link command lines for MinIO and all 2,180 for kube-apiserver
  equal `go build`'s (from its action graph) up to work-directory names, build IDs and `-c`; embed
  configurations equal `go build -n`'s. The first run of this test failed on 31 packages: modules with
  no `go` directive must be compiled as Go 1.16, and I was giving them 1.27. That is a behaviour
  difference (loop variables), and the smoke test had passed with it.
- Strand golex against go/scanner on the build's whole input: 12,874,970 tokens over 7,531 files,
  byte-identical.
- `./test.sh`: ALL PASS.

**Trace.** MinIO cold 54.2 s -> 47.6 s; 282 -> 265 core-seconds. Plan 0.43 s (go build spends 3.3 s
before its first compile; `go list` alone is much cheaper), read 0.22 s, lex 0.21 s, compile 43.1 s at
6.1 cores busy, link 3.1 s, write 0.27 s. Disk writes fell from 1,651 MB to 148 MB (just the binary).
The gain is from not writing a cache and from scheduling; no phase got faster.

**Scope check.** Most ambitious next: a Strand front end producing what the compiler's back end needs.
Not yet: 43.1 of 47.6 s is one row, and two cheaper cuts into it have not been tried: the chain (a
package waits for its dependencies' *code generation*, which it does not need) and the amount of work
(optimization level, debug info, GC settings, all of which the charter lets me relax). Rung 2 is the
chain, via a patch to cmd/compile that writes export data before the back end runs.

## 2026-11-11 22:55 UTC — iteration 3: rung 2, the chain

**Did.** Patched cmd/compile (`driver/early_export.py`, built by `driver/mkcompiler.sh` into
`build/compile-sb`): with `-linkobj` and `SB_EARLY_EXPORT` set, the export data is written as soon as the
front end is done (after inlining decisions and escape analysis, which the export data records) and the
compiler prints `sb:export-ready`. The driver releases a package's importers at that moment.

**Two things went wrong, and both were instructive.**
1. *The first pipelined MinIO crashed at start-up* (`reflect.Type is *reflect.rtype, not *reflect.rtype`),
   and only the smoke test caught it. Cause: symbol indices are assigned after the back end, so early
   export data has none and importers refer to symbols by name. Interface tables are deduplicated by a
   content hash that covers *how* each relocation target is addressed (package+index in the defining
   package, name in an early importer). Two spellings of the same table both survived the link and a type
   assertion compared their addresses. Fix: with early export on, the hash addresses every target by name.
2. *Pipelining alone gained nothing* (48.0 s against 47.6 s). The trace showed why: `minio/internal/net`,
   1 kB of Go, sat 27.7 s in the ready queue because every CPU token was held by another package's back
   end, and `minio/cmd` (the 8 s tail) could not start without it. Front ends are what dependents wait
   for, so now a front end holds a token and gives it back at export; the back end carries on at nice 19
   and fills what the front ends leave.

**Checked** (`tests/sb_early.sh`, on all of MinIO): idle, the patched compiler's 2,624 export files and
objects and the linked binary are byte-identical to the stock compiler's. With early export on, the
binary is not byte-identical (a few duplicate-allowed `type:.eq.*` functions land in a different order),
but all 142,181 symbols have the same names, sizes and types, and text/data/bss sizes are equal. Smoke
test passes.

**Trace.** 47.6 s -> 39.9 s cold. Chain 38.5 s -> about 12 s. Cores busy 5.6 -> 6.8. Core-seconds went
*up*, 265 -> 273: hundreds of back ends alive at once (432 at the peak, 13 GB) cost something.

## 2026-11-11 23:02 UTC — iteration 4: rung 3, less work (the budgeted optimization iteration)

After rung 2 the build is bound by work: 273 core-s / 8 = 34 s. The charter lets optimization level and
debug info go. Measured one at a time on the full MinIO build, each binary smoke-tested:

| change | wall s | compile core-s |
|---|---|---|
| pipelined, stock flags | 41.4 | 285 |
| `-dwarf=false`, link `-s -w` | 33.0 | 228 |
| + `-l` (no inlining) | 26.2 | 180 |
| + `-N -l` instead | 25.8 | 177 (binary 130 MB instead of 100; not adopted) |
| `-dwarf=false -l`, `GOGC=300` | 25.9 | 177 |
| `GOGC=1000` | 21.9 | 147 |
| `GOGC=off` | 21.3 | 141 (peak memory about 16 GB of 31; the build runs in a 22 GB scope) |

**Trace.** MinIO cold: **21.96 s**, 144 core-seconds, 6.6 cores busy (stock: 54.2 s, 282). Link 1.5 s.

**What serializes it now.** (1) Work: 144 core-s / 8 = 18 s. (2) `minio/cmd`: 9.1 s by itself, started
at 10.9 s; its type check is one thread. (3) The link, 1.5 s at 1.4 cores.

**Scope check.** Most ambitious: a Strand back end (the skeptic's proposal, REVIEWS.md). Why not now:
even a perfect back end leaves the front end's ~55 core-s, and a subset back end moves nothing tonight.
The larger lever I can measure in an hour: how much of what is compiled does the linker throw away? If
it is large, whole-program reachability before code generation removes front-end *and* back-end work,
and its core computation (a histogram of identifier spellings over all tokens) is the first job in this
build that is Strand-shaped and whose output would be consumed. That is the next iteration (a spike,
with a kill test).

## 2026-11-11 23:10 UTC — iteration 5: spike, "don't compile what the linker discards". Killed.

**Kill test.** Built MinIO keeping the objects, listed every function symbol in the 1,312 archives
(`go tool nm -size`) and in the linked binary. Of 44.1 MB of compiled function text, the linker discards
**9.4 MB (21.3%)**: 45,720 of 164,648 functions. By kind: exported methods 3.2 MB, unexported methods
2.0, exported functions 1.9, unexported functions 1.7, closures and wrappers 0.4, generic
instantiations 0.1.

**Verdict.** My threshold was a third. A name-based reachability pass can only take the functions
(3.6 MB) and some unexported methods safely: reflection keeps exported methods of live types alive, and
the linker already assumes that. So at best 8-10% of back-end work, about 5% of the build, for a
whole-program analysis plus a second compiler patch. Not worth a rung. MinIO really does use four fifths
of what it compiles.

**Also measured and discarded this iteration.**
- An aggregated CPU profile of all 1,312 compiler processes (per-package `-cpuprofile`, merged) showed
  `runtime.asyncPreempt` at 20% flat. Building with `GODEBUG=asyncpreemptoff=1`: 144.1 against 145.0
  core-s. It is a sampling artifact of threads parked at nice 19, not work.
- The same profile, read by cumulative time: SSA back end about 40% of samples, allocation and clearing
  about 15%, object writing 3%. System time is 25 of 139 compile core-seconds (page faults with the GC
  off, process start, tmpfs writes). This kernel has no transparent huge pages to try.
- By package size: the 8 packages over 1 MB take 28.7 core-s, the 143 between 100 kB and 1 MB take 55.8,
  the 1,161 smaller ones 54.8. No single fat target left; the work is spread thin.

**Scope check.** Most ambitious: replace type checking or code generation outright. The profile says
that is where the work is (lexing all of MinIO is 0.2 s; the stock compiler's scan of the same bytes
would be about 0.4 core-s of 144), so nothing short of that moves the compile row by a large factor.
Doing: the divergence step (due now), and kube-apiserver through the driver while the subagents think.

## 2026-11-11 23:21 UTC — iteration 6: second codebase, divergence step, and where the work is

**kube-apiserver through the driver.** Cold, same configuration: **53.4 s** (stock 86.3 s), 2,079
packages. `tests/kube_smoke.sh` (etcd 3.7.1 as pinned by Kubernetes, `/healthz`, then a ConfigMap
create/get/delete) passes for the stock binary and the driver's. With `GOGC=off` the build was killed
at the 22 GB limit of its scope (the guard worked; the session survived); with `GOGC=1000` and at most
200 back ends at once it fits. golex equals go/scanner on its 12,526,067 tokens (`tests/sb_lex.sh`).
The gain is smaller than MinIO's (1.6x against 2.5x): Kubernetes is front-end heavy, and the front end
is what these rungs did not touch.

**Divergence step.** Ideas appended to IDEAS.md. The useful outcome is negative: three of four
unanchored agents proposed the reachability idea killed in iteration 5.

**Where the compile row's CPU goes** (`tools/trace/phases.sh`, now in TRACE.md): a CPU profile of all
1,312 compiler processes, merged. Of 99 s of samples: SSA passes 35%, walk and SSA construction 14%,
emitting machine code 10%, reading the export IR back into compiler IR 9%, escape analysis and runtime
types 9%, type checking 7%, writing objects 5%, writing export IR 5%. **Lexing and parsing: 0.6%**
(0.6 core-s in the profile; for scale, go/parser takes 2.4 s for the same 75.5 MB on one core, so call
it 1-2%).

That last number is the one that matters for this project. The phases Strand has (lexing,
package-level declarations) are a hundredth of the stock compiler's work on MinIO. Replacing them
cannot move the trace. What costs is everything that happens to a function body after it is parsed,
plus three conversions between tree representations. A faster lexer was never going to be the rung.

**Scope check.** Most ambitious: the flat whole-program compiler from IDEAS (declarations pass, then
bodies as a work list into a template back end). Why not tonight: it is a Go type checker plus a code
generator that satisfies Go's runtime (stack maps, write barriers, defers); I can't finish a consumed
slice of that before morning, and an unconsumed slice moves nothing. What I can finish, and what the
end state needs anyway: the build's front door. Today the plan is `go list` (0.5 s, serial, a quarter
of the whole 2 s budget) followed by a separate read of the same files. Rung 4: the driver finds the
package graph itself from the sources, in the same pass that reads them, with the import lists coming
from Strand's declaration pass, differential-tested against `go list` on both targets.

## 2026-11-11 23:36 UTC — iteration 7: rung 4, the plan without `go list`

**Did.** `driver/plan.go`: the driver finds the package graph itself while it reads the sources.
go.mod / go.work / vendor/modules.txt give the modules; each round lists the directories of the packages
found in the round before, keeps the files go/build's own `MatchFile` accepts (run on the bytes just
read), and runs Strand's declaration pass (`examples/godecl.strand`) over their concatenation; the
import paths it reports are resolved to directories and become the next round. Ten rounds for MinIO,
eleven for kube-apiserver. `go list` and the separate read phase are gone, and so is the token pass
nobody consumed: **this is the first Strand phase whose output the build uses.**

**Checked.** `tests/sb_plandiff.py`: the plan equals `go list -deps` field by field (directory, name, Go
files, assembly files, imports, import map, embed patterns, module, go version, default GODEBUG) for
all 1,312 MinIO packages and all 2,079 of kube-apiserver; it did so on the first run, including
Kubernetes' workspace-plus-vendor layout. `tests/sb_plan.sh ... self`: every tool command line still
equals `go build`'s. Smoke tests pass on both.

**Trace.** MinIO, cold: plan + read + declarations **0.65 s** where go list + read + lex were 0.94 s.
kube-apiserver: 0.60 s where they were 1.53 s. Inside it, cold: reading 0.37 s (the small-file limit
measured in HARDWARE.md is 0.30 s; more readers than cores did not help), godecl 0.22 s, resolving
imports 0.07 s.

**Two things the trace taught.**
- The rounds cost godecl 2.4x more per byte than one pass over everything (220 ms against 90 ms for
  the same 75.5 MB): ten process starts, and the pool does not spread a 6 MB input over eight cores the
  way it spreads 75 MB. A persistent godecl fed over a pipe would get most of that back. Not done.
- **The host is noisy.** The same build, same flags, cost 139 compile core-seconds at 23:00 and 157 at
  23:30; four consecutive cold runs gave 24.1, 24.1, 23.8, 22.1 s where half an hour earlier they gave
  21.3-22.0. Steal time is small (2-3.5 s); the cores are simply slower at times (shared hardware
  threads, presumably). `tools/trace/sb.sh` now takes `REPS=n` and TRACE.md prints every wall clock.
  Differences under about 2 s between single runs are not evidence; I compare phase rows.

**Scope check.** Most ambitious: start the flat whole-program compiler (IDEAS). Why not: unchanged from
iteration 6; it cannot be consumed tonight. What is left that moves the compile row with the stock
compiler: optional SSA passes off (about 7 of 139 core-s), per-pass clock calls (1.3), and not
oversubscribing back ends (the pipelined build burns about 5% more CPU than the unpipelined one).
That is a pure optimization iteration; the last one was iteration 4, so the budget allows it. After it:
the template back end's kill test 2 (shape histogram), the one cheap experiment that says whether the
end state's back end can be stencils.

## 2026-11-11 23:45 UTC — iteration 8: optimization of the compile row (budgeted). Three negatives.

All measured with `tools/trace/ab.sh` (configurations run in turn, twice, page cache warm, because the
host drifts), each binary smoke-tested.

| change | wall s (two rounds) | against baseline in the same rounds |
|---|---|---|
| optional SSA passes off (`-d=ssa/<pass>/off` for cse, prove, sccp, fuse, dse, branchelim, memcombine, pair, licm, loop rotate, ...) | 23.9, 26.8 | 21.9, 24.1: **slower**, binary 3% bigger |
| the same plus `nilcheckelim` off | 24.1, 25.0 | slower |
| at most 8 / 16 / 32 back ends running, the rest paused with SIGSTOP in arrival order (`-running n`) | 22.0, 21.3 / 21.2, 23.8 / 21.7, 21.7 | 21.3, 20.8: no gain (fewer core-seconds at 8, 128-138 against 138-142, but the cores idle instead) |

The optimization passes pay for themselves: the code they remove is code the register allocator and
the assembler do not have to process. And oversubscription costs CPU but not wall clock.

**Duplicate compilation, measured.** 26,052 of the 190,719 functions compiled for MinIO are copies of a
function some other package also compiles (duplicate-allowed symbols: method wrappers through embedded
interfaces, generic instantiations, equality functions): 3.9 MB of 48.9 MB of text, **8.1%**. One set
of wrappers around an embedded `reflect.Type` is compiled in about 400 packages. A compiler patch could
have the first package claim a symbol and the rest skip it; that is at most 8% of the back end, about
5% of the build, with a cross-process protocol to get wrong. Recorded in IDEAS, not done.

**Conclusion.** With the stock compiler doing the work, on these 8 cores, the compile row is at its
floor: 139 core-seconds / 8 = 17.4 s, and the stretch takes 19 s. Every remaining cheap lever I could
find is under 5% or negative. The next factor has to come from a different compiler, which is the
conclusion iteration 6 reached from the profile; this iteration closes the alternatives.

**Scope check.** Most ambitious: start that compiler. Tonight I can still de-risk it with two
measurements nobody has (IDEAS, both kill tests under an hour): what a declarations-only pass over the
whole program costs, and whether Go function bodies fall into few enough shapes for stencils. And the
trace should say what "2 s on N cores" means for the pipeline as it stands: simulate the measured
action graph on N cores.

## 2026-11-11 23:49 UTC — iteration 9: two measurements for the next compiler (spike)

`explore/wholeprog` (built as `build/wholeprog`) reads the driver's plan and works on the whole of
MinIO in one process: 1,312 packages, 75.5 MB.

**1. What does a whole-program front end cost, with nothing clever?**

| step (stock libraries, 8 cores) | wall ms | core-s | longest chain ms |
|---|---|---|---|
| go/parser, every file | 440 | 2.8 | |
| go/types, declarations only (`IgnoreFuncBodies`) | 365-430 | 2.3-2.6 | 304-338 |
| go/types, with every function body | 963 | 4.6 | 800 |

Zero type errors. So parsing and fully type-checking all of MinIO takes **1.4 s of wall clock and 7.4
core-seconds** in one process, against roughly 30 core-seconds that the stock compiler's 1,312 front
ends spend on parse, check and their two IR conversions (TRACE.md: 21 of 99 profile seconds). The
difference is not the checker (types2 is the same algorithm): it is that one process checks each package
once and hands its neighbours pointers, where 1,312 processes each write export data, read their
dependencies' export data, write their own IR and read it back.

The kill test in IDEAS for "declarations first" was 400 ms for the declarations pass: 365-430 ms. Alive,
at the line. And the whole-program chain for declarations is 0.3 s where the per-package chain is 12 s.

**2. Are function bodies made of few enough shapes for templates?**

Typed shape of an expression node = operator, what each operand is (a local, a global, a constant, a
field, or a computed temporary) with its type class (i8..i64, u8..u64, f32, f64, bool, string, pointer,
slice, map, chan, func, interface, struct by size class), and the result's class.

| | occurrences | distinct shapes | top 256 | top 1,024 | top 4,096 | top 16,384 |
|---|---|---|---|---|---|---|
| expression nodes | 1,423,275 | 22,301 | 69.6% | 88.2% | **96.7%** | 99.6% |
| whole statements | 878,220 | 82,505 | 54.2% | 69.5% | 80.9% | 89.8% |

The kill line was 95% at 4,096. Per node: alive. Per statement: dead; statements are too varied to be
templates. (If operands are first brought to fixed places, so a node's shape no longer says where its
operands came from, the count drops further; not measured.)

**What this changes.** The end state's front end does not need to be invented: stock parser and
checker, whole program, one process, already fit in 2 s. What has to be built is everything after
the checker: code generation at about 100 ns per byte of machine code (16 core-s budget minus the
front end's 7, over 45 MB of text) and a link that is a layout pass. That is where a data-parallel
design has to earn its keep; lexing never was.

**Scope check.** Most ambitious: start that compiler. I now think a consumed slice is possible tonight
after all, because Go's toolchain already has the seam: a function declared without a body is
"implemented elsewhere", the compiler takes the implementation's ABI from a symabis file, and a
package archive may hold several object files. So a second compiler can take over single functions
inside the real MinIO build: the patched stock compiler drops the bodies of the functions on a list, the
second compiler emits them into an extra object in the same archive. Coverage starts tiny and the trace
will not move (it will get worse by the whole-program check, about 1.4 s), but every function it takes
is in the binary that the smoke test runs, and from then on widening it is measured in the real build.
That is rung 5's skeleton. Staged so that a failure leaves something: (A) one hand-made function through
cmd/internal/obj into a running program; (B) the body-dropping patch and the driver plumbing; (C) a
real subset over the whole-program type information; (D) a differential harness.

## 2026-11-12 00:10 UTC — iteration 10: rung 5's skeleton. A second compiler is in the build.

**Did.** `driver/sbc` (built by `driver/mksbc.sh` into `build/sbc`): parses and type-checks the whole
program with go/parser and go/types, picks the functions it can compile, writes their machine code
through cmd/internal/obj into one extra object per package, and lists them in a symabis file. The
patched stock compiler (third change in `driver/early_export.py`) drops the Go body of any function
that has a definition in the symabis file, exactly as if it were written in assembly. The driver runs
sbc after the plan, passes each package its list, and appends sbc's object to the package archive.

Version 0 takes frameless leaf functions: `if cond { return e }` ... `return e`, one result, parameters
and result each one integer register, expressions made of parameters, constants, field loads through
pointers (nested structs, embedded pointers), integer arithmetic at every width, comparisons and
short-circuit `&&`/`||`. No calls, no stack frame, so no stack map and no prologue.

**The seam held at every stage.** (A) Two hand-made functions linked into a Go program whose source
declares them without bodies: ran first time, including the nil-receiver panic. (B) The body-dropping
patch: the same program with real bodies in the source runs sbc's versions. (C) MinIO: **4,458 of
99,194 functions in 412 packages are sbc's** (126 kB of code), the binary passes the S3 smoke test,
and `go tool objdump` shows sbc's code under MinIO's own symbols with the right file and line.

**Checked.** `tests/sbc_diff.sh`: random programs inside the subset (`tests/sbc_gen.py`: every integer
width, nested and embedded structs, conditional returns, nil arguments), built by the stock compiler
alone and with sbc taking the functions. 2,000 functions over 10 programs, 24,000 results including
nil-dereference panics: identical. The harness found one thing the MinIO smoke test could not have:
before it, sbc declined every function containing a comparison (go/types gives `a < b` the type
"untyped bool"), so MinIO had passed with 2,117 functions; with the fix, 4,458.
Three bugs on the way, all caught before MinIO: the planner did not add the runtime to a program that
imports nothing; the assembler's tables are initialised on first use without a lock, so parallel
emitters raced; without a debug-info function count the link overflowed a one-byte index.

**Trace.** Worse, as predicted: 23.7 s cold against 21.3-24.2 s for the same configuration without
sbc. sbc's whole-program parse and check is 1.0 s of wall clock, at present before the first compile
starts; the 4,458 functions it takes save the stock compiler next to nothing. This rung is the
skeleton: every function sbc takes from here on is in the binary the smoke test runs, and coverage
is now a number that can be pushed (4.5% of functions, 0.3% of text).

**Scope check.** Most ambitious: functions with stack frames, so that sbc can take functions that call
other functions. That is the real compiler: a frame layout, a stack map for the garbage collector,
the stack-growth prologue with its register spills, the calling convention on both sides. It is also
where a mistake stops being a wrong answer and becomes memory corruption that a smoke test may not
see, so the differential harness has to grow with it (calls, recursion deep enough to grow the
stack, allocation pressure to make the collector scan the frames). Doing that next. Before it, one
cheap fix: let the packages sbc never touches (the runtime and its dependencies) start compiling
while sbc is still checking, so its second is not on the critical path.

## 2026-11-12 00:50 UTC — iteration 11: rung 5 for real. A baseline code generator inside the compiler moves the trace.

**Why inside the compiler.** The second skeptic review (REVIEWS.md) argued for a hybrid back end inside
the stock compiler rather than my outside one: keep its typing and its runtime lowering, intercept
functions before SSA, fall back for the rest. sbc had proved the seam, but from outside it has to redo
every lowering (range, maps, strings, interfaces, closures) and cannot ask for type descriptors. Before
committing, I measured the ceiling: the patched compiler recorded the operations in every function's
lowered tree (`SB_OPSTATS`, `tools/trace/opstats.py`; 209,794 functions for MinIO). With about 45
operations a code generator covers 35-40% of back-end time; with interface calls and type assertions,
most of it.

**Did.** `driver/baseline/sbbaseline.go` (1,750 lines, compiled into cmd/compile/internal/ssagen by
`driver/mkcompiler.sh`, on with `SB_BASELINE=1`). Every function reaches it before SSA. It walks the
tree that `walk` left and writes machine code directly: no SSA construction, no optimization passes,
no register allocation. Whatever it does not understand makes it give up (a panic with the reason,
recovered at the top, logged with `SB_BASELINE_LOG`) and the function goes to SSA untouched.
The design is the simplest one that is safe under the runtime:
- every variable lives in memory (register parameters in their ABI spill slots, locals and temporaries
  in typed frame slots); a value is in AX between two tree nodes and nowhere across a call;
- the frame is zeroed on entry and there is one stack map, every pointer word of every slot, live
  throughout (two entries: at entry only the parameters, afterwards also results kept on the stack);
- the whole body is an unsafe point, so the function is preempted only at calls;
- pointer stores outside the frame go through the write barrier; variables whose address escapes are
  allocated where they are declared, as the stock back end does.
It handles integers of every width, pointers, strings, slices, interfaces, structs and arrays; field,
index and slice expressions with their bounds panics; division and shifts with Go's corner cases;
static, closure and interface calls with register and stack arguments and results; wrappers; switches.
Not yet: closures' captured variables, defer, floating point, `append`, type assertions, intrinsics.

**The oracle.** `tests/sb_stdtest.sh` builds and runs the standard library's own tests with the
baseline on (the patched compiler is installed in a copy of GOROOT). Stage 1 (scalars and static
calls): 29 of 29 packages passed, 3,017 functions. Stage 2 first run: 11 of 29. `tests/sb_bisect.py`
halves the set of functions the baseline may take by a hash of the symbol name; in ten runs it pinned
encoding/base64's failure on one function, the package's `init`. Cause: a local whose address escapes
is moved to the heap by the *back end* (not by `walk`), and I was giving it a stack slot. With that
fixed: **29 of 29 packages pass, 33,760 functions (71%) compiled by the baseline.** Two more bugs were
found by small programs before the oracle ran: indexes that `walk` marks as proven may point one past
the end (I was checking them), and DIV takes its operand in the other slot of the instruction.

**Trace.** MinIO: **158,874 of 199,662 functions (80%) go through the baseline**, the binary passes the
S3 smoke test. Cold, interleaved with the previous configuration because the host drifts:

| | wall s (two cold runs each) | compile core-s |
|---|---|---|
| run `04-selfplan` (SSA for everything) | 25.1, 21.9 | 157, 139 |
| run `06-baseline` | **20.4, 20.4** | **119** |

Warm it is 18.5 s. The binary is 153 MB instead of 100 MB: unoptimized code is bigger. Link time is
unchanged at 1.5 s.

**What serializes it now.** Still work: 119 core-s / 8 = 15 s. The front end (parse, check, two IR
conversions: about 30 core-s) is now the largest piece the baseline does not touch, then the 20% of
functions still going to SSA (which are the big ones), then the assembler, which now encodes a third
more instructions.

**Scope check.** Most ambitious: the remaining 20% of functions (closures, defer, append, type
assertions, floats), which by time are much more than 20% of the back end; then make the baseline's own
output smaller, since every byte of it is assembled, written, read by the linker and written again.
Both are the same rung continuing. Doing that, with the oracle after every step. In parallel the
correctness evidence has to widen: more of the standard library, and MinIO's own unit tests built with
the baseline (the charter's "unit tests of the packages we touch": this touches all of them).
