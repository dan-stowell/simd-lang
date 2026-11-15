#!/usr/bin/env bash
# The stock compiler's //go:nowritebarrierrec proof, redone on fc's runtime (driver/fc/wbcheck.go): build a small
# program with FC_WBCHECK=1 and fail if fc emitted a write barrier in any function reached from a function the
# runtime marks //go:nowritebarrierrec. (Such a barrier deadlocked an fc-built protoc-gen-go on the heap lock about
# once in 150 runs at GOGC=1: JOURNAL 2026-11-15.)       tools/fc/wbcheck.sh
here=$(cd "$(dirname "$0")/../.." && pwd); tmp=$(mktemp -d)
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false FC_WBCHECK=1
[ -n "${FC_LINKER+x}" ] || export FC_LINKER=$here/build/fclink
"${FC_BIN:-$here/build/fc}" build -C "$here/tools/fclink/progs" -o "$tmp/objs" -link "$tmp/bin" ./gcstress > "$tmp/log" 2>&1
grep 'wbcheck' "$tmp/log" | cut -c1-300
if grep -q 'wbcheck: runtime: [0-9]* functions reached from //go:nowritebarrierrec, 0 with write barriers' "$tmp/log" && "$tmp/bin" > /dev/null 2>&1; then
  echo PASS; rc=0
else
  echo FAIL; tail -5 "$tmp/log"; rc=1
fi
rm -r "$tmp"; exit $rc
