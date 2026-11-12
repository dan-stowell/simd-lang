# NEXT

State: MinIO cold 21.3-24.2 s (stock 54.2 s) without sbc; with sbc (4,458 leaf functions taken) about 1 s more.
kube-apiserver 43.9 s (stock 86.3 s). All differential tests and both smoke tests pass.

1. Overlap sbc with the compiles of the packages it never touches (runtime, internal/*, ...), so its ~1 s
   whole-program check is off the critical path.
2. sbc with stack frames: locals in typed slots, one stack map per function (all pointer slots, zeroed at
   entry), register arguments spilled at entry, the stack-growth prologue with obj.RegSpill, calls through
   ABIInternal. Extend tests/sbc_gen.py with locals, loops, calls, deep recursion and allocation pressure.
3. Then widen types: strings, slices, interfaces (multi-register values), value receivers, globals.
4. Still open: persistent godecl; modinfo in the link; divergence step (due now: run in the background).
