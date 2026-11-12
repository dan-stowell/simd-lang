#!/usr/bin/env python3
"""Generate a Go program of random functions inside sbc's subset, plus a main that calls each with edge
and random arguments and prints every result (or "panic").      tests/sbc_gen.py <seed> <nfuncs> > main.go
The same program built by the stock compiler alone and with sbc taking these functions must print the same."""
import random, sys
seed, nf = int(sys.argv[1]), int(sys.argv[2])
R = random.Random(seed)
INTS = {'int8': (8, True), 'uint8': (8, False), 'int16': (16, True), 'uint16': (16, False), 'int32': (32, True),
        'uint32': (32, False), 'int64': (64, True), 'uint64': (64, False), 'int': (64, True), 'uint': (64, False), 'uintptr': (64, False)}
def rng(t):
    b, s = INTS[t]
    return (-(1 << (b - 1)), (1 << (b - 1)) - 1) if s else (0, (1 << b) - 1)
def const(t):
    lo, hi = rng(t)
    v = R.choice([lo, hi, 0, 1, hi - 1, lo + 1, R.randint(lo, hi), R.randint(max(lo, -200), min(hi, 200))])
    return f'{t}({v})'
# struct types: S0..S3; fields of every int type, a bool, pointers to other structs, one nested struct by value,
# and one embedded pointer (so that selections go through implicit dereferences)
NS = 4
fields = {}   # struct -> [(name, type)]
for i in range(NS):
    fs = []
    for j, t in enumerate(R.sample(list(INTS), 7)): fs.append((f'f{j}', t))
    fs.append(('ok', 'bool'))
    fs.append(('nx', f'*S{R.randrange(NS)}'))
    if i > 0: fs.append(('in', f'S{i - 1}'))            # nested by value: offsets add up
    fs.append(('pad', '[3]uint16'))                        # something sbc cannot load, just to shift offsets
    R.shuffle(fs)
    if i < NS - 1: fs.append((f'*E{i}', None))             # embedded pointer
    fields[f'S{i}'] = fs
for i in range(NS - 1):
    fields[f'E{i}'] = [(f'e{i}a', R.choice(list(INTS))), (f'e{i}b', 'bool'), (f'e{i}p', f'*S{R.randrange(NS)}')]

def paths(st, depth=0):
    """(selector suffix, type) of everything reachable from a pointer to st in at most 3 steps"""
    out = []
    for name, t in fields[st]:
        if t is None:                                       # embedded *E: its fields are promoted
            for n2, t2 in fields[name[1:]]: out.append(('.' + n2, t2))
            continue
        if t in INTS or t == 'bool' or t.startswith('*'):
            out.append(('.' + name, t))
            if t.startswith('*') and depth < 2:
                for s2, t2 in paths(t[1:], depth + 1): out.append(('.' + name + s2, t2))
        elif t in fields and depth < 2:
            for s2, t2 in paths(t, depth + 1): out.append(('.' + name + s2, t2))
    return out

class Fn:
    def __init__(self, k):
        self.name = f'F{k}'
        self.params = []
        n = R.randint(1, 6)
        for i in range(n):
            c = R.random()
            t = f'*S{R.randrange(NS)}' if c < 0.45 else ('bool' if c < 0.55 else R.choice(list(INTS)))
            self.params.append((f'p{i}', t))
        self.method = self.params[0][1].startswith('*') and R.random() < 0.5
        c = R.random()
        self.res = 'bool' if c < 0.25 else (f'*S{R.randrange(NS)}' if c < 0.35 else R.choice(list(INTS)))
    def leaves(self, t):
        out = [p for p, pt in self.params if pt == t]
        for p, pt in self.params:
            if pt.startswith('*'):
                ps = [s for s, ft in paths(pt[1:]) if ft == t]
                if ps: out += [p + s for s in R.sample(ps, min(3, len(ps)))]
        return out
    def expr(self, t, d=0):
        lv = self.leaves(t)
        if t.startswith('*'):
            return R.choice(lv) if lv else None
        if t == 'bool':
            c = R.random()
            if d < 3 and c < 0.35:
                it = R.choice(list(INTS)); a, b = self.expr(it, d + 1, True), self.expr(it, d + 1)
                return f'({a} {R.choice(["==", "!=", "<", "<=", ">", ">="])} {b})'
            if d < 3 and c < 0.5:
                pts = [p for p, pt in self.params if pt.startswith('*')]
                if pts:
                    p = R.choice(pts); pt = dict(self.params)[p]
                    q = self.expr(pt, d + 1)
                    return f'({p} {R.choice(["==", "!="])} {R.choice(["nil", q or "nil"])})'
            if d < 3 and c < 0.7: return f'({self.expr("bool", d + 1)} {R.choice(["&&", "||"])} {self.expr("bool", d + 1)})'
            if d < 3 and c < 0.8: return f'!({self.expr("bool", d + 1)})'
            return R.choice(lv) if lv else f'({self.expr("int", d + 1, True)} != 0)'
        c = R.random()
        if d < 3 and c < 0.4:
            a = self.expr(t, d + 1, True); b = self.expr(t, d + 1)
            return f'({a} {R.choice(["+", "-", "*", "&", "|", "^", "&^"])} {b})'
        if d < 3 and c < 0.5: return f"{R.choice(["-", "^"])}({self.expr(t, d + 1, True)})"
        if d < 3 and c < 0.65:
            u = R.choice(list(INTS)); return f'{t}({self.expr(u, d + 1, True)})'
        if lv and c < 0.93: return R.choice(lv)
        return const(t)
    def expr_nonconst(self, t, d):
        for _ in range(20):
            e = self.expr(t, d)
            if any(p in e for p, _ in self.params): return e
        # no parameter of a usable type: convert one that exists
        for p, pt in self.params:
            if pt in INTS: return f'{t}({p})'
            if pt == 'bool': continue
        return None
    def body(self):
        lines = []
        for _ in range(R.randint(0, 3)):
            v = self.value()
            if v is None: return None
            lines.append(f'\tif {self.expr("bool", 1)} {{\n\t\treturn {v}\n\t}}')
        v = self.value()
        if v is None: return None
        lines.append(f'\treturn {v}')
        return '\n'.join(lines)
    def value(self):
        if self.res.startswith('*'):
            return self.expr(self.res) or 'nil'
        return self.expr(self.res, 0)
