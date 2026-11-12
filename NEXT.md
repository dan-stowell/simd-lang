# NEXT

State at 2026-11-12 13:30 UTC (JOURNAL iteration 15): MinIO cold 12.5-15.6 s on a noisy host (no real
change from 12.3-13.3), kube-apiserver 22.5-24.5 s, configuration `09-forkserver`. Fair stock rows:
MinIO 32.5 s, kube 51.2 s. F0 and F1 are done; F1 (fork-server) is worth 1.6 core-s.

The exact CPU table (TRACE.md, `tools/trace/cpuphases.sh`) is the map: be:compilefuncs 34 core-s,
fe:parse 28.6 (of which ~17 is the export-IR round trip and import reading), setup + object/export
writing 11.7.

1. Divergence step (was due at iteration 15; lenses should see the new CPU table): 3-4 subagents,
   `model: "sonnet"`, triage in the same iteration.
2. Rung F2, cheapest consumed slice first. Candidates, in the order I would try them:
   a. Split fe:parse exactly (parse / types2 / IR write / IR read / import reading) with the same
      rusage method (finer `SB_CPUPHASES` boundaries inside noder.LoadPackage), so the slice is chosen
      by measurement and not by last night's profile shares.
   b. Whole-program type check in one compiler process (types2 with in-memory imports, packages in
      parallel), then fork one back end per package from it with `runtime.SBFork`: removes per-package
      parse + type check + the types2 half of import reading. Needs the parent's heap on huge pages (or
      fork's page-table copy costs tens of ms per child) and a way to keep children from copying it.
   c. The same in threads instead of forks: needs cmd/compile's per-package globals (LocalPkg, Target,
      Ctxt) made per-compile. Larger, and the real F2.
3. Repeat `tests/minio_unit.sh` result check (run started 13:15 UTC, output in
   `/home/exedev/targets/minio-unit2/baseline.out`).
