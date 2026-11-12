#!/usr/bin/env bash
# The standard library's own tests as the oracle for the baseline code generator: build and run them with
# the patched compiler, SB_BASELINE on. Any function the baseline takes in these packages is then exercised
# by tests written by people who did not have it in mind.
#   tests/sb_stdtest.sh [packages...]      (default: a set of pure-computation packages)
# Environment: SB_BASELINE_HASH=k/n and SB_BASELINE_ONLY=substr narrow which functions the baseline takes.
set -u
cd "$(dirname "$0")/.."
G=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
cp build/compile-sb "$G/pkg/tool/linux_amd64/compile"
pkgs=("$@")
[ ${#pkgs[@]} -gt 0 ] || pkgs=(strconv strings bytes sort slices unicode/utf8 unicode/utf16 container/list container/heap container/ring
  encoding/binary encoding/hex encoding/base64 hash/crc32 hash/fnv hash/adler32 path bufio text/tabwriter index/suffixarray
  compress/flate compress/gzip encoding/json go/scanner go/parser go/printer regexp text/template html)
log=$(mktemp -d /dev/shm/sbstd.XXXXXX)
cache=$(mktemp -d /dev/shm/sbstdcache.XXXXXX)
(cd "$G/src" && systemd-run --user --scope -q -p MemoryMax=14G env GOROOT="$G" GOTOOLCHAIN=local GOFLAGS= GOCACHE="$cache" \
   SB_BASELINE=1 SB_BASELINE_LOG="$log" "$G/bin/go" test -short ${SB_TESTARGS:--count=1} -p 4 "-gcflags=all=${SB_GCFLAGS:--dwarf=false -l}" -ldflags=-w "${pkgs[@]}" 2>&1) | tee "$log/test.out" | grep -v '^ok ' | head -${SB_SHOW:-40}
ok=$(grep -c '^ok ' "$log/test.out"); bad=$(grep -c '^FAIL\|^---\|panic:' "$log/test.out")
b=$(cat "$log"/[0-9]*.txt 2>/dev/null | grep -c '	baseline$'); s=$(cat "$log"/[0-9]*.txt 2>/dev/null | grep -c '	ssa	')
echo "packages ok: $ok of ${#pkgs[@]}; functions compiled by the baseline: $b, sent to SSA: $s"
cat "$log"/[0-9]*.txt 2>/dev/null | grep '	ssa	' | cut -f3 | sed 's/ of type .*//; s/ [^ ]*\.[^ ]*$//' | sort | uniq -c | sort -rn | head -${SB_WHY:-25}
rm -rf "$cache"; [ -n "${SB_KEEPLOG:-}" ] && echo "log kept in $log" || rm -rf "$log"
[ "$ok" = "${#pkgs[@]}" ] && [ "$bad" = 0 ]
