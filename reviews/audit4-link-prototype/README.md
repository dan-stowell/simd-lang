# Audit 4: `LINKING.md` / `explore/link.c`

Reviewed at **b591f69**, since **af3ecc9**. All production sources are untouched.
Generated sources/binaries/disassembly live in `build/audit4-link-prototype/`.

## State and reproducibility

**Authorized isolated timing campaign complete.** See `RESULTS.md` for conclusions,
`timing-summary.md` for all control summaries, and JSON for every raw sample.
The run comprised 42 control trials (two binaries × three regimes × seven repetitions)
and 21 randomized legacy/source-only trials, pinned to CPU 0, with other audit CPU
work paused. Timing completed successfully in 6m56s; the window was then released.
The check mode does not call a clock; its JSON `*_ms` fields are zero, not measurements.

```sh
python3 reviews/audit4-link-prototype/run.py prepare
```

That extracts the original RNG/hash helpers from the pinned commit, builds the original
prototype and controls with GCC `-O3 -march=native`, builds ASan+UBSan and auto-vectorization
disabled controls, disassembles the optimized binaries, and checks outputs.
`environment.json`, `correctness.json`, and `original-correctness.json` record the evidence.

To repeat, only after the parent grants another isolated timing window:

```sh
python3 reviews/audit4-link-prototype/run.py measure \
  --timing-authorized --repeats 7 --legacy --cpu 0
python3 reviews/audit4-link-prototype/summarize.py
```

The driver serializes all runs, pins one CPU, checks child status, and checks legacy
`same result`/`same bytes`/zero-unresolved output (the legacy program itself exits zero
on disagreement). It writes raw trial records to `timings.json` and
`legacy-timings.json`.
The summarizer reads existing records only; it emits median/min/max stage costs,
faults and within-trial paired ratios in `summary.json` / `timing-summary.md`.
`environment.json` records every exact compile command, GCC's expanded driver
invocation (including its default `_FORTIFY_SOURCE=3`), and all generated source and
binary hashes. Source-only variants retain the same optimization/hardening flags.

## Findings before timing

The sections below record the source-level hypotheses. The authorized run confirmed
some and narrowed others. In particular, **RESULTS.md establishes an additional
optimizer/fortification confound**: the original per-site four-byte `memcpy` becomes
a `__memcpy_chk` call; explicit byte stores reverse the original relocation verdict.
The original variable-size hash-tail copy also becomes a call; rewriting it as safe
constant-size 4/2/1-byte loads materially reduces the exact-string join time.

### 1. The proposed reshaping is real; its numerical attribution is not isolated

The headline `95% ... strings` at **LINKING.md:36–39** is the subtraction of two
different algorithms' total costs, not an experiment isolating string hashing/comparison.
In **explore/link.c:51–58**, an exact hit requires the hash array, the symbol-index
array, the offset array (`noff[id]` and `noff[id+1]`), then the random definition bytes.
The fingerprint path at **63–69** omits hashing, those offset loads, and byte comparison.
Their relative cost includes pointer dependencies, table/string locality and page
state, not merely character processing.

The standalone hash scan at **60–61** is useful, but runs later and sequentially.
It does not measure hash time *within* the interleaved hash/probe/compare workload.
Subtracting that scan from the name total cannot assign the remainder to `memcmp`.
The 260–360 ns and 12–13 ns denominators combine **1M insertions and 3M lookups**;
they are not direct single-lookup or single-probe measurements.

Three table layouts and four matching/hash policies below separate these questions.
Every `_exact` control still checks length and `memcmp`, including prehashed controls.
An improvement from prehashing is work moved into the producer, not free end-to-end
hashing. The source's compiler-precomputed-hash assumption is explicit and legitimate
for its proposed format; it does not by itself demonstrate a fair same-format speedup.

### 2. “Twice scalar” does not isolate SIMD; “fundamental” is unsupported

**LINKING.md:27–34** generalizes from the comparison in **explore/link.c:66–87**.
The scalar algorithm uses linear probing with hashes *in* the table. The other:

* uses four different tables and different multiplicative slot hashes;
* initializes 32 MiB of ID slots, versus 8 MiB of IDs in the scalar setup;
* constructs them with **scalar**, conditional stores (**75–76**), not SIMD scatter;
* performs a table-ID gather followed by a dependent fingerprint gather (**81–84**);
* includes construction, allocation of `placed`, initialization, and lookup in one time;
* contains conditional rounds/early-exit masks, so “branch-free” is also imprecise.

At the actual `M=2,097,152`, the linear ID+hash arrays occupy **24 MiB**; the race
ID tables plus definition fingerprints occupy about **39.6 MiB**. The VM reports
32 MiB L3. Thus “a table too big for the cache” is not established by the scalar
table footprint alone; effective shared-cache occupancy is a separate question.
This is not a cache-miss or memory-level-parallelism measurement.

