# golex correctness evidence (HEAD 25a887e)

Scope: generated lexer and the `go/scanner` reference, not compiler changes.
All executables, input corpora, and large raw token streams are under
`build/audit5-correctness/` and are not audit deliverables.

## Reproduce

From the repository root (Go 1.27.3, Linux/amd64, CPU affinity 0–7):

```sh
mkdir -p build/audit5-correctness
go build -o build/audit5-correctness/golex explore/golex/main.go
build/stage2 < examples/golex.strand > build/audit5-correctness/golex_strand
chmod +x build/audit5-correctness/golex_strand
python3 examples/golex_gen.py | cmp - examples/golex.strand
find "$(go env GOROOT)/src" -name '*.go' -type f | sort > build/audit5-correctness/gofiles.list
build/audit5-correctness/golex corpus build/audit5-correctness/gofiles.list > build/audit5-correctness/corpus.go
build/audit5-correctness/golex dump build/audit5-correctness/corpus.go > build/audit5-correctness/corpus.reference.tokens
for cpus in 0 0,1 0,1,2 0,1,2,3,4,5,6,7; do
  taskset -c "$cpus" build/audit5-correctness/golex_strand < build/audit5-correctness/corpus.go > build/audit5-correctness/corpus.actual.tokens
  cmp build/audit5-correctness/corpus.reference.tokens build/audit5-correctness/corpus.actual.tokens
done
go run reviews/audit5/correctness/corpus_manifest.go build/audit5-correctness/gofiles.list build/audit5-correctness/corpus-files.json
go build -o build/audit5-correctness/oracle reviews/audit5/correctness/oracle.go
python3 reviews/audit5/correctness/probe.py > reviews/audit5/correctness/campaign.log 2>&1
python3 reviews/audit5/correctness/boundaries.py > reviews/audit5/correctness/boundaries.log 2>&1
python3 reviews/audit5/correctness/focused.py > reviews/audit5/correctness/focused.log 2>&1
```

The `test.sh` golex subsection was also reproduced independently, using its
`src/go`, `src/strconv`, and `src/math` file selection and a separate output
directory. It passed on CPU 0 and CPUs 0–7: 536 files, 3,787,470 input bytes,
675,686 tokens. Parent audit runs the full `test.sh`.

## Corpus claim: reproduced

- 8,084 included files, **92,537,748 bytes**.
- Source SHA256:
  `16b788e6b994e0c214e8b7cd9b02c52640fc8e56a882eb355c4f216a7baffb91`.
- Reference reports **zero scanner errors** and **17,047,917 tokens**.
- CPU counts **1, 2, 3, 8**: status 0 and byte-identical offsets **and** kinds.
- All token outputs: **85,239,585 bytes**, SHA256
  `cf46fde239f3b2e91d1f864ef4565515520483e80a6bb59e78df4cd990556a06`.
  Serialization is 4-byte offsets followed by 1-byte kinds, not two dword
  vectors. GOLEX.md's “136 MB of output” does not describe this output format.
- Of 8,087 candidate paths, two scanner-error fixtures and one CR-containing
  fixture are excluded. **14 retained files receive an appended newline.**
  See `corpus-inventory.json` for paths/reasons.
- Generator output exactly matches `examples/golex.strand`.

## Campaign and evidence layout

`probe.py` generates 26,332 deterministic cases, seed 25887. `oracle.go`
uses mode-0 `go/scanner`, preserves physical byte offsets, and additionally
checks marked complete programs with `go/parser` and `go/types`.
The generated source uses no imported packages for that validation.

The campaign executes 45,139 Strand processes including reduction trials.
Every scanner-error-free mismatch is reduced with scanner-validity,
actual exit status, trailing-newline presence, and NUL/CR/leading-BOM category
preserved. Reductions are **single-byte-deletion 1-minimal**, not necessarily
globally shortest: deleting a whole paired quote/comment can improve a
1-minimal string where deleting either byte alone introduces scanner errors.
Each failing origin has its deterministic case ID, source SHA256, and minimal
witness association recorded. Minima include exact hex/source bytes and
expected/actual decoded `(offset, kind)` streams. Input errors are counted
separately; explicitly erroneous controls are retained without minimization.

Coverage includes all integer radices, underscores, decimal and hexadecimal
floats with signed exponents, imaginary forms, numeric/operator adjacency,
all operator pairs, randomized longer operator sequences, `..`/`...`,
escaped/rune/raw strings and comment markers, valid Unicode identifiers,
semicolons, multiline comments, EOF, empty input, CR, and BOM.

Positive results include all **249 isolated numeric forms**, all **729 quoted
adjacencies**, **5,782 scanner-valid operator cases**, and valid Unicode
identifier controls. `boundaries.py` adds **192 passing checks** over 24
sources, CPUs 1/2/3/8, both ordinary and runtime eager-pool modes. It includes
128 byte alignments and >1 MiB identifiers, numeric literals, raw/escaped
strings, and comments. No CPU-dependent discrepancy was found.

See `REPORT.md` for findings and `focused-results.json` for small, independently
replayed witnesses and their parse/type-check classifications.

## Interpretation limits

- This is not a proof for all scanner-error-free input.
- Scanner-valid lexical snippets are not automatically syntactically valid
  complete programs. The inventory explicitly distinguishes them.
- Full corpus equality does not test original per-file EOF behavior: the
  reference concatenates sources and normalizes missing final newlines.
- No token literal values are compared: the advertised interface exposes only
  offsets/kinds. In particular CR removal from raw-string literal *values*
  cannot itself create a discrepancy in this interface.
- Input diagnostics are not a claimed output. NUL, invalid UTF-8, non-letter
  Unicode identifiers, and non-leading BOM controls are scanner-error input,
  not new counterexamples to the scanner-error-free claim.
- Elapsed campaign durations are execution logs, not performance benchmarks.
