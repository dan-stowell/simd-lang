#!/usr/bin/env python3
"""Non-timing checks for af3ecc9. Run from the repository root.

Only writes evidence under reviews/audit3-claims and generated files under
build/audit3-claims. Does not run hw/probe (it contains timing campaigns).
"""
import ast
import hashlib
import json
import os
from pathlib import Path
import re
import struct
import subprocess

R = Path(__file__).resolve().parents[2]
E = R / "reviews/audit3-claims"
B = R / "build/audit3-claims"
B.mkdir(parents=True, exist_ok=True)

def command(args, **kwargs):
    return subprocess.run([str(x) for x in args], capture_output=True, **kwargs)

def checked(args):
    p = command(args)
    assert p.returncode == 0, (args, p.stderr)
    return p.stdout

def dis(path, raw=False):
    args = ["objdump", "-d"]
    if not raw:
        args.append("--no-show-raw-insn")
    return checked(args + [path]).decode()

def functions(text, names):
    bodies = dict(re.findall(r"^[0-9a-f]+ <([^>]+)>:\n(.*?)(?=\n\n|\Z)", text, re.M | re.S))
    return "\n\n".join(f"<{name}>:\n{bodies[name]}" for name in names) + "\n"

checked(["gcc", "-O2", "-o", B / "probe", R / "hw/probe.c"])
checked(["gcc", "-O2", "-pthread", "-o", B / "iotest", R / "explore/iotest.c"])
result = {
    "revision": checked(["git", "rev-parse", "HEAD"]).decode().strip(),
    "gcc": checked(["gcc", "--version"]).decode().splitlines()[0],
    "allowed_cpus": sorted(os.sched_getaffinity(0)),
    "checks": {},
}
tree = ast.parse((R / "hw/check_probe.py").read_text())
want = next(ast.literal_eval(n.value) for n in tree.body
            if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "WANT" for t in n.targets))
symbols = {}
for line in checked(["nm", "-S", "--defined-only", B / "probe"]).decode().splitlines():
    fields = line.split()
    if len(fields) == 4 and fields[-1].startswith("p_"):
        symbols[fields[-1]] = (int(fields[0], 16), int(fields[1], 16))
assert set(symbols) == {"p_" + k for k in want}
elf = bytearray((B / "probe").read_bytes())
phoff = struct.unpack_from("<Q", elf, 32)[0]
phentsz, phnum = struct.unpack_from("<HH", elf, 54)
loads = []
for i in range(phnum):
    typ, flags, offset, vaddr, paddr, filesz, memsz, align = struct.unpack_from("<IIQQQQQQ", elf, phoff + i * phentsz)
    if typ == 1:
        loads.append((offset, vaddr, filesz))

def fileoffset(addr):
    return next(off + addr - va for off, va, size in loads if va <= addr < va + size)

# All named instructions remain in their original symbol bodies, but every probe
# returns immediately. This is a check of reachability, not a current-probe bug.
returns = bytearray(elf)
for addr, size in symbols.values():
    off = fileoffset(addr)
    assert returns[off:off + 4] == bytes.fromhex("f30f1efa")
    returns[off:off + 4] = bytes.fromhex("c3909090")
(B / "probe-return-before-instruction").write_bytes(returns)

# AVX-VNNI (VEX) and AVX-512VNNI have the same mnemonic. Replace only the
# VEX probe with a reachable EVEX zmm vpdpbusd and ret: wrong extension.
wrong = bytearray(elf)
addr, size = symbols["p_avxvnni"]
off = fileoffset(addr)
body = bytes.fromhex("f30f1efa 62f27d4850c0 c3")
wrong[off:off + size] = body + b"\x90" * (size - len(body))
(B / "probe-wrong-vnni-encoding").write_bytes(wrong)

