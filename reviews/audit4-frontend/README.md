# Audit 4: frontend, language acceptance, and name resolution

Scope: `af3ecc9..b591f699daa4bc4d7994c2b16f1891cb538ba113`, specifically the
Python/C/SIMD frontends, the byte-lane lexer, SWAR decimals, perfect keyword
lookup, removal of the prelude, and `E_RACE`/`E_COLLISION`. No compiler,
runtime, benchmark, example, or existing test was changed. No performance
measurements were made.

## Summary

* **A real diagnostic regression:** removing the prelude made map line
  numbers zero-based, but both command-line map consumers still assume
  one-based file lines. The runtime-error lookup can print the wrong source
  line or a blank line.
* **A structural fingerprint weakness, not a silent miscompile:** the new
  hash has algebraically constructible collisions between ordinary short
  names. A two-line program demonstrates it, and a 24-name collision family
  is generated without a hash search. All three implementations correctly
  reject the collisions with the specified `E_COLLISION`.
* **No compiler differential failure** in 10,141 independently generated
  cases: Python, scalar C, and SIMD stage2 agree on exact status and complete
  ELF bytes, including the empty output of failures. All statuses 0–14 were
  exercised. Additional regular-file compiler input checks and independent
  decimal-runtime checks passed.

The collision and race rejection are explicitly part of this language's
specification; they are not presented as implementation disagreements.

## 1. No-prelude change breaks source maps

**Location:** `boot/strand.py:31,36,249,258,472–474`;
`tools/where.py:7–9`.

`analyze()` now enumerates the actual source starting at line 0, with no
prepended prelude. `compile_(..., True)` returns this number unchanged.
The CLI still has the obsolete comment “line 0 is the prelude”, and
`where.py` indexes the original file with `mapped_line - 1`.

Reproduction source (`build/audit4-frontend/map-repro.strand`):

```strand
~1 @missing out
=missing
```

The program compiles successfully; its gather correctly fails at runtime.
Captured results from `focused-repros.json`:

```text
runtime exit: 99
strand: runtime check failed at 0x00000000004018f6

python3 boot/strand.py map build/audit4-frontend/map-repro.strand
4018a7 line 0  ~1
4018da line 0  @missing
40190c line 0  out

python3 tools/where.py build/audit4-frontend/map-repro.strand 4018f6
token @missing on line 0 :
```

The actual failing token is on file **line 1**, whose text is
`~1 @missing out`. The blank diagnostic comes from indexing the trailing
empty file line with `-1`. For other source lines the displayed text is
one physical line too early. Execution and emitted ELF bytes are unaffected.

Suggested follow-up, not applied: define one map-line convention and update
both consumers; add first-line, later-line, blank-line, and comment-line
source-map tests. This is directly attributable to the removal of the
prelude, not a claim that the gather failure is new.

## 2. Both 32-bit fingerprint halves have the same permutation invariance

**Location:** `boot/spec.py:155–167`; `boot/strandc.c:87–100`;
`src/compiler.strand:42,56–59,98–100`.

Each half is

```text
H = sum_j F(w[j] + j) mod 2^32.
```

For any permutation `p`, choose

```text
w'[j] = w[p[j]] + p[j] - j mod 2^32.
```

Then the multiset of `w'[j] + j` is unchanged. This preserves **both** hash
halves regardless of their multiplication constants or mixing shifts. It
does not require a birthday search, guessing 64 bits, or modifying tables.

Minimal ordinary-ASCII witness (`build/audit4-frontend/collision.strand`):

```strand
1 >bbbbdddd
2 >edddabbb
```

Both names are eight lowercase ASCII bytes. Their four little-endian words
are respectively:

```text
bbbbdddd: [1650614882, 1684300900, 0, 0]
edddabbb: [1684300901, 1650614881, 0, 0]
```

Both new fingerprints are `(3044233960, 2705037177)`. Each declaration alone
is accepted; together, Python, C and SIMD reject with status 5 and no output.
The historical byte/offset hash gives different fingerprints,
`(3341220918, 939569415)` and `(2195121061, 644233891)`, respectively.
This historical comparison checks the hash, not a full old-compiler rebuild.

`probe.py` also constructs all 24 permutations of the four words of
`ddddhhhhllllpppp`, adjusting the initial byte of each word as above. Every
result remains a 16-byte lowercase name, and all 24 share one fingerprint.
The full list is in `constructions.json`; the combined program is
`build/audit4-frontend/collision-family.strand`.

**Impact and qualification:** routine legal names can be selected to make
otherwise ordinary programs uncompilable. Treating this hash as a
well-distributed 64-bit fingerprint would be misleading. However, the exact
four-word check catches the collision, and rejection is expressly specified
at `boot/spec.py:145–148`. No aliasing, wrong output, memory corruption, or
cross-compiler acceptance difference was observed. This is a design/
availability limitation and a new easy collision construction, not an
unqualified claim of a language-spec violation.

Suggested follow-up, not applied: if predictable rejection is unacceptable,
make the position contribution order-sensitive or provide a collision
fallback. Keep explicit collision regression tests even if rejection remains
intentional.

### Keywords: distinguish table collisions from full fingerprint collisions

`src/compiler.strand:59` confirms all four words after its 8-bit lookup;
the generator at `tools/mkstencils.py:716–721` creates the index and exact
word tables. `KWSHIFT` is 22 at this revision. Python/C recognize exact
keywords and exclude them from the race (`boot/strand.py:28,99–103`;
`boot/strandc.c:95–100`).

The suite supplies non-keyword names occupying **every one of the 256 lookup
buckets**, every keyword with every sigil, and suffix near-misses. All agree
with exact keyword recognition.

