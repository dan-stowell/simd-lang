#!/usr/bin/env bash
# The oracle's second number (PLAN.md): core-seconds per MB of source on the packages fc compiles, next to
# the stock baseline mode's number on the same packages (the frozen path: build/compile-sb, SB_BASELINE=1,
# GOGC=off, -dwarf=false -l).
#   tools/fc/cost.sh [package list file]        (default: tests/fc/passing.txt)
# Stock side: each package's compile command, taken from a real `go build`, is run with the baseline
# compiler under /usr/bin/time; user+sys are summed. fc side: one process (fc build) type-checks the
# packages' whole dependency closure and compiles the listed packages; it reports the time attributable to
# the listed packages (their parse + check + compile) and the whole process's CPU.
here=$(cd "$(dirname "$0")/../.." && pwd)
list=${1:-$here/tests/fc/passing.txt}
out=${FC_COST:-$here/build/fc-cost}; rm -rf "$out"; mkdir -p "$out"
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=
pkgs=$(grep -v '^#' "$list" | tr '\n' ' ')
go list -deps -json $pkgs runtime internal/runtime/maps > "$out/plan.json" || exit 2
# --- stock baseline mode: time each listed package's compile
cat > "$out/wrap.sh" <<'W'
#!/usr/bin/env bash
tool=$1; shift
if [ "$(basename "$tool")" != compile ]; then exec "$tool" "$@"; fi
pkg=; prev=; for a in "$@"; do [ "$prev" = -p ] && pkg=$a; prev=$a; done
[ -n "$pkg" ] || exec "$tool" "$@"
case " $COST_PKGS " in *" $pkg "*) ;; *) exec "$tool" "$@";; esac
t=$(mktemp)
env GOGC=off SB_BASELINE=1 /usr/bin/time -o "$t" -f "%U %S" "$COST_COMPILER" "$@"; rc=$?
echo "$pkg $(cat "$t")" >> "$COST_LOG"; rm -f "$t"
exit $rc
W
chmod +x "$out/wrap.sh"
for round in 1 2 3; do
  COST_PKGS="$pkgs" COST_COMPILER="$here/build/compile-sb" COST_LOG="$out/stock.$round" GOCACHE=$(mktemp -d /home/exedev/targets/gocache.XXXXXX) \
    bash -c 'go build -toolexec="'$out'/wrap.sh" "-gcflags=all=-dwarf=false -l" '"$pkgs"' 2>&1 | tail -3; rm -rf "$GOCACHE"'
  "$here/build/fc" build -plan "$out/plan.json" -o "$out/objs" -stats "$out/fc.stats.$round" $pkgs > "$out/fc.$round" 2>&1
done
python3 - "$out" <<'P'
import sys, glob, statistics
out = sys.argv[1]
stock = []
for f in sorted(glob.glob(out + '/stock.[0-9]')):
    stock.append(sum(float(l.split()[-2]) + float(l.split()[-1]) for l in open(f)))
fc_attr, fc_total, mb, n = [], [], 0, 0
for f in sorted(glob.glob(out + '/fc.[0-9]')):
    for l in open(f):
        if l.startswith('compiled packages only'):
            fc_attr.append(float(l.split('=')[1].split()[0])); mb = float(l.split('for')[1].split()[0])
        if l.startswith('total:'):
            fc_total.append(float(l.split(',')[1].split()[0]))
        if l.startswith('back end:'):
            n = l.split()[2] + ' of ' + l.split()[4]
med = statistics.median
print(f"packages: {n}; source: {mb:.2f} MB; three rounds each, median (min-max)")
def row(name, xs):
    print(f"  {name:<46} {med(xs):7.2f} core-s ({min(xs):.2f}-{max(xs):.2f})   {med(xs)/mb:6.2f} core-s per MB")
row('stock baseline mode (compile-sb), these packages', stock)
row('fc, attributable to these packages', fc_attr)
row('fc, whole process (checks the whole closure)', fc_total)
print(f"  ratio fc/stock: attributable {med(fc_attr)/med(stock):.3f}; whole process {med(fc_total)/med(stock):.3f}   (kill test: more than 0.333 stops the campaign)")
P
