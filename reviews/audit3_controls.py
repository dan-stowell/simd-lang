#!/usr/bin/env python3
"""Interleaved, output-checked timings of audit-only C controls.

Run after audit3_measure.py, alone. Kernels copied under reviews/ are controls,
not changes to bench/. The timing boundary is the unmodified tools/bench.c.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import random
import statistics
import subprocess

ROOT = Path(__file__).resolve().parents[1]
E = ROOT / "reviews/audit3-2026-11-10"
SUITE = Path(json.loads((E / "environment.json").read_text())["work"]) / "suite"
B = ROOT / "build/audit3-controls"
B.mkdir(exist_ok=True)
sets = json.loads((E / "suite-results.json").read_text())["sets"]
ap = argparse.ArgumentParser(description=__doc__)
ap.add_argument("--only", default="xlat,letters,filter4,strip,idents,strings,json")
ap.add_argument("--rounds", type=int, default=9)
ap.add_argument("--label", default="controls")
args = ap.parse_args()
rounds = args.rounds


def run(cmd, **kwargs):
    return subprocess.run(list(map(str, cmd)), check=True, **kwargs)


def environment(cpus):
    return dict(os.environ, OMP_NUM_THREADS=str(len(cpus)), OMP_DYNAMIC="FALSE", OMP_PROC_BIND="FALSE")


def mask(cpus):
    return ["taskset", "-c", ",".join(map(str, cpus))]


def digest(exe, inp, cpus):
    h = hashlib.sha256()
    with inp.open("rb") as f:
        p = subprocess.Popen(mask(cpus) + [str(exe)], stdin=f, stdout=subprocess.PIPE, env=environment(cpus))
        for b in iter(lambda: p.stdout.read(1 << 20), b""):
            h.update(b)
        assert p.wait() == 0, (exe, cpus)
    return h.hexdigest()


rnd = random.Random(301110)
result = {"rounds": rounds, "sets": sets, "unit": "microseconds wall",
          "sources_sha256": {}, "programs": {}}
for name, kind in (("xlat", "text"), ("letters", "text"), ("filter4", "ints"),
                   ("strip", "code"), ("idents", "code"), ("strings", "code"), ("json", "json")):
    if name not in args.only.split(","):
        continue
    inp = next(SUITE.glob(kind + "256-*"))
    binaries = {k: SUITE / f"{k}_{name}" for k in ("c", "omp", "hand", "st")}
    sources = {}
    if name in ("xlat", "letters", "filter4", "strip", "json"):
        sources["persistent"] = ROOT / "reviews/audit3-hand/hand_persistent.c"
    if name in ("strip", "idents", "strings"):
        sources["packed"] = ROOT / "reviews/audit3-hand/ordinary_packed.c"
    if name == "json":
        sources["dual"] = ROOT / "reviews/audit3-hand/hand_json_dual.c"
    for label, src in sources.items():
        exe = B / f"{label}_{name}"
        run(["gcc", "-O3", "-march=native", "-fopenmp", f"-DK=P_{name}", "-o", exe, src])
        binaries[label] = exe
        result["sources_sha256"][str(src.relative_to(ROOT))] = hashlib.sha256(src.read_bytes()).hexdigest()
    configs = [(k, s) for k in binaries for s in sets if k != "c" or s == "1"]
    expected = digest(binaries["c"], inp, sets["1"])
    samples = {f"{k}:{s}": [] for k, s in configs}
    for k, s in configs:
        assert digest(binaries[k], inp, sets[s]) == expected, (name, k, s)
    for _ in range(rounds):
        order = configs[:]
        rnd.shuffle(order)
        for k, s in order:
            text = run(mask(sets[s]) + [SUITE / "bench", "1", inp, binaries[k]],
                       capture_output=True, text=True, env=environment(sets[s])).stdout
            samples[f"{k}:{s}"].append({"wall_us": float(text.split()[2]), "timer_output": text.strip()})
    gbps = {k: inp.stat().st_size / statistics.median(x["wall_us"] for x in v) / 1000 for k, v in samples.items()}
    result["programs"][name] = {
        "input": str(inp), "input_bytes": inp.stat().st_size, "output_sha256": expected,
        "median_GBps": gbps, "samples": samples,
        "binary_sha256": {k: hashlib.sha256(v.read_bytes()).hexdigest() for k, v in binaries.items()},
    }
    (E / f"{args.label}.json").write_text(json.dumps(result, indent=2) + "\n")
    print(name, "median GB/s", json.dumps(gbps), flush=True)
