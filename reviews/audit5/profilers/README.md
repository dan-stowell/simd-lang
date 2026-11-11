# Profiler attribution audit — 25a887e

**Bottom line:** useful one-thread hotspot probes, not an exact accounting of
whole-process time. The strongest demonstrated problem is unequal treatment of
sleeping and long kernel work. The current GOLEX instruction classifier passed;
source-map imprecision was large for tiny instruction-count inputs but negligible
in three large-input sampling controls. No claim here reopens the old
independently-compiled-prefix profiler finding.

## Reproduce and evidence

From this directory, with the repository already built:

```sh
ulimit -c 0
python3 probe.py
python3 target.py > target.log 2>&1
python3 ip_control.py
```

Requires x86-64 Linux ptrace, GCC, objdump, Python and taskset; uses the first
allowed CPU (0 in this run). `work/` contains ignored executables and the
64,000,001-byte synthetic input. Assembly fixtures and disassemblies are retained.
`results.json`, `target.json`, `target-commands.json`, and `ip-control.json`
record commands, statuses, elapsed time, child-resource deltas and stdout/stderr.
Resource deltas for profiler invocations include both tracer and tracee; **only
unprofiled controls** are used below to estimate target CPU cost. `environment.txt`
and `machine.txt` record the host. Test timings are controls, not speed benchmarks.
Synthetic Go input is exactly `b"package p\nvar x = 1\n"` repeated; `target.py`
records the source and emitted-binary hashes.
Stock sampling does not expose its child's final status: sampling-run status
fields are the **tool's** status, not independently checked tracee exit codes.
The two single-stepped GOLEX controls explicitly report target exit 0.

No production source changed. All profiler executions completed before releasing
the machine to the speed campaign.

## 1. Sampling is neither CPU sampling nor unbiased elapsed-time sampling

`sample.c:25–32` sleeps, sends SIGSTOP, **waits for delivery**, records the saved
user RIP, and resumes. A slow stop adds delay without increasing the sample
weight. The interval is not a fixed 250 us: that is the requested sleep alone.

| Fixture | Unprofiled observation | Stock sampler |
|---|---|---|
| `sleep.S`: one 200-ms nanosleep | 202.4 ms elapsed; 1.8 ms user including taskset; 0 recorded system | 496 samples, **all at the instruction after syscall** |
| `populate.S`: 256-MiB MAP_POPULATE, then 60M decrement-loop iterations | Three runs: 15.1–18.7 ms user, **78.5–82.9 ms system**, about 103 ms elapsed | **One** post-mmap sample each run, versus 37/56/37 loop samples |
| `outside.S`: user loop linked at 0x900000 | Executable outside hard-coded range | All 261 samples called “outside the program: kernel or stub”; empty histogram |

The population control spends 80.8–84.6% of measured CPU time in the kernel,
but the allocation boundary receives only **1.8–2.6%** of samples. Conversely,
the sleep fixture's almost entirely off-CPU wait is represented as 496 samples
at an ordinary user instruction. Both report **zero outside-program samples**.
Thus the diagnostic at `sample.c:35` is not a kernel counter: the range
0x400000–0x7fffff contains the Strand stub too, while “other” can be ordinary
user code.

**GOLEX impact:** the 72/24/4 and token percentages are shares of retained
stops, not demonstrated shares of the 448-ms total. They cannot independently
establish the “85 ms is the kernel” statement or the fraction of sink time due
to first-touch faults. This control uses MAP_POPULATE, unlike GOLEX's lazy heap:
it proves the measurement is not generally unbiased, **not** that GOLEX loses
80% or that its separately obtained system-time number is wrong. No correction
factor for the published corpus is established here.

RIP is a stop location, not a measured instruction latency. Do not assume a
universal one-instruction shift either: in `divide.S`, all 54 samples landed
on the actual DIV, none on the following NOP. The simple decrement loop put
all 271/287/271 samples at its conditional branch. These observations support
coarse hotspots, not causal costs for each source token.

## 2. Thread coverage is explicitly incomplete; taskset does not make a process single-threaded

Neither tool enables clone/fork tracing or waits for arbitrary tracee TIDs.
`clone.S` uses the same clone flags as the Strand pool, runs on **one allowed
CPU**, executes 10,000,000 VPXOR instructions in its worker, sets a completion
flag, and exits successfully after the leader observes that flag.

* `simdratio`: **0 vector, 18 scalar**, 3 syscalls, target exit 0.
  The omitted worker actually executes about 30 million loop instructions,
  roughly one third vector.
* `sample`: all **140** samples at the leader's post-nanosleep RIP; no worker IP.

This invalidates general whole-process or multicore readings. It **does not
refute GOLEX's explicitly one-core profile**: `stencils/stub.S:41–64` checks
CPU affinity and sets the fork threshold to INT64_MAX when one CPU is allowed.
The current serial GOLEX fixture takes no clone path. Do not extend those
serial percentages to the two-/four-/eight-core dry/real passes.

