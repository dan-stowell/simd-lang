# SHAPES: what made the build fast

A distillation of JOURNAL.md, IDEAS.md, TRACE.md (2026-11-11 to 11-16) and bottom-up's FLOOR.md (2026-11-13).
The workload is building MinIO (1,312 Go packages, 75 MB of source) from cold, on 8 cores. Stock `go build`
took 54.2 s; fc builds it in 2.2-2.4 s today. Dates point into JOURNAL.md.

## The ideas at a glance

What held up under measurement, roughly in the order it was found:

| bottleneck | win | idea | evidence |
|---|---|---|---|
| Each package waits for everything it imports to finish compiling: a line of waiting 30 packages deep, 38.5 s of the 54 s | that line: 38.5 s -> 12 s, then to 0.3 s in fc | **Break the chain.** An importer needs only the declarations it imports, not their machine code; produce those first | 11-11 iteration 3; 11-13 |
| Work nobody uses: debug info, inlining, the compiler's own garbage collector | 41 s -> 21 s | **Turn it off.** Stock with the same flags is 32.5 s, so this part isn't a technique | iteration 4 |
| The optimizer: ~35% of compile CPU in SSA passes | 22 s -> 16.4 s; 144 -> 87 core-s | **A cheap code generator.** Walk the typed tree straight to machine code; the optimizer only for the 3% it can't do | iterations 11-13 |
| 1,315 separate compiler processes handing each other files; a package's own typed tree written out and read back | 11.2 s -> 6.8 s | **One process, nothing written in between.** Packages refer to each other by pointer | 11-12 19:52 |
| The one biggest package started late: work slots went out in arrival order | 6.8 s -> 3.6-4.1 s | **Biggest first.** Give the next slot to the largest waiting package | 11-13 22:53 |
| Page faults: 7.8 million first touches of fresh memory, 20 of 87 compile core-s | kernel 21 -> 11 core-s; fc cold 10.5 s -> 6.6 s | **Huge pages.** 2 MB pages instead of 4 KB: 512 times fewer faults | iteration 14; 11-13 22:16 |
| A quarter of all CPU in hash-map code: answers stored in tables keyed by object, looked up again later | user CPU 16.1 -> 14.7 s | **Store the answer on the thing.** A field on the syntax node or the type, not a map entry | 11-14 01:39; 11-16 |
| The allocator's bookkeeping, for memory that's never freed anyway | cold 2.72 s -> 2.38 s | **Bump allocation.** Allocating is adding to a pointer | 11-15 17:17 |
| The linker throws away 21% of the code that was compiled | 158 k -> 137 k functions; warm 2.45-2.64 -> 2.08-2.31 s | **Compile only what's reached.** Start at main and follow calls as code is generated | 11-14 01:15; 11-15 18:07 |
| Shared pieces (generic instances, type descriptors) written by whichever thread got there first: a lock, and different bytes every build | same bytes from every build, for 0.19 s | **Owner chosen by name.** Hash the symbol's name to pick which compiler writes it | 11-15 22:10; 11-16 01:10 |
| The linker matching 2.7 M references to definitions in one big table: every insert a cache miss | link 1,750 ms (stock) -> 303 (fclink) -> 170 (hlink) | **Partition, then join.** Sort keys into buckets first, then match each bucket in a table that fits in cache | 11-12 F4 spike; bottom-up |
| Reading 7,766 small files cold: the disk is limited by requests, not bytes | ~10,000 -> ~4,300 disk requests | **Fewer, bigger reads.** Read each module's zip instead of its extracted files | 11-15 18:33 |
| The assembler re-assembling a function when it guessed a jump's size wrong: 2.7 passes per function | compile 67 -> 61 core-s | **Assemble once.** Size far jumps long from the start | iteration 16 |

And what was tried and killed by its own measurement:

| idea | why it looked good | why it died |
|---|---|---|
| Better scheduling (start the standard library early, etc.) | cores sat idle at the start | three tries bought 0.2-0.4 s, 0.14 s and 0: once the build is CPU-bound, "only removing work moves the trace" |
| Skip dead functions by name before compiling | the linker drops 21% of code | a name-based pass finds only half of that; following real references (above) found the rest |
| Compile duplicate bodies once | wrappers and generic copies repeat | only 8% of code; 20.8% of body shapes repeat, under the 25% kill line |
| Split every big package across processes | one package ran alone at the end | each piece repeats the type check: 108 core-s against 87. "Overlap bought by repeating work is paid in the same currency" |
| Start linking while compiles finish | the link ran last, alone | 390 of its 440 ms needs every object, and the output depended on arrival order |
| A cache in front of a slow map | name lookups were hot | slower both times: the cost was the maps themselves |
| Precise liveness for every stack slot | fewer stale pointers | code 80.7 -> 107.4 MB; every byte is paid three times (generate, assemble, link) |

## Each idea, concretely

