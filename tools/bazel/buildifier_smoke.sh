#!/usr/bin/env bash
# The smoke check for a buildifier binary: its --version runs, and it formats (and lints) BUILD and .bzl files
# exactly as a reference buildifier does.   buildifier_smoke.sh <binary> <reference binary> [files...]
# Without files: every BUILD, BUILD.bazel, .bzl and testdata .build file of the corpus's three Bazel workspaces, each
# formatted from stdin with its own path for the file type, plus the lint warnings of each.
set -u
bin=$1; ref=$2; shift 2
"$bin" --version >/dev/null || { echo "smoke: --version failed"; exit 1; }
[ "$("$bin" --version)" = "$("$ref" --version)" ] || { echo "smoke: versions differ"; exit 1; }
if [ $# -eq 0 ]; then
  mapfile -t files < <(find /home/exedev/targets/corpus/{buildtools,gvisor-bazel,buildbuddy} -path '*/bazel-*' -prune -o \
    \( -name BUILD -o -name BUILD.bazel -o -name '*.bzl' -o -path '*/testdata/*' -name '*.build' \) -type f -print | sort)
else
  files=("$@")
fi
n=0; bad=0
for f in "${files[@]}"; do
  a=$("$bin" --path="$f" < "$f" 2>&1; echo "rc $?"; "$bin" --lint=warn --mode=check "$f" 2>&1 | sed "s|$f|F|g")
  b=$("$ref" --path="$f" < "$f" 2>&1; echo "rc $?"; "$ref" --lint=warn --mode=check "$f" 2>&1 | sed "s|$f|F|g")
  n=$((n+1))
  if [ "$a" != "$b" ]; then bad=$((bad+1)); echo "smoke: differs on $f"; fi
done
echo "smoke: $n files, $bad differ"
[ $bad -eq 0 ]
