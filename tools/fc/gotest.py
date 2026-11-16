#!/usr/bin/env python3
"""The Go distribution's own compiler tests ($GOROOT/test, cmd/internal/testdir's corpus) through fc.

Takes the recipes that build and run a program (cmd/internal/testdir/testdir_test.go is their specification).
Each program is built wholly by fc (`fc build -link`: runtime included, fclink in process) and run; it must
exit 0 printing exactly its .out file (nothing, if it has none). Only when that does not pass is the same
program built and run by the stock toolchain, the same way (`go build` in the same directory, with the
recipe's flags), to tell a bug of fc's from a test that cannot pass here at all (a build constraint, cgo, a
recipe that needs GOROOT/test as its directory). Every program is a module of its own in a temporary
directory, run there.

    tools/fc/gotest.py [-j N] [-o results.txt] [-keep dir] [-r kind,...] [pattern ...]   (patterns: substrings of the path)

Kinds (-r; default all):
  run        "// run" with nothing after it: one file, package main (the first check; its numbers are kept apart).
  run+       "// run" with flags or arguments: arguments ending in .go are more source files (cmplxdivide.go,
             as `go run` takes them), the others go to the program.
  rundir     X.dir's packages in the order testdir compiles them (lexicographic by file, grouped by package
             name; -s: one package per file), each but the last at test/<first file without .go> (testdir's
             -p and importcfg), the last (main) at the root of a module named "test". testdir compiles with
             -D test, so the sources' `import "./a"` means test/a: those import paths, and nothing else, are
             rewritten to "test/a" (module mode has no relative imports), and a file p_test.go is written as
             p_test_.go (testdir compiles it with the others; the go command would leave it to go test).
  runoutput  X.go (plus the .go arguments) is built and run; its output is a program tmp__.go, built and run.
  runindir   X.dir copied as module "<X>.dir" (go 1.14, or -gomodversion), built as `go run .` would.
  buildrun   X.go built and run with the arguments.
  build      X.go built only (compiled only, without -link, when it is not package main).  builddir / buildrundir: X.dir's .go and .s files as one package main (built only /
             built and run).
  Not taken: errorcheck*, compile*, asmcheck, skip (the stock front end's messages and optimizer).

Flags: -t N (run timeout), -godebug (GODEBUG of the run), -gomodversion and -s are honoured. Ignored by fc (the
stock build still gets them): -gcflags with -l, -l=N, -N (inlining and optimization: a dev build does neither),
-d=checkptr[=0] (fc does not instrument unsafe.Pointer conversions; the checkptr programs are valid programs
whose behaviour is still checked), -d=ssa/... (the stock SSA back end's checker and its resched-check pass;
fc has its own back end and async preemption), rundir's -ldflags -strictdups=2 -w=0 (diagnostics of the stock
linker). A program with any other flag is not taken: -race, -goexperiment, -tags, -d=softfloat,
-d=converthash (selects a different float conversion), -d=maymorestack (instrumentation fc does not have).
Build constraints of a directory recipe's X.go are evaluated here (linux, amd64, unix, gc, go1.N; cgo is off).

Lines of the result: PASS, FAIL (fc's binary misbehaves, stock's passes), NOFC (fc does not compile it,
stock does), EXCL (stock does not pass it either), SKIP (tests/fc/gotest-skips.txt: <path> <reason>).
Exit 0 when there is no FAIL and no NOFC. Environment: FC_BIN (default build/fc), FC_GOCACHE.
"""
import concurrent.futures as cf, os, re, shlex, shutil, subprocess, sys, tempfile

here = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
T = "/home/exedev/targets"
env = dict(os.environ, CGO_ENABLED="0", GOTOOLCHAIN="local", GOFLAGS="-buildvcs=false")
goroot = subprocess.run(["go", "env", "GOROOT"], env=env, capture_output=True, text=True).stdout.strip()
gover = subprocess.run(["go", "env", "GOVERSION"], env=env, capture_output=True, text=True).stdout.strip()
root = os.path.join(goroot, "test")
fc = os.environ.get("FC_BIN", os.path.join(here, "build", "fc"))
fcenv = dict(env, GOCACHE=os.environ.get("FC_GOCACHE", T + "/fc-gocache"))
stockenv = dict(env, GOCACHE=T + "/gotest-gocache-stock")
RUN_TIMEOUT = int(os.environ.get("GOTEST_TIMEOUT", "600"))
LANG = ".".join(gover[2:].split(".")[:2])
KINDS = ["run", "run+", "rundir", "runoutput", "runindir", "buildrun", "buildrundir", "build", "builddir"]