### Break the chain
**Before.** Go compiles a package only after everything it imports is compiled. MinIO's imports run about 30
deep (`minio/cmd` -> `minio/internal/...` -> `crypto/...` -> `runtime`), and each step waited for the previous
compile to finish completely: type check, machine code, object file. That line took 38.5 s of 54, with cores
idle for lack of ready work.
**Observation.** An importer needs only the imported package's declarations (type names, function
signatures, constants), and those are known early.
**Fix 1.** Publish the declarations as soon as type checking ends and let importers start; machine code keeps
generating in the background. The line: 38.5 -> 12 s.
**Fix 2 (fc).** Read only the declarations of every package in the program first, skipping all function
bodies: 0.3 s of waiting for all of MinIO. After that every function body is independent and can be checked and
compiled on any core, in any order.
**Shape.** A small pass that computes what everyone depends on, then a wide pile of independent work.

### Turn it off
**Before.** By default the compiler writes debug info, inlines small functions, and runs its own garbage
collector. **Fix.** `-dwarf=false`, `-l`, collector off: 41.4 -> 33.0 -> 26.2 -> 21.3 s. **Honest accounting.**
Stock with the same flags takes 32.5 s, so every later claim is measured against that "fair stock" number,
not 54 s.

### A cheap code generator
**Before.** Every function went through SSA form, dozens of optimization passes, and a register allocator.
**Observation.** About 45 kinds of operation covered 35-40% of the back end's time, and an unoptimized MinIO still
passes its smoke test. **Fix.** Walk the type-checked syntax tree and emit instructions directly. Every local lives in
a stack slot. Anything the generator doesn't understand (1-3% of functions: pragmas, some type assertions) falls back to the
optimizer. 22 -> 16.4 s, and kube-apiserver 43.9 -> 28.5 s. **The cost.** Binaries grew (153 vs 100 MB), and
the built program is slower. This is where optimization was dropped: the charter times the build, not the
program.

### One process, nothing written in between
**Before.** 1,315 compiler processes. Each writes a description of its package to disk; each importer reads and
decodes it. Even a package's own typed tree was written out and read back (5.0 + 10.1 core-s of 72). **Fix (fc).**
One process holds the whole program; packages refer to each other's types by pointer. 11.2 -> 6.8 s.

### Biggest first
**Before.** fc handed its work slots out in the order packages became ready. `minio/cmd`, the largest (5 MB),
became ready late, waited 1.1 s for a slot, then ran nearly alone while 7 cores sat idle. **Fix.** The next slot
goes to the biggest waiting package (the classic longest-job-first rule). Cold 6.8 -> 3.6-4.1 s. **Lesson.** It
was found by logging when each package started, not totals: the slowdown was one package waiting its turn.

### Huge pages
**Before.** With its collector off, a compiler touches fresh memory constantly; each first touch of a 4 KB page
is a page fault costing ~3.6 µs on this VM. 7.8 million of them were 20 of 87 compile core-s. **Fix.** Take
memory from the kernel's pool of 2 MB pages. Kernel time 21 -> 11 core-s; in fc, cold 10.5 -> 6.6 s. (It needs
`vm.nr_hugepages` set on the machine, which counts as configuration, not a cache.)

### Store the answer on the thing
**Before.** Go's type checker records the type of every expression in a hash map from expression to type: 9.6
million entries for MinIO. The code generator then looks up each expression it visits. Every lookup hashes a
pointer and jumps to a random spot in a large table, which is usually a cache miss. Profiles showed a quarter of
all CPU in hash-map code. **Fix.** Add a slot to each syntax node. The checker writes the answer there, and the
generator reads it from the node it's already holding. User CPU 16.1 -> 14.7 s, checked against the maps for all
10 million nodes with zero differences. The same move, repeated: a type's facts (link name, size, alignment) on
the type object; "who writes this descriptor" as a word on the symbol instead of a table under a lock. **What
failed.** A cache in front of those maps was slower: the cost was the maps, so adding one made it worse.

### Bump allocation
**Before.** Go's allocator sorts objects by size class and tracks them for a collector that's turned off. **Fix.**
Each thread gets a 4 MB chunk; allocating is "add n to a pointer, check the end." Nothing is freed; the process
exits. Cold 2.72 -> 2.38 s. **What hurt.** A runtime lock packs a thread's address into a word and assumed the old
allocator's alignment, so the first version hung at start.

### Compile only what's reached
**Before.** Every function of every package was compiled; the linker then threw away 21% of the code. **Fix.**
Start from `main`, the `init` functions and the runtime's entry points. When generating code for a call to `f`,
ask `f`'s package to compile `f`. It's a reachability walk done during code generation. 158 k -> 137 k functions.
**What hurt.** The first version made each requested function its own job, about 130,000 jobs with two
hand-offs each, and was *slower* than compiling everything. The fix was a thread that stays in one package's queue
while it has work, so its data stays in cache. Then warm builds went 2.45-2.64 -> 2.08-2.31 s.

