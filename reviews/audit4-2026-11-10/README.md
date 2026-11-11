# Audit 4: shared checks, compiler timing and profiler controls

Audited tree: `b591f699daa4bc4d7994c2b16f1891cb538ba113`, compared with
`af3ecc935319a4698b499c66044faff95ef0663d`. See root `AUDIT_4.md` for conclusions
and the other `reviews/audit4-*` directories for independent sub-audits.

Only audit evidence/report files are committed. Generated inputs, binaries,
patched ELF controls and full outputs are intentionally under ignored `build/`.
There are no compiler/runtime/benchmark/example fixes in this audit.

## Files

* `environment.json`, `lscpu.txt`: platform, revision, original source hashes.
* `test.log`: complete default `./test.sh` output; all stages/fuzz/pool/link
  checks pass and the fixed point is retained.
* `timer.c`: checked fork/exec/wait4 child timing and resource counters. Unlike
  the original prefix helper, it rejects unsuccessful execution.
* `measure_compiler.py`, `compiler.json`, `compiler.log`: all 180 whole-compiler
  and 234 prefix-control samples, successful output hashes, affinities,
  executable/source hashes, peak RSS, faults and CPU/wall times.
* `prefix-manifest.json`: offsets/hashes of copies of the full compiler patched
  with `exit_group(0)` immediately after selected complete lines. Instructions
  before each exit remain unchanged. These controls retain full-program
  liveness, allocations and local/persistent-tee decisions; independently
  compiled prefixes do not.
* `lineprof.log`: unmodified `tools/lineprof.py` on the same 8-copy source.
  This log is evidence about that method, not a trusted per-line profile.
* `check_method_and_backlog.py`, `method-and-backlog.json`, `method-and-backlog.log`:
  extract and exercise the original profiler's `measure()` function on a
  failing executable; compare prefix/full token classes; reproduce the
  duplicate-key JSON oracle bug; hash unchanged third-audit backlog files.
* `fsm-coverage.json`: deterministic recount of all-function fixture coverage.
* `slice-read.json`: independently reproduced accepted slice/read mismatch.
* `input-gather.json`: stock input-tail gather observations; ordinary execution
  success does not establish the physical width is within the mapped object.

## Commands

From the repository root:

```sh
./test.sh > reviews/audit4-2026-11-10/test.log 2>&1
python3 reviews/audit4-2026-11-10/check_method_and_backlog.py \
  > reviews/audit4-2026-11-10/method-and-backlog.log 2>&1
```

The full test rebuild regenerates derived stencils/tables and compiler
binaries; it does not intentionally change tracked source.

Only on a quiescent VM, separately from correctness tests or other timing jobs:

```sh
python3 reviews/audit4-2026-11-10/measure_compiler.py \
  > reviews/audit4-2026-11-10/compiler.log 2>&1
python3 tools/lineprof.py build/audit4/big8.strand \
  > reviews/audit4-2026-11-10/lineprof.log 2>&1
```

`measure_compiler.py --prepare` only constructs fixtures and controls.
Default measurement uses 15 rounds of all source/implementation/affinity
configurations, then nine rounds of all prefix controls, with seeded shuffled
order. Source input is a cached regular file and timed stdout is `/dev/null`.
Separate output comparisons precede the timed samples; discarded timed output
is not falsely described as hashed. CPU3 is the single-CPU affinity; the other
configuration allows CPUs0–7. The executable itself decides whether to spawn.

The JSON retains individual samples rather than only minima. Its `medians`
fields are independent medians of each counter: do not add median user and
median system times and assume this equals a median total. The timer includes
launch, input/output descriptor setup and process exit; no shell or Python
startup is charged to the compiler child. Very short runtimes remain noisy.

## Limits

Both native compilers use the generated stencil library. Python interpretation
shares its front-end with Python compilation. The prefix controls test an
attribution confound, not a hardware profiler. End-to-end compiler speed is
tested on copies of its own source, not a broad application corpus. Host
scheduling, virtualization and output-device effects are not controlled by
pinning guest CPUs.
