# MORNING: 2026-11-12 (overwritten at the end of every iteration; final for this session, after iteration 28)

## The trace, before and after

Cold builds, 8 vCPUs, binary passes the target's smoke test. Full tables: TRACE.md.

**The numbers to trust for today's work** (`tools/trace/day.sh`: last night's configuration against
today's, cold, order-balanced; MinIO six builds each, kube-apiserver four each; every build passes smoke):

| | wall, mean | compile core-s | link + write | objects written |
|---|---|---|---|---|
| MinIO, end of the 2026-11-11 run (`08-huge-shard`) | **13.23 s** (12.3-14.3) | 76.2 | 1,602 ms | 641 MB |
| MinIO, now (`13-noweakptr`) | **11.80 s** (11.3-12.5) | 68.6 | 1,234 ms | 526 MB |
| kube-apiserver, end of the 2026-11-11 run | **22.99 s** (22.3-24.1) | 144.0 | 2,356 ms | 1,323 MB |
| kube-apiserver, now | **21.10 s** (19.8-23.2) | 133.7 | 1,802 ms | 1,043 MB |

Quote the wall clock and the link + write column from this table; the compile core-seconds column is
uncertain by several core-s (on this host that number has a sigma of about 5 per build and drifts over
minutes; JOURNAL iteration 26).

Against the stock toolchain:

| | MinIO | kube-apiserver |
|---|---|---|
| stock `go build` (2026-11-11) | 54.2 s, 282 core-s | 86.3 s, 554 core-s |
| stock, re-measured 2026-11-12 | 50.0 s, 257 core-s | 77.1 s, 511 core-s |
| **fair stock** (`GOGC=off`, `-dwarf=false -l`, `-s -w`) | **32.5 s**, 169 core-s | **51.2 s**, 329 core-s |
| now | **11.8 s** (above); median of 130 cold builds 11.2 s | **21.1 s** (above); medians of 20 and 30 cold builds 19.8 and 21.3 s |

Soaks of the final configuration, cold, each build smoke-tested (`tools/trace/soak.sh`): MinIO 150 of 150
pass (runs of 60 and 70: wall min 10.5 and 10.6 s, **median 11.2 s** in both, max 12.6 and 13.1 s); kube-apiserver 56 of 56 pass (a
run of 20: median 19.8 s; a run of 30 an hour later: min 19.8 s, **median 21.3 s**, max 23.8 s).

Against the fair row the technique is worth 32.5 -> 11.8 s on MinIO and 51.2 -> 21.1 s on
kube-apiserver; the rest of the distance from 54 s is things turned off. The host drifts (the same
build cost 60 to 84 compile core-seconds today), so single runs and the rung table in TRACE.md are
indicative only.

**The materialization tax** (MinIO; kube is in TRACE.md):

| | stock | fair stock | this build |
|---|---|---|---|
| tool processes started | 1,483 | 1,483 | 172 + 1,315 forked from one warm compiler |
| page faults, whole machine | 11.9 M | 11.0 M | 1.3 M |
| intermediates written | 1,899 MB (disk, twice) | 1,383 MB | 618 MB (tmpfs, once) |
| intermediates read back | 2,731 MB | 1,996 MB | 1,143 MB |

**Where the compile row's CPU goes, measured exactly** (each compiler logs its own rusage at every phase
boundary; one warm MinIO build after the one-pass assembler, 74 core-s in the row):

| | core-s | |
|---|---|---|
| walk, baseline code generation, assembler | 28.7 | compiling |
| build the compiler's IR by reading the package's export IR back | 11.5 | moving data |
| type check | 10.1 | compiling |
| write the package as export IR | 5.7 | moving data |
| wrappers, escape analysis | 4.6 | compiling |
| object files + export data for importers | 6.7 | moving data |
| parse | 2.3 | compiling |
| exec/fork, runtime start, package init, flags | 2.9 | process setup |
| exit, assembler for .s files, joining shards | 1.7 | |

## What this says (the one thing to read)

