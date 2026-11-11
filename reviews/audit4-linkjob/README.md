# Audit 4 — file-job linkers

Audit-only artifacts for `examples/link.strand`, `examples/link2.strand`, and
`explore/linkjob.c`. No implementation file is edited. `explore/link.c` is a
separate audit; its prototype costs must not be confused with this file-job test.

## Scope and verdict

* The ordinal linker agrees with an independent Python byte-image oracle on all
  92 small cases and the full regenerated 2,000-unit job. This is substantially
  stronger than comparing only against the C implementation.
* The fingerprint linker agrees on the generated large job and ordinary unique
  full-64-bit fingerprints, but is **not a total resolver for arbitrary unique
  fingerprints**: five deliberately chosen *distinct* fingerprints cause exit 9.
  Equal fingerprints on different definitions can instead produce a wrong image
  with exit 0. No silent wrong image was found with unique fingerprints and
  sorted, disjoint, in-range relocation fields.
* The C reference's *algorithmic shape* is reasonable: input mmap, one import
  address table, libc unit memcpy immediately followed by unit-local patches,
  one complete output image. It does not copy its input unnecessarily and does
  not allocate one dword per code byte.
* Its **actual compiled inner loop is not an ordinary dword store** on this VM:
  default GCC fortification inserts a checked-library call plus dynamic bound
  calculation at every relocation. Same-source/no-fortify and same-flags/
  explicit-byte-store controls are essential before interpreting the claimed
  approximately 1.1× gap.

## Independent oracle and contract

`oracle.py` encodes/decodes the format in `explore/linkjob.c:4-9`, constructs each
unit's bytes and zero alignment padding, resolves `(unit, ordinal)` through
variable-sized export blocks, then patches little-endian relative dwords in
input order. It does not call the reference, Strand interpreter, or compiler.
`check_large.py` uses Python mmap views and an `array('I')` import table so the
same independent operations also work on the full job without millions of
boxed parse objects.

The **86 core cases** obey grouped exports, valid import ordinals, matching
import/export fingerprints, and sorted disjoint in-range relocation fields.
They include:

* zero units, zero-sized units, no exports/imports/relocations;
* sizes 1, 3, 4, 15, 16, 17, 63, 64, 65, 127, 128, 129, 4095, 4096, 4097;
* relocations at the last legal byte offset, unaligned fields, positive and
  negative relative displacements, distinct code patterns and zero padding;
* per-unit export counts 0/1/2/3 and randomized counts, unused exports/units;
* fingerprints 0, all ones, high bits set in either/both halves, and seeded
  full-width random 64-bit values;
* two deterministic four-round placement failures described below;
* 64 independently seeded small jobs.

Six supplemental cases are separately categorized: four sorted overlapping
relocation streams (stride 0/1/2/3, crossing 16-lane boundaries), an equal-
fingerprint collision, and a relocation stream not grouped by unit. Overlap
semantics are explicitly the input-order overwrite oracle; this is outside
`LINKING.md:127`'s **disjoint** performance workload. Both Strand programs
match this oracle on the overlap cases. This observation is not a proof of
every possible overlapping-store/ISA execution.

The C reference silently skips or misapplies ungrouped relocation streams
because `explore/linkjob.c:47-48` advances `k` only while `runit[k] == u`.
Both Strand linkers handle the supplemental case correctly. File comments
`linkjob.c:4-9` do not state the grouping restriction; `LINKING.md:127` does
describe the sorted workload. Therefore this is a contract limitation, not a
failure on the documented generated benchmark.

`cases.json` has every input/expected-image hash. `checks.json` has exit status,
output hash/length, affinity and first mismatch for all 1,840 runs (20 variants
× 92 cases). `checks-summary.json` is the short summary. Strand was tested on
one CPU and all eight with its eager parallel test mode, including tiny jobs.
Artifacts and expected/actual bytes are in `build/audit4-linkjob/cases/` and
`build/audit4-linkjob/*.actual`.

## Fingerprint findings and exact source references

