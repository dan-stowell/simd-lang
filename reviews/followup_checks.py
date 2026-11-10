#!/usr/bin/env python3
"""Post-AUDIT correctness checks and deterministic claim counterexamples.

Requires ./build.sh. Records, rather than fixes, optimization fallback and the
toy grammar's difference from Python. Only scratch ELFs are instrumented.
"""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tokenize

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "boot"))
import strand


def run(cmd, **kwargs):
    return subprocess.run([str(x) for x in cmd], **kwargs)


def main():
    work = ROOT / "build/reaudit-checks"
    work.mkdir(parents=True, exist_ok=True)
    cpus = sorted(os.sched_getaffinity(0))
    one, multi = str(cpus[0]), ",".join(map(str, cpus[:4]))
    observations = {"revision": run(["git", "-C", ROOT, "rev-parse", "HEAD"],
                                    check=True, capture_output=True, text=True).stdout.strip()}
    # Each class is a permutation, so no prefix forgets its initial state.
    table = [(s + (c % 3) + 1) % 4 if c & 1 else s ^ (c % 4)
             for c in range(16) for s in range(4)]
    prefix = (" ".join(map(str, table)) + " =d\n").encode()
    data = bytes((i * 73 + (i >> 3) * 19) & 255 for i in range(524289))
    cases = []
    for depth in range(1, 9):
        cases.append((f"depth{depth}", prefix + (
            "bytes " + "dup " * (depth - 1) + "/d " + "+ " * (depth - 1) + "out\n").encode(), data[:4097]))
    for name, program in (
        ("permutation", b"bytes /d out\n"),
        ("prev", b"bytes /d dup prev + out\n"),
        ("prev2", b"bytes /d dup prev dup prev + + out\n"),
        ("dependent automata", b"bytes /d dup /d + out\n"),
    ):
        cases.append((name, prefix + program, data))
    for n in (0, 1, 3, 4, 15, 16, 17, 63, 64, 65, 127, 128, 129, 513):
        cases.append((f"boundary{n}", prefix + b"bytes /d out\n", data[:n]))
    checked = []
    for name, src, inp in cases:
        elf = run([ROOT / "build/stage2"], input=src, capture_output=True, check=True).stdout
        exe = work / "program"
        exe.write_bytes(elf)
        exe.chmod(0o755)
        expected = strand.interp(src, inp)
        for mask, mode in ((one, []), (multi, []), (multi, ["eager"])):
            p = run(["taskset", "-c", mask, exe, *mode], input=inp, capture_output=True, timeout=30)
            assert (p.returncode, p.stdout) == expected, (name, mask, mode, p.returncode)
        checked.append({"case": name, "input_bytes": len(inp),
                        "source": src.decode(), "output_sha256": hashlib.sha256(expected[1]).hexdigest()})
        print("PASS", name, len(inp), flush=True)
    observations["targeted"] = checked

    nm = run(["nm", ROOT / "build/stub.o"], check=True, capture_output=True, text=True).stdout
    syms = {p[2]: int(p[0], 16) for line in nm.splitlines() if len(p := line.split()) == 3}
    stub = (ROOT / "build/stub.bin").read_bytes()
    patch = bytes.fromhex("b8e7000000bf4d0000000f05")  # exit_group(77)
    fallback = []
    ones = work / "ones"
    ones.write_bytes(b"\1" * 524289)
    for name, src in (
        ("scan-prev", b"input 1 & scan dup prev + out4\n"),
        ("scan-prev-prev", b"input 1 & scan dup prev dup prev + + out4\n"),
        ("DFA", prefix + b"bytes /d out\n"),
        ("DFA-prev", prefix + b"bytes /d dup prev + out\n"),
    ):
        elf = run([ROOT / "build/stage2"], input=src, capture_output=True, check=True).stdout
        # The current ELF has its startup stub immediately after the 112-byte
        # ELF/program headers. Check the exact instruction bytes before patching.
        start = 112 + syms["fsbad"]
        assert elf[start:start + len(patch)] == stub[syms["fsbad"]:syms["fsbad"] + len(patch)]
        original, instrumented = work / "unmodified", work / "fallback_detector"
        original.write_bytes(elf)
        instrumented.write_bytes(elf[:start] + patch + elf[start + len(patch):])
        original.chmod(0o755)
        instrumented.chmod(0o755)
        expected = strand.interp(src, ones.read_bytes())
        for count in (1, 2, 3, 4, 8):
            if count > len(cpus):
                continue
            mask = ",".join(map(str, cpus[:count]))
            with ones.open("rb") as inp:
                p = run(["taskset", "-c", mask, original], stdin=inp, capture_output=True, timeout=30)
            assert (p.returncode, p.stdout) == expected, (name, count, "unmodified")
            with ones.open("rb") as inp:
                p = run(["taskset", "-c", mask, instrumented], stdin=inp,
                        stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=30)
            assert p.returncode in (0, 77), (name, count, p.returncode)
            fallback.append({"case": name, "source": src.decode(), "cpus": cpus[:count],
                             "unmodified_output": "correct", "detector_exit": p.returncode,
                             "serial_fallback": p.returncode == 77})
            print("PATH", name, count, "CPUs:", p.returncode, flush=True)
    observations["fallback"] = fallback

    programs = {}
    for line in (ROOT / "bench/programs.txt").read_text().splitlines():
        if line and not line.startswith("#"):
            name, _, _, prog = [s.strip() for s in line.split("|", 3)]
            programs[name] = ("\n".join(s.strip() for s in prog.split(";")) + "\n").encode()
    grammar = []
    for name, data in (("strip", b"x = '# not a comment'\n"), ("strings", b"'abc'\n"),
                       ("idents", b"123 abc\n")):
        c, st = work / f"c_{name}", work / f"st_{name}"
        run(["gcc", "-O3", "-march=native", f"-DK=P_{name}", "-o", c, ROOT / "bench/base.c"], check=True)
        elf = run([ROOT / "build/stage2"], input=programs[name], capture_output=True, check=True).stdout
        st.write_bytes(elf)
        st.chmod(0o755)
        got = run([c], input=data, capture_output=True, check=True).stdout
        assert run([st], input=data, capture_output=True, check=True).stdout == got
        tokens = [(tokenize.tok_name[t.type], t.string) for t in tokenize.tokenize(io.BytesIO(data).readline)
                  if t.type in (tokenize.NAME, tokenize.NUMBER, tokenize.STRING, tokenize.COMMENT)]
        grammar.append({"program": name, "input": data.decode(), "both_outputs_hex": got.hex(),
                        "both_outputs": got.decode() if name == "strip" else int.from_bytes(got, "little"),
                        "python_tokens": tokens})
    observations["toy_grammar"] = grammar
    (work / "checks.json").write_text(json.dumps(observations, indent=2) + "\n")
    print("Saved", work / "checks.json")


if __name__ == "__main__":
    main()