A slower implementation of a different algorithm is evidence about that implementation,
not evidence that vector lookup cannot help or that all OoO cores already saturate
available misses. The audit provides identical four-table scalar and explicit-gather
lookups and reports build and lookup separately. Neither is assumed to win.

### 3. The relocation baseline leaves a very ordinary locality optimization out

**explore/link.c:102–105** copies the **entire 250 MiB image** before patching any
unit. **111–123** compares this with precomputed-mask expansion. Copying/patching each
128 KiB unit, or each 16 KiB block, keeps recently copied bytes much closer in the
cache hierarchy. No new object format or SIMD is required. Indeed the later conventional
C reference already does unit-local copy/patch (**explore/linkjob.c:46–48**).

The audit therefore checks whole-image, unit-local and 16 KiB block copy+patch against
both scalar-values+expand and SIMD-values+expand. Block-boundary-spanning relocations
are patched only after their last byte's block has been copied.

“Nearly a stream” is an appropriate description of the **chosen sorted, disjoint,
four-byte relocation model**. It is not proof that `vpexpandb` is needed for good
locality or that the reported 1.25x survives a unit-local conventional control.
The expansion kernel still has a separate **48 MB value buffer** and a **32.8 MB
hole mask** at this geometry. Mask construction at **107–109** is outside timing,
explicitly charged to the compiler in this design.

Also, `tg` at **120** times **all of 111–119**: gathers, arithmetic, temporary lanes
and scalar duplicate cleanup. It does not independently measure the “37 [ms] ...
scalar clean-up” attribution in **LINKING.md:20**.

### 4. Fixed ordering and unequal fault/cache state contaminate attribution

The prototype has one fixed order, with no warmups or repetitions inside `main`:
names → hash scan → fingerprints → four tables → layout → imports → relocations.
The first name join writes freshly allocated `tab`, `th`, and `res` (**43,51–59**).
The scalar fingerprint join reuses the first two but first-writes `res2` (**63–69**).
The race allocates a fresh 32 MiB table and `placed` (**71–73**).
Layout/import arrays also first-touch inside their clocks (**92–100**).

The two image outputs are explicitly prefaulted at **41**, whereas the expansion
value buffer first-touches inside the values phase (**107,110–120**). These biases
do **not** all favor SIMD. They do prevent simple causal subtraction of stage totals.

The GCC 13.3 optimized disassembly preserves both output-initialization `memset`
calls (original addresses `0x1527`, `0x153d`) and the relevant comparison/hash work.
There is **no demonstrated dead-store-elimination failure** behind the reported
original numbers. Our controls use noinline kernels, compiler memory barriers,
full output validation, visible checksums, and explicit prefault/fresh-output regimes.

The source loop **55**, `i++, s += ulen[i - 1]`, is **not** an off-by-one typo:
the comma operator sequences the increment before the indexing expression.

### 5. Fingerprints are not exact names

The original exact path checks full name bytes (**57**); the fingerprint path
accepts equality of a 64-bit value alone (**68,84**). No collision fallback exists.
The four-round construction also has no fallback for unplaced definitions (**74–87**).
The fixed generated job succeeds, but that cannot establish equivalence for all inputs.

The audit deliberately truncates fingerprints to 8 bits on a small fixture to
exercise the equality policy. All six exact variants return all **579/579** correct
answers; linear unchecked variants return **295 wrong**; both race lookups return
the identical **280 wrong**, with **21 unplaced definitions**. This is a forced
collision demonstration, **not an estimate of natural 64-bit collision frequency**.
The original “same result” comparison is only evidence on its generated collision-free
input; treating unchecked fingerprints as exact strings is a real semantic relaxation.

Known-successful inputs are also part of the original contract: the name and
fingerprint loops have no empty-slot termination. Missing names can walk uninitialized
hash slots, and the name path can dereference `noff[-1]` if an empty slot's hash matches.
This is a robustness limit, **not a demonstrated UB execution on the stated fixture**.
Audit controls terminate safely on an empty slot and check a missing-name fixture.

### 6. Original checks are weaker than they appear, but fixed workload bytes pass

The original returns zero even if it prints `MISMATCH` (**69,123–124**). Its SIMD
join loop **79** and comparison **86** both ignore reference counts not divisible
by eight. On a 579-reference fixture the original prints zero unresolved references,
but an independent final check finds **three wrong trailing entries**. The real
3,000,000-reference job is divisible by eight, so this is **not** a flaw in that
specific benchmark result. Audit controls implement and check the tail.

With clocks disabled and a strengthened final exit status, the **unmodified original
kernels** pass the full-size case: exact names match generated symbol IDs, race
results match them, and both images match. Controls are separately checked against
the generator's ground-truth reference IDs and an independent byte-wise relocation
oracle, not merely against one another.

