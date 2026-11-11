#!/usr/bin/env python3
"""Independent, deterministic frontend audit (no imports from existing fuzz generators).

Run from the repository root after ./build.sh:
  python3 reviews/audit4-frontend/probe.py > reviews/audit4-frontend/probe.log
Only writes under build/audit4-frontend and reviews/audit4-frontend.
"""
import argparse
import collections
import hashlib
import itertools
import json
import os
from pathlib import Path
import random
import subprocess
import sys
import time

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "boot"))
import strand
import spec

OUT = ROOT / "reviews/audit4-frontend"
BUILD = ROOT / "build/audit4-frontend"
BUILD.mkdir(parents=True, exist_ok=True)
ENV = dict(os.environ, LC_ALL="C")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def run(cmd, src=b"", file=False):
    if file:
        path = BUILD / "stdin"
        path.write_bytes(src)
        with path.open("rb") as stream:
            p = subprocess.run(cmd, stdin=stream, capture_output=True, timeout=30, env=ENV)
    else:
        p = subprocess.run(cmd, input=src, capture_output=True, timeout=30, env=ENV)
    return p.returncode, p.stdout, p.stderr


def reference(src):
    try:
        return 0, strand.compile_(src)
    except strand.CompileError as e:
        return e.code, b""


def collision_names():
    # Permuting x_j = w_j+j and setting w'_j = x_perm(j)-j preserves BOTH hashes.
    words = spec.name_words(b"ddddhhhhllllpppp")
    names = []
    for perm in itertools.permutations(range(4)):
        name = b"".join(((words[p] + p - j) & spec.M32).to_bytes(4, "little")
                        for j, p in enumerate(perm))
        assert all(97 <= c <= 122 for c in name)
        names.append(name)
    assert len(set(names)) == 24
    assert len({spec.name_hash(n) for n in names}) == 1
    return names


def race_names(mask=31):
    buckets = collections.defaultdict(list)
    for i in range(100000):
        name = ("n%07d" % i).encode()
        h = spec.name_hash(name)
        key = tuple(spec.race_key(r, h, mask) for r in range(4))
        buckets[key].append(name)
        if len(buckets[key]) == 5:
            return buckets[key], key, i + 1
    raise AssertionError("race construction failed")


def construction():
    names = collision_names()
    rn, keys, attempts = race_names()
    coll = b"1 >bbbbdddd\n2 >edddabbb\n"
    race = b"".join(b"0 >" + n + b"\n" for n in rn)
    collision_family = b"".join(b"0 >" + n + b"\n" for n in names)
    result = {
        "collision_pair": [n.decode() for n in (b"bbbbdddd", b"edddabbb")],
        "collision_hash": spec.name_hash(b"bbbbdddd"),
        "collision_family": [n.decode() for n in names],
        "family_hash": spec.name_hash(names[0]),
        "race_names": [n.decode() for n in rn],
        "race_hashes": [spec.name_hash(n) for n in rn],
        "race_mask": 31, "race_keys": keys, "race_search_candidates": attempts,
    }
    for label, source in (("collision", coll), ("collision-family", collision_family), ("race", race)):
        (BUILD / (label + ".strand")).write_bytes(source)
    (OUT / "constructions.json").write_text(json.dumps(result, indent=2) + "\n")
    return coll, collision_family, race


