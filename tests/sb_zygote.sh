#!/usr/bin/env bash
# Differential test of the compiler fork-server (driver -zygote, runtime.SBFork): a whole build with compiles
# forked from one warm compiler must produce, file for file, the bytes of the same build with one exec per
# compile; and a compile error must still fail the build with the compiler's message.
#   tests/sb_zygote.sh <srcdir> [pkg]
set -e
cd "$(dirname "$0")/.."
src=$1; pkg=${2:-.}
t=$(mktemp -d /dev/shm/sbzyg.XXXXXX); trap 'rm -rf "$t"' EXIT
F=(-C "$src" -plan=self -decl "$PWD/build/godecl" -compiler "$PWD/build/compile-sb" -pipeline -shard 3000000 -backends 2000 "-gcflags=-dwarf=false -l" "-ldflags=-s -w" -workdir "$t/w" -keep)
run() { systemd-run --user --scope -q -p MemoryMax=22G env GOGC=off SB_BASELINE=1 build/sb "${F[@]}" "$@" "$pkg" > /dev/null; }
run -o "$t/exec.bin";          mv "$t/w" "$t/exec"
run -o "$t/fork.bin" -zygote;  mv "$t/w" "$t/fork"
if diff -rq "$t/exec" "$t/fork" > "$t/diff.txt" && cmp "$t/exec.bin" "$t/fork.bin"; then
  echo "PASS fork-server: $(find "$t/exec" -name '_pkg_.*' | wc -l) export files and objects and the binary byte-identical to one exec per compile"
else head "$t/diff.txt"; echo "FAIL fork-server"; exit 1; fi
mkdir -p "$t/bad" && printf 'module bad\ngo 1.27\n' > "$t/bad/go.mod" && printf 'package main\nfunc main() { undefinedName() }\n' > "$t/bad/main.go"
if out=$(env GOGC=off build/sb -C "$t/bad" -o "$t/bad.bin" -plan=self -decl "$PWD/build/godecl" -compiler "$PWD/build/compile-sb" -pipeline -zygote . 2>&1); then
  echo "FAIL fork-server: a build with a type error succeeded"; exit 1
elif echo "$out" | grep -q 'undefined: undefinedName'; then echo "PASS fork-server: a compile error fails the build with the compiler's message"
else echo "$out" | tail -5; echo "FAIL fork-server: error message lost"; exit 1; fi
