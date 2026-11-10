# Third audit: carry soundness

Revision: `af3ecc935319a4698b499c66044faff95ef0663d`, compared with `17e317d`.
No implementation files changed. No timing measurements were performed.

## Finding

**No successful-execution carry counterexample found.** The relaxed verification
is sound for the actual carry consumers, and the boundary certificate has a
straightforward induction argument that does not depend on convergence of the
speculative derivation. This is a local argument about the implemented pool and
pool-safe stencils, not a formal proof of the entire compiler.

In particular, "every arbitrary composition runs successfully in parallel" remains
false: accepted programs can fall back depending on input and thread count.
Whether the guesses become exact in a prescribed number of passes is a different
question from whether a *verified* pass is correct.

## Why the reduced checks are sufficient

`stencils/stub.S:539–552` preserves the true first range's incoming state;
`341–364` loads it without the speculative warm-up used for other workers.
`585–638` checks every subsequent boundary for every used carry. Consider the
observable state of each carry, not its entire register representation:

* **Dword `prev`:** only dword 15 of the old carry is read. The other 15 results
  come from the current input chunk (`tools/mkstencils.py:147`). Thus the new
  `and esi,0x8000` (`stub.S:607–613`) compares exactly the required old value.
* **Byte `prev`:** only byte 63 of the old register is read
  (`mkstencils.py:328–332`). The qword shift constructs the other byte predecessors
  from the current chunk. Comparing the last **dword**, as the stub does, also
  compares bytes 60–62 that are irrelevant. This is conservative, not unsound.
* **Automaton:** future emitted states depend only on the function's value at
  state 0 (`DESIGN.md:80–82`; `mkstencils.py:406–431`). With the xor-identity
  representation, this is byte 0 of the carry. The new check compares precisely
  those bytes (`stub.S:614–624`). Equality of all four function values is not
  necessary for continuing this execution.
* **Sum and max:** their carries are broadcasts in scalar execution
  (`mkstencils.py:140–146`); the pool still compares all sixteen dwords. No
  weakening here.

For any accepted boundary, the predecessor's effective outgoing state equals
the successor's effective incoming state. The first range starts with the true
state, so induction over thread ranges makes the first range, then the second,
and so on identical to serial execution. The induction is valid for arbitrary
feed-forward compositions of these operators, not just independently associative
ones: the whole range is a deterministic transducer of all its incoming states.
It does **not** require every intermediate guess to be right.

Real-pass failed checks force fallback (`stub.S:583–584`, `833–835`), and output
is emitted only after successful verification (`714–739`); otherwise the caller
state is restored and the block is rerun serially (`707–712`). This also relies
on the existing pool-veto rules: no scatter, compress-to-memory, or persistent
tee reread is allowed to introduce an unverified cross-range memory dependency.
The argument is not a claim that this audit exhaustively re-proved those rules.

## Why a full function can be recovered after real execution

Let `P_e(s) = s xor e`. At real-pass entry, broadcasting the carry's low byte
(`stub.S:365–370`) changes the represented incoming automaton function to `P_e`.
It is a permutation for **any** state `e`; the original table transitions do not
need to be permutations or injective.

For a particular realized range transition function `M`, the reported outgoing
function is `M o P_e`. If a new proposed entry function is `I`, the retry derives

```
(M o P_e) o (P_e o I) = M o I
```

because `P_e o P_e` is identity. The xor by the old entry byte at
`stub.S:697–700` implements the inner `P_e`; `702–704` composes the functions.
This peeling does not attempt to invert `M`, so constant/non-injective
transitions are not an objection. It describes the range's actual classes in
that pass; upstream-dependent classes can still change in the next pass.

For a sum, `IN_new + OUT_old - IN_old`, modulo 2^32
(`stub.S:682–683`), likewise recovers that pass's range contribution. It need not
be the *correct* contribution until its upstream streams are correct.

For max, `max(IN,M)` cannot reveal `M` when an over-high `IN` hides it.
`stub.S:647–651,673–676` therefore alternates a missed max into a low-seeded
pass, then derives from that pass's range maxima. Thread 0 remains true-seeded;
other workers may be deliberately wrong in the low pass. Acceptance still
requires all the boundary checks. There is no algebraic justification for
simply subtracting a max seed, and the implementation does not do that.

## `prev` composition bound, and retry limits

