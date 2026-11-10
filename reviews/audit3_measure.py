#!/usr/bin/env python3
"""Fresh af3ecc9 measurements. Run alone, after correctness probes finish.

Only redirects the benchmark scripts' build/output directories; does not change
their kernels, timing procedures, CPU selection or validation.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
E = ROOT / "reviews/audit3-2026-11-10"


def run(cmd, **kwargs):
    return subprocess.run(list(map(str, cmd)), check=True, **kwargs)


def sha(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for b in iter(lambda: f.read(1 << 22), b""):
            h.update(b)
    return h.hexdigest()


def main():
    E.mkdir(exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="fresh-", dir=ROOT / "build/audit3"))
    suite = work / "suite"
    source = (ROOT / "bench/run.py").read_text()
    old = "H = os.path.dirname(os.path.abspath(__file__)); R = os.path.join(H, '..'); B = os.path.join(R, 'build', 'suite')"
    assert source.count(old) == 1
    source = source.replace(old, f"H = {str(ROOT / 'bench')!r}; R = {str(ROOT)!r}; B = {str(suite)!r}")
    runner = work / "run_fresh.py"
    runner.write_text(source)
    env = {
        "started_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "work": str(work), "allowed_cpus": sorted(os.sched_getaffinity(0)),
        "revision": run(["git", "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip(),
        "uname": run(["uname", "-a"], capture_output=True, text=True).stdout.strip(),
        "lscpu": run(["lscpu"], capture_output=True, text=True).stdout,
        "sources_sha256": {
            str(p.relative_to(ROOT)): sha(p)
            for p in [ROOT / "bench" / s for s in
                      ("run.py", "oneoff.py", "gen.c", "jsongen.py", "base.c", "base_omp.c", "hand.c", "programs.txt", "xlat.h")]
            + [ROOT / "tools/bench.c", ROOT / "build/stage2"]
        },
    }
    (E / "environment.json").write_text(json.dumps(env, indent=2) + "\n")
    with (E / "suite.log").open("w") as out:
        run(["python3", runner, "256", "--rounds", "7"], stdout=out, stderr=subprocess.STDOUT)
    (E / "suite-results.json").write_bytes((suite / "results.json").read_bytes())
    env["fixtures"] = {
        p.name: {"bytes": p.stat().st_size, "sha256": sha(p)}
        for kind in ("text", "ints", "code", "csv", "json")
        for p in suite.glob(kind + "256-*")
    }
    oneoff = (ROOT / "bench/oneoff.py").read_text()
    assert oneoff.count(old) == 1
    oneoff = oneoff.replace(old, f"H = {str(ROOT / 'bench')!r}; R = {str(ROOT)!r}; B = {str(suite)!r}")
    (work / "oneoff_fresh.py").write_text(oneoff)
    with (E / "oneoff.log").open("w") as out:
        run(["python3", work / "oneoff_fresh.py"], stdout=out, stderr=subprocess.STDOUT)
    env["ended_utc"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    (E / "environment.json").write_text(json.dumps(env, indent=2) + "\n")
    print((E / "suite.log").read_text())
    print((E / "oneoff.log").read_text())


if __name__ == "__main__":
    main()
