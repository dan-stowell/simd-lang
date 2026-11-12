#!/usr/bin/env bash
# Run tools/fc/test.sh over many packages, a few at a time; one line each: PASS, FAIL, NOFC <first reason>,
# or NOTEST. Passing packages are printed again at the end as a list.
#   tools/fc/batch.sh [-j N] [-C dir] <import paths...>      (or paths on stdin if none are given)
here=$(cd "$(dirname "$0")/../.." && pwd)
j=4; dir=.
while :; do case "$1" in -j) j=$2; shift 2;; -C) dir=$2; shift 2;; *) break;; esac; done
out=$(mktemp -d /tmp/fcbatch.XXXXXX)
one() {
  pkg=$1; f=$out/$(echo "$pkg" | tr '/ ' '__')
  timeout ${FC_TIMEOUT:-300} "$here/tools/fc/test.sh" -C "$dir" "$pkg" > "$f" 2>&1; rc=$?
  if grep -q "^fc: $pkg: " "$f"; then echo "NOFC   $pkg   $(grep -m1 "^fc: $pkg: " "$f" | cut -d: -f3- | cut -c1-110)"
  elif [ $rc = 0 ] && grep -q 'no test files' "$f"; then echo "NOTEST $pkg"
  elif [ $rc = 0 ]; then echo "PASS   $pkg"
  else echo "FAIL   $pkg   $(grep -m1 -E 'panic|FAIL:|fatal|undefined|relocation|signal|timed out|error' "$f" | cut -c1-120)  [$f]"; fi
}
export -f one; export here out dir
if [ $# -gt 0 ]; then printf '%s\n' "$@"; else cat; fi | xargs -P "$j" -I{} bash -c 'one {}' | tee "$out/summary"
echo "--- $(grep -c '^PASS' "$out/summary") pass, $(grep -c '^FAIL' "$out/summary") fail, $(grep -c '^NOFC' "$out/summary") not compiled, $(grep -c '^NOTEST' "$out/summary") without tests   (logs: $out)"
