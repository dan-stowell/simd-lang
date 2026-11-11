# Go lexer speed controls (audit 5)

Target `25a887e61ca9493711316ba368771ddce6dce63c`; installed Go `go1.27.3`.
All work is isolated here. `.build/` holds generated binaries/temporary inputs,
is gitignored, and must not be committed. No GOROOT edits.

## Reproduce

From this directory:

```
bash build.sh > logs/build.log 2>&1
python3 run.py --corpus /absolute/path/to/corpus.go --label final
```

**Do not start run.py until other audit campaigns are idle.** It randomizes
whole-process configurations within each of nine rounds and scan configurations
within three independent groups of seven scans. Seed, exact commands, environment,
input/executable hashes, exit statuses, every observation, and resource usage are
saved. There is no unreported warmup in the primary modes. Whole-process runs use
warm OS file cache (the corpus is hashed and checked first); no disk-cold claim.

`syntax/{scanner,source,tokens,token_string}.go` are byte-identical copies of
`/usr/local/go/src/cmd/compile/internal/syntax/`; their license is included.
`syntax/audit.go` adds a wrapper but does not replace scanner logic.
`build.sh` checks originals still match. `source-hashes.txt` records originals.
`main.go:scan` is copied verbatim from the project's reference.

## What is measured

* `original`: original reference scan, fresh FileSet/File, scanner Init, count
  non-EOF tokens, supplied error-counting callback, nil emit callback.
* `direct`: fresh FileSet/File, Init, direct count loop, nil error callback;
  checks Scanner.ErrorCount. This removes the per-token emit branch and closure.
* `handler`: direct loop with error-counting callback, to isolate callback setup.
* `reuse`: a scanner and File reused **after one untimed full scan**. Init still
  runs each repetition, but existing line-table storage is retained. Diagnostic,
  not a fresh-input baseline. The standard package explicitly supports reuse.
* `syntax`: new compiler scanner, init from bytes.Reader on already resident
  source, native tokens counted. Includes scanner's internal buffered copy and
  init, excludes FileSet, normalized go/token kinds, and byte offsets.
* `syntax-reuse`: same, but keeps scanner buffer following an untimed full scan.
* `syntax-dump`: whole process, reads input, builds line-start index, maps
  compiler token metadata to go/token, canonicalizes multiline-comment
  semicolon offsets, builds/writes the exact reference binary dump format.

Native compiler scanner has no go/scanner-style FileSet, no accumulated source
line table, no line-directive validation at mode 0, and no strings for skipped
comments or keywords. It still constructs identifier/literal strings and validates
lexical contents. Its classes combine operators and literals with extra metadata:
equal token counts alone do not prove equal kind/offset streams. `check` separately
compares mapped token kind and canonical offset for every token (including EOF),
reports raw offset differences, and hashes both streams. This normalization work
is deliberately **not** in native scanner-only timing.

`go/scanner` mode 0 skips returning comments, but the installed source still
constructs their string and parses line directives. It tracks line starts in a
token.File and constructs returned identifier/literal strings even when the caller
discards literal results. Strand is an offset/kind transducer, not a validator
with equivalent diagnostic coverage. Thus "faster than go/scanner" does not imply
"faster than Go's compiler scanner" or equal work per token.

The supplied error callback is **not a per-token callback**: valid input never
calls it. The separate optional emit callback is tested each iteration and is nil
in scan timing. Position conversion `int(pos)-file.Base()` occurs only in the
non-nil emit branch. Init resets scanner state and reads its first character; it
does not pre-scan the whole source. Most allocation/position work happens in Scan.
The installed token.File.AddLine takes a mutex on each call, even when reusing
an existing line table; reuse avoids table growth but not those calls.
`logs/escape-selected.txt` records Go compiler escape/inlining diagnostics:
the original scan is not inlined, and its closure/error counter escape.
This alone does not establish that closure overhead materially affects timing.

All scan modes time init+scan+count, exclude input read, and exclude timing/JSON/
memory-stat collection. They do **not** time just Scanner.Scan calls in isolation.
`runtime.ReadMemStats` is outside each interval but can affect collection scheduling;
the unchanged original `time FILE 7` is run at the end to check instrumentation.
JSON printing is deferred until all repetitions finish. Every measured scan records
token/error counts, bytes/objects allocated, GC count/pause, heap before/after,
GC policy and GOMAXPROCS.

GC controls: natural `GOGC=100`; explicit `runtime.GC` before each timed scan
(collection excluded); or GC disabled. A whole-process go/scanner scan loop is
serial, but when allowed eight CPUs its runtime/GC need not be single-threaded.
Both one-CPU and eight-CPU original scans/dumps are therefore recorded.
All affinity masks use CPU 0 through CPU 7, explicitly logged.

Whole-process wall includes taskset, GNU time, process startup, file input,
scanner, output formatting/writes to `/dev/null`, and teardown. Python's monotonic
wall measurement has finer resolution than GNU time's 0.01-second wall; the latter
is retained with user/system time, peak RSS and faults. Strand gets file stdin.
Go dump uses the **unchanged** project reference, including growing output slices,
`encoding/binary`, and output buffering—not a best possible output adapter.

Final timing results: pending coordinating agent authorization.

## Functional checks before timing

`go test ./...`: passes. Tests cover all keywords/operators, literal classes,
EOF, comments, Unicode identifiers, and multiline-comment semicolon positions.
The tiny 54-byte sanity input has one raw compiler semicolon offset difference;
canonicalized dumps match both the original reference and Strand byte-for-byte.
The correctness campaign's `small.go` (3,787,470 bytes) yields 675,686 tokens
with zero errors, zero mapped kind differences, and zero raw/canonical offset
differences. Its compiler-wrapper dump SHA256 is
`d6d6491961c8fbfb902b9f246a893c64a87ad474553b87ddb750921a3d5ede18`,
equal to the original reference (`logs/small-syntax-check.json`).
