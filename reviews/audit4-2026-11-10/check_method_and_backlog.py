#!/usr/bin/env python3
"""Small deterministic checks; neither edits nor times production implementations."""
import ast
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT / "boot"))
sys.path.insert(0, str(ROOT / "bench"))
import strand
import jsongen

# Execute the profiler's exact measurement function, without its top-level
# campaign. It accepts five exit-1 children and returns an ordinary sample.
tree = ast.parse((ROOT / "tools/lineprof.py").read_text())
function = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == "measure")
ns = {"os": os, "cpu": min(os.sched_getaffinity(0)), "inp": "/dev/null"}
exec(compile(ast.Module(body=[function], type_ignores=[]), "tools/lineprof.py", "exec"), ns)
failure = {"child": "/bin/false", "child_returncode": subprocess.run(["/bin/false"]).returncode,
           "profiler_measure_return": ns["measure"]("/bin/false")}

# Truncation changes not just release locations: it can remove a full vector
# allocation/store and the serial veto on a persistent tee's own-line reread.
short = b"input :a a + end\n"
long = short + b"a out4\n"
class_changes = []
for name, src in (("prefix", short), ("full", long)):
    toks, _, _, _ = strand.analyze(src)
    class_changes.append({"kind": name,
                          "first_line": [{"token": t.raw.decode(), "class": t.cls, "local": t.local,
                                          "prologue_bytes": strand.tables()["qlen"][t.tid]}
                                         for t in toks if t.line == 0]})

files = ["bench/base.c", "bench/base_omp.c", "bench/hand.c",
         "bench/oneoff.py", "bench/jsongen.py", "hw/check_probe.py",
         "tests/fsm_all.py", "tests/pool.py",
         "blog/04-between-the-compiler-and-the-expert.md"]
unchanged = []
for name in files:
    before = subprocess.check_output(["git", "-C", str(ROOT), "show", f"af3ecc9:{name}"])
    after = (ROOT / name).read_bytes()
    unchanged.append({"file": name, "unchanged_since_af3ecc9": before == after,
                      "sha256": hashlib.sha256(after).hexdigest()})
report = {
    "failed_children_accepted": failure,
    "prefix_changes_earlier_classes": class_changes,
    "backlog_files": unchanged,
    "duplicate_json_oracle": {"input": '{"x":[],"x":{}}',
                              "skeleton": jsongen.skeleton(b'{"x":[],"x":{}}').decode(),
                              "all_members_skeleton": "{:[],:{}}"},
}
(OUT / "method-and-backlog.json").write_text(json.dumps(report, indent=2) + "\n")
print(json.dumps(report, indent=2))
