# NEXT

State: MinIO cold 20.4 s with the baseline code generator (stock 54.2 s; 21.9-25.1 s without the baseline in
the same hour). 80% of functions bypass SSA. Std tests: 29 of 29 packages pass with the baseline on.

1. Baseline coverage, by what the log says is missing (tests/sb_stdtest.sh prints the reasons):
   closure variables, intrinsic calls (call the real function), defer, append, type assertions
   (AS2DOTTYPE, DOTTYPE), floats, STR2BYTESTMP/BYTES2STRTMP, min/max, AS2FUNC, jump tables, go statements.
   After each: tests/sb_stdtest.sh, MinIO smoke, tests/sb_bisect.py on any failure.
2. Wider oracle: more std packages; MinIO's unit tests built with SB_BASELINE=1 (GOROOT copy toolchain).
3. kube-apiserver with the baseline; cold trace with REPS=3 for both.
4. Baseline output size (binary 153 MB vs 100 MB): reuse of slots, fewer redundant loads.
5. Still open: sbc overlap, persistent godecl, modinfo, second divergence step.
