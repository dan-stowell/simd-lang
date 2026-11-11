#!/usr/bin/env bash
# Strand's Go lexer against go/scanner on exactly the bytes the driver lexes for a build (every Go file of
# every package, concatenated).   tests/sb_lex.sh <srcdir> [pkg]
set -e
cd "$(dirname "$0")/.."
t=$(mktemp -d /dev/shm/sblex.XXXXXX)
build/sb -C "$1" -o /dev/null -lexer "$PWD/build/golex_strand" -keep -workdir "$t/w" -gcflags=-STOPHERE "${2:-.}" > /dev/null 2>&1 || true
[ -s "$t/w/all.tok" ] || { echo "FAIL: the driver produced no tokens"; exit 1; }
build/golex dump "$t/w/all.go" > "$t/ref.tok" 2> "$t/ref.err"
grep -q 'scanner errors: 0' "$t/ref.err" || { cat "$t/ref.err"; echo "FAIL: go/scanner reports errors on this corpus"; exit 1; }
cmp "$t/ref.tok" "$t/w/all.tok" || { echo "FAIL golex != go/scanner"; exit 1; }
echo "PASS golex == go/scanner: $(( $(stat -c %s "$t/ref.tok") / 5 )) tokens, $(stat -c %s "$t/w/all.go") bytes"
rm -rf "$t"
