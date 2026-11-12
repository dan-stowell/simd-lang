#!/usr/bin/env bash
# MinIO's own unit tests, compiled by the patched compiler with the baseline code generator and the
# build's flags, and (second argument "stock") by the same toolchain with the baseline off, for comparison.
#   tests/minio_unit.sh <outdir> [stock] [packages...]      default packages: ./...
# Slow (the cmd package alone takes many minutes). Memory-capped; -short; run it when nothing is being timed.
set -u
out=$1; shift; mode=baseline
if [ "${1:-}" = stock ]; then mode=stock; shift; fi
pk=("$@"); [ ${#pk[@]} -gt 0 ] || pk=(./...)
G=/home/exedev/targets/goroot-sb; M=/home/exedev/targets/minio
mkdir -p "$out"
cp "$(dirname "$0")/../build/compile-sb" "$G/pkg/tool/linux_amd64/compile"
env=(GOROOT=$G GOTOOLCHAIN=local CGO_ENABLED=0 GOCACHE=$out/cache-$mode GOFLAGS=-mod=mod)
[ $mode = baseline ] && env+=(SB_BASELINE=1 SB_BASELINE_LOG=$out/log-$mode) && mkdir -p "$out/log-$mode"
cd "$M" && systemd-run --user --scope -q -p MemoryMax=${MINIO_UNIT_MEM:-12G} nice -n 10 env "${env[@]}" \
  "$G/bin/go" test -short -count=1 -p ${MINIO_UNIT_P:-3} -timeout 60m "-gcflags=all=-dwarf=false -l" -ldflags=-w "${pk[@]}" > "$out/$mode.out" 2>&1
echo "exit $?" >> "$out/$mode.out"
grep -E '^(ok|FAIL|---|\?|panic|exit)' "$out/$mode.out" | awk '{print $1}' | sort | uniq -c