### Unique fingerprints can exhaust four tables

`examples/link2.strand:33-34` chooses `Mask=15`, `Tabm=16` when `Ne=5`.
Use one 64-byte unit, five exports at offsets 0..4, five imports, and fields
at 0,4,8,12,16. Give export fingerprints:

```
low half:  0, 4096, 8192, 12288, 16384
high half: 0,    0,    0,     0,     0
```

These are distinct, all within even the generator's restricted 31-bit halves.
Every round's key at `link2.strand:42,47,52,57` is
`((ea * odd_multiplier + eb * multiplier) >> 8) & 15 == 0`.
At most one export is placed per round, so after four rounds `Lost != 0`.
The explicit check at `link2.strand:61-63` correctly exits 9; no wrong image is
silently emitted. The ordinal program, original C and scalar probing C agree
with the oracle. Removing **all imports and relocations** still makes link2
fail, because the Lost check requires every export to be placed.

This is a deterministic counterexample to unrestricted fingerprint generality,
not a collision of two fingerprints or a probabilistic estimate. The fixed
four-table procedure needs a fallback or an explicit placement-success
precondition. `fingerprint-witness.json` records all keys.

Reproduce:

```
build/audit4-linkjob/link2 < build/audit4-linkjob/cases/five_unique_four_round_collision.job
# exit 9, no output
build/audit4-linkjob/link < build/audit4-linkjob/cases/five_unique_four_round_collision.job \
  > build/audit4-linkjob/witness.out
cmp build/audit4-linkjob/witness.out \
  build/audit4-linkjob/cases/five_unique_four_round_collision.expected
```

### Equal fingerprints are not detected

Two exports at offsets 0 and 63 with the same fingerprint, imported separately
by ordinal, produce expected first relative field 0 but actual field 63.
Both link2 modes exit 0. At `link2.strand:45,50,55,60`, an import accepts the
first equal fingerprint and never changes a nonnegative `res`; no names or
ordinals are checked. Placing both equal exports in different rounds does not
identify the ambiguity. The full-image scalar fingerprint control explicitly
rejects duplicates with exit 9 instead.

This is an input-identity issue: a fingerprint-only input cannot distinguish
colliding names. It should not be described as a failure on a stipulated unique-
fingerprint domain. However `LINKING.md:104-106`'s unqualified “Same image” and
the broader name-fingerprint fallback need that qualification.

### Generator exercises 62, not 64, fingerprint bits

`explore/linkjob.c:27` sets `efp1=(uint32_t)f & 0x7fffffff` and `efp2=f >> 33`.
That leaves 31 bits per half, dropping source bits 31 and 32. General full-width
values are not tested by the original generator. The independent full-width
cases passed; **this is a coverage limitation, not a discovered high-bit bug**.

## Full-job reproducibility

Regenerating with the present `linkjob gen 2000` gives exactly the same file as
the preexisting `build/job2000` (`job-input-hashes.txt`):

| item | exact value |
|---|---:|
| units | 2,000 |
| exports | 1,000,000 |
| imports | 3,000,000 |
| relocations | **12,644,906** |
| raw code bytes | 265,608,716 |
| aligned image bytes | **265,668,544** |
| job bytes | 481,363,608 |

Input SHA-256:
`32571e91737f9ce7cb4ee43077087b46bd09f183833cdacb50bcb48e240b8588`.
Independent expected image SHA-256:
`69e3ce48007c19a0aee9a78c9aeee96ce76743e2b09344228fa4f02fe6181ba6`.

`LINKING.md:84-86` instead says 12,150,000 relocations and a 262 MB image. This
is a source/fixture accounting discrepancy, not evidence that the reported
historical timings were invented. The current fixture has about 4.1% more
relocations than the stated count. No historical input hash or raw samples are
provided in that document. `test.sh:20-25` tests only generator U=40, not U=2000.

`large-checks.json` and `large-extra-checks.json` record full-output equality
against the independent oracle, on one and eight CPUs, including all controls.
The oracle also verifies the generated fields are sorted/disjoint/in range and
the two representations of every import agree.

