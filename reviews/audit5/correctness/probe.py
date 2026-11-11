#!/usr/bin/env python3
"""Deterministic differential lexer campaign. Run from the repository root.

Generated large files/executables live in build/audit5-correctness.
Every failing candidate is reduced with scanner-error classification and actual
exit status preserved. We preserve a trailing newline when present, so an
ordinary wrong-token failure cannot reduce to the separately documented EOF gap.
Reduction is byte-deletion 1-minimal, not a claim of a globally minimal string.
The generated cases and reducers are deterministic (seed below); only unique
minimal failures are committed, with all origin IDs/counts in the summary.
"""
import argparse
import base64
import collections
import hashlib
import itertools
import json
import os
from pathlib import Path
import random
import struct
import subprocess
import sys
import time

ROOT = Path("reviews/audit5/correctness")
BUILD = Path("build/audit5-correctness")
SEED = 25887
OPS = "+ - * / % & | ^ << >> &^ += -= *= /= %= &= |= ^= <<= >>= &^= && || <- ++ -- == < > = ! != <= >= := ... ( [ { , . ) ] } ; : ~".split()
KWS = "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var".split()


def numeric_forms():
    ints = ["0", "1", "42", "1_234", "01", "0755", "0_755",
            "0b0", "0b101", "0B_101", "0o7", "0o755", "0O_7",
            "0x0", "0x1", "0xdeadBEEF", "0X_FF", "0x1e", "0x1E",
            "0x1f", "0x1F"]
    nums = ints[:]
    for mant in ["0.", "1.", ".0", ".1", "1.25", "1_2.3_4"]:
        nums += [mant, mant + "e0", mant + "E+2", mant + "e-3",
                 mant + "e1_2"]
    for mant in ["0", "1", "12", "1_2"]:
        nums += [mant + "e0", mant + "E+2", mant + "e-3", mant + "e1_2"]
    for prefix, mant, exp in itertools.product(
            ["0x", "0X"], ["1", "1.", ".1", "1.2", "1_a.b_c", "_F", "A.B"],
            ["p0", "P+2", "p-3", "p1_2"]):
        nums.append(prefix + mant + exp)
    nums += [x + "i" for x in nums] + ["08i", "09i", "08.0i"]
    return list(dict.fromkeys(nums))


