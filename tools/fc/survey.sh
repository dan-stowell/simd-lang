#!/usr/bin/env bash
# What stops fc on a set of packages: build them with every package offered to fc (the stock object stays
# where fc gives up) and count the reasons.
#   tools/fc/survey.sh [-C dir] <packages...>      e.g. tools/fc/survey.sh std
here=$(cd "$(dirname "$0")/../.." && pwd)
dir=.
if [ "$1" = -C ]; then dir=$2; shift 2; fi
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS= GOCACHE=${FC_GOCACHE:-/home/exedev/targets/fc-gocache}
export FC_BIN=${FC_BIN:-$here/build/fc} FC_STRICT= FC_ONLY='*'
out=${FC_SURVEY:-$here/build/fc-survey}; rm -rf "$out"; mkdir -p "$out"
export FC_LOG=$out/log FC_REASONS=$out/reasons
cd "$dir" || exit 2
go list -deps -json "$@" > "$out/plan.json" || exit 2
export FC_PLAN=$out/plan.json
hash=$(sha1sum "$FC_BIN" | cut -c1-12)
go build -toolexec="$here/tools/fc/toolexec.sh" -ldflags=-w "-gcflags=all=-l -d=fc=$hash" "$@" 2>&1 | tail -20
echo "compiled by fc: $(grep -c '	FC$' "$FC_LOG")   left to stock: $(grep -c '	STOCK$' "$FC_LOG")"
echo "--- reasons (packages affected, functions affected)"
# a line is "fc: <pkg>: <what>: <reason>"
sed -n 's/^fc: \([^:]*\): \(.*\)$/\1\t\2/p' "$FC_REASONS" | awk -F'\t' '{r=$2; sub(/^[^:]*: /,"",r); gsub(/[^ ]*\.[^ ]*/,"X",r); n[r]++; if(!seen[r,$1]++) p[r]++} END{for(r in n) printf "%5d %6d  %s\n", p[r], n[r], r}' | sort -rn | head -${TOP:-40}
