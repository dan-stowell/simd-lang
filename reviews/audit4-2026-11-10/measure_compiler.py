#!/usr/bin/env python3
"""Checked, interleaved compiler/prefix-control measurements. Run alone after build.sh.

--prepare builds fixtures and controls but does not run a timing campaign.
The stopped-full controls change ONLY an audit copy of the generated ELF:
exit_group(0) immediately after a chosen full-program line. Consequently all
preceding allocation, lifetime and local/persistent-tee decisions remain intact.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import random
import statistics
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent
WORK = ROOT / "build/audit4"
sys.path.insert(0, str(ROOT / "boot"))
import strand


def run(args, **kw):
    return subprocess.run(list(map(str, args)), check=True, **kw)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def prepare():
    WORK.mkdir(exist_ok=True, parents=True)
    run(["gcc", "-O2", OUT / "timer.c", "-o", WORK / "timer"])
    for k in (8, 64):
        with (WORK / f"big{k}.strand").open("wb") as f:
            run(["python3", ROOT / "tools/mkbig.py", k], stdout=f)
    tables = (ROOT / "src/tables.strand").read_bytes()
    body = (ROOT / "src/compiler.strand").read_bytes()
    src = tables + body
    full = strand.compile_(src)
    assert full == (ROOT / "build/stage2").read_bytes()
    toks, lines, nslots, pool = strand.analyze(src)
    T = strand.tables()
    offset = 112 + len(T["stub"]) + strand.SLOT * (nslots + 1) + 4 * len(pool)
    ends = {}
    for ln, lt in sorted(lines.items()):
        offset += sum(T["qlen"][t.tid] + T["slen"][t.tid] for t in lt if t.code)
        offset += sum(T["rell"] for t in lt if t.dying)
        ends[ln] = offset
    # Last executable line of each source section before ELF output.
    bodylines = body.splitlines(keepends=True)
    selected, last = [], None
    for i, line in enumerate(bodylines, 1):
        if line.startswith(b"# ---") and last is not None:
            selected.append(last)
        if line.strip() and not line.startswith(b"#"):
            last = i
    selected.append(last)
    selected = sorted(set(selected + [19, 42]))
    prefix = len(tables.splitlines())
    manifest = []
    for k in selected:
        if k >= next(i for i, s in enumerate(bodylines, 1) if s.startswith(b"# ---") and b"ELF" in s):
            continue
        record = {"line": k, "source": bodylines[k - 1].decode().strip()}
        try:
            cut = strand.compile_(tables + b"".join(bodylines[:k]))
        except strand.CompileError as e:
            record["compile_error"] = e.code
            manifest.append(record)
            continue
        off = ends[prefix + k - 1]
        stop = bytearray(full)
        # xor edi,edi; mov eax,231; syscall -- no output has been emitted yet.
        stop[off:off + 9] = bytes.fromhex("31ffb8e70000000f05")
        for kind, data in (("prefix", cut), ("stopped_full", stop)):
            exe = WORK / f"{kind}-{k}"
            exe.write_bytes(data)
            exe.chmod(0o755)
            record[kind] = {"path": str(exe.relative_to(ROOT)), "sha256": sha(data),
                            "bytes": len(data)}
        record["stop_file_offset"] = off
        manifest.append(record)
    (OUT / "prefix-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    return manifest


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--prepare", action="store_true")
    ap.add_argument("--reps", type=int, default=15)
    ap.add_argument("--prefix-reps", type=int, default=9)
    args = ap.parse_args()
    manifest = prepare()
    if args.prepare:
        return
    cpus = sorted(os.sched_getaffinity(0))
    masks = [[cpus[min(3, len(cpus) - 1)]], cpus]
    rows = []
    for src in (ROOT / "build/strandc.strand", WORK / "big8.strand", WORK / "big64.strand"):
        want = None
        for mask in masks:
            for compiler in ("strandc_scalar", "stage2"):
                exe = ROOT / "build" / compiler
                with src.open("rb") as f:
                    p = run(["taskset", "-c", ",".join(map(str, mask)), exe],
                            stdin=f, capture_output=True)
                digest = sha(p.stdout)
                if want is not None:
                    assert digest == want
                want = digest
                rows.append({"source": str(src.relative_to(ROOT)), "source_bytes": src.stat().st_size,
                             "source_sha256": sha(src.read_bytes()), "compiler": compiler,
                             "exe_sha256": sha(exe.read_bytes()), "cpus": mask,
                             "output_bytes": len(p.stdout), "output_sha256": digest,
                             "samples": [], "_exe": exe, "_src": src})
    rng = random.Random(20261110)
    for _ in range(args.reps):
        order = rows.copy()
        rng.shuffle(order)
        for row in order:
            p = run(["taskset", "-c", ",".join(map(str, row["cpus"])),
                     WORK / "timer", row["_src"], row["_exe"]], capture_output=True, text=True)
            row["samples"].append(json.loads(p.stdout))
    prefix_rows = []
    for record in manifest:
        if "compile_error" in record:
            continue
        for kind in ("prefix", "stopped_full"):
            prefix_rows.append({"line": record["line"], "kind": kind, "samples": [],
                                "_exe": ROOT / record[kind]["path"]})
    for _ in range(args.prefix_reps):
        order = prefix_rows.copy()
        rng.shuffle(order)
        for row in order:
            p = run(["taskset", "-c", str(masks[0][0]), WORK / "timer",
                     WORK / "big8.strand", row["_exe"]], capture_output=True, text=True)
            row["samples"].append(json.loads(p.stdout))
    for row in rows + prefix_rows:
        for key in list(row):
            if key.startswith("_"):
                del row[key]
        row["medians"] = {key: statistics.median(s[key] for s in row["samples"])
                          for key in row["samples"][0]}
    report = {"revision": run(["git", "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip(),
              "gcc": run(["gcc", "--version"], capture_output=True, text=True).stdout.splitlines()[0],
              "cpus": cpus, "seed": 20261110, "compiler": rows, "prefix_controls": prefix_rows}
    (OUT / "compiler.json").write_text(json.dumps(report, indent=2) + "\n")
    for row in rows:
        print(row["source_bytes"], row["compiler"], row["cpus"], row["medians"], flush=True)
    for row in prefix_rows:
        print("prefix", row["line"], row["kind"], row["medians"], flush=True)


if __name__ == "__main__":
    main()