Fusing the stock compiler's work into one address space can remove about 27 of 74 core-seconds (setup
3, object and export writing 7, the export-IR round trip 17). What is left, about 47 core-s, is compiling:
5.9 s on 8 cores. **Two seconds on this machine needs the compiling itself to get about three times
cheaper as well** (our own lowering, and an emitter that writes bytes instead of going through the Go
assembler's instruction lists). Fusion is necessary and not sufficient. The ladder's F2-F4 should be
read as "a cheaper compiler that is also fused", not "the stock compiler, fused".

A correction to last night's journal: I had attributed 12 core-s to process start and 20 to the
kernel from a sampled profile that missed a third of the CPU. Measured, process start is 3.6 core-s and
the front end is 35% of the row, not 20%.

## The pattern in today's results

Scheduling and plumbing: fork-server 1.6 core-s, head start 0-0.4 s, direct output 0.14 s, wider head
start 0, streaming link dead. Removing work: one assembler pass and no per-function DWARF, 3-6 core-s;
speculative type descriptors, 1.6 core-s and 0.12 s of link. The schedule is as good as it needs to be
at this work volume; only removing work moves the trace, and the cheap removals are getting small. The
work that is left is the stock compiler's own (type check 10, IR round trip 17, walk + generate +
assemble about 27 core-s) and the stock linker's (1.1 s serial).

**Two numbers I reported too high, and corrected.** The type-descriptor change first went into the
journal and a commit message (`b040190`) as -7 core-s and -0.9 s; the one-pass assembler as -6 core-s
and -0.7 s. Both came from A/B runs in a fixed order (old first) on a host that was drifting. Checking
which compiler phase had shrunk exposed the first; order-balanced re-runs give 1.6 core-s for the
descriptors and about 3 (3-6 across three kinds of evidence) for the assembler. The day's total above
was measured order-balanced for that reason, and the A/B script now alternates order. Even so, a later
experiment showed order balancing with four to six builds per arm is not enough for *compile
core-seconds* on this host (a change with no mechanism "saved" 5.9 core-s in all four pairs and nothing
in a single-package measurement). The claims in this report rest on mechanisms with counts,
single-package alternation, the quiet serial rows, and wall clock over many builds, in that order.

## Rungs climbed today

- **F0** (done): the fair stock row and the tax in TRACE.md, regenerated by `tools/trace/mktrace.py`;
  the exact CPU table (`tools/trace/cpuphases.sh`).
- **F1** (done; nearly a negative result): a fork-server for the compiler. `runtime.SBFork` (fork
  without exec in a Go process, 60 lines in the patched runtime) and `sb -zygote`. Output is
  byte-identical to one exec per compile (`tests/sb_zygote.sh`; the reviewer confirmed it over MinIO's
  2,624 files). Worth **1.6 core-s of ~71 and no measurable wall clock**. The persistent-worker variant
  has the same ceiling and needs every compiler global reset; not built.
- **Critical-path optimization** (the one budgeted iteration): the Go assembler re-assembled every
  function 3.3 times (by bytes) to size branches and pad jumps. Baseline builds now assemble once, and
  skip the per-function DWARF the stock compiler builds even at `-dwarf=false`. **3-6 core-s** (see the
  correction above), objects 641 -> 573 MB.
- **The first edge of the DAG, streamed** (`-headstart`): the runtime and the 28 packages under it are
  planned and compiled while the plan of the whole build is still being made, then adopted by the
  scheduler. Byte-identical output. `runtime` is ready when the plan ends instead of 0.45 s later.
  Worth **0 to 0.4 s**: today's host noise hides it in cold wall clock (interleaved cold means 11.91
  vs 11.96 s; warm 12.15 vs 11.72 s).
- **Work removed: speculative pointer-type descriptors.** For every type it describes the stock
  compiler also describes the pointer to it, weakly referenced; for unnamed types the linker drops
  nearly all of them. In MinIO that is 148 k of 371 k descriptors. The baseline build no longer writes
  them: **-1.6 core-s of compile, -120 ms of link, about -0.3 s** (order-balanced, 12 builds), objects
  573 -> 526 MB, binary the same size to the byte. New differential test `tests/sb_ptrto.sh`.
