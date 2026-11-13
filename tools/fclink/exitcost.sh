#!/usr/bin/env bash
# How much of fclink's whole-process wall clock is outside its own phase table (start-up, and the kernel
# tearing the mappings down at exit)?   tools/fclink/exitcost.sh <project> [runs=5] [fclink flags...]
here=$(cd "$(dirname "$0")/../.." && pwd)
fclink=${FCLINK:-$here/build/fclinklink}; [ -x "$fclink" ] || fclink=$here/build/fclink
name=$1; runs=${2:-5}; shift; shift; d=${FC_CORPUS:-/home/exedev/targets/fc-corpus}/$name
obj=$(comm -23 <(ls "$d"/objs/*.o | sort) <(sed -n 's/^packagefile [^=]*=//p' "$d/objs/importcfg.link" | sort) | head -1)
out=${FCLINK_OUT:-/tmp/fclink-try}/bench; mkdir -p "$out"
for i in $(seq 1 "$runs"); do
  t0=$(date +%s%N)
  taskset -c "${CPUS:-0-7}" "$fclink" -stats "$@" -o "$out/$name.exit" -importcfg "$d/objs/importcfg.link" "$obj" > "$out/exit.log"
  t1=$(date +%s%N)
  awk -v w=$(( (t1 - t0) / 1000 )) '$1=="total"{printf "process %.1f ms, phases %.1f ms, outside %.1f ms\n", w/1000, $2, w/1000-$2}' "$out/exit.log"
done
