#!/usr/bin/env python3
"""Build/check by default. Measurements require explicit --timing-authorized.

Run from any directory; all subprocess cwd values are the repository root.
"""
import argparse, hashlib, json, os, pathlib, platform, random, re, subprocess

ROOT = pathlib.Path(__file__).resolve().parents[2]
HERE = ROOT / "reviews/audit4-link-prototype"
BUILD = ROOT / "build/audit4-link-prototype"
BASE = "b591f69"
TAIL = "if (n) { w = 0; memcpy(&w, s, n); h = mix(h ^ w); }"
# Same little-endian hash, same safe byte range, fixed-size loads rather than a
# fortified variable-size memcpy call. No compiler-flag relaxation.
DIRECT_TAIL = """if (n) {
    w = 0; unsigned shift = 0;
    if (n & 4) { uint32_t a; memcpy(&a, s, 4); w = a; s += 4; shift = 32; }
    if (n & 2) { uint16_t a; memcpy(&a, s, 2); w |= (uint64_t)a << shift; s += 2; shift += 16; }
    if (n & 1) w |= (uint64_t)(uint8_t)*s << shift;
    h = mix(h ^ w);
  }"""
STORE = "memcpy(o + ro[k], &v, 4);"
DIRECT_STORE = "o[ro[k]]=v; o[ro[k]+1]=v>>8; o[ro[k]+2]=v>>16; o[ro[k]+3]=v>>24;"

def execute(args, **kw):
    return subprocess.run(args, cwd=ROOT, check=True, text=True, **kw)

def build():
    BUILD.mkdir(parents=True, exist_ok=True)
    original = execute(["git", "show", f"{BASE}:explore/link.c"], capture_output=True).stdout
    assert original.count(TAIL)==1 and original.count(STORE)==1
    (BUILD / "original.c").write_text(original)
    helpers=original.split("enum { U =")[0]
    (BUILD / "prototype_helpers.h").write_text(
        "#ifdef AUDIT_DIRECT_TAIL\n"+helpers.replace(TAIL,DIRECT_TAIL)+
        "\n#else\n"+helpers+"\n#endif\n")
    (BUILD / "controls.c").write_text((HERE / "controls.c").read_text())
    (BUILD / "original-store.c").write_text(original.replace(STORE,DIRECT_STORE))
    (BUILD / "original-store-tail.c").write_text(original.replace(STORE,DIRECT_STORE).replace(TAIL,DIRECT_TAIL))
    # Preserve the original kernels, disable its clocks, strengthen final status.
    original_check = re.sub(r"static double now\(void\)\{[^\n]*\}",
                            "static double now(void){ return 0; }", original)
    original_check = original_check.replace("  return 0; }", r'''
  size_t wrong_name=0, wrong_race=0;
  for (size_t i=0;i<(size_t)U*R;i++) { wrong_name+=res[i]!=undef[i]; wrong_race+=res2[i]!=undef[i]; }
  int wrong_bytes=memcmp(out,out2,(size_t)U*CODE)!=0;
  printf("AUDIT_CHECK names=%zu race=%zu bytes=%d\n",wrong_name,wrong_race,wrong_bytes);
  return wrong_name||wrong_race||wrong_bytes; }''')
    (BUILD / "original-check.c").write_text(original_check)
    (BUILD / "original-store-check.c").write_text(original_check.replace(STORE,DIRECT_STORE))
    (BUILD / "original-store-tail-check.c").write_text(original_check.replace(STORE,DIRECT_STORE).replace(TAIL,DIRECT_TAIL))
    (BUILD / "original-tail-check.c").write_text(original_check.replace(
        "U = 2000, D = 500, R = 1500, S = U * D, RELOCS = 6000, CODE = 128 << 10",
        "U = 3, D = 137, R = 193, S = U * D, RELOCS = 257, CODE = 4096"))
    flags = ["-std=gnu11", "-O3", "-march=native", "-Wall", "-Wextra", "-Werror"]
    commands=[]
    def compile_command(command):
        commands.append(command)
        execute(command)
    for name, extra in [("controls", []), ("controls-directtail", ["-DAUDIT_DIRECT_TAIL"]),
                        ("controls-directtail-asan", ["-DAUDIT_DIRECT_TAIL", "-O1", "-g", "-fsanitize=address,undefined", "-fno-omit-frame-pointer"]),
                        ("controls-asan", ["-O1", "-g", "-fsanitize=address,undefined", "-fno-omit-frame-pointer"]),
                        ("controls-novec", ["-fno-tree-vectorize", "-fno-tree-slp-vectorize"])]:
        compile_command(["gcc", *flags, *extra, "-I", str(BUILD), str(BUILD/"controls.c"), "-o", str(BUILD/name)])
    compile_command(["gcc", *flags, str(BUILD/"original.c"), "-o", str(BUILD/"original")])
    for name in ["original-check", "original-tail-check", "original-store", "original-store-tail",
                 "original-store-check", "original-store-tail-check"]:
        compile_command(["gcc", *flags, str(BUILD/f"{name}.c"), "-o", str(BUILD/name)])
    for name in ["controls", "controls-directtail", "original", "original-store", "original-store-tail"]:
        with (BUILD/f"{name}.asm").open("w") as out:
            execute(["objdump", "-drwC", str(BUILD/name)], stdout=out)
    meta = {"base": BASE, "original_sha256": hashlib.sha256(original.encode()).hexdigest(),
            "controls_sha256": hashlib.sha256((HERE/"controls.c").read_bytes()).hexdigest(),
            "gcc": execute(["gcc", "--version"], capture_output=True).stdout,
            "flags": flags, "uname": platform.uname()._asdict(),
            "cpu": execute(["lscpu"], capture_output=True).stdout,
            "affinity": sorted(os.sched_getaffinity(0)),
            "compile_commands": commands,
            "gcc_driver_trace": execute(["gcc","-###",*flags,str(BUILD/"original.c"),"-o",str(BUILD/"original")],capture_output=True).stderr,
            "source_sha256": {p.name:hashlib.sha256(p.read_bytes()).hexdigest()
                              for pattern in ["*.c","*.h"] for p in BUILD.glob(pattern)},
            "binary_sha256": {pathlib.Path(c[-1]).name:hashlib.sha256(pathlib.Path(c[-1]).read_bytes()).hexdigest() for c in commands}}
    (HERE/"environment.json").write_text(json.dumps(meta, indent=2)+"\n")

