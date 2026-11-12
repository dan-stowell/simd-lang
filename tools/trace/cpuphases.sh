#!/usr/bin/env bash
# Rung F0: where the compile row's CPU goes, exactly. One warm MinIO build of the current configuration with
# SB_CPUPHASES set: every compiler process logs its CPU time and page faults at each phase boundary.
#   cpuphases.sh [extra sb flags]   -> prints the table; raw files in /dev/shm/sbcpu
here=$(cd "$(dirname "$0")/../.." && pwd); t=/dev/shm/sbcpu; rm -rf $t; mkdir -p $t
hp0=$(cat /proc/sys/vm/nr_hugepages); sudo sysctl -q -w vm.nr_hugepages=3072; trap 'sudo sysctl -q -w vm.nr_hugepages=$hp0' EXIT
systemd-run --user --scope -q -p MemoryMax=22G env GOGC=off SB_BASELINE=1 SB_HUGE=1 SB_CPUPHASES=$t "$here/build/sb" -C /home/exedev/targets/minio -o $t/bin -trace $t/trace.json \
  -plan=self -decl "$here/build/godecl" -compiler "$here/build/compile-sb" -linker "$here/build/link-sb" -pipeline -shard 3000000 -backends 2000 "-gcflags=-dwarf=false -l" "-ldflags=-s -w" "$@" . | tail -3
python3 - $t <<'PY'
import glob, sys, json, collections
cpu = collections.Counter(); sysc = collections.Counter(); flt = collections.Counter(); n = 0; last_total = 0
for f in glob.glob(sys.argv[1] + '/*.txt'):
    rows = [l.split() for l in open(f)][1:]
    if not rows: continue
    n += 1
    prev = ('(before main)', 0, 0, 0)
    for r in rows:
        label, start, u, s, m = r[0] if len(r) == 5 else '', r[-4], int(r[-3]), int(r[-2]), int(r[-1])
        cpu[prev[0]] += u - prev[1]; sysc[prev[0]] += s - prev[2]; flt[prev[0]] += m - prev[3]
        prev = (label if start == 'true' else '(between phases)', u, s, m)
    last_total += prev[1] + prev[2]
tr = json.load(open(sys.argv[1] + '/trace.json'))
acts = [a for a in tr['actions'] if a['kind'] != 'link']
allcpu = sum(a['user_ms'] + a['sys_ms'] for a in acts) / 1000
print(f'{n} compiler processes; compile row {allcpu:.1f} core-s (compilers, assembler, pack)')
print(f'{"phase":52s} {"user s":>7s} {"sys s":>6s} {"faults":>9s}')
for k in sorted(cpu, key=lambda k: -(cpu[k] + sysc[k])):
    print(f'{k:52s} {cpu[k]/1e6:7.1f} {sysc[k]/1e6:6.1f} {flt[k]:9d}')
print(f'{"(exit, asm, pack)":52s} {allcpu - last_total/1e6:7.1f}')
rows = [(k, cpu[k] / 1e6, sysc[k] / 1e6, flt[k]) for k in sorted(cpu, key=lambda k: -(cpu[k] + sysc[k])) if cpu[k] + sysc[k] > 50000]
rows.append(('(exit, asm, pack)', allcpu - last_total / 1e6, 0.0, 0))
json.dump({'procs': n, 'row': allcpu, 'rows': rows}, open('/home/exedev/targets/traces/minio/sb-cpuphases.json', 'w'))
PY
