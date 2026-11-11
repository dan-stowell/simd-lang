#!/usr/bin/env python3
"""Audit-only stronger-flags controls. Run ALONE after measure_compiler.py."""
import hashlib
import json
import os
from pathlib import Path
import random
import statistics
import subprocess

R = Path(__file__).resolve().parents[2]
O = Path(__file__).resolve().parent
B = R / "build/audit4"
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
variants = [("c_o2", ["-O2"]),
            ("c_o3", ["-O3", "-march=native"]),
            ("c_o2_nf", ["-O2", "-U_FORTIFY_SOURCE", "-D_FORTIFY_SOURCE=0"]),
            ("c_o3_nf", ["-O3", "-march=native", "-U_FORTIFY_SOURCE", "-D_FORTIFY_SOURCE=0"])]
commands = []
for name, flags in variants:
    command = ["gcc", *flags, "-Ibuild", "boot/strandc.c", "-o", str(B / name)]
    subprocess.run(command, cwd=R, check=True)
    commands.append(command)
variants = [(name, B / name) for name, _ in variants] + [("strand", R / "build/stage2")]
cpu = sorted(os.sched_getaffinity(0))[min(3, len(os.sched_getaffinity(0)) - 1)]
rows = []
for size in (8, 64):
    src = B / f"big{size}.strand"
    want = None
    for name, exe in variants:
        with src.open("rb") as f:
            p = subprocess.run(["taskset", "-c", str(cpu), exe], stdin=f, capture_output=True, check=True)
        digest = hashlib.sha256(p.stdout).hexdigest()
        if want is not None:
            assert want == digest
        want = digest
        rows.append({"name": name, "source": str(src.relative_to(R)), "source_sha256": sha(src),
                     "exe": str(exe.relative_to(R)), "exe_sha256": sha(exe), "output_sha256": digest,
                     "samples": []})
order, rng = [], random.Random(20261111)
for trial in range(15):
    indices = list(range(len(rows)))
    rng.shuffle(indices)
    for i in indices:
        row = rows[i]
        p = subprocess.run(["taskset", "-c", str(cpu), B / "timer", R / row["source"], R / row["exe"]],
                           capture_output=True, text=True, check=True)
        row["samples"].append(json.loads(p.stdout))
        order.append({"trial": trial, "row": i})
for row in rows:
    row["wall_median_ms"] = statistics.median(s["wall_ms"] for s in row["samples"])
    row["wall_min_ms"] = min(s["wall_ms"] for s in row["samples"])
    row["wall_max_ms"] = max(s["wall_ms"] for s in row["samples"])
report = {"cpu": cpu, "seed": 20261111, "commands": commands, "rows": rows, "order": order,
          "source_sha256": sha(R / "boot/strandc.c"), "headers_sha256": sha(R / "build/tables.h")}
(O / "compiler-flags.json").write_text(json.dumps(report, indent=2) + "\n")
for row in rows:
    print(row["source"], row["name"], row["wall_median_ms"], row["wall_min_ms"], row["wall_max_ms"])
