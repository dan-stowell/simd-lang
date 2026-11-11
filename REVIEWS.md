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

## Review 3 — adversarial review of the runtime (thread pool, byte lines, I/O) (2026-11-10)

Prompt: `reviews/03-runtime-prompt.txt`; answer: `reviews/03-runtime-codex.txt`.

| # | Codex said | Checked? | What I did |
|---|---|---|---|
| 1 | **Silent corruption in ordinary mode**: `input :x x prev + >y` / `x end` / `y out4` on 524,289 bytes gives wrong values at thread boundaries. The warm-up chunk suppresses the persistent tee's store, but the re-read of `x` still loads from the array, which the neighbouring thread may or may not have written yet. | **Reproduced** (8-CPU output differed from 1-CPU output). My fuzzer generates tees that outlive their line and tees re-read on their line, but almost never both, and its inputs are too small to fork without eager mode. | A push that re-reads a persistent tee on the tee's own line now gets its own class (`VECT` = `VEC` + a veto), in all three compilers, so such a line never forks. The reproducer is the first entry of the new `tests/pool.py`. |
| 2a | Every negative `read()` is treated as end of input: a directory on stdin "succeeds" with empty output. | Reproduced. | A read error now exits 98. |
| 2b | A regular file of size 0 is assumed empty: `bytes out < /proc/version` printed nothing. | Reproduced. | Size-0 files go through the read path. |
| 3a | Eager mode does not do what I said: with 8 CPUs every eager `out4`/`outif4` line stayed serial (blocks too small to split), and scan probes cannot dispatch on more than 8 CPUs. | Correct by arithmetic on the block sizes. | Eager blocks enlarged (probe 1,024, others 4,096-8,192 elements). The misleading comment is gone. |
| 3b | The fuzzer only feeds pipes (never the mmap path), never varies CPU count, never uses production-sized blocks. | True. | `tests/pool.py`: 22 targeted programs (nested scans, scan->prev->scan, negative scanmax, overflow, pending output before a fork, filters after scans, byte lines...) x sizes `64P-1, 64P, 64P+1, 4097, 512 K+1, 9 MiB+77` x 2/3/4/all CPUs x file/pipe x normal/eager, repeated for scheduling variation; the oracle is the same binary pinned to one CPU, plus the interpreter for small inputs. 2,728 runs pass. The fuzzer's normal mode now reads a regular file. CPU counts above 8 remain untested (this VM has 8). |
| 4a | "Matches or beats gcc -O3 on every row" contradicts the `upper` row (6.08 vs 6.63); "eight vCPUs are not better than four" contradicts `pairs`. | True; both were written from an earlier run. | Reworded to what the table says. |
| 4b | The C columns use `read()` blocks and one thread, so the ratios bundle I/O model, SIMD and threading; `run.py` does not reproduce the OpenMP table; and the most threatening missing comparison is hand-vectorised, hand-threaded C for `words` and filters — failure to auto-vectorise is not a limit of C. | Agreed on all three. | SWEETSPOT.md now has a "what this does not show" paragraph saying exactly that, pointing at `explore/ceiling.c` (hand intrinsics = the same instructions = the same speed). The matched hand-vectorised + threaded comparison is **not done** and is listed as such; I expect a tie. |
| 5 | mmap does not advance the shared file offset or follow growth; truncation can SIGBUS; EINTR on write is not retried; a closed pipe kills by SIGPIPE; `halt` and failures skip the flush. | Accurate description. | Documented here as the semantics; not changed. Output is unspecified on a non-zero exit by definition. |
| - | "No supported counterexample to scan verification itself", no lost wakeup, no ABA, no byte-semantics mismatch, no region overlap found; 245 generated programs on two CPUs passed. | (its probes) | Absence of evidence, noted. The pool remains the least-tested part of the system. |

## Review 4 — independent audit of claims and measurements (2026-11-10)

Commissioned by the repository owner (astra, x-high), not by me; the full text is AUDIT.md
and its evidence is in `reviews/audit-2026-11-10/`. It reran the suite and the benchmarks
and looked for counterexamples. What it found and what was done:

