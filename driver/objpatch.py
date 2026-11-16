#!/usr/bin/env python3
"""Patch the fc GOROOT's cmd/internal/obj (driver/mkfc.sh): LSym gets a word for fc's descriptor writer.

    driver/objpatch.py <GOROOT>/src/cmd/internal/obj

Idempotent. The field is the claim of driver/fc/rtype.go (one compare-and-swap on the symbol where a
concurrent map keyed by the symbol was 1% of a MinIO build); nothing in cmd/internal/obj reads it.
"""
import sys
p = sys.argv[1] + '/link.go'
s = open(p).read()
if 'FcClaim' not in s:
    old = '\tPkg    string\n\tPkgIdx int32\n\tSymIdx int32\n}'
    assert old in s, 'LSym has changed: look at driver/objpatch.py'
    s = s.replace(old, '\tPkg    string\n\tPkgIdx int32\n\tSymIdx int32\n\n\tFcClaim uint32 // fc: claimed by its descriptor writer (driver/fc/rtype.go)\n}', 1)
    open(p, 'w').write(s)
