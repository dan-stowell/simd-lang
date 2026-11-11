#!/usr/bin/env python3
"""Regenerate TRACE.md from the instrumented builds under $TRACES (default /home/exedev/targets/traces).

    tools/trace/stock_all.sh      # the stock builds (slow: ~30 min)
    tools/trace/mktrace.py        # this: seconds

Everything in TRACE.md comes from files written by stock.sh / census.py / hw/limits.c; nothing is typed in.
"""
import collections, datetime, json, os, re, sys

T = os.environ.get('TRACES', '/home/exedev/targets/traces')
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
TARGETS = ['minio', 'kube-apiserver']
NCPU = 8
TICKS = 3.095e9            # TSC ticks per second per core (HARDWARE.md)

# Instruction model for the floors: ticks per input byte for a data-parallel implementation.
# These are budgets, not measurements, except where noted; the trace reports them so they can be attacked.
MODEL = {
    'lex':     2.0,    # golex today: 15 ticks/byte measured (GOLEX.md); kernels in HARDWARE.md are 0.03-0.3/byte each
    'parse':   8.0,    # a handful of scans + compress passes over the token stream (4 tokens/20 bytes)
    'types':   40.0,   # name resolution = hash + gather per identifier; gathers cost 1.1 ticks each, miss L2
    'codegen': 40.0,   # stencil copy + patch per node; output ~1-2 machine bytes per source byte
}


def parse_time(s):
    # 2026-11-11T21:56:53.327818123Z -> seconds (float, enough precision relative to a base)
    m = re.match(r'(\d+)-(\d+)-(\d+)T(\d+):(\d+):(\d+)(\.\d+)?Z', s)
    y, mo, d, h, mi, se = map(int, m.groups()[:6])
    base = datetime.datetime(y, mo, d, h, mi, se, tzinfo=datetime.timezone.utc).timestamp()
    return base + float(m.group(7) or 0)


def meta(path):
    d = {}
    for line in open(path):
        k, _, v = line.rstrip('\n').partition(' ')
        d[k] = v
    return d


def tool_of(cmd):
    m = re.match(r'\S*/(compile|asm|link|pack|buildid|cover|vet)\b', cmd)
    return m.group(1) if m else 'other'


class Run:
    def __init__(self, d):
        self.dir = d
        self.meta = meta(os.path.join(d, 'meta.txt'))
        self.wall = int(self.meta['wall_ms']) / 1000
        self.cpu = float(self.meta['user_s']) + float(self.meta['sys_s'])
        self.P = int(self.meta['P'])
        self._a = None

    @property
    def actions(self):
        if self._a is None:
            self._a = json.load(open(os.path.join(self.dir, 'actions.json')))
            for a in self._a:
                if 'TimeStart' in a and a.get('TimeDone'):
                    a['t0'] = parse_time(a['TimeStart']); a['t1'] = parse_time(a['TimeDone'])
                    a['ready'] = parse_time(a['TimeReady'])
                    a['dur'] = a['t1'] - a['t0']
                else:
                    a['dur'] = 0.0
        return self._a

    def by_tool(self):
        """CPU and count per tool. The action graph only has per-action totals, so an action with compile+asm
        is attributed by its dominant tool: asm-bearing packages are listed separately."""
        cpu = collections.Counter(); real = collections.Counter(); n = collections.Counter()
        for a in self.actions:
            if not a.get('Cmd'): continue
            tools = [tool_of(c) for c in a['Cmd']]
            key = 'link' if 'link' in tools else ('compile+asm' if 'asm' in tools else 'compile')
            cpu[key] += (a.get('CmdUser', 0) + a.get('CmdSys', 0)) / 1e9
            real[key] += a.get('CmdReal', 0) / 1e9
            n[key] += 1
        return cpu, real, n

    def link(self):
        for a in self.actions:
            if a['Mode'] == 'link': return a
        return None

    def critical_path(self):
        """Longest chain of actions by measured duration (start->done), following Deps."""
        acts = {a['ID']: a for a in self.actions}
        memo = {}
        order = sorted(acts.values(), key=lambda a: a.get('t1', 0))
        for a in order:                       # deps always finish before their dependents
            best, arg = 0.0, None
            for d in a.get('Deps') or []:
                if d in memo and memo[d][0] > best: best, arg = memo[d][0], d
            memo[a['ID']] = (best + a['dur'], arg)
        end = max(memo, key=lambda i: memo[i][0])
        path = []; i = end
        while i is not None:
            path.append(acts[i]); i = memo[i][1]
        return memo[end][0], path[::-1]

    def first_start(self):
        return min(a['t0'] for a in self.actions if a.get('Cmd'))

    def busy(self):
        """[(t, busy cores)] from the 100 ms /proc/stat samples, limited to the build's window."""
        rows = []
        for line in open(os.path.join(self.dir, 'cpu.txt')):
            f = line.split()
            if len(f) < 9: continue
            v = list(map(int, f[2:10]))
            rows.append((float(f[0]), sum(v), v[3] + v[4]))
        t0, t1 = float(self.meta['t0']), float(self.meta['t1'])
        out = []
        for (ta, sa, ia), (tb, sb, ib) in zip(rows, rows[1:]):
            if tb < t0 or ta > t1 or sb == sa: continue
            out.append((tb - t0, NCPU * (1 - (ib - ia) / (sb - sa))))
        return out


