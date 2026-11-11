# NEXT

1. Rung 1: `build/sb` (driver/main.go) builds MinIO end to end: go list -> read -> Strand golex over every
   file -> go tool compile/asm per package -> link -> write. Validate: command lines against the stock action
   graph, `tests/minio_smoke.sh`, golex tokens against go/scanner on every MinIO file. Add its trace to TRACE.md.
2. Rung 2: cut the dependency chain. `driver/mkcompiler.sh` patches cmd/compile to write export data before
   the back end (early export); the driver releases dependents on "sb:export-ready". Differential test: export
   data and objects byte-identical to the unpatched compiler's.
3. Then the work itself: -N -l / -dwarf=false / GOGC measurements in the driver; dead-function share (IDEAS).
