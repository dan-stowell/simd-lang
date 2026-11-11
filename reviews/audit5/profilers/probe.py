#!/usr/bin/env python3
"""Run from this directory. All generated binaries/large files stay in work/."""
import hashlib, json, os, pathlib, re, resource, signal, subprocess, sys, time

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[2]
WORK = HERE / "work"
WORK.mkdir(exist_ok=True)
CPU = min(os.sched_getaffinity(0))
records = []

def run(args, *, timeout=10, env=None, stdin=None):
    args = list(map(str, args))
    before = resource.getrusage(resource.RUSAGE_CHILDREN)
    t = time.monotonic()
    p = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                         stdin=stdin, start_new_session=True, env={**os.environ, **(env or {})})
    try:
        out, err = p.communicate(timeout=timeout)
        expired = False
    except subprocess.TimeoutExpired:
        os.killpg(p.pid, signal.SIGKILL)
        out, err = p.communicate()
        expired = True
    after = resource.getrusage(resource.RUSAGE_CHILDREN)
    r = dict(argv=args, status=p.returncode, timeout=expired,
             wall=time.monotonic()-t, user=after.ru_utime-before.ru_utime,
             system=after.ru_stime-before.ru_stime, faults=after.ru_minflt-before.ru_minflt,
             stdout=out.decode(errors="replace"), stderr=err.decode(errors="replace"))
    records.append(r)
    return r

def asm(name, text):
    path = HERE / (name + ".S")
    path.write_text(".global _start\n.text\n_start:\n" + text)
    r = run(["gcc", "-nostdlib", "-no-pie", "-Wl,--build-id=none", "-o", WORK/name, path])
    assert r["status"] == 0, r
    (HERE/(name+".dis")).write_text(run(["objdump","-d",WORK/name])["stdout"])
    return WORK/name

EXIT = "\n mov $60,%eax\n xor %edi,%edi\n syscall\n"

def ratio(name, path, timeout=10):
    return run(["taskset","-c",CPU,WORK/"simdratio",0,path], timeout=timeout,
               env={"SIMDRATIO_HIST":str(HERE/(name+".hist"))})

def sample(name, path):
    r = run(["taskset","-c",CPU,WORK/"sample",path],
            env={"SAMPLE_OUT":str(HERE/(name+".samples"))})
    counts = {}
    for line in (HERE/(name+".samples")).read_text().splitlines():
        a, _, n = map(int, line.split())
        counts[hex(a)] = n
    r["samples"] = counts
    return r

def main():
    (HERE/"environment.txt").write_text(run(["uname","-a"])["stdout"] +
        run(["git","-C",ROOT,"rev-parse","HEAD"])["stdout"] +
        run(["gcc","--version"])["stdout"] +
        "allowed cpus: "+str(sorted(os.sched_getaffinity(0)))+"\n")
    for name in ("sample","simdratio"):
        assert run(["gcc","-O2","-Wall","-Wextra","-o",WORK/name,ROOT/"tools"/(name+".c")])["status"] == 0
    normal = asm("normal", " mov $100,%ecx\nloop:\n vpxor %ymm0,%ymm0,%ymm0\n dec %ecx\n jnz loop\n"+EXIT)
    ratio("normal", normal)
    trap = asm("trap", " int3\n"+EXIT)
    run([trap])
    ratio("trap", trap)
    sample("trap", trap)
    fault = asm("fault", " ud2\n"+EXIT)
    run([fault])
    ratio("fault", fault, timeout=0.15)
    sample("fault", fault)
    repeated = asm("repeated", " lea buf(%rip),%rdi\n mov $100,%ecx\n xor %eax,%eax\n rep stosb\n"+EXIT+
                   "\n.bss\n.lcomm buf,100\n")
    ratio("repeated", repeated)
    clone = asm("clone", """
 lea stackend(%rip),%rsi
 mov $0x50f00,%edi
 xor %edx,%edx
 xor %r10d,%r10d
 xor %r8d,%r8d
 mov $56,%eax
 syscall
 test %eax,%eax
 jz worker
parent_wait:
 mov $35,%eax
 lea delay(%rip),%rdi
 xor %esi,%esi
 syscall
 cmpq $1,done(%rip)
 jne parent_wait
 mov $231,%eax
 xor %edi,%edi
 syscall
worker:
 mov $10000000,%ecx
worker_loop:
 vpxor %ymm0,%ymm0,%ymm0
 dec %ecx
 jnz worker_loop
 movq $1,done(%rip)
 mov $60,%eax
 xor %edi,%edi
 syscall
.data
delay: .quad 0,50000000
done: .quad 0
.bss
.lcomm stack,65536
stackend:
""")
    run(["taskset","-c",CPU,clone])
    ratio("clone", clone)
    sample("clone", clone)
    sleep = asm("sleep", """
 mov $35,%eax
 lea delay(%rip),%rdi
 xor %esi,%esi
 syscall
after_sleep:
""" + EXIT + "\n.data\ndelay: .quad 0,200000000\n")
    run(["taskset","-c",CPU,sleep])
    sample("sleep", sleep)
    busy = asm("busy", " mov $300000000,%ecx\nloop:\n dec %ecx\n jnz loop\n"+EXIT)
    run(["taskset","-c",CPU,busy])
    for i in range(3):
        sample("busy"+str(i), busy)
    populate = asm("populate", """
 mov $9,%eax
 xor %edi,%edi
 mov $268435456,%esi
 mov $3,%edx
 mov $0x8022,%r10d
 mov $-1,%r8
 xor %r9d,%r9d
population_syscall:
 syscall
after_population:
 mov $60000000,%ecx
busy_loop:
 dec %ecx
 jnz busy_loop
""" + EXIT)
    for i in range(3):
        run(["taskset","-c",CPU,populate])
        sample("populate"+str(i), populate)
    pie = asm("outside", " mov $300000000,%ecx\nloop:\n dec %ecx\n jnz loop\n"+EXIT)
    run(["gcc","-nostdlib","-no-pie","-Wl,--build-id=none,-Ttext=0x900000","-o",WORK/"outside",HERE/"outside.S"])
    (HERE/"outside.dis").write_text(run(["objdump","-d",pie])["stdout"])
    sample("outside", pie)
    for missing in ("/definitely/not/a/program",):
        run([WORK/"sample", missing], env={"SAMPLE_OUT":str(HERE/"missing.samples")})
        run([WORK/"simdratio",0,missing])
    (HERE/"results.json").write_text(json.dumps(records, indent=2)+"\n")
    print("Probe complete; see results.json")

if __name__ == "__main__":
    main()
