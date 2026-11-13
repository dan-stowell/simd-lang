#!/usr/bin/env bash
# One cold build of a Go corpus project with fc, as CHARTER.md defines cold: fresh GOCACHE (the packages that
# stay with the stock compiler, the runtime's closure, are compiled inside the timer), page cache dropped,
# nothing left running from an earlier build. GOMODCACHE is kept (fetching is a separate row).
#   tools/fc/cold.sh <name from tools/scoreboard/corpus.txt> [cores]     ->  $FC_COLD/<name>/p<cores>/{meta.txt,build.log,bin}
# WARM=1 leaves the page cache alone.
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
name=$1; P=${2:-8}
read _ tool dir target < <(grep -E "^$name +go " "$here/tools/scoreboard/corpus.txt")
[ -n "$dir" ] || { echo "no go project $name"; exit 2; }
out=${FC_COLD:-$T/fc-cold}/$name/p$P${WARM:+-warm}; rm -rf "$out"; mkdir -p "$out"
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false
export GOCACHE; GOCACHE=$(mktemp -d $T/gocache.XXXXXX)
cd "$T/$dir" || exit 2
main=$(go list "$target")
go clean -cache >/dev/null 2>&1
sync; [ -n "${WARM:-}" ] || echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null
cat > "$out/run.sh" <<IN
#!/usr/bin/env bash
cg=/sys/fs/cgroup\$(cut -d: -f3 /proc/self/cgroup)
t0=\$(date +%s.%N)
taskset -c 0-$((P-1)) go list -deps -json "$target" runtime > "$out/plan.json" || exit 1
t1=\$(date +%s.%N)
GOMAXPROCS=$P taskset -c 0-$((P-1)) "${FC_BIN:-$here/build/fc}" build -j $P -plan "$out/plan.json" -o "$out/objs" -link "$out/bin" "$main" > "$out/build.log" 2>&1; rc=\$?
t2=\$(date +%s.%N)
echo "rc \$rc"; echo "plan_ms \$(echo "(\$t1 - \$t0) * 1000 / 1" | bc)"; echo "wall_ms \$(echo "(\$t2 - \$t0) * 1000 / 1" | bc)"
awk '\$1=="usage_usec"{print "core_s", \$2/1e6}' \$cg/cpu.stat
IN
chmod +x "$out/run.sh"
systemd-run --user --scope -q -p MemoryMax=24G "$out/run.sh" > "$out/meta.txt"
{ echo "name $name"; echo "P $P"; grep -E '^(link|front end|back end):' "$out/build.log" | sed 's/^/fc /'; echo "bin_bytes $(stat -c %s "$out/bin" 2>/dev/null || echo 0)"; echo "commit $(git rev-parse HEAD)"; } >> "$out/meta.txt"
rm -rf "$GOCACHE" "$out/objs"
cat "$out/meta.txt"
