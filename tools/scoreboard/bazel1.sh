#!/usr/bin/env bash
# One cold Bazel build, instrumented:  bazel1.sh <name> <workspace> <P> <target>...
# Cold (CHARTER.md): fresh output base, the Bazel server started inside the timer, no disk or remote cache,
# no BES upload, page cache dropped. Kept between runs, like GOMODCACHE for go: the repository cache
# (downloaded archives and the repo-contents cache), filled beforehand by `FETCH=1 bazel1.sh ...`.
# Core-seconds come from the cgroup of the scope the whole build (client and server) runs in.
# Writes $SB_OUT/<name>/bazel-p<P>/ {meta.txt, profile.json.gz, build.log}
set -u
name=$1; ws=$2; P=$3; shift 3
BAZEL=${BAZEL:-/home/exedev/bin/bazelisk}
SB_OUT=${SB_OUT:-/home/exedev/targets/scoreboard}
RC=/home/exedev/targets/bazel-repocache
out=$SB_OUT/$name/bazel-p$P${TAG:+-$TAG}
rm -rf "$out"; mkdir -p "$out"
ob=$(mktemp -d /home/exedev/targets/bazel-ob.XXXXXX)
flags=(--repository_cache=$RC --disk_cache= --remote_cache= --remote_executor= --bes_backend= --bes_results_url=
       --jobs=$P --local_resources=cpu=$P --noshow_progress --profile=$out/profile.json.gz)
cd "$ws" || exit 1
if [ -n "${FETCH:-}" ]; then
  $BAZEL --output_base=$ob build --nobuild "${flags[@]}" "$@"; rc=$?
  $BAZEL --output_base=$ob shutdown; chmod -R u+w $ob; rm -rf $ob; exit $rc
fi
sync; echo 3 | sudo tee /proc/sys/vm/drop_caches >/dev/null
cat > $ob.run <<IN
#!/usr/bin/env bash
cg=/sys/fs/cgroup\$(cut -d: -f3 /proc/self/cgroup)
t0=\$(date +%s.%N)
taskset -c 0-$((P-1)) $BAZEL --output_base=$ob build ${flags[*]} $* > $out/build.log 2>&1; rc=\$?
t1=\$(date +%s.%N)
taskset -c 0-$((P-1)) $BAZEL --output_base=$ob shutdown
echo "rc \$rc"; echo "t0 \$t0"; echo "t1 \$t1"; echo "wall_ms \$(echo "(\$t1 - \$t0) * 1000 / 1" | bc)"
awk '\$1=="usage_usec"{print "core_s", \$2/1e6} \$1=="user_usec"{print "user_s", \$2/1e6} \$1=="system_usec"{print "sys_s", \$2/1e6}' \$cg/cpu.stat
IN
chmod +x $ob.run
systemd-run --user --scope -q -p MemoryMax=24G $ob.run > "$out/meta.txt"
{ echo "name $name"; echo "P $P"; echo "tool bazel"; echo "commit $(git rev-parse HEAD)"; echo "bazel $($BAZEL --version 2>/dev/null | head -1)"; } >> "$out/meta.txt"
# Bazel's own critical path, from its last summary line
sed -n 's/.*Critical Path: \([0-9.]*\)s.*/\1/p' "$out/build.log" | tail -1 | awk '{print "crit_ms", int($1*1000)}' >> "$out/meta.txt"
grep -o '[0-9]* processes' "$out/build.log" | tail -1 | awk '{print "processes", $1}' >> "$out/meta.txt"
$BAZEL --output_base=$ob shutdown >/dev/null 2>&1
chmod -R u+w $ob 2>/dev/null; rm -rf $ob $ob.run
cat "$out/meta.txt"