## C-reference strength: algorithm versus compiled shape

Algorithmically, `linkjob.c:37-48` is a sound baseline for this synthetic
ordinal job. Unlike Strand's globally separate copy and patch lines
(`link.strand:28-29`), it patches while the just-copied unit is hot. It resolves
imports without materializing a separate address for every export. Both sides
map regular-file input (`linkjob.c:37-41`; `stencils/stub.S:111-147`), and both
materialize the complete output with demand-zero memory (`linkjob.c:46`;
`link.strand:27`; `tools/mkstencils.py:261-274`). This is **not** a string-hashing
conventional linker baseline.

On this VM, exact `gcc -O2` invokes cc1 with `-D_FORTIFY_SOURCE=3`; see
`gcc-defaults.txt`. The four-byte `memcpy` in `linkjob.c:48` is a real
`__memcpy_chk` call once per relocation, with substantial dynamic object-size
arithmetic. `-O3 -march=native` alone does not eliminate it.

Audit controls:

* `ref_o2`, `ref_o3`: unchanged source, original flags / O3-native.
* `ref_o2_nf`, `ref_o3_nf`: unchanged source with explicit
  `-U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=0`; four-byte memcpy inlines.
* `ref_o2_bytes`: **same original gcc -O2 flags**, changing only the fixed
  four-byte memcpy into four explicit little-endian byte assignments.
  `make_byte_store.py` checks/reproduces that single replacement;
  `ref_bytestore.c` is the audit-only resulting source. GCC combines the four
  assignments into one unaligned dword `mov`; the unit copy remains fortified.
* `c_full_nf`: same whole-image, unit-local copy/patch shape in `variants.c`,
  O3-native/no-fortify.
* `c_checked_nf`: same as full, with an explicit per-site in-unit bounds check.
  This demonstrates that an ordinary inline check need not imply a dynamic
  object-size calculation and library call.
* `c_fingerprint_nf`: same full-image output path as `c_full_nf`, but a scalar
  64-bit hash plus linear-probing table. It compares full fingerprints,
  resolves hash-table collisions without a fixed round cap, and rejects exact
  duplicate fingerprints. It does not hash or compare name strings.
  It is a stronger alternative algorithm on the same keys, not a scalar
  translation of Strand's four scatter rounds. Its whole-process delta also
  includes hash-table construction/allocation and different memory traffic;
  it is not an isolated instruction-level probe benchmark.
* `c_parallel_nf`: resolves imports and copy/patches independent units with
  OpenMP; binary searches locate each unit's relocation subrange. It still
  materializes the complete image. It relies on the original baseline's
  grouped-by-unit precondition and preserves within-unit patch order.
* `c_stream_nf`: one reusable maximum-unit-size output buffer, emitted unit by
  unit. It has the **same stdout bytes** but does **not** materialize a complete
  in-memory image. It is a valid alternative for a streaming-output API, not
  a drop-in memory-image baseline. A `/dev/null` sink does not consume/copy the
  bytes as a real output sink would; do not generalize its win to file I/O or
  a consumer requiring the whole image.

Removing fortification or using byte assignments changes store-bound checking.
The original C still trusts all indices and offsets elsewhere; neither it nor
these controls is a hardened untrusted-job parser. `c_checked_nf` retains a
stronger per-field **unit** bound than the original output-allocation bound.
A safety-policy difference must be stated, not hidden as compiler speed.

`relocation-disassembly.txt` contains compact original/byte-store/no-fortify
loop excerpts, and full objdumps are under `build/audit4-linkjob/`.

## Output-path and parallelism qualifications

