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
# Head start (-headstart): the runtime's closure is compiled during the plan, in other directories; the
# archives and the binary must be the same bytes.
rm -rf "$t/w"; run -o "$t/head.bin" -zygote -headstart; mv "$t/w" "$t/head"
sums() { find "$1" -name '_pkg_.*' -exec sha256sum {} + | awk '{print $1}' | sort | sha256sum; }
if cmp "$t/exec.bin" "$t/head.bin" && [ "$(sums "$t/exec")" = "$(sums "$t/head")" ] && [ -d "$t/head/head" ]; then
  echo "PASS head start: $(find "$t/head/head" -name '_pkg_.a' | wc -l) packages compiled during the plan; every archive and the binary byte-identical"
else echo "FAIL head start"; exit 1; fi
mkdir -p "$t/bad" && printf 'module bad\ngo 1.27\n' > "$t/bad/go.mod" && printf 'package main\nfunc main() { undefinedName() }\n' > "$t/bad/main.go"
if out=$(env GOGC=off build/sb -C "$t/bad" -o "$t/bad.bin" -plan=self -decl "$PWD/build/godecl" -compiler "$PWD/build/compile-sb" -pipeline -zygote . 2>&1); then
  echo "FAIL fork-server: a build with a type error succeeded"; exit 1
elif echo "$out" | grep -q 'undefined: undefinedName'; then echo "PASS fork-server: a compile error fails the build with the compiler's message"
else echo "$out" | tail -5; echo "FAIL fork-server: error message lost"; exit 1; fi
# A request too large for one socket message (review 2026-11-12, finding 2) must fall back to exec, not fail.
mkdir -p "$t/big" && printf 'module big\ngo 1.27\n' > "$t/big/go.mod" && printf 'package main\nfunc main() {}\n' > "$t/big/main.go"
big=$(head -c 90000 /dev/zero | tr '\0' x)
if env GOGC=off SB_BIG1=$big SB_BIG2=$big SB_BIG3=$big SB_BIG4=$big SB_BIG5=$big build/sb -C "$t/big" -o "$t/big.bin" -plan=self -decl "$PWD/build/godecl" -compiler "$PWD/build/compile-sb" -pipeline -zygote . > "$t/big.out" 2>&1 && "$t/big.bin"; then
  echo "PASS fork-server: a 450 kB environment is compiled by exec instead"
else tail -3 "$t/big.out"; echo "FAIL fork-server: oversized request"; exit 1; fi