def recipe(path):
    with open(path, errors="replace") as f:
        for line in f:
            line = line.strip()
            if line.startswith("//go:build") or line.startswith("// +build") or line == "":
                continue
            return line
    return ""


# ---- flags


def gcflag_ok(v):
    """A -gcflags value: None if every compile flag in it is one fc can ignore, else the first that is not."""
    for f in shlex.split(v.split("=", 1)[1] if v.startswith("all=") else v):
        if f in ("-N", "-l") or f.startswith("-l="):
            continue
        if f.startswith("-d="):
            if all(d.startswith("checkptr") or d.startswith("ssa/") for d in f[3:].split(",")):
                continue
        return f
    return None


class Case:
    def __init__(self, rel, kind, words):
        self.rel, self.kind = rel, kind
        self.stockflags, self.args, self.files, self.runenv = [], [], [], {}
        self.timeout, self.modvers, self.single, self.ldflags = RUN_TIMEOUT, LANG, False, []
        self.no = None  # why the program is not taken
        a = words
        while a and a[0].startswith("-"):
            f = a[0]
            if f == "-t":
                self.timeout = int(a[1])
                a = a[1:]
            elif f == "-godebug":
                self.runenv["GODEBUG"] = a[1]
                a = a[1:]
            elif f == "-gomodversion":
                self.modvers = a[1]
                a = a[1:]
            elif f == "-s":
                self.single = True
            elif kind == "rundir":
                if f == "-ldflags":
                    self.ldflags = a[1:]
                    if any(x not in ("-strictdups=2", "-w=0") for x in self.ldflags):
                        self.no = "linker flags " + " ".join(self.ldflags)
                    a = []
                    break
                bad = gcflag_ok(f)
                if bad:
                    self.no = "flag " + bad
                self.stockflags.append(f)  # compile flags: -gcflags below
            elif f in ("-gcflags", "--gcflags") or f.startswith("-gcflags=") or f.startswith("--gcflags="):
                if "=" in f:
                    v = f.split("=", 1)[1]
                    self.stockflags.append(f)
                else:
                    v = a[1]
                    self.stockflags += [f, v]
                    a = a[1:]
                bad = gcflag_ok(v)
                if bad:
                    self.no = "flag " + bad
            else:
                self.no = "flag " + " ".join(a[:2] if f in ("-goexperiment", "-ldflags") else a[:1])
            a = a[1:]
        if kind == "rundir":
            self.stockflags = (["-gcflags=test/...=" + " ".join(self.stockflags)] if self.stockflags else []) + \
                (["-ldflags=" + " ".join(self.ldflags)] if self.ldflags else [])
        if kind in ("run+", "runoutput"):
            # `go run x.go y.go arg`: the leading .go arguments are files
            while a and a[0].endswith(".go"):
                self.files.append(a[0])
                a = a[1:]
        if kind in ("run+", "rundir", "buildrun"):
            self.args = a
        # `// build` of a package that is not main: compiled only (go build -o a.exe writes nothing)
        self.main = kind != "build" or re.search(r"^package main\b", open(os.path.join(root, rel), errors="replace").read(), re.M)
        want = os.path.join(root, rel[:-3] + ".out")
        self.want = open(want, errors="replace").read() if os.path.exists(want) else ""


def kind_of(path):
    r = recipe(path)
    if r in ("// run", "//run"):
        return "run", []
    if not r.startswith("//"):
        return None, None
    try:
        w = shlex.split(r[2:])
    except ValueError:
        return None, None
    if not w:
        return None, None
    k = w[0]
    if k == "run":
        return "run+", w[1:]
    if k in KINDS:
        return k, w[1:]
    return None, None


def tests(patterns, kinds):
    out, notaken = [], []
    for d, dirs, files in os.walk(root):
        dirs[:] = sorted(x for x in dirs if not x.endswith(".dir") and x != "testdata")
        for f in sorted(files):
            if not f.endswith(".go"):
                continue
            p = os.path.join(d, f)
            rel = os.path.relpath(p, root)
            if patterns and not any(x in rel for x in patterns):
                continue
            k, w = kind_of(p)
            if k is None or k not in kinds:
                continue
            c = Case(rel, k, w)
            if c.no:
                notaken.append((rel, c.no))
            else:
                out.append(c)
    return out, notaken


# ---- build constraints of a directory recipe's file


