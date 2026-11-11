#!/usr/bin/env bash
# Smoke test of a kube-apiserver binary: start etcd and the server, wait for /healthz to say ok, then
# create, read back and delete a ConfigMap through the API.   tests/kube_smoke.sh <kube-apiserver> [port]
# Needs an etcd binary (ETCD, default /home/exedev/targets/etcd: the release Kubernetes pins in hack/lib/etcd.sh).
set -u
bin=$(readlink -f "$1"); port=${2:-16443}; etcd=${ETCD:-/home/exedev/targets/etcd}
d=$(mktemp -d /tmp/kube-smoke.XXXXXX)
"$etcd" --data-dir "$d/etcd" --listen-client-urls http://127.0.0.1:12379 --advertise-client-urls http://127.0.0.1:12379 \
  --listen-peer-urls http://127.0.0.1:12380 > "$d/etcd.log" 2>&1 &
epid=$!
echo "smoketoken,smoke,1,system:masters" > "$d/tokens"
openssl genrsa -out "$d/sa.key" 2048 2>/dev/null; openssl rsa -in "$d/sa.key" -pubout -out "$d/sa.pub" 2>/dev/null
"$bin" --etcd-servers=http://127.0.0.1:12379 --secure-port=$port --bind-address=127.0.0.1 --cert-dir="$d/certs" \
  --service-account-key-file="$d/sa.pub" --service-account-signing-key-file="$d/sa.key" \
  --service-account-issuer=https://kubernetes.default.svc.cluster.local \
  --authorization-mode=AlwaysAllow --token-auth-file="$d/tokens" --service-cluster-ip-range=10.0.0.0/24 > "$d/api.log" 2>&1 &
pid=$!
stop() { kill -9 $pid $epid 2>/dev/null; wait $pid $epid 2>/dev/null; }   # no graceful shutdown: that takes a minute
fail() { echo "FAIL: $*"; tail -5 "$d/api.log" | cut -c1-300; curl -sk -H "Authorization: Bearer smoketoken" -m 5 -i "$u/healthz" | head -12; stop; rm -rf "$d"; exit 1; }
u=https://127.0.0.1:$port
ok=
for i in $(seq 1 60); do
  [ "$(curl -sk -H "Authorization: Bearer smoketoken" "$u/healthz")" = ok ] && { ok=1; break; }
  kill -0 $pid 2>/dev/null || fail "server exited"
  sleep 0.2
done
[ -n "$ok" ] || fail "/healthz never said ok: $(curl -sk -H "Authorization: Bearer smoketoken" "$u/healthz" | head -c 300)"
j='{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"smoke"},"data":{"k":"strand"}}'
curl -sk -H "Authorization: Bearer smoketoken" -X POST -H 'Content-Type: application/json' -d "$j" "$u/api/v1/namespaces/default/configmaps" | grep -q '"k": "strand"' || fail "create configmap"
curl -sk -H "Authorization: Bearer smoketoken" "$u/api/v1/namespaces/default/configmaps/smoke" | grep -q '"k": "strand"' || fail "get configmap"
curl -sk -H "Authorization: Bearer smoketoken" -X DELETE "$u/api/v1/namespaces/default/configmaps/smoke" | grep -q '"kind"' || fail "delete configmap"
[ "$(curl -sk -H "Authorization: Bearer smoketoken" -o /dev/null -w '%{http_code}' "$u/api/v1/namespaces/default/configmaps/smoke")" = 404 ] || fail "configmap still there"
stop; rm -rf "$d"
echo "PASS kube-apiserver smoke (/healthz ok; ConfigMap create/get/delete)"
