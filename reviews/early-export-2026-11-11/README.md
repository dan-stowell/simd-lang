# Early-export correctness review

Scope: commit `561d3c2`, exclusively `driver/early_export.py` and the
`driver/main.go -pipeline` integration. No implementation or existing tests
changed. Applying `early_export.py` to fresh copies of the three stock source
files reproduced the compiler source used for these builds exactly. Reference: `/usr/local/go`, `go1.27.3 linux/amd64`.

All builds are inside `systemd-run --user --scope -p MemoryMax=8G`.
The existing test requests a nested 22G scope; the outer 8G cgroup remains the
ancestor limit. No MinIO or Kubernetes rebuilds, cache drops, or timing claims.

## Findings

No default-mode token, AST, type, or runtime-behavior discrepancy was found.
The confirmed differences below are in the new driver integration: one build
hang with verbose compiler output, and one broken idle-mode control.

### 1. [P2] Pipeline stops draining compiler stdout at 16 MiB and hangs

Counterexample: `cases/stdout/exponential_shape_name.go`.
This is a 381-byte valid program, not a 16 MiB source file: successive structural
type aliases double an instantiated generic function's expanded symbol name.
Use `-gcflags=-S` to make the compiler print that name.

`driver/main.go:790-800` sets a 16 MiB `bufio.Scanner` limit. When a compiler
output line exceeds it, `Scan` returns false. The code neither checks
`sc.Err()` nor drains/closes stdout before `cmd.Wait()`. Enough subsequent
output fills the pipe, blocking the compiler while the driver waits for it.
This is independent of the export-ready marker and affects even stock
compilers when invoked through the new `-pipeline` path.

The initial explicit-identifier probe pins the boundary: a 16,777,148-byte
identifier completes; a 16,777,149-byte identifier times out. Its first
assembly-output line has 68 bytes of overhead, making the latter line
16,777,217 bytes (one byte beyond the configured buffer). The next giant
line then fills the unread pipe. A SIGQUIT stack capture shows the driver
inside `runCompile` -> `os/exec.(*Cmd).Wait`, at line 800.
`logs/stdout-threshold.log`, `stdout-controls.log` and `stdout-wait-stack.log`
retain the boundary/control evidence.

Reproduction from the repository root (build/sb and build/compile-sb must
already exist):

```sh
src="$PWD/reviews/early-export-2026-11-11/cases/stdout"
systemd-run --user --scope -p MemoryMax=8G \
  timeout 60s "$PWD/build/sb" -C "$src" -pipeline \
  -compiler "$PWD/build/compile-sb" -j 2 -backends 4 \
  -gcflags=-S -o /tmp/strand-shape.bin
```

The 20-level, 397-byte version was also checked with stock `go build -S`
(via `-gcflags=-S`), non-pipelined sb with `-S`, and pipelined sb without
`-S`: all build and run successfully. Only pipeline plus `-S` times out
(exit 124 after 30 seconds). See `logs/shape20-controls.log`.

The retained input is reduced to 19 alias levels: stock `go build` with
`-gcflags=-S` succeeds; the patched pipeline times out after 40 seconds.
Removing one more alias level makes the pipeline build successfully with
`-S`. See `logs/shape-minimize.log`, `shape19-run.log`, and
`shape18-pipeline.log`. The retained 19-level file was also rerun from this
directory: timeout after 30 seconds; its SIGQUIT capture again shows
`runCompile` waiting in `cmd.Wait` (`logs/shape-final{,-stack}.log`).

This is a build-completion regression with `-S`, not a demonstrated wrong
binary under the normal TRACE.md flags. No implementation or test fix made.

### 2. [P3] `-noearly` does not override inherited `SB_EARLY_EXPORT`

Counterexample: `cases/baseline/inherited_noearly.go` (empty main).

At `driver/main.go:777-780`, `cmd.Env` inherits the whole environment.
`-noearly` only suppresses appending a new value; it does not remove or empty
an existing `SB_EARLY_EXPORT`. The patched compiler therefore still writes
early exports, signals early readiness, and uses name hashes. The supposedly
idle export files, objects and binary differ from stock.

