#!/usr/bin/env bash
# Review runner: generated inputs, never alters repository implementation/tests.
set -euo pipefail
here=$(realpath "$(dirname "$0")")
work=${1:?usage: run.sh work-directory private-GOROOT}
g=${2:?private GOROOT containing build/compile-sb}
mkdir -p "$work"
work=$(realpath "$work")
python3 "$here/generate.py" "$work/inputs"
for mode in stock keep skip; do
  root=$g; flags=()
  case $mode in
    stock) root=$(/usr/local/go/bin/go env GOROOT);;
    keep) flags=(SB_BASELINE=1 SB_KEEP_WEAKPTR=1);;
    skip) flags=(SB_BASELINE=1);;
  esac
  env -u SB_BASELINE -u SB_KEEP_WEAKPTR GOROOT="$root" GOTOOLCHAIN=local GOFLAGS= GOCACHE="$work/cache-$mode" "${flags[@]}" \
    "$root/bin/go" -C "$work/inputs" build '-gcflags=all=-dwarf=false -l' -ldflags=-w -o "$work/$mode" . >"$work/$mode.build.log" 2>&1
  "$work/$mode" >"$work/$mode.out" 2>"$work/$mode.err"
  cat "$work/$mode.out"
done
cmp "$work/stock.out" "$work/keep.out"
cmp "$work/stock.out" "$work/skip.out"
echo 'PASS: successful builds/executions and identical stdout in all three modes'
for pkg in p q; do
  mode=mixed-$pkg
  env -u SB_KEEP_WEAKPTR SB_BASELINE=1 STRAND_KEEP_PACKAGE=ptrprobe/$pkg GOROOT="$g" GOTOOLCHAIN=local GOFLAGS= GOCACHE="$work/cache-$mode" \
    "$g/bin/go" -C "$work/inputs" build -toolexec="$here/mixed-tool.sh" '-gcflags=all=-dwarf=false -l' -ldflags=-w -o "$work/$mode" . >"$work/$mode.build.log" 2>&1
  "$work/$mode" >"$work/$mode.out" 2>"$work/$mode.err"
  cmp "$work/stock.out" "$work/$mode.out"
  echo "PASS mixed descriptors: retain in $pkg, skip in other packages"
done
