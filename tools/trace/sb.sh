#!/usr/bin/env bash
# One cold build with the driver (build/sb), instrumented like stock.sh, then the target's smoke test.
#   sb.sh <name> <srcdir> <pkg> <label> [sb flags...]     -> $TRACES/<name>/sb-<label>/
# WARM=1 leaves the page cache alone.
set -u
name=$1; src=$2; pkg=$3; label=$4; shift 4
here=$(cd "$(dirname "$0")/../.." && pwd)
TRACES=${TRACES:-/home/exedev/targets/traces}
out=$TRACES/$name/sb-$label${WARM:+-warm}
rm -rf "$out"; mkdir -p "$out"
sync
[ -n "${WARM:-}" ] || echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null
disk() { awk '$3=="vda"{print $6*512, $10*512}' /proc/diskstats; }
read r0 w0 < <(disk)
( while :; do echo "$(date +%s.%N) $(head -1 /proc/stat)"; sleep 0.1; done ) > "$out/cpu.txt" &
sampler=$!
t0=$(date +%s.%N)
/usr/bin/time -f 'user_s %U\nsys_s %S\nwall_s %e\nmaxrss_kb %M' -o "$out/time.txt" \
  "$here/build/sb" -C "$src" -o "$out/bin" -lexer "$here/build/golex_strand" -trace "$out/trace.json" "$@" "$pkg" > "$out/build.log" 2>&1
rc=$?
t1=$(date +%s.%N)
kill $sampler 2>/dev/null
sync
read r1 w1 < <(disk)
{
  echo "name $name"; echo "label $label"; echo "rc $rc"; echo "flags $*"
  echo "t0 $t0"; echo "t1 $t1"
  echo "wall_ms $(echo "($t1 - $t0) * 1000 / 1" | bc)"
  cat "$out/time.txt"
  echo "disk_read_bytes $((r1 - r0))"; echo "disk_written_bytes $((w1 - w0))"
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
cat "$out/build.log" "$out/meta.txt"
exit $rc
