# NEXT

State: MinIO cold 21.96 s (stock 54.2 s), run `03-relaxed`: pipelined patched compiler, -dwarf=false -l, GOGC=off.

1. Spike (kill test first): how much compiled text does the linker discard on MinIO? Sum function sizes over
   all archives against the linked binary. If more than about a third: reachability by name before codegen.
   Reference implementation with go/parser (also the oracle), stubs for dead function bodies, differential
   test: every stubbed function must be absent from the stock-linked binary. Then the Strand version
   (godecl gives names and body braces; add an identifier histogram).
2. kube-apiserver through the same driver (charter: don't overfit): needs tests/kube_smoke.sh (/healthz),
   and a memory check with GOGC=off (use GOGC=1000 if it does not fit in 22 GB).
3. modinfo in the link (build info is missing from sb binaries; see SCALAR_LEDGER.md).
4. Divergence step is due at journal entry 5 (prompts/divergence.md).