def generated(coll, family, race):
    yield "constructed", "collision", coll, 5
    yield "constructed", "collision-family", family, 5
    yield "constructed", "race", race, 4
    yield "constructed", "collision-first-alone", b"1 >bbbbdddd\n", 0
    yield "constructed", "collision-second-alone", b"2 >edddabbb\n", 0
    for n in range(4):
        yield "constructed", "race-delete-%d" % n, b"\n".join(race.split(b"\n")[:n] + race.split(b"\n")[n+1:]), 0
    yield "constructed", "race-one-neutral-line", race + b"0 end\n", 4
    # Four harmless lines push nt from 10 to 18, changing the race table mask 31 -> 63.
    yield "constructed", "race-four-neutral-lines", race + b"0 end\n" * 4, None
    # Pairwise all error priorities. Reconstruct the race at the combined token count if needed.
    errors = {
        1: b'"unterminated\n', 2: b"1x end\n", 3: b"0 >abcdefghijklmnopq\n",
        5: coll, 6: b"0 >scan\n", 7: b'"s" end\n', 8: b"input\n",
        9: b"input drop\n", 10: b"input scan scan end\n",
        11: b"input >a\na a !a\n", 12: b"neverdefined end\n",
        13: b"input :b :b end\n", 14: b"bytes >c\nc end\n",
    }
    for e, source in errors.items():
        yield "priority", "single-%02d" % e, source, e
    for (e, a), (f, b) in itertools.combinations(errors.items(), 2):
        for order, src in enumerate((a+b, b+a)):
            yield "priority", "%02d-%02d-order%d" % (e, f, order), src, min(e, f)
    # A larger mask makes room for appended error witnesses while preserving an E_RACE witness.
    names, _, _ = race_names(127)
    larger_race = b"".join(b"0 >" + n + b"\n" for n in names) + b"0 end\n" * 12
    assert reference(larger_race)[0] == 4
    for e, src in errors.items():
        for order, text in enumerate((larger_race + src, src + larger_race)):
            yield "priority", "04-%02d-order%d" % (e, order), text, min(4, e)
    # Exhaust short strings over lexical equivalence classes, including comments, NUL, and high-bit.
    alphabet = [b" ", b"a", b"0", b'"', b"#", b"\n", b"\x00", b"\xff"]
    for n in range(5):
        for i, seq in enumerate(itertools.product(alphabet, repeat=n)):
            yield "lexer-exhaustive", "%d-%d" % (n, i), b"".join(seq), None
    # Every byte at every literal position, preserving a decimal first character when possible.
    for pos in range(9):
        for c in range(256):
            lit = bytearray(b"123456789")
            lit[pos] = c
            yield "decimal-byte", "%d-%03d" % (pos, c), bytes(lit) + b" end\n", None
    for n in range(1, 11):
        for digit in b"019":
            yield "decimal-length", "%d-%d" % (n, digit), bytes([digit]) * n + b" out4", 0 if n <= 9 else 2
    # Adjacent invalid nibbles/bytes can trigger carries in SWAR digit checks.
    bad = [0x2f, 0x3a, 0x3f, 0x40, 0x7f, 0x80, 0xff]
    for pos in range(1, 8):
        for a, b in itertools.product(bad, repeat=2):
            s = bytearray(b"123456789")
            s[pos:pos+2] = bytes([a, b])
            yield "decimal-pair", "%d-%02x-%02x" % (pos, a, b), bytes(s) + b" out4\n", 2
    for sigil in [""] + list(spec.SIGILS):
        for kw in spec.KEYWORDS:
            yield "keywords", "sigil-%s-%s" % (sigil, kw), (sigil + kw + " end\n").encode(), None
    # Exercise all 256 keyword hash indices with non-keyword lowercase names.
    buckets = {}
    for i in range(20000):
        name = ("kw%05d" % i).encode()
        index = (spec.name_hash(name)[0] >> spec.KWSHIFT) & 255
        buckets.setdefault(index, name)
        if len(buckets) == 256:
            break
    assert len(buckets) == 256
    for index, name in sorted(buckets.items()):
        yield "keyword-impostors", str(index), b"65 >" + name + b"\n" + name + b" out\n", 0
    for kw in spec.KEYWORDS:
        for suffix in (b"x", b"0", b"\xff"):
            name = kw.encode() + suffix
            yield "keyword-nearmiss", kw + "-" + suffix.hex(), name + b" end\n", None
    # Names have an initial class, not an identifier-alphabet restriction after that first byte.
    for c in range(256):
        name = b"abc" + bytes([c]) + b"def"
        yield "name-byte", str(c), b"65 >" + name + b"\n" + name + b" out\n", None
    for n in range(1, 21):
        for start in (b"a", b"A", b"_"):
            name = start + b"x" * (n-1)
            yield "name-length", str(n) + start.decode(), b"1 >" + name + b"\n", 0 if n <= 16 else 3
    for c in range(256):
        yield "string-byte", str(c), b'"' + bytes([c]) + b'" =d\nd out4\n', None
    for n in (0, 1, 3, 4, 7, 8, 15, 16, 17, 31, 32, 63, 64, 65, 127, 128, 129, 255, 256, 4095, 4096, 4097):
        for tail in (b"", b"\n", b"#comment", b' #"ignored"'):
            yield "string-length", "%d-%s" % (n, tail.hex()), b'"' + b"x" * n + b'" =d\nd out' + tail, 0
    core = [
        b'"" "a" 123 "b\x00c" 0009 =d\nd out4',
        b'# "\n65 >abcdefghijklmnop\nabcdefghijklmnop out',
        b'bytes :s dup 32 > %p end\np [s out',
        b'"abc"#ignored"\ndrop',
        b'1\n"a""b" =d',
        b'0 end\n#comment without newline"',
    ]
    for i, source in enumerate(core):
        for pad in range(130):
            yield "alignment", "%d-%d" % (i, pad), b" " * pad + source, None
    rng = random.Random(4102026)
    vals = []
    for length in range(1, 10):
        nums = [0, 1, 10**length-1, 10**(length-1), max(0, 10**(length-1)-1)]
        nums += [rng.randrange(10**length) for _ in range(1000)]
        vals.extend(("%0*d" % (length, value)).encode() for value in nums)
    for i in range(0, len(vals), 500):
        yield "decimal-valid-batch", str(i), b" ".join(vals[i:i+500]) + b" =d\nd out4\n", 0
    for n in (15, 16, 17, 63, 64, 65, 255, 256, 257, 4095, 4096, 4097):
        yield "token-count", str(n), b"0 " * n + b"=d\nd out4\n", 0


