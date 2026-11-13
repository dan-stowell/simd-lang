#!/usr/bin/env bash
# One cold build of a Go corpus project with fc, as CHARTER.md defines cold: fresh GOCACHE (the packages that
# stay with the stock compiler, the runtime's closure, are compiled inside the timer), page cache dropped,
# nothing left running from an earlier build. GOMODCACHE is kept (fetching is a separate row).
#   tools/fc/cold.sh <name from tools/scoreboard/corpus.txt> [cores]     ->  $FC_COLD/<name>/p<cores>/{meta.txt,build.log,bin}
# WARM=1 leaves the page cache alone. HUGEPAGES=n (default 5120 = 10 GB; 0: none) reserves a pool of explicit
# 2 MB pages for the run and puts the old setting back after: fc's heap grows from the pool (SB_HUGE, the patched
# runtime fc is built with), ordinary pages when the pool is empty. Machine configuration (CHARTER.md), not a
# cache: MinIO's 8.3 GB heap otherwise costs 2 million page faults (about 4 s of kernel time) and 0.7 s of exit.
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
name=$1; P=${2:-8}
read _ tool dir target < <(grep -E "^$name +go " "$here/tools/scoreboard/corpus.txt")
[ -n "$dir" ] || { echo "no go project $name"; exit 2; }
out=${FC_COLD:-$T/fc-cold}/$name/p$P${WARM:+-warm}; rm -rf "$out"; mkdir -p "$out"
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false
export GOCACHE; GOCACHE=$(mktemp -d $T/gocache.XXXXXX)
# the campaign's linker when it is built (FC_LINKER= for the stock linker)
[ -n "${FC_LINKER+x}" ] || { [ -x "$here/build/fclink" ] && export FC_LINKER=$here/build/fclink; }
hp0=$(cat /proc/sys/vm/nr_hugepages); HUGEPAGES=${HUGEPAGES-5120}; mem=24G
if [ "${HUGEPAGES:-0}" != 0 ]; then
  sudo sysctl -q -w vm.nr_hugepages=$HUGEPAGES; trap 'sudo sysctl -q -w vm.nr_hugepages=$hp0' EXIT
  export SB_HUGE=1; mem=16G   # (the pool is not counted by the scope's MemoryMax)
fi
cd "$T/$dir" || exit 2
main=$(go list "$target")
go clean -cache >/dev/null 2>&1
sync; [ -n "${WARM:-}" ] || echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null
cat > "$out/run.sh" <<IN
#!/usr/bin/env bash
cg=/sys/fs/cgroup\$(cut -d: -f3 /proc/self/cgroup)
t0=\$(date +%s.%N)
case "${PLANNER:-fc}" in
fc)  # fc finds the packages itself while it parses them (driver/fc/selfplan.go)
  t1=\$t0
  GOMAXPROCS=$P taskset -c 0-$((P-1)) "${FC_BIN:-$here/build/fc}" build -j $P -C "$T/$dir" -o "$out/objs" -link "$out/bin" "$target" > "$out/build.log" 2>&1; rc=\$? ;;
self) # the driver's own planner as a separate step (driver/plan.go)
  taskset -c 0-$((P-1)) "$here/build/sb" -C "$T/$dir" -n -plan=self -decl "$here/build/godecl" -dumpplan "$out/plan.json" -o "$out/unused" "$target" > "$out/plan.log" 2>&1 || exit 1
  t1=\$(date +%s.%N)
  GOMAXPROCS=$P taskset -c 0-$((P-1)) "${FC_BIN:-$here/build/fc}" build -j $P -plan "$out/plan.json" -o "$out/objs" -link "$out/bin" "$main" > "$out/build.log" 2>&1; rc=\$? ;;
*)   # go list
  taskset -c 0-$((P-1)) go list -deps -json "$target" runtime > "$out/plan.json" || exit 1
  t1=\$(date +%s.%N)
  GOMAXPROCS=$P taskset -c 0-$((P-1)) "${FC_BIN:-$here/build/fc}" build -j $P -plan "$out/plan.json" -o "$out/objs" -link "$out/bin" "$main" > "$out/build.log" 2>&1; rc=\$? ;;
esac
t2=\$(date +%s.%N)
echo "rc \$rc"; echo "plan_ms \$(echo "(\$t1 - \$t0) * 1000 / 1" | bc)"; echo "wall_ms \$(echo "(\$t2 - \$t0) * 1000 / 1" | bc)"
awk '\$1=="usage_usec"{print "core_s", \$2/1e6}' \$cg/cpu.stat
IN
chmod +x "$out/run.sh"
systemd-run --user --scope -q -p MemoryMax=$mem "$out/run.sh" > "$out/meta.txt"
{ echo "name $name"; echo "P $P"; grep -E '^(link|front end|back end):' "$out/build.log" | sed 's/^/fc /'; echo "hugepages ${HUGEPAGES:-0}"; echo "bin_bytes $(stat -c %s "$out/bin" 2>/dev/null || echo 0)"; echo "commit $(git rev-parse HEAD)"; } >> "$out/meta.txt"
rm -rf "$GOCACHE" "$out/objs"
cat "$out/meta.txt"
