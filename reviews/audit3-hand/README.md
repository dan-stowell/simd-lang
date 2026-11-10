# Hand/C/JSON audit of af3ecc9 — correctness and controls, not fresh timings

No compiler, runtime, or installed benchmark implementation was changed. No commits.
Generated binaries/large files are in `build/audit3-hand/`. Sources here are controls only.
The parent subsequently timed the controls with `reviews/audit3_controls.py`;
see `AUDIT_3.md` and `reviews/audit3-2026-11-10/controls*.json`.
Final correctness records were rerun with explicit CPU affinity as well as
`OMP_NUM_THREADS`, so Strand also receives the requested 1/4/8-CPU budget.

## Results

`python3 reviews/audit3-hand/checks.py` made **2,997** output checks (actual processes,
not timing measurements):

* **1,017 direct parsed-tree oracle checks** of valid JSON (OpenMP C, hand C, Strand;
  1/4/8 threads), including 20 generator seeds and long backslash runs around
  64-byte, worker partition, and 4 MiB boundaries. All passed.
* **1,746** checks of the other seven hand kernels, including random bytes, empty/
  short/tail inputs, and long comments/strings across 4 MiB boundaries. All passed.
* **234** JSON checks on arbitrary, generally malformed random byte strings. All
  ordinary/OpenMP/Strand outputs agreed; hand differed in 18 checks (six inputs,
  three CPU budgets). These are NOT valid-JSON counterexamples.

`checks.json` retains failures, expected/actual prefixes and lengths, and revision;
`input-manifest.json` records each input hash, byte length and task/oracle;
inputs are deterministically reconstructed by `checks.py` (seed 819), failed inputs
also retained under `build/audit3-hand/`. `variants-checks.json` records **723**
additional successful checks of controls plus exhaustive xlat byte coverage.

## Judgments with exact source references

1. **Real expert techniques, not just auto-vectorized strawmen.** `hand.c:46–67`
   genuinely scans transition functions, `:114/:128/:148/:206` use SIMD compression,
   `:99–101` use VBMI arbitrary byte permutation, and `:78–85/:219–222` use escape
   arithmetic and CLMUL parity. No valid-input correctness defect found here.

2. **Ordinary C automata are already branch/switch syntax**, not per-byte transition
   table lookups: `base.c:88–107`, `base_omp.c:44–45,173–180,194–201,210–223`.
   `ISW` is just identifier byte classification. Thus the specific accusation that
   the ordinary DFA is artificially slow because it uses a state-table recurrence
   is unsupported by the current source. The multithreaded summary does evaluate
   four scalar starts per byte, and then rereads for output; this is real overhead,
   not an inevitable upper bound on ordinary C (`base_omp.c:164–180`).

3. **Structural asymmetries remain, but their cost must be measured.** All output
   kernels in hand C enter/end an OpenMP parallel region for each 4 MiB block
   (`hand.c:30,97–103,107–117,121–131,136–153,187–211`). At 256 MiB, that is 64
   regions, with work completion/output serialized per block; strip/JSON have
   summary barriers plus single prefix propagation before output. This does not
   mean 64 OS thread creations: libgomp can reuse workers. The hand buffers are
   `MT * (4 MiB+64)` for filters, and every worker is allocated full block capacity
   despite getting only a fraction (`:105–106,119–120,134–135,185–186`). Input is
   reread in full for FSM summaries when T>1 (`:141/:146,:159/:166`) and JSON
   (`:193–202`). Plain C uses 64 KiB read/write buffering (`base.c:8–9,41,111`),
   mmap C uses 4 MiB blocks. These whole-process IO/runtime differences belong in
   interpretation, not attributed solely to compiler code generation.

4. **JSON is structural punctuation extraction, not parsing or a complete scanner.**
   `programs.txt:24`, `base.c:104–107`, and `hand.c:202–208` neither validate syntax,
   nesting, UTF-8, escape validity, literals nor numbers; they output just six
   punctuation types, not token positions/values. `SWEETSPOT.md:244` saying a
   "complete scanner for a real grammar" overstates it. The algorithm is a useful
   real JSON stage, and can be exact on valid input, but is not simdjson end-to-end.

