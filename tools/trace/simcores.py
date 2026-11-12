#!/usr/bin/env python3
"""What the build as it stands would take on N cores: replay the measured work over the real graph.

    simcores.py <trace.json of a build without -headstart> [N ...]

Model, from one traced build: a package's front end is one thread for as long as it took to release its
importers (start to export); its back end is the rest of its CPU, spread over at most the -c it was given;
a front end may start when the front ends it imports are out; the plan comes first, the link (serial, its
measured wall clock) and the sync come last. Front ends are scheduled first, longest chain above them
first; back ends share what is left. No model of memory bandwidth or of the kernel: an optimistic bound.
The 8-core row is the check on the model: it should land near the measured build."""
import json, sys
tr = json.load(open(sys.argv[1]))
Ns = [int(x) for x in sys.argv[2:]] or [8, 16, 32, 64, 10**6]
ph = {p['name'].split(' ')[0]: p for p in tr['phases']}
plan = ph['plan']['end_ms'] - ph['plan']['start_ms']
link = ph['link']['end_ms'] - ph['link']['start_ms']
write = ph['write']['end_ms'] - ph['write']['start_ms']
acts = {a['pkg']: a for a in tr['actions'] if a['kind'] != 'link'}
fe = {}; be = {}; cc = {}
for p, a in acts.items():
    f = max((a.get('export_ms') or a['end_ms']) - a['start_ms'], 0.5)
    cpu = a['user_ms'] + a['sys_ms']
    fe[p] = min(f, max(cpu, 0.5))          # a front end cannot have used more CPU than the process did
    be[p] = max(cpu - fe[p], 0.0)
    cc[p] = max(a.get('c', 1), 1)
deps = {p: [d for d in a.get('deps', []) if d in acts] for p, a in acts.items()}
users = {p: [] for p in acts}
for p, ds in deps.items():
    for d in ds: users[d].append(p)
memo = {}
def chain(p):
    if p not in memo:
        memo[p] = fe[p] + max((chain(u) for u in users[p]), default=0.0)
    return memo[p]
sys.setrecursionlimit(100000)
for p in acts: chain(p)

def sim(N, release=1.0, dt=2.0):
    """release: the fraction of a front end after which its importers may start (1.0 = today: the whole
    front end; 0.15 = a declarations-first export costing 15% of the front end)."""
    t = plan
    waiting = {p: len(ds) for p, ds in deps.items()}
    ready = [p for p in acts if waiting[p] == 0]
    fe_left = dict(fe); be_left = {}; released = set()
    running_fe = []; done = 0; total = len(acts)
    while done < total:
        ready.sort(key=lambda p: -memo[p])
        while ready and len(running_fe) < N:
            running_fe.append(ready.pop(0))
        free = N - len(running_fe)
        for p in list(running_fe):
            fe_left[p] -= dt
            if p not in released and fe_left[p] <= fe[p] * (1 - release):
                released.add(p)
                for u in users[p]:
                    waiting[u] -= 1
                    if waiting[u] == 0: ready.append(u)
            if fe_left[p] <= 0:
                running_fe.remove(p)
                if be[p] > 0: be_left[p] = be[p]
                else: done += 1
        if be_left and free > 0:
            want = sum(cc[p] for p in be_left)
            share = min(1.0, free / want)
            for p in list(be_left):
                be_left[p] -= cc[p] * share * dt
                if be_left[p] <= 0:
                    del be_left[p]; done += 1
        t += dt
    return t

cpu = sum(fe.values()) + sum(be.values())
crit = max(memo.values())
print(f"plan {plan:,.0f} ms, link {link:,.0f} ms, sync {write:,.0f} ms; compile work {cpu/1000:,.1f} core-s "
      f"(front ends {sum(fe.values())/1000:,.1f}, back ends {sum(be.values())/1000:,.1f}); longest chain of front ends {crit:,.0f} ms")
print("| cores | whole build ms, as it stands | whole build ms, if importers were released after 15% of a front end |")
print("|---|---|---|")
for N in Ns:
    a, b = sim(N), sim(N, 0.15)
    print(f"| {'unlimited' if N >= 10**6 else N} | {a + link + write:,.0f} | {b + link + write:,.0f} |")
# the chain that bounds the build when cores are plentiful
p = max(memo, key=memo.get); path = []
while True:
    path.append(p)
    nxt = max(users[p], key=lambda u: memo[u], default=None)
    if nxt is None: break
    p = nxt
big = sorted(path, key=lambda q: -fe[q])[:8]
print("\nlongest chain of front ends: %d packages; the eight longest on it: " % len(path) + ", ".join(f"{q.split('/')[-2] + '/' + q.split('/')[-1] if '/' in q else q} {fe[q]:,.0f} ms" for q in big))

