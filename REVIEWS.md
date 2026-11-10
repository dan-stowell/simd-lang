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
