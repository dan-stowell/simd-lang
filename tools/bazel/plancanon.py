#!/usr/bin/env python3
"""plancanon.py WORK: the plans bzfc -planonly wrote under WORK/builds/*/ (the binary's and every tool's), one line per
package and field, canonical: the work directory is W, bazel-out/<configuration>/ is bazel-out/CFG/ (also in farm
directory names, where / is %). Lines are sorted and unique: a tool built in two configurations by the aquery front end
is one plan here.

plancanon.py --files WORK: every file under WORK/execroot/bazel-out (generated sources, descriptor sets, scripts,
tools), its path canonical as above: path, kind (file, elf: a tool fc built, link), the sha256 of its bytes with the
work directory's name replaced by W's (plandiff.sh gives both runs work directories of the same length), the sha256
of its bytes with the configuration directories' names canonical too (a symlink: its target, canonical).

plancanon.py --cmp A B: two --files outputs compared path by path, the set of contents of a path (the aquery front end
may have it in several configurations) against the other's."""
import collections, hashlib, json, os, re, sys

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
    wb = work.encode()
    for dp, dns, fns in os.walk(base):
        for n in fns + [d for d in dns if os.path.islink(os.path.join(dp, d))]:
            p = os.path.join(dp, n)
            rel = canon(os.path.relpath(p, os.path.join(work, "execroot")), work)
            if os.path.islink(p):
                t = canon(os.readlink(p), work)
                out.add("%s\tlink\t%s\t%s" % (rel, t, t))
            elif os.path.isfile(p):
                b = open(p, "rb").read().replace(wb, b"W" * len(wb))
                raw = hashlib.sha256(b).hexdigest()
                kind = "elf" if b[:4] == b"\x7fELF" else "file"
                norm = raw if kind == "elf" else hashlib.sha256(re.sub(rb"bazel-out/[^/\s\"']+/", b"bazel-out/CFG/", b)).hexdigest()
                out.add("%s\t%s\t%s\t%s" % (rel, kind, raw, norm))
    for l in sorted(out):
        print(l)

def cmp(a, b):
    def load(f):
        m = {}
        for l in open(f):
            p, kind, raw, norm = l.rstrip("\n").split("\t")
            k, r, n = m.get(p, (kind, set(), set()))
            r.add(raw); n.add(norm)
            m[p] = (kind, r, n)
        return m
    A, B = load(a), load(b)
    both = sorted(set(A) & set(B))
    stats = collections.Counter()
    bad = []
    for p in both:
        kind = A[p][0]
        stats[kind] += 1
        if A[p][1] == B[p][1]:
            stats[kind + " equal"] += 1
        elif A[p][2] == B[p][2]:
            stats[kind + " equal after the configuration names"] += 1
        else:
            stats[kind + " differ"] += 1
            if kind == "file":
                bad.append(p)
    print("files under bazel-out in both: %s" % ", ".join("%s %d" % kv for kv in sorted(stats.items())))
    print("only the aquery run's: %d; only the query run's: %d" % (len(set(A) - set(B)), len(set(B) - set(A))))
    for p in sorted(set(A) - set(B)):
        print("  only aquery: %s" % p)
    for p in sorted(set(B) - set(A)):
        print("  only query: %s" % p)
    # symlinks with other targets, grouped by the pair of targets
    links = collections.defaultdict(list)
    for p in both:
        if A[p][0] == "link" and A[p][2] != B[p][2]:
            links[(" ".join(sorted(A[p][2])), " ".join(sorted(B[p][2])))].append(p)
    for (ta, tb), ps in sorted(links.items()):
        print("  %d links to %s in the aquery run, to %s in the query run (%s ...)" % (len(ps), ta, tb, ps[0]))
    for p in bad:
        print("  DIFFERS: %s" % p)
    return 1 if bad else 0

if sys.argv[1] == "--files":
    files(os.path.abspath(sys.argv[2]))
elif sys.argv[1] == "--cmp":
    sys.exit(cmp(sys.argv[2], sys.argv[3]))
else:
    plans(os.path.abspath(sys.argv[1]))
