#!/usr/bin/env python3
"""Small differential boundary/lifetime checks; no production changes."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

R = Path(__file__).resolve().parents[2]
O = Path(__file__).resolve().parent
B = R / "build/audit4/memory"
B.mkdir(parents=True, exist_ok=True)
sys.path.insert(0, str(R / "boot"))
import strand

cases = []
def add(name, src, inp=b""):
    cases.append((name, src.encode(), inp))

add("slice_read_hazard", "bytes >s\n32 ,d\n~32 dup 1 idx (s )d 0 * 31 + [d out\n", b"A"*32)
add("positions_and_gathers", "bytes :t dup 64 >= %p end\np out4\np [t out\np {t out4\n", bytes(range(256))*3)
for n in (0,1,2,3,4,15,16,17,63,64,65,127,128,129,4093,4094,4095,4096,4097,8192):
    inp = bytes((i*17+1)&255 for i in range(n))
    for lead, label in (("bytes >x", "owned"), ("<x", "input")):
        add(f"tail_{label}_{n}", f"{lead}\n~{n} {{x out4\n~{n} [x out\n'x out\n", inp)
    add(f"slice_full_{n}", f"<s\n{n} ,d\n~1 0 {n} 0 (s )d end\n'd out\n", inp)
for name, operands in (("negative_source","0 1 - 1 0"), ("negative_size","0 0 1 - 0"),
                       ("negative_dest","0 1 0 1 -"), ("source_over","8 1 0"),
                       ("dest_over","0 1 8"), ("source_tail","7 2 0"),
                       ("dest_tail","0 2 7"), ("zero_at_end","8 0 8")):
    add("slice_"+name, f"bytes >s\n8 ,d\n~1 {operands} (s )d end\n'd out\n", b"abcdefgh")
add("missing_slice_source", "8 ,d\n~1 0 1 0 )d end\n'd out\n")
add("source_cell_next_line", "bytes >s\n8 ,d\n~1 0 1 0 (s )d end\n~1 0 1 1 )d end\n'd out\n", b"abcdefgh")
add("source_last_prologue_wins", "bytes >a\n~8 66 + .b\n8 ,d\n~1 0 1 0 (a )d 0 1 1 (b )d end\n'd out\n", b"abcdefgh")
add("self_slice", "bytes >s\n~1 0 1 1 (s )s end\n's out\n", b"abcdefgh")
add("readonly_slice", "<s\nbytes >d\n~1 0 1 0 (d )s end\n", b"abcdefgh")
add("readonly_store", "<s\n123 0 }s\n", b"abcdefgh")
add("readonly_alias_release", "<a\n<b\n'a out\n'b out\n8 ,z\n'z out\n", b"abcdefgh")
add("input_redefinition_release", "<a\n'a out\nbytes >a\n'a out\n8 ,z\n'z out\n", b"abcdefgh")
add("recycled_zero_vector", "~257 65 + .old\n'old out\n257 ,z\n'z out\n")
add("zero_length_vectors", "0 ,a\n0 ,b\n'a out\n'b out\n")
add("store_negative_mask", "8 ,d\n123 0 1 - }d\n'd out\n")
add("store_last_legal", "8 ,d\n16909060 4 }d\n'd out\n")
add("store_tail_invalid", "8 ,d\n16909060 5 }d\n'd out\n")
add("gather_negative", "bytes >x\n0 1 - [x out\n", b"abcdefgh")
add("gather_end", "bytes >x\n8 {x out4\n", b"abcdefgh")
add("byte_persistent_tee", "bytes :b 1 + out\n'b out\n", bytes(range(256))*3)

cpu = min(os.sched_getaffinity(0))
rows = []
for name, src, inp in cases:
    row = {"name": name, "source": src.decode(), "input_hex": inp.hex(), "native": []}
    try:
        exe = strand.compile_(src)
        cs = 0
    except strand.CompileError as e:
        cs, exe = e.code, b""
    row["compile_status"] = cs
    assert cs == 0, (name, cs)
    for compiler in ("strandc_scalar", "stage1", "stage2", "stage3"):
        p = subprocess.run([R/"build"/compiler], input=src, capture_output=True)
        assert (p.returncode, p.stdout) == (cs, exe), (name, compiler, p.returncode, cs)
    if cs:
        rows.append(row)
        continue
    expected = strand.interp(src, inp)
    row["interpreter"] = {"status": expected[0], "output_hex": expected[1].hex()}
    f = B / (name+".exe")
    f.write_bytes(exe)
    f.chmod(0o755)
    (B/(name+".strand")).write_bytes(src)
    data = B/(name+".input")
    data.write_bytes(inp)
    for mode in ("pipe", "regular_file"):
        cmd = ["taskset", "-c", str(cpu), f]
        if mode == "pipe":
            p = subprocess.run(cmd, input=inp, capture_output=True, timeout=10)
        else:
            with data.open("rb") as stdin:
                p = subprocess.run(cmd, stdin=stdin, capture_output=True, timeout=10)
        row["native"].append({"mode": mode, "status": p.returncode, "output_hex": p.stdout.hex(),
                              "stderr": p.stderr.decode(errors="replace"),
                              "matches": p.returncode == expected[0] and
                              (expected[0] != 0 or p.stdout == expected[1])})
    rows.append(row)
report = {"cases": len(rows), "cpu": cpu, "source_revision": "b591f69",
          "stage2_sha256": hashlib.sha256((R/"build/stage2").read_bytes()).hexdigest(), "rows": rows}
(O/"memory-checks.json").write_text(json.dumps(report, indent=2)+"\n")
print("cases", len(rows), "compiled", sum(r["compile_status"] == 0 for r in rows))
for row in rows:
    if row["compile_status"] or any(not s["matches"] for s in row["native"]):
        print(row["name"], row["compile_status"],
              [(s["mode"], s["status"], s["matches"]) for s in row["native"]])
