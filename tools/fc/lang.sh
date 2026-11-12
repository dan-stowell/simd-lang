#!/usr/bin/env bash
# The language-feature corpus (tests/fc/lang, module fclang) under fc's mixed mode: one line per package.
#   tools/fc/lang.sh [pkg...]          (default: every package of the module)
#   PASS            fc compiled the package and its tests pass
#   FAIL            fc compiled the package; the tests fail, crash, do not link, or run past the timeout
#   NOFC <reason>   fc gave the package up (first "fc: <pkg>: " line of its stderr)
# Environment: FC_BIN (default build/fc), LANG_JOBS (4), LANG_TIMEOUT (seconds, 120), LANG_KEEP=<dir> keeps the logs,
# LANG_CPUS (e.g. 4-7) pins the runs; by default they are pinned to 4-7 while the scoreboard measurement runs.
# Exit 0 when every package is PASS.
here=$(cd "$(dirname "$0")/../.." && pwd)
lang=$here/tests/fc/lang
jobs=${LANG_JOBS:-4} tmo=${LANG_TIMEOUT:-120}
logs=${LANG_KEEP:-$(mktemp -d /tmp/fclang.XXXXXX)}
mkdir -p "$logs"
[ -n "$LANG_KEEP" ] || trap 'rm -rf "$logs"' EXIT
export FC_BIN=${FC_BIN:-$here/build/fc} FC_STRICT=1
[ -x "$FC_BIN" ] || { echo "no fc binary at $FC_BIN (FC_TAG=... driver/mkfc.sh, then FC_BIN=...)" >&2; exit 2; }
pin=()
if [ -z "${LANG_CPUS+x}" ] && pgrep -f scoreboard/run.sh >/dev/null; then LANG_CPUS=4-7; fi
[ -n "$LANG_CPUS" ] && pin=(taskset -c "$LANG_CPUS")

if [ $# -gt 0 ]; then pkgs=("$@"); else
  pkgs=(); for d in "$lang"/*/; do ls "$d"*.go >/dev/null 2>&1 && pkgs+=("$(basename "$d")"); done
fi

one() { # one <pkg>: the verdict line into $logs/<pkg>.res
  local p=$1 log=$logs/$1.log rc
  timeout -k 5 "$tmo" "${pin[@]}" "$here/tools/fc/test.sh" -C "$lang" "fclang/$p" -count=1 > "$log" 2>&1; rc=$?
  local why
  why=$(grep -m1 "^fc: fclang/$p: " "$log" | sed "s|^fc: fclang/$p: ||")
  if [ $rc = 0 ]; then echo "PASS"
  elif [ $rc = 124 ] || [ $rc = 137 ]; then echo "FAIL (timeout ${tmo}s)"
  elif [ -n "$why" ]; then echo "NOFC $why"
  elif grep -q "^fc: " "$log"; then echo "NOFC $(grep -m1 '^fc: ' "$log" | sed 's|^fc: ||')"
  else echo "FAIL"
  fi > "$logs/$p.res"
}

for p in "${pkgs[@]}"; do
  while [ "$(jobs -rp | wc -l)" -ge "$jobs" ]; do wait -n; done
  one "$p" &
done
wait

pass=0 fail=0 nofc=0
for p in "${pkgs[@]}"; do
  r=$(cat "$logs/$p.res" 2>/dev/null || echo FAIL)
  printf '%-10s %s\n' "$p" "$r"
  case $r in PASS*) pass=$((pass+1));; NOFC*) nofc=$((nofc+1));; *) fail=$((fail+1));; esac
done
echo "total ${#pkgs[@]}: PASS $pass  FAIL $fail  NOFC $nofc"
[ "$pass" = "${#pkgs[@]}" ]
