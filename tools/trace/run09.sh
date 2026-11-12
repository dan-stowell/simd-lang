#!/usr/bin/env bash
# The current configuration, cold, instrumented:  run09.sh <minio|kube> <label> [extra sb flags...]
here=$(cd "$(dirname "$0")/../.." && pwd); t=$1; label=$2; shift 2
common=(-plan=self -decl "$here/build/godecl" -compiler "$here/build/compile-sb" -linker "$here/build/link-sb" -pipeline -shard 3000000 "-gcflags=-dwarf=false -l" "-ldflags=-s -w")
case $t in
  minio) HUGEPAGES=${HUGEPAGES:-3072} NOLEX=' ' SBENV='GOGC=off SB_BASELINE=1 SB_HUGE=1' "$here/tools/trace/sb.sh" minio /home/exedev/targets/minio . "$label" "${common[@]}" -backends 2000 "$@" ;;
  kube) HUGEPAGES=${HUGEPAGES:-5120} SBMEM=16G NOLEX=' ' SBENV='GOGC=off SB_BASELINE=1 SB_HUGE=1' "$here/tools/trace/sb.sh" kube-apiserver /home/exedev/targets/kubernetes ./cmd/kube-apiserver "$label" "${common[@]}" -backends 200 "$@" ;;
esac
