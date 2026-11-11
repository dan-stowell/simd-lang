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
