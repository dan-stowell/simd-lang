# Every compiler-phase entry checked (af3ecc9)

Static/source-supported audit of `SWEETSPOT.md:236–269`, with small untimed
controls in `observations.json`. “Not implemented as a primitive/fused byte
pipeline”, “not expressible by any Strand program”, and “not fast” are different
claims. The table sometimes conflates them.

| Entry | Assessment and exact evidence |
|---|---|
| Comments and strings (`:242`) | **Supported for the stated toy grammar only.** `bench/programs.txt` implements double quotes, escapes and hash comments; the explicit disclaimer at `SWEETSPOT.md:159–165` is welcome. No complete programming-language lexer is demonstrated. The exact “C is 9 states” is unsupported here: no recogniser or dialect/translation-phase specification establishes that count. Wider/cascaded automata are proposed rather than demonstrated for a real language. |
| Token boundaries and kinds (`:243`) | **Expressible in restricted cases; measured evidence narrower.** `idents` counts starts of `[A-Za-z0-9_]` runs beginning in code according to that toy DFA. It does not emit token kinds or a token stream and accepts digit-leading runs as words. `src/compiler.strand:21–37` does compute boundaries and classes for Strand's own simple syntax, on dword lanes. Do not transfer the `idents` throughput ratio to a full tokeniser. |
| Complete scanner for a real grammar (`:244`) | **Overstated.** JSON extracts only `{ } [ ] : ,` outside strings; CSV counts separators outside quotes. These are useful real-format scanning tasks, not complete lexical scanners: no names, strings, numbers or token positions are emitted and syntax is not validated. The parser-derived JSON skeleton is a genuinely independent oracle for this task on the generated documents, not evidence of a complete lexer/parser. Untimed controls: `{"x":1}`, `{"different":false}` and invalid `{"x":}` all exit 0 and emit `{:}`. This is expected for extraction, not a compiler bug or a failure of its stated valid-input precondition. |
| Token positions (`:245`) | **True missing byte/DFA-to-dword bridge, false if read as no token positions anywhere.** `DESIGN.md:71–76` forbids persistent byte vectors; byte sinks are `out`, `outif`, `$S`, `end`. `boot/spec.py:50–59` and `tools/mkstencils.py:264ff` provide no byte STORE, `idx` or `scan`. Consequently a byte mask cannot simply be stored with `>mask` then read on a dword line; both direct-store and persistent-tee controls fail to compile. But `input 32 > dup prev 1 ^ & idx swap outif4` compiles and emits indices 0 and 4 for `one two\n`; the compiler already compresses token starts/tends with indices at `src/compiler.strand:26`. Correct claim: no direct way to emit dword positions from the new fused byte-automaton pipeline. |
| Identifier and keyword interning (`:246`) | **Implemented, with limitations honestly disclosed.** `src/compiler.strand:44–50` computes per-name hashes; `:54–91` uses a bounded scatter race, exact name comparisons, and keyword self-join. This is evidence that some joins are expressible, not a general graph/name-resolution implementation. The hardware microkernel figures do not prove an unavoidable application-performance limit; the measured self-hosted comparison is the relevant whole-program evidence. |
| Number literals (`:247`) | **Missing general primitive, not missing number parsing.** No general segmented affine scan stencil is supplied. Nevertheless the compiler already parses bounded decimal integer literals: `src/compiler.strand:44` applies `pow10` weights, scans and compresses token endpoints; `:45–50` checks length/lexical errors and subtracts prefix sums. It even explicitly calls this “a segmented sum ... a difference of two scan values.” “Not built” should mean the proposed segmented affine primitive or a general format parser, not number-literal processing altogether. |
| Bracket nesting depth (`:248`) | **Fair narrow limitation.** Dword `scan` expresses depth from known +1/−1 classifications. The byte DFA cannot feed it directly, for the bridge reason above. No independent, string-aware bracket-depth application is supplied. This is not proof that every multi-line formulation is impossible. |
| Matching brackets/building a tree (`:249`) | **No implementation shown; categorical impossibility not established.** No general stack/tree-building primitive exists and no real parser is measured. Postfix Strand avoids a conventional nested-syntax parser, but it still lexes, classifies, validates and scans stack depths (`src/compiler.strand:95–146`); “no parser” should not mean no syntactic analysis. Bounded/unrolled array algorithms are different from general dynamic tree traversal. |
| Name resolution/type checking (`:250`) | **Reasonable restriction with an explicit exception.** A flat compiler symbol table exists; general scope/graph/type algorithms are not supplied. This supports “not implemented or measured”, not a theorem that no join-shaped work fits: interning/keyword joins already do. |
| Dataflow optimisation (`:251`) | **General fixpoint limitation fair; specific implementation claim false.** A backward scan of associative gen/kill transfer functions is a plausible technique, but the compiler does **not** do liveness that way. `src/compiler.strand:162–164` recognises the last occurrence already selected as canonical by the scatter race and marks its vector dead. There is no backward gen/kill scan there. No general control-flow graph optimiser is shown. |
| Instruction selection/encoding (`:252`) | **Implemented in the restricted copy-and-patch sense.** `src/compiler.strand:118–121` selects preassembled stencil by class and stack depth; `:175–207` gathers stencil bytes and patches operands. This is not general machine-instruction synthesis or tree/DAG instruction selection. A whole-compiler slowdown is disclosed, but is not a measured timing of this phase alone. |
| Layout/relocation (`:253`) | **Implemented.** `src/compiler.strand:148–207` builds data/code offsets with scans, gathers pieces and scatters fixed-width operands. This is restricted ELF/stencil layout, not a general relaxing linker. Again the reported ratio is whole compiler, not phase timing. |

## Cross-document consequences

* `blog/04:76–81` appropriately says token positions **from a byte line**, but
  “everything after the lexer ... is outside the model” is too broad. The
  interning, validation, stack-depth allocation, stencil selection and layout
  above are already expressed in the model. The supported conclusion is that
  general optimising-compiler algorithms are not implemented and the particular
  whole self-hosted compiler is slower.
* `REPORT.md` Addendum 4 (`:258–266`) is narrower than “complete scanner”: it
  correctly calls JSON structural-character extraction and retains the toy
  grammar disclaimer. “The scanning end ... for grammars of at most four
  states” still must not imply a complete lexer or arbitrary four-state
  application's measured speed.
* `SWEETSPOT.md:283–284` says joins are “outside the model entirely” while
  `:246` and the compiler implement a join-shaped symbol mechanism. This is an
  internal wording contradiction. “No general join primitive / no demonstrated
  general graph walks” is defensible.
* Missing wider automata, a byte-to-dword bridge, and a general segmented-scan
  primitive are real implementation limits. Claims that those *primitives* are
  absent should not erase existing dword token positions or decimal parsing.