| finding | verdict | action |
|---|---|---|
| Bootstrap, tests, mutation suite and the single-CPU benchmark table reproduce | — | none needed |
| **`words` does not need intrinsics**: peeling the first iteration makes GCC vectorise the OpenMP baseline; Strand's lead drops from 3-4x to about 1.1x | **correct, and the most important point** | Both C baselines now use adjacent loads. SWEETSPOT.md is rewritten around the rebuilt table; `explore/cliff.c` measures five spellings of the loop (one vectorises). The claim "the compiler cannot vectorise a carried dependency" is withdrawn; what survives is narrower (filters, table lookups) and one new class was added where it does hold (automata). |
| The 15% compile-time win on 8 vCPUs does not reproduce | accepted | withdrawn in SWEETSPOT.md and REPORT.md |
| Defect 1: `tools/bench.c` reports failed children as fast runs; `run.py` hashes only one configuration | correct | `bench.c` fails on any failed child; `run.py` hashes every timed configuration under its own CPU mask, interleaves rounds, keeps all samples, reports median and best |
| Defect 2: `base.c` reprocesses bytes after short reads; `base_omp.c` assumes the requested team size | correct | remainder handling restricted to dword kernels; every parallel region partitions by `omp_get_num_threads()` |
| Defect 3: hardware probes that do not test what they say | correct, and understated | four probes (not two) lacked their instruction; rewritten in assembly with a disassembly check; signed overflow fixed; HARDWARE.md corrects the gather, classification and frequency statements |
| Defect 4: the "I/O floor" is an experiment, not a bound; `iotest.c` has a data race | correct | race removed; SWEETSPOT.md now calls it an observation |
| Defect 5: `halt` and failures can leave partial output, contrary to DESIGN.md | correct | DESIGN.md states the streaming semantics; tests still compare only the status on failure |
| Defect 6: README omits VBMI/VBMI2; hard-coded CPU ids; stale statements | correct | README lists the requirements; `run.py` and `pool.py` use the real affinity ids; ledger and report corrected |
| `E_RACE` is easy to construct (a 137-byte program) | correct | the audit added the fixture to `tests/cases.py`; the 2^-64 argument applies to fingerprint collisions only |
| The interpreter shares `analyze()` with the Python compiler; all compilers share stencils | correct | not fixed. An independent front end for the interpreter is still on the list. |
| Narrow benchmark data (lowercase only; `nonascii` always 0) | correct | the text generator now emits capitals, digits, punctuation and non-ASCII bytes; new inputs are CSV with quoted fields and real source text |

Not done: resource-limit and `E_COLLISION` tests; an independent interpreter; a matched
comparison against hand-vectorised, hand-threaded C for filters and automata (the expected
result is a tie: `explore/fsm.c` is that C for one automaton, in memory, and is faster than
the Strand program).

What I take from it: I explained a measurement with a general claim about compilers and
did not try to break the claim. The benchmark harness now makes the cheap version of that
mistake hard (outputs are checked per configuration, failures stop the run), and the
baselines were each tried in several formulations before being timed.

## Review 5 — second audit, of the response to the first (2026-11-10)

Again commissioned by the repository owner (astra, x-high); the text is AUDIT_FOLLOWUP.md, the
verdict and its backlog are at the top of AUDIT.md, the evidence is in
`reviews/reaudit-2026-11-10/`. It reviewed `17e317d`.

