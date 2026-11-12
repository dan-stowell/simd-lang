#!/usr/bin/env bash
# One cold build with the driver (build/sb), instrumented like stock.sh, then the target's smoke test.
#   sb.sh <name> <srcdir> <pkg> <label> [sb flags...]     -> $TRACES/<name>/sb-<label>/
# WARM=1 leaves the page cache alone. HUGEPAGES=n reserves a pool of 2 MB pages for the run. NOLEX=' ' leaves out the token pass (nothing consumes it). SBENV='GOGC=off' sets the environment of every tool. The build runs in a
# memory-limited scope (SBMEM, default 22G) so that a runaway cannot take the machine down.
set -u
name=$1; src=$2; pkg=$3; label=$4; shift 4
here=$(cd "$(dirname "$0")/../.." && pwd)
TRACES=${TRACES:-/home/exedev/targets/traces}
out=$TRACES/$name/sb-$label${WARM:+-warm}
rm -rf "$out"; mkdir -p "$out"
# HUGEPAGES=n: reserve n explicit 2 MB pages for the run (the tools take their heaps from the pool when SB_HUGE
# is set; this kernel has no transparent huge pages) and put the setting back afterwards.
hp0=$(cat /proc/sys/vm/nr_hugepages)
[ -z "${HUGEPAGES:-}" ] || sudo sysctl -q -w vm.nr_hugepages=$HUGEPAGES
trap 'sudo sysctl -q -w vm.nr_hugepages=$hp0' EXIT
sync
[ -n "${WARM:-}" ] || echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null
disk() { awk '$3=="vda"{print $6*512, $10*512}' /proc/diskstats; }
read r0 w0 < <(disk)
forks() { awk '$1=="processes"{print $2}' /proc/stat; }; faults() { awk '$1=="pgfault"{print $2}' /proc/vmstat; }
f0=$(forks); pf0=$(faults)
( while :; do echo "$(date +%s.%N) $(head -1 /proc/stat)"; sleep 0.1; done ) > "$out/cpu.txt" &
sampler=$!
t0=$(date +%s.%N)
/usr/bin/time -f 'user_s %U\nsys_s %S\nwall_s %e\nmaxrss_kb %M' -o "$out/time.txt" \
  systemd-run --user --scope -q -p MemoryMax=${SBMEM:-22G} env ${SBENV:-SB=1} "$here/build/sb" -C "$src" -o "$out/bin" ${NOLEX:--lexer "$here/build/golex_strand"} -trace "$out/trace.json" "$@" "$pkg" > "$out/build.log" 2>&1
rc=$?
t1=$(date +%s.%N)
f1=$(forks); pf1=$(faults)
kill $sampler 2>/dev/null
sync
read r1 w1 < <(disk)
{
  echo "name $name"; echo "label $label"; echo "rc $rc"; echo "flags ${SBENV:+$SBENV }$*" | sed "s|$here/||g"
  echo "hugepages $(cat /proc/sys/vm/nr_hugepages)"
  echo "t0 $t0"; echo "t1 $t1"
  echo "wall_ms $(echo "($t1 - $t0) * 1000 / 1" | bc)"
  cat "$out/time.txt"
  echo "disk_read_bytes $((r1 - r0))"; echo "disk_written_bytes $((w1 - w0))"
  echo "forks $((f1 - f0))"; echo "pgfault_system $((pf1 - pf0))"
  echo "bin_bytes $(stat -c %s "$out/bin" 2>/dev/null || echo 0)"
  echo "commit $(git -C "$src" rev-parse HEAD)"
  echo "driver $(git -C "$here" rev-parse --short HEAD)"
} > "$out/meta.txt"
if [ $rc = 0 ]; then
  case $name in
    minio) "$here/tests/minio_smoke.sh" "$out/bin" > "$out/smoke.txt" 2>&1; echo "smoke $(tail -1 "$out/smoke.txt")" >> "$out/meta.txt" ;;
    kube-apiserver) "$here/tests/kube_smoke.sh" "$out/bin" > "$out/smoke.txt" 2>&1; echo "smoke $(tail -1 "$out/smoke.txt")" >> "$out/meta.txt" ;;
  esac
fi
rm -f "$out/bin"
# REPS=n: n-1 more cold runs of the same build, wall clock only, so the trace can show the spread
walls=$(grep '^wall_ms' "$out/meta.txt" | cut -d' ' -f2)
for i in $(seq 2 ${REPS:-1}); do
  sync; [ -n "${WARM:-}" ] || echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null
  a=$(date +%s.%N)
  systemd-run --user --scope -q -p MemoryMax=${SBMEM:-22G} env ${SBENV:-SB=1} "$here/build/sb" -C "$src" -o "$out/bin" ${NOLEX:--lexer "$here/build/golex_strand"} "$@" "$pkg" > /dev/null 2>&1 || walls="$walls FAILED"
  b=$(date +%s.%N); rm -f "$out/bin"
  walls="$walls $(echo "($b - $a) * 1000 / 1" | bc)"
done
echo "walls $walls" >> "$out/meta.txt"
cat "$out/build.log" "$out/meta.txt"
exit $rc
