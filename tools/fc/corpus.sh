#!/usr/bin/env bash
# Build the Go corpus projects (tools/scoreboard/corpus.txt, tool "go") whole-program with fc and link them.
#   tools/fc/corpus.sh [name-regex]       results: $FC_CORPUS/<name>/{plan.json,build.log,bin}
# One line per project: how many packages fc compiled, wall and core-seconds, or what stopped it.
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
out=${FC_CORPUS:-$T/fc-corpus}; re=${1:-.}
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false GOCACHE=${FC_GOCACHE:-$T/fc-gocache}
grep -v '^#' "$here/tools/scoreboard/corpus.txt" | while read name tool dir target; do
  [ "$tool" = go ] && [[ $name =~ $re ]] || continue
  d=$out/$name; mkdir -p "$d"
  (cd "$T/$dir" && go list -deps -export -json -gcflags=all=-l "$target" runtime > "$d/plan.json" 2> "$d/list.err") || { echo "$name: go list failed: $(tail -1 "$d/list.err")"; continue; }
  main=$(cd "$T/$dir" && go list "$target")
  "${FC_BIN:-$here/build/fc}" build -plan "$d/plan.json" -o "$d/objs" -link "$d/bin" "$main" > "$d/build.log" 2>&1
  if [ -x "$d/bin" ] && grep -q '^total:' "$d/build.log"; then
    echo "$name: $(grep '^link:' "$d/build.log" | cut -d, -f1 | sed 's/link: //'); $(grep '^total:' "$d/build.log" | sed 's/total: //')"
  else
    echo "$name: NOT BUILT: $(grep -c '^fc: ' "$d/build.log") packages; $(grep -m3 '^fc: \|^panic\|link:' "$d/build.log" | cut -c1-160 | tr '\n' '|')"
  fi
done
