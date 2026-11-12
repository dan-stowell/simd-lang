# NEXT

State at 2026-11-12 (after the 2026-11-11 run, JOURNAL iteration 14): MinIO cold 12.3-13.3 s,
kube-apiserver 24.1-25.2 s, configuration `08-huge-shard` (TRACE.md). The run stopped at
03:19 UTC on the model usage limit; MORNING.md was never written. The charter now points at
fusing the build DAG (CHARTER.md, "Fusion rungs").

1. Loose ends from last night (one iteration, no more):
   - Triage `reviews/inbox/20261112T0202-codex-skeptic.md` and `20261112T0317-sol.md`, and the
     divergence round fired at 03:18 if its output survived.
   - Repeat `tests/minio_unit.sh` with the final compiler (last run spanned two compiler rebuilds).
   - Bring `SCALAR_LEDGER.md` up to date: last night took almost every phase out of Strand; say
     which, and why (lexing+parsing is ~1% of compile CPU).
   - Write MORNING.md now, and keep it current every iteration from here on.
2. Rung F0: add the fair stock row (cold `GOGC=off go build -trimpath
   -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`, 8 CPUs, MinIO and kube) and the
   materialization tax (intermediate bytes written/read, process starts, page faults,
   core-s outside compiler phases) to TRACE.md via mktrace.py.
3. Rung F1: persistent workers or a fork-server for `cmd/compile`, with export data and
   objects in shared memory. Measure both shapes on the Go toolchain corpus first, then MinIO.
4. Then F2 (whole-program front end in one address space), per CHARTER.md.