A line cannot contain arbitrarily many `prev` operators. The front end rejects
duplicate `(carry group, stack depth)` pairs (`boot/strand.py:180–186`);
`prev`, `scan`, and automata share group 1 (`boot/spec.py:100–103`).
There are only eight stack depths. Therefore at most eight `prev` tokens can
occur on an accepted line, versus sixteen dwords or sixty-four bytes per chunk.
For a plain line, eight one-element delays fit in one preceding warm-up chunk.
Adding a max at the same depth is permitted but does not add another element
of delay. On lines with nonlocal carries, the incoming state before the warm-up
chunk is derived too (`stub.S:567–569`), and any wrong effective `prev` produced
by that warm-up is now checked.

The code's retry budget is `old_retry + 4 <= nthreads`
(`stub.S:641–646`): at four threads there is at most one retry, at eight at most
five, and at two/three no retry. This is not a universal "any chain converges"
claim. Dependencies, a max's extra low pass, data cancellation, and guesses
that happen to match can all change the path. `tests/pool.py:97–108,122` checks
hard-coded depths on its particular fixture; its "iff" comment should not be
read as a theorem over every input/program. `DESIGN.md:112–115` is best read as
"an additional dependency level can become exact", not "all upstream carries
are necessarily exact in the first real pass".

## Independent correctness probes

Run from the repository root:

```
python3 reviews/audit3-carries/probe.py
```

`probe-results.json` contains complete source strings, revision/compiler/stub
hashes, CPU masks, lengths, output hashes, and fallback-detector exit statuses.
Generated binaries and inputs are under `build/audit3-carries/`.

Result: **104 programs, 540 successful correctness comparisons; no mismatch.**
There are 520 small-input runs, including 104 single-CPU controls and 416 eager
multicore runs, plus 20 normal-mode production-size multicore controls.
The small runs compare directly with the scalar interpreter; production-size
runs compare with the same unmodified ELF pinned to one CPU.

The detector changes only a separate ELF's `fsbad` entry to `exit_group(77)`,
checking that the bytes match the current stub before patching. It observed
190 fallbacks among the 436 multicore runs, leaving 246 multicore runs without
fallback. This is not timing-based path inference.

Notable exact cases, with the deterministic inputs generated by `probe.py`:

| Case | Source / data | Observed pool path |
|---|---|---|
| Prior audit's failure to parallelize | `input 1 & scan dup prev dup prev + + out4`, 524289 bytes of `0x01` | No fallback on 2, 3, 4, 8 CPUs; output equals serial. |
| Nested carries and max | `input 128 - scan dup 0 swap scanmax + + out4`, 528513 bytes (`small.in` repeated 129 times) | Fallback on 2, 3, 4 CPUs, no fallback on 8; every output correct. |
| Max → prev → sum | `input 128 - scanmax dup prev dup scan + + out4`, same production fixture | Fallback on 2, 3; no fallback on 4, 8; every output correct. |
| Reduction after three sums | `input 1 & scan dup 3 & scan dup 7 & scan + + $S` then `~1 S + out4` | Fallback on 2, 3, 4; no fallback on 8; scalar reduction also correct. |
| Maximum prev depth | `input prev` / `bytes prev`, followed by seven `dup prev`, seven `+`, `out4` / `out` | Correct and no fallback in eager mode on 2, 3, 4, 8 CPUs. |
| Sixteen total carries | `input scanmax prev`, seven `dup scanmax prev`, seven `+`, `out4` | Correct on every tested CPU mask. |

`input prev prev out4` and the included ninth-prev source both fail compilation
with `E_CARRY` (10), rather than becoming counterexamples to the warm-up bound.

## Limits of the repository's all-256 test

`tests/fsm_all.py:22–24` includes every one of the 256 four-state transition
functions somewhere. It does **not** exhaust arbitrary automaton tables or
arbitrary ordered pairs of functions: each table contains only one block of
sixteen functions, so only 4096 of 65536 global ordered function pairs can occur
in a single table. Functions within each block have identical state-2 and
state-3 mappings. Cascaded automata are not tested by this source; it is one
automaton followed by `prev`.

Its generated input is 103022 bytes, below the ordinary 524288-element fork
threshold (`stub.S:58`). Thus its default all-CPU run is serial; only eager mode
actually exercises the pool (`fsm_all.py:29–30`). Neither output comparison
asserts absence of fallback.

The generation loop (`fsm_all.py:16–18`) inserts `shift % 7 + 1` zero bytes,
not a construction guaranteeing every ordered pair at all 64 offsets.
Consequently the docstring's exhaustive pair-position/boundary statement is
stronger than the construction establishes. The parent audit independently
measured 16350/16384 class-pair offsets, with 34 missing. These coverage gaps do
not themselves show a runtime miscompile; they limit the test's evidentiary
scope. The separate permutation/cascade pool tests and the probes here provide
additional, still finite evidence.
