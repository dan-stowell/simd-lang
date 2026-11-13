#!/usr/bin/env bash
# fclink against `go tool link` on one corpus project's objects (left by tools/fc/corpus.sh), interleaved:
# each round links once with each, in alternating order; reports min and median wall and CPU per linker,
# and fclink's phase table from its fastest run.
#   tools/fclink/bench.sh <project> [rounds=7]        CPUS=0-7 (taskset), FC_CORPUS, FCLINK
here=$(cd "$(dirname "$0")/../.." && pwd)
fclink=${FCLINK:-$here/build/fclinklink}; [ -x "$fclink" ] || fclink=$here/build/fclink
name=$1; rounds=${2:-7}; d=${FC_CORPUS:-/home/exedev/targets/fc-corpus}/$name; cpus=${CPUS:-0-7}
# the main package's object: the one object the importcfg does not list
obj=$(comm -23 <(ls "$d"/objs/*.o | sort) <(sed -n 's/^packagefile [^=]*=//p' "$d/objs/importcfg.link" | sort) | head -1)
out=${FCLINK_OUT:-/tmp/fclink-try}/bench; mkdir -p "$out"
cfg=$d/objs/importcfg.link
[ -f "$obj" ] || { echo "no main object in $d/objs"; exit 1; }
tm() { # tm <label> cmd...: appends "wall cpu" (seconds) to $out/<label>.times
  local label=$1; shift
  /usr/bin/time -f "%e %U %S" -o "$out/t" taskset -c "$cpus" "$@" > "$out/$label.log" 2>&1 || { echo "$label failed"; cat "$out/$label.log" | head; exit 1; }
  awk '{printf "%.3f %.3f\n", $1, $2+$3}' "$out/t" >> "$out/$label.times"
}
rm -f "$out"/*.times "$out/best.stats"; best=999999
for r in $(seq 1 "$rounds"); do
  order="fclink stock"; [ $((r % 2)) = 0 ] && order="stock fclink"
  for w in $order; do
    if [ $w = stock ]; then
      tm stock go tool link -o "$out/$name.stock" -importcfg "$cfg" -buildmode=exe -s -w "$obj"
    else
      tm fclink "$fclink" -s -stats -o "$out/$name.fclink" -importcfg "$cfg" "$obj"
      t=$(awk '$1=="total"{print int($2*1000)}' "$out/fclink.log")
      [ "$t" -lt "$best" ] && { best=$t; cp "$out/fclink.log" "$out/best.stats"; }
    fi
  done
done
for w in fclink stock; do
  sort -n "$out/$w.times" | awk -v w=$w -v n="$rounds" '{a[NR]=$1; c[NR]=$2} END {printf "%-7s wall min %.3f s, median %.3f s", w, a[1], a[int((NR+1)/2)]}'
  sort -k2 -n "$out/$w.times" | awk '{c[NR]=$2} END {printf "; cpu min %.3f core-s, median %.3f core-s (%d runs)\n", c[1], c[int((NR+1)/2)], NR}'
done
echo "sizes: fclink $(stat -c %s "$out/$name.fclink"), stock $(stat -c %s "$out/$name.stock") bytes; cpus $cpus (whole process, /usr/bin/time)"
echo "fclink's fastest run by its own clock:"; cat "$out/best.stats"