def spark(vals, top):
    bars = ' ▁▂▃▄▅▆▇█'
    return ''.join(bars[max(0, min(8, round(8 * v / top)))] for v in vals)


def bucket(series, n, t_end):
    out = [[] for _ in range(n)]
    for t, v in series:
        out[min(n - 1, int(n * t / t_end))].append(v)
    return [sum(b) / len(b) if b else 0 for b in out]


def bench_split(path):
    """Sum the compiler's own -bench phase timers over every package. Returns {phase: seconds}."""
    ph = collections.Counter()
    if not os.path.exists(path): return ph
    for line in open(path, errors='replace'):
        m = re.match(r'BenchmarkCompile:\S*?:(fe|be|sym):?(\S*)\s+1\s+(\d+) ns/op', line)
        if not m:
            m2 = re.match(r'BenchmarkCompile:.*:(\w+:\w+)\s+1\s+(\d+) ns/op', line)
            if m2: ph[m2.group(1)] += int(m2.group(2)) / 1e9
            continue
        ph[m.group(1) + ':' + m.group(2)] += int(m.group(3)) / 1e9
    return ph


def limits():
    d = {}
    p = os.path.join(ROOT, 'hw', 'limits.out.txt')
    if os.path.exists(p):
        for line in open(p):
            f = line.split()
            if len(f) >= 2:
                try: d[f[0]] = float(f[1])
                except ValueError: pass
    return d


def fmt_bytes(b):
    return f'{b / 1e6:,.0f} MB' if b >= 1e6 else f'{b / 1e3:,.0f} kB'


def ms(s): return f'{s * 1000:,.0f}'


