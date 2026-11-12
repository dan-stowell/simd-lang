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

## 2026-11-12 00:34 UTC — iteration 11: rung 5 for real. A baseline code generator inside the compiler moves the trace.

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

## 2026-11-12 01:10 UTC — iteration 12: the baseline to 97% of functions; sharding a package (works, does not pay here)

**Baseline, stages 3 and 4** (each step through `tests/sb_stdtest.sh`, MinIO's smoke test, and
`tests/sb_bisect.py` when something broke). Added: captured variables, `defer` and `go`, `append`, type
assertions to concrete and to interface types, interface type switches, `select`, calls of assembly
(ABI0) functions and of functions the optimizer would have replaced by instructions, jump tables,
`min`/`max`, `unsafe.Add`, slices moved from the stack to the heap, copies and clears of large values;
then fewer instructions: conditions jump directly, small constants are immediates, simple operands are
loaded without a temporary.

Bugs the oracle found, all by bisection to one function and then a 20-line reproducer:
- a closure that is called where it is written gets its captured variables as extra parameters, not
  through the context register (slices' TestGrow: a deferred closure read a nil pointer);
- every call owes its callee a spill area at the bottom of the frame, also the runtime helpers I call
  without an ABI analysis (container/heap: growslice overwrote my temporaries);
- `runtime.Caller` and `log` want the call's own position, not that of its argument set-up.

Two kinds of test fail with the baseline and are not bugs: tests that count frames across *inlined*
calls (the baseline writes no inline tree; my builds use `-l`), and tests that count allocations or wait
for prompt collection (no inlining; and a slot that once held a pointer keeps its object alive until
the function returns). With the build's flags the wide run (110 standard-library packages, 127,185
functions through the baseline) has no other failures.

**Trace.** MinIO, cold, three runs: **16.3, 18.0, 16.4 s** (stock 54.2 s). 96.8% of functions bypass
SSA; compile is 87 core-s. kube-apiserver cold: **28.5 s** (stock 86.3 s; 43.9 s before the baseline),
smoke test passes. The binary is 176 MB (optimized: 100 MB); the link is still 1.4 s.

**Where the compile CPU is now** (profile, 70 s): the front end 31% (type check 7.5, export IR written
4.5 and read back 9.1), emitting machine code 22% (the baseline's walk about a third of that, the
assembler the rest), escape analysis / wrappers / runtime types 13%, lowering 9%, the SSA that is
left 8% (for 3% of functions: the ones with floating point), objects 7%.

**The chain is back.** With the back end cheap, `minio/cmd` (5 MB) starts when the last front end
before it is done (9.6 s) and then takes 4.7 s nearly alone. Two attempts:
- *Pause back ends beyond a cap* (`-running n`) or run them under SCHED_IDLE (`-idle`): the package
  starts up to 2 s earlier and the build ends no sooner; the work only moves.
- *Shards* (`driver/baseline/sbshard.go`, `-shard bytes`): K compiler processes for one package, each
  type-checks all of it and then keeps its share of the function bodies; shard 0 alone emits data,
  types, initialization and export data; the others' objects join its archive. It works: MinIO with
  every package over 20 kB sharded passes the smoke test (after four fixes: static temporaries belong
  to the shard that uses them, methods named `init`, object fingerprints, instantiated generic types).
  It does not pay on 8 cores: on an idle machine 8 shards finish `minio/cmd` in 3.1 s against 4.3 s
  unsharded, because every shard repeats the 1.1 s type check, and in the real build that extra CPU
  competes with everything else: 17.3-17.8 s against 15.9-16.3 s. Off by default. It would pay with
  more cores, or if the shards other than 0 dropped bodies before type checking.

