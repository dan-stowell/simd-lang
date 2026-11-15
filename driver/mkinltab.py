#!/usr/bin/env python3
"""driver/mkinltab.py <go build -gcflags=all=-m output> : the functions the stock compiler can inline, one
"import path<TAB>name" per line, names as fc spells a declaration: F, T.M, (*T).M (type arguments dropped; function
literals left out). Read by driver/mkfc.sh, which compiles the list into fc (driver/fc/inltab.go)."""
import re, sys

def strip_brackets(s):
    out, depth = [], 0
    for ch in s:
        if ch == "[":
            depth += 1
        elif ch == "]":
            depth -= 1
        elif depth == 0:
            out.append(ch)
    return "".join(out)

lines = open(sys.argv[1]).read().split("\n")
bylast = {}   # last path element -> import path
for line in lines:
    if line.startswith("# "):
        p = line[2:].split()[0]
        bylast[p.rsplit("/", 1)[-1]] = p
pkg, seen = None, set()
for line in lines:
    if line.startswith("# "):
        pkg = line[2:].split()[0]
        continue
    m = re.search(r": can inline (\S+)", line)
    if not m or pkg is None:
        continue
    generic = "[" in m.group(1)
    name = strip_brackets(m.group(1))
    if re.search(r"\.func\d|-range\d|\.gowrap\d|\.deferwrap\d", name):
        continue
    p = pkg
    if generic and not name.startswith("(") and name.count(".") == 1 and name.split(".")[0] in bylast:
        # an instance of a generic function is printed with its package, and in every package that
        # instantiates it: bytealg.hashStr[go.shape.string] under internal/stringslite
        q, name = name.split(".")
        p = bylast[q]
    seen.add(p + "\t" + name)
for s in sorted(seen):
    print(s)
