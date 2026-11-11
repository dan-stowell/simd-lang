#!/usr/bin/env python3
"""Focused reproductions and large-boundary correctness checks; run from repo root."""
import importlib.util
import itertools
import json
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(Path(__file__).resolve().parent))
import probe
from probe import BUILD, OUT, spec, strand, reference, run, sha


def old_hash_module():
    old = subprocess.check_output(["git", "show", "af3ecc9:boot/spec.py"], cwd=ROOT)
    path = BUILD / "old_spec.py"
    path.write_bytes(old)
    modspec = importlib.util.spec_from_file_location("audit4_old_spec", path)
    module = importlib.util.module_from_spec(modspec)
    modspec.loader.exec_module(module)
    return module


def map_regression():
    src = b"~1 @missing out\n=missing\n"
    path = BUILD / "map-repro.strand"
    path.write_bytes(src)
    binary, mapping = strand.compile_(src, True)
    exe = BUILD / "map-repro"
    exe.write_bytes(binary); exe.chmod(0o755)
    rc, out, err = run([str(exe)])
    assert rc == 99
    address = err.decode().strip().split()[-1]
    cli = subprocess.run([sys.executable, str(ROOT / "boot/strand.py"), "map", str(path)], capture_output=True)
    where = subprocess.run([sys.executable, str(ROOT / "tools/where.py"), str(path), address], capture_output=True)
    return {"source": src.decode(), "map_tuples": mapping,
            "runtime": {"status": rc, "stdout": out.decode(), "stderr": err.decode()},
            "map_cli": cli.stdout.decode(), "where_cli": where.stdout.decode(), "where_status": where.returncode}


def historical_deltas():
    old = old_hash_module()
    return {"old_maxname": old.MAXNAME, "new_maxname": spec.MAXNAME,
            "old_prelude_token_count": old.K,
            "old_collision_pair_hashes": [old.name_hash(n) for n in (b"bbbbdddd", b"edddabbb")],
            "new_collision_pair_hashes": [spec.name_hash(n) for n in (b"bbbbdddd", b"edddabbb")]}


def keyword_permutations():
    attempts = 0
    different, lexical = [], []
    for kw in spec.KEYWORDS:
        words = spec.name_words(kw.encode())
        for perm in itertools.permutations(range(4)):
            raw = b"".join(((words[p] + p - j) & spec.M32).to_bytes(4, "little")
                           for j, p in enumerate(perm))
            name = raw.rstrip(b"\0")
            assert spec.name_hash(name) == spec.name_hash(kw.encode())
            attempts += 1
            if name != kw.encode():
                different.append(name)
                # A token name cannot contain a blank, quote, or comment introducer.
                if all(c > 32 and c not in b'"#' for c in name):
                    lexical.append({"keyword": kw, "permutation": perm, "name_hex": name.hex()})
    return {"attempts": attempts, "different_raw_names": len(different),
            "lexically_valid_different_names": lexical,
            "note": "No full keyword-fingerprint collision found. This is only an exhaustive permutation-family check, not a general collision proof."}


def boundary_cases():
    for n in (63, 64, 65, 4095, 4096, 4097, 65535, 65536, 65537, 1048575, 1048576, 1048577):
        for tail in (b"\n65 out", b'"\n65 out', b'#"\n65 out', b'\x00"\n65 out'):
            yield "comment-%d-%s" % (n, tail.hex()), b"#" + b"x" * n + tail
        yield "string-%d" % n, b'"' + b"x" * n + b'" =d\nd out'
        yield "string-open-eof-%d" % n, b'"' + b"x" * n
        yield "string-comment-%d" % n, b'"' + b"x" * n + b'#" =d'
        yield "long-literal-%d" % n, b"1" * n + b" end"
        yield "long-name-%d" % n, b"1 >a" + b"x" * n
        yield "nul-padding-%d" % n, b"\x00" * n + b"65 out"
    # State changes straddle all byte-lane boundaries; EOF occurs in code/comment/string.
    for offset in range(64):
        for tail in (b'"', b'#', b"\n", b"\x00", b"", b'"#', b'""'):
            source = b" " * offset + (b'#ignored"\n"abc" =d\n' * 100) + tail
            yield "alternating-%d-%s" % (offset, tail.hex()), source


def main():
    result = {"head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT).decode().strip(),
              "map_regression": map_regression(), "historical_deltas": historical_deltas(),
              "keyword_permutations": keyword_permutations(), "boundary_checks": 0,
              "boundary_mismatches": [], "stage_reproductions": []}
    # Python compiling 1 MiB string pools is deliberately bounded to this small, fixed matrix.
    for label, src in boundary_cases():
        want = reference(src)
        for compiler in ("build/strandc_scalar", "build/stage2"):
            for file_mode in (False, True):
                got = run([str(ROOT / compiler)], src, file=file_mode)
                if got[:2] != want:
                    path = BUILD / (label + ".strand")
                    path.write_bytes(src)
                    result["boundary_mismatches"].append({"label": label, "compiler": compiler,
                        "file": file_mode, "expected": want[0], "actual": got[0],
                        "expected_sha256": sha(want[1]), "actual_sha256": sha(got[1])})
                result["boundary_checks"] += 1
    coll, family, race = probe.construction()
    for compiler in ("build/stage1", "build/stage2", "build/stage3", "build/strandc_scalar"):
        for label, src in (("collision", coll), ("collision-family", family), ("race", race)):
            got = run([str(ROOT / compiler)], src)
            want = reference(src)
            assert got[:2] == want
            result["stage_reproductions"].append({"compiler": compiler, "case": label,
                                                  "status": got[0], "stdout_len": len(got[1])})
    (OUT / "focused-results.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))
    return bool(result["boundary_mismatches"])


if __name__ == "__main__":
    raise SystemExit(main())
