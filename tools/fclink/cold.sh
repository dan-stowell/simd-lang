#!/usr/bin/env bash
# fclink and `go tool link` with the inputs out of the page cache: before each link, every file the
# importcfg names (and the main object) is evicted with posix_fadvise(DONTNEED) after a sync; nothing else
# on the machine is touched (no drop_caches).   tools/fclink/cold.sh <project> [rounds=3]
here=$(cd "$(dirname "$0")/../.." && pwd)
fclink=${FCLINK:-$here/build/fclinklink}; [ -x "$fclink" ] || fclink=$here/build/fclink
name=$1; rounds=${2:-3}; d=${FC_CORPUS:-/home/exedev/targets/fc-corpus}/$name
obj=$(comm -23 <(ls "$d"/objs/*.o | sort) <(sed -n 's/^packagefile [^=]*=//p' "$d/objs/importcfg.link" | sort) | head -1)
out=${FCLINK_OUT:-/tmp/fclink-try}/bench; mkdir -p "$out"; cfg=$d/objs/importcfg.link
evict() {
  sync
  python3 -I -c '
import os, sys
n = 0
for line in open(sys.argv[1]):
    if line.startswith("packagefile "):
        f = line.split("=", 1)[1].strip()
        fd = os.open(f, os.O_RDONLY); n += os.fstat(fd).st_size
        os.posix_fadvise(fd, 0, 0, os.POSIX_FADV_DONTNEED); os.close(fd)
fd = os.open(sys.argv[2], os.O_RDONLY); os.posix_fadvise(fd, 0, 0, os.POSIX_FADV_DONTNEED); os.close(fd)
print("evicted %.0f MB of inputs" % (n / 1e6))' "$cfg" "$obj"
}
for r in $(seq 1 "$rounds"); do
  evict
  /usr/bin/time -f "fclink cold: %e s wall, %U+%S core-s" "$fclink" -stats -o "$out/$name.cold" -importcfg "$cfg" "$obj" > "$out/cold.log" 2>"$out/cold.time"
  cat "$out/cold.time"; awk '{printf "    %-10s %8s %8s\n", $1, $2, $3}' "$out/cold.log"
  evict > /dev/null
  /usr/bin/time -f "stock  cold: %e s wall, %U+%S core-s" go tool link -o "$out/$name.coldstock" -importcfg "$cfg" -buildmode=exe -s -w "$obj"
done
