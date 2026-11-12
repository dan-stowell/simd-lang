# NEXT

State at 2026-11-12 13:25 UTC (JOURNAL iteration 17): MinIO cold 11.6-12.8 s (interleaved), kube-apiserver
20.5-23.8 s, configuration `11-headstart` (fork-server, one-pass assembler, head start). Fair stock rows:
MinIO 32.5 s, kube 51.2 s. The exact CPU table is in TRACE.md (`tools/trace/cpuphases.sh`).

1. In progress: the tail. Link + write output is 1.5 s at 1.5 cores, the largest serial item (F4).
   Measure the linker's phases on the baseline build's objects (`-ldflags=-benchmark=cpu`), then decide
   between overlapping it with the compile tail and replacing layout + relocation (LINKING.md).
2. Stream the whole plan into the scheduler (the head start is its first slice): compile a package when
   its imports are resolved, not when the last planning round ends. Bounded by the plan's idle cores:
   about 0.5 s.
3. F2 proper, in the form the measurements leave standing: compiler IR built directly from checked
   syntax (no export-IR round trip for the package being compiled: 15 core-s), and whole packages
   bypassing the stock compiler (needs export data produced without it). Large; start with a design
   note and the smallest package that can go end to end.
4. Triage `reviews/inbox/*-sol.md` (correctness review of the one-pass assembler, fired 13:12 UTC; its
   answer arrives as a commit in /home/exedev/strand-w under reviews/).
