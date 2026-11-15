#!/usr/bin/env python3
"""The Go distribution's own compiler tests ($GOROOT/test, cmd/internal/testdir's corpus) through fc.

Takes the single-file programs whose recipe is "// run" with no flags: each is package main, checks itself,
and must exit 0 printing exactly its .out file (nothing, if it has none). A program is built wholly by fc
(`fc build -link`: runtime included, fclink in process) and run. Only when that does not pass is the same
program built and run by the stock toolchain, the same way, to tell a bug of fc's from a test that cannot
pass here at all (a build constraint, a recipe that needs GOROOT/test as its directory).

    tools/fc/gotest.py [-j N] [-o results.txt] [-keep dir] [pattern ...]     (patterns: substrings of the path)

Lines of the result: PASS, FAIL (fc's binary misbehaves, stock's passes), NOFC (fc does not compile it,
stock does), EXCL (stock does not pass it either), SKIP (tests/fc/gotest-skips.txt: <path> <reason>).
Exit 0 when there is no FAIL and no NOFC. Environment: FC_BIN (default build/fc), FC_GOCACHE.
"""
import concurrent.futures as cf, os, shutil, subprocess, sys, tempfile

here = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
T = "/home/exedev/targets"
env = dict(os.environ, CGO_ENABLED="0", GOTOOLCHAIN="local", GOFLAGS="-buildvcs=false")
goroot = subprocess.run(["go", "env", "GOROOT"], env=env, capture_output=True, text=True).stdout.strip()
gover = subprocess.run(["go", "env", "GOVERSION"], env=env, capture_output=True, text=True).stdout.strip()
root = os.path.join(goroot, "test")
fc = os.environ.get("FC_BIN", os.path.join(here, "build", "fc"))
fcenv = dict(env, GOCACHE=os.environ.get("FC_GOCACHE", T + "/fc-gocache"))
stockenv = dict(env, GOCACHE=T + "/gotest-gocache-stock")
RUN_TIMEOUT = int(os.environ.get("GOTEST_TIMEOUT", "120"))


def recipe(path):
    with open(path, errors="replace") as f:
        for line in f:
            line = line.strip()
            if line.startswith("//go:build") or line.startswith("// +build") or line == "":
                continue
            return line
    return ""


def tests(patterns):
    out = []
    for d, dirs, files in os.walk(root):
        dirs[:] = sorted(x for x in dirs if not x.endswith(".dir") and x != "testdata")
        for f in sorted(files):
            if not f.endswith(".go"):
                continue
            p = os.path.join(d, f)
            rel = os.path.relpath(p, root)
            if patterns and not any(x in rel for x in patterns):
                continue
            if recipe(p) in ("// run", "//run"):
                out.append(rel)
    return out


def run(cmd, cwd, e, timeout):
    try:
        r = subprocess.run(cmd, cwd=cwd, env=e, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
        return r.returncode, r.stdout.decode(errors="replace")
    except subprocess.TimeoutExpired as x:
        return 124, (x.stdout or b"").decode(errors="replace") + "\n[timeout]"


def verdict(rc, out, want):
    if rc != 0:
        return "rc %d: %s" % (rc, " | ".join(out.strip().splitlines()[:3])[:240])
    if out.replace("\r\n", "\n") != want:
        return "output differs: %r" % out[:160]
    return ""


def one(rel, keep):
    src = os.path.join(root, rel)
    want = ""
    if os.path.exists(src[:-3] + ".out"):
        want = open(src[:-3] + ".out", errors="replace").read()
    w = tempfile.mkdtemp(prefix="fcgt.")
    try:
        shutil.copy(src, os.path.join(w, os.path.basename(rel)))
        with open(os.path.join(w, "go.mod"), "w") as f:
            f.write("module testmod\n\ngo %s\n" % ".".join(gover[2:].split(".")[:2]))
        rc, log = run([fc, "build", "-C", w, "-o", os.path.join(w, "objs"), "-link", os.path.join(w, "bin-fc"), "."], w, fcenv, 300)
        if rc == 0:
            rc, out = run([os.path.join(w, "bin-fc")], w, env, RUN_TIMEOUT)
            bad = verdict(rc, out, want)
            if not bad:
                return "PASS", rel, ""
            kind = "FAIL"
        else:
            kind = "NOFC"
            lines = [l for l in log.splitlines() if l.startswith("fc:")] or log.strip().splitlines()[-3:]
            bad = " | ".join(lines[:2])[:300]
        # what does the stock toolchain make of it, the same way?
        rc, log = run(["go", "build", "-o", os.path.join(w, "bin-stock"), "."], w, stockenv, 600)
        if rc != 0:
            return "EXCL", rel, "stock does not build it: " + " | ".join(log.strip().splitlines()[:2])[:200]
        rc, out = run([os.path.join(w, "bin-stock")], w, env, RUN_TIMEOUT)
        sbad = verdict(rc, out, want)
        if sbad:
            return "EXCL", rel, "stock: " + sbad[:200]
        if keep:
            dst = os.path.join(keep, rel.replace("/", "_")[:-3])
            shutil.rmtree(dst, ignore_errors=True)
            shutil.copytree(w, dst, ignore=shutil.ignore_patterns("objs"))
        return kind, rel, bad
    finally:
        shutil.rmtree(w, ignore_errors=True)


def main():
    a = sys.argv[1:]
    j, outp, keep = 2, None, None
    while a and a[0] in ("-j", "-o", "-keep"):
        if a[0] == "-j":
            j = int(a[1])
        elif a[0] == "-o":
            outp = a[1]
        else:
            keep = a[1]
            os.makedirs(keep, exist_ok=True)
        a = a[2:]
    skips = {}
    sk = os.path.join(here, "tests", "fc", "gotest-skips.txt")
    if os.path.exists(sk):
        for line in open(sk):
            if line.strip() and not line.startswith("#"):
                p, _, why = line.strip().partition(" ")
                skips[p] = why
    ts = tests(a)
    res = [("SKIP", t, skips[t]) for t in ts if t in skips]
    todo = [t for t in ts if t not in skips]
    with cf.ThreadPoolExecutor(j) as ex:
        for r in ex.map(lambda t: one(t, keep), todo):
            res.append(r)
            if r[0] != "PASS":
                print(*r, flush=True)
    res.sort(key=lambda r: r[1])
    n = {k: sum(1 for r in res if r[0] == k) for k in ("PASS", "FAIL", "NOFC", "EXCL", "SKIP")}
    summary = "%d run tests of %s: PASS %d  FAIL %d  NOFC %d  EXCL %d (stock does not pass them this way)  SKIP %d" % (
        len(res), gover, n["PASS"], n["FAIL"], n["NOFC"], n["EXCL"], n["SKIP"])
    if outp:
        with open(outp, "w") as f:
            for r in res:
                f.write(" ".join(x for x in r if x) + "\n")
            f.write("# " + summary + "\n")
    print(summary)
    sys.exit(1 if n["FAIL"] + n["NOFC"] else 0)


main()
