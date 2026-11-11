#!/usr/bin/env bash
# A/B timing on a drifting host: run configurations in turn, N rounds, page cache warm (these are
# compile-CPU experiments), print wall clock and compile core-seconds of every run, then smoke-test the
# last binary of each.     ab.sh <rounds> <gcflags A> <gcflags B> [<gcflags C> ...]
# Common flags are in SBCOMMON (default: the current MinIO configuration); SBENV as in sb.sh.
set -u
here=$(cd "$(dirname "$0")/../.." && pwd)
n=$1; shift
M=${ABSRC:-/home/exedev/targets/minio}
common=(-plan=self -decl "$here/build/godecl" -compiler "${ABCOMPILER:-$here/build/compile-sb}" -pipeline -backends 2000 "-ldflags=-s -w")
for r in $(seq 1 "$n"); do
  i=0
  for g in "$@"; do
    i=$((i+1))
    out=$(systemd-run --user --scope -q -p MemoryMax=22G env ${SBENV:-GOGC=off} "$here/build/sb" -C "$M" -o /dev/shm/ab$i.bin "${common[@]}" "-gcflags=${ABGC:--dwarf=false -l}" $g 2>&1)
    echo "round $r  config $i  total $(echo "$out" | awk '/^TOTAL/{print $2}') ms  compile $(echo "$out" | awk '/^compile/{print $5, "ms wall", $8/1000, "core-s"}')  bin $(stat -c %s /dev/shm/ab$i.bin)"
  done
done
i=0
for g in "$@"; do i=$((i+1)); echo "config $i [$g]: $("$here/tests/minio_smoke.sh" /dev/shm/ab$i.bin | tail -1)"; rm -f /dev/shm/ab$i.bin; done
