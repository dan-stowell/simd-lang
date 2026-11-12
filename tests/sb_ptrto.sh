#!/usr/bin/env bash
# Differential test for "no speculative *T descriptors" (driver/early_export.py, fourteenth change):
# tests/go/ptrto prints what reflect says about pointers to unnamed types reached statically, through
# PointerTo and through New. Stock compiler, baseline with the descriptors, baseline without: same output.
set -e
cd "$(dirname "$0")/go/ptrto"
G=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
cp ../../../build/compile-sb "$G/pkg/tool/linux_amd64/compile"
t=$(mktemp -d /dev/shm/sbptrto.XXXXXX); trap 'rm -rf "$t"' EXIT
# Every build must compile and run (exit 0) and print the whole report: identical error output from a broken
# toolchain is not a pass (review 2026-11-12).
run() { n=$1; shift; env GOROOT=$G GOTOOLCHAIN=local GOFLAGS= GOCACHE=$t/cache-$n "$@" "$G/bin/go" run "-gcflags=all=-dwarf=false -l" -ldflags=-w . > "$t/$n.out" 2>&1 || { tail -3 "$t/$n.out"; echo "FAIL pointer-type identity: the $n build did not compile and run"; exit 1; }; }
run stock A=1; run keep SB_BASELINE=1 SB_KEEP_WEAKPTR=1; run skip SB_BASELINE=1
if [ "$(wc -l < "$t/stock.out")" -ge 39 ] && cmp -s "$t/stock.out" "$t/keep.out" && cmp -s "$t/stock.out" "$t/skip.out" && ! grep -q 'false\|panic' "$t/stock.out"; then
  echo "PASS pointer-type identity: $(wc -l < "$t/stock.out") lines identical for stock, baseline with *T descriptors, baseline without"
else diff "$t/stock.out" "$t/skip.out" | head; echo "FAIL pointer-type identity"; exit 1; fi
