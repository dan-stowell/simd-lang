#!/usr/bin/env bash
# One cold stock-toolchain build, instrumented.
#   stock.sh <name> <srcdir> <pkg> <P> <cold|warm> [extra go build flags...]
# Writes $TRACES/<name>/p<P>-<mode>[-tag]/ {trace.json, actions.json, meta.txt, cpu.txt, build.log}
# cold  = fresh GOCACHE and dropped page cache;  warm = fresh GOCACHE, page cache left as is.
set -u
name=$1; src=$2; pkg=$3; P=$4; mode=$5; shift 5
TRACES=${TRACES:-/home/exedev/targets/traces}
out=$TRACES/$name/p$P-$mode${TAG:+-$TAG}
rm -rf "$out"; mkdir -p "$out"
export GOTOOLCHAIN=local CGO_ENABLED=0 GOFLAGS=-trimpath
export GOCACHE; GOCACHE=$(mktemp -d /home/exedev/targets/gocache.XXXXXX)
cpus=0-$((P-1))
here=$(cd "$(dirname "$0")" && pwd); set -- "$@"; cd "$src" || exit 1
sync
if [ "$mode" = cold ]; then echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null; fi
disk() { awk '$3=="vda"{print $6*512, $10*512}' /proc/diskstats; }   # bytes read, bytes written
read r0 w0 < <(disk)
forks() { awk '$1=="processes"{print $2}' /proc/stat; }; faults() { awk '$1=="pgfault"{print $2}' /proc/vmstat; }
f0=$(forks); pf0=$(faults)
# sample whole-machine CPU every 100 ms (the machine is otherwise idle)
( while :; do echo "$(date +%s.%N) $(head -1 /proc/stat)"; sleep 0.1; done ) > "$out/cpu.txt" &
sampler=$!
t0=$(date +%s.%N)
taskset -c "$cpus" /usr/bin/time -f 'user_s %U\nsys_s %S\nwall_s %e\nmaxrss_kb %M\nmajfault %F\nminflt %R' -o "$out/time.txt" \
  go build -p "$P" -debug-trace="$out/trace.json" -debug-actiongraph="$out/actions.json" \
  "$@" -o "$out/bin" "$pkg" > "$out/build.log" 2>&1
rc=$?
t1=$(date +%s.%N)
kill $sampler 2>/dev/null
sync
read r1 w1 < <(disk)
{
  echo "name $name"; echo "P $P"; echo "mode $mode"; echo "rc $rc"
  echo "t0 $t0"; echo "t1 $t1"
  echo "wall_ms $(echo "($t1 - $t0) * 1000 / 1" | bc)"
  cat "$out/time.txt"
  echo "disk_read_bytes $((r1 - r0))"; echo "disk_written_bytes $((w1 - w0))"
  echo "bin_bytes $(stat -c %s "$out/bin" 2>/dev/null || echo 0)"
  echo "forks $(( $(forks) - f0 ))"; echo "pgfault_system $(( $(faults) - pf0 ))"
  wd=$(grep -m1 '^WORK=' "$out/build.log" | cut -d= -f2)
  [ -z "$wd" ] || { python3 "$here/tax.py" "$out/actions.json"; rm -rf "$wd"; }
  echo "gocache_bytes $(du -sb "$GOCACHE" | cut -f1)"
  echo "go $(go version)"
  echo "commit $(git rev-parse HEAD)"
} > "$out/meta.txt"
rm -rf "$GOCACHE"
cat "$out/meta.txt"
exit $rc