def constraint_ok(path):
    """The //go:build line before the package clause, evaluated for linux/amd64, gc, cgo off."""
    minor = int(LANG.split(".")[1])

    def tag(t):
        if t in ("linux", "amd64", "unix", "gc"):
            return True
        m = re.fullmatch(r"go1\.(\d+)", t)
        return bool(m) and int(m.group(1)) <= minor

    for line in open(path, errors="replace"):
        if line.startswith("package "):
            break
        if line.startswith("//go:build "):
            e = line[len("//go:build "):].strip()
            py = re.sub(r"[A-Za-z0-9_.]+", lambda m: str(tag(m.group(0))), e)
            py = py.replace("&&", " and ").replace("||", " or ").replace("!", " not ")
            if not eval(py, {}):
                return False, line.strip()
    return True, ""


# ---- laying out the sources


def write_mod(d, mod, vers):
    with open(os.path.join(d, "go.mod"), "w") as f:
        f.write("module %s\n\ngo %s\n" % (mod, vers))


IMPORT_RE = re.compile(r'^(\s*(?:import\s+)?(?:[\w.]+\s+)?)"\./([^"]+)"', re.M)


def rewrite_imports(src):
    """`import "./a"` (testdir's -D test) -> "test/a", in import declarations only."""
    # import declarations: `import "x"`, `import n "x"`, `import ( ... )`; stop at the first func/type/var/const
    head = re.search(r"^(func|type|var|const)\b", src, re.M)
    end = head.start() if head else len(src)
    h, k = IMPORT_RE.subn(lambda m: m.group(1) + '"test/' + m.group(2) + '"', src[:end])
    return h + src[end:], k


def goDirPackages(d, single):
    pkgs, by = [], {}
    for f in sorted(os.listdir(d)):
        if not f.endswith(".go"):
            continue
        m = re.search(r"^package ([\w]+)", open(os.path.join(d, f), errors="replace").read(), re.M)
        name = m.group(1) if m else ""
        if single or name not in by:
            by[name] = [name, []]
            pkgs.append(by[name])
        by[name][1].append(f)
    return pkgs


def layout(c, w):
    """Writes the program's module under w/src; returns that directory."""
    src = os.path.join(w, "src")
    long = os.path.join(root, c.rel)
    if c.kind in ("run", "run+", "runoutput", "buildrun", "build"):
        if c.kind == "run":
            src = w  # the first check's layout, unchanged
        os.makedirs(src, exist_ok=True)
        shutil.copy(long, os.path.join(src, os.path.basename(c.rel)))
        for f in c.files:
            shutil.copy(os.path.join(os.path.dirname(long), f), os.path.join(src, os.path.basename(f)))
        write_mod(src, "testmod", LANG)
        return src
    dd = long[:-3] + ".dir"
    if c.kind == "runindir":
        src = os.path.join(w, "src", os.path.basename(dd))
        shutil.copytree(dd, src)
        write_mod(src, os.path.basename(dd), c.modvers or "1.14")
        return src
    os.makedirs(src)
    if c.kind in ("builddir", "buildrundir"):
        for f in sorted(os.listdir(dd)):
            if f.endswith(".go") or f.endswith(".s"):
                shutil.copy(os.path.join(dd, f), os.path.join(src, f))
        write_mod(src, "testmod", LANG)
        return src
    # rundir
    pkgs = goDirPackages(dd, c.single)
    for i, (name, files) in enumerate(pkgs):
        to = src if i == len(pkgs) - 1 else os.path.join(src, files[0][:-3])
        os.makedirs(to, exist_ok=True)
        for f in files:
            s, _ = rewrite_imports(open(os.path.join(dd, f), errors="replace").read())
            # testdir compiles every .go file; the go command would take p_test.go for a test file
            with open(os.path.join(to, f[:-3] + "_.go" if f.endswith("_test.go") else f), "w") as o:
                o.write(s)
    write_mod(src, "test", LANG)
    return src


# ---- building and running


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


def build(tool, c, src, w, tag):
    """Builds the main package in src into w/bin-<tag>: (binary, "") or (None, why)."""
    exe = os.path.join(w, "bin-" + tag)
    if tool == "fc":
        objs = os.path.join(w, "objs") if tag == "fc" else os.path.join(w, "objs-" + tag)
        link = ["-link", exe] if c.main else []
        rc, log = run([fc, "build", "-C", src, "-o", objs] + link + ["."], src, fcenv, 300)
        if rc != 0:
            lines = [l for l in log.splitlines() if l.startswith("fc:")] or log.strip().splitlines()[-3:]
            return None, " | ".join(lines[:2])[:300]
    else:
        flags = c.stockflags if tag == "stock" else []
        rc, log = run(["go", "build"] + flags + ["-o", exe, "."], src, stockenv, 600)
        if rc != 0:
            return None, "stock does not build it: " + " | ".join(log.strip().splitlines()[:2])[:200]
    return exe, ""