def cases():
    rng = random.Random(SEED)
    nums = numeric_forms()
    for i, n in enumerate(nums):
        yield "numeric-isolated", f"num-{i}", (n + "\n").encode(), False
        yield "complete-program", f"num-program-{i}", ("package p\nconst X = " + n + "\n").encode(), True
    # Every operator pair, plus maximal-munch triple stress.
    for i, (a, b) in enumerate(itertools.product(OPS, repeat=2)):
        yield "operators", f"op-pair-{i}", (a + b + "\n").encode(), False
    for i in range(3500):
        s = "".join(rng.choices(OPS, k=rng.randrange(3, 8))) + "\n"
        yield "operators", f"op-run-{i}", s.encode(), False
    # Digits, letters, exponent indicators, suffixes, and radix points adjacent
    # to numeric literals: scan each independently (reject oracle errors).
    suffixes = ["a", "b", "c", "e", "e0", "f", "g", "i", "ii", "j", "p",
                "p0", "x", "X", "z", "_", "_x", "α", "变量", "return",
                ".0", ".1", ".2", "..", "...", "....", "0", "1i",
                "+1", "-2", "++", "--", "/*x*/", "//x\n"]
    for i, (a, b) in enumerate(itertools.product(nums, suffixes)):
        yield "numeric-adjacent", f"num-adj-{i}", (a + b + "\n").encode(), False
    for i in range(2500):
        s = rng.choice(nums) + rng.choice(nums) + "\n"
        yield "numeric-adjacent", f"num-num-{i}", s.encode(), False
    ids = ["x", "_", "_1", "e", "p", "i", "xe", "px", "ix",
           "α", "变量", "日本語", "𐐀", "é", "π_٢", "a９", "K", "ǅ", "ᚠ",
           "a\u0301", "\u0301a", "²", "Ⅻ", "😀", "a😀"]
    strings = ['""', '"a"', r'"\""', r'"\\"', r'"\a\b\f\n\r\t\v"',
               r'"\x00\u0000\U00000000"', r'"\xff\u00ff\U0001F600"',
               r'"\000\377"', '"/* */ // ` \'"', '"α变量"',
               "``", "`a`", "`a\nb`", "`// /* */ \" ' \\\\`",
               "`\\`", "`a\r\nb\rc`"]
    runes = ["'a'", "'α'", "'𐐀'", r"'\''", r"'\\'", r"'\x00'",
             r"'\u00ff'", r"'\U0001F600'", r"'\377'", "'\"'", "'`'"]
    comments = ["", " ", "\t", "\n", "\n\n", "/**/", "/*x*/",
                "/*\n*/", "/*a\nb*/", "/*\n\n*/", "//x\n",
                "// /* \" ' ` \\\n", "/*/ */", "/***/", "/*//*/",
                "/*\" '\\`//*/", "/*\r\n*/", "\r", "\r\n"]
    terms = ids + nums[:35] + strings + runes + KWS + OPS
    for i, (a, b) in enumerate(itertools.product(strings + runes, repeat=2)):
        yield "quoted-adjacent", f"quote-pair-{i}", (a + b + "\n").encode(), False
    for i, (a, sep, b) in enumerate(itertools.product(
            ["x", "return", "break", "continue", "fallthrough", ")", "]", "}",
             "++", "--", "1", "1.", "1i", '""', "``", "'a'", "+", "{", "if"],
            comments, ["", "x", "+", ")", "//end", "/*end*/"])):
        yield "semicolons-comments", f"semi-{i}", (a + sep + b + "\n").encode(), False
    for i in range(6000):
        s = "".join(rng.choice(terms) + rng.choice(comments) for _ in range(rng.randrange(1, 12))) + "\n"
        yield "mixed", f"mix-{i}", s.encode(), False
    for i, a in enumerate(ids):
        yield "unicode", f"unicode-{i}", (a + "\n").encode(), False
        yield "complete-program", f"unicode-program-{i}", ("package p\nvar " + a + " = 1\n").encode(), True
    # Programs retain their validity while CR, comments, empty lines, and EOF
    # conditions vary. No imported packages: go/types validation is independent.
    programs = [
        "package p\n",
        "package p\nfunc f() { return }\n",
        "package p\nfunc f(){ x:=1; x++; _=x }\n",
        "package p\ntype T interface { ~int | ~string }\n",
        "package p\nfunc f(){ for range 2 {} }\n",
        "package p\nvar x = [...]int{1,2,3}\n",
        "package p\nfunc f(v ...int){}; func g(){ f([]int{1,2}...) }\n",
        "package p\nvar a=1/*x*/+2\nvar b=1/*\nx*/\n",
        "package p\nvar a=`x\r\ny\rb`\nvar b=\"a\\r\\nb\"\n",
        "package p\nvar α = 1; var 变量 = α\n",
    ]
    for i, src in enumerate(programs):
        for j, s in enumerate([src, src.replace("\n", "\r\n"), src.rstrip("\n"),
                               src.rstrip("\n") + " ", src.rstrip("\n") + "/**/",
                               src.rstrip("\n") + "//end", "\ufeff" + src]):
            yield "complete-program", f"program-{i}-{j}", s.encode(), True
    for i, src in enumerate(["", "\n", " ", "\t", "\r", "\r\n", "x", "+",
                              "1", "1.", "1i", '"x"', "'x'", "`x`", "x ",
                              "x\t", "x\r", "x//end", "x/**/", "//end",
                              "/**/", "x/*\n*/", "\n\n", "\ufeff",
                              "\ufeff\n", "\ufeffx\n", "\ufeff x\n"]):
        yield "edge", f"edge-{i}", src.encode(), False
    for i, src in enumerate([b"\0", b"x\0\n", b"//\0\n", b'"a\0b"\n',
                              b"`\0`\n", b"/*\0*/\n", b"\xff\n",
                              b"\xef\xbb\xbf\xef\xbb\xbf\n", b"x\\ufeff\n"]):
        yield "excluded-input", f"excluded-{i}", src, False
    # Literal escapes whose backslash runs cross SIMD boundaries.
    for i, n in enumerate([1, 2, 3, 7, 15, 31, 32, 63, 64, 65, 127, 128, 255]):
        src = '"' + "\\\\" * n + '\\"x"\n'
        yield "escaped-runs", f"esc-{i}", src.encode(), False


