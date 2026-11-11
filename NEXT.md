# NEXT

State: MinIO cold 21.3-24.1 s depending on the hour (stock 54.2 s); kube-apiserver 43.9 s (stock 86.3 s).
Run `04-selfplan`: the driver plans by itself (Strand godecl consumed), pipelined patched compiler,
-dwarf=false -l, GOGC=off. All differential tests and both smoke tests pass.

1. Budgeted optimization of the compile row (A/B interleaved, because the host drifts):
   optional SSA passes off via -d=ssa/<pass>/off; remove per-pass time.Now in ssa.Compile (patch);
   limit oversubscription of back ends without stalling front ends (SIGSTOP/SIGCONT beyond a cap).
2. Spike: shape histogram for a template back end (IDEAS, kill test 2).
3. A persistent godecl (one process, rounds over a pipe) to bring the declaration pass from 220 to ~100 ms.
4. modinfo in the link (SCALAR_LEDGER.md).
5. Divergence step again at journal entry 10.
