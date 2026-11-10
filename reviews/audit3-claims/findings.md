# af3ecc9: claims and supporting-probe audit

Reviewed `af3ecc935319a4698b499c66044faff95ef0663d`, after reading
`AUDIT_FOLLOWUP.md`. Source/static checks and tiny non-timing correctness controls
only. No implementation edits, commits, hardware timing runs or benchmark
campaigns. `phase-map.md` checks **every** entry of the compiler-phase table.

## Findings in priority order

1. **Some compiler-phase claims remain broader than their evidence.**
   `SWEETSPOT.md:244` calls JSON structural-character extraction / CSV separator
   counting a “complete scanner”; it is not a complete tokeniser or validator.
   `:251` says the compiler performs backward gen/kill-scan liveness, but
   `src/compiler.strand:162–164` actually uses the scatter-selected last name
   occurrence. `:247`'s “number literals ... not built” omits the decimal parser
   already implemented by positional weights and prefix differences at `:44–50`.
   `:245`'s missing byte/DFA-to-dword bridge is real, but token positions in
   general already exist in dword programs. See the all-phase appendix.

2. **The new bracket is a useful, honest change; universal compiler/expert
   language still exceeds the experiment.** The source now supplies matched
   whole-process AVX-512/OpenMP comparators; the text says these are the
   author's implementations, not an upper bound on C or experts
   (`SWEETSPOT.md:24–27,139–141`). But “no compiler turns ...”
   (`:21`), “a compiler will not ...” (`:30`), “shapes compilers do not
   vectorise at all” (`:53`) and “quote parity ... beats any automaton”
   (`:22`) are not established by this GCC/author/binary comparison.
   “This GCC build and these ordinary-C baselines” is supported.
   No new ratios are measured by this audit.

3. **Dependent-carry documentation is substantially more candid, but the pass
   budget is a heuristic, not a speed measurement or full expressibility
   theorem.** `stencils/stub.S:600–629` masks `prev` comparisons to the last
   dword and automaton comparisons to its output-at-zero byte; `:641–705`
   reuses real-pass summaries, subtracts incoming sums, peels the automaton
   permutation, and has the running-max low-start retry. The retry gate is
   `f_retry + 4 <= nthreads` (`:642–646`), allowing up to `T-3` retries for
   `T>=4`: for ordinary carry dependencies this explains 2 levels at 4 CPUs
   and 6 at 8. `tests/pool.py` really includes 20 patched-fallback path
   checks per CPU setting and labels its DEPTH values as those whose guesses
   fail **on this input**. This is much stronger than output checks alone.
   It is not a proof that any eligible composition of that source depth will
   pass verification; explicit pool vetoes and data-dependent accidental
   convergence also matter. “Past that, one thread is as fast”
   (`SWEETSPOT.md:220–221`) is unjustified by pass counting alone. The
   immediate disclosure “not benchmarked ... path, not a speedup” (`:229–230`)
   is the correct qualification. `REPORT.md:262–263` and blog `:65–70` should
   retain that distinction when summarising “dependent carries stay parallel”.
   Runtime corner-case/path testing is separate evidence, not performed here.

4. **Supporting I/O DCE fix is confirmed.** With the documented
   `gcc -O2 -pthread`, `touch` now loads `base`, executes
   `movzbl (%r8,%rax,1),%ecx`, adds each byte and stores the worker sum.
   `par` adds joined sums to `check`, printed by `main`.
   `iotest.dis` and `observations.json` preserve this non-timing evidence.
   Thus `REVIEWS.md:107` / `SWEETSPOT.md:304–306` accurately describe the fix.
   A one-byte-per-page checksum is not a full-input checksum or universal I/O
   floor. The probe still assumes successful open/map/pread/thread creation and
   the convenient 256 MiB divisibility; this is a supporting experiment, not a
   robust general I/O utility. The docs now correctly withdraw the floor.

