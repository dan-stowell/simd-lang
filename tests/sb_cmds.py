#!/usr/bin/env python3
"""Differential test of the driver's plan: every compile/asm/link command line sb runs must be the one
`go build` runs for the same package (from -debug-actiongraph), up to work-directory names, build IDs
and the -c worker count.     tests/sb_cmds.py <sb -dumpcmds file> <stock actions.json>"""
import json, re, shlex, sys

def norm(cmd):
    a = shlex.split(cmd)
    tool = a[0].split('/')[-1]
    out = []; i = 1; pkg = None
    while i < len(a):
        x = a[i]
        if x == '-p': pkg = a[i + 1]
        if x in ('-buildid',): i += 2; continue
        if x.startswith('-buildid=') or x.startswith('-c=') or x.startswith('-extld='): i += 1; continue
        x = re.sub(r'(/tmp/go-build\d+|\$WORK)/b\d+', '$OBJ', x)
        x = re.sub(r'\$OBJ/exe/a\.out|\$WORK/a\.out', '$EXE', x)
        x = re.sub(r'\$WORK/importcfg\.link|\$OBJ/importcfg\.link', '$LINKCFG', x)
        out.append(x); i += 1
    return tool, pkg, out

def load_sb(p):
    d = {}
    for line in open(p):
        line = line.rstrip('\n')
        if not line: continue
        # sb logs argv joined by spaces without quoting; the only argument with spaces would be a path, and there are none
        t, pkg, a = norm(' '.join(shlex.quote(x) for x in line.split(' ')))
        d.setdefault((t, pkg if t != 'link' else 'LINK'), []).append(a)
    return d

def load_stock(p):
    d = {}
    for act in json.load(open(p)):
        for c in act.get('Cmd') or []:
            t, pkg, a = norm(c)
            d.setdefault((t, pkg if t != 'link' else 'LINK'), []).append(a)
    return d

sb, st = load_sb(sys.argv[1]), load_stock(sys.argv[2])
bad = 0
for k in sorted(set(sb) | set(st), key=str):
    x, y = sorted(sb.get(k, [])), sorted(st.get(k, []))
    if x != y:
        bad += 1
        if bad <= 8:
            print('DIFF', k)
            for u, v in zip(x, y):
                if u != v:
                    print('  sb   :', ' '.join(a for a in u if a not in v)[:400]); print('  stock:', ' '.join(a for a in v if a not in u)[:400])
            if len(x) != len(y): print('  count', len(x), len(y))
n = sum(len(v) for v in st.values())
print(f'{"FAIL" if bad else "PASS"} sb command lines: {n} stock commands, {bad} packages differ')
sys.exit(1 if bad else 0)
