# NEXT

State at 2026-11-12 14:05 UTC (JOURNAL iteration 20): MinIO cold 10.8-11.3 s in a quiet stretch (expect 11-12), kube-apiserver
20.5 s, configuration `13-noweakptr` (`tools/trace/run09.sh <minio|kube> <label> -zygote
-headstart -directout`). Fair stock rows: MinIO 32.5 s, kube 51.2 s. The exact CPU table is in TRACE.md
(`tools/trace/cpuphases.sh`): walk + generate + assemble 28.7 core-s, export-IR round trip 17, type
check 10, objects + export 6.7.

Scheduling steps are exhausted (iteration 19); removing work is what moves the trace (iteration 16:
6 core-s; iteration 20: 1.6 core-s after correction). Measure with `tools/trace/abd.sh` (order-balanced)
or min-of-N on single packages; never old-then-new in a fixed order. Two big rungs, and one cheap vein worth an hour before them:

0. More work the compiler does only for the linker to discard. `SB_TYPELOG` lists every type descriptor
   emitted (223 k left after iteration 20, most of them anonymous); compare with what survives in the
   binary (`go tool nm -type`), and do the same for method wrappers and equality/hash functions.

1. F4, the link (1.2 s serial, the whole tail). Numbers to design against (JOURNAL iteration 18):
   loadlib 439 ms of which reading objects is 55 and symbol resolution ~390; deadcode 246; asmb 118;
   pclntab 114; dodata 112. The stock linker's output depends on load order, so an incremental link
   needs its own deterministic order. First slice to consider: the compilers hand out dense symbol IDs
   (the driver knows the whole graph), so resolution is a table write as objects arrive.
2. F2/F3: whole packages leaving the stock compiler. The export-IR round trip (17 core-s) and the
   assembler path (`obj.Prog`, 8.8% of compile samples after the one-pass change) are what a second
   compiler would not pay; `05-sbc` showed that taking single functions does not pay. Needs export data
   for stock-compiled importers produced without the stock front end, or a leaf-first rollout (main
   packages and packages only sbc-compiled packages import).
3. Smaller, if a session has an hour to spare: stream the whole plan into the scheduler (about 0.3 s);
   baseline coverage of the 2% of functions still sent to SSA (they cost 4.7% of compile samples).
