# JOURNAL

Append-only. Each entry: timestamp (absolute, UTC), what I did, what moved in TRACE.md,
scope check ("most ambitious next thing; why not that").

## 2026-11-11 22:30 UTC — iteration 1: rung 0, the profile

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

## 2026-11-11 22:40 UTC — iteration 2: rung 1, the skeleton

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

## 2026-11-11 23:05 UTC — iteration 3: rung 2, the chain

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

## 2026-11-11 23:10 UTC — iteration 4: rung 3, less work (the budgeted optimization iteration)

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
