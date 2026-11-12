# NEXT

State at 2026-11-12 13:35 UTC (JOURNAL iteration 16): MinIO cold 11.6-13.5 s, kube-apiserver
21.1-23.2 s, configuration `10-onepass` (fork-server on, one-pass assembler). Fair stock rows: MinIO
32.5 s, kube 51.2 s. The exact CPU table is in TRACE.md (`tools/trace/cpuphases.sh`).

1. In progress: stream the first edge of the DAG. Start compiling `runtime` and its dependency closure
   at time zero, in parallel with the plan (0.9 s on 2 cores), so the build fans out when the plan
   ends instead of 0.5 s later. Expected: about -0.5 s of wall clock at the head.
2. The tail: link + write output is 1.5 s at 1.5 cores, now the largest serial item (F4). First
   measurement to make: the linker's own phase times on the baseline build's objects
   (`-ldflags=-benchmark=cpu`), then decide between overlapping it with the compile tail and replacing
   layout + relocation (LINKING.md).
3. F2 proper, in the form the measurements leave standing: compiler IR built directly from checked
   syntax (no export-IR round trip for the package being compiled: 15 core-s), and whole packages
   bypassing the stock compiler (needs export data produced without it). Large; start with a design
   note and the smallest package that can go end to end.
