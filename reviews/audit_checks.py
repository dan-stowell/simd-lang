#!/usr/bin/env python3
"""Record small, deterministic audit counterexamples; requires ./build.sh.

This is an audit recorder, not a release gate: some recorded observations are
intentional reproductions of defects. See AUDIT.md for their interpretation.
"""
import argparse
import json
import os
from pathlib import Path
import struct
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "boot"))
import strand
from spec import K, name_hash, race_key, race_mask


def run(cmd, **kwargs):
    return subprocess.run([str(x) for x in cmd], capture_output=True, **kwargs)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--output", type=Path, required=True)
    args = ap.parse_args()
    work = ROOT / "build/review-checks"
    work.mkdir(parents=True, exist_ok=True)
    observations = {}

    # A deterministic collision-witness tree, not a fingerprint collision.
    names = []
    counter = 0
    mask = race_mask(K + 32)

    def fresh(round_=None, target=None):
        nonlocal counter
        while True:
            counter += 1
            name = f"X{counter}".encode()
            if round_ is None or race_key(round_, name_hash(name), mask) == race_key(
                round_, name_hash(target), mask
            ):
                return name

    def witness(name, rounds):
        names.append(name)
        for round_ in range(rounds):
            witness(fresh(round_, name), round_)

    witness(fresh(), 4)
    src = b"".join(b"0 >" + name + b"\n" for name in names)
    (work / "race16.strand").write_bytes(src)
    try:
        strand.compile_(src)
        py_status = 0
    except strand.CompileError as e:
        py_status = e.code
    statuses = {"Python": py_status}
    for comp in ("strandc_scalar", "stage1", "stage2", "stage3"):
        p = run([ROOT / "build" / comp], input=src, timeout=20)
        statuses[comp] = {"exit": p.returncode, "stdout_bytes": len(p.stdout)}
    observations["race"] = {
        "source": src.decode(), "name_hash_trials": counter, "mask": mask,
        "expected_exit": 4, "actual": statuses,
    }

    src = b"~300000 out\n1 halt\n"
    compiled = run([ROOT / "build/stage2"], input=src, check=True)
    exe = work / "halt"
    exe.write_bytes(compiled.stdout)
    exe.chmod(0o755)
    cpu = str(min(os.sched_getaffinity(0)))
    actual = run(["taskset", "-c", cpu, exe], input=b"", timeout=20)
    reference = strand.interp(src, b"")
    observations["halt_output"] = {
        "source": src.decode(),
        "interpreter": {"exit": reference[0], "stdout_bytes": len(reference[1])},
        "binary": {"exit": actual.returncode, "stdout_bytes": len(actual.stdout)},
    }

    run(["gcc", "-O2", "-o", work / "bench", ROOT / "tools/bench.c"], check=True)
    measured = run([work / "bench", 1, "/dev/null", "/bin/false"], timeout=20)
    observations["timer_accepts_failed_child"] = {
        "child_exit": 1, "timer_exit": measured.returncode,
        "timer_stdout": measured.stdout.decode(),
    }

    # Force legal one-byte reads so that a final partial block is not the only
    # partial block. Interpose glibc's fortified form as well as plain read().
    shortread = work / "shortread.c"
    shortread.write_text("""#include <unistd.h>
#include <sys/syscall.h>
ssize_t read(int fd,void*p,size_t n){return syscall(SYS_read,fd,p,n>1?1:n);}
ssize_t __read_chk(int fd,void*p,size_t n,size_t len){
  return syscall(SYS_read,fd,p,n>1?1:n);
}
""")
    run(["gcc", "-O2", "-shared", "-fPIC", "-o", work / "shortread.so", shortread],
        check=True)
    env = dict(os.environ, LD_PRELOAD=str(work / "shortread.so"))
    shortread_results = {}
    for kernel, inp in (("swapcase", b"aB"), ("lines32", b"\n\n")):
        exe = work / kernel
        run(["gcc", "-O2", "-fno-tree-vectorize", f"-DK=P_{kernel}",
             "-o", exe, ROOT / "bench/base.c"], check=True)
        normal = run([exe], input=inp, check=True)
        limited = run([exe], input=inp, env=env, check=True)
        shortread_results[kernel] = {
            "input_hex": inp.hex(), "normal_hex": normal.stdout.hex(),
            "short_read_hex": limited.stdout.hex(),
        }
    observations["c_baseline_short_reads"] = shortread_results

    env = dict(os.environ, OMP_NUM_THREADS="4", OMP_THREAD_LIMIT="1", OMP_DYNAMIC="FALSE")
    team_results = {}
    for kernel, inp, expected in (
        ("letters", b"abcd", b"abcd"),
        ("prefix4", struct.pack("<4i", 1, 2, 3, 4), struct.pack("<4i", 1, 3, 6, 10)),
    ):
        exe = work / ("omp_" + kernel)
        run(["gcc", "-O3", "-march=native", "-fopenmp", f"-DK=P_{kernel}",
             "-o", exe, ROOT / "bench/base_omp.c"], check=True)
        path = work / (kernel + ".input")
        path.write_bytes(inp)
        with path.open("rb") as f:
            p = run([exe], stdin=f, env=env, check=True)
        team_results[kernel] = {"expected_hex": expected.hex(), "actual_hex": p.stdout.hex()}
    observations["openmp_reduced_team"] = team_results

    # Call exactly the original scalar prefix kernel on its original rand()
    # distribution, without running the expensive timing loops.
    wrapper = work / "prefix_ubsan.c"
    wrapper.write_text(f"""#define main unused_probe_main
#include {json.dumps(str(ROOT / 'hw/probe.c'))}
#undef main
int main(void) {{
  A=POOL; C=POOL+2*N; srand(1);
  for(int i=0;i<N;i++) {{ A[i]=rand(); rand(); }}
  k_prefix_scalar(); return 0;
}}
""")
    run(["gcc", "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=undefined",
         "-o", work / "prefix_ubsan", wrapper], check=True)
    p = run([work / "prefix_ubsan"])
    observations["hardware_scalar_prefix_ubsan"] = {
        "exit": p.returncode, "stderr": p.stderr.decode(),
    }
    run(["gcc", "-O2", "-o", work / "probe", ROOT / "hw/probe.c"], check=True)
    disassembly = {}
    for fn in ("p_avx2", "p_adx"):
        p = run(["objdump", "-d", "-M", "intel", f"--disassemble={fn}", work / "probe"],
                check=True, text=True)
        disassembly[fn] = p.stdout
    observations["execution_probe_disassembly"] = disassembly
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(observations, indent=2) + "\n")
    for key, value in observations.items():
        if key == "execution_probe_disassembly":
            continue
        print(key + ": " + json.dumps(value))


if __name__ == "__main__":
    main()
