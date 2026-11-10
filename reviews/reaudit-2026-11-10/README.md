# Follow-up evidence — November 10, 2026

Reviewed source revision: `17e317da99fe4a6c15ea16e08f005675a0ab8c11`.
See `AUDIT.md` for the updated verdict and investigation backlog, and
`AUDIT_FOLLOWUP.md` for interpretation and reproduction commands.

## Retained files

- `test.log`: complete default `./test.sh` result, including fixed-point hashes,
  110 hand cases per compiler stage, fuzzing, and 3,968 pool executions.
- `environment.json`: toolchain/revision, CPU availability, and SHA-256/size of
  all four freshly generated 256 MiB inputs. All four hashes also matched the
  pre-existing `build/suite` fixtures on this VM.
- `suite.log`, `suite-results.json`: complete 21-program table and all seven
  interleaved timing rounds, with successful output validation at each CPU
  configuration. One CPU is CPU 3; four CPUs are 0–3; eight are 0–7.
- `swapcase.json`, `swapcase.log`, `swap-vectorization.log`,
  `swap_arithmetic.c`: nine randomly interleaved trials per variant/CPU
  configuration, output hashes, arithmetic control source, and GCC vectorization
  report. All 256 byte values were also checked exhaustively.
- `iotest_original.*`, `iotest_observable.*`, `iotest_observable.c`: original
  and observable-checksum experiments, their `touch` disassembly, and the control
  source. Each timing table is one invocation, not a statistical campaign.
- `hardware.log`, `probe-check.log`, `probe-check-true.log`: hardware probe run
  on CPU 3, checker result for the actual probe, and its false success on
  `/bin/true`.
- `compile-latency.log`: 21 grouped whole-command runs each for compiling
  `words` and `strip` with Strand, optimized OpenMP C, and unoptimized OpenMP C.
  These measurements are separate from execution throughput.
- `checks.log`, `correctness-checks.json`: 26 targeted semantic cases, direct
  scratch-ELF fallback detection (status 77), and toy-grammar/Python-token
  examples. Normal binaries were checked against the scalar interpreter.
- `old-checks.json`, `old-checks.log`: rerun of the first audit's deterministic
  checks, showing the timer, short-read, actual-team, and signed-overflow fixes.
  Partial output on failure remains and is now explicitly permitted.

`reviews/followup_measure.py` and `reviews/followup_checks.py` generate these
experiments. Inputs and binaries remain in ignored `build/`; the retained
sources are experimental controls, not installed changes. No timing campaigns
ran concurrently with the test suite. Text-log/disassembly trailing whitespace
and trailing blank lines were normalized for storage; numbers were not edited.