def swar_proof():
    # Independent bounded exhaustive check of both 4-digit reduction and reciprocal scaling.
    count = 0
    recip = {1: (8389, 23), 2: (5243, 19), 3: (52429, 19)}
    for n in range(10000):
        text = ("%04d" % n).encode()
        x = int.from_bytes(text, "little") ^ 0x30303030
        group = (((((x * 2561) & spec.M32) >> 8) & 0x00ff00ff) * 6553601 & spec.M32) >> 16
        assert group == n
        for digits, (mult, shift) in recip.items():
            if n % (10**(4-digits)) == 0:
                assert (n * mult & spec.M32) >> shift == n // (10**(4-digits))
        count += 1
    return {"four_digit_groups_exhausted": count, "reciprocal_scalings": "all 1110 reachable 1/2/3-digit prefixes"}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--compiler", default="build/stage2")
    ap.add_argument("--only", default="")
    args = ap.parse_args()
    compilers = [str(ROOT / "build/strandc_scalar"), str(ROOT / args.compiler)]
    start = time.time()
    coll, family, race = construction()
    result = {
        "head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT).decode().strip(),
        "compiler_sha256": {str(Path(c).relative_to(ROOT)): sha(Path(c).read_bytes()) for c in compilers},
        "stencils_sha256": sha((ROOT / "build/stencils.json").read_bytes()),
        "swar_arithmetic": swar_proof(),
        "counts": {}, "statuses": {}, "mismatches": [], "expected_mismatches": [], "runtime_checks": 0,
    }
    counts, statuses = collections.Counter(), collections.Counter()
    logpath = OUT / ("cases" + ("-" + args.only if args.only else "") + ".jsonl")
    with logpath.open("w") as log:
        for i, (group, label, src, expected) in enumerate(generated(coll, family, race)):
            if args.only and group != args.only:
                continue
            want = reference(src)
            if expected is not None and want[0] != expected:
                result["expected_mismatches"].append({"group": group, "label": label, "expected": expected, "got": want[0], "source_hex": src.hex()})
            row = {"group": group, "label": label, "source_sha256": sha(src), "size": len(src),
                   "reference_status": want[0], "output_size": len(want[1]), "output_sha256": sha(want[1])}
            runs = []
            for compiler in compilers:
                got = run([compiler], src)
                runs.append({"status": got[0], "size": len(got[1]), "sha256": sha(got[1]), "stderr": got[2].decode("latin1")[:250]})
                if got[:2] != want:
                    name = "mismatch-%d-%s.strand" % (i, Path(compiler).name)
                    (BUILD / name).write_bytes(src)
                    result["mismatches"].append({"group": group, "label": label, "file": name, "want": want[0], "got": runs[-1]})
            # Exercise mmap vs pipe at all alignment and long-token/string boundaries.
            if group in ("alignment", "string-length", "token-count", "constructed"):
                got = run([compilers[-1]], src, file=True)
                if got[:2] != want:
                    name = "mismatch-%d-file.strand" % i
                    (BUILD / name).write_bytes(src)
                    result["mismatches"].append({"group": group, "label": label, "file": name, "want": want[0], "got": got[0]})
                row["file_status"] = got[0]
            # All valid decimal data batches must actually emit their independently parsed pool.
            if group == "decimal-valid-batch":
                exe = BUILD / "decimal-exe"
                exe.write_bytes(want[1]); exe.chmod(0o755)
                decimal_tokens = src.split(b" =d", 1)[0].split()
                expected_bytes = b"".join(int(v).to_bytes(4, "little") for v in decimal_tokens)
                p = run([str(exe)])
                assert p[:2] == (0, expected_bytes)
                result["runtime_checks"] += 1
            row["native"] = runs
            log.write(json.dumps(row, sort_keys=True) + "\n")
            counts[group] += 1; statuses[want[0]] += 1
            if sum(counts.values()) % 1000 == 0:
                print("checked", sum(counts.values()), "mismatches", len(result["mismatches"]), flush=True)
    result["counts"] = dict(sorted(counts.items()))
    result["statuses"] = dict(sorted(statuses.items()))
    result["elapsed_seconds"] = round(time.time() - start, 3)
    filename = "results" + ("-" + args.only if args.only else "") + ".json"
    (OUT / filename).write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps(result, indent=2, sort_keys=True), flush=True)
    return bool(result["mismatches"] or result["expected_mismatches"])


if __name__ == "__main__":
    raise SystemExit(main())
