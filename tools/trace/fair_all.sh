#!/usr/bin/env bash
# Rung F0: plain stock and "fair" stock (the same relaxations the driver uses), 8 CPUs, cold, with the
# materialization tax (-work keeps the archives long enough to measure them). One after another.
cd "$(dirname "$0")"
M=/home/exedev/targets/minio; K=/home/exedev/targets/kubernetes
run() { systemd-run --user --scope -q -p MemoryMax=24G env "$@"; }
for t in "minio $M ." "kube-apiserver $K ./cmd/kube-apiserver"; do
  set -- $t
  run TAG=tax ./stock.sh $1 $2 $3 8 cold -work
  run TAG=fair GOGC=off ./stock.sh $1 $2 $3 8 cold -work "-gcflags=all=-dwarf=false -l" "-ldflags=-s -w"
done
echo ALLDONE
