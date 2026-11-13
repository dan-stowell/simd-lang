#!/usr/bin/env bash
# tests/kube_smoke.sh with its etcd on other ports, so that two smoke tests can run on one machine.
#   tools/fclink/kube_smoke.sh <kube-apiserver> [port] [etcd client port] [etcd peer port]
here=$(cd "$(dirname "$0")/../.." && pwd)
sed "s/12379/${3:-22379}/g; s/12380/${4:-22380}/g" "$here/tests/kube_smoke.sh" | bash -s -- "$1" "${2:-26443}"
