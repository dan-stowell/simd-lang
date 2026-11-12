#!/usr/bin/env bash
# A/B of driver flags on the current MinIO configuration (huge pages, baseline, shards), interleaved,
# warm page cache:    abd.sh <rounds> '<extra sb flags A>' '<extra sb flags B>' ...
# Prints wall, compile wall and core-s, minor faults of every run; smoke-tests the last binary of each.
set -u
here=$(cd "$(dirname "$0")/../.." && pwd)
n=$1; shift
M=${ABSRC:-/home/exedev/targets/minio}
hp0=$(cat /proc/sys/vm/nr_hugepages); sudo sysctl -q -w vm.nr_hugepages=${HUGEPAGES:-3072}
trap 'sudo sysctl -q -w vm.nr_hugepages=$hp0' EXIT
common=(-plan=self -decl "$here/build/godecl" -compiler "${ABCOMPILER:-$here/build/compile-sb}" -linker "$here/build/link-sb" -pipeline -shard 3000000 -backends 2000 "-gcflags=-dwarf=false -l" "-ldflags=-s -w")
for r in $(seq 1 "$n"); do
  # Even rounds run the configurations in reverse order (ABBA): on a host that drifts, a fixed order
  # credits the drift to whichever configuration runs second (JOURNAL 2026-11-12, iteration 20).
  order=$(seq 1 $#); [ $((r % 2)) = 0 ] && order=$(seq $# -1 1)
  for i in $order; do
    g=${!i}
    out=$(systemd-run --user --scope -q -p MemoryMax=22G env ${SBENV:-GOGC=off SB_BASELINE=1 SB_HUGE=1} "$here/build/sb" -C "$M" -o /dev/shm/abd$i.bin "${common[@]}" $g 2>&1)
    echo "round $r  config $i  total $(echo "$out" | awk '/^TOTAL/{print $2}') ms  compile $(echo "$out" | awk '/^compile/{print $5, "ms wall", $8/1000, "core-s"}')  $(echo "$out" | grep '^tax' | cut -d, -f1-3)"
  done
done
i=0
for g in "$@"; do i=$((i+1)); echo "config $i [$g]: $("$here/tests/minio_smoke.sh" /dev/shm/abd$i.bin | tail -1)"; rm -f /dev/shm/abd$i.bin; done
