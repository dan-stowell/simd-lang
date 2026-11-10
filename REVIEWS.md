# REVIEWS.md

Log of outside review (Codex `gpt-6-astra`), what it said, and what was done about each
point. Its claims are treated as hypotheses. Raw transcripts are in `reviews/`.

## Review 1 — unanchored brainstorm, before the design was fixed (2026-11-10)

Codex was given only the problem statement (`reviews/01-brainstorm-prompt.txt`), not my
plan. My own design (DESIGN.md) was written and committed *before* I read its answer
(`reviews/01-brainstorm-codex.txt`), so the two can be compared honestly.

| # | Codex said | Verdict | What I did |
|---|---|---|---|
| 1 | Make the source a "proof-carrying instruction table": fixed-width records, numeric operand IDs, explicit dependency ranks; names are editor annotations. | **Rejected.** | This removes the front end by removing the language: the "source" is an IR dump and the hard parts (names, structure) move into an editor nobody is building. Strand keeps real names and free-form tokens and still has no sequential phase: names resolve by scatter race, structure by one depth scan. |
| 2 | Replace register allocation with one memory slot per SSA value; allow hand-annotated "fusion capsules" with declared scratch registers. | **Rejected, mine is stronger.** | In Strand the register *is* the postfix stack depth, computed by one prefix sum; nothing is declared and nothing round-trips through memory inside a line. Named values do live in slots, which matches Codex's point across lines. |
| 3 | Fixed-size executable "tiles" so addresses are arithmetic on indices; uniform long branches. | **Partly same, partly rejected.** | Same idea as my stencils (template + patch). I keep variable-size stencils, since one prefix sum over sizes is cheap, and I remove branch fixups entirely (back-edge via `jmp r11`) rather than padding them. Its warning that padding wastes I-cache is why. |
| 4 | Don't hash or sort names; make collision-freedom "part of program validity". | **Half adopted.** | I do hash, but the framing is right and I use it: a 32-bit hash collision or an unresolvable 3-round race is a *compile error* (detected exactly by a byte-compare gather), not something the compiler must handle. |
| 5 | Bounded "expansion algebra": maps, scans, reductions, gathers, unique scatters, compaction; no recursion. | **Agrees with DESIGN.md.** | Already the design. Strand goes further: no functions and no fixed-count repetition either. |
| 6 | Trap: "parallel work is not shallow work"; rank-by-rank execution can have linear depth. | **Accepted as a constraint.** | Strand has no dependency-ordered phase at all (types are lexical, names are a race), so there is no rank loop. Claim to verify at the end: the compiler has a fixed number of passes independent of input. |
| 7 | Trap: masks don't erase hazards (invalid gathers, conflicting scatters). | **Accepted.** | Gathers/scatters are bounds-checked under the active-lane mask; loads are fault-suppressing masked loads; scatter order is specified (last writer wins) and the reference interpreter implements the same rule. A line may not read the array it scatters into (static check), because chunking would make that order-dependent. |
| 8 | Trap: determinism needs a spec, including *diagnostic order*. | **Adopted — I had not planned this.** | Error checks are numbered; the exit status is the lowest-numbered failing check. The scalar reference implements the same order, and malformed inputs are part of the differential fuzz. |
| 9 | Trap: Zen 4 512-bit is 2x256. | **Already measured** in HARDWARE.md (zmm add = 2.0x ymm). | — |
| 10 | Trap: fixed point proves stability, not correctness; test malformed input and executable output. | **Adopted.** | Independent scalar interpreter + byte-exact scalar reference compiler + malformed-input fuzz, run on every stage. |

## Review 2 — adversarial push-back on the real artifacts (2026-11-10)

Codex was pointed at the repository after the fixed point and told agreement was not useful
(`reviews/02-pushback-prompt.txt`; its answer is `reviews/02-pushback-codex.txt`). It ran probes
against the binaries. Every claim below was checked before acting on it.

