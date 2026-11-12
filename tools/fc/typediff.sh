#!/usr/bin/env bash
# The oracle for fc's type descriptors and itabs: build packages with the stock compiler and, for each, compare
# the type:*, go:itab.* and runtime.gcbits.* symbols of the stock object with what fc writes for the same
# names (driver/fc/rtype_diff.go). No fc code is run or kept: the build is the stock compiler's.
#   tools/fc/typediff.sh [-C dir] <packages...>      e.g. tools/fc/typediff.sh std
# Output: build/fc-typediff/report (lines "<pkg> SUMMARY|DIFF|MISSING|BAIL ..."), totals on stdout.
# TOP=n: how many differences and missing-reasons to show (default 30).
here=$(cd "$(dirname "$0")/../.." && pwd)
dir=.
if [ "$1" = -C ]; then dir=$2; shift 2; fi
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS= GOCACHE=${FC_GOCACHE:-/home/exedev/targets/fc-gocache}
export FC_BIN=${FC_BIN:-$here/build/fc} FC_STRICT= FC_ONLY='*' FC_TYPEDIFF=only
out=${FC_TYPEDIFF_DIR:-$here/build/fc-typediff}; rm -rf "$out"; mkdir -p "$out"
export FC_LOG=$out/log FC_REASONS=$out/stderr FC_TYPEDIFF_OUT=$out/report
cd "$dir" || exit 2
go list -deps -json "$@" > "$out/plan.json" || exit 2
export FC_PLAN=$out/plan.json
# a fresh flag value every run: the go command must not answer from its cache
stamp=$(sha1sum "$FC_BIN" | cut -c1-12).$(date +%s)
go build -toolexec="$here/tools/fc/toolexec.sh" -ldflags=-w "-gcflags=all=-l -d=fc=$stamp" "$@" 2>&1 | tail -20
r=$out/report; touch "$r"
awk -F'\t' '$2=="SUMMARY"{n++; split($3,a," "); for(i in a){split(a[i],kv,"="); t[kv[1]]+=kv[2]}}
  END{printf "packages compared: %d   stock descriptors: %d   identical: %d   different: %d   missing in fc: %d   (fc also wrote %d others)\n", n, t["stock"], t["same"], t["diff"], t["missing"], t["extra"]}' "$r"
echo "fc crashed in: $(grep -c '	CRASH$' "$FC_LOG" 2>/dev/null) packages $(grep '	CRASH$' "$FC_LOG" 2>/dev/null | cut -f1 | tr '\n' ' ')"
echo "--- differences by kind"
awk -F'\t' '$2=="DIFF"{d=$4; gsub(/[0-9a-f]+\/[0-9a-f]+$/,"X",d); gsub(/[0-9a-f]+\/[0-9a-f]+;/,"X;",d); gsub(/at [0-9]+/,"at N",d); gsub(/\([0-9]+\/[0-9]+ bytes\)/,"",d); gsub(/[0-9]+:[0-9]+ /,"",d); gsub(/(type:|go:|<name)[^;]*/,"SYM",d); n[d]++} END{for(d in n) printf "%6d  %s\n", n[d], d}' "$r" | sort -rn | head -${TOP:-30}
echo "--- missing in fc, by reason"
awk -F'\t' '$2=="MISSING"{n[$4]++} END{for(d in n) printf "%6d  %s\n", n[d], d}' "$r" | sort -rn | head -${TOP:-30}
echo "--- gave up"
awk -F'\t' '$2=="BAIL"{d=$4; n[d]++} END{for(d in n) printf "%6d  %s\n", n[d], d}' "$r" | sort -rn | head -${TOP:-30}
