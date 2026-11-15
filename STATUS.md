# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)

Campaign 1, 2026-11-15 17:30 UTC. **MinIO built entirely by fc and linked in process: cold 2.4-2.7 s on 8 cores, 13.7-15.5 core-s, idle machine, smoke test passing** (exit test 3.3 s; last scoreboard 3.9 s; frozen stock path 11.2 s; fair stock 29.5 s). Corpus cold on 8 cores: buildifier 0.5 s, runsc 0.6, tailscaled 1.25, caddy 1.6, kube-apiserver 2.8, prometheus 4.9; every binary passes its check.
What moved it: type-checker results stored in the syntax nodes, dead descriptors skipped, objects handed to the linker in memory, and a bump allocator in fc's runtime (no collector runs, so nothing is freed).
Not yet the exit test: MinIO's own tests are rerunning on this fc (45 of 46 packages pass so far, cmd running); SCOREBOARD.md is not regenerated; of the Bazel rows buildifier and runsc build from Bazel's DAG, BuildBuddy's two are with a builder.
From Bazel's DAG the cold build is Bazel itself: buildifier 9.6 s (Bazel's start and analysis 6.1 s, fc 0.4 s), runsc 26 s (Bazel 22 s, fc 0.6 s).

## Decisions for Dan

1. The Bazel rows: cold, Bazel's own server start, repository extraction and analysis cost 6-22 s before fc sees the graph. Is reading the DAG without a cold Bazel server (e.g. an aquery result treated as a fetched input, like GOMODCACHE) within "cold" for the demo, or must Bazel start inside the timer as CHARTER says now? I keep it inside the timer until told otherwise.
2. Huge pages (10 GB pool during the build), the 54 skipped tests, and the race detector's stack guard in an fc-built runtime: as in the previous STATUS; unchanged.