- **Direct output** (`-directout`): the linker writes the binary in place instead of to tmpfs followed
  by a copy. Link + write 1,459 -> 1,316 ms. Same bytes.

- **F4 spike, measured, not consumed** (`explore/f4link`): the core of a link on MinIO's real objects,
  on 8 cores: resolve all 2.7 M symbols with a lock-free table (about 110 ms, memory latency), reachability
  as a frontier-parallel flood (23-42 ms; stock dead-code pass 246 ms), layout of 213 MB by prefix sums
  (4 ms), copy and 3.4 M relocations (35 ms): **about 0.20 s** for what takes the stock linker about
  0.9 s of its 1.1 s. Missing: pclntab, the Go rule for keeping methods, ELF, and being right: checked
  against a stock link, the flood reaches all but 46 of the 122,860 functions the stock linker keeps
  (assembly routines reached through a table, and a few names the two lists spell differently) and
  keeps 10,919 more (methods the Go method rule would drop). Reading: F4 could take the link from 1.1 s to perhaps 0.3 s; it is
  the only large item left on this machine that is not "replace the compiler", and getting within
  0.04% of the stock live set from above in an afternoon suggests weeks, not months. Two of my own errors in
  this spike were caught by checking against a slower reference (a hash key that merged +x with -x; a
  wrong root set), which is the argument for building F4 with the stock linker as a permanent oracle.
- **A consumed F4 slice, tried and reverted**: per-function pc tables as plain symbols instead of
  content-addressed ones (about a million of the 1.84 M hashed symbols the linker resolves). It works,
  and the link gets 76 ms *slower* and the binary 2.3% bigger: the sharing those hashes buy is worth
  more than the hashing costs. Negative result.

## Ideas tried and killed (all by measurement; IDEAS.md and JOURNAL iteration 16)

- Process start as a large cost: 3.6 core-s, not 12.
- A warm process holding imported packages (two divergence lenses): import decoding is 1-3% of compile
  CPU; the cost is the package's own IR round trip.
- A never-free bump allocator: Go's own (`GODEBUG=sbrk=1`) gains nothing and deadlocks.
- Emitting each shared type descriptor once across the build: 28.6% are duplicates, worth 1.6 core-s.
- Widening the head start to the whole standard library (`-headstd`: 264 packages compiled during the
  plan, byte-identical): 0 +/- 0.2 s. The build is bound by work, not by the schedule.
- Starting the linker early and streaming objects into it: loading objects is 55 ms of the link's
  1,220; the rest needs every object. Also found: the stock linker's output depends on load order.
- "Compile each function shape once" (20.8% repeats, an upper bound); reachability before code
  generation (iteration 9); interface-first export (simulated: 9.9 s at best against 10.3 s).

## Reviews worth your attention

- The correctness reviewer (sol) earned its keep twice today: a lost reply from the fork-server was
  read as "compile succeeded", and a baseline object linked without `-w` crashed the stock linker. Both
  reproduced from its cases, both fixed with regression tests (`reviews/forkserver-2026-11-12/`,
  `reviews/instruction-encoding-2026-11-12/`). Its 10,800-result and 25,515-call differential runs found
  no fault in the one-pass assembler.
- The skeptic has said four times that the compile and link "floors" in TRACE.md are copy budgets, not
  floors. Accepted each time; they are still in the table with that label. If you want them out or
  replaced by a measured semantic floor, say so.
- The skeptic's standing proposal is a consumed F2/F3 skeleton (own lowering for a function subset).
  `05-sbc` was that and lost 2 s, because a taken function still pays its package's stock front end.
  I think the first slice that pays is whole packages leaving the stock compiler; that needs export
  data written without it. Not started.

## Every behaviour difference of the dev build

