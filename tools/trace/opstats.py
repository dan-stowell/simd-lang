#!/usr/bin/env python3
"""How much of the back end's work would a code generator cover that supports a given set of operations?
Input: the files a build writes with SB_OPSTATS=<dir> (one line per function the stock back end compiled:
name, nodes in its lowered tree, back-end nanoseconds, flags, operation counts).
    tools/trace/opstats.py <dir> [flag to exclude ...]"""
import collections, glob, sys
fns = []
for f in glob.glob(sys.argv[1] + '/*.txt'):
    for line in open(f, errors='replace'):
        p = line.rstrip('\n').split('\t')
        if len(p) < 5: continue
        ops = frozenset(x.split(':')[0] for x in p[4].split(',') if x)
        fns.append((p[0], int(p[1]), int(p[2]), frozenset(x for x in p[3].split(',') if x), ops))
excl = set(sys.argv[2:])
N = len(fns); T = sum(f[2] for f in fns); NODES = sum(f[1] for f in fns)
print(f'{N:,} functions, {NODES:,} nodes, {T / 1e9:,.1f} s of back-end time (wall inside the compilers, summed)')
fl = collections.Counter(); flt = collections.Counter()
for f in fns:
    for x in f[3]: fl[x] += 1; flt[x] += f[2]
print('flags: ' + ', '.join(f'{k} {v:,} fns / {100 * flt[k] / T:.1f}% of time' for k, v in fl.most_common()))
# every op: in how many functions, and the time of the functions that contain it
cnt = collections.Counter(); tm = collections.Counter()
for f in fns:
    for o in f[4]: cnt[o] += 1; tm[o] += f[2]
# greedy: add the operation that unlocks the most time next
supported = set(); done = [False] * N
need = [set(f[4]) for f in fns]
blocked = [bool(f[3] & excl) for f in fns]
covered_t = covered_n = 0
for i, f in enumerate(fns):
    if not need[i] and not blocked[i]: done[i] = True; covered_t += f[2]; covered_n += 1
print(f'\nexcluding functions flagged {sorted(excl) or "nothing"}; empty bodies: {covered_n:,} functions, {100 * covered_t / T:.1f}% of time')
print(f'{"#":>3} {"add operation":22} {"functions":>10} {"% functions":>12} {"% of back-end time":>20}   (the op appears in N fns)')
byop = collections.defaultdict(list)
for i, s in enumerate(need):
    for o in s: byop[o].append(i)
step = 0
remaining = set(cnt)
while remaining and step < 90:
    # choose the op that completes the most time; ties and zero gains: the op blocking the most single-op-short functions
    gain = collections.Counter(); short = collections.Counter()
    for i, s in enumerate(need):
        if done[i] or blocked[i]: continue
        if len(s) == 1: gain[next(iter(s))] += fns[i][2]
        for o in s: short[o] += fns[i][2] / len(s)
    if not short: break
    op = max(remaining, key=lambda o: (gain[o], short[o]))
    remaining.discard(op); supported.add(op); step += 1
    for i in byop[op]:
        need[i].discard(op)
        if not need[i] and not done[i] and not blocked[i]:
            done[i] = True; covered_t += fns[i][2]; covered_n += 1
    print(f'{step:>3} {op:22} {covered_n:>10,} {100 * covered_n / N:>11.1f}% {100 * covered_t / T:>19.1f}%   ({cnt[op]:,})')