for label, path in (
    ("original", B / "probe"),
    ("missing", Path("/bin/true")),
    ("unreachable", B / "probe-return-before-instruction"),
    ("wrong_vnni_encoding", B / "probe-wrong-vnni-encoding"),
):
    p = command(["python3", R / "hw/check_probe.py", path], text=True)
    result["checks"][label] = {"exit": p.returncode, "stdout": p.stdout, "stderr": p.stderr}
    (E / f"checker-{label}.txt").write_text(p.stdout + p.stderr)
    if label != "missing":
        names = (["p_sse2", "p_avx", "p_avx2", "p_adx", "p_512vl", "p_apx", "p_avxvnni"]
                 if label == "original" else
                 ["p_sse2", "p_512vl", "p_apx", "p_avxvnni"] if label == "unreachable"
                 else ["p_avxvnni"])
        (E / f"probe-{label}.dis").write_text(functions(dis(path, raw=True), names))
assert result["checks"]["original"]["exit"] == 0
assert result["checks"]["missing"]["exit"] != 0
assert result["checks"]["unreachable"]["exit"] == 0
assert result["checks"]["wrong_vnni_encoding"]["exit"] == 0
io_dis = dis(B / "iotest")
(E / "iotest.dis").write_text(functions(io_dis, ["touch", "par", "rd"]))
touch = re.search(r"<touch>:\n(.*?)(?=\n\n)", io_dis, re.S).group(1)
assert "movzbl (" in touch and "<base>" in touch and "<sums>" in touch
result["iotest_touch_has_input_load_and_sum_store"] = True

# Tiny semantic checks, not benchmarks. The phase-map "complete scanner" is
# stronger than the task implemented by the JSON structural-character filter.
programs = {}
for line in (R / "bench/programs.txt").read_text().splitlines():
    if line and not line.startswith("#"):
        name, kind, shape, source = [s.strip() for s in line.split("|", 3)]
        programs[name] = "\n".join(s.strip() for s in source.split(";")) + "\n"
for name in ("json", "csv", "idents"):
    exe = B / ("strand-" + name)
    p = command([R / "build/stage2"], input=programs[name].encode())
    assert p.returncode == 0
    exe.write_bytes(p.stdout)
    exe.chmod(0o755)
samples = [
    ("json", b'{"x":1}\n'),
    ("json", b'{"different":false}\n'),
    ("json", b'{"x":}\n'),
    ("json", b'["unterminated]\n'),
    ("json", b"[1,2]\n[3,4]\n"),
    ("csv", b'"a,b",c\r\n'),
    ("idents", b"abc 123\n"),
]
result["semantic_examples"] = []
for name, data in samples:
    p = command(["taskset", "-c", str(result["allowed_cpus"][0]), B / ("strand-" + name)], input=data)
    row = {"program": name, "input": data.decode(), "exit": p.returncode, "output_hex": p.stdout.hex()}
    if name == "json":
        try:
            json.loads(data)
            row["python_json_accepts"] = True
        except ValueError:
            row["python_json_accepts"] = False
    result["semantic_examples"].append(row)
result["position_controls"] = []
for label, source in (
    ("byte_store", "bytes 32 > >mask\nmask idx swap outif4\n"),
    ("persistent_byte_tee", "bytes 32 > :mask end\nmask idx swap outif4\n"),
    ("dword_positions", "input 32 > dup prev 1 ^ & idx swap outif4\n"),
    ("byte_idx", "bytes idx out\n"),
):
    data = b"one two\n"
    p = command([R / "build/stage2"], input=source.encode())
    row = {"label": label, "source": source, "compile_exit": p.returncode}
    if p.returncode == 0:
        exe = B / label
        exe.write_bytes(p.stdout)
        exe.chmod(0o755)
        run = command(["taskset", "-c", str(result["allowed_cpus"][0]), exe], input=data)
        row.update(input=data.decode(), exit=run.returncode, output_hex=run.stdout.hex())
    result["position_controls"].append(row)
assert [r["compile_exit"] == 0 for r in result["position_controls"]] == [False, False, True, False]
assert result["position_controls"][2]["output_hex"] == "0000000004000000"
result["binaries"] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                      for p in B.iterdir() if p.is_file()}
(E / "observations.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result, indent=2))
