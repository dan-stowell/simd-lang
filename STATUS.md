# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)

Campaign 1, 2026-11-15 19:03 UTC. **MinIO built entirely by fc, linked in process: cold 2.4-2.7 s on 8 cores this morning (idle, smoke passing; exit test 3.3 s); warm 1.8-1.9 s this afternoon.** No quiet numbers since; the machine is busy with verification and builders.
**A bug in every fc-built binary until 19:00 today, fixed (7259dfb):** fc put write barriers where the runtime forbids them, and a program could deadlock on its heap lock during collection (an fc-built protoc-gen-go hung 3 times in 1,400 runs at GOGC=1 under load; 0 of 1,600 after). fc now redoes the stock compiler's proof (`tools/fc/wbcheck.sh`: 880 runtime functions, 0 barriers). One segmentation fault in the same soak is not explained yet.
Correctness: a full pass on the fixed compiler is running (std 260 packages, MinIO's tests, language corpus). On the compiler before the fix: std 259 of 260; the runtime's own tests fail when its test package is compiled by fc too (13 tests to sort into bugs and allowed skips).
Cold reading: a cold build is 0.55 s slower than a warm one because ~10,000 small disk requests cap at 20-50 thousand a second; dependency sources now come from the module cache's zips (4,300 requests; every file compared equal to the extracted one on the corpus). Wall-clock verdict owed on a quiet machine.
Bazel rows: BuildBuddy's CLI and executor build from Bazel's DAG and pass their checks, 3x under stock Bazel (38-52 s against 124; 111-141 s against 335), not yet purely ours: cgo packages and their closure (923 of the executor's 1,990) go through the stock compiler (a builder is on it). 48 of Bazel's own 85 s there was writing JSON nobody reads: removed.

## Decisions for Dan

1. Module sources read from the zips `go mod download` left (not the extracted trees beside them): cold as CHARTER defines it, in my reading; say if not (FC_NOZIP=1 is the other way).
2. Huge pages (10 GB pool during the build), skipped tests (62 lines in tests/fc/skips.txt: allocation counts and liveness waits, one environment skip), the race detector's stack guard in an fc-built runtime: unchanged. The --vendor_dir question is withdrawn (Bazel 9 already keeps extracted repositories under --repository_cache).