5. **Hardware/checker response fixed the named earlier defects; a narrower
   encoding check remains incomplete.** Fresh `gcc -O2` compilation passes
   `check_probe.py`; `/bin/true` now fails with 42 missing probes.
   Actual `p_512vl` contains `vpcmpeqd %ymm0,%ymm0,%k0` and `p_apx` contains
   a decoded REX2 `mov %eax,%r16d`. Their old weak checks were meaningfully
   strengthened. A crafted binary replacing **only** `p_avxvnni` with an
   EVEX/zmm `vpdpbusd` still passes because `WANT['avxvnni']` only checks
   `vpdp`, although that probe promises **VEX AVX-VNNI**, not AVX-512VNNI.
   The original disassembly contains the correct `{vex}` instruction:
   this is a remaining regression-check gap, **not** evidence that the
   repaired current extension results are false.

   A separate ret-before-instruction control also passes all 42 checks;
   this is only the inherent “contains versus executes” limitation of a
   textual disassembly checker, not treated as a new implementation defect.
   The current source/disassembly must be inspected for reachability.
   `HARDWARE.md:51,54,60,64–66,76–110` now consistently qualifies gather's
   fixed working set, unequal classification tasks, prefix's 2–4x arithmetic,
   kernel totals rather than lane serialization, and the one-run frequency
   observation. Those substantive earlier contradictions are corrected.
   “Everything advertised ... nothing unadvertised” (`:26`) should mean the
   representative tested instruction forms, not exhaustive CPUID validation;
   “zmm costs exactly two ymm ops” (`:98–100`, also SWEETSPOT `:187`) is
   broader than the measured selected kernels.

## Review 5 / Addendum 4 / blog: remaining bookkeeping and scope

* Review 5 (`REVIEWS.md:108–114`) accurately records arithmetic swapcase, an
  arbitrary-table comparator, the retained per-configuration hashing, a
  resource-budget comparator, and generator-hashed filenames. `run.py`'s
  “at most” chooses among configurations actually sampled (1, 4, all), not
  every team size up to the budget; that distinction should remain explicit.
  The code fixture also depends on installed stdlib contents, not just source
  hashes; metadata's input digest preserves what was actually used.
* `REPORT.md:253–254`, `REVIEWS.md:109`, and SWEETSPOT's introduction still
  say **seven** hand kernels; `bench/hand.c:19–26` defines **eight**:
  letters, filter4, strip, csv, idents, strings, xlat, json. “Every program
  where ordinary C loses” also has the declared `maxline` one-core exception.
  This is minor stale prose, not missing the promised JSON implementation.
* `REVIEWS.md:115`/`REPORT.md:269` correctly report coverage of all 256
  transition functions. `tests/fsm_all.py:22–24` puts them in 16 groups of 16;
  its ordered pairs are **within each group**, not all 256² cross-function
  pairs. Interpreted-output equality and those test runs are not a proof
  that all state-function compositions are correct; the suite's own
  docstring is narrower than such an interpretation. No timing or full test
  suite rerun was performed by this sub-audit.
* JSON's oracle (`bench/jsongen.py:skeleton`) is independent for structural
  extraction on generated values and is a useful improvement. At 256 MiB,
  `bench/run.py:50–57` concatenates 16 separately valid documents and compares
  concatenated per-document skeletons. That fixture is **not one valid JSON
  document** accepted by `json.loads` as a whole. This does not invalidate
  structural extraction throughput or the per-document oracle; distinguish
  it from complete JSON parsing or universal JSON correctness.
* `blog/04:79–80` says everything after the lexer is outside the model,
  although the table explicitly includes implemented backend/layout phases
  and limited symbol joins. “A general optimising compiler has unsupported
  phases and this self-hosted compiler is slower” is defensible.

## Reproduction and retained evidence

From repository root:

```sh
python3 reviews/audit3-claims/check_claims.py
```

The script never executes `hw/probe` or `iotest`: it only compiles and
disassembles them. Tiny generated Strand programs are run for semantic and
bridge controls, pinned to one CPU. Generated binaries and patched controls
are under ignored `build/audit3-claims/`; no implementations are modified.

* `observations.json`: compiler version, revision, masks, exit codes,
  byte-exact output, and binary hashes.
* `checker-*.txt`: successful repaired checker and its controls.
* `probe-original.dis`, `probe-wrong_vnni_encoding.dis`,
  `probe-unreachable.dis`: relevant raw-byte function excerpts.
* `iotest.dis`: optimized I/O-probe worker/joiner function excerpts.
* `phase-map.md`: the twelve-entry compiler-phase audit.
