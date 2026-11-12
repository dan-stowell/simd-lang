#!/usr/bin/env bash
# The corpus scoreboard's builds: every project in corpus.txt cold at 1, 2, 4 and 8 CPUs; go projects stock and
# fair stock (CHARTER.md, "A fair stock row"), Bazel projects stock.  Hours; run it in the background on an idle machine.
#   run.sh [name-regex] [cores...]      then  mkscoreboard.py
# Results: $SB_OUT/<name>/{p<P>-cold, p<P>-cold-fair, bazel-p<P>}/meta.txt
here=$(cd "$(dirname "$0")" && pwd); T=/home/exedev/targets
export SB_OUT=${SB_OUT:-$T/scoreboard} ; export TRACES=$SB_OUT
re=${1:-.}; shift; cores=${*:-8 4 2 1}
run() { systemd-run --user --scope -q -p MemoryMax=24G env "$@"; }
for P in $cores; do
  grep -v '^#' "$here/corpus.txt" | while read name tool dir target; do
    [[ $name =~ $re ]] || continue
    echo "== $(date -u +%FT%TZ) $name $tool P=$P"
    if [ $tool = go ]; then
      run "$here/../trace/stock.sh" $name $T/$dir $target $P cold < /dev/null | grep -E '^(rc|wall_ms) '
      run TAG=fair GOGC=off "$here/../trace/stock.sh" $name $T/$dir $target $P cold "-gcflags=all=-dwarf=false -l" "-ldflags=-s -w" < /dev/null | grep -E '^(rc|wall_ms) '
    else
      "$here/bazel1.sh" $name $T/$dir $P $target < /dev/null | grep -E '^(rc|wall_ms) '
    fi
  done
done
echo ALLDONE
