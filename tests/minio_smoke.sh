#!/usr/bin/env bash
# S3 smoke test of a minio binary: start the server, then put / get / list / delete through the S3 API
# (curl signs the requests with AWS SigV4).   tests/minio_smoke.sh <minio-binary> [port]
set -u
bin=$(readlink -f "$1"); port=${2:-19000}
d=$(mktemp -d /tmp/minio-smoke.XXXXXX)
export MINIO_ROOT_USER=smokeadmin MINIO_ROOT_PASSWORD=smokesecret123 MINIO_BROWSER=off MINIO_UPDATE=off HOME=$d
"$bin" server --address 127.0.0.1:$port --quiet "$d/data" > "$d/server.log" 2>&1 &
pid=$!
fail() { echo "FAIL: $*"; tail -20 "$d/server.log"; kill $pid 2>/dev/null; wait $pid 2>/dev/null; rm -rf "$d"; exit 1; }
s3() { curl -sS --fail-with-body --aws-sigv4 "aws:amz:us-east-1:s3" --user "$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD" "$@"; }
u=http://127.0.0.1:$port
for i in $(seq 1 100); do
  curl -s -o /dev/null "$u/minio/health/live" && break
  kill -0 $pid 2>/dev/null || fail "server exited"
  sleep 0.1
done
curl -sf -o /dev/null "$u/minio/health/live" || fail "not live"
head -c 3000000 /dev/urandom > "$d/obj"; echo "hello strand" > "$d/small"
s3 -X PUT "$u/smoke" > /dev/null || fail "make bucket"
s3 -T "$d/obj" "$u/smoke/big.bin" > /dev/null || fail "put big"
s3 -T "$d/small" "$u/smoke/dir/small.txt" > /dev/null || fail "put small"
s3 "$u/smoke/big.bin" -o "$d/back" || fail "get big"
cmp "$d/obj" "$d/back" || fail "big object differs"
[ "$(s3 "$u/smoke/dir/small.txt")" = "hello strand" ] || fail "small object differs"
l=$(s3 "$u/smoke?list-type=2") || fail "list"
echo "$l" | grep -q '<Key>big.bin</Key>' && echo "$l" | grep -q '<Key>dir/small.txt</Key>' || fail "list misses keys: $l"
echo "$l" | grep -q '<Size>3000000</Size>' || fail "list size"
s3 -X DELETE "$u/smoke/big.bin" > /dev/null || fail "delete"
s3 -X DELETE "$u/smoke/dir/small.txt" > /dev/null || fail "delete 2"
code=$(curl -s -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" --user "$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD" "$u/smoke/big.bin")
[ "$code" = 404 ] || fail "deleted object still there ($code)"
l=$(s3 "$u/smoke?list-type=2"); echo "$l" | grep -q '<KeyCount>0</KeyCount>' || fail "bucket not empty: $l"
s3 -X DELETE "$u/smoke" > /dev/null || fail "remove bucket"
kill $pid; wait $pid 2>/dev/null
rm -rf "$d"
echo "PASS minio smoke (put/get/list/delete, 3 MB object round trip)"
