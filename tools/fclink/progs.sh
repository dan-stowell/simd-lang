#!/usr/bin/env bash
# Oracle level 2: the test programs of tools/fclink/progs, each linked by the stock linker and by fclink from
# the same objects, with and without inlining, outputs compared.   tools/fclink/progs.sh [name-regex]
here=$(cd "$(dirname "$0")" && pwd); re=${1:-.}; fail=0
run() { "$here/try.sh" "$@" || fail=1; }
for p in hello gcstress traceback ifaces initorder; do
  [[ $p =~ $re ]] || continue
  for g in "-gcflags=all=-l" " "; do
    GCFLAGS=$g run "$here/progs/$p"
  done
done
# a crash: exit status 2 and the same traceback text
[[ gcstress =~ $re ]] && run "$here/progs/gcstress" boom
[[ traceback =~ $re ]] && { run "$here/progs/traceback" crash; GCFLAGS=" " run "$here/progs/traceback" crash; }
exit $fail
