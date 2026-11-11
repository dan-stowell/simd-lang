#!/usr/bin/env bash
# Regression tests for the driver's use of the patched compiler, from the sol review of 2026-11-11
# (reviews/early-export-2026-11-11/README.md).
set -e
cd "$(dirname "$0")/.."
R=$PWD/reviews/early-export-2026-11-11/cases
t=$(mktemp -d /dev/shm/sbdrv.XXXXXX)
run() { systemd-run --user --scope -q -p MemoryMax=8G "$@"; }
# 1. A compiler that prints lines longer than any buffer (-S on a 381-byte program whose instantiated name
#    is megabytes long) must not hang the pipelined driver.
run timeout 120 build/sb -C "$R/stdout" -pipeline -compiler "$PWD/build/compile-sb" -j 2 -backends 4 -gcflags=-S -o "$t/shape.bin" > /dev/null \
  || { echo "FAIL: pipelined build with -S hung or failed"; exit 1; }
"$t/shape.bin"; echo "PASS giant stdout lines do not hang the pipeline"
# 2. -noearly means stock behaviour even when SB_EARLY_EXPORT is set in the driver's own environment.
run build/sb -C "$R/baseline" -pipeline -o "$t/stock.bin" > /dev/null
run env SB_EARLY_EXPORT=1 build/sb -C "$R/baseline" -pipeline -compiler "$PWD/build/compile-sb" -noearly -o "$t/idle.bin" > /dev/null
cmp "$t/stock.bin" "$t/idle.bin" || { echo "FAIL: -noearly with SB_EARLY_EXPORT inherited differs from stock"; exit 1; }
echo "PASS -noearly ignores an inherited SB_EARLY_EXPORT"
rm -rf "$t"
