#!/usr/bin/env python3
"""Reproduce the audit's matched mmap/OpenMP comparison, with checked exits.

Run ./build.sh first, then:
  python3 reviews/audit_bench.py --output build/review/matched.json

Builds the five existing OpenMP baselines, Strand equivalents, and a C words
variant that merely peels the first iteration. Checks output hashes for EVERY
timed CPU configuration. Randomizes individual whole-process timing trials and
retains all samples, rather than reporting only the fastest sample.
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

ROOT = Path(__file__).resolve().parents[1]


def run(args, **kwargs):
    return subprocess.run([str(x) for x in args], check=True, **kwargs)


def digest(exe, data, cpus, env):
    h = hashlib.sha256()
    size = 0
    with data.open("rb") as inp:
        p = subprocess.Popen(
            ["taskset", "-c", cpus, str(exe)],
            stdin=inp, stdout=subprocess.PIPE, env=env,
        )
        for chunk in iter(lambda: p.stdout.read(1 << 20), b""):
            h.update(chunk)
            size += len(chunk)
        p.stdout.close()
        if p.wait() != 0:
            raise RuntimeError(f"{exe} failed during correctness check")
    return {"sha256": h.hexdigest(), "bytes": size}


def checked_timer(work):
    """Keep the repository's timing boundary, but fail on unsuccessful runs."""
    timer = (ROOT / "tools/bench.c").read_text()
    needle = "wait4(p,&st,0,&ru);"
    assert timer.count(needle) == 1
    timer = timer.replace(needle, """if (wait4(p,&st,0,&ru) != p ||
        !WIFEXITED(st) || WEXITSTATUS(st) != 0) {
        fprintf(stderr, "benchmark child failed: status %d\\n", st); return 1;
    }""")
    timer = timer.replace("dup2(fd,0);", "if(fd<0 || dup2(fd,0)<0) _exit(125);")
    timer = timer.replace("dup2(o,1);", "if(o<0 || dup2(o,1)<0) _exit(126);")
    timer = timer.replace("pid_t p=fork();",
                          'pid_t p=fork(); if(p<0){perror("fork");return 1;}')
    timer = timer.replace("int st;", "int st=0;")
    (work / "bench_checked.c").write_text(timer)
    run(["gcc", "-O2", "-o", work / "bench_checked", work / "bench_checked.c"])
    return work / "bench_checked"


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--mib", type=int, default=256)
    ap.add_argument("--reps", type=int, default=9)
    ap.add_argument("--output", type=Path, required=True)
    args = ap.parse_args()
    if args.mib < 1 or args.reps < 1:
        ap.error("--mib and --reps must be positive")
    work = ROOT / "build/review-matched"
    work.mkdir(parents=True, exist_ok=True)
    allowed = sorted(os.sched_getaffinity(0))
    cpusets = {1: [allowed[min(3, len(allowed) - 1)]]}
    for count in (4, len(allowed)):
        if count <= len(allowed) and count not in cpusets:
            cpusets[count] = allowed[:count]
    env = os.environ.copy()
    # Match the repository's unbound workers inside taskset's allowed mask.
    env.update(OMP_DYNAMIC="FALSE", OMP_PROC_BIND="FALSE")
    run(["gcc", "-O2", "-o", work / "gen", ROOT / "bench/gen.c"])
    paths = {kind: work / f"{kind}{args.mib}" for kind in ("text", "ints")}
    with paths["text"].open("wb") as f:
        run([work / "gen", args.mib], stdout=f)
    rng = random.Random(7)
    chunk = b"".join(int(rng.gauss(0, 1000)).to_bytes(4, "little", signed=True)
                     for _ in range(1 << 18))
    with paths["ints"].open("wb") as f:
        for _ in range(args.mib):
            f.write(chunk)

    timer = checked_timer(work)

    original = (ROOT / "bench/base_omp.c").read_text()
    needle = """  #pragma omp parallel for reduction(+:acc) schedule(static)
  for (size_t i = 0; i < n; i++) acc += (in[i] > 32) & (i == 0 || in[i - 1] <= 32);"""
    replacement = """  acc = n && in[0] > 32;
  #pragma omp parallel for reduction(+:acc) schedule(static)
  for (size_t i = 1; i < n; i++) acc += (in[i] > 32) & (in[i - 1] <= 32);"""
    assert original.count(needle) == 1
    (work / "words_peeled.c").write_text(original.replace(needle, replacement))

    programs = {}
    selected = ("lines", "upper", "prefix4", "words", "letters")
    for line in (ROOT / "bench/programs.txt").read_text().splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        name, kind, _, src = [x.strip() for x in line.split("|", 3)]
        if name not in selected:
            continue
        source = ("\n".join(s.strip() for s in src.split(";")) + "\n").encode()
        st = work / f"st_{name}"
        st.write_bytes(run([ROOT / "build/stage2"], input=source,
                           capture_output=True).stdout)
        st.chmod(0o755)
        variants = {"Strand": st}
        for label, csrc in [("OpenMP", ROOT / "bench/base_omp.c")] + (
            [("OpenMP peeled", work / "words_peeled.c")] if name == "words" else []
        ):
            out = work / f"{name}_{label.replace(' ', '_')}"
            # stderr is retained: it identifies exactly which loops vectorized.
            with out.with_suffix(".vectorization.txt").open("w") as report:
                run(["gcc", "-O3", "-march=native", "-fopenmp",
                     "-fopt-info-vec-optimized", f"-DK=P_{name}",
                     "-o", out, csrc], stderr=report)
            variants[label] = out
        programs[name] = (paths[kind], variants)

    results = {
        "date_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "revision": run(["git", "-C", ROOT, "rev-parse", "HEAD"],
                        capture_output=True, text=True).stdout.strip(),
        "gcc": run(["gcc", "--version"], capture_output=True, text=True).stdout.splitlines()[0],
        "uname": list(os.uname()),
        "allowed_cpus": allowed,
        "cpusets": cpusets,
        "mib": args.mib,
        "repetitions": args.reps,
        "order_seed": 20261110,
        "omp_dynamic": env["OMP_DYNAMIC"],
        "omp_proc_bind": env["OMP_PROC_BIND"],
        "timing": "tools/bench.c, checked child status, one sample per invocation; "
                  "warm cached file, stdout /dev/null, randomized order",
        "results": [],
    }
    jobs = []
    for name in selected:
        data, variants = programs[name]
        want = None
        for count, cpulist in cpusets.items():
            cpus = ",".join(map(str, cpulist))
            e = dict(env, OMP_NUM_THREADS=str(count))
            for label, exe in variants.items():
                got = digest(exe, data, cpus, e)
                if want is None:
                    want = got
                if got != want:
                    raise AssertionError((name, count, label, got, want))
                row = {"program": name, "variant": label, "cpus": cpulist,
                       "input_bytes": data.stat().st_size, "output": got, "samples": []}
                results["results"].append(row)
                jobs.append((exe, data, cpus, e, row))
    order = jobs * args.reps
    random.Random(results["order_seed"]).shuffle(order)
    for exe, data, cpus, e, row in order:
        text = run(["taskset", "-c", cpus, timer, 1, data, exe],
                   env=e, capture_output=True, text=True).stdout.strip()
        wall_us = float(text.split()[2])
        row["samples"].append({"wall_us": wall_us, "raw": text})
    print("| program | variant | CPUs | best GB/s | median GB/s |")
    print("|---|---|---|---|---|")
    for row in results["results"]:
        walls = [x["wall_us"] for x in row["samples"]]
        row["best_gbps"] = row["input_bytes"] / min(walls) / 1e3
        row["median_gbps"] = row["input_bytes"] / statistics.median(walls) / 1e3
        print(f"| {row['program']} | {row['variant']} | {len(row['cpus'])} | "
              f"{row['best_gbps']:.2f} | {row['median_gbps']:.2f} |")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(results, indent=2) + "\n")


if __name__ == "__main__":
    main()
