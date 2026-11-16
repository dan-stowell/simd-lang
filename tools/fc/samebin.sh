#!/usr/bin/env bash
# The oracle for a change that should only make fc faster: two compilers must give the same bytes. Builds each
# corpus project named (default: buildifier caddy minio) with both and compares the binaries.
#   tools/fc/samebin.sh <fc A> <fc B> [name from tools/scoreboard/corpus.txt ...]
# Exit 0 when every pair is equal. (Builds are the same bytes run to run since 2026-11-16: tools/fc/samebytes.sh.)
here=$(cd "$(dirname "$0")/../.." && pwd); T=/home/exedev/targets
a=$1; b=$2; shift 2
[ -x "$a" ] && [ -x "$b" ] || { echo "usage: samebin.sh <fc A> <fc B> [project ...]" >&2; exit 2; }
[ $# -gt 0 ] || set -- buildifier caddy minio
w=$(mktemp -d /tmp/fcsamebin.XXXXXX); trap 'rm -rf "$w"' EXIT
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-buildvcs=false
rc=0
for name in "$@"; do
  read _ tool dir target < <(grep -E "^$name +go " "$here/tools/scoreboard/corpus.txt")
  [ -n "$dir" ] || { echo "$name: not a go project of corpus.txt"; rc=2; continue; }
  for x in a b; do
    fc=$a; [ $x = b ] && fc=$b
    "$fc" build -C "$T/$dir" -o "$w/objs" -link "$w/bin" "$target" > "$w/log-$x" 2>&1 || { echo "$name: $fc failed: $(tail -2 "$w/log-$x")"; rc=2; }
    sha1sum < "$w/bin" > "$w/sum-$x" 2>/dev/null; rm -rf "$w/objs" "$w/bin"
  done
  if cmp -s "$w/sum-a" "$w/sum-b"; then echo "$name: equal ($(cut -c1-12 "$w/sum-a"))"; else echo "$name: DIFFERENT ($(cut -c1-12 "$w/sum-a") / $(cut -c1-12 "$w/sum-b"))"; rc=1; fi
done
exit $rc
