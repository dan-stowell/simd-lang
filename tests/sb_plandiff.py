#!/usr/bin/env python3
"""The driver's own plan (-plan=self -dumpplan) against `go list -deps`: the same packages, and for each the
same directory, name, Go files, assembly files, imports, import map, embed patterns, module and go version,
and for the main package the same default GODEBUG.     tests/sb_plandiff.py <plan.json> <srcdir> <pkg>"""
import json, os, subprocess, sys
plan = {p['ImportPath']: p for p in json.load(open(sys.argv[1]))}
env = dict(os.environ, GOTOOLCHAIN='local', CGO_ENABLED='0', GOFLAGS='')
out = subprocess.run(['go', 'list', '-deps', '-json=ImportPath,Dir,Name,GoFiles,SFiles,EmbedPatterns,Imports,ImportMap,Standard,Module,DefaultGODEBUG', sys.argv[3]],
                     cwd=sys.argv[2], env=env, capture_output=True, text=True, check=True).stdout
dec = json.JSONDecoder(); i = 0; ref = {}
while i < len(out):
    while i < len(out) and out[i].isspace(): i += 1
    if i >= len(out): break
    o, i = dec.raw_decode(out, i)
    if o['ImportPath'] != 'unsafe': ref[o['ImportPath']] = o
bad = 0
def report(*a):
    global bad
    bad += 1
    if bad <= 15: print('DIFF', *a)
for k in sorted(set(plan) | set(ref)):
    if k not in plan: report(k, 'missing from the plan'); continue
    if k not in ref: report(k, 'not in go list'); continue
    a, b = plan[k], ref[k]
    for f in ('Dir', 'Name'):
        if a.get(f) != b.get(f): report(k, f, a.get(f), b.get(f))
    for f in ('GoFiles', 'SFiles', 'EmbedPatterns'):
        if sorted(a.get(f) or []) != sorted(b.get(f) or []): report(k, f, sorted(set(a.get(f) or []) ^ set(b.get(f) or [])))
    ia = sorted(x for x in (a.get('Imports') or []))
    ib = sorted({v: 0 for v in (b.get('Imports') or [])})
    # go list reports imports after the import map is applied; so does this comparison
    ma = a.get('ImportMap') or {}
    ia = sorted(ma.get(x, x) for x in ia)
    if ia != ib: report(k, 'Imports', sorted(set(ia) ^ set(ib)))
    if (a.get('ImportMap') or {}) != (b.get('ImportMap') or {}): report(k, 'ImportMap', a.get('ImportMap'), b.get('ImportMap'))
    if bool(a.get('Standard')) != bool(b.get('Standard')): report(k, 'Standard')
    am, bm = a.get('Module') or {}, b.get('Module') or {}
    for f in ('Path', 'Version', 'GoVersion'):
        if (am.get(f) or '') != (bm.get(f) or ''): report(k, 'Module.' + f, am.get(f), bm.get(f))
    if (a.get('DefaultGODEBUG') or '') != (b.get('DefaultGODEBUG') or ''): report(k, 'DefaultGODEBUG', a.get('DefaultGODEBUG'), b.get('DefaultGODEBUG'))
nf = sum(len(p.get('GoFiles') or []) for p in ref.values())
print(f'{"FAIL" if bad else "PASS"} plan == go list: {len(ref)} packages, {nf} Go files, {bad} differences')
sys.exit(1 if bad else 0)