def check():
    results = []
    cases = [
        ("controls", ["--small"]),
        ("controls", ["--small", "--missing"]),
        ("controls", ["--small", "--hash-bits", "8"]),
        ("controls", ["--small", "--fresh-output"]),
        ("controls-asan", ["--small"]),
        ("controls-asan", ["--small", "--missing"]),
        ("controls-asan", ["--small", "--hash-bits", "8"]),
        ("controls-novec", ["--small"]),
        ("controls-directtail", ["--small"]),
        ("controls-directtail-asan", ["--small", "--missing"]),
        ("controls-directtail-asan", ["--small", "--hash-bits", "8"]),
        ("controls", []),
        ("controls-directtail", []),
    ]
    for name, args in cases:
        command = [str(BUILD/name), "--check", *args]
        p = execute(command, capture_output=True, env={**os.environ, "ASAN_OPTIONS": "detect_leaks=0"})
        rows = [json.loads(line) for line in p.stdout.splitlines()]
        assert rows[0]["check_only"]
        if "--hash-bits" in args:
            assert any(r.get("mismatches", 0)>0 for r in rows if r.get("variant")=="soa_pre_fp")
        results.append({"command": command, "returncode": p.returncode, "rows": rows, "stderr": p.stderr})
        print("PASS", name, " ".join(args) or "full-size", flush=True)
    original_results = []
    for name, expected in [("original-check",0),("original-tail-check",1),
                           ("original-store-check",0),("original-store-tail-check",0)]:
        p=subprocess.run([str(BUILD/name)],cwd=ROOT,text=True,capture_output=True)
        assert p.returncode==expected, (name,p.returncode,p.stdout,p.stderr)
        marker=p.stdout.split("AUDIT_CHECK ")[-1].strip()
        assert marker == ("names=0 race=0 bytes=0" if not expected else "names=0 race=3 bytes=0"), marker
        original_results.append({"variant":name,"returncode":p.returncode,"stdout":p.stdout,"stderr":p.stderr})
        print("PASS",name,"expected failure" if expected else "",flush=True)
    (HERE/"original-correctness.json").write_text(json.dumps(original_results,indent=2)+"\n")
    (HERE/"correctness.json").write_text(json.dumps(results, indent=2)+"\n")

def measure(args):
    if not args.timing_authorized:
        raise SystemExit("No performance campaign without explicit --timing-authorized after an isolated window is granted.")
    cpu = args.cpu if args.cpu is not None else min(os.sched_getaffinity(0))
    os.sched_setaffinity(0, {cpu})
    records = []
    # One process per paired randomized-order trial; exact same deterministic job.
    conditions = [([], "warm-prefaulted"), (["--cold"], "evicted-prefaulted"),
                  (["--cold", "--fresh-output"], "evicted-fresh-output")]
    plan = [(rep, flags, label, binary)
            for rep in range(args.repeats) for flags, label in conditions
            for binary in ["controls","controls-directtail"]]
    random.Random(59169).shuffle(plan)
    for rep, flags, label, binary in plan:
        command = [str(BUILD/binary), "--order", str(rep+100), *flags]
        p = execute(command, capture_output=True)
        rows = [json.loads(line) for line in p.stdout.splitlines()]
        records.append({"condition": label, "binary": binary, "rep": rep, "cpu": cpu, "command": command, "rows": rows})
        (HERE/"timings.json").write_text(json.dumps(records, indent=2)+"\n")
        print("DONE", binary, label, rep, flush=True)
    # Optional legacy reproduction is deliberately separate: it has fixed-order,
    # first-touch and underchecked semantic behavior; never merge its rows above.
    if args.legacy:
        legacy = []
        legacy_plan=[(rep,name) for rep in range(args.repeats) for name in ["original","original-store","original-store-tail"]]
        random.Random(59170).shuffle(legacy_plan)
        for rep,name in legacy_plan:
            p = execute([str(BUILD/name)], capture_output=True)
            assert "MISMATCH" not in p.stdout, p.stdout
            assert "0 definitions lost all 4 rounds, 0 references unresolved" in p.stdout, p.stdout
            assert "same result" in p.stdout and "same bytes" in p.stdout, p.stdout
            legacy.append({"rep": rep, "variant": name, "stdout": p.stdout, "stderr": p.stderr})
        (HERE/"legacy-timings.json").write_text(json.dumps(legacy, indent=2)+"\n")

if __name__ == "__main__":
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument("action", choices=["build","check","prepare","measure"], default="prepare", nargs="?")
    p.add_argument("--timing-authorized", action="store_true")
    p.add_argument("--cpu", type=int)
    p.add_argument("--repeats", type=int, default=7)
    p.add_argument("--legacy", action="store_true")
    a=p.parse_args()
    if a.action in ("build","prepare"):build()
    if a.action in ("check","prepare"):check()
    if a.action=="measure":measure(a)
