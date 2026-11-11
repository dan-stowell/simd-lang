# IDEAS

Each idea: one line, why it could move the trace, kill test, status (untested / spiking / dead / adopted).

## Seeded by the user
- Compiling the build graph itself (later rung). Kill test: TBD.

## 2026-11-11/12 overnight

- **Publish export data before the back end (pipelining).** Kill test: does MinIO's chain shrink. Status: **adopted** (rung 2; chain 38.5 s -> 12.1 s).
- **Front ends hold the CPU tokens, back ends run at nice 19.** Status: **adopted** (without it pipelining gained nothing: one 1 kB package waited 27.7 s behind other packages' back ends).
- **No DWARF, no inlining, no compiler GC.** Status: **adopted** (rung 3; 273 -> 144 core-s). `-N` on top of `-l` gains nothing (25.8 vs 26.2 s) and makes the binary 30% bigger: not adopted.
- **Do not compile what the linker will throw away.** Whole-program reachability by name before code generation: a function whose name occurs nowhere else in the program's tokens cannot be called. The counting is a histogram over the token stream, which is Strand-shaped. Kill test: share of compiled text the linker discards (under an hour). Status: **spiking next**.
- **One sequential read instead of 7,640 opens.** The module cache already holds each module as one zip. Reading sources cold costs 300 ms as files, 26 ms as one sequential read of the same bytes (HARDWARE.md). Kill test: time `unzip -p` of the build's modules against the file reads. Status: untested.
- **Plan without `go list`.** 0.5 s today, serial. Strand's godecl already emits every file's package clause and import paths; the graph is a join on those. Needs build-constraint evaluation and module path resolution. Status: untested.
- **A Strand back end for a subset of functions** (codex skeptic, 2236). Export typed IR from the stock front end, emit machine code for the functions the subset covers, stock for the rest. Status: deferred until functions are scheduled individually.
- **Split `minio/cmd`'s front end.** The chain is now one package: 5 MB of Go type-checked on one thread (about 4 s) and then compiled. The type checker is the serial part. Kill test: none cheap. Status: untested.
