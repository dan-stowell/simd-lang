# Third-audit evidence — November 10, 2026

Reviewed revision: `af3ecc935319a4698b499c66044faff95ef0663d`.
Verdict, interpretation and prioritized follow-ups: repository-root `AUDIT_3.md`.
No production implementation changes accompany this evidence.

## Main evidence

| Files | Contents |
|---|---|
| `test.log` | Complete default `./test.sh` after the main-suite/control timing campaigns: fixed point, 110 cases/stage, both fuzzers, 4,836 pool runs, 80 path checks, 48 all-function runs and big-input comparisons. An earlier full run also passed. |
| `mutation.log` | Five injected stencil faults caught by the repository's mutation test; generator restored. No mutation ran during a timing campaign. |
| `old-checks.json`, `old-checks.log` | Rerun of deterministic first-audit controls: timer status, race rejection, specified partial failure output, short reads, actual OpenMP teams and unsigned-prefix UBSan. |
| `claimed-results.json` | Original `build/suite/results.json`, preserved before any audit timing/build. Its dirty-tree and merged-JSON provenance is retained verbatim. |
| `environment.json` | Audited revision, host/CPU metadata, original source hashes, exact fresh fixture sizes/hashes, compiler hash and fresh-directory location. Source files were clean at the main measurement. |
| `suite.log`, `suite-results.json` | Fresh 23-program whole-process run, seven randomized interleaved rounds; every implementation/CPU configuration output-checked. |
| `oneoff.log` | Fresh compile-plus-execute helper, seven grouped trials per cell; all nine rows. This helper emits medians only, not raw samples. |
| `measurement-driver.log` | Driver's combined table output; same numbers as the two preceding logs. |
| `ratios.json` | Ratios and extrema recomputed from both original and fresh raw samples, including best-C-within-sampled-budget selection. |
| `controls.log`, `controls.json` | Nine-round checked/interleaved comparison of original implementations and audit-only persistent-team, packed ordinary-C, and one-pass/two-output JSON controls. Full samples, timing-command outputs and binary/source hashes retained. |
| `controls-repeat.log`, `controls-repeat.json` | Separate 15-round repeat for strip/idents/strings/json; not merged with the first batch. |
| `compiler.log`, `compiler.json` | Separate isolated 15-round checked/randomized whole-compiler comparison, current source and 64-copy large source, one/eight allowed CPUs. |
| `fsm-coverage.json` | Exact pair/offset coverage of `tests/fsm_all.py`'s input and table grouping. |
| `iotest.log` | One isolated invocation of the repaired checksum-observable I/O probe; not a statistical campaign or lower-bound assertion. |
| `hardware.log`, `probe-check.log` | Actual fresh hardware probe on CPU 3 and the current checker's success on that binary. |

Fresh input contents match all five hashes in the original results. JSON is
268,435,936 bytes (16 repeated valid documents); other fixtures are exactly
268,435,456 bytes. The suite computes throughput from the actual file size.

## Correctness and source-analysis companions

* `../audit3-carries/`: carry-state invariant/peeling argument, deterministic
  104-program/540-run probe, complete program strings and output/path records.
  Fallback detection patches a **separate generated ELF**, not the runtime.
* `../audit3-hand/`: 2,997 ordinary/OpenMP/hand/Strand comparisons, exact fixture
  manifest, 723 control checks, reproducible alternative C sources. The final
  recorded checks set **both affinity and OpenMP thread counts**, so Strand
  sees the requested CPU budget too. Expected malformed-JSON differences are
  explicitly distinguished from valid-input failures.
* `../audit3-claims/`: all twelve compiler-phase entries audited; probe checker
  false-positive controls; relevant disassembly; small semantic examples.
  These checks compile/disassemble probes but do not run hardware timings.

Audit-only C alternatives are experimental controls, not proposed installed
patches. The block-size-only control and optional sub-audit timing helper are
retained for follow-up but were **not** used in the reported campaigns.

## Reproduction

Run from the repository root after `./build.sh` or `./test.sh`.
Correctness checks can run separately from timing:

```sh
python3 reviews/audit3-carries/probe.py
python3 reviews/audit3-hand/checks.py
python3 reviews/audit3-hand/check_variants.py
python3 reviews/audit3-hand/input_manifest.py
python3 reviews/audit3-claims/check_claims.py
python3 reviews/audit3_coverage.py > reviews/audit3-2026-11-10/fsm-coverage.json
python3 reviews/audit_checks.py --output reviews/audit3-2026-11-10/old-checks.json
```

The alternatives are checked in; to regenerate them from the reviewed
production sources: `python3 reviews/audit3-hand/make_variants.py`.
`checks.py` builds original binaries; `check_variants.py` builds the controls
before testing them.

Run timings **alone and sequentially**, not alongside tests, compilation by
other jobs, browser work or another measurement campaign:

```sh
python3 reviews/audit3_measure.py
python3 reviews/audit3_controls.py > reviews/audit3-2026-11-10/controls.log
python3 reviews/audit3_controls.py --only strip,idents,strings,json --rounds 15 --label controls-repeat > reviews/audit3-2026-11-10/controls-repeat.log
python3 reviews/audit_compile.py --reps 15 --output reviews/audit3-2026-11-10/compiler.json > reviews/audit3-2026-11-10/compiler.log
python3 reviews/audit3_ratios.py > reviews/audit3-2026-11-10/ratios.json
```

`audit3_measure.py` copies the runner and oneoff helper into a fresh ignored
build directory, changing **only** the root/build path assignment. The copy's
different filename hash does not alter input contents. The original
`build/suite/results.json` is not overwritten.

Ancillary probe runs, also sequential and isolated:

```sh
gcc -O2 -pthread -o build/audit3/iotest explore/iotest.c
# Use the fresh text path recorded in environment.json:
build/audit3/iotest build/audit3/fresh-XXXXX/suite/text256-XXXXXXXX
gcc -O2 -o build/audit3/probe hw/probe.c
python3 hw/check_probe.py build/audit3/probe
taskset -c 3 build/audit3/probe
```

Large inputs and generated binaries remain under ignored `build/`; the scripts,
small deterministic cases, source hashes, results and disassembly needed to
interpret/reproduce them are retained under `reviews/`. Numeric output has not
been edited; trailing whitespace may be normalized for storage.
