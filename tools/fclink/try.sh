#!/usr/bin/env bash
# The fclink oracle for one program: build a Go main package with the stock toolchain, link the very same
# objects again with fclink, run both binaries with the same arguments and compare output and exit status.
#   tools/fclink/try.sh <dir of a Go main package> [program arguments...]
# Environment: FCLINK (default build/fclinklink or build/fclink), GCFLAGS (default -gcflags=all=-l; set to a
# space for the default optimisations, i.e. with inlining), FCLINK_OUT (default /tmp/fclink-try), TIMEOUT (60).
# Addresses differ between the two links, so hex numbers are masked before comparing (tracebacks).
here=$(cd "$(dirname "$0")/../.." && pwd)
fclink=${FCLINK:-$here/build/fclinklink}; [ -x "$fclink" ] || fclink=$here/build/fclink
dir=$(cd "$1" && pwd) || exit 2; shift
out=${FCLINK_OUT:-/tmp/fclink-try}/$(basename "$dir"); mkdir -p "$out"; rm -f "$out/stock" "$out/new"
log=$(cd "$dir" && CGO_ENABLED=0 GOFLAGS=-buildvcs=false GOTOOLCHAIN=local go build ${GCFLAGS--gcflags=all=-l} -ldflags='-s -w' -work -x -o "$out/stock" . 2>&1)
work=$(echo "$log" | sed -n 's/^WORK=//p' | head -1)
link=$(echo "$log" | grep -m1 '/link -o ')
[ -n "$work" ] && [ -n "$link" ] || { echo "FAIL $dir: stock build failed"; echo "$log" | grep -v '^\(mkdir\|cat\|cd\|packagefile\|EOF\|#\)' | tail -20; exit 1; }
link=${link//\$WORK/$work}
cfg=$(echo "$link" | sed -n 's/.* -importcfg \([^ ]*\).*/\1/p')
id=$(echo "$link" | sed -n 's/.* -buildid=\([^ ]*\).*/\1/p')
main=${link##* }
echo "$work" > "$out/work"
if ! "$fclink" ${FCLINK_FLAGS:-} -o "$out/new" -importcfg "$cfg" -buildid="$id" "$main" > "$out/fclink.log" 2>&1; then
  echo "FAIL $dir: fclink failed"; head -20 "$out/fclink.log"; exit 1
fi
[ -s "$out/fclink.log" ] && cat "$out/fclink.log"
mask() { sed -E 's/0x[0-9a-f]+/0xX/g; s/goroutine [0-9]+/goroutine N/g'; }
(cd "$dir" && timeout "${TIMEOUT:-60}" "$out/stock" "$@" > "$out/stock.out" 2>&1; echo "exit $?" >> "$out/stock.out")
(cd "$dir" && timeout "${TIMEOUT:-60}" "$out/new" "$@" > "$out/new.out" 2>&1; echo "exit $?" >> "$out/new.out")
if cmp -s <(mask < "$out/stock.out") <(mask < "$out/new.out"); then
  echo "PASS $(basename "$dir"): same output ($(wc -l < "$out/new.out") lines, $(tail -1 "$out/new.out")); stock $(stat -c %s "$out/stock") bytes, fclink $(stat -c %s "$out/new") bytes"
  [ -n "${KEEP_WORK:-}" ] || rm -rf "$work"
else
  echo "FAIL $(basename "$dir"): output differs (stock | fclink); objects kept in $work"
  diff <(mask < "$out/stock.out") <(mask < "$out/new.out") | head -${DIFF_LINES:-30}
  exit 1
fi
