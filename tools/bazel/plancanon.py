#!/usr/bin/env python3
"""plancanon.py WORK: the plans bzfc -planonly wrote under WORK/builds/*/ (the binary's and every tool's), one line per
package and field, canonical: the work directory is W, bazel-out/<configuration>/ is bazel-out/CFG/ (also in farm
directory names, where / is %). Lines are sorted and unique: a tool built in two configurations by the aquery front end
is one plan here.   plancanon.py --files WORK: every file under WORK/execroot/bazel-out (generated sources, descriptor
sets, tools), path canonical as above, with its sha256 (the work directory's name replaced by W's; a symlink: '-> target', canonical)."""
import hashlib, json, os, re, sys

def canon(s, work):
    s = s.replace(work, "W")
    s = re.sub(r"bazel-out/[^/]+/", "bazel-out/CFG/", s)
    s = re.sub(r"bazel-out%[^%]+%", "bazel-out%CFG%", s)
    return s

def plans(work):
    out = set()
    root = os.path.join(work, "builds")
    for b in sorted(os.listdir(root)):
        d = os.path.join(root, b)
        if not os.path.exists(os.path.join(d, "plan.json")):
            continue
        name = canon(b, work)
        with open(os.path.join(d, "plan.json")) as f:
            plan = json.load(f)
        for p in plan:
            ip = p.get("ImportPath")
            for k in sorted(p):
                if k == "ImportPath":
                    continue
                v = p[k]
                if isinstance(v, list):
                    v = " ".join(v)
                elif isinstance(v, dict):
                    v = " ".join("%s=%s" % kv for kv in sorted(v.items()))
                out.add("%s %s %s=%s" % (name, ip, k, canon(str(v), work)))
        xd = os.path.join(d, "xdefs.txt")
        if os.path.exists(xd):
            for x in open(xd).read().split("\n"):
                if x:
                    out.add("%s -X %s" % (name, canon(x, work)))
    for l in sorted(out):
        print(l)

def files(work):
    base = os.path.join(work, "execroot", "bazel-out")
    out = set()
    for dp, dns, fns in os.walk(base):
        for n in fns + [d for d in dns if os.path.islink(os.path.join(dp, d))]:
            p = os.path.join(dp, n)
            rel = canon(os.path.relpath(p, os.path.join(work, "execroot")), work)
            if os.path.islink(p):
                out.add("%s -> %s" % (rel, canon(os.readlink(p), work)))
            elif os.path.isfile(p):
                # (the work directory's name replaced in the bytes too: fc's binaries hold source paths; plandiff.sh
                # gives both runs work directories of the same length)
                b = open(p, "rb").read()
                h = hashlib.sha256(b.replace(work.encode(), b"W" * len(work))).hexdigest()
                # (a tool fc built: its bytes hold the configuration directories' names, which differ by design)
                out.add("%s %s%s" % (rel, "ELF:" if b[:4] == b"\x7fELF" else "", h))
    for l in sorted(out):
        print(l)

if sys.argv[1] == "--files":
    files(os.path.abspath(sys.argv[2]))
else:
    plans(os.path.abspath(sys.argv[1]))
