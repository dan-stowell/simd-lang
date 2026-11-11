#!/usr/bin/env python3
"""Isolated-window, randomized whole-process benchmarks. NEVER run automatically.

Output checks hash real stdout before/after the campaign, outside timing.
Timed samples write /dev/null to reproduce LINKING.md; they cannot themselves
be output-hashed. Each sample records that fact plus its preflight output hash.
No compilation/generation is hidden in the measurement window.
"""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import random
import resource
import statistics
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[2]
B = ROOT / "build/audit4-linkjob"
D = ROOT / "reviews/audit4-linkjob"
COMMANDS = {
    "ref_o2": [str(B / "ref_o2"), "ref"],
    "ref_o2_bytes": [str(B / "ref_o2_bytes"), "ref"],
    "ref_o3": [str(B / "ref_o3"), "ref"],
    "ref_o2_nf": [str(B / "ref_o2_nf"), "ref"],
    "ref_o3_nf": [str(B / "ref_o3_nf"), "ref"],
    "c_full": [str(B / "variants"), "full"],
    "c_stream": [str(B / "variants"), "stream"],
    "c_parallel": [str(B / "variants"), "parallel"],
    "c_fingerprint": [str(B / "variants"), "fingerprint"],
    "c_full_nf": [str(B / "variants_nf"), "full"],
    "c_checked_nf": [str(B / "variants_nf"), "checked"],
    "c_stream_nf": [str(B / "variants_nf"), "stream"],
    "c_parallel_nf": [str(B / "variants_nf"), "parallel"],
    "c_fingerprint_nf": [str(B / "variants_nf"), "fingerprint"],
    "link": [str(B / "link")],
    "link2": [str(B / "link2")],
}


