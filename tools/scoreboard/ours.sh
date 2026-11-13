#!/usr/bin/env bash
# The scoreboard's "ours" column: every Go corpus project built cold by fc (tools/fc/cold.sh) at 8, 4, 2 and 1
# CPUs, the binary checked (the project's smoke test where there is one, else its version command), and the
# result filed where mkscoreboard.py looks.   tools/scoreboard/ours.sh [name-regex] [cores...]
# Run it on an idle machine. A build whose binary fails its check is recorded as failed.
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
SB_OUT=${SB_OUT:-$T/scoreboard}; export FC_COLD=$T/fc-cold
re=${1:-.}; shift; cores=${*:-8 4 2 1}
check() { # check <name> <binary>
  case $1 in
    minio) "$here/tests/minio_smoke.sh" "$2" 19300 >/dev/null 2>&1 ;;
    kube-apiserver) "$here/tests/kube_smoke.sh" "$2" >/dev/null 2>&1 ;;
    caddy) timeout 30 "$2" version >/dev/null 2>&1 ;;
    *) timeout 30 "$2" --version >/dev/null 2>&1 ;;
  esac
}
for P in $cores; do
  grep -v '^#' "$here/tools/scoreboard/corpus.txt" | while read name tool dir target; do
    [ "$tool" = go ] && [[ $name =~ $re ]] || continue
    "$here/tools/fc/cold.sh" "$name" "$P" > /dev/null 2>&1 < /dev/null
    m=$FC_COLD/$name/p$P/meta.txt; o=$SB_OUT/$name/ours-p$P; mkdir -p "$o"
    cp "$m" "$o/meta.txt"
    if ! check "$name" "$FC_COLD/$name/p$P/bin" < /dev/null; then sed -i 's/^rc 0$/rc 1/' "$o/meta.txt"; fi
    echo "$(date -u +%T) $name P=$P $(grep -E '^(rc|wall_ms|core_s) ' "$o/meta.txt" | tr '\n' ' ')"
  done
done
