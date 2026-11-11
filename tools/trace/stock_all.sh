#!/usr/bin/env bash
# All stock builds for rung 0, one after another (they must not overlap: each owns the machine).
cd "$(dirname "$0")"
M=/home/exedev/targets/minio; K=/home/exedev/targets/kubernetes
B=/home/exedev/targets/traces
for P in 8 4 2 1; do
  [ -f $B/minio/p$P-cold/meta.txt ] || ./stock.sh minio $M . $P cold
  [ -f $B/kube-apiserver/p$P-cold/meta.txt ] || ./stock.sh kube-apiserver $K ./cmd/kube-apiserver $P cold
done
./stock.sh minio $M . 8 warm
./stock.sh kube-apiserver $K ./cmd/kube-apiserver 8 warm
# per-phase split inside the compiler and linker (separate runs: the flags perturb the build slightly)
for t in "minio $M ." "kube-apiserver $K ./cmd/kube-apiserver"; do
  set -- $t
  rm -f $B/$1.bench.txt
  TAG=bench ./stock.sh $1 $2 $3 8 warm "-gcflags=all=-bench=$B/$1.bench.txt" "-ldflags=-benchmark=cpu -benchmarkprofile=$B/$1.linkprof"
done
echo ALLDONE
