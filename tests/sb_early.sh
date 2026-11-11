#!/usr/bin/env bash
# Differential test of the compiler patch (driver/early_export.py), on a whole build.
#   tests/sb_early.sh <srcdir> [pkg]
# 1. Idle, the patched compiler is the stock compiler: every package's export file and object, and the linked
#    binary, are byte-identical to the stock compiler's.
# 2. With early export on, the linked binary has the same symbols as the stock compiler's build: same names,
#    same sizes, same types, same section sizes. (Not the same bytes: importers refer to symbols by name
#    instead of by index, and the linker then lays a few duplicate-allowed functions out in a different order.)
set -e
cd "$(dirname "$0")/.."
src=$1; pkg=${2:-.}
t=$(mktemp -d /dev/shm/sbearly.XXXXXX)
F=(-C "$src" -pipeline -backends 2000 "-gcflags=-dwarf=false -l" "-ldflags=-w" -workdir "$t/w" -keep)
run() { systemd-run --user --scope -q -p MemoryMax=22G env GOGC=off build/sb "${F[@]}" "$@" "$pkg" > /dev/null; }
run -o "$t/stock.bin";                                        mv "$t/w" "$t/stock"
run -o "$t/idle.bin" -compiler "$PWD/build/compile-sb" -noearly; mv "$t/w" "$t/idle"
if diff -rq "$t/stock" "$t/idle" > "$t/diff.txt" && cmp "$t/stock.bin" "$t/idle.bin"; then
  echo "PASS patched compiler, idle: $(find "$t/stock" -name '_pkg_.*' | wc -l) files and the binary byte-identical to stock"
else head "$t/diff.txt"; echo "FAIL patched compiler, idle"; exit 1; fi
rm -rf "$t/stock" "$t/idle"
run -o "$t/early.bin" -compiler "$PWD/build/compile-sb";      rm -rf "$t/w"
for b in stock early; do
  GOTOOLCHAIN=local go tool nm -size "$t/$b.bin" | awk '{print $2,$3,$4}' | sort > "$t/$b.nm"
  size "$t/$b.bin" | tail -1 | awk '{print $1,$2,$3}' > "$t/$b.size"
done
if cmp "$t/stock.nm" "$t/early.nm" && cmp "$t/stock.size" "$t/early.size"; then
  echo "PASS early export: $(wc -l < "$t/early.nm") symbols with the same names, sizes and types as stock; sections $(cat "$t/early.size")"
else diff "$t/stock.nm" "$t/early.nm" | head; echo "FAIL early export"; exit 1; fi
rm -rf "$t"
