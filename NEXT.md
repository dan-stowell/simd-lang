# NEXT

1. Rung 0 of CHARTER.md. Clone MinIO and Kubernetes under /home/exedev/targets/ (pin and
   record the commits), `go mod download` each, and record the Go version. Write one script
   that does a cold build (fresh GOCACHE, dropped page cache, CGO_ENABLED=0) of `minio` and
   `kube-apiserver` on 1, 2, 4 and 8 CPUs with `-debug-trace`, and turns the traces into
   TRACE.md: per-phase ms, critical path through the package graph, idle cores over time,
   the link's share, bytes read, source size, and the floor for each phase. Commit it.
   Time the module fetch separately.
2. Rung 1. A walking skeleton for MinIO: a driver that runs Strand's exact Go lexer over every
   file in the build, then delegates to `go tool compile`/`asm`/`link`, and regenerates
   TRACE.md. Validate on the Go toolchain corpus first (fast), then on MinIO.
3. Then read TRACE.md and write in JOURNAL.md which phase owns the most critical-path wall
   clock, and what serializes it. That is rung 2.
