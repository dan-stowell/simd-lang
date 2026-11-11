#!/usr/bin/env bash
# Where the compile row's CPU goes: a CPU profile of every compiler process of one build (per-package
# -cpuprofile), merged, reduced to the compiler's phases. Writes $TRACES/<name>/sb-phases.json.
#   phases.sh <name> <srcdir> <pkg> [sb flags...]     (same flags as the run being explained)
# Samples whose stack contains runtime.asyncPreempt are dropped: they are threads parked at nice 19 at a
# preemption point, not work (disabling async preemption does not change the build's CPU time).
set -e
name=$1; src=$2; pkg=$3; shift 3
here=$(cd "$(dirname "$0")/../.." && pwd)
TRACES=${TRACES:-/home/exedev/targets/traces}
t=$(mktemp -d /dev/shm/sbprof.XXXXXX)
args=(); gc=
for a in "$@"; do case $a in -gcflags=*) gc=${a#-gcflags=} ;; *) args+=("$a") ;; esac; done
systemd-run --user --scope -q -p MemoryMax=${SBMEM:-22G} env ${SBENV:-SB=1} "$here/build/sb" -C "$src" -o "$t/bin" \
  "${args[@]}" "-gcflags=$gc -cpuprofile=$t/%p.prof" "$pkg" > "$t/build.log"
comp=$(ls "$here"/build/compile-sb)
(cd "$t" && GOTOOLCHAIN=local go tool pprof -proto "$comp" b*.prof > merged.pb.gz 2>/dev/null)
GOTOOLCHAIN=local go tool pprof -top -cum -nodecount=4000 -ignore='runtime.asyncPreempt' "$comp" "$t/merged.pb.gz" 2>/dev/null > "$t/top.txt"
GOTOOLCHAIN=local go tool pprof -top -cum -nodecount=4000 "$comp" "$t/merged.pb.gz" 2>/dev/null > "$t/art.txt"
python3 - "$t/top.txt" "$t/art.txt" "$TRACES/$name/sb-phases.json" <<'PY'
import re, sys, json
cum = {}
tot = 0.0
def secs(x):
    m = re.match(r'([\d.]+)(ms|s|min|hrs)$', x)
    return float(m.group(1)) * {'ms': 1e-3, 's': 1, 'min': 60, 'hrs': 3600}[m.group(2)]
for line in open(sys.argv[1]):
    m = re.search(r'Total samples = ([\d.]+\w+)', line)
    if m: tot = secs(m.group(1))
    f = line.split()
    if len(f) >= 6 and f[1].endswith('%') and f[4].endswith('%'):
        cum[' '.join(f[5:])] = secs(f[3])
art = 0.0
for line in open(sys.argv[2]):
    m = re.search(r'Total samples = ([\d.]+\w+)', line)
    if m: tot = secs(m.group(1))
    f = line.split()
    if len(f) >= 6 and f[-1] == 'runtime.asyncPreempt': art = secs(f[3])
g = lambda n: cum.get(n, 0.0)
C = 'cmd/compile/internal/'
ph = {
  'lex + parse (syntax.Parse)': g(C + 'syntax.Parse'),
  'type check (types2)': g(C + 'noder.checkFiles') - g(C + 'syntax.Parse'),
  'typed tree -> export IR (write)': g(C + 'noder.writePkgStub') - g(C + 'noder.checkFiles'),
  'export IR -> compiler IR (read), imports': g(C + 'noder.LoadPackage') - g(C + 'noder.writePkgStub'),
  'escape analysis, wrappers, runtime types, init': g(C + 'gc.Main') - g(C + 'noder.LoadPackage') - g(C + 'gc.dumpobj1') - g(C + 'gc.enqueueFunc'),
  'walk + build SSA': g(C + 'ssagen.buildssa') - g(C + 'ssa.Compile') + g(C + 'gc.enqueueFunc'),
  'SSA passes (regalloc %.1f)' % g(C + 'ssa.regalloc'): g(C + 'ssa.Compile'),
  'emit machine code (genssa, assembler)': g(C + 'ssagen.Compile') - g(C + 'ssagen.buildssa'),
  'write objects and export data': g(C + 'gc.dumpobj1'),
}
json.dump(dict(total_samples_s=tot, parked_artifact_s=art, work_s=tot - art, phases=ph,
               malloc_s=g('runtime.mallocgc'), memclr_s=g('runtime.memclrNoHeapPointers'),
               dumpobj_s=g(C + 'gc.dumpobj') + g(C + 'gc.dumpobj1') - g(C + 'gc.dumpobj')), open(sys.argv[3], 'w'), indent=1)
print(json.dumps(ph, indent=1)); print('work', tot - art, 'artifact', art)
PY
cp "$t/merged.pb.gz" "$TRACES/$name/sb-phases.pb.gz"; rm -rf "$t"
