#!/usr/bin/env bash
# Run a package's own tests with the test binary built through fc's whole-program path: `fc build -plan ... -link`,
# every package by fc (runtime and testing included; assembly by the stock assembler), descriptors written by
# demand, linked by fclink in fc's process. The plan is the go command's (`go list -deps -test`: the test variants
# "P [P.test]" and the generated test main), so the binary is what `go test` would link, compiled our way.
#   tools/fc/wptest.sh [-C dir] <import path> [go test flags: -run X, -count=1, -short, -v, ...]
# Exit: 0 the tests pass; 1 they fail (or crash, or time out); 3 fc did not compile a package (first "fc: <pkg>: "
# line on stderr); 4 the package has no tests; 2 anything else. Skips: tests/fc/skips.txt, as tools/fc/test.sh.
# Environment: FC_BIN (default build/fc), FC_GOCACHE, WPTEST_TIMEOUT (the binary's -test.timeout, default 10m),
# WPTEST_KEEP=<dir> keeps the plan, the build log and the binary there; FC_LINKFLAGS reaches fclink.
here=$(cd "$(dirname "$0")/../.." && pwd)
dir=.
if [ "$1" = -C ]; then dir=$2; shift 2; fi
pkg=$1; shift
[ -n "$pkg" ] || { echo "usage: tools/fc/wptest.sh [-C dir] <import path> [go test flags...]" >&2; exit 2; }
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false GOCACHE=${FC_GOCACHE:-/home/exedev/targets/fc-gocache}
fc=${FC_BIN:-$here/build/fc}
cd "$dir" || exit 2
work=${WPTEST_KEEP:-$(mktemp -d /tmp/fcwp.XXXXXX)}; mkdir -p "$work"
[ -n "$WPTEST_KEEP" ] || trap 'rm -rf "$work"' EXIT
{ go list -deps -test -json "$pkg" && go list -deps -json runtime; } > "$work/plan.json" 2> "$work/list.err" || { cat "$work/list.err" >&2; exit 2; }
# the test main's import path and the package's directory (the tests run there, as under go test)
read -r main pdir < <(python3 - "$work/plan.json" "$pkg" <<'PY'
import json, sys
dec, s, i = json.JSONDecoder(), open(sys.argv[1]).read(), 0
main = pdir = ""
while True:
    while i < len(s) and s[i].isspace(): i += 1
    if i >= len(s): break
    o, i = dec.raw_decode(s, i)
    if o.get("Name") == "main" and o.get("ImportPath") == sys.argv[2] + ".test": main = o["ImportPath"]
    if o.get("ImportPath") == sys.argv[2]: pdir = o.get("Dir", "")
print(main or "-", pdir or "-")
PY
)
# (-X testing.testBinary=1: what go test passes its linker; testing.Testing() and crypto's test hooks read it)
if [ "$main" = - ]; then echo "?   	$pkg	[no test files]"; exit 4; fi
"$fc" build -X testing.testBinary=1 -plan "$work/plan.json" -o "$work/objs" -link "$work/test.bin" "$main" > "$work/build.log" 2>&1; rc=$?
rm -rf "$work/objs"
if [ $rc != 0 ]; then
  grep -m1 '^fc: ' "$work/build.log" >&2 || tail -5 "$work/build.log" >&2
  grep -q '^fc: ' "$work/build.log" && exit 3; exit 2
fi
# go test flags -> the test binary's (-run X -> -test.run X); skips as tools/fc/test.sh applies them
args=()
for a in "$@"; do case "$a" in -test.*) args+=("$a");; -*) args+=("-test.${a#-}");; *) args+=("$a");; esac; done
skip=$(awk -v p="$pkg" '$1==p{printf "%s%s", sep, $2; sep="|"}' "$here/tests/fc/skips.txt" 2>/dev/null)
[ -z "$skip" ] || args=(-test.skip "$skip" "${args[@]}")
cd "$pdir" || exit 2
# (go test runs a test with GOROOT set to its own tree: tests that read the standard library's source count on it)
GOROOT=$(go env GOROOT) "$work/test.bin" -test.paniconexit0 -test.timeout="${WPTEST_TIMEOUT:-10m}" "${args[@]}"; rc=$?
if [ $rc = 0 ]; then echo "ok  	$pkg	(whole program)"; exit 0; fi
echo "FAIL	$pkg	(whole program)"; exit 1
