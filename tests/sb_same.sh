#!/usr/bin/env bash
# Differential test for a compiler change that must not change output: build a whole program twice with the
# baseline configuration, the second time with extra environment for the compilers (a switch that turns
# the change off), and require every export file, every object and the binary to be the same bytes.
#   tests/sb_same.sh <srcdir> <pkg> <SB_COMPILE_ENV value that turns the change off>
set -e
cd "$(dirname "$0")/.."
src=$1; pkg=$2; off=$3
t=$(mktemp -d /dev/shm/sbsame.XXXXXX); trap 'rm -rf "$t"' EXIT
F=(-C "$src" -plan=self -decl "$PWD/build/godecl" -compiler "$PWD/build/compile-sb" -linker "$PWD/build/link-sb" -pipeline -shard 3000000 -backends 200 -zygote "-gcflags=-dwarf=false -l" "-ldflags=-s -w" -workdir "$t/w" -keep)
run() { systemd-run --user --scope -q -p MemoryMax=20G env GOGC=off SB_BASELINE=1 "$@" build/sb "${F[@]}" -o "$t/bin" "$pkg" > /dev/null; }
run;                         mv "$t/w" "$t/on";  mv "$t/bin" "$t/on.bin"
run SB_COMPILE_ENV="$off";   mv "$t/w" "$t/off"; mv "$t/bin" "$t/off.bin"
if diff -rq "$t/on" "$t/off" > "$t/diff.txt" && cmp "$t/on.bin" "$t/off.bin"; then
  echo "PASS $(find "$t/on" -name '_pkg_.*' | wc -l) export files and objects and the binary byte-identical with and without [$off]"
else head -5 "$t/diff.txt"; echo "FAIL output differs with [$off]"; exit 1; fi