def target_section(name, L, out):
    base = os.path.join(T, name)
    runs = {}
    for P in (1, 2, 4, 8):
        d = os.path.join(base, f'p{P}-cold')
        if os.path.exists(os.path.join(d, 'meta.txt')): runs[P] = Run(d)
    if 8 not in runs:
        out.append(f'## {name}\n\n(no 8-CPU cold run yet)\n'); return None
    r = runs[8]
    cen = json.load(open(os.path.join(base, 'census.json')))
    tot = cen['total']
    warm = Run(os.path.join(base, 'p8-warm')) if os.path.exists(os.path.join(base, 'p8-warm', 'meta.txt')) else None
    src_bytes = tot['go_bytes'] + tot['s_bytes']
    in_bytes = src_bytes + tot['embed_bytes']
    binb = int(r.meta['bin_bytes'])

    out.append(f'## {name}\n')
    out.append(f"Commit `{r.meta['commit'][:12]}`, `{r.meta['go'].replace('go version ', '')}`, `CGO_ENABLED=0`, `-trimpath`. "
               f"{tot['packages']:,} packages ({tot['std_packages']} from the standard library), "
               f"{tot['go_files']:,} Go files, {tot['go_bytes'] / 1e6:.1f} MB and {tot['go_lines']:,} lines of Go, "
               f"{tot['s_files']} assembly files ({tot['s_bytes'] / 1e6:.1f} MB), {tot['embed_bytes'] / 1e6:.1f} MB embedded. "
               f"Output binary: {binb / 1e6:.1f} MB.\n")

    # --- scaling
    out.append('### Stock toolchain, cold, by CPU count\n')
    out.append('| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |')
    out.append('|---|---|---|---|---|---|---|')
    for P in sorted(runs):
        x = runs[P]; lk = x.link()
        out.append(f"| {P} | {ms(x.wall)} | {x.cpu:,.0f} | {x.cpu / x.wall:.2f} | {ms(lk['dur']) if lk else '?'} | "
                   f"{fmt_bytes(int(x.meta['disk_read_bytes']))} | {fmt_bytes(int(x.meta['disk_written_bytes']))} |")
    if warm:
        lk = warm.link()
        out.append(f"| 8, warm page cache | {ms(warm.wall)} | {warm.cpu:,.0f} | {warm.cpu / warm.wall:.2f} | {ms(lk['dur'])} | "
                   f"{fmt_bytes(int(warm.meta['disk_read_bytes']))} | {fmt_bytes(int(warm.meta['disk_written_bytes']))} |")
    out.append('')

    # --- timeline of the 8-CPU cold run
    t0 = float(r.meta['t0'])
    first = r.first_start() - t0
    lk = r.link()
    link_start = lk['t0'] - t0; link_end = lk['t1'] - t0
    cp_len, path = r.critical_path()
    cpu, real, n = r.by_tool()
    busy = r.busy()
    seg = lambda a, b: [v for t, v in busy if a <= t < b]
    avg = lambda v: sum(v) / len(v) if v else 0

    # compiler phase split (from the -bench run), applied to the cold run's compile CPU
    bs = bench_split(os.path.join(T, f'{name}.bench.txt'))
    G_START, G_FRONT, G_MID, G_BACK, G_OBJ = ('process start (init, load runtime decls)', 'lex/parse + resolve/types',
                                              'inline + escape analysis', 'lower/codegen', 'write objects')
    groups = collections.OrderedDict((g, 0.0) for g in (G_START, G_FRONT, G_MID, G_BACK, G_OBJ))
    for k, v in bs.items():
        if 'total' in k: continue
        if k in ('fe:init', 'fe:loadsys'): groups[G_START] += v
        elif k == 'fe:parse': groups[G_FRONT] += v
        elif k == 'be:compilefuncs': groups[G_BACK] += v
        elif k == 'be:dumpobj': groups[G_OBJ] += v
        else: groups[G_MID] += v
    bsum = sum(groups.values())
    comp_cpu = cpu['compile'] + cpu['compile+asm']

    out.append('### The build trace (8 CPUs, cold)\n')
    out.append('Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the '
               'phases interleave across 1,300+ processes, so they are given in core-seconds '
               '(the compiler\'s own `-bench` timers, summed over all packages, scaled to the cold run\'s compile CPU).\n')
    out.append('| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |')
    out.append('|---|---|---|---|---|---|---|')

    def floor_ms(nbytes, bw, ticks_per_byte=0.0, cores=NCPU):
        f = 0.0
        if bw: f = nbytes / bw
        f = max(f, nbytes * ticks_per_byte / (cores * TICKS))
        return f

    rows = []
    fetch = os.path.join(T, '..', f'{name}.fetch.time')
    if os.path.exists(fetch):
        fm = meta(fetch)
        out.append(f"| fetch (separate, not in total) | {ms(float(fm['fetch_s']))} | | | {fmt_bytes(int(fm.get('modcache_bytes', 0)))} on disk | | |")
    else:
        out.append('| fetch (separate, not in total) | 0 (vendored in the repository) | | | | | |')
    disk_bw = L.get('smallfiles_cold_8t_bytes_per_s', 0); mem_bw = L.get('memcpy_8t_bytes_per_s', 0)
    dread = int(r.meta['disk_read_bytes'])
    cold_penalty = (r.wall - warm.wall) if warm else None
    f_read = floor_ms(in_bytes, disk_bw)
    out.append(f"| read inputs | {ms(cold_penalty) + ' (cold minus warm)' if cold_penalty is not None else '?'} | | | "
               f"{fmt_bytes(dread)} read from disk; sources are {fmt_bytes(in_bytes)} | {ms(f_read)} | "
               f"{cold_penalty / f_read:,.0f}x |" if cold_penalty and f_read else
               f"| read inputs | ? | | | {fmt_bytes(dread)} | {ms(f_read)} | |")
    out.append(f"| load package graph (`go build` before the first compile) | {ms(first)} | | {avg(seg(0, first)):.1f} | | | |")
    comp_wall = link_start - first
    out.append(f"| **compile stretch** (first compile to link start) | {ms(comp_wall)} | {comp_cpu:,.0f} | {avg(seg(first, link_start)):.1f} | "
               f"{fmt_bytes(src_bytes)} source in, {fmt_bytes(int(r.meta['gocache_bytes']))} of objects out | | |")
    if bsum:
        # The timers are wall clock inside each compiler process. Every phase but compilefuncs is single-threaded,
        # so its wall clock is its CPU; compilefuncs (-c workers, plus the garbage collector) gets the rest of the CPU.
        serial = sum(v for g, v in groups.items() if g != G_BACK)
        cs_of = {g: v for g, v in groups.items()}
        cs_of[G_BACK] = max(0.0, comp_cpu - serial)
        floors = {G_FRONT: floor_ms(tot['go_bytes'], mem_bw, MODEL['lex'] + MODEL['parse'] + MODEL['types']),
                  G_BACK: floor_ms(tot['go_bytes'], mem_bw, MODEL['codegen'])}
        for g in groups:
            cs = cs_of[g]
            if g in floors:
                out.append(f"| &nbsp;&nbsp;{g} | | {cs:,.1f} | | {fmt_bytes(tot['go_bytes'])} of Go | {ms(floors[g])} | {cs / NCPU / floors[g]:,.0f}x (core-s / 8 against the floor) |")
            else:
                out.append(f"| &nbsp;&nbsp;{g} | | {cs:,.1f} | | | 0 (not needed) | |")
        rows.append(sum(floors.values()))
    f_link = floor_ms(binb, mem_bw)
    lcpu = (lk.get('CmdUser', 0) + lk.get('CmdSys', 0)) / 1e9
    out.append(f"| link | {ms(lk['dur'])} | {lcpu:.1f} | {avg(seg(link_start, link_end)):.1f} | {fmt_bytes(binb)} binary | {ms(f_link)} | {lk['dur'] / f_link:,.0f}x |" if f_link else
               f"| link | {ms(lk['dur'])} | {lcpu:.1f} | {avg(seg(link_start, link_end)):.1f} | {fmt_bytes(binb)} binary | | |")
    wbw = L.get('disk_write_bytes_per_s', 0)
    f_write = floor_ms(binb, wbw)
    out.append(f"| write output | (inside link) | | | {fmt_bytes(binb)}; the build wrote {fmt_bytes(int(r.meta['disk_written_bytes']))} in all | {ms(f_write)} | |")
    tail = r.wall - link_end
    out.append(f"| after link (install, exit) | {ms(tail)} | | | | | |")
    f_total = f_read + (rows[0] if rows else 0) + f_link + f_write
    out.append(f"| **TOTAL** | **{ms(r.wall)}** | **{r.cpu:,.0f}** | **{r.cpu / r.wall:.2f}** | | **{ms(f_total)}** | **{r.wall / f_total:,.0f}x** |" if f_total else
               f"| **TOTAL** | **{ms(r.wall)}** | **{r.cpu:,.0f}** | **{r.cpu / r.wall:.2f}** | | | |")
    out.append(f"| critical path (longest dependency chain, {len([a for a in path if a.get('Cmd')])} actions) | {ms(cp_len)} | | | | | |")
    out.append('')

    if bs:
        out.append("The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages "
                   "(`-bench` run, 8 CPUs, warm page cache): " +
                   ', '.join(f'{k} {v:.1f}' for k, v in sorted(bs.items(), key=lambda kv: -kv[1]) if 'total' not in k) +
                   ". Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.\n")

    # --- cores over time
    nb = 60
    out.append('### Cores busy over time (8 CPUs, cold)\n')
    out.append('```')
    out.append(f'8 |{spark(bucket(busy, nb, r.wall), 8)}|   one column = {r.wall / nb * 1000:,.0f} ms')
    out.append(f'0 +{"-" * nb}+   load graph ends {ms(first)} ms, link {ms(link_start)}-{ms(link_end)} ms')
    out.append('```')
    hist = collections.Counter(min(8, int(v + 0.5)) for _, v in busy)
    tot_s = sum(hist.values())
    out.append('Share of wall clock by number of busy cores: ' +
               ', '.join(f'{k}: {100 * hist[k] / tot_s:.0f}%' for k in range(0, 9) if hist[k]) + '. '
               f'Idle core-seconds: {NCPU * r.wall - r.cpu:,.0f} of {NCPU * r.wall:,.0f}.\n')

    # --- critical path
    out.append('### Critical path through the package graph (8 CPUs, cold)\n')
    out.append(f'The longest chain of actions, each waiting for the one before: {ms(cp_len)} ms of the {ms(r.wall)} ms wall clock. '
               'With unlimited cores and this toolchain the build could not be shorter than this.\n')
    out.append('| # | package | action | start ms | ms | waited for a core, ms | Go source |')
    out.append('|---|---|---|---|---|---|---|')
    shown = [a for a in path if a.get('Cmd')]
    big = sorted(shown, key=lambda a: -a['dur'])[:14]
    for i, a in enumerate(shown):
        if a not in big: continue
        pk = cen['packages'].get(a['Package'], {})
        out.append(f"| {i + 1} | `{a['Package']}` | {a['Mode']} | {ms(a['t0'] - t0)} | {ms(a['dur'])} | {ms(a['t0'] - a['ready'])} | "
                   f"{fmt_bytes(pk.get('go_bytes', 0)) if pk else ''} |")
    out.append(f"\n({len(shown)} actions on the path; the 14 longest shown.)\n")

    # --- biggest actions
    out.append('### The ten most expensive actions (8 CPUs, cold)\n')
    out.append('| package | action | wall ms | core-s | Go source |')
    out.append('|---|---|---|---|---|')
    for a in sorted([a for a in r.actions if a.get('Cmd')], key=lambda a: -(a.get('CmdUser', 0) + a.get('CmdSys', 0)))[:10]:
        pk = cen['packages'].get(a['Package'], {})
        out.append(f"| `{a['Package']}` | {a['Mode']} | {ms(a['dur'])} | {(a.get('CmdUser', 0) + a.get('CmdSys', 0)) / 1e9:.1f} | {fmt_bytes(pk.get('go_bytes', 0)) if pk else ''} |")
    out.append('')
    return dict(run=r, tot=tot, floor=f_total, binb=binb, cp=cp_len)