### 7. Scope of “80 ms vs 1.1–1.5 s”

**LINKING.md:62–64** adds selected, differently warmed microbenchmark stages with
compiler-precomputed identities/masks. It excludes producer work, file I/O,
allocation/prefaulting of the output, and most linker features. **LINKING.md:3–9,
74–80** explicitly discloses the synthetic model and omitted linker features:
those are supported scope limits, not concealed implementation defects.

The architectural point remains valid: explicitly resolved `(unit,ordinal)` references
can remove a linker name join in a language/toolchain designed for that contract.
It is not an automatic semantics-preserving replacement for every C/C++ symbol
resolution workflow, especially the expressly excluded archive/weak/COMDAT cases.

The later **LINKING.md:108–136** distinguishes actual process timings and admits
Strand loses to conventional C. Those numbers are a **different workload and timing
boundary**, not corroboration that an 80 ms production linker beats a 1.1 s one.
The current audit can establish the core costs only after controlled timing;
it cannot establish whole-system competitiveness from this prototype.

## Control matrix

`controls.c` preserves the original full-size workload seed, hash, string lengths,
reference popularity, code bytes, relocation sites and values. It uses independent
reference-ID and byte-wise image oracles.

| Group | What changes | What stays the same |
|---|---|---|
| `soa_*` | original separated ID/hash/offset arrays | load factor, hash, inputs |
| `aos24_*` | `{hash, string pointer, length, id}` slots | exact-byte policy when `_exact` |
| `aos16_*` | `{hash, length, id}` slots; offset for byte address | exact-byte policy when `_exact` |
| `*_hash_exact` / `*_pre_exact` | link-time hash versus producer-computed hash | full length+byte verification |
| `*_hash_fp` / `*_pre_fp` | hash work retained/removed | deliberately unchecked FP semantics |
| `race_scalar` / `race_simd` | lookup instruction form only | same four tables and scalar construction |
| `copy_whole_patch` / `copy_unit_patch` / `copy_block_patch` | copy/patch locality | exact relocation semantics, ordinary `memcpy` |
| `stream_scalar_values` / `stream_simd_values` | value-generation loop only | same mask, value order, expansion kernel |

All linear kernels are compile-time specialized: there is no per-probe mode dispatch.
The 24-byte and 16-byte tables consume 48 and 32 MiB, respectively; a shorter pointer
chain costs a bigger table. Their initialization cost is reported in build time,
not concealed. These controls are reasonable conventional alternatives, not claims
that these are the best possible hash tables or production linkers.

Each authorized trial randomizes variant order independently of the workload seed.
Three regimes are kept separate:

1. Prefaulted allocations, no explicit eviction (“warm-prefaulted” means *resident*,
   not a claim that every working set fits cache).
2. A 256 MiB eviction sweep before each complete join/relocation kernel, prefaulted
   output. The join build naturally warms its own tables before lookup.
3. The same eviction regime with freshly discarded anonymous **image output** pages
   before every relocation variant. Other allocations remain prefaulted.

Fault counts cover each measured stage. Checksums and full correctness checks happen
outside clocks, and their cache effects are controlled by the next eviction sweep
in the evicted regimes. The driver does not add subprocess launch/job-generation/
oracle costs to kernel times; it must not be read as an end-to-end benchmark.

## Correctness results

* Full workload: 2,000 units; 1,000,000 definitions; 3,000,000 references;
  12,000,000 relocations; 262,144,000 output bytes.
* Names: 55,969,279 definition bytes; 168,095,629 reference bytes.
* Full relocation sites: **12,000,000 unique, zero clamp duplicates**;
  563,495 sites cross a 64-byte boundary. Clamp cleanup exists in the original
  but is not needed for this particular seed. Small fixtures deliberately exercise it.
* All fourteen full-size join variants: zero wrong/unplaced; result checksum
  `a169d4f7f73457fc`.
* All five full-size relocation variants: byte-exact equality; checksum
  `36dde3f333a29352`.
* Small fixture: 579 references (SIMD tail), 771 relocations, 590 unique sites,
  181 duplicate-clamp entries and 32 cacheline-crossing sites.
* ASan+UBSan: small, missing, collision cases all pass (intentional whole-process
  allocation lifetimes; leak detection disabled).
* The source-only fixed-size hash-tail variant preserves the full hash checksum
  `f14f8afd64be03f2`, all join outputs and image bytes; full and ASan/UBSan small
  cases pass. The source-only original store/store+tail variants also pass.
* Auto-vectorization disabled build: small fixture passes; explicit intrinsics
  remain explicit intrinsics.
* Original full fixture passes; original non-multiple-of-eight fixture fails its
  added independent tail check as expected.
