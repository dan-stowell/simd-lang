#!/usr/bin/env python3
"""Human-readable representatives, semantic-validity checks, and CPU replay."""
import collections
import json
from pathlib import Path
import struct
import subprocess
from probe import BUILD, ROOT, Harness, encode_case


CASES = [
    ("numeric-identifier", "lexical snippet", b"0a\n"),
    ("imaginary-identifier", "lexical snippet", b"1ii\n"),
    ("imaginary-digit", "lexical snippet", b"1i0\n"),
    ("exponent-identifier", "lexical snippet", b"1e2e3\n"),
    ("hex-identifier", "lexical snippet", b"0x1g\n"),
    ("hex-float-identifier", "lexical snippet", b"0x1p0g\n"),
    ("identifier-leading-dot", "lexical snippet", b"x.0\n"),
    ("float-leading-dot", "lexical snippet", b".0.0\n"),
    ("ellipsis-digit", "lexical snippet", b"...0\n"),
    ("two-dots-digit-control", "lexical snippet", b"..0\n"),
    ("two-dots-control", "lexical snippet", b"..\n"),
    ("ellipsis-control", "lexical snippet", b"...\n"),
    ("complete-return-leading-dot", "complete program", b"package p\nfunc f() float64 { return.5 }\n"),
    ("complete-if-leading-dot", "complete program", b"package p\nfunc f() { if.5 < 1 {} }\n"),
    ("complete-for-leading-dot", "complete program", b"package p\nfunc f() { for.5 < 1 { break } }\n"),
    ("complete-switch-leading-dot", "complete program", b"package p\nfunc f() { switch.5 { case.5: } }\n"),
    ("complete-case-leading-dot", "complete program", b"package p\nfunc f() { switch 0.5 { case.5: } }\n"),
    ("complete-return-leading-dot-imaginary", "complete program", b"package p\nfunc f() complex128 { return.5i }\n"),
    ("complete-return-leading-dot-exponent", "complete program", b"package p\nfunc f() float64 { return.5e+1 }\n"),
    ("complete-return-space-control", "complete program", b"package p\nfunc f() float64 { return .5 }\n"),
    ("complete-return-comment-control", "complete program", b"package p\nfunc f() float64 { return/**/.5 }\n"),
    ("complete-minimal", "complete program", b"package p;func f(){switch.0{}}\n"),
    ("complete-final-token", "complete program / documented EOF limit", b"package p"),
    ("complete-final-space-control", "complete program", b"package p "),
    ("empty-control", "lexical snippet", b""),
    ("blank-control", "lexical snippet", b"\n"),
    ("eof-token", "lexical snippet / documented EOF limit", b"x"),
    ("eof-operator", "lexical snippet / documented EOF limit", b"+"),
    ("eof-closed-string", "lexical snippet / documented EOF limit", b'""'),
    ("eof-line-comment-control", "lexical snippet", b"x//end"),
    ("eof-block-comment-control", "lexical snippet", b"x/**/"),
    ("block-comment-semicolon-control", "lexical snippet", b"x/*\n*/"),
    ("complete-leading-BOM", "complete program / excluded BOM", b"\xef\xbb\xbfpackage p\n"),
    ("leading-BOM", "lexical snippet / excluded BOM", b"\xef\xbb\xbf\n"),
    ("nonleading-BOM", "excluded scanner-error input", "x\ufeff\n".encode()),
    ("NUL-before-code", "excluded scanner-error input", b"\0x\n"),
    ("NUL-after-code", "excluded scanner-error input", b"x\0\n"),
    ("NUL-in-line-comment", "excluded scanner-error input", b"//\0\n"),
    ("NUL-in-raw-string", "excluded scanner-error input", b"`\0`\n"),
    ("nonletter-Unicode", "excluded scanner-error input", "😀\n".encode()),
    ("combining-mark", "excluded scanner-error input", "a\u0301\n".encode()),
]


def main():
    h = Harness("0")
    kind_names = {}
    rows = []
    for name, category, src in CASES:
        r = h.reference(src, True)
        kind_names.update({t["kind"]: t["name"] for t in r["tokens"]})
        actuals = []
        inp = BUILD/"focused-input.go"
        inp.write_bytes(src)
        for cpus in ["0", "0,1", "0,1,2", "0,1,2,3,4,5,6,7"]:
            # Eager mode additionally exercises the pool for >64-byte padded
            # inputs. It does not change the original witness bytes.
            for eager in [False, True]:
                cmd = ["taskset", "-c", cpus, str(BUILD/"golex_strand")] + (["eager"] if eager else [])
                with inp.open("rb") as f:
                    p = subprocess.run(cmd, stdin=f, capture_output=True, timeout=5)
                n = len(p.stdout)//5
                tokens = list(zip(struct.unpack("<%dI"%n, p.stdout[:4*n]), p.stdout[4*n:])) if len(p.stdout)%5 == 0 else []
                actuals.append({"cpus": cpus, "eager": eager, "status": p.returncode,
                                "tokens": tokens, "bytes": len(p.stdout)})
        assert all(a["tokens"] == actuals[0]["tokens"] and a["status"] == actuals[0]["status"] for a in actuals)
        a = actuals[0]
        row = {"name": name, "category": category, **encode_case(src, r, a),
               "match": not r["errors"] and a["status"] == 0 and a["tokens"] == [(t["offset"], t["kind"]) for t in r["tokens"]],
               "cpu_replays": actuals}
        if not r["errors"] and not row["match"]:
            minimum = h.minimize(src)
            row["minimum"] = encode_case(minimum, h.reference(minimum, True), h.actual(minimum))
        if name == "complete-minimal":
            # Verify the complete-program witness is 1-minimal under the
            # stronger constraint that it remains scanner/parser/type-valid.
            assert not r["errors"] and not r.get("parse_error") and not r.get("type_error") and not row["match"]
            for i in range(len(src) - 1):
                trial = src[:i] + src[i+1:]
                tr = h.reference(trial, True)
                if not tr["errors"] and not tr.get("parse_error") and not tr.get("type_error"):
                    assert not (h.actual(trial)["status"] == 0 and h.diff(trial)), (i, trial)
            row["complete_program_single_byte_deletion_minimal"] = True
        rows.append(row)
        print(name, repr(src), "scan-errors", len(r["errors"]), "parse-valid", not r.get("parse_error"),
              "type-valid", not r.get("parse_error") and not r.get("type_error"),
              "status", a["status"], "match", row["match"], flush=True)
        print(" expected", [(t["offset"], t["kind"], t["name"]) for t in r["tokens"]], flush=True)
        print(" actual  ", a["tokens"], flush=True)
    # Numeric kinds used only in Strand's output are established by go/token's
    # oracle token stream above (no hand-maintained remapping).
    for row in rows:
        for a in [row["actual"]] + row["cpu_replays"]:
            a["decoded_tokens"] = [{"offset": off, "kind": kind, "name": kind_names.get(kind, str(kind))}
                                   for off, kind in a["tokens"]]
    (ROOT/"focused-results.json").write_text(json.dumps(rows, indent=2)+"\n")
    h.oracle.terminate()


if __name__ == "__main__":
    main()
