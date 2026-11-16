# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)

Campaign 1, 2026-11-16 01:15 UTC. **MinIO built entirely by fc: warm 2.1-2.3 s on 8 cores, the same bytes from every build; cold not yet measured on tonight's compiler** (11-15 morning 2.4-2.7 s; exit test 3.3 s). Last night's session was killed by the kernel's out-of-memory killer (a stock-built test program at 28 GB); heavy runs now go through tools/memlimit.py.
**A bug in every fc binary so far, found tonight: a program that started another program could crash (signal 11, nothing printed) or store 8 bytes through a stale pointer.** It needs the soft open-file limit under the hard one (the usual case; not in this agent's shell, so no suite saw it until the tests ran inside a systemd service). Cause: a frame temporary kept across the vfork call in syscall.forkAndExecInChild1. Fixed, with a test. Also fixed: loop variables in files before go1.22 are one per loop.
**The same bytes from the same sources: done** (buildifier, caddy, MinIO: three builds each equal). Its cost was 0.55 s of MinIO's wall clock when merged (found by the timing pass, bisected); two fixes later it is 0.19 s and 1.1 core-s, and it stays on. Functions by demand is the default now (0.35 s faster warm). The Go test directory, every recipe that builds a program: 1,209 pass, 0 fail, of 1,251 (28 the stock toolchain does not pass this way, 14 skips).
**Running:** the full correctness pass on HEAD (fc-verify-1116a: std, MinIO's packages, corpus smokes, Go tests in rounds), then cold numbers, SCOREBOARD and TRACE.

## Decisions for Dan

1. Module sources read from the zips `go mod download` left (not the extracted trees beside them): cold as CHARTER defines it, in my reading; say if not (FC_NOZIP=1 is the other way).
2. Byte-identical builds cost 0.19 s of MinIO's ~2.2 s (I said I would keep them only under 10 ms). I kept them on: a build that differs run to run cannot be cached or compared. FC_SHARDS=0 turns them off; say if the exit test should be timed that way.
3. Another agent's headless browser (shelley, pid 1241768, a WebGL page) has held one core since 00:15 UTC; I left it alone, and timings say so.
