#!/usr/bin/env python3
"""Run only after the coordinating audit agent authorizes exclusive timing.

All stdout/stderr/exit statuses and exact argv/env controls are recorded.
Whole-process wall is monotonic time around subprocess.run (includes taskset,
GNU time, and process launch); GNU time separately records coarse child wall,
CPU time, RSS, faults, and exit status. Output bytes go to /dev/null.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import random
import statistics
import subprocess
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()


def run(args):
    corpus = Path(args.corpus).resolve()
    out = HERE / "logs" / args.label
    out.mkdir(parents=True, exist_ok=False)
    env = dict(os.environ)
    # Make runtime defaults explicit and reproducible.
    for k in ("GOMAXPROCS", "GOMEMLIMIT", "GODEBUG", "GOEXPERIMENT"):
        env.pop(k, None)
    env["GOGC"] = "100"
    env["GOTOOLCHAIN"] = "local"
    controls = {"date_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                "corpus": str(corpus), "bytes": corpus.stat().st_size,
                "sha256": sha(corpus), "affinity": sorted(os.sched_getaffinity(0)),
                "reps": args.reps, "groups": args.groups, "seed": args.seed,
                "env_removed": ["GOMAXPROCS", "GOMEMLIMIT", "GODEBUG", "GOEXPERIMENT"],
                "env_set": {"GOGC": "100", "GOTOOLCHAIN": "local"},
                "executables": {str(p.relative_to(HERE)): sha(p)
                                for p in (HERE/".build").iterdir() if p.is_file() and os.access(p, os.X_OK)}}
    (out / "controls.json").write_text(json.dumps(controls, indent=2) + "\n")
    for label, cmd in {
        "go-version": ["go", "version"],
        "go-env": ["go", "env", "-json"],
        "uname": ["uname", "-a"],
        "lscpu": ["lscpu"],
        "git-head": ["git", "rev-parse", "HEAD"],
        "processes-before": ["ps", "-eo", "pid,ppid,psr,pcpu,etime,args", "--sort=-pcpu"],
    }.items():
        p = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env, cwd=HERE)
        (out / (label + ".txt")).write_bytes(p.stdout)
        if p.returncode:
            raise RuntimeError((cmd, p.returncode))
    records = open(out / "commands.jsonl", "w")
    data = open(out / "measurements.jsonl", "w")

    def invoke(label, argv, mask="0", inp=None, discard=False):
        rfile = out / (label + ".resources")
        cmd = ["taskset", "-c", mask, "/usr/bin/time", "-f",
               "wall_s=%e user_s=%U sys_s=%S maxrss_kb=%M major_faults=%F minor_faults=%R exit=%x",
               "-o", str(rfile)] + [str(a) for a in argv]
        stdin = open(inp, "rb") if inp else subprocess.DEVNULL
        with open(os.devnull if discard else out / (label + ".stdout"), "wb") as stdout, \
                open(out / (label + ".stderr"), "wb") as stderr:
            start = time.perf_counter_ns()
            p = subprocess.run(cmd, stdin=stdin, stdout=stdout, stderr=stderr, env=env, cwd=HERE)
            elapsed = time.perf_counter_ns() - start
        if inp:
            stdin.close()
        rec = {"label": label, "argv": cmd, "stdin": str(inp) if inp else None,
               "stdout": "/dev/null" if discard else label+".stdout",
               "stderr": label+".stderr", "returncode": p.returncode,
               "process_wall_ns": elapsed, "mask": mask,
               "resources": rfile.read_text().strip()}
        records.write(json.dumps(rec) + "\n"); records.flush()
        if p.returncode:
            raise RuntimeError(rec)
        return rec

    # Correctness is checked independently of timed runs.
    invoke("syntax-equivalence", [HERE/".build/scanners", "-mode", "check", "-file", corpus])
    rng = random.Random(args.seed)
    whole = [("strand-"+str(n), "0" if n == 1 else "0-"+str(n-1),
              [HERE/".build/golex-strand"], corpus)
             for n in (1, 2, 3, 8)]
    whole += [("go-dump-"+str(n), "0" if n == 1 else "0-7",
               [HERE/".build/golex-original", "dump", corpus], None) for n in (1, 8)]
    whole += [("syntax-dump-1", "0", [HERE/".build/scanners", "-mode", "syntax-dump", "-file", corpus], None)]
    for rep in range(args.reps):
        schedule = whole[:]; rng.shuffle(schedule)
        for name, mask, argv, inp in schedule:
            r = invoke(f"whole-{name}-{rep}", argv, mask, inp, True)
            data.write(json.dumps({"kind": "whole", "name": name, "rep": rep, **r})+"\n"); data.flush()
    # Three independent best-of-seven groups, no discarded first scan.
    primary = [("original", "natural", "0"), ("direct", "natural", "0"),
               ("syntax", "natural", "0"), ("original", "natural", "0-7")]
    for group in range(args.groups):
        schedule = primary[:]; rng.shuffle(schedule)
        for mode, gc, mask in schedule:
            label = f"scan-{mode}-{gc}-{mask}-group{group}"
            invoke(label, [HERE/".build/scanners", "-mode", mode, "-gc", gc, "-file", corpus, "-reps", "7"], mask)
            for line in (out/(label+".stdout")).read_text().splitlines():
                data.write(json.dumps({"kind": "scan", "group": group, "mask": mask, **json.loads(line)})+"\n")
            data.flush()
    controls = [(mode, gc) for mode in ("original", "direct", "syntax") for gc in ("before", "off")]
    controls += [(mode, "natural") for mode in ("handler", "reuse", "syntax-reuse")]
    rng.shuffle(controls)
    for mode, gc in controls:
        label = f"control-{mode}-{gc}"
        invoke(label, [HERE/".build/scanners", "-mode", mode, "-gc", gc, "-file", corpus, "-reps", "7"])
        for line in (out/(label+".stdout")).read_text().splitlines():
            data.write(json.dumps({"kind": "control", "mask": "0", **json.loads(line)})+"\n")
        data.flush()
    # Run the unchanged original time command as a cross-check.
    for mask in ("0", "0-7"):
        invoke("original-time-"+mask, [HERE/".build/golex-original", "time", corpus, "7"], mask)
    records.close(); data.close()
    summarize(out)


def summarize(out):
    rows = [json.loads(x) for x in (out/"measurements.jsonl").read_text().splitlines()]
    keys = {}
    for r in rows:
        key = r["name"] if r["kind"] == "whole" else f'{r["kind"]}:{r["mode"]}:{r["gc_policy"]}:{r["mask"]}'
        keys.setdefault(key, []).append(r)
    result = {}
    for key, rs in keys.items():
        times = [r["process_wall_ns"] if r["kind"] == "whole" else r["ns"] for r in rs]
        value = {"n": len(rs), "median_ms": statistics.median(times)/1e6,
                 "min_ms": min(times)/1e6, "max_ms": max(times)/1e6}
        if rs[0]["kind"] != "whole":
            for k in ("tokens", "errors", "alloc_bytes", "allocs", "gcs", "gc_pause_ns", "gomaxprocs"):
                value[k] = sorted(set(r[k] for r in rs))
            if "group" in rs[0]:
                value["best7_ms"] = {str(g): min(r["ns"] for r in rs if r["group"]==g)/1e6
                                    for g in sorted(set(r["group"] for r in rs))}
        result[key] = value
    (out/"summary.json").write_text(json.dumps(result, indent=2)+"\n")
    for key,v in result.items():
        print(key, "median %.3f ms min %.3f ms max %.3f ms" % (v["median_ms"],v["min_ms"],v["max_ms"]),
              "best7="+str(v.get("best7_ms","")))


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--corpus")
    p.add_argument("--label", default="final")
    p.add_argument("--reps", type=int, default=9)
    p.add_argument("--groups", type=int, default=3)
    p.add_argument("--seed", type=int, default=250887)
    p.add_argument("--summarize", type=Path)
    args = p.parse_args()
    if args.summarize:
        summarize(args.summarize)
    else:
        if not args.corpus:
            p.error("--corpus required")
        run(args)