def sb_runs(name):
    base = os.path.join(T, name)
    if not os.path.isdir(base): return []
    out = []
    for d in sorted(os.listdir(base)):
        if d.startswith('sb-') and not d.endswith('-warm') and os.path.exists(os.path.join(base, d, 'trace.json')):
            m = meta(os.path.join(base, d, 'meta.txt'))
            if m.get('rc') == '0': out.append((d[3:], os.path.join(base, d), m))
    return out


def sb_busy(d, m):
    rows = []
    for line in open(os.path.join(d, 'cpu.txt')):
        f = line.split()
        if len(f) < 9: continue
        v = list(map(int, f[2:10])); rows.append((float(f[0]), sum(v), v[3] + v[4]))
    t0 = float(m['t0']); out = []
    for (ta, sa, ia), (tb, sb, ib) in zip(rows, rows[1:]):
        if sb != sa: out.append((tb - t0, NCPU * (1 - (ib - ia) / (sb - sa))))
    return out


def sb_chain(tr):
    """The chain that decides when the build can end. Walk back from the package that finished last: each
    step goes to the dependency whose release (its export data, or its exit when not pipelining) came last.
    The chain's length is what those packages needed before releasing the next one (front ends, or whole
    compiles), plus the whole of the last package, plus the link: no number of cores shortens it."""
    comp = {a['pkg']: a for a in tr['actions'] if a['kind'] != 'link'}
    link = [a for a in tr['actions'] if a['kind'] == 'link']
    if not link: return 0, []
    rel = lambda a: a.get('export_ms') or a['end_ms']
    cur = max(comp.values(), key=lambda a: a['end_ms'])
    path = [dict(link[0], need=link[0]['end_ms'] - link[0]['start_ms']), dict(cur, need=cur['end_ms'] - cur['start_ms'])]
    while cur.get('deps'):
        nxt = max((comp[d] for d in cur['deps'] if d in comp), key=rel, default=None)
        if nxt is None: break
        path.append(dict(nxt, need=rel(nxt) - nxt['start_ms'])); cur = nxt
    return sum(a['need'] for a in path), path[::-1]


