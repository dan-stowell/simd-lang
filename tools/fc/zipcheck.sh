#!/usr/bin/env bash
# The differential test of reading module sources from the module cache's zips (driver/fc/modzip.go): build
# every Go corpus project with FC_ZIPCHECK=1, where fc compares each file it takes from a zip with the
# extracted file and stops at the first difference.   tools/fc/zipcheck.sh [name ...]
here=$(cd "$(dirname "$0")/../.." && pwd); rc=0
names=${*:-$(awk '$2=="go"{print $1}' "$here/tools/scoreboard/corpus.txt")}
for n in $names; do
  WARM=1 FC_ZIPCHECK=1 FC_COLD=${FC_COLD:-/home/exedev/targets/fc-zipcheck} "$here/tools/fc/cold.sh" "$n" 8 > /dev/null
  d=${FC_COLD:-/home/exedev/targets/fc-zipcheck}/$n/p8-warm
  if grep -q '^rc 0' "$d/meta.txt" && ! grep -q 'ZIPCHECK' "$d/build.log"; then
    echo "PASS $n: $(grep -o '[0-9]* files ([0-9.]* MB) from [0-9]* module zips.*' "$d/build.log" || echo 'no module zips (vendored or standard library only)')"
  else
    echo "FAIL $n: $(grep -m1 'ZIPCHECK\|^fc: ' "$d/build.log" | cut -c1-200)"; rc=1
  fi
  rm -f "$d/bin"
done
exit $rc
