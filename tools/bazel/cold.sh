#!/usr/bin/env bash
# One cold build of a Bazel corpus project our way: Bazel reads the DAG, fc builds it (tools/bazel/bzfc).
#   tools/bazel/cold.sh <name from tools/scoreboard/corpus.txt (a bazel row)> [cores]
#        ->  $FC_COLD/<name>/bazel-p<cores>/{meta.txt, aquery.log, build.log, bin}
# Cold as CHARTER.md and tools/scoreboard/bazel1.sh define it for Bazel rows: a fresh output base, the Bazel server
# started inside the timer, no disk or remote cache, page cache dropped; the repository cache (downloaded archives,
# filled beforehand by `FETCH=1 tools/scoreboard/bazel1.sh`) is kept, like GOMODCACHE. Inside the timer:
#   1. bazel aquery 'deps(<target>)' (server start, repository extraction, loading and analysis; nothing built)
#   2. bzfc: the generated sources (protoc, genrules) by running their actions, the Go tools they need built
#      by fc; then the binary by fc and fclink, the standard library and runtime included.
# Core-seconds from the cgroup of the scope everything runs in, Bazel's server included (read after it shut down,
# as bazel1.sh does). HUGEPAGES as tools/fc/cold.sh (fc's heap; machine configuration, not a cache).
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
name=$1; P=${2:-8}
read _ tool dir target < <(grep -E "^$name +bazel " "$here/tools/scoreboard/corpus.txt")
[ -n "$dir" ] || { echo "no bazel project $name"; exit 2; }
BAZEL=${BAZEL:-/home/exedev/bin/bazelisk}
FC_BIN=${FC_BIN:-$here/build/fc}; BZFC=${BZFC:-$here/build/bzfc}
[ -x "$BZFC" ] || (cd "$here/tools/bazel/bzfc" && GOTOOLCHAIN=local go build -o "$BZFC" .) || exit 2
[ -n "${FC_LINKER+x}" ] || { [ -x "$(dirname "$FC_BIN")/fclink" ] && export FC_LINKER=$(dirname "$FC_BIN")/fclink; }
out=${FC_COLD:-$T/fc-cold}/$name/bazel-p$P${WARM:+-warm}; rm -rf "$out"; mkdir -p "$out"
scratch=${BZ_SCRATCH:-$T/bz-scratch}; mkdir -p "$scratch"
ob=$(mktemp -d "$scratch/ob.XXXXXX"); work=$(mktemp -d "$scratch/work.XXXXXX")
flags="--repository_cache=$T/bazel-repocache --disk_cache= --remote_cache= --remote_executor= --bes_backend= --bes_results_url= --jobs=$P --local_resources=cpu=$P --noshow_progress"
hp0=$(cat /proc/sys/vm/nr_hugepages); HUGEPAGES=${HUGEPAGES-5120}; mem=24G
if [ "${HUGEPAGES:-0}" != 0 ]; then
  sudo sysctl -q -w vm.nr_hugepages=$HUGEPAGES; trap 'sudo sysctl -q -w vm.nr_hugepages=$hp0' EXIT
  export SB_HUGE=1; mem=16G
fi
cd "$T/$dir" || exit 2
sync; [ -n "${WARM:-}" ] || echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null
cat > "$out/run.sh" <<IN
#!/usr/bin/env bash
cg=/sys/fs/cgroup\$(cut -d: -f3 /proc/self/cgroup)
t0=\$(date +%s.%N)
taskset -c 0-$((P-1)) $BAZEL --output_base=$ob aquery $flags --include_file_write_contents --include_param_files --output=jsonproto 'deps($target)' > "$work/aq.json" 2> "$out/aquery.log"; rc=\$?
t1=\$(date +%s.%N); c1=\$(awk '\$1=="usage_usec"{print \$2/1e6}' \$cg/cpu.stat)
if [ \$rc = 0 ]; then
  GOMAXPROCS=$P taskset -c 0-$((P-1)) "$BZFC" -aquery "$work/aq.json" -ob $ob -ws "$T/$dir" -label "$target" -work "$work/w" -fc "$FC_BIN" -bin "$out/bin" -j $P > "$out/build.log" 2>&1; rc=\$?
fi
t2=\$(date +%s.%N)
taskset -c 0-$((P-1)) $BAZEL --output_base=$ob shutdown
echo "rc \$rc"; echo "bazel_ms \$(echo "(\$t1 - \$t0) * 1000 / 1" | bc)"; echo "bazel_core_s \$c1"; echo "wall_ms \$(echo "(\$t2 - \$t0) * 1000 / 1" | bc)"
awk '\$1=="usage_usec"{print "core_s", \$2/1e6}' \$cg/cpu.stat
IN
chmod +x "$out/run.sh"
systemd-run --user --scope -q -p MemoryMax=$mem "$out/run.sh" > "$out/meta.txt"
{ echo "name $name"; echo "P $P"; echo "tool bazel+fc"
  sed -n 's/^bzfc: *\([0-9]*\) ms  generate: \([0-9]*\) files, \([0-9]*\) actions run, \([0-9.]*\) core-s, \([0-9]*\) retried/gen_end_ms \1\ngen_files \2\ngen_actions \3\ngen_core_s \4\ngen_retries \5/p' "$out/build.log"
  sed -n 's/^bzfc: *\([0-9]*\) ms  read the DAG.*/dag_ms \1/p' "$out/build.log"
  sed -n 's/^bzfc: *\([0-9]*\) ms  fc build.*/bzfc_ms \1/p' "$out/build.log"
  grep -E '^(link|front end|back end):' "$out/build.log" | sed 's/^/fc /'
  echo "hugepages ${HUGEPAGES:-0}"; echo "bin_bytes $(stat -c %s "$out/bin" 2>/dev/null || echo 0)"
  echo "commit $(git rev-parse HEAD)"; echo "bazel $($BAZEL --version 2>/dev/null | head -1)"; } >> "$out/meta.txt"
$BAZEL --output_base=$ob shutdown >/dev/null 2>&1
chmod -R u+w "$ob" "$work" 2>/dev/null; rm -rf "$ob" "$work"
cat "$out/meta.txt"
