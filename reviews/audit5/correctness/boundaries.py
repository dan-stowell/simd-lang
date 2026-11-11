#!/usr/bin/env python3
"""SIMD/parallel boundary and conservative-CR-exclusion checks."""
import hashlib
import json
from pathlib import Path
import struct
import subprocess

BUILD = Path("build/audit5-correctness")
OUT = Path("reviews/audit5/correctness")


def sources():
    snippets = [
        b'0 0b101 0O_755 0xFF 0x1e 0x1p+2 0x.1P-2i .2e-3i 09i\n',
        b'"" "a\\\"b" "\\\\" "\\x00\\u0000\\U0001f600" \'\\377\'\n',
        b'`` `// /* */ \\\\ " \'` `a\nb` \'\\\'\' \'\\\\\'\n',
        b'x/*\n*/y\nx/*a*/+/**/y//tail\n',
        b'/*/ */ /***/ /*//*/ /* " \' ` // \\ */ x\n',
        b'.. ... .... ..... ...... . .0\n',
        b'+ ++ += - -- -= << <<= >> >>= & &^ &^= && || <-\n',
        b'return\nbreak\ncontinue\nfallthrough\nx\n)\n]\n}\n++\n--\n',
        'α π_٢ 变量 𐐀 a９\n'.encode(),
    ]
    align = b"".join(b" " * n + s for n in range(128) for s in snippets)
    yield "all-128-alignments", align
    yield "all-128-alignments-large", align * 12
    n = 2**20 + 65
    yield "long-identifier", b"x" * n + b"\n"
    yield "long-unicode-identifier", "𐐀".encode() * (n//4) + b"\n"
    yield "long-decimal", b"1" * n + b"\n"
    yield "long-hex-float", b"0x" + b"a" * n + b"p-2i\n"
    yield "long-block-comment", b"x/*" + b"q" * n + b"\n" + b"*" * n + b"/y\n"
    yield "long-line-comment", b"x//" + b"q" * n + b"\ny\n"
    yield "long-raw-string", b"`" + b'/*//"\'\\\n' * (n//8) + b"`\n"
    yield "long-escape-string", b'"' + b"\\\\" * n + b'\\"x"\n'
    cr = [
        b"x\ry\n", b"x\r\n", b"x\r", b"x//\r", b"x//\r\n",
        b"x/*\r*/y\n", b"x/*\r\n*/y\r\n", b'"a\rb"\r\n',
        b"`a\rb\r\nc`\r\n", b"'\r'\r\n", b"//line x:2\r\nx\r\n",
        b"/*line x:2*/x\r\n", b"package p\r\nvar x=1\r\n",
    ]
    for i, s in enumerate(cr):
        yield f"CR-control-{i}", s
    goroot = subprocess.check_output(["go", "env", "GOROOT"], text=True).strip()
    yield "excluded-CR-fixture", (Path(goroot) / "src/internal/types/testdata/fixedbugs/issue39754.go").read_bytes()


def decode(data):
    n = len(data)//5
    return list(zip(struct.unpack("<%dI" % n, data[:n*4]), data[n*4:]))


def main():
    rows = []
    for name, src in sources():
        inp = BUILD / "boundary-input.go"
        inp.write_bytes(src)
        r = subprocess.run([str(BUILD/"golex"), "dump", str(inp)], capture_output=True, check=True)
        assert r.stderr == b"scanner errors: 0\n", (name, r.stderr)
        for cpus in ["0", "0,1", "0,1,2", "0,1,2,3,4,5,6,7"]:
            for eager in [False, True]:
                cmd = ["taskset", "-c", cpus, str(BUILD/"golex_strand")]
                if eager:
                    cmd.append("eager")
                with inp.open("rb") as f:
                    a = subprocess.run(cmd, stdin=f, capture_output=True, timeout=20)
                row = {"name": name, "bytes": len(src), "input_sha256": hashlib.sha256(src).hexdigest(),
                       "cpus": cpus, "eager": eager, "scanner_errors": 0, "expected_status": 0,
                       "actual_status": a.returncode, "match": a.returncode == 0 and a.stdout == r.stdout,
                       "tokens": len(r.stdout)//5, "expected_sha256": hashlib.sha256(r.stdout).hexdigest(),
                       "actual_sha256": hashlib.sha256(a.stdout).hexdigest()}
                if not row["match"]:
                    row["expected_first_tokens"] = decode(r.stdout)[:50]
                    row["actual_first_tokens"] = decode(a.stdout)[:50] if len(a.stdout)%5 == 0 else a.stdout.hex()
                rows.append(row)
                print(name, cpus, "eager" if eager else "default", "PASS" if row["match"] else "FAIL", flush=True)
    (OUT/"boundaries-results.json").write_text(json.dumps(rows, indent=2) + "\n")
    print(f"{len(rows)} checks, {sum(not r['match'] for r in rows)} failures")
    assert all(r["match"] for r in rows)


if __name__ == "__main__":
    main()