- Unoptimized code from the baseline generator; no inlining (`-l`); no DWARF; `-s -w` (no symbol table).
- Binary 160 MB against 100 MB for fair stock (147 MB for plain stock).
- Longer object liveness: a baseline frame keeps every slot alive until the function returns.
  Known tests that wait for a finalizer, cleanup or weak pointer and so never finish or fail: `net/http`
  `TestTransportGCRequest`; `crypto/internal/fips140cache` `TestCache` (found today); `reflect`
  `TestCallReturnsEmpty`; the sync/reflect promptness tests listed in JOURNAL iteration 12.
- Standard-library tests with the final compiler (141 packages, GOGC=10): 129 pass. Of the other twelve,
  ten fail only tests that count allocations, identically with the baseline off; one is the weak-pointer
  test above; one checks copyright headers of files I added to the patched GOROOT.
- MinIO's own unit tests (run twice today with the final compiler, after the assembler and descriptor
  changes): 45 packages pass; `cmd` has one failure, `TestTrackingResponseWriter`, identical with the
  baseline off.
- No module build info in the binary (`go version -m` shows no dependency list).
- Huge pages: the trace configuration reserves 3,072 2 MB pages (MinIO) or 5,120 (kube) with
  `sudo sysctl vm.nr_hugepages` for the duration of the build and puts the old value (0) back.
- The compilers run with the garbage collector off (`GOGC=off`); the fair stock row does too.
- New today: with `-zygote`, compiles are forked from one warm compiler process by a runtime function
  that is not part of Go. Output is byte-identical; the risk is a hang or crash in a child, not a wrong
  binary. None seen in about 290 MinIO builds, 80 kube builds, 6,000 test forks and the reviewer's 648.
- New today: baseline code has no jump padding to 32-byte boundaries (the Intel JCC erratum mitigation:
  speed on some Intel cores, not correctness) and forward branches further than 12 instructions use the
  long encoding. No per-function DWARF when the driver's link has `-w` (the reviewer showed that a link
  without `-w` panicked; the compiler now leaves DWARF out only when the driver says the link strips it).
- The first `reflect.PointerTo(T)` for an unnamed type T does a table lookup instead of a field read
  (the compiler no longer emits `*T` descriptors speculatively); results are the same.
- Our wall clock includes syncing the binary to disk; the stock rows do not sync theirs.

## Decisions that are yours

1. **Is "2 s on N cores" acceptable, or must it be this VM?** I simulated today's measured build on more
   cores (`tools/trace/simcores.py`; the 8-core row lands at 11.0 s against 11.9 s measured):

   | cores | as it stands | if importers were released after 15% of a front end |
   |---|---|---|
   | 8 | 11.0 s | 10.7 s |
   | 16 | 8.7 s | 7.0 s |
   | 32 | 8.7 s | 5.5 s |
   | unlimited | 8.7 s | 4.7 s |

   As it stands, cores stop helping at about twelve: the floor is the plan, a chain of 44 package front
   ends (5.8 s of serial work) and the link. So there is no N today. A bigger machine makes the
   skeptic's declarations-first export the first thing to build (worth 3 s at 32 cores, 0.3 s here), and
   even then 2 s needs the plan streamed, no front end a second long (bodies as the unit of work) and no
   separate link: the ladder's F3 and F4. On this 8-core VM it needs the work itself down from 67 to 16
   core-s, which is a different compiler: months, not nights. Which machine is the target decides the
   order of the rungs.
2. **Keep the fork-server on by default?** It is correct as far as I and the reviewer can test and buys
   almost nothing today. I left it on in the trace configuration and off by default in the driver.
   Same question for `-headstart`.
3. **The host.** The same build cost between 61 and 84 core-seconds within an hour today, and it drifts
   over minutes. Differences under about half a second cannot be seen in single cold runs here, and an
   A/B in a fixed order can invent one (see above). A quieter machine (or pinned cores) would make the
   trace sharper.
4. **The floors in TRACE.md** (see the reviews section above). There is now a measured number for the
   link's core (0.20 s with reachability, the F4 spike) that could replace the 17 ms copy budget; I have not changed the
   table without your say.