The C reference writes the existing full image directly (`linkjob.c:20,49`).
Strand's `'img out` (`link.strand:30`, `link2.strand:69`) goes through its output
buffer and flush path (`tools/mkstencils.py:226-245`,
`stencils/stub.S:200-223`). Consequently their output-side memory traffic and
write call count differ even with the same `/dev/null` sink. Do not attribute
the entire elapsed difference to SIMD relocation or join arithmetic.

Copy/dword-store lines are indeed vetoed from pool execution
(`tools/mkstencils.py:280-296` and following slice generator). A faster
whole-image unit-parallel C control tests whether the workload itself has
parallelism; it does not implement Strand's proposed runtime overlap check.
For four-byte fields, **strictly increasing starting offsets alone are not
sufficient for disjointness**: starts 0 and 1 increase but overlap. The required
condition is `next_start >= previous_start + 4`; for slices it depends on the
previous extent. `LINKING.md:127-129` should say increasing *nonoverlapping
intervals*, not merely increasing destinations.

## Reproduction and measurement protocol

From the repository root, after the shared build:

```
sh reviews/audit4-linkjob/build.sh
python3 reviews/audit4-linkjob/check.py
build/audit4-linkjob/ref_o2 gen 2000 > build/audit4-linkjob/job2000
python3 reviews/audit4-linkjob/check_large.py \
  --job build/audit4-linkjob/job2000 \
  --output reviews/audit4-linkjob/large-checks.json
```

`build.log` records compiler/binary hashes. Shared stage2:
`818d4b7202579f00f1b154741709d1fddd3c169d6f2078579e66eb63ebc0c730`.

**Only after the parent explicitly grants the isolated timing window:**

```
python3 reviews/audit4-linkjob/measure.py --authorize-isolated \
  --job build/audit4-linkjob/job2000 --rounds 7 \
  --expected-sha256 69e3ce48007c19a0aee9a78c9aeee96ce76743e2b09344228fa4f02fe6181ba6 \
  > reviews/audit4-linkjob/timings.log 2>&1
