#!/usr/bin/env bash
# fc's cgo test: the program in tests/fc/cgo (a cgo package with C taking and returning ints, doubles, pointers
# and structs, a callback from C into exported Go, C strings and memory, errno, C variables) built by
# `fc build -link` (cgo and gcc for the C, fc for every Go package of the program, the stock linker in external
# mode) must print what the stock toolchain's build prints (`CGO_ENABLED=1 go build`, a fresh GOCACHE).
#   tools/fc/cgotest.sh            exit 0: the outputs are equal
# Every Go package is fc's, the runtime and runtime/cgo included (FC_RUNTIME=1; FC_RUNTIME=0 leaves the runtime
# and what it imports to the go command, the earlier mixed mode): the line "stock compiler: N packages" says.
# The program runs three times; every run must print what the stock build prints.
# Environment: FC_BIN (default build/fc), CGOTEST_KEEP=<dir> keeps the work directory.
here=$(cd "$(dirname "$0")/../.." && pwd)
FC_BIN=${FC_BIN:-$here/build/fc}
[ -x "$FC_BIN" ] || { echo "no fc binary at $FC_BIN" >&2; exit 2; }
w=${CGOTEST_KEEP:-$(mktemp -d /tmp/fccgo.XXXXXX)}
mkdir -p "$w"
[ -n "$CGOTEST_KEEP" ] || trap 'rm -rf "$w"' EXIT
cd "$here/tests/fc/cgo" || exit 2
export CGO_ENABLED=1 GOTOOLCHAIN=local GOFLAGS= GOCACHE=$w/gocache FC_RUNTIME=${FC_RUNTIME-1}
go build -o "$w/stock" . || { echo "FAIL: the stock build"; exit 1; }
go list -deps -json . > "$w/plan.json" || exit 2
FC_LINKER= FC_LINKFLAGS=-linkmode=external timeout 600 "$FC_BIN" build -plan "$w/plan.json" -o "$w/objs" -link "$w/fc" fccgo > "$w/build.log" 2>&1 \
  || { tail -30 "$w/build.log"; echo "FAIL: fc build"; exit 1; }
grep -E '^(link|back end)' "$w/build.log"
n=$(sed -n 's/^stock packages: \([0-9]*\) built by the go command.*/\1/p' "$w/build.log")
echo "stock compiler: ${n:-0} packages"
"$w/stock" > "$w/stock.out" 2>&1; rs=$?
for run in 1 2 3; do
  timeout 60 "$w/fc" > "$w/fc.out" 2>&1; rf=$?
  if [ $rs != 0 ] || [ $rf != 0 ] || ! cmp -s "$w/stock.out" "$w/fc.out"; then
    echo "FAIL (run $run): exit $rs (stock) / $rf (fc)"
    diff "$w/stock.out" "$w/fc.out" | head -40
    exit 1
  fi
done
echo "PASS ($(wc -l < "$w/fc.out") lines equal, 3 runs)"
