#!/usr/bin/env bash
# Soak an fc-built program against the stock-built one: google.golang.org/protobuf's protoc-gen-go, fed one
# CodeGeneratorRequest (tests/fc/soak/protoc-gen-go.req.bin: protobuf's own test protos, captured from protoc with a
# plugin that is `cat > req.bin`), run many times with the collector always on (GOGC=1), several at once. A run is
# bad when it exits non-zero or its output differs from the stock binary's; a run older than 40 s is a hang: every
# thread's stack is taken from a core (tools/fc/corebt) before it is killed. This is how the write barriers in
# nowritebarrierrec code were found (JOURNAL 2026-11-15 19:01: 3 hangs in 1,400 runs under load).
#   tools/fc/soak.sh [runs per worker (400)] [workers (4)]     work and evidence: $FC_SOAK (default /home/exedev/targets/fc-soak)
here=$(cd "$(dirname "$0")/../.." && pwd); n=${1:-400}; w=${2:-4}
d=${FC_SOAK:-/home/exedev/targets/fc-soak}; mkdir -p "$d"; cd "$d" || exit 2
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false
M=$(ls -d "$(go env GOMODCACHE)"/google.golang.org/protobuf@v1.36.11 2>/dev/null) || { echo "need google.golang.org/protobuf@v1.36.11 in GOMODCACHE"; exit 2; }
FC_LINKER=${FC_LINKER-$here/build/fclink} "${FC_BIN:-$here/build/fc}" build -C "$M" -o "$d/objs" -link "$d/pgg-fc" ./cmd/protoc-gen-go > build.log 2>&1 || { tail -3 build.log; exit 2; }
(cd "$M" && GOCACHE="$d/gocache" go build -o "$d/pgg-stock" ./cmd/protoc-gen-go) || exit 2
(cd "$here/tools/fc/corebt" && go build -o "$d/corebt" .) || exit 2
req=$here/tests/fc/soak/protoc-gen-go.req.bin
want=$(./pgg-stock < "$req" | sha1sum | cut -d' ' -f1)
rm -f bad.* hang.*; ulimit -S -c 0   # (the workers allow cores for their own runs)
( while sleep 5; do   # the watcher: hangs
    for pid in $(pgrep -x pgg-fc); do
      age=$(ps -o etimes= -p "$pid" 2>/dev/null | tr -d ' ')
      if [ -n "$age" ] && [ "$age" -gt 40 ]; then
        gcore -o "$d/core" "$pid" > /dev/null 2>&1 && ./corebt pgg-fc "core.$pid" > "hang.$pid.txt" 2>&1; rm -f "core.$pid"
        kill -KILL "$pid"
      fi
    done
  done ) & watcher=$!
for i in $(seq "$w"); do
  ( bad=0
    for j in $(seq "$n"); do
      # (run in a directory of its own with core dumps allowed: a death by signal that the runtime's handler
      # did not report, rc 139 with nothing on stderr, leaves its core there)
      mkdir -p "w$i"
      ( cd "w$i" && ulimit -c unlimited && GOGC=1 GOTRACEBACK=all exec ../pgg-fc < "$req" > "../out.$i.tmp" 2> "../err.$i.tmp" ); rc=$?
      if [ $rc != 0 ] || [ "$(sha1sum < "out.$i.tmp" | cut -d' ' -f1)" != "$want" ]; then
        bad=$((bad+1)); cp "err.$i.tmp" "bad.$i.$j.err"; echo "run $i.$j: rc $rc"
        for c in "w$i"/core*; do
          [ -f "$c" ] && { ./corebt pgg-fc "$c" > "bad.$i.$j.stacks.txt" 2>&1; mv "$c" "bad.$i.$j.core"; }
        done
      fi
    done
    echo "worker $i: $bad bad of $n" ) &
  workers="$workers $!"
done
wait $workers; kill $watcher 2>/dev/null
echo "hangs: $(ls hang.*.txt 2>/dev/null | wc -l) (stacks in $d/hang.*.txt)  load: $(cut -d' ' -f1-3 /proc/loadavg)"
