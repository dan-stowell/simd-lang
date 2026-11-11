# NEXT

State: MinIO cold 21.96 s (stock 54.2 s); kube-apiserver cold 53.4 s (stock 86.3 s). Both smoke tests pass.

1. Rung 4: plan without `go list`. The driver resolves the package graph itself while reading the sources:
   go.mod build list -> module directories; files chosen by build constraints; imports from Strand's
   declaration pass (examples/godecl.strand emits import paths and package clauses); breadth-first rounds.
   Differential test: package set, file lists, import lists equal `go list -deps` on MinIO and kube-apiserver.
2. Budgeted optimization (not before item 1): optional SSA passes off (`-d=ssa/<pass>/off`, about 7 core-s),
   per-pass `time.Now` in ssa.Compile (1.3 s), cap on running back ends without stalling front ends.
3. Spike: shape histogram for a template back end (IDEAS, kill test 2).
4. modinfo in the link (SCALAR_LEDGER.md).