| # | Codex said | Checked? | What I did |
|---|---|---|---|
| 1a | Two names (`xfntrxrmgiq`, `xqhhsigtfyu`) have the same 32-bit hash: stage2 exits 5, the Python reference accepts. "Byte for byte on every input" is false. | **Reproduced.** My hash was a linear weighted sum; collisions are easy to construct. | Fingerprint is now 64 bits (two sums of a multiply-xorshift mix of (byte, offset)); and the hash is part of the *spec*, see 1c. Both names now compile, identically everywhere. Added as a test case. |
| 1b | 40,000 distinct names exhaust the 3-round race in a fixed 65,536-slot table: stage2 exits 4, Python emits 14 MB. | **Reproduced.** | Table size is now `2 x pow2ceil(tokens)` (computed with a branch-free bit smear), four rounds, round keys mix both hashes. The 40,000-name program compiles byte-identically in all three compilers and is in `test.sh`. |
| 1c | "The weakest design decision is making a bounded hashing heuristic part of language acceptance without matching reference semantics or a fallback." | **Agreed on the diagnosis, not on needing a fallback.** | A complete fallback needs either a sort or an unbounded loop; the language has neither and I did not want to add one for a 2^-64 event. Instead the heuristic is now *the definition*: `boot/spec.py` specifies the fingerprint and the race, and the Python and C references simulate it with ordinary loops. Acceptance no longer depends on which compiler you run. A program can still be rejected with E_RACE/E_COLLISION; that is now a documented property of Strand (and listed in SCALAR_LEDGER.md as a wart), not a compiler bug. |
| 2 | mmap results unchecked; heap and output cursors never checked; with >= 2^31 elements the unsigned bounds check and the sign-extending gather disagree; at 2^32 the lane mask truncates. | Source-derived by Codex; **confirmed by reading the stencils.** The two mmaps were even adjacent, so output overrun would have corrupted the heap. | mmap failure and inputs >= 2 GiB exit 98; the bump allocator and both output sinks check against a limit and fail with 99. Since every length now derives from an input < 2^31, a non-negative i32, or an existing length, vectors cannot reach 2^31 elements, which removes the sign problem. *Not* differentially tested (the interpreter cannot model 64 GiB); stated in the ledger. |
| 3 | `build.sh`: `cmp a b && echo ok` does not fail under `set -e`, so the bootstrap check could never fail. | **True, and embarrassing.** | `cmp ... || { echo FAIL; exit 1; }`. Verified by corrupting stage1. |
| 4a | The semantic fuzzer always stores to fresh names, so release-on-overwrite and block reuse are untested. | **True.** | The generator now overwrites existing vectors (including ones the same line reads, and static data) 35% of the time; hand cases added. No bug found. To check the suite can find such bugs at all, `tests/mutation.sh` injects five (wrong compare, freeing static data, lost `prev` carry, broken scan, premature release): all five are caught. |
| 4b | Programs are at most 24 lines, inputs at most 1000 bytes; no hand case for errors 4 and 5. | True. | 10% of programs now have 60-200 lines; input lengths include 4095/4096/4097 and 20001; `test.sh` compiles a 340 KB and a 40,000-name program with all three compilers. Errors 4 and 5 still have **no** test: I cannot construct a 64-bit fingerprint collision or a 4-round loss. Stated in the ledger. |
| 4c | Padding samples one offset rather than all residues. | True. | 8% of fuzz programs are now compiled at all 16 alignments. |
| 4d | The interpreter shares lexing and validation with the reference compiler, so shared acceptance bugs survive. | True. | Partly mitigated: `boot/strandc.c` has its own lexer and name table, and is now a second oracle in the suite (`--also`). The interpreter still shares `analyze()`; a fully independent interpreter remains undone. |
| 5a | "Only branches are loop back-edges and runtime safety checks" is false: the allocator branches; `halt` is a conditional syscall outside the stub. | **True.** | DESIGN.md corrected; both added to SCALAR_LEDGER.md. |
| 5b | The blog says the suite compares against two scalar compilers; `test.sh` only used Python. | True at the time. | Now true (4d). Blog post carries a dated correction. |
| 5c | "SIMD code generator" is fair for layout, copying and patching; it must not imply SIMD instruction *encoding*. | Agreed. | Already in the ledger; wording tightened in DESIGN.md and REPORT.md. |
| 6 | No use-after-free or scatter-order bug found; allocator ordering looks right. | (its probes) | Nothing to do; noted that this is absence of evidence. |
| 7 | Novelty is "the combination and restriction", not a new general technique. | Agreed in the main. | REPORT.md says exactly which pieces I believe are new and which are borrowed. |
| P1 | Fuse passes and eliminate intermediates. | Already under way when the review arrived. | Done: tee stores, 232 -> 149 source lines, byte-level front end 3 loops instead of ~40. Measured in REPORT.md. |
| P2 | Packed byte lanes for source and code. | Plausible; HARDWARE.md agrees. | **Not done.** Recorded as the top "next" item with the allocator model's numbers. |
| P3 | Specialise scalar lines; stop clearing 16 carry registers per line. | Checked: every line prologue did 17 vector inits. | Done differently: each carry/accumulator is initialised by the prologue of the token that uses it, so no line pays for carries it does not have. |
