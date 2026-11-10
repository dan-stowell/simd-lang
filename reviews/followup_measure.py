#!/usr/bin/env python3
"""Reproduce the post-AUDIT review of revision 17e317d.

Run ./test.sh first, then this script. It runs bench/run.py with only its path
assignment changed, in a NEW directory so stale input files cannot be reused.
It then times an arithmetic C swapcase against the table-shaped C and Strand,
and records the original/observable I/O probes and small compilation latencies.
The original sources and pre-existing benchmark files are not changed.
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
import tempfile

from audit_bench import ROOT, digest, run


def logged(cmd, path, **kwargs):
    with path.open("w") as out:
        return run(cmd, stdout=out, stderr=subprocess.STDOUT, **kwargs)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--mib", type=int, default=256)
    ap.add_argument("--rounds", type=int, default=7)
    args = ap.parse_args()
    if args.mib < 1 or args.rounds < 1:
        ap.error("positive MiB and rounds required")
    parent = ROOT / "build/reaudit-reproduction"
    parent.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="run-", dir=parent))
    suite = work / "suite"
    print(f"Evidence directory: {work}", flush=True)
    source = (ROOT / "bench/run.py").read_text()
    old = "H = os.path.dirname(os.path.abspath(__file__)); R = os.path.join(H, '..'); B = os.path.join(R, 'build', 'suite')"
    assert source.count(old) == 1
    source = source.replace(old, f"H = {str(ROOT / 'bench')!r}; R = {str(ROOT)!r}; B = {str(suite)!r}")
    fresh_runner = work / "run_fresh.py"
    fresh_runner.write_text(source)
    logged(["python3", fresh_runner, args.mib, "--rounds", args.rounds], work / "suite.log")
    allowed = sorted(os.sched_getaffinity(0))
    cpusets = [[allowed[min(3, len(allowed) - 1)]]]
    if len(allowed) >= 4:
        cpusets.append(allowed[:4])
    if len(allowed) > 4:
        cpusets.append(allowed)
    metadata = {
        "date_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "revision": run(["git", "-C", ROOT, "rev-parse", "HEAD"],
                        text=True, capture_output=True).stdout.strip(),
        "gcc": run(["gcc", "--version"], text=True, capture_output=True).stdout.splitlines()[0],
        "allowed_cpus": allowed, "mib": args.mib, "rounds": args.rounds, "inputs": {},
    }
    for kind in ("text", "ints", "code", "csv"):
        path = suite / f"{kind}{args.mib}"
        h = hashlib.sha256()
        with path.open("rb") as f:
            for b in iter(lambda: f.read(1 << 20), b""):
                h.update(b)
        metadata["inputs"][kind] = {"bytes": path.stat().st_size, "sha256": h.hexdigest()}

    original = (ROOT / "bench/base_omp.c").read_text()
    needle = "out[i] = tab[c];"
    assert original.count(needle) == 1
    arithmetic = original.replace(needle, "{ int u = c & 223; out[i] = c ^ (((u >= 65) & (u <= 90)) * 32); }")
    (work / "swap_arithmetic.c").write_text(arithmetic)
    logged(["gcc", "-O3", "-march=native", "-fopenmp", "-fopt-info-vec-optimized",
            "-DK=P_swapcase", "-o", work / "swap_arithmetic", work / "swap_arithmetic.c"],
           work / "swap-vectorization.log")
    data = suite / f"text{args.mib}"
    variants = {"C table": suite / "omp_swapcase",
                "C arithmetic": work / "swap_arithmetic", "Strand": suite / "st_swapcase"}
    exhaustive = suite / "all256"
    exhaustive.write_bytes(bytes(range(256)))
    expected = bytes(c ^ 32 if 65 <= (c & 223) <= 90 else c for c in range(256))
    for exe in variants.values():
        with exhaustive.open("rb") as inp:
            got = run([exe], stdin=inp, capture_output=True).stdout
        assert got == expected
    metadata["swapcase_exhaustive_256_check"] = "passed"
    rows, jobs = [], []
    want = None
    for cpus in cpusets:
        mask = ",".join(map(str, cpus))
        env = dict(os.environ, OMP_NUM_THREADS=str(len(cpus)), OMP_DYNAMIC="FALSE", OMP_PROC_BIND="FALSE")
        for label, exe in variants.items():
            got = digest(exe, data, mask, env)
            if want is None:
                want = got
            assert got == want, (cpus, label, got, want)
            row = {"variant": label, "cpus": cpus, "output": got, "samples": []}
            rows.append(row)
            jobs.append((exe, mask, env, row))
    order = jobs * 9
    random.Random(20261110).shuffle(order)
    for exe, mask, env, row in order:
        text = run(["taskset", "-c", mask, suite / "bench", 1, data, exe],
                   env=env, text=True, capture_output=True).stdout.strip()
        row["samples"].append({"wall_us": float(text.split()[2]), "raw": text})
    with (work / "swapcase.log").open("w") as out:
        for row in rows:
            samples = [s["wall_us"] for s in row["samples"]]
            row["median_gbps"] = data.stat().st_size / statistics.median(samples) / 1e3
            row["best_gbps"] = data.stat().st_size / min(samples) / 1e3
            print(row["variant"], len(row["cpus"]), "CPUs:", "median", row["median_gbps"],
                  "best", row["best_gbps"], file=out)
    (work / "swapcase.json").write_text(json.dumps(rows, indent=2) + "\n")

    # An observable, race-free version of the existing I/O experiment.
    io = (ROOT / "explore/iotest.c").read_text()
    assert io.count("static unsigned long sums[16];") == 1
    fixed = io.replace("static unsigned long sums[16];",
                       "static unsigned long sums[16]; static volatile unsigned long sink;")
    needle = "pthread_join(th[t],0); return now()-t0;"
    assert fixed.count(needle) == 1
    fixed = fixed.replace(needle, "pthread_join(th[t],0); for(int t=0;t<T;t++) sink+=sums[t]; return now()-t0;")
    (work / "iotest_observable.c").write_text(fixed)
    for label, src in (("original", ROOT / "explore/iotest.c"), ("observable", work / "iotest_observable.c")):
        exe = work / f"iotest_{label}"
        run(["gcc", "-O2", "-pthread", "-o", exe, src])
        logged(["objdump", "-d", "-M", "intel", "--disassemble=touch", exe],
               work / f"iotest_{label}.asm")
        logged([exe, data], work / f"iotest_{label}.log")
    run(["gcc", "-O2", "-o", work / "probe", ROOT / "hw/probe.c"])
    logged(["python3", ROOT / "hw/check_probe.py", work / "probe"], work / "probe-check.log")
    logged(["python3", ROOT / "hw/check_probe.py", "/bin/true"], work / "probe-check-true.log")
    logged(["taskset", "-c", str(cpusets[0][0]), work / "probe"], work / "hardware.log")
    with (work / "compile-latency.log").open("w") as out:
        for name in ("words", "strip"):
            for label, inp, cmd in (
                ("Strand", suite / f"{name}.strand", [ROOT / "build/stage2"]),
                ("gcc-O3", "/dev/null", ["/usr/bin/gcc", "-O3", "-march=native", "-fopenmp",
                                        f"-DK=P_{name}", "-o", work / f"compile_{name}", ROOT / "bench/base_omp.c"]),
                ("gcc-O0", "/dev/null", ["/usr/bin/gcc", "-O0", "-fopenmp",
                                        f"-DK=P_{name}", "-o", work / f"compile_{name}", ROOT / "bench/base_omp.c"]),
            ):
                print(name, label, flush=True, file=out)
                run(["taskset", "-c", str(cpusets[0][0]), suite / "bench", 21, inp, *cmd], stdout=out)
    (work / "environment.json").write_text(json.dumps(metadata, indent=2) + "\n")
    (parent / "last-run.txt").write_text(str(work) + "\n")
    print((work / "suite.log").read_text(), flush=True)
    print((work / "swapcase.log").read_text(), flush=True)
    print((work / "iotest_original.log").read_text(), flush=True)
    print((work / "iotest_observable.log").read_text(), flush=True)
    print((work / "compile-latency.log").read_text(), flush=True)


if __name__ == "__main__":
    main()
