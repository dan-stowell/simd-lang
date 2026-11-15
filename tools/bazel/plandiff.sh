#!/usr/bin/env bash
# The query front end's oracle: the plans (every Go package handed to fc, for the binary and for every tool built
# on the way) and the generated files of bzfc -query against those of bzfc -aquery, on the same bzfc and fc.
#   tools/bazel/plandiff.sh <name from corpus.txt> <aquery.json> <query.pb> <output base> [dir]
# aquery.json and query.pb as tools/bazel/cold.sh makes them (BZ_QUERY=1 for the query), from the output base given
# (fetched: both read sources from its external/). Both runs are -planonly: generation and the tools run, the binary
# is not built. Writes dir/{aq,qy}.plan and dir/{aq,qy}.files (tools/bazel/plancanon.py: configuration directory names
# normalized, nothing else) and prints the differences. Exit 0 when the plans are equal and no generated file differs.
set -u
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
name=$1; aq=$2; q=$3; ob=$4; dir=${5:-$T/bz-scratch/plandiff-$1}
read _ tool ws target < <(grep -E "^$name +bazel " "$here/tools/scoreboard/corpus.txt")
[ -n "$ws" ] || { echo "no bazel project $name"; exit 2; }
FC_BIN=${FC_BIN:-/home/exedev/simd-lang/build/fc}; BZFC=${BZFC:-$here/build/bzfc}
[ -n "${BZFC_KEEP:-}" ] || [ "$BZFC" != "$here/build/bzfc" ] || (cd "$here/tools/bazel/bzfc" && GOTOOLCHAIN=local go build -o "$BZFC" .) || exit 2
[ -n "${FC_LINKER+x}" ] || { [ -x "$(dirname "$FC_BIN")/fclink" ] && export FC_LINKER=$(dirname "$FC_BIN")/fclink; }
mkdir -p "$dir"
run() { # run <tag> <front-end flag> <file>
  rm -rf "$dir/$1.w"
  GOMAXPROCS=8 "$BZFC" "$2" "$3" -ob "$ob" -ws "$T/$ws" -label "$target" -work "$dir/$1.w" -fc "$FC_BIN" -bin "$dir/$1.bin" -j 8 -planonly > "$dir/$1.log" 2>&1 || { echo "plandiff: bzfc $2 failed (rc $?): $dir/$1.log"; tail -20 "$dir/$1.log"; return 1; }
  python3 "$here/tools/bazel/plancanon.py" "$dir/$1.w" > "$dir/$1.plan"
  python3 "$here/tools/bazel/plancanon.py" --files "$dir/$1.w" > "$dir/$1.files"
}
run aq -aquery "$aq" & p1=$!
run qy -query "$q" & p2=$!
wait $p1 || exit 1; wait $p2 || exit 1
rc=0
pk() { awk '{print $1, $2}' "$1" | sort -u | wc -l; }
echo "plandiff $name: aquery $(pk "$dir/aq.plan") (binary, package) pairs, $(wc -l < "$dir/aq.plan") lines; query $(pk "$dir/qy.plan"), $(wc -l < "$dir/qy.plan")"
if diff "$dir/aq.plan" "$dir/qy.plan" > "$dir/plan.diff"; then echo "plandiff $name: plans equal"; else
  rc=1; echo "plandiff $name: plans differ ($(grep -c '^[<>]' "$dir/plan.diff") lines, $dir/plan.diff):"; head -40 "$dir/plan.diff"; fi
# generated files: a path both runs have must have the same contents (or the same after configuration directory names,
# counted apart); paths only one run has are listed. elf: tools fc built (they hold configuration directory names).
python3 "$here/tools/bazel/plancanon.py" --cmp "$dir/aq.files" "$dir/qy.files" > "$dir/files.cmp" || rc=1
sed "s/^/plandiff $name: /" "$dir/files.cmp" | head -60
exit $rc
