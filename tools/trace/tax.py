#!/usr/bin/env python3
"""The stock build's intermediates, from a `go build -work -debug-actiongraph` run (the work directory must
still exist): bytes of archives written, and bytes read back (each compile maps the export data of each
direct import; the linker reads every archive; go build reads every archive again to copy it to GOCACHE).
Prints meta.txt lines."""
import json, os, sys
acts = json.load(open(sys.argv[1]))
byid = {a['ID']: a for a in acts}
def arch(a): return os.path.join(a.get('Objdir') or '', '_pkg_.a')
def pkgdef(path):
    try:
        with open(path, 'rb') as f:
            if f.read(8) != b'!<arch>\n': return 0
            h = f.read(60)
            return int(h[48:58]) if h[:16].strip() == b'__.PKGDEF' else 0
    except OSError: return 0
size = {}; xs = {}
for a in acts:
    if a['Mode'] == 'build':
        p = arch(a)
        if os.path.exists(p): size[a['ID']] = os.path.getsize(p); xs[a['ID']] = pkgdef(p)
w = sum(size.values()); r = 0
for a in acts:
    if a['Mode'] == 'build':
        r += sum(xs.get(d, 0) for d in a.get('Deps') or [])
print('tax_archives', len(size))
print('tax_inter_written', w * 2)        # once in the work directory, once more into GOCACHE
print('tax_inter_read', r + w + w)       # importers; the linker; the copy to GOCACHE
print('tax_export_bytes', sum(xs.values()))
