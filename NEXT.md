# NEXT

State at 2026-11-12 03:20 UTC (journal iteration 14). MinIO cold 12.3-13.3 s, kube-apiserver cold
24.1-25.2 s, configuration `08-huge-shard` (TRACE.md).

1. Triage the divergence step's ideas into IDEAS.md (subagents fired 03:18); pick the next spike.
2. The head of the build: plan 0.7-0.9 s with nothing compiling, then the runtime's front end alone.
   Start the standard library's runtime closure while the plan of the rest is still being read.
3. Kernel and start-up time that is left: 1.7 M faults (the first 4 MB of each process, binaries),
   1,312 process starts at ~9 ms. Smaller compiler binary (drop the other architectures)?
4. Baseline density, next items from the census: scalar slot-to-slot moves, compare-and-branch on
   variables, the prologue's REP STOSQ.
5. Before morning, with the final compiler: `tests/minio_unit.sh` (both modes), the wide standard
   library stress run (`GOGC=10 SB_TESTARGS="-count=3 -skip=TestTransportGCRequest"`), ten
   consecutive MinIO builds, cold traces REPS=4 for MinIO and REPS=2 for kube.
6. Read the correctness review (`reviews/inbox/*-sol.md`, `shelley client read <id>`).
7. MORNING.md at the deadline (10:54 UTC): trace before/after, rungs, ideas tried, disagreements with
   reviewers, and the two decisions that are the user's: the huge-page pool, and the liveness
   difference of the baseline (objects kept alive by dead slots).