```sh
src="$PWD/reviews/early-export-2026-11-11/cases/baseline"
systemd-run --user --scope -p MemoryMax=8G env SB_EARLY_EXPORT=1 \
  bash tests/sb_early.sh "$src"
```

Observed: exit 1, `FAIL patched compiler, idle`, with differences in
`_pkg_.x`, `_pkg_.a`, and `a.out`. Unsetting that environment variable makes
the identical command/input pass both existing checks.
See `logs/inherited-noearly-minimal.log` and `logs/existing-minimal.log`.
This violates the idle-mode byte-equality contract and compromises the
idle control; it is not a demonstrated runtime miscompile.

## Coverage

- Unmodified `tests/sb_early.sh` on `cases/baseline`: PASS. Idle compiler: 60
  compiler/export files and binary byte-identical. Early compiler: 2,954
  symbols identical in name, size and type, identical section sizes.
- Same script on a two-package program exercising named interfaces/assertions,
  generic structs/methods/aliases, promoted methods, reflection, range-over-func,
  closure state, defer and global map initialization: PASS. Idle: 120 files;
  early: 3,821 symbols and identical section sizes.
- Standard library short tests with both stock and early-export compilers: `reflect math/big
  sort slices maps unique iter encoding/gob`: all PASS using a fresh isolated
  cache, default inlining/debug flags, `CGO_ENABLED=0 GOMAXPROCS=2`, `-p 2`.
  A temporary toolexec adapter adds `-linkobj`, runs the patched compiler with
  `SB_EARLY_EXPORT=1`, and rejoins the two completed archives for stock cmd/go.
  This exercises export/hash semantics, not the driver's overlapping scheduler.
- Driver runtime comparisons with default inlining and with
  `-gcflags="-dwarf=false -l" -ldflags=-w`: PASS. Additional assembly declaration
  called directly and through a function value, linknamed function/global and
  an inline-returned closure: PASS. Both modes print the same results as stock.
- 95 standalone upstream Go programs: 95 PASS, no skips/build/runtime errors.
  65 are from `test/typeparam`, 30 from the root test directory. Includes
  dictionaries, generic interfaces/type switches, captures, promoted methods,
  aliases, defer/recover, and reflection. Complete source list and captures:
  `logs/upstream.json`.
- 27 generated boundary variants: all build and run identically. Declaration
  offsets: 127/128/129, 4095/4096/4097, 65535/65536/65537,
  1048575/1048576/1048577. Exported identifier lengths: 127/128/129,
  255/256/257, 4095/4096/4097, 65535/65536/65537. Also EOF without a newline,
  CRLF and leading BOM. Each variant exports a generic struct/method and a
  factory with a function-local named type captured by an inline-returned
  closure. Stock/early importer runtime output and exit status match.

Logs are retained in `logs/`. The short tests and standalone upstream corpus
use separate stock/early cache trees; the driver itself always recompiles.
This is targeted coverage, not exhaustive fuzzing or an AST/type-data
normalization comparison. No MinIO or Kubernetes correctness claim is made.

## Important experimental control

An exploratory toolexec run sharing an existing stock cache produced itab
panics in reflect/gob/JSON. A completely fresh early-only cache eliminated
these failures. This is **not** evidence of a cold Strand build failure:
name-hash and stock-index-hash artifacts cannot safely be mixed. No such
cache is used by the driver. The wrapper's stock version output did not
supply a distinct cache identity, so its initial mixed-cache run is excluded.

## Source inspection

The new anchor follows inlining, ABI-wrapper generation, escape analysis,
loop-variable transformation and init-task construction. Exported inline
bodies come from serialized frontend bodies, not walked backend IR. No
missing type/escape/inline field was confirmed. Hashing by name omits ABI and
static namespace distinctions that package indices carried; this remains an
unconfirmed concern, not a counterexample.