class Harness:
    def __init__(self, cpus):
        self.cpus = cpus
        self.oracle = subprocess.Popen([str(BUILD / "oracle")], stdin=subprocess.PIPE,
                                       stdout=subprocess.PIPE, text=True)
        self.cache = {}
        self.oracle_cache = {}
        self.runs = 0

    def reference(self, src, check=False):
        key = (src, check)
        if key not in self.oracle_cache:
            self.oracle.stdin.write(json.dumps({"source": base64.b64encode(src).decode(), "check": check}) + "\n")
            self.oracle.stdin.flush()
            self.oracle_cache[key] = json.loads(self.oracle.stdout.readline())
        return self.oracle_cache[key]

    def actual(self, src):
        if src not in self.cache:
            inp = BUILD / "probe-input.go"
            inp.write_bytes(src)
            with inp.open("rb") as f:
                p = subprocess.run(["taskset", "-c", self.cpus, str(BUILD / "golex_strand")],
                                   stdin=f, capture_output=True, timeout=5)
            out = p.stdout
            if len(out) % 5:
                result = {"status": p.returncode, "malformed_hex": out.hex()}
            else:
                n = len(out) // 5
                offs = struct.unpack("<%dI" % n, out[:4*n])
                result = {"status": p.returncode, "tokens": list(zip(offs, out[4*n:])),
                          "stderr": p.stderr.decode(errors="backslashreplace")}
            self.cache[src] = result
            self.runs += 1
        return self.cache[src]

    def diff(self, src):
        r = self.reference(src)
        a = self.actual(src)
        want = [(t["offset"], t["kind"]) for t in r["tokens"]]
        return a["status"] != 0 or a.get("tokens") != want

    def minimize(self, src):
        """ddmin then single-byte deletion with exact error/status categories."""
        initial_r = self.reference(src)
        initial_a = self.actual(src)
        valid = not initial_r["errors"]
        status = initial_a["status"]
        newline = src.endswith(b"\n")
        # Preserve CR/BOM/NUL exclusion class independently, to avoid changing
        # the applicability category while reducing a failure.
        category = (b"\0" in src, b"\r" in src, src.startswith(b"\xef\xbb\xbf"))

        def fails(s):
            if newline and not s.endswith(b"\n"):
                return False
            if (b"\0" in s, b"\r" in s, s.startswith(b"\xef\xbb\xbf")) != category:
                return False
            if bool(self.reference(s)["errors"]) == valid:
                return False
            return self.actual(s)["status"] == status and self.diff(s)

        n = 2
        while len(src) >= 2:
            size = (len(src) + n - 1) // n
            for start in range(0, len(src), size):
                trial = src[:start] + src[start+size:]
                if fails(trial):
                    src = trial
                    n = max(2, n - 1)
                    break
            else:
                if n >= len(src):
                    break
                n = min(len(src), n * 2)
        # Standard ddmin reaches 1-minimality; make that property explicit.
        for i in range(len(src)):
            assert not fails(src[:i] + src[i+1:]), (src, i)
        return src


def encode_case(src, r, a):
    return {"input_utf8": src.decode(errors="backslashreplace"),
            "input_hex": src.hex(), "input_bytes": len(src),
            "expected_status": 0, "reference": r, "actual": a}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--cpus", default="0")
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--categories", default="")
    ap.add_argument("--output", default="campaign")
    args = ap.parse_args()
    h = Harness(args.cpus)
    summary = collections.defaultdict(collections.Counter)
    minima = {}
    failed_origins = []
    begin = time.monotonic()
    generated = seen = 0
    want_categories = args.categories.split(",") if args.categories else None
    for cat, name, src, check in cases():
        generated += 1
        if want_categories and cat not in want_categories:
            continue
        if args.limit and seen >= args.limit:
            break
        seen += 1
        r = h.reference(src, check)
        a = h.actual(src)
        s = summary[cat]
        s["cases"] += 1
        if r["errors"]:
            s["scanner_error_inputs"] += 1
            # These are outside the claim, retain their behavior for evidence,
            # but don't spend the reduction budget on arbitrary invalid input.
            if cat == "excluded-input":
                key = src.hex()
                minima[key] = {"reduction": "excluded exact control",
                               **encode_case(src, r, a), "origins": [name]}
            continue
        s["scanner_error_free"] += 1
        if check:
            s["parse_valid" if not r.get("parse_error") else "parse_invalid"] += 1
            if not r.get("parse_error"):
                s["type_valid" if not r.get("type_error") else "type_invalid"] += 1
        if not h.diff(src):
            s["match"] += 1
        else:
            s["divergence"] += 1
            if a["status"]:
                s["nonzero_status"] += 1
            if check and not r.get("parse_error") and not r.get("type_error"):
                s["valid_complete_program_divergence"] += 1
            minimal = h.minimize(src)
            key = minimal.hex()
            if key not in minima:
                minima[key] = {"reduction": "byte-deletion 1-minimal",
                               **encode_case(minimal, h.reference(minimal, True), h.actual(minimal)),
                               "origins": []}
            minima[key]["origins"].append(name)
            # Full originals are deterministic case generator products, record
            # their SHA and ID rather than duplicating every source and stream.
            failed_origins.append({"id": name, "category": cat,
                                   "sha256": hashlib.sha256(src).hexdigest(),
                                   "minimum_hex": key,
                                   "scanner_errors": len(r["errors"]),
                                   "actual_status": a["status"],
                                   "complete_program_valid": check and not r.get("parse_error") and not r.get("type_error")})
        if seen % 1000 == 0:
            print(f"{seen} cases, {h.runs} processes, {len(failed_origins)} failures, {len(minima)} minima, {time.monotonic()-begin:.1f}s", flush=True)
    data = {"head": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
            "go": subprocess.check_output(["go", "version"], text=True).strip(),
            "seed": SEED, "cpus": args.cpus, "generated": generated,
            "tested": seen, "strand_processes": h.runs,
            "elapsed_seconds_not_benchmark": round(time.monotonic()-begin, 3),
            "summary": dict(summary), "unique_minima": len(minima),
            "failing_origins": failed_origins}
    (ROOT / (args.output + "-summary.json")).write_text(json.dumps(data, indent=2) + "\n")
    (ROOT / (args.output + "-minima.json")).write_text(json.dumps(list(minima.values()), indent=2) + "\n")
    print(json.dumps({k: v for k, v in data.items() if k != "failing_origins"}, indent=2), flush=True)
    h.oracle.terminate()


if __name__ == "__main__":
    main()
