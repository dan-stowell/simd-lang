#!/usr/bin/env python3
"""Randomized, checked whole-process compiler timings. Run ./build.sh first."""
import argparse
import datetime
import json
import os
from pathlib import Path
import random
import statistics

from audit_bench import ROOT, checked_timer, digest, run


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--reps", type=int, default=21)
    ap.add_argument("--output", type=Path, required=True)
    args = ap.parse_args()
    if args.reps < 1:
        ap.error("--reps must be positive")
    work = ROOT / "build/review-compile"
    work.mkdir(parents=True, exist_ok=True)
    timer = checked_timer(work)
    large = work / "big64.strand"
    with large.open("wb") as out:
        run(["python3", ROOT / "tools/mkbig.py", 64], stdout=out)
    allowed = sorted(os.sched_getaffinity(0))
    cpusets = [[allowed[min(3, len(allowed) - 1)]]]
    if len(allowed) > 1:
        cpusets.append(allowed)
    results = {
        "date_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "revision": run(["git", "-C", ROOT, "rev-parse", "HEAD"],
                        capture_output=True, text=True).stdout.strip(),
        "gcc": run(["gcc", "--version"], capture_output=True, text=True).stdout.splitlines()[0],
        "repetitions": args.reps, "order_seed": 20261111,
        "allowed_cpus": allowed, "results": [],
    }
    jobs = []
    for source in (ROOT / "build/strandc.strand", large):
        want = None
        for cpulist in cpusets:
            cpus = ",".join(map(str, cpulist))
            for compiler in ("strandc_scalar", "stage2"):
                exe = ROOT / "build" / compiler
                got = digest(exe, source, cpus, os.environ.copy())
                if want is None:
                    want = got
                if got != want:
                    raise AssertionError((compiler, cpus, source, got, want))
                row = {"source": source.name, "source_bytes": source.stat().st_size,
                       "compiler": compiler, "cpus": cpulist,
                       "output": got, "samples": []}
                jobs.append((source, exe, cpus, row))
                results["results"].append(row)
    order = jobs * args.reps
    random.Random(results["order_seed"]).shuffle(order)
    for source, exe, cpus, row in order:
        text = run(["taskset", "-c", cpus, timer, 1, source, exe],
                   capture_output=True, text=True).stdout.strip()
        row["samples"].append({"wall_us": float(text.split()[2]), "raw": text})
    print("| source bytes | compiler | CPUs | best ms | median ms |")
    print("|---|---|---|---|---|")
    for row in results["results"]:
        times = [s["wall_us"] / 1000 for s in row["samples"]]
        row["best_ms"], row["median_ms"] = min(times), statistics.median(times)
        print(f"| {row['source_bytes']} | {row['compiler']} | {len(row['cpus'])} | "
              f"{row['best_ms']:.3f} | {row['median_ms']:.3f} |")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(results, indent=2) + "\n")


if __name__ == "__main__":
    main()
