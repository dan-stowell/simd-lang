#!/usr/bin/env bash
# Run a package's own tests with the package compiled by fc (everything else by the stock compiler).
#   tools/fc/test.sh [-C dir] <import path> [go test flags...]
# Exit 0: fc compiled the package and its tests pass. FC_ALL=1: fc compiles every package above the runtime.
here=$(cd "$(dirname "$0")/../.." && pwd)
dir=.
if [ "$1" = -C ]; then dir=$2; shift 2; fi
pkg=$1; shift
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS= GOCACHE=${FC_GOCACHE:-/home/exedev/targets/fc-gocache}
export FC_BIN=${FC_BIN:-$here/build/fc} FC_STRICT=${FC_STRICT-1}
[ -x "$here/build/compile-esc" ] && export FC_ESC_COMPILE=${FC_ESC_COMPILE-$here/build/compile-esc}
cd "$dir" || exit 2
plan=$(mktemp /tmp/fcplan.XXXXXX); trap 'rm -f $plan' EXIT
go list -deps -test -json "$pkg" runtime > "$plan" || exit 2
export FC_PLAN=$plan FC_ONLY=$pkg
all=$pkg
if [ -n "$FC_ALL" ]; then
  # FC_ALL=1: every package of the test binary by fc, except the runtime and what it imports (as fc build does)
  export FC_ONLY='*' FC_STOCK; all=all
  FC_STOCK=" runtime $(go list -deps runtime | tr '\n' ' ') "
fi
hash=$( (cat "$FC_BIN" "${FC_ESC_COMPILE:-/dev/null}"; echo "$FC_ESC") | sha1sum | cut -c1-12)
# tests the dev build is excused from (tests/fc/skips.txt: package, test name pattern, reason)
skip=$(awk -v p="$pkg" '$1==p{printf "%s%s", sep, $2; sep="|"}' "$here/tests/fc/skips.txt" 2>/dev/null)
[ -z "$skip" ] || set -- -skip "$skip" "$@"
go test -vet=off -toolexec="$here/tools/fc/toolexec.sh" -ldflags=-w -gcflags=all=-l "-gcflags=$all=-l -d=fc=$hash" "$@" "$pkg"
