#!/usr/bin/env bash
# One number for a day's work: the configuration at the end of 2026-11-11 (`08-huge-shard`: exec per
# compile, multi-pass assembler, per-function DWARF, speculative *T descriptors, binary copied from
# tmpfs) against the current one, cold, order-balanced (old new new old ...).
#   day.sh <pairs> [minio|kube]      prints every run and the means
here=$(cd "$(dirname "$0")/../.." && pwd); n=${1:-2}; tgt=${2:-minio}
for i in $(seq 1 "$n"); do
  for cfg in old new new old; do
    if [ $cfg = old ]; then
      SB_COMPILE_ENV=SB_ASM_MULTIPASS=1,SB_KEEP_WEAKPTR=1,SB_NODWARFSYMS=0 TRACES=/home/exedev/targets/traces-scratch "$here/tools/trace/run09.sh" $tgt day-old 2>&1
    else
      TRACES=/home/exedev/targets/traces-scratch "$here/tools/trace/run09.sh" $tgt day-new -zygote -headstart -directout 2>&1
    fi | awk -v c=$cfg '/^compile/{cw=$5; cc=$8; ob=$10} /^link/{l=$5} /^write/{w=$3} /^wall_ms/{t=$2} /^smoke/{s=$2} END{print c, t, cc/1000, l+w, int(ob/1e6), s}'
  done
done | tee /dev/shm/day.txt
awk '{n[$1]++; t[$1]+=$2; c[$1]+=$3; l[$1]+=$4; o[$1]=$5} END{for(k in n) printf "%s: mean wall %.0f ms, compile %.1f core-s, link+write %.0f ms, objects %d MB (n=%d)\n", k, t[k]/n[k], c[k]/n[k], l[k]/n[k], o[k], n[k]}' /dev/shm/day.txt