5. **Independent oracle is sound for the generated unique-key fixture, not all
   JSON.** `jsongen.py:22–25` makes unique keys, `:32–47` walks `json.loads`' dict.
   Duplicate names collapse: valid syntax `{"x":[],"x":{}}` gives oracle `{:{}}`,
   while every DFA correctly emits `{:[],:{}}`. This is an oracle coverage gap,
   not a timed-fixture output failure. Use `object_pairs_hook` preserving ordered
   key/value pairs if expanding correctness claims to arbitrary objects. Also
   `run.py:44–50` repeats complete documents above 16 MiB: 256 MiB is 16 adjacent
   arrays, not one valid JSON document (`json.loads(unit*2)` raises Extra data).
   A valid-document stream remains a fine extraction workload; label it honestly.

6. **Hand JSON has narrower malformed-input semantics than the stated DFA.**
   `hand.c:84` suppresses backslashed quotes regardless of being inside a string;
   the DFA ignores backslash outside strings. Minimal reproducer:
   `b'\\"{' + b' '*61` (64 bytes): hand outputs `{`; plain/OpenMP/Strand output
   nothing. At 63 bytes hand falls through scalar code and agrees. Reproduce with
   binaries under `build/audit3-hand/`, input `malformed-json64`. Valid JSON forbids
   those outside-string backslashes; **do not call this a valid-input bug**.

7. **xlat task is now genuinely arbitrary byte translation.** Exhaustively checked
   all 256 values against Python `cp037` decoding / Latin-1 encoding, for base,
   OpenMP, hand, Strand and persistent control, each with 1/4/8 threads. All agree.
   `xlat.h` and `programs.txt:11` no longer ask only arithmetic ASCII swapcase.
   Text fixture is still principally ordinary ASCII; another byte distribution
   would broaden coverage but does not make the current arbitrary table fake.

## Prepared sources (not installed; speed unmeasured here)

Regenerate with `python3 reviews/audit3-hand/make_variants.py`.

* `hand_persistent.c`: for **xlat, letters, filter4, strip, json**, move the team
  outside the block loop, retaining same 4 MiB blocks, partitioning, math, buffers,
  ordered writes. Explicit completion/single barriers replace region boundaries.
  Other cases are unchanged controls. Tests passed short, random and >4 MiB inputs.
* `hand_blocksize.c`: identical original code with `AUDIT_BLK` override; compare
  `-DAUDIT_BLK='(64<<20)'` to distinguish team frequency / block size from algorithm.
* `ordinary_packed.c`: **strip, idents, strings** only; packed transition byte +
  variable shift rather than branch/switch. Same four-state semantics, two passes,
  partition/buffering/IO. This might be faster or slower; it is a source-shape
  robustness control, not an asserted superior baseline.
* `hand_json_dual.c`: task-level **one-pass** alternative. While starting quote
  state is unknown, each worker compresses both outside and inside punctuation
  alternatives, records quote flip, then host selects the correct buffer using
  prefix XOR of worker flips. Avoids full summary reread at the cost of two
  compress/store streams and doubled buffer capacity. Same original block/parallel
  organization; valid-JSON precondition. All 408 JSON control checks passed
  (20 seeds + backslash/boundary cases, 1/4/8 threads, two JSON variants together).

Build syntax: `gcc -O3 -march=native -fopenmp -DK=P_json -o build/audit3-hand/dual_json reviews/audit3-hand/hand_json_dual.c`
(similarly select other source and K). Correctness-only builds already exist.

### Timing later, serialized with all other machine work

`python3 reviews/audit3-hand/measure_controls.py --rounds 7`

This **is a timing campaign**; it has NOT been run by this subagent. It finds
existing 256 MiB fixtures matching hashes in claimed-results.json, verifies every
executable/configuration output, random-interleaves whole-process trials, records
raw samples, hashes, actual masks and source hashes to `control-timings.json`, and
prints median input GB/s. It includes original hand/OpenMP/Strand, persistent,
64 MiB block controls, packed ordinary FSMs and one-pass JSON where applicable.
It uses the same tools/bench.c wall-time boundary, output /dev/null. No claimed
speedup should be assigned to any prepared control until those samples exist.

## External primary-source cross-checks

* OpenMP 5.2, section 10.1 (`parallel` construct): team semantics and end barrier;
  does NOT require new OS threads on each encounter.
* simdjson `src/generic/stage1/json_string_scanner.h`, lines 53–70 (upstream checked
  November 10, 2026): same quote-prefix XOR and carried in-string bit; explicitly
  leaves outside-quote backslash rejection for stage 2.
* RFC 8259 section 4: object names SHOULD be unique, not a syntactic MUST;
  implementations differ when duplicates occur.
