#!/usr/bin/env python3
"""Recompute both ratio columns from retained raw samples, not printed GB/s."""
import json
from pathlib import Path
import statistics

ROOT = Path(__file__).resolve().parents[1]
E = ROOT / "reviews/audit3-2026-11-10"
groups = {
    "maps/reductions/neighbours/prefix": [
        "lines", "lines32", "words", "sum", "pairs", "nonascii", "upper",
        "rot13", "swapcase", "sum4", "sumpos4", "maxgap4", "scale4", "prefix4"],
    "xlat": ["xlat"],
    "filters": ["letters", "filter4"],
    "same-technique automata": ["strip", "idents", "strings"],
    "CSV/JSON": ["csv", "json"],
}


def compute(path):
    d = json.loads(path.read_text())
    sets = list(d["sets"])
    ratios = {}
    for name, data in d["samples"].items():
        med = {k: statistics.median(v) for k, v in data.items()}
        ratios[name] = {}
        for i, s in enumerate(sets):
            for label, kinds in (("ordinary", ("c", "omp")), ("hand", ("hand",))):
                opts = {f"{k}:{t}": med[f"{k}:{t}"] for k in kinds for t in sets[:i + 1] if f"{k}:{t}" in med}
                if not opts:
                    continue
                best = min(opts, key=opts.get)
                ratios[name][f"{label}:{s}"] = {
                    "ratio": opts[best] / med[f"st:{s}"], "best_C_config": best}
    ranges = {}
    for name, members in groups.items():
        ranges[name] = {}
        for label in ("ordinary", "hand"):
            v = [(n, k, r["ratio"]) for n in members for k, r in ratios[n].items() if k.startswith(label + ":")]
            if v:
                ranges[name][label] = {"min": min(v, key=lambda x: x[2]), "max": max(v, key=lambda x: x[2])}
    same = ["xlat", "letters", "filter4", "strip", "idents", "strings"]
    v = [(n, ratios[n]["hand:1"]["ratio"]) for n in same]
    ranges["one CPU same-technique hand"] = {"min": min(v, key=lambda x: x[1]), "max": max(v, key=lambda x: x[1])}
    return {"metadata_revision": d["revision"], "metadata_dirty": d["dirty"], "ratios": ratios, "ranges": ranges}


result = {}
for name, filename in (("claimed", "claimed-results.json"), ("fresh", "suite-results.json")):
    if (E / filename).exists():
        result[name] = compute(E / filename)
print(json.dumps(result, indent=2))
