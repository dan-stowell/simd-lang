#!/usr/bin/env bash
# tools/fc/wptest.sh over many packages, a few at a time; one line each: PASS, FAIL, NOFC <first reason>, NOTEST.
#   tools/fc/wpbatch.sh [-j N] [-C dir] <import paths...>      (or paths on stdin if none are given)
# FC_TIMEOUT: seconds per package (default 300; the runtime's own suite gets 1500). Logs: the directory printed last.
here=$(cd "$(dirname "$0")/../.." && pwd)
j=2; dir=.
while :; do case "$1" in -j) j=$2; shift 2;; -C) dir=$2; shift 2;; *) break;; esac; done
out=$(mktemp -d /tmp/fcwpbatch.XXXXXX)
one() {
  pkg=$1; f=$out/$(echo "$pkg" | tr '/ ' '__')
  t=${FC_TIMEOUT:-300}; [ "$pkg" = runtime ] && t=1500
  timeout -k 10 "$t" "$here/tools/fc/wptest.sh" -C "$dir" "$pkg" -count=1 > "$f" 2>&1; rc=$?
  case $rc in
    0) echo "PASS   $pkg";;
    3) echo "NOFC   $pkg   $(grep -m1 '^fc: ' "$f" | cut -c1-140)";;
    4) echo "NOTEST $pkg";;
    124|137) echo "FAIL   $pkg   (timeout ${t}s)  [$f]";;
    *) echo "FAIL   $pkg   $(grep -m1 -E 'panic|--- FAIL|fatal|undefined|relocation|signal|timed out|error' "$f" | cut -c1-120)  [$f]";;
  esac
}
export -f one; export here out dir
if [ $# -gt 0 ]; then printf '%s\n' "$@"; else cat; fi | xargs -P "$j" -I{} bash -c 'one {}' | tee "$out/summary"
echo "--- $(grep -c '^PASS' "$out/summary") pass, $(grep -c '^FAIL' "$out/summary") fail, $(grep -c '^NOFC' "$out/summary") not compiled, $(grep -c '^NOTEST' "$out/summary") without tests   (logs: $out)"
