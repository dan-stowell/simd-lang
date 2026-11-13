#!/usr/bin/env bash
# A -toolexec wrapper that substitutes fclink for the linker and runs every other tool as it is:
#   go build -toolexec=tools/fclink/toolexec.sh ...       (FCLINK names the binary; default build/fclinklink)
# fclink accepts the go command's link line (-o, -importcfg, -buildid, -buildmode=exe, -s, -w, -extld, -X).
tool=$1; shift
if [ "$(basename "$tool")" = link ] && [ "${1:-}" != "-V=full" ]; then
  here=$(cd "$(dirname "$0")/../.." && pwd)
  fclink=${FCLINK:-$here/build/fclinklink}; [ -x "$fclink" ] || fclink=$here/build/fclink
  [ -n "${FCLINK_LOG:-}" ] && echo "$fclink $*" >> "$FCLINK_LOG"
  exec "$fclink" "$@"
fi
exec "$tool" "$@"
