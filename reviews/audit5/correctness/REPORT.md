# Correctness findings

Target: `25a887e61ca9493711316ba368771ddce6dce63c`.
Corpus equality is reproduced exactly; this does **not** establish a general
mode-0 `go/scanner` replacement. Three additional lexical failure mechanisms
were found beyond the documented EOF/NUL and CR/BOM exclusions.

Notation below: `(byte offset, token kind/name)`. Reference `dump` exits 0
even if scanning reports errors, so **scanner error count is checked separately**.
All new lexical counterexamples below have zero scanner errors, no CR/BOM/NUL,
and a final newline. Their actual exit status is **0**, not rejection.

## C1 — leading-dot floats after an identifier/keyword or a float

`x.0\n`:

- Expected: `(0,4/IDENT), (1,6/FLOAT), (3,57/;)`.
- Actual: `(0,4/IDENT), (1,53/.), (2,5/INT), (3,57/;)`.

`.0.0\n`:

- Expected: `(0,6/FLOAT), (2,6/FLOAT), (4,57/;)`.
- Actual: `(0,6/FLOAT), (2,53/.), (3,5/INT), (4,57/;)`.

These two small witnesses are valid **lexical snippets**, not complete
programs. A complete-program adjacency is `return.5`, or `switch.0` inside
a function. See `focused-results.json` for independent scanner, parser, and
type-check results and full offsets.

Cause in `examples/golex_gen.py`: `wstep(cl=7, s=ID)` and
`wstep(cl=7, s=DF)` return `N0`, rather than starting a new leading-dot
float. The dot is then classified as an operator, losing the float boundary.
Keyword recognition happens later, so a keyword is affected exactly like an
identifier.

## C2 — numeric runs absorb following identifiers or another literal

`0a\n`:

- Expected: `(0,5/INT), (1,4/IDENT), (2,57/;)`.
- Actual: `(0,5/INT), (2,57/;)`.

`1ii\n`:

- Expected: `(0,7/IMAG), (2,4/IDENT), (3,57/;)`.
- Actual: `(0,7/IMAG), (3,57/;)`.

The same family covers decimal exponents and hexadecimal/imaginary forms;
for example a suffix after `1e2` or `0x1p0` can disappear into the preceding
number. These are scanner-valid **lexical snippets**. No independently
type-valid complete program was found whose mismatch requires this mechanism.

Cause: number states accept general letter classes, repeatedly accept
exponent-like letters, and do not terminate after imaginary suffix `i`.
The byte automaton never emits the token boundary that `scanNumber` does.
This is not merely missing validation of an erroneous numeric literal:
the reference accepts `0a` as two legal tokens and reports no error.

## C3 — ellipsis immediately before a digit loses the ellipsis

`...0\n`:

- Expected: `(0,48/...), (3,5/INT), (4,57/;)`.
- Actual: `(2,6/FLOAT), (4,57/;)`.

This is a scanner-valid **lexical snippet**, not a complete program.
`..\n` and `...\n` by themselves pass. `..0\n` is a useful distinct
longest-match control: PERIOD then FLOAT.

Cause: `.digit` detection marks the third dot as a number before the operator
automaton can complete `...`. The first two dots become a nonexistent
two-character operator with kind 0 and are removed by newline filtering.

## Confirmed documented limits and excluded classes

### Final byte belongs to a token: status 3

`x` expects `(0,4/IDENT), (1,57/;)` but exits **3 with no output**.
`+` and the fully closed string `""` are also refused; “token” includes
operators and closed literals, not just an unterminated construct.
`package p` is a complete, scanner/parser/type-valid file that is refused.
`package p ` succeeds without a newline, as do `x//end` and `x/**/`.
Empty input and a single newline both succeed with empty output.

This is acknowledged in GOLEX.md and the generator. Corpus normalization
appends 14 missing final newlines, so whole-corpus equality does not exercise
those original EOF cases.

### Leading BOM: actual corruption, but explicitly excluded

For UTF-8 bytes `ef bb bf 0a` (BOM plus newline), expected token stream is
empty; actual status is 0 with `(0,4/IDENT), (3,57/;)`.
A leading BOM before `package p\n` likewise contaminates the keyword and
first offset. The reference permits a leading BOM without errors; complete
program validation confirms that this is valid Go, but GOLEX.md explicitly
excludes BOM inputs. A non-leading BOM is separately erroneous input.

### CR: exclusion is conservative for tested offset/kind interface

All dedicated CR controls passed, including CR whitespace, CRLF, interpreted
strings/runes, raw strings, comments, line directives, no-final-newline
controls, and the excluded standard-library CR fixture
`internal/types/testdata/fixedbugs/issue39754.go`.
Programs translated from LF to CRLF passed. Some mixed campaign failures
contain CR, but their underlying bug is numeric adjacency, not CR handling.
No independent CR mismatch was found.

### NUL and invalid/nonletter Unicode: not scanner-error-free

Actual NUL bytes cause reference scanner errors, including inside comments and
strings. They are documented out of scope. Strand may silently omit an illegal
NUL token or otherwise produce a different stream while returning 0; it does
not diagnose the input. The focused evidence records exact outputs.
Escaped source forms such as `"\x00"` contain no actual NUL byte and pass.

Valid Unicode identifiers, including non-ASCII digits after a valid letter,
pass. Emoji/combining-mark identifier controls are scanner errors and must not
be presented as new failures of the scanner-error-free claim.

## Coverage counts and caveats

The first deterministic campaign tested 26,332 sources: 24,494 scan without
error, 1,838 are excluded scanner-error input. Of the scanner-error-free cases,
16,804 match and 7,690 diverge. The high divergence count is intentional
adversarial numeric adjacency, not an estimated real-world error rate.
There are 667 recorded minimal/control witnesses; repetitions and origins
remain traceable in JSON.

The initial complete-program set has 338 scanner/parser/type-valid cases:
319 pass, 9 hit documented EOF rejection, 10 hit excluded leading-BOM
behavior. The focused extension checks valid keyword/leading-dot programs
specifically so the distinction between arbitrary lexical snippets and valid
complete Go is not lost.

All 192 boundary/CR checks pass. They cover CPUs 1/2/3/8 in ordinary and eager
pool modes, all 128 prefix alignments, and >1 MiB tokens/comments. No
concurrency-specific discrepancy was observed. Test sources, decoding,
scanner diagnostics, return statuses, and reproducible commands are in this
directory; generated binaries, corpus, and full token files stay in `build/`.