def attempt(tool, c, src, w):
    """("ok"|"nobuild"|"bad", detail): the program built by tool ("fc" or "stock") and run as its recipe says."""
    exe, why = build(tool, c, src, w, tool)
    if not exe:
        return "nobuild", why
    if c.kind in ("build", "builddir") or not c.main:
        return "ok", ""
    renv = dict(env, **c.runenv)
    rc, out = run([exe] + c.args, src, renv, c.timeout)
    if c.kind == "runoutput":
        if rc != 0:
            return "bad", "generator " + verdict(rc, out, out)
        g = os.path.join(w, "gen-" + tool)
        os.makedirs(g)
        with open(os.path.join(g, "tmp__.go"), "w") as f:
            f.write(out)
        write_mod(g, "testmod", LANG)
        exe, why = build(tool, c, g, w, tool + "-gen")
        if not exe:
            return "nobuild", "generated program: " + why
        rc, out = run([exe], g, renv, c.timeout)
    bad = verdict(rc, out, c.want)
    return ("bad", bad) if bad else ("ok", "")


def one(c, keep):
    if c.kind not in ("run", "run+", "runoutput", "buildrun", "build"):
        ok, line = constraint_ok(os.path.join(root, c.rel))
        if not ok:
            return "EXCL", c.rel, "build constraint: " + line
    w = tempfile.mkdtemp(prefix="fcgt.")
    try:
        src = layout(c, w)
        r, bad = attempt("fc", c, src, w)
        if r == "ok":
            return "PASS", c.rel, ""
        kind = "FAIL" if r == "bad" else "NOFC"
        # what does the stock toolchain make of it, the same way?
        s, sbad = attempt("stock", c, src, w)
        if s != "ok":
            return "EXCL", c.rel, (sbad if sbad.startswith("stock") else "stock: " + sbad)[:220]
        if keep:
            dst = os.path.join(keep, c.rel.replace("/", "_")[:-3])
            shutil.rmtree(dst, ignore_errors=True)
            shutil.copytree(w, dst, ignore=shutil.ignore_patterns("objs", "objs-*"))
        return kind, c.rel, bad
    finally:
        shutil.rmtree(w, ignore_errors=True)


def main():
    a = sys.argv[1:]
    j, outp, keep, kinds = 2, None, None, KINDS
    while a and a[0] in ("-j", "-o", "-keep", "-r"):
        if a[0] == "-j":
            j = int(a[1])
        elif a[0] == "-o":
            outp = a[1]
        elif a[0] == "-r":
            kinds = a[1].split(",")
            for k in kinds:
                if k not in KINDS:
                    sys.exit("gotest: unknown kind %s (kinds: %s)" % (k, " ".join(KINDS)))
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
    ts, notaken = tests(a, kinds)
    kindof = {c.rel: c.kind for c in ts}
    res = [("SKIP", c.rel, skips[c.rel]) for c in ts if c.rel in skips]
    todo = [c for c in ts if c.rel not in skips]
    # the long runoutput programs first, so they do not finish last
    todo.sort(key=lambda c: c.kind != "runoutput")
    with cf.ThreadPoolExecutor(j) as ex:
        for r in ex.map(lambda c: one(c, keep), todo):
            res.append(r)
            if r[0] != "PASS":
                print(*r, flush=True)
    res.sort(key=lambda r: r[1])
    lines = []
    for k in kinds:
        rk = [r for r in res if kindof[r[1]] == k]
        if not rk:
            continue
        n = {x: sum(1 for r in rk if r[0] == x) for x in ("PASS", "FAIL", "NOFC", "EXCL", "SKIP")}
        lines.append("%d %s tests of %s: PASS %d  FAIL %d  NOFC %d  EXCL %d (stock does not pass them this way)  SKIP %d" % (
            len(rk), k, gover, n["PASS"], n["FAIL"], n["NOFC"], n["EXCL"], n["SKIP"]))
    n = {x: sum(1 for r in res if r[0] == x) for x in ("PASS", "FAIL", "NOFC", "EXCL", "SKIP")}
    if len(lines) > 1:
        lines.append("%d tests in all: PASS %d  FAIL %d  NOFC %d  EXCL %d  SKIP %d  (not taken, flags fc does not honour: %d)" % (
            len(res), n["PASS"], n["FAIL"], n["NOFC"], n["EXCL"], n["SKIP"], len(notaken)))
    if outp:
        with open(outp, "w") as f:
            for r in res:
                f.write(" ".join(x for x in r if x) + "\n")
            for rel, why in notaken:
                f.write("# not taken: %s %s\n" % (rel, why))
            for l in lines:
                f.write("# " + l + "\n")
    print("\n".join(lines))
    sys.exit(1 if n["FAIL"] + n["NOFC"] else 0)


main()