**Scope check.** Most ambitious: replace the front end too (whole-program go/types, measured at 1.4 s
wall in iteration 9, feeding the baseline's kind of generator). Why not now: the baseline consumes the
stock compiler's lowered tree, which only its front end produces; a second lowering is the months of
work I avoided by moving inside the compiler. What is left tonight inside this design: the last 3% of
functions (floating point), an optimization-only pass that still runs, and correctness evidence
(MinIO's own unit tests under the baseline).

## 2026-11-12 02:00 UTC — iteration 13: floating point; two memory-safety bugs and a compiler race, found by stress

**Floating point** in the baseline: a float travels between tree nodes as its bit pattern in the
integer accumulator, so loads, stores, temporaries and copies needed no new code; arithmetic,
comparison (with NaN's rules), conversion and the calling convention use the SSE registers. 98% of
functions in the oracle's packages and 97% of MinIO's now bypass SSA. The devirtualize-and-inline pass
is skipped in baseline mode with `-l` (it has nothing to inline and the rest is optimization).

**The important part: bugs that only repetition finds.**
1. One run in five of the oracle died in compress/flate with a dump of heap words and no test failure.
   Repeating the tests (`-count=8`) at three collector settings made it reliable: `found bad pointer in
   Go heap`, and go/printer with `found pointer to free object`. The "object" in the report was a
   goroutine stack and the pointer was exactly one past the end of an allocation. The lowered tree
   computes such addresses on purpose (`&s[len(s)]`, the destination of a copy of nothing) and hands them
   straight to memmove; the stock back end keeps them in a register for three instructions. My design
   parked every pointer-typed intermediate in a stack slot that the collector reads for the rest of the
   function. Two fixes, both from one rule I should have started with: *an intermediate needs to be
   visible to the collector only if a returning call can happen while it waits.* Argument and operand
   temporaries are now unscanned unless that is the case (`sbMayCall`), and the compiler's own
   temporaries that only ever hold an unchecked element address get unscanned slots
   (`sbPassingAddresses`; found by mapping the reported stack address to a frame offset to a store
   instruction to a source line: `want = append(want, abcabc[:n]...)`).
2. One MinIO build in ten failed: the *compiler* crashed with `concurrent map read and map write`. I
   was calling the package symbol table from the parallel back-end workers to make keys for labelled
   break/continue. A map of the function's own now.

**Evidence after the fixes.**
- 15 packages x 8 repetitions x GOGC 5/20/100: clean.
- 107 standard-library packages x 3 at GOGC=10 with the build's flags: the only failures are four tests
  that count allocations, and the same four fail with the unmodified compiler at `-l`.
- `tests/minio_stress.sh`: the server at GOGC=5, 300 objects of mixed sizes written, read back, compared,
  listed, deleted, eight at a time: passes.
- Ten consecutive MinIO builds: ten successes.

**Trace.** MinIO cold, four runs: 16.4, 16.7, 17.5, 16.6 s. kube-apiserver cold: 29.1 s. No real change
from iteration 12: the functions that were left were few.

**What this says about the approach.** The baseline's safety argument was "everything is in memory and
always live, so the collector can never miss anything". True, and not the whole requirement: the
collector must also never *see* something that is not a pointer. Always-live maps trade one for the
other. The standard library's tests did not find it in a single pass; the same tests repeated under
collector pressure did, in minutes. The stress run is now part of the oracle.

**Scope check.** Most ambitious: unchanged (front end). What I am doing instead for the rest of the
night: consolidate. The pipeline is 3.3x faster than stock on MinIO and 3.0x on kube-apiserver with a
code generator that is five hours old; the most valuable thing left is evidence that it is right, a
smaller generated binary (176 MB against 100), and a report the user can act on.

## 2026-11-12 03:16 UTC — iteration 14: where the CPU really goes; huge pages (spike, positive); the giant package sharded

**Trace.** MinIO cold, four runs: **13.3, 12.3, 12.6, 12.9 s** (iteration 13: 16.4-17.5; stock 54.2).
kube-apiserver cold, two runs: **24.1, 25.2 s** (was 28.5-29.1; stock 86.3). Both pass their smoke tests;
MinIO passes the stress test. Configuration `08-huge-shard`.

**What moved it, largest first.**

1. *Explicit huge pages for the tools' heaps (weird-idea spike; -2.6 s on MinIO, -4 s on kube).* The
   question was why 20 of MinIO's 87 compile core-seconds are kernel time. Answer, counted from
   /proc/vmstat: the compilers run with the collector off, touch 30 GB of fresh memory, and take 7.8
   million page faults at about 3.6 us each (a 4 kB fault under KVM costs about fifteen times what
   zeroing the page does). This kernel has no transparent huge pages (HARDWARE.md) but it does have
   hugetlbfs. The patched runtime (`SB_HUGE=1`) maps each 4 MB piece of heap growth from the explicit
   pool, falling back to ordinary pages when the pool is empty; the first 4 MB of every process stay
   ordinary, so the thousand small compiles never touch the pool. One compile of `minio/cmd`: 4.1 s
   -> 3.0 s wall, kernel time 1.45 -> 0.29 s, and user time falls too (5.6 -> 4.9 s: fewer TLB misses).
   Whole build: faults 7.8 M -> 1.7 M; kernel 21 -> 11 core-s; user 71-78 -> 65-68. The linker built
   against the same runtime (`build/link-sb`, `-linker`) links in 1.28 s instead of 1.48.
   *Cost and caveat:* it needs `sudo sysctl vm.nr_hugepages=N` (3,072 = 6 GB for MinIO, whose peak use
   was 4.2 GB; kube exhausted 10 GB and fell back for the rest). `tools/trace/sb.sh HUGEPAGES=n`
   reserves the pool before the run and puts the old value back afterwards. Hugetlb memory is not
   counted by the scope's MemoryMax, so the kube run lowers that cap to 16 G. This is machine
   configuration, not a cache: it holds nothing from a previous build. It is still a decision for the
   user, so MORNING.md gives the numbers both ways. (The cheaper variant, `SB_POPULATE=1`: one
   madvise(MADV_POPULATE_WRITE) per 4 MB instead of a thousand faults, no pool needed: halves the
   kernel time of a big compile, about -2.5 core-s on the whole build.)

2. *The one giant package compiled as shards (-1.0 s on MinIO).* The utilisation timeline of the build
   has two idle stretches: the first two seconds (plan, then the runtime's front end alone) and the
   last five (`minio/cmd`'s front end on one core, seven idle). Shards now drop other shards' bodies
   before type checking (`sbShardFilesPre`), shard 0 type-checks everything (initialisation order needs
   that) and emits the data. Measured: a function shard has about 1.15 core-s of fixed cost (parse,
   declarations, imports, object), so sharding everything above 400 kB costs 108 core-s against 87 and
   loses; eight shards for `cmd` lose to four for the same reason; four shards for the one package
   above 3 MB win a second. kube's giant (`pkg/generated/openapi`, 19 s under SSA) takes 2 s with the
   baseline and is no longer the tail; kube is CPU-bound from start to finish.

3. *Denser baseline code (-5 core-s, binary 180 -> 160 MB).* An instruction census of the baseline's
   output for five library packages (68 k instructions against 28 k optimized): 54% frame loads and
   stores, 19% word-by-word copies through R8. Now: two words at a time through X14 (and zeroing from
   X15), call results stored straight into the destination variable when nothing has been emitted
   since the call (the stores are retargeted, not repeated), constants stored in one instruction, more
   operand forms loaded directly into argument registers (fields, lengths, `&x`, string constants).
   60 k instructions on the same sample. The assembler is the largest single item in the compile
   profile (12 core-s), so instructions not emitted are saved three times: generator, assembler, linker.

**Measured and not built: interface-first export** (the skeptic's proposal, twice). A scheduling
simulation over the real trace (per-package front-end and back-end CPU, real dependency graph, 8
cores): today's rule gives 10.3 s for the compile row, the CPU bound is 9.3 s, and exporting
interfaces before bodies gives 9.9 s at best (if an interface pass costs 15% of a front end, and only
for the 150 largest packages) and 10.4-12.3 s otherwise. The build is CPU-bound; overlap bought by
repeating work is paid for in the same currency. Recorded as negative without building it.

**Evidence of correctness added.**
- MinIO's own unit tests (`tests/minio_unit.sh`: `go test -short ./...` with the patched toolchain,
  baseline on, the build's flags): 45 packages pass; `cmd` (245 s of tests) has one failure,
  `TestTrackingResponseWriter`, which fails identically with the baseline off (the test writes a body
  after status 123 and this Go's httptest refuses). Caveat: I rebuilt the compiler twice while that run
  was in progress, so it is a run of three consecutive baselines, not one. To be repeated with the
  final compiler before morning.
- A limitation found by widening the stress set: `net/http`'s `TestTransportGCRequest` never finishes.
  It waits for a request to be garbage-collected, and a baseline frame keeps every slot alive until
  the function returns: the transport's read loop is still holding the last request while it waits for
  the next one. No crash, no wrong answer, and nothing Go promises, but a program that relies on a
  finalizer firing while a long-lived goroutine sits in a loop will wait longer. Same class as the
  sync/reflect promptness tests of iteration 12. Listed in MORNING.md as a real difference in behaviour.
- The standard-library oracle (both flag sets), the stress subset, MinIO smoke and stress, kube smoke:
  pass with the final compiler of this iteration.
- The correctness reviewer's session died of a stalled model request after five minutes. Fired again.

**Scope check.** Most ambitious next thing: a front end of our own feeding the baseline generator
(whole-program go/types of MinIO is 1.4 s wall). Why not that: the compile profile is now 46 core-s
of compiler work under 75 of compile row; type check + IR write + IR read are 14 of them, and replacing
those means replacing `walk`, which is where the baseline gets its input. Not an overnight step, and
tonight's two largest wins came from asking where the time goes rather than from replacing a phase.
What I do instead: (1) the remaining kernel and start-up time (1.7 M faults left, 1,312 process
starts at 9 ms); (2) the head of the build (plan, then the runtime alone: 2 s at 1-2 cores);
(3) repeat the evidence with the final compiler.

## 2026-11-12 12:40 UTC — iteration 15: F0 (the fair row, the tax, an exact CPU table) and F1 (a fork-server: works, and is worth 1.5 core-seconds)

**Trace.** MinIO cold, four runs of `09-forkserver`: **15.6, 14.0, 12.5, 13.0 s**; kube-apiserver, two:
**24.5, 22.5 s**. Both pass their smoke tests. The host was noisier than last night (the same warm build
cost 71, 81 and 84 compile core-seconds within half an hour), so the honest reading is "no change in
wall clock from `08-huge-shard`" (12.3-13.3 and 24.1-25.2). What this iteration produced is measurement,
and it changes what the next rungs are worth.

**Loose ends from last night.** Both inbox reviews triaged (the skeptic's was already in REVIEWS.md; the
correctness reviewer's file holds only a session id). The 03:20 divergence round survived in IDEAS.md and
is committed with its triage. `tests/minio_unit.sh` is running again with the final compiler as I write.
SCALAR_LEDGER.md brought up to date.

**F0, the fair stock row.** `GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`,
cold, 8 CPUs: MinIO **32.5 s** (169 core-s), kube-apiserver **51.2 s** (329 core-s). Plain stock in the
same hour: 50.0 s and 77.1 s. So of last night's "54 s -> 12.5 s", 54 -> 32.5 is turning things off and
32.5 -> 12.5 is technique (early export, the baseline generator, huge pages, shards, our own plan).

**F0, the tax.** New in TRACE.md, for stock, fair stock and this build: tasks created, tool processes,
page faults, bytes of intermediates written and read back. MinIO: stock writes 1.9 GB of archives (each
twice: work directory, then GOCACHE) and reads 2.7 GB back; this build writes 733 MB to tmpfs and reads
1.26 GB back; faults 11.9 M -> 1.3 M.

**F0, the attribution, and a correction.** Last night's profile table put "46 s of samples" under a
75 core-s row and I filled the gap with a guess (process start 12 core-s, kernel 20). I replaced the
guess with a measurement: the patched compiler logs its own rusage at every phase boundary
(`SB_CPUPHASES`, `tools/trace/cpuphases.sh`); phases are sequential, so the differences are exact
whatever the machine is doing. One warm MinIO build, 1,315 compiler processes, core-seconds:

    be:compilefuncs   34.0   walk, baseline generator, assembler            compiling
    fe:parse          28.6   parse, types2, write export IR, read it back,  compiling + moving data
                             read imports
    be:dumpobj         3.9   object files                                   moving data
    before main        3.6   exec, runtime start, package init (654 k of    process setup
                             the 1.6 M faults)
    fe:earlyexport     2.7   export data for importers                      moving data
    wrappers, escapes  4.8                                                  compiling
    init, loadsys      1.5                                                  process setup
    exit, asm, pack    1.7

Three things I had wrong. (1) Process start is 3.6 core-s, not 12. (2) The front end is 35% of the row,
not 20%: the sampler under-counted it (many short processes). (3) The wall-clock `-bench` timers are
useless here (be:compilefuncs "290 s"): back ends sit at nice 19 waiting for a core.

**What serializes each phase, and what fusion can remove (the charter's question, answered from the
table).** Pure setup and data movement that one address space removes outright: before main 3.6 + init
1.5 + dumpobj 3.9 + earlyexport 2.7 = **11.7 core-s**. Inside fe:parse, by last night's profile shares,
about 40% is type checking and 60% is the export-IR round trip and import reading: **~17 core-s** that
exists only because the compiler's IR can only be built by deserializing. The rest, **~50 core-s, is
compiling** (types ~11, walk + generate + assemble 34, wrappers/escape 5). So perfect fusion *of the
stock compiler's work* ends near 50 core-s = 6.3 s on this machine. Two seconds needs the compiling
itself cut by three: our own lowering and an emitter that does not go through `obj.Prog`. Fusion is
necessary and not sufficient; F2-F4 have to bring a cheaper compiler with them, not only a different
process structure.

**F1: fork-server (built, consumed, differential-tested) vs persistent worker (measured, not built).**
- *What was built.* `runtime.SBFork` (driver/runtime/sbfork.go, 60 lines): fork without exec in a Go
  process. Stop the world, lock out sysmon, fork; in the child drop the idle Ms (their threads do not
  exist), fix the thread id, start a new sysmon. The compiler with `SB_ZYGOTE=<fd>` finishes start-up
  and waits on a socket; the driver (`-zygote`) sends each compile's directory, environment, argv and
  two pipe ends; the zygote forks and the child carries on into the ordinary `gc.Main`. Exit status and
  rusage come back through the zygote's wait4.
- *Kill test first:* 6,000 forked children each running 16 goroutines, maps, timers and (a third of
  them) a forced collection, at GOGC 100 and 5: no failure, no hang.
- *A bug the first real build found:* the binary failed in `encoding/gob` init ("*reflect.rtype is not
  *reflect.rtype"). Three of my own package-level variables read the environment at init
  (`SB_EARLY_EXPORT`, `SB_SHARD` twice), i.e. in the zygote, before the compile's environment existed.
  The zygote now starts with the environment every compile gets, and the two per-compile values are
  read again in the child.
- *Correctness evidence:* `tests/sb_zygote.sh`: every export file, object and the binary byte-identical
  to one exec per compile (cmd/go: 664 files; MinIO: binary SHA-256 identical); a compile error still
  fails the build with the compiler's message. MinIO and kube smoke tests pass.
- *What it is worth* (interleaved A/B, 4 rounds, warm): compile core-s 67.9/66.2, 70.9/69.0, 75.6/72.6,
  71.4/71.7: **-1.6 core-s on average, wall clock unchanged** (11.80 vs 11.77 s). Faults 1.60 M -> 1.17 M.
  Why so little: fork does not copy the page tables of the 37 MB of mapped binary, so the child takes
  those faults again; and "before main" was 3.6 core-s to begin with.
- *Persistent worker:* its ceiling is the same rows (before main + init + loadsys + exit: ~6 core-s),
  and it needs every global in cmd/compile reset between packages. Not built: the ceiling does not pay
  for the audit.
- *Verdict on F1:* done, and nearly a negative result. Kept (`-zygote`, off by default in the driver, on
  in the trace configuration) because fork-without-exec is a tool the later rungs can use: it is the
  cheap way to hand a typed whole program to many workers without export data, *if* the heap is on huge
  pages (page-table copy of a multi-GB 4 kB heap would cost tens of ms per fork; noted for F2).

**Divergence step.** Due by count (15). Not fired: a round ran at 03:20 after iteration 14, nothing had
been built since, and usage is the binding limit. It runs next iteration, when the lenses can see the
exact CPU table, which is new.

**Scope check.** Most ambitious thing I could attempt next: F2 proper: type-check the whole program
once in one address space and hand typed packages to back ends in memory. Why not this iteration: F0 and
F1 were the charter's next rungs and F0's table is what tells me which half of F2 pays (the 17 core-s
round trip, not the 3.6 of process start). Next iteration goes at F2's cheapest consumed slice.

## 2026-11-12 13:05 UTC — iteration 16: divergence round, four ideas killed by measurement, a one-pass assembler (-6 core-s), review fixes

**Trace.** MinIO cold, four runs of `10-onepass`: **11.7, 11.6, 13.5, 12.9 s** (67 core-s in the first;
`09`: 12.5-15.6). kube-apiserver, two: **21.1, 23.2 s** (137 core-s; `09`: 22.5-24.5). Both pass smoke;
MinIO passes the stress test. Objects written: 641 -> 573 MB. The host is noisy enough that single runs
overlap between configurations; the interleaved comparison is below.

**The front end split exactly** (finer `SB_CPUPHASES` boundaries; one warm MinIO build, 72 core-s row):
parse 1.9, **type check 8.9**, **write export IR 5.0**, **read it back as compiler IR 10.1**. The round
trip is 15.1 core-s, a fifth of the compile row, and it is the package's *own* IR, not imports.

**Divergence step** (three lenses on a cheaper model, each saw CHARTER, TRACE, HARDWARE; nine ideas;
IDEAS.md has all of them). Triage, with the kill tests I ran:
- *Resident typed universe, importers follow pointers* (two lenses). Their premise is that reading
  imports is expensive. The merged profile says it is not: `importer.*` 0.4-0.7 s and `readImportFile`
  1.1 s of 51.8 s of samples, 1-3%. Import decoding is lazy and cheap; what costs is serializing and
  deserializing the package being compiled. **Dead as stated.** What survives is harder: build the
  compiler's IR straight from the checked syntax (no export IR for the own package).
- *Bump allocator, never free* (the allocator is 11% of samples). Go ships one: `GODEBUG=sbrk=1`.
  On go/types it changes nothing (0.56 s against 0.57 s CPU) and it deadlocks intermittently with
  concurrent back ends. **Dead in 15 minutes.** (The first attempt hung the whole build: the setting
  reached the driver. `SB_COMPILE_ENV` now passes environment to compiler processes only.)
- *Emit each shared type descriptor once across the graph.* Logged every descriptor the build emits
  (`SB_TYPELOG`): 371 k emitted, of which 331 k are the kind every needing package emits, with 225 k
  distinct names: 106 k duplicates, **28.6%**. Descriptors are 8% of compile samples, so a perfect
  cross-package claim table saves about 1.6 core-s. Under my 40% threshold: **dead**. (Surprise worth
  keeping: most descriptor work is unique anonymous types, 225 k of them; `func() string` is the most
  repeated at 613 copies.)
- *Persistent pinned workers with arena reset*: ceiling is the setup rows, ~4.5 core-s after the
  fork-server. Dead by the table, not built.
- *Lazy compilation in the output binary*: moves the build into the run; not a build. Rejected.
- *Template emitter / columnar codegen, presized layout* (two lenses): this is F3/F4's emitter. Not
  tested this iteration; the assembler result below changes its arithmetic.

**The consumed change: one assembler pass instead of 3.3** (the budgeted optimization iteration; the
profile pointed at it: `obj.Flushplist` was 13.5% of compile samples).
- `x86.span6` assumes every forward branch is short and re-assembles the whole function each time one
  does not fit. Counted on go/types: 2.68 passes per function, 3.33 weighted by bytes, and *every*
  function at least two, because padding jumps to 32-byte boundaries (the Intel JCC erratum
  mitigation) needs sizes from a previous pass.
- Now, in the baseline build only: a forward branch more than 12 instructions ahead starts out long
  (`driver/x86/sbspan.go`), and jump padding is off. Result: exactly one pass for all 1,376 functions
  of go/types, and the text is 0.7% *smaller* (no padding) despite the longer branches.
- Also: with `-dwarf=false` the stock compiler still builds a DWARF subprogram entry and line table per
  function. The baseline build skips them (requires `-w` at link, which the build already uses).
- Measured. One package, min of 15 alternating runs: 566 -> 520 ms for one pass (8.5%), -> 504 with the
  DWARF skip. Whole MinIO build, four interleaved rounds, compile core-s: old 75.4, 81.0, 67.4, 76.0;
  new 77.2, 67.5, 60.9, 69.7 (**min 67.4 -> 60.9, mean 74.9 -> 68.8**); wall mean 12.4 -> 11.7 s.
  Profile after: Flushplist 7.0 -> 4.0 s of samples, span6 4.3 -> 1.7.
- Evidence: standard-library oracle 27 of 29 packages (the two failures, `TestNewBufferShallow` and
  `TestAllocationsDecode`, count allocations and fail identically with the old assembler path and with
  the stock compiler at `-l`); MinIO smoke and stress; kube smoke; `tests/sb_early.sh` (idle patched
  compiler byte-identical to stock: the change is gated on `SB_BASELINE`); `tests/sb_zygote.sh`.
- Behaviour difference for MORNING.md: no JCC-erratum jump padding (matters for speed on some Intel
  cores, not for correctness; this is an AMD machine and a dev build).

**Reviews.** The correctness reviewer returned a real review of the fork-server
(`reviews/forkserver-2026-11-12/`): MinIO byte-identical fork vs exec (2,624 files), 648-fork stress
clean, and two confirmed findings, both fixed with regression cases in `tests/sb_zygote.sh`:
(1) if the zygote died after taking a wait request, the empty reply parsed as exit status 0, a compile
"succeeded" (now: anything but a full five-field reply is a failure); (2) an environment + argv over
the socket's message limit (425 kB here) failed the build where exec would not (now falls back to
exec for that compile; the zygote also refuses truncated requests). The skeptic's review agrees with
iteration 15's arithmetic and asks again for a consumed F2/F3 skeleton; triaged in REVIEWS.md.

**MinIO unit tests with one fixed compiler** (the loose end): 45 packages pass, `cmd` (253 s) has the
one known failure, `TestTrackingResponseWriter`, identical with the baseline off.

**Scope check.** Most ambitious next: the F2/F3 skeleton the skeptic names (whole-program typing once,
our own lowering and direct emission for a function subset, linked into MinIO). Why not that with the
two and a half hours left: `05-sbc` was that skeleton and it lost 2 s, because every function it does
not take still pays the stock front end *and* the build pays a second whole-program type check; it pays
only when whole packages leave the stock compiler, which needs export data written without it. What I
do instead, in the fusion direction and on the trace: the first edge of the DAG is not streamed. The
planner runs for 0.9 s on 2 cores while 6 idle, and only then does `runtime` (which everything waits
for) start compiling. Start the runtime's closure at time zero, in parallel with the plan.

## 2026-11-12 13:25 UTC — iteration 17: the first edge of the DAG, streamed (head start); worth 0.2-0.4 s, below today's noise in cold wall clock

**Trace.** MinIO cold, four runs of `11-headstart`: 13.8, 15.8, 13.0, 13.2 s; kube-apiserver, two:
23.8, 20.5 s. Those four MinIO runs fell in a noisy stretch (79 core-s for a build that costs 61 in a
quiet minute), so they say nothing about the change. The evidence is an interleaved cold A/B run
straight after, four rounds, same configuration with and without `-headstart`:

    off  11.37  11.84  12.31  12.12 s     compile core-s 61.4  64.9  68.0  66.8
    on   11.56  11.59  12.79  11.89 s                    65.5  63.6  73.3  66.2

Raw means 11.91 vs 11.96 s: nothing. The two rounds where both runs cost the same CPU show -0.25 and
-0.23 s; correcting the other two for their extra CPU (0.145 s of wall per core-s, from the off runs)
gives -0.40 and -0.30. Warm page cache, four rounds: mean 12.15 -> 11.72 s, min 11.44 -> 10.76 s. I
call it **0.2-0.4 s, not resolvable in cold wall clock on this host today**.

**What was built.** The planner takes 0.6 s warm and 0.9-1.0 s cold on about two cores, and only then
does the scheduler start `runtime`, which everything waits for. With `-headstart` the driver makes a
second, tiny plan at time zero (the runtime's closure: 29 packages; `planner.run("")`) and compiles it
while the real plan is being made, in its own directories. When the real plan arrives, the scheduler
adopts each of those packages if the plan describes it identically (directory, files, imports), by
swapping `buildPkg` for a function that waits for the head start's export and exit. Same command
lines, so the same bytes.

**What it does to the graph** (structural, so not subject to the noise): `runtime`'s export data is
ready when the plan ends (before: 445 ms later); `fmt` starts at 1,088 ms instead of 1,426; cores busy
during the plan row 2.3 -> 5.1.

**Why the gain is small, in writing (the charter's question).** What serializes the head is the plan,
and the head start does not shorten the plan: it fills five idle cores for half a second, 2.5
core-s of capacity in a build bound by 61 core-s of work. The remaining idle time at the head is the
plan's own: rounds of directory reads waiting on a cold disk. Streaming the *whole* plan into the
scheduler (compile a package when its imports are resolved, not when the last round ends) would use
the same idle cores for more than 29 packages; the head start is the cheap first slice of that.

**Evidence.** `tests/sb_zygote.sh` has a head-start case: 29 packages compiled during the plan, every
archive and the binary byte-identical to the ordinary build (cmd/go). MinIO: binary SHA-256 identical
with and without; smoke passes in every run above; kube smoke passes.

**Scope check.** Most ambitious next: F4, the link. It is the largest serial item left (1.2 s link +
0.3 s write at 1.5 cores, 13% of the build) and nothing built so far touches it. Why I am not building
a linker in the hour and a half left: no measurement yet of where the stock linker's time goes *on
the baseline build's objects* (last night's profile was of the optimized build with DWARF). That
measurement is the next step, and it decides between overlapping the link with the compile tail and
replacing layout + relocation.

## 2026-11-12 13:35 UTC — iteration 18: the tail. Direct output (-0.14 s), a streaming-link spike killed in 25 minutes, a review finding fixed

(Times in these headers are the VM's clock, which ran slow against my sense of elapsed time today; the
order of entries is right.)

**Trace.** MinIO cold, four runs of `12-directout`: **11.5, 12.3, 11.6, 11.6 s** (71 core-s in the
first); kube-apiserver, two: **20.1, 22.3 s**. Both pass smoke; MinIO passes the stress test. Link +
write output: 1,459 -> 1,316 ms (three interleaved rounds; these two rows are serial and quiet).

**Where the linker's 1.22 s goes on the baseline build's objects** (`-benchmark=cpu` on a kept work
directory; 1,312 archives, 573 MB): loadlib 439 ms, deadcode 246, asmb 118, pclntab 114, dodata 112,
symtab 52, everything else about 140. Explicit huge pages are worth 0.2 s of it (1.39 -> 1.16-1.23 s).

**Direct output (consumed).** The linker wrote the binary to tmpfs and the driver copied 160 MB to the
disk and synced. With `-directout` the linker maps the destination file itself and the driver only
syncs: write row 250-271 -> 89-105 ms, link row unchanged. Same bytes (`tests/sb_zygote.sh` compares).
A note on fairness that cuts against us: our total includes the sync of the binary; the stock rows do
not sync theirs (stock.sh syncs after its clock stops). I have left it that way.

**Spike: start the linker early and stream objects into it (negative).** Idea from the divergence
rounds and the skeptic: the link is serial and last; let it ingest objects as compiles finish. Built a
60-line patch to cmd/link (`SB_LINK_STREAM=<fd>`: load each object as its line arrives, then the
ordinary loop skips what is loaded). Two findings:
1. *The output depends on load order.* The same 1,312 objects streamed in plan order, reversed and
   shuffled give three different binaries (first definition of a duplicate-allowed symbol wins, and
   symbol indices follow load order). Streamed in the stock linker's own order (main's imports
   breadth-first, in source order) the binary is byte-identical, but `minio/cmd` is third in that order
   and last to finish compiling, so the stock order cannot be streamed. A deterministic order of our
   own (say by size) would work and change the bytes.
2. *It is not worth it anyway.* Loading the objects is **55 ms** of loadlib's 440; the other ~390 ms
   is symbol resolution over everything, which needs every object. Kill threshold was 150 ms.
   **Dead; patch removed.** What it leaves for F4: an incremental linker has to resolve symbols as
   objects arrive (dense IDs handed out by the compiler would make that a table write), and deadcode
   (246 ms) is the next serial piece behind it.

**Review (correctness, one-pass assembler; `reviews/instruction-encoding-2026-11-12/`).** Its
differential work found no branch-sizing fault: 10,800 generated results and a 25,515-call boundary
sweep across displacement 128 match stock and the old path; every function assembled in one pass
where the old path took 2-7; runtime metadata checks (Callers, stack growth, panics with exact lines)
match. One confirmed finding, **P1: a baseline object linked without `-w` makes the stock linker
panic** (`writedebugaddr` wants the per-function DWARF symbols I skip). I had written "requires -w"
in the journal and not enforced it. Fixed: the compiler skips those symbols only when told to
(`SB_NODWARFSYMS`), and only the driver sets that, when its own link flags contain `-w`. Regression
case in `tests/sb_zygote.sh` (baseline build without `-w` links and runs). The trace configuration is
unaffected (objects still 573 MB).

**What serializes the tail now (in writing).** After `minio/cmd`'s front end (1.1 s on one core per
shard) and back end, the link is 1.2 s at 1.7 cores and the sync 0.1 s. Nothing can overlap the link
with the stock loader, by the measurement above. The tail is F4 or nothing.

**Scope check.** Most ambitious next: F4's first consumed slice: our own layout and relocation for a
subset of the image, or symbol resolution moved into the compilers (dense IDs) so the loader's 390 ms
becomes a table. Why not now: it needs a design that survives contact with pclntab and deadcode, and
LINKING.md predates everything measured today. The next session should start there with the numbers
in this entry. For the rest of this session: keep MORNING.md exact, and stop spending usage on steps
worth a tenth of a second.

## 2026-11-12 13:50 UTC — iteration 19: the head start widened to the standard library (no measurable gain); unit tests with the final compiler

**Trace.** Unchanged: `12-directout` stays the trace configuration (MinIO cold 11.5-12.3 s,
kube-apiserver 20.1-22.3 s).

**What was built.** `-headstd`: as the real plan meets standard-library packages, round by round, it
hands them to the head start, which plans their closure (the standard library depends only on itself,
so that part of the graph is complete long before the plan has finished crawling the module cache)
and compiles them under its own eight tokens. For MinIO all 264 standard packages are compiled this
way and adopted; for cmd/go, 246. Byte-identical output (`tests/sb_zygote.sh` now runs with it).
A bug the Go toolchain corpus found at once: packages under `GOROOT/src/cmd` look standard but belong
to a module with its own vendor directory, which the head start's planner never loads; they are left
to the real plan.

**What it is worth: nothing I can measure.** Interleaved, same flags with and without `-headstd`:

    warm, 4 rounds   without 11.29 11.78 11.26 11.57 s    with 11.66 12.28 10.68 11.72 s
    cold, 3 rounds   without 13.01 12.38 12.93 s          with 11.63 12.50 11.62 s

The cold rounds look like a win until the CPU column is read: the two "slow" runs without it cost 9-10
core-s more (host noise), and corrected for that the difference is 0 +/- 0.2 s. It makes sense: the
build is bound by about 62 core-s of work; this moves perhaps two of them into the plan's window. It
stays as a flag, off in the trace configuration. **Negative result.**

**Lesson for the ladder.** Three scheduling steps in a row (fork-server aside): head start 0.2-0.4 s,
direct output 0.14 s, wider head start 0. The schedule is as good as it needs to be at this work
volume; only removing work moves the trace now. Today's one real mover was the one that removed work
(one assembler pass: -6 core-s).

**Evidence refreshed.** MinIO's own unit tests with the final compiler (one-pass assembler in every
function): 45 packages pass; `cmd` (254 s) has the one known failure, `TestTrackingResponseWriter`,
identical with the baseline off. The standard-library oracle at `-count=3` under GOGC=10 is running
as I write; result in MORNING.md.

**Scope check.** Most ambitious next is unchanged (F4, or whole packages leaving the stock compiler).
Why not now: both start with a design that I would rather hand over with today's numbers than begin
badly in the last stretch of a session. NEXT.md has them in order with the numbers to design against.

## 2026-11-12 14:05 UTC — iteration 20: work removed: 148,000 type descriptors nobody wanted (-7 core-s, -0.9 s)

**Trace.** MinIO cold, four runs of `13-noweakptr`: **11.0, 11.3, 10.9, 10.8 s** (69 core-s in the
first; `12`: 11.5-12.3). kube-apiserver, two: 23.6 (a noisy run, 161 core-s), **20.5 s**. Both pass
smoke; MinIO passes the stress test. Objects 573 -> 526 MB; link 1.2 -> 1.07-1.15 s.

**How it was found.** Iteration 19's lesson was that only removing work moves the trace, so I went
back to the largest block of work I had measured and not explained: runtime type descriptors, 8% of
compile samples, 371 k of them, 225 k distinct anonymous types. The most repeated names in iteration
16's log came in pairs: `func() string` and `*func() string`, 613 each. Why would anything need a
pointer to a func type?

**What it is.** For every type T it describes, the stock compiler also writes a descriptor for `*T`,
so that T's `PtrToThis` field can point at it and `reflect.PointerTo` is a field read. When T is
unnamed and `*T` has no methods the reference is *weak*: the linker keeps `*T` only if something else
uses it, and whatever else uses it writes its own copy (an unnamed type is emitted by every package
that needs it). Counted on MinIO: **148,235 of the 371,329 descriptors** the compilers write are such
pointers (125 k to func types), each built, named, hashed, written to an object, read by the linker,
entered in its symbol table, and almost always dropped by dead-code elimination.

**The change** (fourteenth in `driver/early_export.py`, baseline build only): do not write them;
`PtrToThis` is 0, which reflect and the runtime already treat as "look it up" (`PointerTo` finds an
existing `*T` through the typelinks table, exactly as it does today whenever the weak reference was not
kept). The binary is the same size to the byte (159,101,052): what was removed was never in it.

**Measured.** Four interleaved rounds, old behaviour via `SB_KEEP_WEAKPTR=1`:

    keep   total 12.76 12.00 11.68 11.41 s   compile 76.9 74.1 72.0 69.2 core-s   link 1260 1240 1160 1188 ms
    skip   total 11.94 11.32 10.62 10.08 s   compile 72.9 68.4 64.3 59.1 core-s   link 1153 1132 1097 1119 ms

Every round the same sign: **-6.9 core-s and -0.97 s on average**, link -87 ms. I had estimated 1.5
core-s from the descriptor count; the descriptors drag more with them than their own construction
(name data, object writing, hashing) than the profile's `writeType` line showed.

**Evidence.**
- `tests/sb_ptrto.sh` (new): a program that reaches pointers to ten kinds of unnamed type statically,
  through `PointerTo` and through `New`, and checks identity, assertions and type switches: 39 lines of
  output identical for the stock compiler, the baseline with the descriptors and the baseline without.
  (My first version of the program had its own bug, a `Field` call on an array type; all three builds
  panicked identically, which is the differential test doing its job on the test.)
- Standard-library oracle: 27 of 29 (the two known allocation-count tests). `reflect`,
  `encoding/json`, `encoding/gob`, `encoding/xml`, `text/template`, `fmt` and four more: 9 of 10; the
  tenth, `reflect`, fails `TestMapIterSet` (allocation count) and `TestCallReturnsEmpty` (waits for a
  cleanup; the liveness class), both identically with the descriptors kept.
- MinIO smoke and stress; kube smoke; `tests/sb_early.sh` (gated on `SB_BASELINE`: idle compiler still
  byte-identical to stock); `tests/sb_zygote.sh`. MinIO's unit tests are running with this compiler.

**Behaviour difference for MORNING.md.** The first `reflect.PointerTo(T)` for an unnamed T does a
table lookup instead of a field read (then it is cached). Nothing else.

**Scope check.** Most ambitious next: unchanged (F4; whole packages leaving the stock compiler). What
this iteration says about how to spend the time until then: there may be more work of this kind,
things the stock compiler does for every package so that a linker can throw them away. The descriptor
log has 225 k distinct anonymous types left; the next question is how many survive the link.

## 2026-11-12 14:25 UTC — correction to iteration 20: the descriptor change is worth 1.6 core-s and 0.3 s, not 7 and 0.9

Iteration 20 reported -6.9 core-s and -0.97 s "in every one of four interleaved rounds". That number
is wrong, and the way it is wrong matters more than the number.

**How I found out.** I re-ran the exact per-phase CPU table with and without the change to see which
phase had shrunk. In the two phases the change can touch (be:compilefuncs + be:dumpobj) the difference
was 2.3 and 0.6 core-s; the "saving" in round one was spread over type checking and IR reading as
well, which the change cannot affect.

**What was wrong with the A/B.** Each round ran "keep" first and "skip" second, and over those eight
builds the host was getting steadily quieter (totals 12.76, 11.94, 12.00, 11.32, 11.68, 10.62, 11.41,
10.08 s: a trend through the whole sequence, not a step between configurations). A fixed order credits
the drift to whichever configuration runs second.

**Measured again, order balanced** (keep skip skip keep, three times; 12 builds):

    keep   compile 69.0 71.6 72.1 75.6 72.8 72.6 core-s (mean 72.3)   link 1353 1195 1213 1244 1221 1309 ms (mean 1,256)
    skip   compile 72.0 71.6 63.5 74.4 69.9 73.0 core-s (mean 70.7)   link 1098 1107 1143 1133 1165 1172 ms (mean 1,136)

**-1.6 core-s of compile; -120 ms of link (the two link samples do not overlap at all); about -0.3 s
of wall clock** (11.97 -> 11.69 s without the first run, a 15.5 s outlier). That matches what I had
estimated from the descriptor count before measuring (1.5 core-s) and should have made me suspicious
of a result four times larger. The change stays: it is correct, tested, and the link gain is clean.

**What else this touches.**
- The cold trace for `13-noweakptr` (10.8-11.3 s against `12`'s 11.5-12.3) was taken in the same
  quieter stretch; most of that half second is the host.
- Today's other comparisons, re-read for the same flaw: the one-pass assembler's whole-build rounds
  were also old-then-new, but its claim rests on a single-package min-of-15 in strict alternation
  (8.5% of a package compile, plus 3.5% for the DWARF skip) and on the profile (Flushplist 7.0 -> 4.0 s
  of samples); it stands at about 6 core-s. Direct output is read from two serial rows. The fork-server
  and both head starts were reported as nothing or nearly nothing.
- `tools/trace/abd.sh` now reverses the order on even rounds.
- "Only removing work moves the trace" survives, but with one example, not two: this change removed
  148 k descriptors that turned out to be cheap ones.

## 2026-11-12 14:45 UTC — iteration 21: one honest number for the day (13.2 -> 11.8 s), and the assembler claim re-measured

**The day, order-balanced and cold.** After the correction above I did not trust any of today's
whole-build comparisons, so I measured the sum directly. `tools/trace/day.sh`: last night's
configuration exactly (`08-huge-shard`: exec per compile, multi-pass assembler, per-function DWARF,
speculative descriptors, binary copied from tmpfs; the old behaviours are selectable by environment, and
its 641 MB of objects reproduce) against today's (`13-noweakptr`), cold, in the order old new new old,
three times:

    old   wall 12.87 12.39 13.79 12.29 14.33 13.72 s (mean 13.23)   compile 76.2 core-s   link+write 1,602 ms   objects 641 MB
    new   wall 11.38 11.32 11.90 11.73 12.50 11.99 s (mean 11.80)   compile 68.6 core-s   link+write 1,234 ms   objects 526 MB

**-1.43 s (11%), -7.6 compile core-s, -368 ms of link + write.** Every run passes smoke. Link + write
does not overlap between the two at all; wall clock overlaps in one run of twelve.

**The one-pass assembler, re-measured the same way** (multi one one multi, three times, warm):
compile 74.2 -> 71.3 core-s, with single runs ranging from 60 to 84. So about 3 core-s by whole
builds, 4.3 by the profile (Flushplist 7.0 -> 4.0 s of ~50 s of samples), 8.5% of one package's
compile in strict alternation. Iteration 16 said "6 core-s and 0.7 s"; **"3 to 6 core-s" is what the
evidence supports.** The pieces add up to the day's total without it needing to be 6: assembler +
DWARF 3-6, descriptors 1.6, fork-server 1.6.

**Tooling changed so this does not recur.** `tools/trace/abd.sh` alternates order; `day.sh` exists;
the DWARF skip can be turned off by environment (`SB_NODWARFSYMS=0`) like the other two, so "last
night's compiler" is reproducible from today's binary.

**Scope check.** Most ambitious: unchanged (F4; whole packages leaving the stock compiler). This
iteration was spent on the credibility of the numbers instead, because a report whose headline is
inflated is worse than no report.

## 2026-11-12 15:05 UTC — iteration 22: soak (20 of 20, 6 of 6), a clean review, and where the cheap vein ends

**Trace.** Configuration unchanged (`13-noweakptr`). Spread of the final configuration, cold, each
build smoke-tested (`tools/trace/soak.sh`): MinIO 20 of 20 pass, **min 10.7 s, median 11.5 s, max
12.8 s** (compile 60.0-76.6 core-s: the wall clock follows the host); kube-apiserver 6 of 6 pass,
**min 20.7 s, median 21.0 s, max 23.1 s**.

**Review of the descriptor change** (`reviews/runtime-descriptors-2026-11-12/`): no counterexample.
635 type expressions (512 generated, nested to depth 4), two-package and mixed keep/skip builds, 32
concurrent `PointerTo` readers, targeted reflect tests: identical for the real stock compiler, the
baseline with descriptors and the baseline without; every reader of `PtrToThis` audited. Its one
finding was about my test, not the change: `tests/sb_ptrto.sh` ignored exit statuses and passed on
three identical error outputs. Fixed (each build must exit 0 and print all 39 lines).

**What I looked at and did not build, with numbers for whoever picks this up.**
- Why functions still reach SSA in MinIO: 2,404 of 209,732 (1.15%), costing 4.7% of compile samples.
  By reason: pragma 938 (mostly the runtime; legitimate), dynamic type assertion (generics) 332,
  variable with no home 324, NAME 167, keepalive arguments 160, uint64/float conversions 179,
  range-over-func bodies 56. Covering the five largest non-pragma reasons is worth at most 1.5 core-s.
- Type-name formatting: `types.tconv` is 4.3% of compile samples (the symbol of a type is found by
  formatting its full name and looking the string up, every time). A per-type cache would save about
  1.5 core-s; I did not build it because a stale entry would be a silent type-identity bug and the
  gain is at the edge of what this host can measure.
- Emitted type descriptors after iteration 20: 212 k (125 k are func types, one per method signature of
  every named type). How many survive the link I could not measure cheaply: this Go version folds type
  symbols and typelinks into `.rodata`. Needs a counter in the linker's dead-code pass.
- The linker by profile: symbol-table inserts by name are about 300 ms of the 1.1 s (370 k named
  duplicate-allowed symbols), dead-code flood 230 ms. Dense symbol IDs from the compilers would remove
  the first.

**Scope check.** Most ambitious: unchanged. Everything cheap that I can find is now worth 2% or less
and several of them carry real risk; the session's remaining value is in the report being exact. The
wide standard-library oracle (the 100-odd packages of last night's run, twice, under GOGC=10) is
running with the final compiler; its result goes into MORNING.md.

## 2026-11-12 15:20 UTC — addendum to iteration 22: the wide oracle, and a wrong sentence

The last line of iteration 22 said the wide oracle was "the 100-odd packages of last night's run". It
was not: the list I had extracted from last night's logs held nine packages (those logs keep only
failures). I built a real list instead: every standard-library package with tests except the ones
that need the network, the runtime's own, and the known hangs: **141 packages**, `-short`, GOGC=10,
final compiler, 160,403 functions through the baseline and 4,452 through SSA.

**129 of 141 pass.** The twelve, each run again with the baseline off and the same flags as control:
- nine packages fail only tests that count allocations (`crypto/ed25519`, `crypto/internal/fips140test`,
  `crypto/rand`, `crypto/rsa`, `crypto/sha256`, `crypto/sha512`, `encoding/json/jsontext`, `math/big`,
  `unicode/utf16`): identical failures with the baseline off (no inlining changes what escapes);
- `reflect`: `TestMapIterSet` (allocation count, same in control) and `TestCallReturnsEmpty` (waits
  for a cleanup: the liveness class);
- `crypto/internal/fips140cache` `TestCache`: **new to the list.** It loops on `runtime.GC()` until a
  weak-pointer cache entry is evicted and times out; with the baseline off it passes. Same class as
  `TestTransportGCRequest`: a baseline frame keeps every slot alive until the function returns;
- `internal/copyright`: the files I add to the patched GOROOT have no Go copyright header. Not a
  compiler matter.

No crash, no wrong result. The liveness class now has three named tests in MORNING.md.

## 2026-11-12 15:35 UTC — iteration 23: what "2 s on N cores" means for the pipeline as it stands (a simulation)

MORNING.md asks the user whether "2 s on N cores" is acceptable. I had answered with one division (50
core-s / 2 s = 25 cores). That ignores the graph, so I measured it instead.

**`tools/trace/simcores.py`**: replay one traced cold MinIO build (no head start, so every package has
real times) on N cores. A front end is one thread for as long as it took to release its importers; a
back end is the rest of the package's CPU spread over its `-c`; front ends first, longest chain first;
plan before, link and sync after. The 8-core row is the check: 11.0 s simulated against 11.9 s
measured (the model has no kernel and no memory bandwidth: an optimistic bound).

| cores | whole build, as it stands | if importers were released after 15% of a front end |
|---|---|---|
| 8 | 11.0 s | 10.7 s |
| 12 | 8.7 s | 8.1 s |
| 16 | 8.7 s | 7.0 s |
| 32 | 8.7 s | 5.5 s |
| 64 | 8.7 s | 4.7 s |
| unlimited | 8.7 s | 4.7 s |

**Reading it.**
- As it stands, cores stop helping at about twelve. The floor is 0.9 s of plan + a chain of 44 front
  ends, 5.8 s of serial work (`minio/cmd` 1.0 s, envoy `core/v3` 0.6, `runtime` 0.5, `route/v3` 0.4,
  cloud `storage` 0.4, protobuf `internal/impl` 0.4, ...) + the last back end + 1.1 s of link + sync.
- This is the skeptic's standing objection (package front ends serialize the build), and both of us
  were right about different machines: on 8 cores breaking the chain buys 0.3 s (last night's
  simulation, and the 8-core row here), on 32 it buys 3.2 s.
- Even with the chain broken and unlimited cores the build is 4.7 s: plan 0.9, the largest front end
  and its back end about 2.6, link 1.1, sync 0.1. Each of those is one of the ladder's rungs: the plan
  streamed (F3's schedule), bodies as the unit of work so that no single front end is a second long
  (F3), no separate link (F4).

**So, for the charter's "2 s on N cores, N derived from the floor":** there is no N today. The
pipeline as it stands has a floor of about 8.7 s at any core count; with declarations-first export
about 4.7 s; 2 s needs the three rungs above *and* enough cores for 67 core-s of work (34), or less
work. On this 8-core machine it needs the work itself down to 16 core-s.

**Scope check.** Most ambitious: unchanged. This iteration produced no build change; it replaces a
wrong one-line answer in the report with a measured table, and it changes the order I would climb the
rungs on a bigger machine (declarations-first export first; on this one it stays last).

## 2026-11-12 15:55 UTC — iteration 24: two more small removals tried, both measured at zero; the plateau

**Trace.** Unchanged (`13-noweakptr`; MinIO median 11.5 s of twenty cold builds).

**Type-symbol cache (built, proven identical, measured, removed).** The profile attributes 4.3% of
compile samples to `types.tconv`, most of it under `types.TypeSym` (a type's descriptor symbol is found
by formatting the type's whole name and looking the string up, on every reference). I cached the symbol
per `*Type`. `tests/sb_same.sh` (new: build a program twice, with and without a switch, and compare
every file): cmd/go 664, MinIO 2,624 and kube-apiserver 4,158 export files and objects and all three
binaries byte-identical. Then the measurement: one package, min of 15 in strict alternation, 504 vs
503 ms; twelve whole builds, order-balanced, 64.4 core-s without and 65.6 with. **Nothing.** A cache
that cannot be measured and can go stale silently is not worth having: removed.

**Fewer scheduler threads in the compilers (kill test only).** 1,315 short processes each start an
8-way Go scheduler; `GOMAXPROCS=2` for every compiler, eight builds, order-balanced: 66.6 vs 68.4
core-s, pairs pointing both ways. **Nothing I can resolve.** (A side observation: holding every
compiler to two threads does not lengthen the build, 10.7 vs 11.0 s. On 8 cores the compilers' own
`-c` parallelism is irrelevant; the scheduler across processes does the work.)

**The plateau, stated plainly.** Since iteration 16 I have tried nine changes. Two moved the trace by
more than the host's noise (one assembler pass; the day in total, 13.2 -> 11.8 s). The profile is
flat: no function above 4% that is not the allocator. Predictions from the sampled profile were off by
a factor of four in both directions today (process start: predicted 12, measured 3.6; descriptors:
predicted 1.5, first "measured" 7, really 1.6; this cache: predicted 1.5, measured 0). On this
machine, with this compiler, the build is within about 10% of what scheduling and trimming can reach.
What is left is in NEXT.md and it is not cheap.

**Scope check.** Most ambitious: the same two rungs. For the remainder of this session I am running
longer soaks of the final configuration (the fork-server's one stated risk is a rare hang, and 120
builds is thin evidence for "rare") rather than starting a rung I cannot finish.

## 2026-11-12 16:20 UTC — soak: 60 of 60

Sixty cold MinIO builds of the final configuration in a row, each smoke-tested: 60 pass; wall min
10.5 s, median 11.2 s, max 12.6 s; compile 59.0-75.1 core-s (median 63.7). With the earlier run of
twenty: 80 of 80. No hang, no failed fork, no failed smoke test. A kube-apiserver soak of twenty follows.

## 2026-11-12 16:45 UTC — kube-apiserver: soak 20 of 20, and the day order-balanced (23.0 -> 21.1 s)

- Soak, twenty cold builds, each smoke-tested (/healthz, ConfigMap create/get/delete): 20 pass; wall
  min 19.1 s, median 19.8 s, max 21.7 s.
- `tools/trace/day.sh 2 kube` (last night's configuration against today's, cold, old new new old,
  twice): old 22.79, 24.12, 22.72, 22.33 s (mean 22.99; compile 144.0 core-s; link + write 2,356 ms;
  objects 1,323 MB); new 20.33, 23.19, 21.02, 19.85 s (mean 21.10; 133.7 core-s; 1,802 ms; 1,043 MB).
  **-1.9 s (8%), -10 compile core-s, -0.55 s of link + write.** Same shape as MinIO's -1.4 s (11%):
  the comparison point is not overfitted to.

## 2026-11-12 17:10 UTC — iteration 25 (spike): the core of a link on MinIO's real objects, done the data-parallel way: 0.26 s

**Why.** The skeptic has said four times that the link "floor" in TRACE.md (17 ms) is a copy budget
with no model of symbol resolution or relocation, and the link is the tail of the build. F4 has no
measured number. This spike makes one. One iteration, in `explore/f4link` (270 lines of Go, built
inside the Go tree for `cmd/internal/goobj`; `explore/f4link/mk.sh`).

**What it does.** Reads the `importcfg.link` of a real build and, on all cores: maps every archive and
finds the Go objects; enters every definition that is found by name or by content hash into a sharded
table (lowest object wins, so the result does not depend on thread timing); resolves every symbol slot
of every object to its definition; lays every owned definition out by prefix sums (per object, then
over objects); copies the data into one image and applies every relocation.
**What it does not do:** dead-code elimination, pclntab, the symbol table, ELF, or the semantics of
relocation types (each writes target + addend). It is not a linker and produces no program.

**Measured** (MinIO, 1,312 archives, 1,424 objects, 501 MB; 8 cores; ms):

    map archives, find objects            6-12
    claim names and content hashes      132-189    2.59 M claims, 960 k distinct
    resolve every symbol slot              62-65    2.72 M slots
    layout by prefix sums                      4    213 MB image (everything, no dead code removed)
    copy data, apply relocations           34-41    180 MB, 3.43 M relocations
    total                              about 260    (one core: 1,270)

The stock linker on the same objects: 1,130 ms, of which loading and resolving is 440, dead code 246,
copying and relocating (asmb + dodata) 230, pclntab 114.

**What the numbers say for F4.**
- Layout and relocation, the parts LINKING.md calls Strand-shaped, are 40 ms together. They are not the
  problem and never were.
- **Resolution is the cost: about 200 of the 260 ms, and it is hashing.** The symbol census explains
  it: 419 k symbols are found by package index (a table read), 300 k are defined by name, 121 k are
  references by name, and **1.84 M are content-addressed** (per-function pc tables, stack maps, string
  constants: things the compiler emits as separate little symbols so the linker can deduplicate them by
  a 32-byte hash). Two thirds of everything the linker resolves exists only to be deduplicated.
- So F4's first slice is not "a faster relocation pass". It is: the compilers stop emitting pc tables
  and stack maps as content-addressed symbols (write them into the function's own record, or dedup
  them per package where almost all the duplication is), and hand out dense ids for what is left.
  Then resolution is the 62 ms pass, and a link of this shape is under 0.15 s plus dead code and
  pclntab, which are the next two things to measure the same way.
- The dead-code flood (246 ms in stock) is the one serial algorithm left; as a frontier-parallel
  graph traversal over 3.4 M edges it should be tens of ms, untested.

**Scope check.** This was the spike budget (none used today until now). Most ambitious next is to
make this consumed: emit an image that runs. Not in the hour left; the measurement is what the next
session starts from, and it changes what F4's first step is.

## 2026-11-12 17:40 UTC — iteration 26: a consumed F4 slice tried and reverted (plain pc tables), and how far to trust whole-build CPU numbers

**Trace.** Unchanged (`13-noweakptr`).

**The skeptic's review of the final state** (`reviews/done/20261112T1458-codex-skeptic.md`) agrees with
the replay (44 front ends, 5.8 s; no N today) and names its night-sized choice: a consumed F4 slice,
dense symbol ids and incremental resolution in the real build, aimed at the loader's ~390 ms. The spike
of iteration 25 had just said where resolution time goes in *my* implementation: hashing 1.84 M
content-addressed symbols. So I tried the smallest consumed version of "stop hashing".

**The slice.** Each function's pc-value tables (pcsp, pcfile, pcline, pcdata) are anonymous symbols
marked content-addressable: the compiler SHA-256es each and the linker enters each in a hash table so
that identical tables share storage. About a million of the 1.84 M. The stock loader already has a
path for unnamed plain symbols that involves no hashing. Fifteenth change: in the baseline build, do
not mark them content-addressable. Six lines.

**Result: negative, reverted.**
- It works: tracebacks, `runtime.Callers` line numbers and recovered panics identical on a test
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
