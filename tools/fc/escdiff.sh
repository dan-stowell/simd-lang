#!/usr/bin/env bash
# The oracle for fc's escape analysis (driver/fc/escape*.go): the stock compiler's own decisions.
#   tools/fc/escdiff.sh [-s pkg,pkg,...] <packages...>      (e.g. std; run it under taskset on a shared machine)
# Stock side: `go build -gcflags='-m -l'` (inlining off, as fc has none) prints a line per decision.
# fc side: `build/fcesc esc` (FC_TAG=esc driver/mkfc.sh; FC=... names another binary) prints its own in the
# same form. Per package the two are compared position by position:
#   UNSOUND    stock sends it to the heap (moved to heap / escapes to heap / leaking param), fc does not.
#              Must be zero but for the classes listed as exceptions below.
#   IMPRECISE  stock keeps it on the stack, fc sends it to the heap: a count per category.
# Output: totals, a line for each package named with -s, and in $ESCDIFF_OUT (default build/escdiff) the
# files stock.txt, fc.txt, unsound.txt, imprecise.txt (every difference, one per line).
here=$(cd "$(dirname "$0")/../.." && pwd)
out=${ESCDIFF_OUT:-$here/build/escdiff}; mkdir -p "$out"
fcbin=${FC:-$here/build/fcesc}
show=
if [ "$1" = -s ]; then show=$2; shift 2; fi
[ $# -gt 0 ] || { echo "usage: tools/fc/escdiff.sh [-s pkg,pkg] <packages...>"; exit 2; }
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=
pkgs=$(go list "$@" | grep -v '^unsafe$') || exit 2
go list -deps -json $pkgs > "$out/plan.json" || exit 2
go build -gcflags='-m -l' $pkgs 2> "$out/stock.txt"
"$fcbin" esc -plan "$out/plan.json" $pkgs > "$out/fc.txt" 2> "$out/fc.err" || { echo "fc esc failed:"; tail -5 "$out/fc.err"; exit 1; }
python3 - "$out" "$PWD" "$show" <<'P'
import sys, re, os, collections
out, cwd, show = sys.argv[1], sys.argv[2], [s for s in sys.argv[3].split(',') if s]
line_re = re.compile(r'^(.+?):(\d+):(\d+): (.*)$')

def parse(path):
    pkgs = collections.OrderedDict()
    cur = None
    for l in open(path, errors='replace'):
        l = l.rstrip('\n')
        if l.startswith('# '):
            cur = pkgs.setdefault(l[2:].split(' ')[0], [])
            continue
        m = line_re.match(l)
        if not m or cur is None:
            continue
        f = m.group(1)
        if not os.path.isabs(f):
            f = os.path.normpath(os.path.join(cwd, f))
        cur.append(((f, int(m.group(2)), int(m.group(3))), m.group(4)))
    return pkgs

stock, fc = parse(out + '/stock.txt'), parse(out + '/fc.txt')

# A package's output includes the instantiations it makes of other packages' generic functions, at their
# positions: those lines belong with the package that declares the function.
import json
dirpkg = {}
dec = json.JSONDecoder()
text = open(out + '/plan.json').read()
i = 0
while i < len(text):
    while i < len(text) and text[i].isspace():
        i += 1
    if i >= len(text):
        break
    o, i = dec.raw_decode(text, i)
    if o.get('Dir'):
        dirpkg[o['Dir']] = o['ImportPath']
routed = collections.OrderedDict((p, []) for p in stock)
foreign = set()
for pkg, lines in stock.items():
    for pos, t in lines:
        owner = dirpkg.get(os.path.dirname(pos[0]), pkg)
        if owner == pkg:
            routed[pkg].append((pos, t))
            continue
        m = re.match(r'^moved to heap: [^ ]*\.([^. ]+)$', t)
        if m:
            t = 'moved to heap: ' + m.group(1)
        if (pos, t) not in foreign:
            foreign.add((pos, t))
            routed.setdefault(owner, []).append((pos, t))
stock = routed
res_re = re.compile(r'^leaking param: (\S+) to result (\S+) level=(\d+)$')

class Side:
    def __init__(self):
        self.vars = {}                                  # pos -> (name, tags)
        self.heap = collections.defaultdict(list)      # pos -> texts of sites that escape
        self.stack = collections.defaultdict(list)     # pos -> texts of sites that do not
        self.param = {}                                 # pos -> {'name', 'heap': derefs or -1, 'res': {name: level}, 'tags'}
        self.noesc = collections.defaultdict(list)     # stock: "x does not escape" (site or parameter)
        self.gen = set()

def P(side, pos, name, tags=''):
    return side.param.setdefault(pos, {'name': name, 'heap': -1, 'res': {}, 'tags': tags})

def load_fc(lines):
    s = Side()
    for pos, text in lines:
        m = re.match(r'^(.*) \[(var|site|param)([^\]]*)\]$', text)
        if not m:
            continue
        msg, kind, tags = m.group(1), m.group(2), m.group(3)
        if ' gen' in tags:
            s.gen.add(pos)
        if kind == 'var':
            s.vars[pos] = (msg[len('moved to heap: '):], tags.strip())
        elif kind == 'site':
            if ' escapes to heap' in msg:
                s.heap[pos].append(msg)
            else:
                s.stack[pos].append(msg)
        else:
            r = res_re.match(msg)
            if r:
                P(s, pos, r.group(1), tags)['res'][r.group(2)] = int(r.group(3))
            elif msg.startswith('leaking param content: '):
                P(s, pos, msg[23:], tags)['heap'] = 1
            elif msg.startswith('leaking param: '):
                P(s, pos, msg[15:], tags)['heap'] = 0
            else:
                P(s, pos, msg[:-len(' does not escape')], tags)
    return s

def load_stock(lines):
    s = Side()
    n = 0
    seen = set()
    for pos, text in lines:
        if (pos, text) in seen and not text.endswith(' escapes to heap') and not text.endswith(' does not escape'):
            continue
        seen.add((pos, text))
        r = res_re.match(text)
        if r:
            P(s, pos, r.group(1))['res'][r.group(2)] = int(r.group(3))
        elif text.startswith('moved to heap: '):
            s.vars[pos] = (text[15:], '')
        elif text.startswith('leaking param content: '):
            P(s, pos, text[23:])['heap'] = 1
        elif text.startswith('leaking param: '):
            P(s, pos, text[15:])['heap'] = 0
        elif text.endswith(' escapes to heap'):
            s.heap[pos].append(text)
        elif text.endswith(' does not escape'):
            s.noesc[pos].append(text)
        else:
            continue
        n += 1
    return s, n

def fmt(pos):
    return '%s:%d:%d' % pos

tot = collections.Counter()
per = {}
unsound_f, imprecise_f = open(out + '/unsound.txt', 'w'), open(out + '/imprecise.txt', 'w')
missing = []
for pkg, slines in stock.items():
    if pkg not in fc:
        missing.append(pkg)
        continue
    c = collections.Counter()
    S, n = load_stock(slines)
    F = load_fc(fc[pkg])
    c['stock decisions'] = n
    def unsound(cls, pos, what):
        c['UNSOUND ' + cls] += 1
        unsound_f.write('%s\t%s\t%s: %s\n' % (pkg, cls, fmt(pos), what))
    def imprecise(cls, pos, what):
        c['IMPRECISE ' + cls] += 1
        imprecise_f.write('%s\t%s\t%s: %s\n' % (pkg, cls, fmt(pos), what))
    # variables
    for pos, (name, _) in S.vars.items():
        if pos in F.vars:
            continue
        if name[:1] in '#.~' or name == '':
            c['EXCEPTION variable the stock compiler made up'] += 1
            continue
        unsound('variable in frame', pos, 'moved to heap: ' + name)
    for pos, (name, tags) in F.vars.items():
        if pos in S.vars:
            continue
        if 'gen' in tags.split():
            c['generic, not compared'] += 1
            continue
        why = 'captured by a closure' if 'captured' in tags else ('too large' if 'large' in tags else 'address taken')
        imprecise('variable, ' + why, pos, 'moved to heap: ' + name)
    # sites. At one position there may be several (a slice literal converted to an interface); in a generic
    # function the stock compiler reports once per instantiation.
    allpos = set(S.heap) | set(S.noesc) | set(F.heap) | set(F.stack)
    for pos in allpos:
        if pos in S.param or pos in F.param:
            continue
        sh, sn, fh, fs = S.heap.get(pos, []), S.noesc.get(pos, []), F.heap.get(pos, []), F.stack.get(pos, [])
        if pos in F.gen:
            if sh and not fh:
                unsound('site in frame' if fs else 'site unknown to fc', pos, sh[0])
            elif fh and sn and not sh:
                imprecise('site: ' + re.sub(r' escapes to heap', '', fh[0]), pos, sn[0])
            elif not sh and not sn:
                c['generic, not compared'] += 1
            continue
        if len(sh) > len(fh):
            for t in sh[len(fh):]:
                unsound('site in frame' if fs else 'site unknown to fc', pos, t)
        elif len(fh) > len(sh):
            if not sh and not sn:
                c['fc site the stock compiler does not report'] += len(fh)
                continue
            for i in range(min(len(fh) - len(sh), max(len(sn), 0))):
                imprecise('site: ' + re.sub(r' escapes to heap', '', fh[-1 - i]), pos, sn[i])
    # parameters
    for pos, sp in S.param.items():
        fp = F.param.get(pos)
        if fp is None:
            unsound('parameter unknown to fc', pos, 'leaking param %s' % sp['name'])
            continue
        fh = fp['heap']
        if sp['heap'] == 0 and fh != 0:
            unsound('parameter leak', pos, 'leaking param: %s (fc: %s)' % (sp['name'], fp))
        elif sp['heap'] == 1 and fh < 0:
            unsound('parameter leak', pos, 'leaking param content: %s (fc: %s)' % (sp['name'], fp))
        elif sp['heap'] == 1 and fh == 0:
            imprecise('parameter: content leaks in stock, itself in fc', pos, sp['name'])
        for r, lvl in sp['res'].items():
            fl = fp['res'].get(r, -1)
            if fl < 0 and r.startswith('~b'):
                fl = fp['res'].get('~r' + r[2:], -1)
            if 0 <= fl <= lvl or 0 <= fh <= lvl:
                if not (0 <= fl <= lvl) and sp['heap'] < 0:
                    imprecise('parameter: to result in stock, to heap in fc', pos, sp['name'])
                elif 0 <= fl < lvl:
                    imprecise('parameter: lower level to result', pos, sp['name'])
                continue
            unsound('parameter leak to result', pos, 'leaking param: %s to result %s level=%d (fc: %s)' % (sp['name'], r, lvl, fp))
    for pos, fp in F.param.items():
        if pos in S.param or 'gen' in fp['tags'].split():
            continue
        if pos in S.noesc:
            if fp['heap'] >= 0 or fp['res']:
                kind = 'leaks' if fp['heap'] == 0 else ('content leaks' if fp['heap'] > 0 else 'flows to result')
                if 'moved' in fp['tags']:
                    kind = 'is a heap variable'
                imprecise('parameter: does not escape in stock, %s in fc' % kind, pos, fp['name'])
    per[pkg] = c
    tot.update(c)

def report(title, c):
    print('%s: %d stock decisions compared' % (title, c['stock decisions']))
    u = sum(v for k, v in c.items() if k.startswith('UNSOUND'))
    i = sum(v for k, v in c.items() if k.startswith('IMPRECISE'))
    print('  UNSOUND %d   IMPRECISE %d' % (u, i))
    for k, v in sorted(c.items(), key=lambda kv: (kv[0].split(' ')[0], -kv[1])):
        if k != 'stock decisions':
            print('    %6d  %s' % (v, k))

print('%d packages' % len(per))
if missing:
    print('not analyzed by fc: ' + ' '.join(missing))
report('total', tot)
for p in show:
    if p in per:
        report(p, per[p])
    else:
        print('%s: no decisions' % p)
P
