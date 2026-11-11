#!/usr/bin/env python3
"""Correctness only: no performance measurements."""
import hashlib
import json
import os
import pathlib
import subprocess
from oracle import ROOT, OUT, generate
from measure import COMMANDS

D = ROOT / "reviews/audit4-linkjob"
B = ROOT / "build/audit4-linkjob"
generate()
cpus = sorted(os.sched_getaffinity(0))
one = [cpus[0]]
variants = {
    "ref_o2": ([str(B / "ref_o2"), "ref"], one),
    "ref_o3": ([str(B / "ref_o3"), "ref"], one),
    "c_full": ([str(B / "variants"), "full"], one),
    "c_stream": ([str(B / "variants"), "stream"], one),
    "c_parallel_1": ([str(B / "variants"), "parallel"], one),
    "c_parallel_all": ([str(B / "variants"), "parallel"], cpus),
    "c_fingerprint": ([str(B / "variants"), "fingerprint"], one),
    "link_1": ([str(B / "link")], one),
    "link_eager_all": ([str(B / "link"), "eager"], cpus),
    "link2_1": ([str(B / "link2")], one),
    "link2_eager_all": ([str(B / "link2"), "eager"], cpus),
}
for name in ["ref_o2_bytes", "ref_o2_nf", "ref_o3_nf", "c_full_nf", "c_checked_nf",
             "c_stream_nf", "c_parallel_nf", "c_fingerprint_nf"]:
    variants[name] = (COMMANDS[name], one)
variants["c_parallel_nf_all"] = (COMMANDS["c_parallel_nf"], cpus)
manifest = json.loads((D / "cases.json").read_text())
results = []
for case in manifest:
    wanted = (OUT / (case["name"] + ".expected")).read_bytes()
    for name, (cmd, affinity) in variants.items():
        env = dict(os.environ, OMP_NUM_THREADS=str(len(affinity)), OMP_DYNAMIC="FALSE")
        with (OUT / (case["name"] + ".job")).open("rb") as f:
            p = subprocess.run(["taskset", "-c", ",".join(map(str, affinity))] + cmd,
                               stdin=f, capture_output=True, env=env, timeout=15)
        match = p.returncode == 0 and p.stdout == wanted
        row = dict(case=case["name"], category=case["category"], variant=name,
                   argv=cmd, affinity=affinity, exit=p.returncode, match=match,
                   output_sha256=hashlib.sha256(p.stdout).hexdigest(),
                   output_bytes=len(p.stdout), stderr=p.stderr.decode(errors="replace"))
        if not match:
            row["first_difference"] = next((i for i, (a, b) in enumerate(zip(p.stdout, wanted))
                                            if a != b), min(len(p.stdout), len(wanted)))
            (B / (case["name"] + "." + name + ".actual")).write_bytes(p.stdout)
            print(json.dumps(row), flush=True)
        results.append(row)
(D / "checks.json").write_text(json.dumps(dict(variants=variants, cases=results), indent=2) + "\n")
summary = {}
for name in variants:
    rows = [r for r in results if r["variant"] == name]
    summary[name] = dict(pass_count=sum(r["match"] for r in rows),
                         total=len(rows), failures=[r["case"] for r in rows if not r["match"]])
(D / "checks-summary.json").write_text(json.dumps(summary, indent=2) + "\n")
print(json.dumps(summary, indent=2))