The algebraic construction was additionally tried for all 34 keywords and
all 24 permutations: 816 attempts, 782 different raw word strings, **zero
lexically valid different names**. Short-keyword zero padding produces
embedded NUL/control bytes in those candidate strings. This is not a proof
that no full keyword fingerprint collision exists. No valid full-fingerprint
keyword collision was found or claimed.

## 3. Five names suffice to exercise all four failed race rounds

**Location:** `boot/spec.py:172–178`; `boot/strand.py:95–113`;
`boot/strandc.c:101–122`; `src/compiler.strand:72–100`.

`probe.py` deterministically enumerates `n%07d` and finds this five-name
witness after 21,514 candidates:

```strand
0 >n0001511
0 >n0004546
0 >n0011514
0 >n0014549
0 >n0021513
```

There are ten tokens, hence race mask 31. All five distinct fingerprints
give the four keys `(13, 26, 16, 13)`. In each round only the latest remaining
name resolves; after four rounds one remains unresolved. Python, C and SIMD
return status 4. Removing any of the first four declarations makes the
checked cases pass. The test does not require thousands of names or a
fingerprint collision.

Adding one neutral `0 end` line still fails. Adding four such lines raises
the token count to 18 and mask to 63, and this witness then passes. Thus
acceptance can change with unrelated source padding, as the specified
token-count-dependent race permits. The no-prelude change alters that
count, while excluding keywords changes the competitors.

The priority matrix includes all unordered pairs among errors 1–3 and 5–14,
both source orders, plus separately generated mask-127 race witnesses
combined with every other error. Thus both `E_RACE < E_COLLISION` and the
earlier lexical/length errors overriding either were tested, not inferred
from independently failing programs. All expected priorities matched.

## 4. What was checked independently

Run after building the checkout:

```sh
python3 reviews/audit4-frontend/probe.py > reviews/audit4-frontend/probe.log 2>&1
python3 reviews/audit4-frontend/focused.py > reviews/audit4-frontend/focused.log 2>&1
```

The scripts deliberately do **not** import the repository's fuzz generators.
They write only in `reviews/audit4-frontend/` and `build/audit4-frontend/`.
Unexpected Python exceptions, process timeouts, and failed independent
assertions fail the script rather than being counted as successful tests.
Differential mismatches preserve their source under the build directory and
are recorded in JSON.

Main generated case counts:

| Group | Cases |
|---|---:|
| Exhaustive length 0–4 strings over blank, letter, digit, quote, comment, LF, NUL, high-bit byte | 4,681 |
| Every byte at each of nine literal positions | 2,304 |
| Adjacent hostile SWAR bytes | 343 |
| Literal length/leading-zero boundaries | 30 |
| Valid decimal batches | 19 |
| Keywords × no sigil/every sigil | 748 |
| Non-keyword impostors at all keyword-table buckets | 256 |
| Keyword suffix near-misses | 102 |
| Every byte inside a name | 256 |
| Name lengths 1–20, lowercase/uppercase/underscore | 60 |
| Every byte inside a quoted string | 256 |
| Empty and boundary-sized strings, four EOF suffixes | 88 |
| Source shifts 0–129 for six lexical/bridge cases | 780 |
| Token-count boundaries | 12 |
| Explicit constructions | 11 |
| Error-priority cases | 195 |
| **Total** | **10,141** |

Every case compares Python's complete `(status, stdout)` with scalar C and
SIMD stage2. This includes exact ELF bytes for all 2,556 accepted programs.
An additional 891 stage2 compilations use a regular file rather than a pipe.
The 9,045 generated valid decimal values are compiled in 19 batches and the
resulting programs' output is compared to independently generated Python
`int(...).to_bytes(4, "little")` bytes.

The decimal arithmetic check also exhausts all **10,000 four-digit groups**
and all **1,110 reachable padded 1/2/3-digit prefix scalings**, using explicit
32-bit wraparound. This tests `src/compiler.strand:49–52` and the reciprocal
constants generated at `tools/mkstencils.py:723–735` independently of
`strand.analyze`.

Recorded reference status counts:

```text
0:2556 1:1628 2:2285 3:35 4:22 5:21 6:1734 7:273
8:1434 9:14 10:9 11:7 12:119 13:3 14:1
```

`results.json` records compiler/table SHA-256s and summary counts.
`cases.jsonl` records each case's group, deterministic label, source hash,
size, reference status/output hash, native results, and any stderr. Sources
are regenerated by the script instead of retaining 10,141 redundant files.
`constructions.json`, `focused-repros.json`, and the focused script capture
the concrete findings.

The focused run adds **568 sources / 2,272 native comparisons** (scalar C
and SIMD stage2, each using both pipe and regular-file input), with **zero
mismatches**. Its lengths straddle 64 bytes, 4 KiB, 64 KiB and 1 MiB; cases
include long comments, strings, unmatched quotes at EOF, `#` inside a
string, NUL padding, invalid long names/literals, and repeated lexical-state
changes at every byte-lane offset. It also reproduces the pair collision,
24-name collision family and five-name race witness on stage1, stage2,
stage3 and scalar C (12 checks, all with the expected error and no output).
See `focused-results.json` and `focused.log`.

### Limits

Agreement is **not** proof that all implementations satisfy an independent
language specification: the Python frontend remains the differential oracle,
and generated stencil metadata is shared. Independent expected error codes,
the algebraic hash argument, exact map/file-line evidence, decimal arithmetic,
and executable decimal-output checks address specific common-mode risks;
they do not eliminate every shared bug.

This report does not claim complete Unicode/name grammar coverage, all
possible 9-digit values, all source sizes, or an exhaustive 64-bit hash
collision search. General new-memory-primitive semantics and performance
measurements are outside this frontend sub-audit.
