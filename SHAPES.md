# SHAPES: what the runs found, as problem shapes

A distillation of JOURNAL.md, IDEAS.md, TRACE.md (simd-lang, 2026-11-11 to 11-16) and bottom-up's FLOOR.md and
IDEAS.md (2026-11-13). It isn't about the compiler. It's about which reshapings made a real build fast, which
few primitives they turned out to be, and what that says about a language that would make them the only way to
write the code. Numbers are MinIO (1,312 packages, 75 MB of Go) on 8 cores unless it says otherwise; dates point
into JOURNAL.md.

## The arc in one table

| step | MinIO cold | core-s | what changed |
|---|---|---|---|
| stock `go build` | 54.2 s | 282 | |
| driver: own plan + critical-path scheduling | 47.6 | 265 | "no phase got faster" |
| export data before back end, front ends first | 39.9 | 273 | chain 38.5 -> 12 s |
| DWARF, inlining, collector off ("fair stock" is 32.5 s) | 22.0 | 144 | turning things off |
| baseline code generator, no SSA, 97% of functions | 16.4 | 87 | largest single technique |
| huge pages, one giant package sharded, denser code | 12.5 | 70 | 20 of 87 core-s had been page faults |
| one-pass assembler, direct output, fork server | 11.2 | 62 | plateau: CPU-bound |
| fc: one process, whole program, no export data | 6.8 | 26 | serialization boundary gone |
| declarations first; slots to the biggest package | 3.9 | 22 | the tail was one package waiting its turn |
| checker results in the syntax nodes, not maps | (warm 2.9 -> 2.6-2.7) | 16.1 -> 14.7 user | a quarter of the CPU was hash maps |
| bump allocator; functions by demand | 2.2-2.4 | 13-15 | today |

The agent's own summary of the first night: "54 -> 32.5 is turning things off; 32.5 -> 12.5 is technique." Of
the fc era: the wins were all about touching less memory.

## What the hardware says the work is

- **Text processing is under 1%.** Lexing and parsing all 75 MB of MinIO is 0.3-0.34 core-s (TRACE; JOURNAL
  iteration 6). Byte-at-a-time SIMD work on source text is almost irrelevant to build time.
- **The work is symbols, types and references: a graph, keyed by name.** Type descriptors were 15% of the build
  (524,497 calls to write 60,597 descriptors). 1.84 M of the linker's 2.72 M symbol slots exist only to be deduplicated.
  The link is a join plus a reachability walk.
- **It is bound by memory latency, not instructions.** Core-seconds grow with cores (16.4 / 17.7 / 18.3 / 22.8
  at 1/2/4/8; 11-14 00:55), and SMT, locks and GC were ruled out. One claim into a lock-free 128 MB table is about
  270 ns, i.e. a few DRAM misses (bottom-up: a dependent miss is 158 ns on 4 KiB pages). The build allocated 88 bytes
  of heap per byte of source and 13.9 kB per compiled function for 514 bytes of code (11-15 17:36).
- **Process and file boundaries were a tax of about a fifth.** Export IR written and read back was 15 core-s of 72.
  Page faults were 20 of 87 compile core-s. Cold reads cost requests, not bytes (20-50 k requests/s against 3 GB/s
  sequential).
- **Distance to the floor.** The link is at about 5% of its hardware floor (hlink 170-250 ms against 9.7 ms;
  bottom-up FLOOR.md). The whole build is at about 2.5x TRACE's rough floor model of 0.84 s, but that model
  guessed its per-byte costs for type checking and code generation, so treat that ratio as soft.

## The primitives the wins turned out to be

Each row is a reshaping that held up under measurement, what it really was, and what the agent fought by hand.

| primitive | where it showed up | evidence | what hurt |
|---|---|---|---|
| **break the chain** (a small serial pass, then a wide one) | export before back end; declarations of the whole program first, bodies after (fc) | chain 38.5 -> 12 s; front end done at 1.3-2.0 s with flat cores-busy | two passes made two objects for one type: every identity comparison needed a fix, until go/types was patched to keep one |
| **map over independent units** | function bodies checked and compiled per function; sharded giant packages | bodies are the parallel bulk | sharding by repeating the type check lost: "overlap bought by repeating work is paid in the same currency" |
| **flood** (reachability as a frontier) | plan as BFS rounds over imports; linker dead code; functions by demand | flood 52 ms for 424 k symbols in 117 levels; demand: 158 k -> 137 k functions | one job per requested function (130,000 jobs, two hand-offs each) made it *slower* until a thread stayed in one package's queue: the frontier has to be batched by locality |
| **join** | linker resolve by name and content hash | stock 440 ms -> spike 160 ms -> hlink partitioned joins | a global lock-free table is latency-bound; partition first (radix), then join in cache-sized pieces (bottom-up) |
| **scan** | layout by prefix sum; pc tables concatenated | layout 4 ms in the spike | none: the easy one |
| **reduce by key, deterministic** ("lowest id wins") | duplicate symbols; shared descriptors and wrappers owned by a shard chosen by hash of name | same bytes from every build (buildifier, caddy, MinIO) | arrival order leaked everywhere: layout, numbering, file tables; a miscompile hid for days because an address varied |
| **ownership instead of shared maps** | rtMu -> claim; claim as a word on the symbol; facts on the type object; checker results in node slots | a quarter of CPU was hash-map code; slots: user 16.1 -> 14.7 s, 0 differences in 10 M nodes | `concurrent map writes`, a race on a flag word the flood ORs, 5 of 8 threads queued on one mutex |
| **arena / bump allocation** | collector off; bump allocator on huge pages; per-thread scratch | 2.72 -> 2.38 s cold; kernel 21 -> 11 core-s with huge pages | a runtime mutex packs an M's address and assumed allocator alignment |
| **batching** | zips instead of 7,766 files; 128 kB writes; one assembler pass | requests ~10,000 -> ~4,300; 98,000 write calls gone | none |

