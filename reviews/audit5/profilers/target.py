#!/usr/bin/env python3
"""Target-specific instruction classifier and source-map controls."""
import bisect, collections, ctypes, hashlib, json, os, pathlib, re, subprocess, sys
import probe
from probe import HERE, ROOT, WORK, CPU, run
sys.path.insert(0,str(ROOT/"boot"))
import strand

src = (ROOT/"examples/golex.strand").read_bytes()
binary, cmap = strand.compile_(src, want_map=True)
target = WORK/"golex"
target.write_bytes(binary)
target.chmod(0o755)
T = strand.tables()
toks, lines, slots, pool = strand.analyze(src)
base = strand.VBASE + 112 + len(T["stub"]) + strand.SLOT * (slots+1) + 4*len(pool)
ranges = []
off = base
for ln in sorted(lines):
    for tok in lines[ln]:
        if tok.code:
            n = T["qlen"][tok.tid]
            if n: ranges.append((off,off+n,ln+1,"prologue",tok.raw.decode("latin1")))
            off += n
    for tok in lines[ln]:
        if tok.code:
            n = T["slen"][tok.tid]
            if n: ranges.append((off,off+n,ln+1,"body",tok.raw.decode("latin1")))
            off += n
    for tok in lines[ln]:
        if tok.dying:
            n = T["rell"]
            ranges.append((off,off+n,ln+1,"release",tok.raw.decode("latin1")))
            off += n
ranges.append((off,off+len(T["tail"]),None,"tail",None))
assert off+len(T["tail"]) == 0x400000+len(binary)
(HERE/"golex-ranges.json").write_text(json.dumps(ranges,indent=2)+"\n")
addrs = [x[0] for x in cmap]
starts = [x[0] for x in ranges]
(HERE/"classifier.c").write_text('#define main original_main\n#include "../../../tools/simdratio.c"\n#undef main\nint audit_classify(const unsigned char *p) { return classify(p); }\n')
assert run(["gcc","-shared","-fPIC","-O2","-o",WORK/"classifier.so",HERE/"classifier.c"])["status"] == 0
cl = ctypes.CDLL(str(WORK/"classifier.so")).audit_classify
dis = run(["objdump","-D","-b","binary","-m","i386:x86-64","--adjust-vma=0x400000",
           f"--start-address={base}","--insn-width=16",target])["stdout"]
(WORK/"golex.dis").write_text(dis)
decoded = {}
mismatch = []
for l in dis.splitlines():
    match = re.match(r"\s*([0-9a-f]+):\s*((?:[0-9a-f]{2} )+)\s*(.*)", l)
    if not match: continue
    a, hexes, instruction = match.groups()
    a = int(a,16)
    code = bytes.fromhex(hexes)
    mnemonic = instruction.split()[0]
    expected = 1 if ("gather" in mnemonic or "scatter" in mnemonic) else 2 if mnemonic.startswith("k") else 0 if mnemonic.startswith("v") else 3
    got = cl(ctypes.c_char_p(code+b"\0"*16))
    decoded[a] = [mnemonic,got,expected]
    if got != expected: mismatch.append([hex(a),code.hex(),instruction,got,expected])
summary = dict(source_sha256=hashlib.sha256(src).hexdigest(),
               binary_sha256=hashlib.sha256(binary).hexdigest(),
               binary_bytes=len(binary), stub_bytes=len(T["stub"]), code_start=hex(base),
               first_mapped=hex(addrs[0]), last_mapped=hex(addrs[-1]),
               static_instructions=len(decoded), classifier_mismatches=mismatch)
results = []
for repeats in (4,128):
    inp = WORK/f"go{repeats}.in"
    inp.write_bytes(b"package p\nvar x = 1\n"*repeats)
    hist = HERE/f"go{repeats}.hist"
    with inp.open("rb") as f:
        r = run(["taskset","-c",CPU,WORK/"simdratio",len(T["stub"]),target],
                env={"SIMDRATIO_HIST":str(hist)},stdin=f,timeout=30)
    assert r["status"] == 0 and "exit status 0" in r["stderr"], r
    exact = collections.Counter()
    mapped = collections.Counter()
    phases = collections.Counter()
    wrongly = collections.Counter()
    kinds = collections.Counter()
    for l in hist.read_text().splitlines():
        a,k,n = map(int,l.split())
        true = ranges[bisect.bisect_right(starts,a)-1]
        mi = bisect.bisect_right(addrs,a)-1
        attributed = cmap[mi] if mi >= 0 else None
        exact[true[2]] += n
        phases[true[3]] += n
        mapped[attributed[1] if attributed else None] += n
        if (attributed[1] if attributed else None) != true[2]:
            wrongly[(true[2], attributed[1] if attributed else None,true[3])] += n
        assert a in decoded, hex(a)
        assert k == decoded[a][1], (hex(a),k,decoded[a])
        kinds[(true[2],k)] += n
    results.append(dict(input_bytes=inp.stat().st_size, command=r, exact_lines=dict(exact),
        mapped_lines=dict(mapped), phases=dict(phases),
        wrong_line=[dict(real=t[0],mapped=t[1],phase=t[2],n=n) for t,n in wrongly.items()],
        line_kinds=[dict(line=t[0],kind=t[1],n=n) for t,n in kinds.items()]))
    results[-1]["whattime"] = run(["python3",ROOT/"tools/whattime.py",ROOT/"examples/golex.strand",hist])["stdout"]
summary["instruction_runs"] = results
fake = HERE/"past-end.samples"
fake.write_text(f"{0x400000+len(binary)+4096} 0 100\n")
summary["past_end"] = run(["python3",ROOT/"tools/whattime.py",ROOT/"examples/golex.strand",fake])
large = WORK/"repeated-go.in"
large.write_bytes((b"package p\nvar x = 1\n"*4000000)[:64000000])
# End on a delimiter, not halfway through an identifier/literal.
with large.open("ab") as f: f.write(b"\n")
summary["sampling_runs"] = []
for i in range(3):
    hist = HERE/f"golex-sample{i}.samples"
    with large.open("rb") as f:
        r = run(["taskset","-c",CPU,WORK/"sample",target],stdin=f,
            env={"SAMPLE_OUT":str(hist)})
    assert r["status"] == 0
    byphase, mapped, exact = collections.Counter(),collections.Counter(),collections.Counter()
    total=0
    for l in hist.read_text().splitlines():
        a,k,n=map(int,l.split()); total+=n
        if a<base: byphase["stub"]+=n; continue
        true=ranges[bisect.bisect_right(starts,a)-1]
        byphase[true[3]]+=n; exact[true[2]]+=n
        mi=bisect.bisect_right(addrs,a)-1
        mapped[cmap[mi][1] if mi>=0 else None]+=n
    summary["sampling_runs"].append(dict(command=r,total=total,phases=dict(byphase),
        exact_lines=dict(exact),mapped_lines=dict(mapped),
        whattime=run(["python3",ROOT/"tools/whattime.py",ROOT/"examples/golex.strand",hist])["stdout"]))
(HERE/"target.json").write_text(json.dumps(summary,indent=2)+"\n")
(HERE/"target-commands.json").write_text(json.dumps(probe.records,indent=2)+"\n")
print(json.dumps({k:v for k,v in summary.items() if k not in ("instruction_runs","sampling_runs","past_end")},indent=2))
for r in results:
    print("INPUT",r["input_bytes"],r["command"]["stderr"],"phases",r["phases"],"wrong",r["wrong_line"])
for r in summary["sampling_runs"]:
    print(r["command"]["stderr"],r["phases"],r["whattime"])
