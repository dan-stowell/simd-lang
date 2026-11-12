# IDEAS

Each idea: one line, why it could move the trace, kill test, status (untested / spiking / dead / adopted).

## Seeded by the user
- Compiling the build graph itself (later rung). Kill test: TBD.

## 2026-11-11/12 overnight

- **Publish export data before the back end (pipelining).** Kill test: does MinIO's chain shrink. Status: **adopted** (rung 2; chain 38.5 s -> 12.1 s).
- **Front ends hold the CPU tokens, back ends run at nice 19.** Status: **adopted** (without it pipelining gained nothing: one 1 kB package waited 27.7 s behind other packages' back ends).
- **No DWARF, no inlining, no compiler GC.** Status: **adopted** (rung 3; 273 -> 144 core-s). `-N` on top of `-l` gains nothing (25.8 vs 26.2 s) and makes the binary 30% bigger: not adopted.
- **Do not compile what the linker will throw away.** Whole-program reachability by name before code generation: a function whose name occurs nowhere else in the program's tokens cannot be called. The counting is a histogram over the token stream, which is Strand-shaped. Kill test: share of compiled text the linker discards. Status: **dead** (21.3% on MinIO, and only about half of that is reachable by a name-based analysis; JOURNAL iteration 5).
- **One sequential read instead of 7,640 opens.** The module cache already holds each module as one zip. Reading sources cold costs 300 ms as files, 26 ms as one sequential read of the same bytes (HARDWARE.md). Kill test: time `unzip -p` of the build's modules against the file reads. Status: untested.
- **Plan without `go list`.** 0.5 s today, serial. Strand's godecl already emits every file's package clause and import paths; the graph is a join on those. Needs build-constraint evaluation and module path resolution. Status: untested.
- **A Strand back end for a subset of functions** (codex skeptic, 2236). Export typed IR from the stock front end, emit machine code for the functions the subset covers, stock for the rest. Status: deferred until functions are scheduled individually.
- **Split `minio/cmd`'s front end.** The chain is now one package: 5 MB of Go type-checked on one thread (about 4 s) and then compiled. The type checker is the serial part. Kill test: none cheap. Status: untested.

## Divergence step, 2026-11-11 23:25 UTC (four subagents; each saw only CHARTER, TRACE, HARDWARE and one lens)

Three of the four independently proposed reachability before code generation, with kill thresholds of
50-60% of compiled text surviving the link. Measured before they answered: 78.7% survives. **Dead.**

- **Declarations first, then every function body as one flat work list** (two lenses). Pass A resolves
  package-level declarations for all packages in one process; pass B checks and compiles each body
  independently against that table, sharded over the cores with no package boundary and no export data.
  Removes the chain (12.1 s, of which `minio/cmd` is 9.1 s) and object writing. Kill test: compile
  `minio/cmd` alone; dead if its front end is under about 1.5 s. *Measured: its front end is 3.6-4 s on
  one thread, so the idea survives.* Second kill test (untested): `go/types` with `IgnoreFuncBodies` over
  all packages; dead if over about 400 ms or if many package-level vars need body typing. Status: untested;
  this is the shape of the end state.
- **Template back end: copy and patch stencils chosen by (IR op, type class, operand location)** (three
  lenses). Every local in a stack slot, frames zeroed, all pointer slots live for the whole function, so
  stack maps need no liveness analysis. Kill test 1: an unoptimized (`-N -l`) MinIO passes the smoke
  test. *Measured: passes; binary 130 MB against 100 MB.* Kill test 2 (untested): histogram of statement
  and expression shapes over the 76 MB; dead if the top 4,096 cover under 95%. Kill test 3 (untested):
  per-pass SSA timing; dead if the passes a template back end deletes are under half the back end.
  *Partly measured (TRACE.md): SSA passes 35 core-s of 67 in the back end, so about half.* Status: untested.
- **No linker: dense symbol IDs, emit into the final image, one prefix sum for addresses.** Removes the
  link (1.5 s at 1.4 cores) and 601 MB of objects written and re-read. Kill test: link with
  `-benchmark=cpu`; dead if symbol loading, resolution, dead code and relocation are under 40% of the
  link. Status: untested.
- **Deduplicate function bodies before compiling** (hash each body's token stream with identifiers
  renamed; compile one per class). The four go-json VM packages are near-copies. Kill test: duplicate
  bytes by normalized-body hash over the lexer's output; dead under 25% of the source. Status: untested.

**Picked for the next spike:** none of the survivors can be finished tonight, and the cheapest
informative test is the shape histogram (template back end, kill test 2).

## 2026-11-12, from the optimization iteration

- **Turn optional SSA passes off.** Status: **dead** (slower: 23.9-26.8 s against 21.9-24.1 s; JOURNAL iteration 8).
- **Pause back ends beyond a cap instead of letting hundreds run at nice 19.** Status: **dead** for wall clock (no gain); it does lower CPU and peak memory, so `-running n` stays as an option for builds that do not fit in memory.
- **Compile each duplicate-allowed function once per build, not once per package.** Measured: 8.1% of MinIO's compiled text is copies (26,052 functions). Kill test passed in the weak sense (it is not nothing); payoff at most 5% of the build. Status: untested.

## 2026-11-12, measured (JOURNAL iteration 9)

- **Declarations first** kill test 2: go/types with IgnoreFuncBodies over all of MinIO, one process: 365-430 ms wall, 2.3-2.6 core-s, chain 0.3 s, no errors. Line was 400 ms. **Alive.** With bodies: 963 ms wall, 4.6 core-s.
- **Template back end** kill test 2: top 4,096 typed expression-node shapes cover 96.7% of 1.42 M nodes (**alive** per node); top 4,096 whole-statement shapes cover 80.9% (**dead** per statement).
- **A second compiler taking over single functions inside the stock build** (bodyless declaration + symabis + extra object in the package archive). Status: **spiking now** (rung 5 skeleton).

## Divergence step, 2026-11-12 03:20 UTC (four subagents; each saw only CHARTER, TRACE, HARDWARE and one lens)

Twelve ideas, five distinct. Status after triage:

| idea | from | status |
|---|---|---|
| **Reachability before code generation**: compute the linker's live set from `main` first, lower and emit only live functions. (Proposed by all four lenses; their kill test: object text against linked text.) | all | **already measured, negative** (iteration 9: 21% of functions are dead; saving at most a fifth of the 27 body-proportional core-s, and the live set needs the whole program type-checked first, which costs 7.4 core-s by the `explore/wholeprog` measurement). 641 MB of objects against a 160 MB binary is mostly export data, pclntab pieces and duplicated instantiations, not dead text. |
| **One resident compiler**: one address space, importers follow pointers, no export data, no process start. | flip, design, ridiculous | **the direction, not a spike.** Their kill tests are done: process start is 1,312 x ~10 ms = 13 core-s (HARDWARE.md); export IR write + read is 9.2 core-s. Together a quarter of the compile row. Cannot be done inside cmd/compile (global state); it is the front-end replacement of NEXT.md. |
| **Zygote**: fork compilers from a pre-initialized process. | borrow | ~~dead: a Go process cannot fork without exec~~ **built in iteration 15** (`runtime.SBFork`, `-zygote`): it can, with 60 lines in the runtime. Byte-identical output; worth 1.6 core-s of ~71 and no wall clock, because process start was 3.6 core-s, not 12. |
| **No link / cut-through link**: assign addresses as code is produced, patch relocations in one parallel pass; or a linker that ingests objects as they finish. | flip, borrow, design | **untested.** Link is 1.3 s of a 12.5 s build, serial, last. Needs a linker of our own; kill test (phase-time the stock linker) partly done in iteration 4 (`linkprof_*`): no single phase dominates. Best large idea left; too big for the rest of tonight. |
| **Compile each function shape once**: hash bodies with names and literals erased, compile one per shape, patch the rest. | ridiculous | **killed in 10 minutes** (`explore/shapes`): of 35 MB of function bodies in the MinIO build, 27.1% is in token shapes seen at least twice and 20.8% is beyond the first copy; that is an upper bound (types must match too; exact token text repeats for 5.7%). Under the proposer's own 25% threshold. |

Picked for the next spike: none of these. The trace's own idle stretches (the first second, where only
the planner runs) are cheaper to attack than any of them.

