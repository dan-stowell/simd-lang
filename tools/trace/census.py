#!/usr/bin/env python3
"""Source census of a build: every file `go build` hands to compile/asm for the target.
   census.py <srcdir> <pkg> <out.json>"""
import json, os, subprocess, sys
src, pkg, out = sys.argv[1:4]
env = dict(os.environ, GOTOOLCHAIN='local', CGO_ENABLED='0', GOFLAGS='-trimpath')
p = subprocess.run(['go', 'list', '-deps', '-json=ImportPath,Dir,GoFiles,SFiles,EmbedFiles,Standard,Imports,Module', pkg],
                   cwd=src, env=env, capture_output=True, text=True, check=True)
dec = json.JSONDecoder(); s = p.stdout; i = 0; pk = []
while i < len(s):
    while i < len(s) and s[i].isspace(): i += 1
    if i >= len(s): break
    o, i = dec.raw_decode(s, i); pk.append(o)
tot = dict(packages=0, go_files=0, go_bytes=0, go_lines=0, s_files=0, s_bytes=0, embed_bytes=0,
           std_packages=0, std_go_bytes=0)
per = {}
for o in pk:
    if o['ImportPath'] == 'unsafe': continue
    d = o['Dir']; gb = gl = sb = 0
    for f in o.get('GoFiles', []):
        b = open(os.path.join(d, f), 'rb').read(); gb += len(b); gl += b.count(b'\n')
    for f in o.get('SFiles', []):
        sb += os.path.getsize(os.path.join(d, f))
    eb = sum(os.path.getsize(os.path.join(d, f)) for f in o.get('EmbedFiles', []) if os.path.isfile(os.path.join(d, f)))
    per[o['ImportPath']] = dict(go_bytes=gb, go_lines=gl, s_bytes=sb, go_files=len(o.get('GoFiles', [])),
                                std=bool(o.get('Standard')), imports=o.get('Imports', []),
                                files=[os.path.join(d, f) for f in o.get('GoFiles', [])],
                                sfiles=[os.path.join(d, f) for f in o.get('SFiles', [])])
    tot['packages'] += 1; tot['go_files'] += len(o.get('GoFiles', [])); tot['go_bytes'] += gb; tot['go_lines'] += gl
    tot['s_files'] += len(o.get('SFiles', [])); tot['s_bytes'] += sb; tot['embed_bytes'] += eb
    if o.get('Standard'): tot['std_packages'] += 1; tot['std_go_bytes'] += gb
json.dump(dict(total=tot, packages=per), open(out, 'w'))
print(json.dumps(tot))
