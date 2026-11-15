# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)

Campaign 1, 2026-11-15 18:20 UTC. **MinIO built entirely by fc, linked in process: cold 2.4-2.7 s on 8 cores this morning (idle machine, smoke passing; exit test 3.3 s); warm 1.8-1.9 s this afternoon** (cold not retaken: the machine has been busy with test runs and a builder). Corpus cold this morning: buildifier 0.5 s, runsc 0.6, tailscaled 1.25, caddy 1.6, kube-apiserver 2.8, prometheus 4.9.
What moved it today: a bump allocator in fc's runtime, the stock assembler run inside fc's process (MinIO started it 170 times), reusable scratch buffers in code generation, pc tables encoded on the compiling thread.
Correctness: MinIO's own tests 45 of 46 packages, cmd failing the one test stock fails. Running the std suite with test packages compiled by fc too found two real bugs (a string or float compared with an interface value; structs with zero-size array fields passed in registers) and three unsupported constructs; all fixed, with corpus cases. Full reruns on the fixed compiler are in progress.
Bazel rows: buildifier 9.6 s and runsc 26 s cold from Bazel's DAG (stock Bazel: 57 s and 195 s); nearly all of ours is Bazel's own start and analysis (6 and 22 s); fc is 0.4-0.6 s. BuildBuddy's two rows are with a builder.

## Decisions for Dan

1. Bazel rows: Bazel's own cold analysis is now the whole cost. I keep it inside the timer as CHARTER says. Would extracted external repositories kept between runs (`--vendor_dir`, the analogue of GOMODCACHE) count as cold? I will report both numbers.
2. Huge pages (10 GB pool during the build), skipped tests (now 63 lines: allocation counts and liveness waits only), the race detector's stack guard in an fc-built runtime: unchanged.