## 3. “Every executed instruction” requires normal-trace and REP caveats

`simdratio.c:48–56` counts before attempting the step, accepts any stop as
another instruction, and resumes with signal argument zero.

| Fixture | Native result | simdratio result |
|---|---|---|
| `normal.S`, 100 three-instruction loop iterations | Successful | Correct: 100 vector + 204 scalar = 304 |
| `trap.S`, INT3 then exit | SIGTRAP, subprocess status -5 | Trap suppressed; **target exit 0**, 4 scalar |
| `fault.S`, UD2 | SIGILL, subprocess status -4 | Still running after 150 ms; harness kills process group |
| `repeated.S`, RCX=100; REP STOSB | One repeat instruction, 3 setup and 3 exit instructions | **106 scalar**, including 100 steps at REP |

Repeated string operations therefore use iteration counts while ordinary SIMD
uses one count per instruction. This matters to the advertised instruction
definition: the actual runtime contains REP MOVSQ (`stub.S:672`), and the
generated allocation-reuse path contains REP STOSB (`build/stencils.S:3611`).
It was **not exercised by the two serial GOLEX instruction controls**, so no
76%-vector correction is asserted.

`sample` does forward the tested SIGTRAP/SIGILL, but treats the delivery stop
as a sample and returns success without reporting target termination. Both
tools also return status 0 for a nonexistent executable; only simdratio prints
the child's 127. Profiler success is not evidence that the measured run passed.

## 4. Classifier passes current GOLEX; source mapping has measurable boundaries

The emitted GOLEX image is 46,793 bytes; stub 6,144 bytes; generated code starts
at 0x4075f0; first mapped token at 0x407656. `target.py` independently decodes
all **2,904 generated-code instructions** with objdump and compares mnemonic
classes (vector / gather-scatter / mask / scalar) with the actual C classifier:
**zero mismatches**. Every dynamically counted generated address in the two
serial controls also matches that decoding. This supports the classifier for
this binary, not every possible x86 encoding or the claimed gather latency.

The script separately reconstructs every emitted prologue/body/release/tail
interval from the compiler tables. `whattime.py:13–17` instead uses only the
last token-body start, with no upper bound or image identity.

* In both 80-byte and 2,560-byte instruction-count inputs, **2,189 prologue
  instructions** are absent or assigned to the preceding line, and the final
  tail instruction is assigned to the last token. In particular 1,782 byte-line
  prologue instructions go to line 26, `N 1 - >Tm`.
* On the 80-byte input, line 42 really contains 3,611/5,004 generated
  instructions (**72.2%**); the mapper reports 1,841/4,992 (**36.9%**).
  Line 26 really has 32 instructions; it is reported with 1,802 (**36.1%**).
  On 2,560 bytes, the line-42 distortion shrinks to **67.1% versus 63.8%**.
  This is the same full executable, not separately compiled prefixes.
* On three 64,000,001-byte sampling controls, 1,344–1,370 generated samples
  included only **0/1/0** prologue samples; all others were bodies.
  Correcting intervals changes the middle run by **one sample, 0.074 percentage
  points**. So prologue charging is **not demonstrated to explain away the
  published large-input distribution**.
* A fabricated histogram address 4,096 bytes past the emitted image is accepted
  and charged **100% to the last `outif`**. No assertion or mismatch warning.
  This is a mapper-boundary test, not an observed GOLEX execution.

Those large synthetic runs put 60.5–62.1% in the byte line and 31.1–32.4% in
the keyword line. They are **a different corpus**, not a failed reproduction
of 72/24. The token category `\\x` varied 12.1–15.5% on identical input.
Even ideal independent sampling at n=1,457 has approximate 95% binomial
half-widths of 2.3 percentage points at 72%, 2.1 at 20%, and 1.0 at 4%;
these are reference scales, **not confidence intervals for this periodic,
potentially correlated sampler**. Original sample addresses, executable hash,
affinity, repeated runs and separate target user/system accounting are needed
to audit the exact published percentages.

## Suggested narrow remedies

Keep instruction counts distinct from time; explicitly report serial/thread
coverage, REP semantics, stub/unmapped/excluded counts and child status.
Distinguish step traps from signal-delivery stops and preserve real signals.
Use explicit code intervals with phase ownership plus an executable/map hash.
For time, record timestamps and stop latency, retain repeated profiles and
independent whole-process user/system measurements; do not label “other” as
kernel or use a user RIP to apportion page-fault service time.

Linux man-pages `ptrace(2)`, sections on `PTRACE_O_TRACECLONE` and “Signal
injection and suppression”, corroborates the tracing semantics: zero suppresses
delivery, suppressed signals can restart syscalls, and child tracing requires
the corresponding options. The findings above are independently demonstrated
by the retained fixtures.

```text
https://man7.org/linux/man-pages/man2/ptrace.2.html
```