def sha_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(4 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def output_check(cmd, cpus, job, env):
    with open(job, "rb") as f:
        p = subprocess.Popen(["taskset", "-c", ",".join(map(str, cpus))] + cmd,
                             stdin=f, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
        h = hashlib.sha256()
        count = 0
        for chunk in iter(lambda: p.stdout.read(4 << 20), b""):
            h.update(chunk)
            count += len(chunk)
        error = p.stderr.read().decode(errors="replace")
        rc = p.wait()
    return dict(sha256=h.hexdigest(), bytes=count, exit=rc, stderr=error)


def sample(cmd, cpus, job, env):
    inf = os.open(job, os.O_RDONLY)
    out = os.open("/dev/null", os.O_WRONLY)
    start = time.perf_counter_ns()
    pid = os.fork()
    if not pid:
        try:
            os.sched_setaffinity(0, cpus)
            os.dup2(inf, 0)
            os.dup2(out, 1)
            os.close(inf)
            os.close(out)
            os.execve(cmd[0], cmd, env)
        except BaseException:
            os._exit(127)
    _, status, usage = os.wait4(pid, 0)
    stop = time.perf_counter_ns()
    os.close(inf)
    os.close(out)
    return dict(wall_ns=stop - start, exit=os.waitstatus_to_exitcode(status),
                rusage={key: getattr(usage, key) for key in dir(usage) if key.startswith("ru_")})


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--authorize-isolated", action="store_true", help="parent explicitly granted exclusive window")
    ap.add_argument("--job", type=pathlib.Path, default=ROOT / "build/job2000")
    ap.add_argument("--rounds", type=int, default=7)
    ap.add_argument("--seed", type=int, default=410109)
    ap.add_argument("--variants", default="ref_o2,ref_o2_bytes,ref_o3,ref_o2_nf,ref_o3_nf,c_full_nf,c_checked_nf,c_stream_nf,c_parallel_nf,c_fingerprint_nf,link,link2")
    ap.add_argument("--output", type=pathlib.Path, default=D / "timings.json")
    ap.add_argument("--expected-sha256", help="optional independently verified output hash")
    args = ap.parse_args()
    if not args.authorize_isolated:
        ap.error("No timing permission: require --authorize-isolated after parent grants window")
    assert args.rounds > 0
    names = args.variants.split(",")
    allowed = sorted(os.sched_getaffinity(0))
    cpusets = {"1": allowed[:1]}
    if len(allowed) > 1:
        cpusets[str(len(allowed))] = allowed
    configs = [(name, n) for name in names for n in cpusets]
    argv = {name: COMMANDS[name] for name in names}
    programs = sorted(set(cmd[0] for cmd in argv.values()))
    sources = ["explore/linkjob.c", "examples/link.strand", "examples/link2.strand",
               "stencils/stub.S", "tools/mkstencils.py",
               "reviews/audit4-linkjob/ref_bytestore.c",
               "reviews/audit4-linkjob/variants.c", "reviews/audit4-linkjob/measure.py"]
    result = dict(start_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                  revision=subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
                  command_line=os.sys.argv, rounds=args.rounds, seed=args.seed,
                  uname=list(os.uname()), cpusets=cpusets, commands=argv,
                  input=dict(path=str(args.job), bytes=args.job.stat().st_size, sha256=sha_file(args.job)),
                  sources={s: sha_file(ROOT / s) for s in sources},
                  executables={p: sha_file(p) for p in programs},
                  cpuinfo=pathlib.Path("/proc/cpuinfo").read_text(),
                  meminfo_start=pathlib.Path("/proc/meminfo").read_text(),
                  gcc=subprocess.check_output(["gcc", "--version"], text=True).splitlines()[0],
                  timing_contract="fork-to-wait4; regular-file cached stdin; /dev/null stdout; no timed hash consumer",
                  preflight={}, postflight={}, samples=[], summary={})

    def save():
        args.output.write_text(json.dumps(result, indent=2) + "\n")

    envs = {n: dict(os.environ, OMP_NUM_THREADS=str(len(cpus)), OMP_DYNAMIC="FALSE",
                    OMP_PROC_BIND="FALSE") for n, cpus in cpusets.items()}
    expected = args.expected_sha256
    for name, n in configs:
        key = name + ":" + n
        got = output_check(argv[name], cpusets[n], args.job, envs[n])
        result["preflight"][key] = got
        if expected is None:
            expected = got["sha256"]
            result["expected_hash_origin"] = "first configured implementation; small cases independently oracled"
        save()
        if got["exit"] or got["sha256"] != expected:
            raise SystemExit(f"preflight mismatch {key}: {got}")
    result["expected_output_sha256"] = expected
    rng = random.Random(args.seed)
    for roundno in range(args.rounds):
        order = configs.copy()
        rng.shuffle(order)
        for name, n in order:
            key = name + ":" + n
            row = sample(argv[name], cpusets[n], args.job, envs[n])
            row.update(round=roundno, sequence=len(result["samples"]), variant=name,
                       cpuset=n, affinity=cpusets[n], argv=argv[name],
                       input_sha256=result["input"]["sha256"],
                       executable_sha256=result["executables"][argv[name][0]],
                       preflight_output_sha256=result["preflight"][key]["sha256"],
                       output_hashed_during_sample=False)
            result["samples"].append(row)
            save()
            print(f'{roundno} {key} {row["wall_ns"]/1e6:.3f} ms user={row["rusage"]["ru_utime"]:.3f} sys={row["rusage"]["ru_stime"]:.3f}', flush=True)
            if row["exit"]:
                raise SystemExit(f"timed sample failed: {row}")
    for name, n in configs:
        key = name + ":" + n
        got = output_check(argv[name], cpusets[n], args.job, envs[n])
        result["postflight"][key] = got
        save()
        if got["exit"] or got["sha256"] != expected:
            raise SystemExit(f"postflight mismatch {key}: {got}")
        rows = [r for r in result["samples"] if r["variant"] == name and r["cpuset"] == n]
        walls = [r["wall_ns"]/1e6 for r in rows]
        result["summary"][key] = dict(samples=len(rows), median_ms=statistics.median(walls),
                                     min_ms=min(walls), max_ms=max(walls),
                                     median_user_ms=1000*statistics.median(r["rusage"]["ru_utime"] for r in rows),
                                     median_sys_ms=1000*statistics.median(r["rusage"]["ru_stime"] for r in rows),
                                     median_minflt=statistics.median(r["rusage"]["ru_minflt"] for r in rows),
                                     median_maxrss_kib=statistics.median(r["rusage"]["ru_maxrss"] for r in rows))
    result["end_utc"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    save()
    print(json.dumps(result["summary"], indent=2))


if __name__ == "__main__":
    main()
