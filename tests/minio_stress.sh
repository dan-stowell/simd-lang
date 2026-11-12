#!/usr/bin/env bash
# A heavier check of a minio binary than the smoke test: the server runs with the collector at its most
# aggressive (GOGC=5) while a few hundred objects of mixed sizes are written, read back, compared, listed
# and deleted, several at a time. Memory-safety bugs in generated code show up here as crashes.
#   tests/minio_stress.sh <minio-binary> [port] [objects=300]
set -u
bin=$(readlink -f "$1"); port=${2:-19100}; n=${3:-300}
d=$(mktemp -d /tmp/minio-stress.XXXXXX)
export MINIO_ROOT_USER=stressadmin MINIO_ROOT_PASSWORD=stresssecret123 MINIO_BROWSER=off MINIO_UPDATE=off HOME=$d
GOGC=5 "$bin" server --address 127.0.0.1:$port --quiet "$d/data" > "$d/server.log" 2>&1 &
pid=$!
fail() { echo "FAIL: $*"; grep -m3 -n 'fatal error\|panic:\|runtime:' "$d/server.log"; tail -5 "$d/server.log"; kill -9 $pid 2>/dev/null; wait $pid 2>/dev/null; cp "$d/server.log" /tmp/minio-stress-last.log; rm -rf "$d"; exit 1; }
s3() { curl -sS --fail-with-body --aws-sigv4 "aws:amz:us-east-1:s3" --user "$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD" "$@"; }
u=http://127.0.0.1:$port
for i in $(seq 1 150); do curl -s -o /dev/null "$u/minio/health/live" && break; kill -0 $pid 2>/dev/null || fail "server exited at start"; sleep 0.1; done
s3 -X PUT "$u/stress" > /dev/null || fail "make bucket"
head -c 6000000 /dev/urandom > "$d/blob"
one() {
  i=$1; sz=$(( (i * 7919) % 400000 + (i % 7 == 0 ? 3000000 : 1) ))
  head -c $sz "$d/blob" > "$d/o$i"
  s3 -T "$d/o$i" "$u/stress/dir$((i % 9))/obj$i" > /dev/null || return 1
  s3 "$u/stress/dir$((i % 9))/obj$i" -o "$d/b$i" || return 1
  cmp -s "$d/o$i" "$d/b$i" || return 1
  rm -f "$d/o$i" "$d/b$i"
}
for base in $(seq 0 8 $((n - 1))); do
  pids=()
  for j in 0 1 2 3 4 5 6 7; do one $((base + j)) & pids+=($!); done
  for p in "${pids[@]}"; do wait $p || fail "object round trip near $base"; done
  kill -0 $pid 2>/dev/null || fail "server died near object $base"
done
l=$(s3 "$u/stress?list-type=2&max-keys=1000") || fail "list"
c=$(echo "$l" | grep -o '<Key>' | wc -l)
[ "$c" -ge "$n" ] || fail "list shows $c objects, expected at least $n"
for i in $(seq 0 $((n - 1))); do s3 -X DELETE "$u/stress/dir$((i % 9))/obj$i" > /dev/null || fail "delete $i"; done
kill -0 $pid 2>/dev/null || fail "server died"
grep -q 'fatal error\|panic:' "$d/server.log" && fail "server log has a fatal error"
kill $pid; wait $pid 2>/dev/null; rm -rf "$d"
echo "PASS minio stress ($n objects written, read back, compared, listed, deleted; server at GOGC=5)"
