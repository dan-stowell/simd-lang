#!/usr/bin/env bash
# Reliability and spread of the current configuration: n cold builds in a row, each one smoke-tested.
#   soak.sh <minio|kube> <n>     prints one line per build, then min / median / max of the wall clock
here=$(cd "$(dirname "$0")/../.." && pwd); t=$1; n=${2:-20}
for i in $(seq 1 "$n"); do
  TRACES=/home/exedev/targets/traces-scratch "$here/tools/trace/run09.sh" "$t" soak -zygote -headstart -directout 2>&1 |
    awk -v i=$i '/^compile/{cc=$8} /^wall_ms/{t=$2} /^rc /{rc=$2} /^smoke/{s=$2} END{print i, t, cc/1000, "rc=" rc, (s == "" ? "NOSMOKE" : s)}'
done | tee /dev/shm/soak-$t.txt
sort -k2 -n /dev/shm/soak-$t.txt | awk -v n=$n '{w[NR]=$2; if ($5 == "PASS") ok++} END{printf "%d builds, %d pass; wall min %d, median %d, max %d ms\n", NR, ok, w[1], w[int((NR+1)/2)], w[NR]}'