| finding | verdict | action |
|---|---|---|
| Tests, the 21 output checks and the automaton gains over the supplied C reproduce | — | none needed |
| **1. `explore/iotest.c`: my race fix left the per-thread sums unused, so GCC deleted the page touches** | correct; I broke the probe while fixing it | the sums are folded into a printed checksum; the disassembly of `touch` has its load again. SWEETSPOT.md no longer leans on this probe for any number. |
| **2. `swapcase` in C is arithmetic, not a table**; written that way it vectorises and the lead shrinks to 1.1-1.6x | correct. The second time a "C cannot" turned out to be "this spelling does not". | `swapcase` is arithmetic on both sides now and is reported as the tie it is. The table claim moved to a table that is not arithmetic: `xlat`, EBCDIC to Latin-1 (`bench/xlat.h`). |
| "Best C" is not an upper bound on C; compare with the intrinsics prototype under the same conditions | correct, and the most useful request | new `bench/hand.c`: AVX-512 intrinsics + OpenMP for the seven programs where ordinary C loses, using the same techniques as the stencils and, for CSV and JSON, the better-known algorithm (carry-less multiply, as simdjson). The table has a "Strand / best hand C" column. Result: Strand is level with or behind hand-written intrinsics; the advantage is over what a compiler makes of ordinary source. SWEETSPOT.md is rewritten to say that. |
| "Best C at n CPUs" was OpenMP at exactly n threads | correct | best C for a budget is now the fastest C configuration on at most that many CPUs |
| **3. Chained `prev` falls back to one thread** (`scan dup prev dup prev + +`); tests pass regardless | correct. Cause found: the check compared the whole carry vector of a `prev`, of which only the last element is ever read; the other elements legitimately differ. | fixed (compare the element that is used). Then the real limit behind it, carries that depend on carries, was addressed: the pool retries with carries derived from the real pass (sums by difference, automata by starting from a permutation that can be peeled off, maxima by one pass from -inf), bounded by the thread count. `tests/pool.py` now asserts the path taken, with a copy of each binary whose fallback entry exits 77 (the audit's own detector), for 20 programs at 2, 3, 4 and 8 CPUs. README and DESIGN no longer say "every line can be split". |
| **4. The automaton examples are a toy grammar, not Python lexing** | correct | README, SWEETSPOT and REPORT say "'#' comments and "..." strings" and call it a simplified grammar. A real grammar was added where four states are enough: `json` (structural characters outside strings), checked against a skeleton rebuilt from Python's `json` parser, not against another scanner. |
| **5. `check_probe.py` accepts `/bin/true`; weak APX and VL patterns; HARDWARE.md contradicts its own corrections** | correct on all three | the checker fails on a missing probe (42 problems for `/bin/true`) and names the instruction for VL and APX; the prefix-sum row says 2-4x with the reason (0.85 or 1.69 ticks per element for the scalar loop, by build flags); the gather row and conclusion 2 are rewritten instead of footnoted |
| Fixtures are reused by file name across generator changes | correct | fixture names carry a hash of the generators; `results.json` records input hashes, compiler version, source revision and CPU sets |
| Exercise all 256 four-state functions and their compositions | done | `tests/fsm_all.py`, in `test.sh` |

Not done from its backlog: an independent interpreter; varying input size, distribution and
output destination; uncertainty estimates beyond median and best; a real-language lexer
against an independent lexer (needs more than four states, see SWEETSPOT.md).

What I take from it: the first audit's lesson was "try other spellings of the baseline", and
I then did not try the most obvious other spelling of `swapcase`. The structural fix is the
hand-C column: instead of arguing about how strong the C is, the table now brackets Strand
between what a compiler does with ordinary source and what an expert does with intrinsics.

## Review 6 — third audit (2026-11-10)

Commissioned by me this time, at the owner's request that stable checkpoints be audited:
astra, x-high, started with the shelley CLI against `af3ecc9`. The text is AUDIT_3.md, the
evidence under `reviews/audit3*`.

| finding | verdict | action |
|---|---|---|
| The retry and prev-check changes are sound; no false acceptance found in adversarial carry tests | — | none needed |
| The hand-C column is substantive; "75-95% of hand C with the same technique, one CPU" is supported by retained and fresh samples | — | none needed |
| **A stronger ordinary C for the multicore automata**: packing the four transitions into one byte (`s = (PACKED[c] >> 2*s) & 3`) roughly doubles the two-pass baseline on 8 CPUs; Strand's lead there is 1.8-2.8x, not 3-5.6x | correct. The third time. | **not yet done**: the packed form should join `base_omp.c` as an alternative (it is slower on one CPU for `strip` and `strings`, so both stay) and the table should be re-run. Until then read the 4- and 8-CPU "ordinary C" ratios for `strip`, `idents`, `strings` as about half of what SWEETSPOT.md prints. |
| `tests/fsm_all.py` covers all 256 functions but not all ordered pairs at all positions, as its docstring claims | correct | **not yet done**; the docstring overstates |
| JSON structural extraction is not "a complete scanner" | correct | SWEETSPOT.md row reworded |
| The phase map misdescribes the compiler: positions and decimal literals were already built; liveness is not a gen/kill scan; "everything after the lexer is outside the model" is too broad | correct | rows rewritten (and overtaken: the lexer has since moved to byte lanes with a new bridge, see DESIGN.md) |
| `check_probe.py` checks `avxvnni` with a pattern that AVX-512 VNNI also satisfies | correct | **not yet done** |
| Measure retries, not only their existence | fair | **not yet done**: no benchmark of dependent-carry lines exists |

## Review 7 — fourth audit, unfinished (2026-11-10)

Started by me against `b591f69` (astra, x-high, through the shelley CLI). The model request
stalled before the report was complete: AUDIT_4.md has placeholders where three measurement
tables and the memory section were to go, and the auditor never committed it. I have added
the file and its evidence (`reviews/audit4-*`) as they were left. What it had established:

| finding | verdict | action |
|---|---|---|
| **A line could read the vector it was copying slices into, and compiled code and interpreter disagreed** (status 0, different output) | correct; my fuzzer found the same thing an hour later | a line may not read its slice destination, and all slices of a line go to one vector (`E_ALIAS`), in all three compilers |
| **The new name fingerprint is a symmetric function of a name's four dwords**: permuting them collides; 24 colliding ASCII names constructed without a search | correct, and embarrassing | each dword position has its own multiplier (`HA1J`, `HA2J` in `boot/spec.py`). The exact comparison had kept this from being a miscompile; it was a way to make valid programs fail with `E_COLLISION`. |
| A five-name `E_RACE` witness exists | noted | the fixture in `tests/cases.py` is now searched for from the hash definition instead of being a list of names that breaks whenever the hash changes |
| `boot/strand.py map` and `tools/where.py` report lines one too low since the prelude went | correct | the map is 1-based again |
| Front end: no divergence in 10,141 generated cases plus boundary sources; SWAR decimals exhaustively right | — | none needed |
| Compiler speed: 44.7 ms against 41.1 for scalar C at 1 MB; 395 against 360 at 8.3 MB on one core; 312 against 369 on eight. Peak memory 298 MB against 333 | so my "level at 7.7 MB" was a favourable run; **on one core Strand is about 10% slower** | the claim is corrected here; REPORT.md gets the audit's figures |
| The C linker reference called a fortified `memcpy` per relocation | correct | four byte assignments; the reference went from 266 to 238 ms. Strand's linker: 303 ms on one core, 295 on eight. |
| Prefix differences are not a per-line profile | fair: vectors die earlier in a prefix and classes change (a tee becomes chunk-local) | I saw the same when profiling the Go lexer. `tools/lineprof.py` keeps its caveat; the instruction counts from `tools/simdratio.c` are exact. |
| Review 6 was not a complete account of AUDIT_3's backlog | correct | still open, listed there and here: the packed-transition C baseline in the benchmark table, the `avxvnni` probe pattern, a retry-cost benchmark, `jsongen.skeleton` on duplicate keys, `bench/oneoff.py` provenance, stale sentences in blog 04 and the SWEETSPOT phase row |

Not established, because the audit stopped: its memory-safety section. That is the part I
most wanted, with eleven new primitives that write through computed addresses.

## Review 8 — fifth audit, of the Go lexer; unfinished (2026-11-11)

Started by me against `25a887e` (astra, x-high). It ran three campaigns and then stalled the
same way as the fourth (the provider flagged a request, then the model went silent); there
is no AUDIT_5.md. Its evidence is in `reviews/audit5/` as it was left; I ran its speed
harness myself afterwards (`reviews/audit5/speed/logs/final/`).

| finding | verdict | action |
|---|---|---|
| **Corpus equality reproduces** (17,047,917 tokens; 1, 2, 3, 8 CPUs; all 128 prefix alignments; tokens and comments over 1 MiB) | — | none needed |
| **`x.0`, `.0.0`, `return.5`: a dot before a digit starts a float also after an identifier or a float.** The lexer made it an operator. | correct. `return.5` is in a valid Go program. | fixed |
| **`0a`, `1ii`, `1e2x`: a number ends where `scanNumber` ends it**; the lexer let any letter continue it. go/scanner accepts these without error, as two tokens. | correct | fixed |
| **`...0`: the third dot of an ellipsis was taken for the start of `.0`** and the first two dots vanished | correct | fixed |
| A complete, valid file that does not end in a newline (`package p`) is refused with status 3; 14 corpus files had a newline appended | correct; documented, but worse than I made it sound | fixed: `bytesz` leads a byte line over the input and one zero byte after it, so the last token ends like any other; the fuzzer covers files without a final newline and files that end on a page boundary |
| A leading byte-order mark becomes an identifier | correct; documented as excluded | open |
| Carriage returns: no mismatch found in dedicated controls | so that exclusion was over-cautious | GOLEX.md says so |
| **go/scanner is not the scanner the Go compiler uses.** `cmd/compile/internal/syntax` scans the corpus in 497-513 ms on one core, against 549-596 for go/scanner | correct, and the baseline that matters | GOLEX.md's table has both. On one core Strand's whole process (447-453 ms) is about 10% under the compiler scanner's scan alone, not 25%. |
| The sampling profiler is neither a CPU profile nor an unbiased wall-clock one: kernel time is under-sampled (one sample for 80 ms of page population), a sleeping process is sampled at full rate, other threads are not followed | correct | GOLEX.md calls the percentages "shares of samples on one core"; the 85 ms kernel figure never came from the sampler (it is `time`'s system time) and is now attributed |
| `simdratio` counts a `rep` instruction once per iteration, swallows signals, and does not follow threads; `whattime.py` charges a line's prologue to the line before | correct | documented in the tools' headers; on the lexer's large-input profile the prologue effect is one sample in 1,350 |
| The instruction classifier agrees with objdump on all 2,904 instructions of the lexer | — | none needed |

The three lexer bugs were one mistake: I wrote the word automaton from my idea of Go's
numbers instead of from `scanNumber`, and tested it on a corpus of programs people had
already compiled. The automaton is now a transcription of `scanNumber` for input it accepts
(`examples/golex_gen.py`, section 2), still eight states: dots of a possible `...` are
counted in the automaton that needs to know (a float cannot start on the third), a `0x`
prefix is recognised from the two bytes before it rather than by a state for "just a 0",
and the state after an exponent letter and the state after the suffix `i` are folded into
their neighbours with one input class each. `tests/golex_fuzz.py` is the test I should have
had: random lines built from the pieces where token boundaries are decided, filtered by
go/scanner to those it accepts, compared token for token. It found a fourth bug of the same
family within a second (`0X1F0b`: a `b` after a zero inside a hex number is a digit, not a
binary prefix), and has since agreed on 1.3 million accepted lines; all 642 of the audit's
applicable minimal witnesses pass. The lexer's time did not change (447-453 ms).

Not reached by the audit: memory safety, again.

## Review 9 — a sixth audit that did not run (2026-11-11)

Started against `01e53c1` to attack the one-pass thread pool and the corrected lexer. It
did not get past reading the repository: every model request came back from the provider
"flagged for possible cybersecurity risk" (the fifth audit ended the same way after its
three campaigns), and it produced no findings. So the one-pass pool has been reviewed by
nobody but me. What stands behind it: `tests/pool.py` (5,456 runs against the same binary
on one CPU, 96 checks of which path ran), `tests/pool_big.py` (inputs built to defeat the
guesses, with checks that the retry, copy and fallback paths ran), the semantic fuzzer in
eager mode, the mutation suite, and the Go corpus on 1, 2, 3 and 8 CPUs. What does not: an
adversary.

## Review 10 — the pool's kernel, by gpt-6.1-sol (2026-11-11)

Asked for after the audits stopped running: a narrower question to a different model.
RUNTIME.md had just claimed that one comparison (`fs7`) decides whether a parallel block is
kept and that everything else in the pool cannot affect output. `REVIEW_SOL.md` is the
answer, complete.

| finding | verdict | action |
|---|---|---|
| A line whose only carries are `prev` never reaches `fs7`: it is cut into ranges after a one-chunk warm-up and kept. Demonstrated with a trap at `fs7` that is never hit. Not a wrong-output bug (the warm-up argument holds), but RUNTIME.md's rule was stated as universal and chaos did not cover the path. | correct | such lines, and lines with an `out` sink, are now one-pass lines: guessed, compared, retried or redone like any other. Only a line with no carries skips the comparison. |
| A byte line's `prev` carry is compared by its last dword though only the last byte is read: needless retries, and chaos left bytes 60-62 unspoiled | correct | compared by the last byte; chaos spoils the other 63. (The Go lexer on eight CPUs went from about 126 ms to about 115: fewer needless retries.) |
| Comparison per carry kind, scratch-area arithmetic, copy step, fallback, accumulators: checked by hand, no defect found | — | none needed |
| Chaos is a bounded fault model (one dword of noise per range), exempts the first range and sink counts by design, and is not a proof | fair | RUNTIME.md says so |

While this review ran its tests in one checkout and I ran mine in another, `tests/pool.py`
reported three empty outputs: both runs were writing their inputs to `/tmp/strand_pool`.
The tests now use private directories.

## Overnight 2026-11-11/12: background reviews of the build-speed work

One line per inbox item; raw files in `reviews/done/`.

| file | reviewer said | verdict | action |
|---|---|---|---|
| `20261111T2236-codex-skeptic.md` (1) | Early export leaves the front-end chain, single giant functions (the four go-json VM packages) and the link serial. | **Accept** as a statement of what the rung does not do. | Measured after the rung: the chain is 12.1 s, of which `minio/cmd` alone is 9.1 s. Its front end is one thread inside one process; that is now the chain. |
| (2) | The 3 ms link floor is a copy of the output at memory bandwidth, not a linker floor. | **Accept.** | Floor now counts one pass over the objects in plus the binary out, and TRACE.md says it is a copy floor with no model of symbol resolution or relocation. |
| (3) | Scheduling stock work is a dead end: 265 core-s / 8 is 33 s against 16. Build a whole-program representation and schedule functions globally. | **Accept the diagnosis.** | Rungs 2 and 3 took the cheap cuts first (now 144 core-s). The next rung removes work rather than scheduling it: whole-program reachability before code generation. |
| (4) | The ambitious step is a Strand back end for a subset of real functions, fed into a real MinIO binary. | **Defer**, with a reason: a subset back end covering a few percent of functions cannot move the trace tonight, and it needs function-granular scheduling and reachability first, which is the next rung. Recorded in IDEAS.md. | — |
| `20261111T2302-sol.md` (its answer is `reviews/early-export-2026-11-11/README.md`, commit `346c9c2`) (1) | With `-gcflags=-S`, a 381-byte program whose instantiated generic name is megabytes long makes the compiler print a line over 16 MiB; the driver's scanner stops reading and the build hangs. | **Reproduced, fixed.** | `runCompile` now reads stdout to the end in 64 KiB pieces, whatever the line length. Its counterexample is in `tests/sb_driver.sh`. |
| (2) | `-noearly` does not clear an inherited `SB_EARLY_EXPORT`, so the "idle" half of `tests/sb_early.sh` could silently test early mode. | **Reproduced, fixed.** | The driver never passes the inherited variable on. In `tests/sb_driver.sh`. |
| (rest) | No token, AST, type or behaviour difference found in default mode: the existing differential tests, 95 upstream test programs, 27 boundary probes and eight standard-library test packages built with the patched compiler pass. | Noted as absence of evidence. | — |
