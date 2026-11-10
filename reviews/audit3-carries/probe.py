#!/usr/bin/env python3
"""Deterministic, correctness-only carry composition probes. Run from repository root."""
import hashlib, itertools, json, os, pathlib, random, subprocess, sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / "build/audit3-carries"
OUT.mkdir(parents=True, exist_ok=True)
sys.path.insert(0, str(ROOT / "boot"))
import strand

r = random.Random(909)
allowed = sorted(os.sched_getaffinity(0))
compiler = ROOT / "build/stage2"
stub = (ROOT / "build/stub.bin").read_bytes()
syms = {p[2]: int(p[0], 16) for l in subprocess.check_output(
    ["nm", ROOT / "build/stub.o"], text=True).splitlines() if len(p := l.split()) == 3}
patch = bytes.fromhex("b8e7000000bf4d0000000f05")
cpusets = [",".join(map(str, allowed[:n])) for n in (2, 3, 4, 8) if n <= len(allowed)]
programs = {}
ops = ["scan", "scanmax", "prev", "scan scanmax", "scanmax scan", "prev scanmax"]
for i in range(56):
    depth = i % 7 + 1
    words = ["input", "128", "-"]
    for d in range(depth):
        if d:
            words += ["dup"]
        words += r.choice(["257 * 17 -", "7 & 3 -", "16777216 * 31 +", "0 max"]).split()
        words += r.choice(ops).split()
    words += ["+"] * (depth - 1) + ["out4"]
    programs[f"dword-{i:02}"] = " ".join(words) + "\n"
perm = "0 1 2 3  1 0 2 3  1 2 3 0  0 1 3 2 =p\n"
for i in range(40):
    # Include constants, non-injective maps, and noncommuting permutations.
    table = " ".join(str(r.randrange(4)) for _ in range(64)) + " =a\n"
    depth = i % 7 + 1
    words = ["bytes"]
    for d in range(depth):
        if d:
            words += ["dup"]
        words += r.choice(["3 & /p", "15 & /a", "prev", "7 + prev", "3 ^ /p"]).split()
    words += ["+"] * (depth - 1) + ["out"]
    programs[f"bytes-{i:02}"] = perm + table + " ".join(words) + "\n"
programs.update({
    "old-counterexample": "input 1 & scan dup prev dup prev + + out4\n",
    "max-high-seed": "input 128 - scan dup 0 swap scanmax + + out4\n",
    "max-prev-scan": "input 128 - scanmax dup prev dup scan + + out4\n",
    "local-tee-max": "input 128 - :x scanmax dup x + scan + out4\n",
    "reduce-retry": "input 1 & scan dup 3 & scan dup 7 & scan + + $S\n~1 S + out4\n",
    "eight-prev-dword": "input prev " + "dup prev " * 7 + "+ " * 7 + "out4\n",
    "eight-prev-byte": "bytes prev " + "dup prev " * 7 + "+ " * 7 + "out\n",
    "eight-mixed-carries": "input scanmax prev " + "dup scanmax prev " * 7 + "+ " * 7 + "out4\n",
})
data = bytes(r.randrange(256) for _ in range(4097))
(OUT / "small.in").write_bytes(data)
records = []
for name, source in programs.items():
    (OUT / f"{name}.strand").write_text(source)
    comp = subprocess.run([compiler], input=source.encode(), capture_output=True, timeout=10)
    assert comp.returncode == 0, (name, comp.returncode, comp.stderr)
    elf = comp.stdout
    at = 112 + syms["fsbad"]
    assert elf[at:at + len(patch)] == stub[syms["fsbad"]:syms["fsbad"] + len(patch)]
    exe = OUT / name
    exe.write_bytes(elf)
    exe.chmod(0o755)
    detector = OUT / (name + "-detect")
    detector.write_bytes(elf[:at] + patch + elf[at + len(patch):])
    detector.chmod(0o755)
    expected = strand.interp(source.encode(), data)
    for cpus in [str(allowed[0])] + cpusets:
        p = subprocess.run(["taskset", "-c", cpus, exe, "eager"], input=data, capture_output=True, timeout=10)
        assert (p.returncode, p.stdout) == expected, (name, cpus, p.returncode, expected[0])
        q = subprocess.run(["taskset", "-c", cpus, detector, "eager"], input=data, capture_output=True, timeout=10)
        assert q.returncode in (0, 77), (name, cpus, q.returncode)
        records.append({"name": name, "cpus": cpus, "n": len(data), "mode": "eager",
                        "status": p.returncode, "output_sha256": hashlib.sha256(p.stdout).hexdigest(),
                        "fallback_detector": q.returncode})
    # Production-size, multi-block controls; serial native execution is the oracle here.
    if name in ("old-counterexample", "max-high-seed", "max-prev-scan", "reduce-retry", "bytes-09"):
        big = bytes([1]) * 524289 if name == "old-counterexample" else data * 129
        (OUT / (name + ".in")).write_bytes(big)
        p = subprocess.run(["taskset", "-c", str(allowed[0]), exe], input=big, capture_output=True, timeout=10)
        for cpus in cpusets:
            q = subprocess.run(["taskset", "-c", cpus, exe], input=big, capture_output=True, timeout=10)
            assert (q.returncode, q.stdout) == (p.returncode, p.stdout), (name, cpus, "production")
            d = subprocess.run(["taskset", "-c", cpus, detector], input=big, capture_output=True, timeout=10)
            assert d.returncode in (0, 77), (name, cpus, d.returncode)
            records.append({"name": name, "cpus": cpus, "n": len(big), "mode": "normal",
                            "status": q.returncode, "output_sha256": hashlib.sha256(q.stdout).hexdigest(),
                            "fallback_detector": d.returncode})
rejects = {}
for name, source in {
    "two-prev-same-depth": "input prev prev out4\n",
    "nine-prev": "input prev " + "dup prev " * 7 + "+ prev " + "+ " * 6 + "out4\n",
}.items():
    comp = subprocess.run([compiler], input=source.encode(), capture_output=True, timeout=10)
    assert comp.returncode == 10, (name, comp.returncode)
    rejects[name] = {"source": source, "status": comp.returncode}
result = {"revision": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
          "compiler_sha256": hashlib.sha256(compiler.read_bytes()).hexdigest(),
          "stub_sha256": hashlib.sha256(stub).hexdigest(), "programs": programs,
          "compile_rejections": rejects, "runs": records}
(ROOT / "reviews/audit3-carries/probe-results.json").write_text(json.dumps(result, indent=2) + "\n")
print("PASS", len(programs), "programs", len(records), "runs;",
      sum(x["fallback_detector"] == 77 for x in records), "fallbacks")