def sb_section(name, L, out, stock):
    runs = sb_runs(name)
    if not runs: return
    label, d, m = runs[-1]
    tr = json.load(open(os.path.join(d, 'trace.json')))
    cen = json.load(open(os.path.join(T, name, 'census.json')))['total']
    busy = sb_busy(d, m)
    wall = int(m['wall_ms']) / 1000
    cpu = float(m['user_s']) + float(m['sys_s'])
    mem = L.get('memcpy_8t_bytes_per_s', 0); small = L.get('smallfiles_cold_8t_bytes_per_s', 0); wbw = L.get('disk_write_bytes_per_s', 0)
    gb = cen['go_bytes']; inb = gb + cen['s_bytes'] + cen['embed_bytes']; binb = int(m['bin_bytes'])
    tick = lambda n, t: n * t / (NCPU * TICKS)
    objb = next((ph['bytes'] for ph in tr['phases'] if ph['name'].startswith('compile')), 0)
    floors = {'plan + read': inb / small if small else 0, 'declarations': max(tick(gb, MODEL['lex']), gb / mem if mem else 0),
              'plan': max(tick(gb, MODEL['lex']), gb / mem if mem else 0), 'read': inb / small if small else 0,
              'lex': max(tick(gb, MODEL['lex']), gb / mem if mem else 0),
              'compile': tick(gb, MODEL['parse'] + MODEL['types'] + MODEL['codegen']), 'link': (objb + binb) / mem if mem else 0,
              'write': binb / wbw if wbw else 0}
    out.append(f'## {name}: the build trace (driver `sb`, 8 CPUs, cold)\n')
    out.append(f"Run `{label}` (driver at `{m.get('driver', '?')}`{', flags `' + m['flags'] + '`' if m.get('flags') else ''}). "
               f"Result: {m.get('smoke', 'no smoke test')}. "
               f"Stock `go build` on the same machine, cold: {ms(stock.wall) if stock else '?'} ms.\n")
    ws = m.get('walls', '').split()
    if len(ws) > 1:
        out.append(f"Wall clock of {len(ws)} cold runs of this configuration, ms: {', '.join(f'{int(w):,}' if w.isdigit() else w for w in ws)}. "
                   "The table is the first of them.\n")
    out.append('| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |')
    out.append('|---|---|---|---|---|---|---|---|')
    fetch = os.path.join(T, '..', f'{name}.fetch.time')
    if os.path.exists(fetch):
        fm = meta(fetch)
        out.append(f"| fetch (separate, not in total) | {ms(float(fm['fetch_s']))} | | | {fmt_bytes(int(fm.get('modcache_bytes', 0)))} | | | `go mod download` |")
    else:
        out.append('| fetch (separate, not in total) | 0 (vendored) | | | | | | |')
    ftot = 0.0
    for ph in tr['phases']:
        a, b = ph['start_ms'] / 1000, ph['end_ms'] / 1000
        seg = [v for t, v in busy if a <= t < b + 0.1]
        key = next((k for k in floors if ph['name'].startswith(k)), None)
        fl = floors.get(key, 0); ftot += fl
        who = ph['name'][ph['name'].index('(') + 1:ph['name'].index(')')] if '(' in ph['name'] else 'driver'
        nm = ph['name'].split(' (')[0]
        nm = {'plan': 'plan (package graph)', 'plan + read': 'plan (package graph) + read inputs', 'lex': 'lex (tokens; not yet consumed)',
              'declarations': '&nbsp;&nbsp;of which lex + package-level declarations', 'compile': 'parse + resolve/types + lower/codegen'}.get(nm, nm)
        out.append(f"| {nm} | {ph['end_ms'] - ph['start_ms']:,.0f} | {sum(seg) / len(seg) if seg else 0:.1f} | {(ph['user_ms'] + ph['sys_ms']) / 1000:,.1f} | "
                   f"{fmt_bytes(ph['bytes'])} | {fl * 1000:,.0f} | {(b - a) / fl:,.0f}x | {who} |" if fl else
                   f"| {nm} | {ph['end_ms'] - ph['start_ms']:,.0f} | | | {fmt_bytes(ph['bytes'])} | | | {who} |")
    cl, path = sb_chain(tr)
    out.append(f"| **TOTAL** | **{ms(wall)}** | **{cpu / wall:.2f}** | **{cpu:,.0f}** | read {fmt_bytes(int(m['disk_read_bytes']))}, wrote {fmt_bytes(int(m['disk_written_bytes']))} (disk) | **{ftot * 1000:,.0f}** | **{wall / ftot:,.0f}x** | |")
    out.append(f"| critical path ({len(path)} actions) | {cl:,.0f} | | | | | | |")
    out.append('')
    pj = os.path.join(T, name, 'sb-phases.json')
    if os.path.exists(pj):
        P = json.load(open(pj))
        comp_cpu = next(((ph['user_ms'] + ph['sys_ms']) / 1000 for ph in tr['phases'] if ph['name'].startswith('compile')), 0)
        w = P['work_s']
        out.append(f"**Inside the compile row** (`tools/trace/phases.sh`: a CPU profile of every compiler process, merged; "
                   f"{w:,.0f} s of samples for {comp_cpu:,.0f} core-s of compile, the rest being kernel time and process start):\n")
        out.append('| compiler phase | core-s | share | the charter\'s row |')
        out.append('|---|---|---|---|')
        rowof = {'lex': 'lex/parse', 'type check': 'resolve/types', 'typed tree': 'resolve/types', 'export IR': 'resolve/types',
                 'escape': 'lower/codegen', 'walk': 'lower/codegen', 'SSA': 'lower/codegen', 'emit': 'lower/codegen', 'write': 'write objects'}
        for k, v in P['phases'].items():
            out.append(f"| {k} | {v:,.1f} | {100 * v / w:.1f}% | {next((r for p, r in rowof.items() if k.startswith(p)), '')} |")
        rest = w - sum(P['phases'].values())
        out.append(f"| not under any of these (scheduler, runtime start, flag parsing) | {rest:,.1f} | {100 * rest / w:.1f}% | |")
        out.append(f"\nAcross all phases, {P['malloc_s']:,.1f} s is the allocator and {P['memclr_s']:,.1f} s is clearing memory. "
                   f"Lexing and parsing all of the build's Go is {P['phases'][next(iter(P['phases']))]:.2f} core-s: "
                   f"{100 * P['phases'][next(iter(P['phases']))] / w:.1f}% of the compiler's work.\n")
    notes = [f"{ph['name'].split(' (')[0]}: {ph['note']}" for ph in tr['phases'] if ph.get('note')]
    if notes: out.append('Notes: ' + '; '.join(notes) + '.\n')
    nb = 60
    out.append('```')
    out.append(f'8 |{spark(bucket([(t, v) for t, v in busy if t <= wall], nb, wall), 8)}|   cores busy; one column = {wall / nb * 1000:,.0f} ms')
    out.append(f'0 +{"-" * nb}+')
    out.append('```\n')
    if path:
        out.append('The chain that ends the build (each action was the last thing the next one waited for):\n')
        out.append('| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |')
        out.append('|---|---|---|---|---|---|')
        for a in sorted(path, key=lambda a: -a['need'])[:8]:
            out.append(f"| `{a['pkg']}` | {a['kind']} | {a['start_ms']:,.0f} | {a['need']:,.0f} | {a['start_ms'] - a.get('ready_ms', a['start_ms']):,.0f} | {fmt_bytes(a.get('go_bytes', 0))} |")
        out.append('')
    if len(runs) > 1 or stock:
        out.append(f'### {name}: every rung so far (8 CPUs, cold)\n')
        out.append('Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds '
                   'at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.\n')
        out.append('| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |')
        out.append('|---|---|---|---|---|---|---|---|---|---|---|')
        if stock:
            out.append(f"| stock `go build` | {ms(stock.wall)} | {stock.cpu:,.0f} | {stock.cpu / stock.wall:.2f} | | | | | {ms(stock.link()['dur'])} | | |")
        for lb, dd, mm in runs:
            t = json.load(open(os.path.join(dd, 'trace.json')))
            phd = {p['name'].split(' ')[0]: p['end_ms'] - p['start_ms'] for p in t['phases']}
            if 'lex' in phd: phd['lexdecl'] = phd['lex']
            if 'declarations' in phd: phd['lexdecl'] = phd['declarations']
            c = float(mm['user_s']) + float(mm['sys_s']); w = int(mm['wall_ms']) / 1000
            out.append(f"| `{lb}` {('(' + mm['flags'] + ')') if mm.get('flags') else ''} | {ms(w)} | {c:,.0f} | {c / w:.2f} | " +
                       ' | '.join((f"{phd[k]:,.0f}" if k in phd else ('(in plan)' if k == 'read' and 'declarations' in phd else '')) for k in ('plan', 'read', 'lexdecl', 'compile', 'link', 'write')) +
                       f" | {'pass' if 'PASS' in mm.get('smoke', '') else mm.get('smoke', 'not run')} |")
        out.append('')