def _expr(self, t, d=0, nonconst=False):
    if nonconst and t in INTS:
        e = Fn.expr_nonconst(self, t, d)
        if e is None: raise ValueError
        return e
    return Fn._expr0(self, t, d)
Fn._expr0 = Fn.expr; Fn.expr = _expr

out = ['package main', '', 'import "unsafe"', '']
for st in sorted(fields):
    out.append(f'type {st} struct {{')
    for name, t in fields[st]: out.append(f'\t{name}' if t is None else f'\t{name} {t}')
    out.append('}')
fns = []
k = 0
while len(fns) < nf:
    f = Fn(k); k += 1
    try: b = f.body()
    except ValueError: continue
    if b is None: continue
    ps = f.params
    if f.method: sig = f'func ({ps[0][0]} {ps[0][1]}) {f.name}({", ".join(a + " " + t for a, t in ps[1:])}) {f.res}'
    else: sig = f'func {f.name}({", ".join(a + " " + t for a, t in ps)}) {f.res}'
    out.append(f'{sig} {{\n{b}\n}}')
    fns.append(f)
# objects: a few instances of each struct with random contents, some pointers nil
out.append('\nvar (')
NOBJ = 5
for st in sorted(fields):
    for i in range(NOBJ): out.append(f'\t{st.lower()}_{i} = new({st})')
out.append(')\n\nfunc setup() {')
for st in sorted(fields):
    for i in range(NOBJ):
        def fill(prefix, s, depth=0):
            for name, t in fields[s]:
                if t is None:
                    if R.random() < 0.95: out.append(f'\t{prefix}.{name[1:]} = {name[1:].lower()}_{R.randrange(NOBJ)}')
                elif t in INTS: out.append(f'\t{prefix}.{name} = {const(t)}')
                elif t == 'bool': out.append(f'\t{prefix}.{name} = {R.choice(["true", "false"])}')
                elif t.startswith('*'):
                    if R.random() < 0.95: out.append(f'\t{prefix}.{name} = {t[1:].lower()}_{R.randrange(NOBJ)}')
                elif t in fields and depth < 2: fill(f'{prefix}.{name}', t, depth + 1)
        fill(f'{st.lower()}_{i}', st)
for st in sorted(fields):
    for i in range(NOBJ): out.append(f'\tobjs = append(objs, unsafe.Pointer({st.lower()}_{i}))')
out.append('}\n')
out.append('var objs []unsafe.Pointer\n\n// pid names a pointer result by which object it is: addresses differ from build to build\nfunc pid(p unsafe.Pointer) int {\n\tif p == nil {\n\t\treturn -1\n\t}\n\tfor i, o := range objs {\n\t\tif o == p {\n\t\t\treturn i\n\t\t}\n\t}\n\treturn -2\n}\n')
out.append('func try(id int, f func()) {\n\tdefer func() {\n\t\tif recover() != nil {\n\t\t\tprintln(id, "panic")\n\t\t}\n\t}()\n\tf()\n}\n')
out.append('func main() {\n\tsetup()')
cid = 0
for f in fns:
    for _ in range(12):
        args = []
        for p, t in f.params:
            if t.startswith('*'): args.append('nil' if R.random() < 0.04 else f'{t[1:].lower()}_{R.randrange(NOBJ)}')
            elif t == 'bool': args.append(R.choice(['true', 'false']))
            else: args.append(const(t))
        call = f'{args[0]}.{f.name}({", ".join(args[1:])})' if f.method else f'{f.name}({", ".join(args)})'
        if f.method and args[0] == 'nil': call = f'(({f.params[0][1]})(nil)).{f.name}({", ".join(args[1:])})'
        cid += 1
        if f.res.startswith('*'): call = f'pid(unsafe.Pointer({call}))'
        out.append(f'\ttry({cid}, func() {{ println({cid}, {call}) }})')
out.append('}')
print('\n'.join(out))
