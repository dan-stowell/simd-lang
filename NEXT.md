# NEXT

State at 2026-11-12 14:45 UTC (JOURNAL iteration 21): MinIO cold 11.8 s (mean of six, order-balanced
against last night's 13.2 s: `tools/trace/day.sh`), kube-apiserver 20.5 s, configuration `13-noweakptr` (`tools/trace/run09.sh <minio|kube> <label> -zygote
-headstart -directout`). Fair stock rows: MinIO 32.5 s, kube 51.2 s. The exact CPU table is in TRACE.md
(`tools/trace/cpuphases.sh`): walk + generate + assemble 28.7 core-s, export-IR round trip 17, type
check 10, objects + export 6.7.

Which rung is first depends on the target machine (JOURNAL iteration 23, `tools/trace/simcores.py`): on
this 8-core VM the build is bound by work; on 16+ cores it is bound by a 44-package chain of front ends
(floor 8.7 s) and declarations-first export comes first (5.5 s at 32 cores). Ask the user (MORNING.md,
decision 1) or assume this VM.

Scheduling steps are exhausted on 8 cores (iteration 19); removing work is what moves the trace (iteration 16:
6 core-s; iteration 20: 1.6 core-s after correction). Measure with `tools/trace/abd.sh` (order-balanced)
or min-of-N on single packages; never old-then-new in a fixed order. Two big rungs, and one cheap vein worth an hour before them:

0. More work the compiler does only for the linker to discard. `SB_TYPELOG` lists every type descriptor
   emitted (223 k left after iteration 20, most of them anonymous); compare with what survives in the
   binary (`go tool nm -type`), and do the same for method wrappers and equality/hash functions.

1. F4, the link (1.1 s serial, the whole tail). Start from `explore/f4link` (JOURNAL iteration 25):
   resolve + layout + relocate everything is 0.26 s on 8 cores, 200 ms of it hashing 1.84 M
   content-addressed symbols (pc tables, stack maps, strings). First slice: make the compilers stop
   emitting those as separate symbols (or dedup per package), measure resolution again (expect ~60 ms),
   then add dead code (frontier-parallel flood over 3.4 M relocation edges) and pclntab to the spike,
   then make it emit an image that runs. The stock linker's output depends on load order (iteration
   18); ours must define its own deterministic order (the spike's rule: lowest object index wins).
2. F2/F3: whole packages leaving the stock compiler. The export-IR round trip (17 core-s) and the
   assembler path (`obj.Prog`, 8.8% of compile samples after the one-pass change) are what a second
   compiler would not pay; `05-sbc` showed that taking single functions does not pay. Needs export data
   for stock-compiled importers produced without the stock front end, or a leaf-first rollout (main
   packages and packages only sbc-compiled packages import).
3. Smaller, if a session has an hour to spare: stream the whole plan into the scheduler (about 0.3 s);
   baseline coverage of the 2% of functions still sent to SSA (they cost 4.7% of compile samples).
