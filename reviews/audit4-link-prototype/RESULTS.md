# Authorized prototype results

Baseline: **b591f69**. Seven trials per binary/condition, one CPU (0), separate
serialized processes, randomized variant/trial ordering. Parent granted an isolated
VM execution window; no other audit CPU work overlapped. The campaign succeeded
in 6m56s, and the window was released immediately after summarization. This is still
one VM/CPU and one synthetic workload, not a production-linker result.

All performance outputs passed their correctness checks. `timings.json` contains
42 complete control-process records; `legacy-timings.json` contains 21 original/
source-only records. `environment.json` has exact commands, compiler driver trace,
source SHA-256s and binary SHA-256s. `summary.json` and `timing-summary.md` include
every condition's median/min/max and matched-trial ratios. No timings from correctness
runs were used.

## Bottom line

* **The original numbers are reproducible in broad scale**, but the relocation
  win disappears under a small source-only conventional improvement with identical
  compiler flags.
* **“95% strings” is not a defensible causal decomposition.** Strings and their
  metadata really are costly, but the baseline contains fortification/optimization
  costs and serial dependencies, and the unchecked alternative relaxes correctness.
* **SIMD really loses for these four-table lookups on this CPU**, including a
  same-table control. That supports the narrow observation, not a universal
  “fundamental” limit on SIMD joins.
* **The streaming relocation representation works and is byte-correct.** It is not
  generally faster than an ordinary copy/patch loop once code generation and
  locality are controlled; prefaulting also changes the ranking.
* **80 ms versus 1.1–1.5 s remains a proposed-format, selected-kernel comparison**,
  not an end-to-end linker comparison. The source does disclose this scope.

## 1. Exact reproduction and a source-only relocation reversal

Milliseconds, median [min, max] over seven trials:

| Original-shaped binary | exact-name join | hash-only scan | conventional relocation | expand relocation |
|---|---:|---:|---:|---:|
| original | 1118.2 [1055.4, 1247.7] | 84.2 [83.0, 85.3] | 78.3 [77.5, 90.5] | 63.7 [62.6, 71.6] |
| explicit 4-byte stores | 1110.4 [1006.0, 1186.2] | 83.1 [82.8, 89.1] | **50.8 [50.3, 52.5]** | 63.2 [61.7, 66.8] |
| explicit stores + fixed-size hash tail | **658.9 [617.2, 777.1]** | 82.1 [80.2, 89.9] | 52.0 [49.7, 53.1] | 64.0 [62.3, 75.2] |

The original fingerprint join median was 54.1 ms [47.4, 60.1], and its race/gather
join was 130.4 ms [106.9, 183.3]. Thus the documented general magnitude of the
name/fingerprint gap and the original 78/63 ms relocation pair reproduce, although
the race is noisier/slower than the document's 93–103 ms range.

**Actual code-generation flaw in the baseline:** with this machine's default GCC
`_FORTIFY_SOURCE=3`, **explore/link.c:104** emits a **`__memcpy_chk` function call
per four-byte relocation**. The original saved assembly contains the call at
`0x2944`. Replacing only that operation with four explicit unsigned-byte stores
generates a direct dword store, retaining valid-input semantics, alignment safety,
and every optimization/hardening flag.

This reduces the ordinary baseline from 78.3 to 50.8 ms (ratio of medians about
1.54x faster); **the 63.2 ms streaming implementation is then about 1.24x slower**.
No different file format, SIMD, table algorithm, unit ordering or input is required.
The original’s 1.25x claimed advantage in **LINKING.md:41–45** is therefore **not
robust to a reasonable conventional control**. Do not attribute all of the
original difference to a mandatory second memory pass.

The audit's separately factored `patch_unit` already compiles its constant-sized
`memcpy` to a direct dword store (`controls.asm:0x4282`) with those same flags.
Its block loop does likewise (`0x5293`). It does not inherit the original per-site
call handicap. The source-only original variant is useful because it isolates the
effect without also changing the loop/function organization.

## 2. Hash code generation changes the join, not just its hash-only cost

**explore/link.c:27** also emits `__memcpy_chk` for its final 1–7 bytes of a name
(e.g. original assembly `0x2d87`; control hash kernels have analogous calls).
The new tail uses constant-size 4-byte and 2-byte `memcpy` loads and, if needed,
one byte load, composing **the identical little-endian 64-bit word**. It neither
reads past the string nor changes the hash. The complete hash checksum remains
`f14f8afd64be03f2`; all reference IDs and image bytes match.

The “stores + fixed-size hash tail” row changes the name join from roughly **1.11 s
to 0.66 s**, while the separate sequential hash scan stays around **82–84 ms**.
That is strong evidence against interpreting the standalone scan as an additive
measurement of the hash cost *inside* the interleaved random-probe/string workload.
The rewrite changes available compiler optimization and scheduling as well as
removing calls; we did not measure hardware events and do **not** assign the entire
difference to a specific microarchitectural mechanism.

This is a source-only control, not “turn hardening off.” `controls-directtail`
selects this alternate source body at compile time. Its disassembly has **no**
`__memcpy_chk` calls; `original-store-tail` retains only the bulk reference-string
copy during untimed input generation.