def main():
    L = limits()
    out = ['# TRACE', '',
           f'Generated by `tools/trace/mktrace.py` on {datetime.datetime.now(datetime.timezone.utc):%Y-%m-%d %H:%M} UTC '
           'from the instrumented builds made by `tools/trace/stock_all.sh`. Do not edit by hand.', '',
           'Machine: 8 vCPUs (AMD EPYC 9554P under KVM), 31 GiB RAM; 2 s of wall clock is 16 core-seconds.', '']
    if L:
        out.append('Measured limits (`hw/limits.c`, `hw/limits.out.txt`): ' + ', '.join(
            f'{k.replace("_bytes_per_s", "").replace("_", " ")} {v / 1e9:.2f} GB/s' for k, v in L.items() if k.endswith('bytes_per_s')) + '.\n')
    res = {}
    stock_out = []
    for name in TARGETS:
        if os.path.exists(os.path.join(T, name)):
            res[name] = target_section(name, L, stock_out)
    for name in TARGETS:
        sb_section(name, L, out, res[name]['run'] if res.get(name) else None)
    out.append('# The stock toolchain (rung 0: the gap)\n')
    out += stock_out
    out.append('## How the floors are computed\n')
    out.append('A floor is the larger of (bytes moved / measured bandwidth of the level they move through) and '
               
               '(bytes x modelled ticks per byte / (8 cores x 3.095e9 ticks/s)). The bandwidths are measured. '
               'The ticks per byte are budgets for a data-parallel implementation, set in `tools/trace/mktrace.py`: ' +
               ', '.join(f'{k} {v:g}' for k, v in MODEL.items()) +
               '. They are the weakest numbers in this file. The link floor is one pass over the objects in and the binary out at memory bandwidth: a copy floor, which says nothing about symbol resolution or relocation (no model for those yet). For scale: the Strand Go lexer measures 15 ticks/byte today '
               '(GOLEX.md), and the stock toolchain spends about '
               + (f"{res['minio']['run'].cpu * TICKS / res['minio']['tot']['go_bytes']:,.0f}" if res.get('minio') else '?') +
               ' ticks per source byte end to end on MinIO.\n')
    m = res.get('minio')
    if m and m['floor']:
        r = m['run']
        out.append('## Does 2 seconds fit on this machine?\n')
        out.append(f"By the floors above, MinIO's irreducible work is about {m['floor'] * NCPU:,.1f} core-seconds "
                   f"({m['floor'] * 1000:,.0f} ms on 8 cores), inside the 16 core-seconds that 2 s on this VM allows, so physics does not rule the target out here. "
                   f"The stock toolchain does {r.cpu:,.0f} core-seconds of work for the same build: with it doing the work, 2 s would need "
                   f"{r.cpu / 2:,.0f} perfectly used cores, and its dependency chain ({m['cp'] * 1000:,.0f} ms) would still be in the way. "
                   f"So the gap is {r.cpu / 16:,.0f}x in work done and {m['cp'] / 2:,.0f}x in the critical path; both have to go.\n")
    open(os.path.join(ROOT, 'TRACE.md'), 'w').write('\n'.join(out) + '\n')
    print('\n'.join(out))


if __name__ == '__main__':
    main()