## Divergence step, 2026-11-12 12:45 UTC (three subagents on a cheaper model; each saw only CHARTER, TRACE, HARDWARE and one lens)

Nine ideas, seven distinct. Status after triage (JOURNAL iteration 16 has the measurements):

| idea | from | status |
|---|---|---|
| **Resident typed universe**: a warm process holds imported packages' typed state; importers follow pointers; no export IR read. | fusion, design | **dead as stated**: import decoding is 1-3% of compile samples (`importer.*` 0.4-0.7 s, `readImportFile` 1.1 s of 51.8 s). The 15 core-s round trip is the package's own IR. Surviving form: build compiler IR directly from checked syntax (large; no kill test under an hour). |
| **Function-granularity work-stealing, dedup, streaming link.** | fusion | **untested; this is F3.** Last night's scheduling simulation says the compile stretch is CPU-bound (10.3 s against a 9.3 s bound), so the gain is the tail: link + write, 1.5 s at 1.5 cores. |
| **Pinned persistent workers with arena reset, graph partitioned for locality.** | fusion | **dead by the table**: setup rows are ~4.5 core-s after the fork-server; cross-core traffic is 0.05 s by its own arithmetic. |
| **Compile shared bodies, wrappers and type descriptors once (content-addressed).** | design | **dead for descriptors**: 371 k emitted, 106 k duplicates (28.6%), worth ~1.6 core-s. Bodies: killed earlier (`explore/shapes`, 20.8%). |
| **Delete the IR round trip: type-check straight into back-end IR.** | design | **alive, large**: 15.1 core-s measured (write 5.0, read back 10.1). Same as the surviving form of the first row. |
| **Template/stencil emitter, columnar codegen, presized layout, no link.** | design, ridiculous | **untested; F3/F4's emitter.** After the one-pass assembler, instruction encoding is `span6` 3.7% of samples (~2.6 core-s) and all of `Flushplist` 8.8%; the larger prize in this row is the serial link (1.2-1.4 s wall). |
| **Bump allocator, never free.** | ridiculous | **dead in 15 minutes**: `GODEBUG=sbrk=1` gives no CPU gain on go/types (0.56 s vs 0.57 s) and deadlocks intermittently. |
| **Lazy compilation stubs in the output binary.** | ridiculous | **rejected**: moves compilation into the run; the charter's output is a working binary. |

Picked for the next step: none of these. The head of the build (plan, then `runtime`, on two cores) is cheaper.