```

The driver refuses to run without the explicit authorization flag. It records
source/executable/input hashes, environment, CPU affinity, complete randomized
sample order, all wallclock samples and all wait4 rusage fields. Each of seven
rounds contains every implementation/affinity combination in a seeded random
order. Timed execution is whole child process, cached regular-file stdin,
`/dev/null` stdout. Real output streams are SHA-256 checked against the
independent oracle before **and** after the campaign.
“Eight CPUs” means the allowed affinity set, not that every implementation
spawns eight threads: only Strand's pool and the explicit OpenMP parallel
control do so. The other C controls remain single-threaded.

Timed `/dev/null` runs cannot simultaneously have a hash consumer without
changing the benchmark. Samples explicitly say
`output_hashed_during_sample=false` and reference their checked preflight hash;
the report must not claim to have hashed bytes thrown away during each sample.

## Authorized timing results

The parent granted the isolated window. The campaign ran
**2026-11-10 23:17:34–23:18:45 UTC** with other audit workloads paused.
All **168** timed samples exited zero. All **24 preflight and 24 postflight**
image hashes matched the independent oracle. No timed child had a major page
fault. The window was released immediately after completion.

`timings.json` contains every raw sample, full rusage, shuffled order and
provenance. `timings.md` contains **all 24 implementation/affinity rows**:
median wall/user/system time, complete wall-time range, median minor faults and
maximum RSS. `summarize.py` regenerates that table without executing a benchmark.

Selected results (milliseconds, seven samples per cell):

| implementation | 1 allowed CPU: median [min,max] | 8 allowed CPUs: median [min,max] |
|---|---:|---:|
| unchanged C, original `gcc -O2` | 267.54 [259.73,302.09] | 269.10 [260.65,281.66] |
| source-only byte-store C, **same `gcc -O2`** | **233.38 [217.44,256.82]** | 234.08 [224.13,247.10] |
| unchanged C, O2/no-fortify | 229.65 [222.58,235.44] | 232.63 [222.24,242.45] |
| full-image C with explicit per-field bounds | **230.67 [228.23,252.60]** | 239.06 [222.56,285.89] |
| full-image unit-parallel C | 231.10 [227.87,253.02] | **83.98 [75.74,92.08]** |
| bounded-buffer streaming C | 90.13 [86.94,106.65] | 89.20 [86.58,93.91] |
| full-image scalar fingerprint C | 352.84 [338.73,356.29] | 341.14 [325.76,355.97] |
| ordinal Strand | **294.02 [288.75,314.57]** | **286.88 [274.47,306.72]** |
| fingerprint Strand | **420.90 [394.92,441.62]** | **330.72 [327.67,347.81]** |

Interpretation:

1. **The reported 1.1× gap reproduces against the actual original build**
   (`294.02 / 267.54 = 1.099`). Its C median is inside `LINKING.md`'s 264–268 ms,
   and ordinal Strand's one-CPU median is inside 294–300 ms. The original
   measurement is plausible, but not evidence of closeness to a stronger
   ordinary scalar loop: changing only the four-byte store spelling at the
   **same original compiler flags** yields 233.38 ms, making Strand **1.260×**
   slower. Original-source O2/no-fortify gives 229.65 ms (Strand about 1.28×
   slower). O3 alone does not fix the per-relocation fortify call.
2. This is not merely an argument to remove every bounds check:
   `c_checked_nf` explicitly tests
   `size[u] >= 4 && roff[k] <= size[u]-4` **for each field**, and its full-image
   result is 230.67 ms. The no-fortify flag prevents the *additional* generic
   fortify wrapper, not that explicit branch. The original and Strand perform
   different checks overall, so this remains a valid-input performance control,
   not a claim of equal comprehensive safety policies. Do not infer that the
   checked variant is intrinsically faster than unchecked from seven noisy
   whole-process medians.
3. Original C's median user/system times are **117.04/161.05 ms**; same-O2
   byte-store C is **76.28/159.77 ms** with essentially identical **74,939**
   minor faults and **711,480 KiB** maximum RSS. This supports locating the
   improvement in compiled user-space patch work, not an mmap/output-API trick.
   Ordinal Strand is **163.82/135.80 ms**, **75,929** faults, **714,196 KiB**.
   RSS includes touched input mappings, not just the output image. User/system/
   wall columns are independently computed medians and need not sum.
4. **Limited ordinal Strand scaling reproduces**: 1.025× from one to eight
   allowed CPUs. Same-algorithm unit-parallel C scales **2.752×**
   (`231.10 / 83.98`), or 2.922× against `c_full_nf`'s 245.42 ms serial path.
   At eight CPUs its user/system totals are **187.75/247.49 ms**, **74,990**
   faults and **711,464 KiB** RSS: it retains the large image and page-fault work,
   rather than achieving speed by omitting output. The workload has usable
   unit-level parallelism, though these are not claims about arbitrary linker
   jobs or a general overlap-proof algorithm.
5. **Streaming is a different materialization contract**: 90.13 ms with
   **51.78/36.00 ms** user/system, **10,141** faults and **452,832 KiB** RSS.
   The large anonymous image is absent, while the input mapping remains.
   This shows the importance of image materialization/page faults under the
   stdout-to-`/dev/null` interface; it is not a 3× SIMD/compiler comparison.
6. Fingerprint overhead is real but not cleanly attributable to compiler
   overhead. Strand fingerprint minus ordinal is **126.89 ms** (median
   within-round difference **127.37 ms**); matched C fingerprint minus full C
   is **107.42 ms** (paired median **102.45 ms**). These are alternative hash
   algorithms with different traffic/allocation, not isolated phase timers.
   At eight CPUs Strand's fingerprint-minus-ordinal difference is **43.84 ms**,
   and fingerprint Strand's one/eight speedup is **1.273×**.
7. Current fingerprint medians **420.90/330.72 ms** do not reproduce the
   document's **397–407/304–315 ms** bands. Ordinal eight-CPU median **286.88 ms**
   is also just above the stated 279–285 ms band. The current fixture's
   relocation count and image size differ from the document, and historical
   raw samples/build/input hashes were not supplied. This is a measured
   reproduction limitation, not proof that the historical ranges were false.
