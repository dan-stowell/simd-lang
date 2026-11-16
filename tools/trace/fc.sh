#!/usr/bin/env bash
# The fc rows of TRACE.md: N cold builds of a corpus project by fc (tools/fc/cold.sh: fresh cache, page cache
# dropped, huge pages), the link's own table (-stats), the smoke test, and one cores-busy timeline (a warm
# build: the sampler would otherwise time the page-cache drop). Run on an idle machine; the load is recorded.
#   tools/trace/fc.sh [name (minio)] [runs (5)]     ->  $FCTRACE/<name>/run<i>/{meta.txt,build.log,smoke.txt}, timeline.txt
# tools/trace/mktrace.py reads them.
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
name=${1:-minio}; n=${2:-5}
out=${FCTRACE:-$T/fc-trace}/$name; rm -rf "$out"; mkdir -p "$out"
case $name in minio) smoke=tests/minio_smoke.sh ;; kube-apiserver) smoke=tests/kube_smoke.sh ;; *) smoke= ;; esac
cd "$here"
for i in $(seq 1 "$n"); do
  load=$(cut -d' ' -f1 /proc/loadavg)
  FC_COLD=$out/cold FC_LINKFLAGS=-stats tools/fc/cold.sh "$name" 8 > /dev/null 2>&1
  mkdir -p "$out/run$i"; cp "$out/cold/$name/p8/meta.txt" "$out/cold/$name/p8/build.log" "$out/run$i/"
  echo "load $load" >> "$out/run$i/meta.txt"
  if [ -n "$smoke" ]; then bash "$smoke" "$out/cold/$name/p8/bin" 2>&1 | tail -1 > "$out/run$i/smoke.txt"; fi
  echo "run $i: $(grep -E '^(wall_ms|core_s)' "$out/run$i/meta.txt" | tr '\n' ' ') load $load $(cat "$out/run$i/smoke.txt" 2>/dev/null)"
done
read _ tool dir target < <(grep -E "^$name +go " "$here/tools/scoreboard/corpus.txt")
tmp=$(mktemp -d)
(cd "$T/$dir" && CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false python3 "$here/tools/fc/timeline.py" -b 100 \
  "${FC_BIN:-$here/build/fc}" build -j 8 -C . -o "$tmp/objs" -link "$tmp/bin" "$target") > "$out/timeline.txt" 2>&1
rm -rf "$tmp" "$out/cold"
git rev-parse --short HEAD > "$out/commit.txt"; date -u +'%Y-%m-%d %H:%M UTC' > "$out/date.txt"
