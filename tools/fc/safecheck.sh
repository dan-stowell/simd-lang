#!/usr/bin/env bash
# No safepoint where the stock build has none: for every //go:nosplit function of the runtime's packages, what can
# stop inside it in fc's build of the runtime's test binary (a callee with a stack check, or one a signal may
# preempt) against the stock build of the same binary, where the helpers are inlined; and the deepest nosplit chain
# against the stack guard (tools/fc/safecheck.py; JOURNAL 2026-11-15).        tools/fc/safecheck.sh
# The stock side (go test -c runtime, disassembled) is kept in $SAFECHECK_DIR (default targets/fc-safecheck).
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
d=${SAFECHECK_DIR:-$T/fc-safecheck}; mkdir -p "$d"
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false
ver=$(go env GOVERSION); src=$(go env GOROOT)/src
if [ ! -s "$d/runtime.$ver.dis" ]; then
  (cd "$src" && GOCACHE=$d/gocache go test -c -o "$d/runtime.$ver.test" runtime) || exit 2
  go tool objdump "$d/runtime.$ver.test" > "$d/runtime.$ver.dis" || exit 2
fi
tmp=$(mktemp -d)
FC_CALLGRAPH=$tmp/graph WPTEST_KEEP=$tmp/wp "$here/tools/fc/wptest.sh" -C "$src" runtime -count=1 -run XXXNONE > "$tmp/log" 2>&1 || { tail -5 "$tmp/log"; rm -r "$tmp"; echo FAIL; exit 1; }
python3 "$here/tools/fc/safecheck.py" "$tmp/graph" "$d/runtime.$ver.dis" ${SAFECHECK_V:+v} | cut -c1-600; rc=${PIPESTATUS[0]}
rm -r "$tmp"
[ "$rc" = 0 ] && echo PASS || echo FAIL
exit "$rc"