### Owner chosen by name
**Before.** Some pieces are needed by many packages: generic instantiations, wrappers, type descriptors. Whichever
thread asked first wrote them, under one global lock (5 of 8 threads were seen queued on it). The result depended
on timing, so two builds of the same source gave different bytes. One real miscompile hid because it
passed or failed depending on where a descriptor landed. **Fix.** Hash the symbol's name to pick one of 64 "shard"
compilers to own it, and sort everything else by name or content hash instead of arrival. The same bytes come from
every build (buildifier, caddy, MinIO, three builds each). It costs 0.19 s, kept on purpose: a build that differs
run to run can't be cached or compared.

### Partition, then join
**Before.** Every object file lists the symbols it defines and the names it uses; linking matches the two, 2.7
million slots for MinIO. The stock linker inserts them one at a time into a big hash table. A table of 128 MB means
each insert lands somewhere random in memory: about 270 ns, several DRAM misses. **Fix (bottom-up's hlink).** First
split all keys into buckets by a few bits of their hash, which is fast because it writes sequentially. Then match
each bucket in a small table that fits in cache. This is how analytical databases do joins. Whole link: 1,750 ms
(stock) -> 303 (fclink) -> 170 (hlink); the hardware floor is about 10 ms.

### Fewer, bigger reads
**Before.** Reading 7,766 source files cold took about 10,000 disk requests. **Observation.** The disk does 20-50
thousand requests a second regardless of how many readers ask, but 3 GB/s sequentially: the count is the cost, not
the bytes. **Fix.** Read each module's zip from the download cache, one read per package. About 4,300 requests.

### Assemble once
**Before.** The assembler guesses every jump is short and re-assembles the function when one isn't: 2.7 passes per
function on average. **Fix.** A jump to more than 12 instructions ahead starts long. One pass for everything, code
0.7% bigger, compile 67 -> 61 core-s.

## What the hardware says the work is

- **Text is under 1%.** Lexing and parsing all of MinIO is 0.3 core-s. Fast byte-at-a-time processing of source
  text doesn't move a build.
- **The work is symbols, types and references.** Type descriptors were 15% of the build (524,497 requests to write
  60,597 descriptors). Two thirds of the linker's symbols exist only to be deduplicated.
- **It's bound by memory, not instructions or locks.** The same build takes 16.4 core-s on 1 core and 22.8 on 8:
  each core gets slower as more of them share the memory system. Locks, the collector and hyperthreading were each
  ruled out. The build allocates 88 bytes for every byte of source, and 13.9 kB of scratch to produce one function's
  514 bytes of code.
- **The link is at about 5% of its floor** (hlink 170-250 ms against ~10 ms computed from memory bandwidth and
  latency). The whole build is about 2.5x TRACE's floor model of 0.84 s, but that model guessed its per-byte costs
  for type checking and code generation, so treat that one loosely.

## What's still in the way

1. **Code generation is the biggest remaining cost** (about a third of CPU), and it's still one function at a time,
   walking a tree. A batch alternative was measured and never built: the 4,096 most common expression shapes (like
   "add an int64 local to a constant") cover 96.7% of MinIO's 1.4 million expressions. Group expressions by shape,
   emit each shape from a template, patch in the operands. That's bottom-up's campaign 2. Whole statements are too
   varied for it (80.9%).
2. **Type checking a function body jumps around memory.** Bodies are independent of each other, but every name in
   a body is a lookup into declarations scattered across the program.
3. **Most of the hand work at the level of locks and stack frames was keeping Go's existing runtime happy:** the
   garbage collector's stack maps, functions that must not grow the stack, a vfork child's frame, a lock that packs
   an address. None of it was about speed. A toolchain with its own runtime wouldn't have those rules to honor.

## What this suggests for a language

- **Things have dense numbers, and their attributes live in arrays indexed by them.** "Store the answer on the
  thing" was the fix that kept recurring; a table keyed by an object was the thing the profile kept finding.
- **A handful of operations cover the wins:** do this to every item (map); running totals, for laying things out
  (scan); sort into buckets (partition); match two lists by key (join); follow references from a starting set,
  in batches (reachability); combine everything with the same key, the same way every time (reduce by key).
- **The last one replaces the locks.** Nearly every lock in fc was answering "who owns this symbol?" by hand. As a
  built-in that picks deterministically (say, lowest number wins), it needs no lock, and identical builds come free.
- **Memory belongs to a phase and is dropped all at once.** The collector off and bump allocation were both wins.
- **The cost model should count memory touched,** next to total work and the longest chain of waiting. Cores stopped
  helping here because of memory, not because of the chain.

## Pictures worth drawing (all from data already in the repo)

- The table above as a waterfall: each change's seconds and core-seconds, kept and killed.
- Core-seconds against core count (16.4 -> 22.8): the memory wall.
- Cores busy over time, before and after declarations-first (TRACE has the timelines).
- The critical-path simulation (11-12 15:35): wall time against cores, as-is and declarations-first.
- The link against its floor, phase by phase (bottom-up FLOOR.md).