**The one pattern that ran through the whole fc era: replace a lookup keyed by an object with a field on the
object (or a dense ID into a column).** The checker's maps went into slots on the nodes. The type facts and the
descriptor claim moved onto the objects. Per-package Info became per-function. A "cache" of names was tried and
was slower, because the cost was the keyed maps themselves (11-16 01:25). This is data-oriented design arrived at
by profile: entities with dense IDs, attributes as columns, no pointer-keyed hash maps.

## What did not survive, and why

- **Scheduling, once the build was CPU-bound.** Three scheduling steps in a row bought 0.2-0.4 s, 0.14 s and 0:
  "only removing work moves the trace" (11-12 13:50). Scheduling mattered exactly once: the critical path (front
  ends first; slots to the biggest waiting package).
- **Not compiling what the linker drops, by name.** Only 21% of code is dead, and a name-based pass finds half of
  it. By demand, following real references, it saves 16% of code: kept, but modest.
- **Deduplicating bodies or shapes.** 20.8% of bodies have a shape seen twice; 8.1% of code is duplicate-allowed
  copies. Under every kill line.
- **Streaming the link.** Resolution needs every object (390 of 440 ms), and output depended on arrival order.
- **Caches in front of maps.** Slower or no gain twice (type-symbol cache, descriptor-name cache).
- **Tidy frames everywhere.** Code 80.7 -> 107.4 MB. Precision about liveness costs bytes, and bytes are paid
  three times (generator, assembler, linker).

## What resists the shape (the open part)

1. **Code generation is the biggest remaining row** (compileFunc about a third of samples) and it's still
   per-function tree walking. The data-parallel answer was measured but never built: the top 4,096 *expression-node*
   shapes cover 96.7% of 1.42 M nodes, while statement shapes cover only 80.9% (iteration 9). That's a
   group-by-shape, then a stencil per shape, then a scatter of patches. It's bottom-up's campaign 2, and the
   numbers say it's worth trying at node grain, not statement grain.
2. **Type checking is inference over a graph.** The declaration pass is small (0.3 s chain for all of MinIO), but
   bodies need answers from it in random order. It parallelizes as a map, but every lookup is a dependent miss.
3. **Interop with an existing runtime cost the most hand work.** Stack maps for the collector, nosplit chains,
   write-barrier rules, a vfork child's frame, a mutex word that packs an address. None of it is about speed. All of
   it is where the agent fought at the level of locks and frames. A toolchain that owns its runtime doesn't have to
   honor another compiler's private invariants.
4. **Verification needed its own structure.** A slow, obviously right twin diffed on every build (the
   string-keyed resolver, FC_SLOTCHECK, samebytes). A data-parallel language could give this for free: the same
   program run sequentially is the oracle.

## What this suggests for a language

- **The data model is entities with dense IDs and columns.** Maps keyed by objects are the anti-pattern the
  profile kept finding.
- **The core operations are few:** map, scan, partition/sort, join, flood (with frontier batching built in), and
  reduce-by-key with a deterministic combiner. Nearly every lock in fc was a reduce-by-key written by hand ("who owns
  this symbol?"). As a primitive it's lock-free and deterministic by construction, and the same-bytes property comes
  free.
- **No shared mutation; arenas per phase.** The bump allocator and the collector off were wins, and a phase that
  owns its memory frees it in one step.
- **A cost model in the type system.** Work and span (NESL) say statically how a program scales with cores. The
  measurements add a third number that matters as much: memory touched. "Faster with more cores" failed here
  (core-s +39% from 1 to 8) because of memory, not span.
- **Vector width matters less than expected.** It speeds up scans, hashing, partitioning and the stencil copy. The
  build's time is in latency-bound graph work, where the wins are cache locality and parallelism.

## Pictures worth drawing (all from data already in the repo)

- The arc above as a waterfall: each change's seconds and core-seconds, kept vs killed.
- Core-seconds against core count (16.4 -> 22.8): the memory wall, the most surprising number here.
- The critical-path simulation (simcores, 11-12 15:35): wall time against cores, as-is vs declarations-first.
- The build's cores-busy timeline before and after declarations-first (TRACE has the timelines).
- The link against its floor, phase by phase (FLOOR.md): ms on one axis, % of floor on the other.
- Where the CPU goes inside fc, before and after slots: the hash-map share shrinking.