## 3. Strings still cost real time; exact and unchecked are different jobs

Evicted-prefaulted **direct-tail** controls, median total ms:

| Layout | hash at link, exact bytes | prehashed, exact bytes | prehashed, unchecked |
|---|---:|---:|---:|
| separated hash/ID/offset arrays | 648.4 | 276.7 | 48.1 |
| 24-byte `{hash,pointer,length,id}` slots | **622.1** | **250.2** | 61.9 |
| 16-byte `{hash,length,id}` slots | 679.9 | 303.1 | 49.2 |

The original-tail version of the same SoA exact control is 1173.2 ms; the
pointer+length-slot exact control is 1047.9 ms. Better metadata locality provides a
modest improvement; it does **not** eliminate the cost of checking the remote bytes.
Widening the table can also hurt, as the 16-byte result shows.

With both sides prehashed, exact-byte lookup remains materially more expensive:
for direct-tail SoA, 266.2 versus 37.7 ms lookup, and for pointer+length slots,
230.5 versus 42.2 ms. The matched-trial lookup ratios are respectively **7.20x
[4.70, 9.75]** and **5.66x [4.95, 6.02]**. Exact comparison plus its metadata/locality
cost is important; calling the whole original 95% gap “strings” conceals the
different work, code generation and memory dependencies.

Compiler-precomputed hashes are a meaningful format option but move work upstream.
Exact prehash controls retain every length and byte check; fingerprint-only
controls do not. The deliberately collision-heavy correctness case demonstrates
why they are not semantically interchangeable (README, section 5).

## 4. The same-table SIMD control confirms a narrow SIMD loss

Evicted-prefaulted direct-tail controls:

| Four-table algorithm | build ms median | lookup ms median | total ms median |
|---|---:|---:|---:|
| scalar lookup | 17.45 | **37.88** | **55.84** |
| explicit SIMD gathers | 17.61 | **105.50** | **123.65** |

These use the same table construction, same hashes, same definition-fingerprint
array and same answer semantics. Every answer agrees, including tails. Matched-trial
SIMD/scalar lookup ratio is **2.68x [2.23, 3.27]**; total ratio is **2.21x [1.81, 2.62]**.
The original-tail control has the same conclusion (lookup paired ratio 2.46x
[2.30, 4.07]). The SIMD result is therefore **not explained away** solely by the
original's different table algorithm or setup cost.

This supports “our gather implementation is slower on this CPU/workload.” It does
not establish that alternative vector layouts/probing strategies cannot win or
that SIMD joins are fundamentally impossible. Scalar construction remains scalar;
the comparison still does not time an actual vector scatter-construction kernel.

## 5. Relocation ranking depends on implementation and page state

Evicted direct-tail controls, median ms:

| Implementation | prefaulted output | fresh output |
|---|---:|---:|
| whole-image copy, then direct patch | 47.63 | **137.37** |
| unit-local copy/direct patch | **41.47** | 162.89 |
| 16 KiB block copy/direct patch | 46.07 | 166.29 |
| scalar values + byte expansion | **38.79** | 142.74 |
| SIMD values + byte expansion | 45.59 | 145.38 |

All five produce every byte of the same `36dde3f333a29352`-checksum image.
Prefaulted stage fault medians are zero. Fresh-output stages have **64,000 minor
faults each**, as expected for the 262,144,000-byte output at 4 KiB per page.

The unit-local conventional control beats the original SIMD values+expand strategy
on prefaulted output: the matched SIMDstream/unit ratio is **1.068 [1.007, 1.211]**.
The source-level SIMD value-production loop itself is slower than scalar
value-production: matched ratio **1.420 [1.299, 1.664]**. The scalar-values stream is
competitive and usually fastest in the prefaulted case; it still relies on the
compiler-provided mask/extra values stream.

Fresh-output timing changes the ranking: large whole-image copy/direct patch wins
here, and unit/block locality does not overcome first-touch/copy behavior. There
is no universal “unit-local is always faster” claim. **Do not mix prefaulted
microkernel results with process startup/first-touch timings**, and do not infer
one fixed relocation speedup from just one page regime.

The documentation's `tg` measurement is the whole value-production loop, not an
isolated scalar-cleanup measurement. These scalar/SIMD value controls directly
address that ambiguity.

## 6. What remains of the architectural argument

The values, masks, ordered copy/patch sites and ordinal identities can express
this toy linker succinctly and correctly. Removing an actual name join in a format
whose producer already knows stable identities is an authentic architectural
tradeoff, and the prototype makes that concrete.

But the measured evidence supports a narrower account than the most emphatic prose:

* the original 1.1 s exact-name baseline is not the best reasonable implementation
  even with identical flags and exact-byte semantics;
* unchecked fingerprints are an approximate identity scheme without fallback;
* the original conventional relocation baseline pays avoidable per-site calls;
* prefaulted stage sums do not include all allocation/output and producer costs;
* general production linker features and I/O are explicitly outside this experiment.

The later whole-process Strand/C comparison has a different workload and includes
different costs. Its candid statement that C is faster is not contradicted by
these kernel controls. Neither those process numbers nor this prototype establish
an “80 ms production linker versus 1.1 s conventional linker.”
