#!/usr/bin/env bash
# -toolexec wrapper: retain speculative descriptors only in one selected package.
set -euo pipefail
tool=$1; shift
if [[ ${tool##*/} != compile ]]; then exec "$tool" "$@"; fi
prev=; keep=()
for arg in "$@"; do
  if [[ $prev == -p && $arg == "${STRAND_KEEP_PACKAGE:?}" ]]; then keep=(SB_KEEP_WEAKPTR=1); fi
  prev=$arg
done
exec env -u SB_KEEP_WEAKPTR SB_BASELINE=1 "${keep[@]}" "$tool" "$@"
