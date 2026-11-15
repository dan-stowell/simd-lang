#!/usr/bin/env bash
# The same bytes from the same sources: build one program n times with fc (whole program, fclink in process)
# and compare the binaries byte for byte. For a pair that differs, fclink's -dumpmap (address, size, bucket,
# package and name of every live symbol) says what moved: symbols that changed package (made by whichever
# package asked first), and per bucket the symbols whose order inside their package changed.
#   tools/fc/samebytes.sh <module dir> [target (default .)] [n (default 3)]
# Environment: FC_BIN (default build/fc), SAME_KEEP=<dir> keeps the binaries and maps.
# Exit 0 when every build equals the first.
here=$(cd "$(dirname "$0")/../.." && pwd)
dir=$1; tgt=${2:-.}; n=${3:-3}
[ -d "$dir" ] || { echo "usage: samebytes.sh <module dir> [target] [n]" >&2; exit 2; }
fc=${FC_BIN:-$here/build/fc}
w=${SAME_KEEP:-$(mktemp -d /tmp/fcsame.XXXXXX)}; mkdir -p "$w"
[ -n "$SAME_KEEP" ] || trap 'rm -rf "$w"' EXIT
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false
for i in $(seq 1 "$n"); do
  FC_LINKFLAGS="$FC_LINKFLAGS -dumpmap $w/map$i" "$fc" build -C "$dir" -o "$w/objs$i" -link "$w/bin$i" "$tgt" > "$w/log$i" 2>&1 ||
    { echo "build $i failed:"; tail -5 "$w/log$i"; exit 2; }
  rm -rf "$w/objs$i"
done
rc=0
for i in $(seq 2 "$n"); do
  if cmp -s "$w/bin1" "$w/bin$i"; then echo "build $i == build 1 ($(stat -c %s "$w/bin1") bytes)"; continue; fi
  rc=1
  echo "build $i != build 1: $(cmp -l "$w/bin1" "$w/bin$i" | wc -l) of $(stat -c %s "$w/bin1") bytes differ"
  # (content-addressed symbols carry numbers in their names that mean nothing: leave the names out for those)
  for m in 1 $i; do awk '{n=$5; if (n ~ /^go:(string|strhdr|complex)\./) n="go:const"; print $3, $4, n, $2}' "$w/map$m" > "$w/m$m"; done
  moved=$(diff <(sort "$w/m1") <(sort "$w/m$i") | grep -c '^<')
  echo "  symbols in another package (or of another size): $moved of $(wc -l < "$w/m1")"
  diff <(sort "$w/m1") <(sort "$w/m$i") | grep '^<' | awk '{print $2}' | sort | uniq -c | sed 's/^/    bucket /' | head -20
  [ "$moved" != 0 ] || { echo "  order inside a package:"; diff "$w/m1" "$w/m$i" | grep '^<' | awk '{print $2, $3}' | sort | uniq -c | sort -rn | head -10; }
done
exit $rc
