# Baseline runtime descriptor correctness review

Scope: b040190, fourteenth change in `driver/early_export.py` only.
No implementation or existing tests changed. Plugins excluded.

## Running findings

- `driver/mkcompiler.sh` built successfully. Default GOROOT was busy;
  use a private copy to avoid modifying another running compiler.
- Harness caveat: `tests/sb_ptrto.sh` ignores all three execution exit statuses
  (`|| true`) and accepts identical error output unless it contains `false` or
  `panic`. An incomplete private GOROOT produced a spurious
  `PASS pointer-type identity: 1 lines identical ...`. This is not evidence of
  descriptor correctness; rerun with a complete GOROOT and independently require
  successful compilation/execution. Existing harness left unchanged.
- Source: `reflect.(*rtype).ptrTo` explicitly handles zero `PtrToThis` by checking
  its cache and typelinks before creating a descriptor; `internal/abi.Type`
  documents that the field may be zero. Promoted-method guard still calls the
  stock `methods(tptr)` before deciding whether to skip.

## Source audit

Inspected the stock toolchain under `/usr/local/go/src`:

- `cmd/compile/internal/reflectdata/reflect.go`: `methods` calls `CalcMethods`
  and `IsMethodApplicable`, so promoted pointer/value/interface and unexported
  methods are considered, not just explicitly declared methods. No change to
  that calculation in this commit. Named types retain their descriptors.
- `reflect/type.go:1402–1443`: zero-offset fallback filters string matches using
  exact element type identity, and `ptrMap.LoadOrStore` canonicalizes concurrent
  lookups/creations. A synthetic pointer has no methods, matching the skipped set.
- `reflectdata.writeType` marks real unnamed pointers for typelinks; `TypeLinksym`
  queues independent strong demands. `NeedEmit` still emits unnamed types in
  each using package. DUPOK/type-symbol naming is unchanged.
- `cmd/link/internal/ld/deadcode.go` already ignores weak type-offset edges;
  `data.go` already resolves unreachable weak targets to zero. Neither requires
  a speculative pointer descriptor. Runtime-owned basic-type descriptors and
  the compiler's special `WriteBasicTypes` handling are unchanged.
- An exhaustive `PtrToThis` search in runtime, reflect, internal/reflectlite,
  internal/abi and cmd/link found no second runtime reader requiring nonzero.
  ABI's field explicitly permits zero. `Noalg` descriptors are excluded from
  typelinks, but are compiler-private synthetic types (closure/method contexts,
  map groups/backing storage), not ordinary source-visible reflected types.

These are bounded source-audit conclusions, not a proof for every program or
build configuration. Plugins and unsafe inspection of raw ABI bytes are not
behavioral identity tests for this review. No performance trace regenerated:
this review changes no build phase.

## Confirmed runs so far

- `bash driver/mkcompiler.sh`: successful (`go1.27.3 linux/amd64`). Its generated
  `driver/early_export.diff` changed timestamps only; restored to HEAD.
- `SB_GOROOT=/tmp/strand-ptr-goroot bash tests/sb_ptrto.sh`: **PASS, 39 lines**
  identical, with a complete private GOROOT.
- True stock compiler, baseline + `SB_KEEP_WEAKPTR=1`, baseline skip:
  targeted reflect pointer/method/runtime-constructor tests passed in each;
  full `internal/reflectlite` and `internal/abi` tests passed in each.
  Flags: `-vet=off -gcflags=all='-dwarf=false -l' -count=1 -timeout=120s`.
  Targeted regex:
  ```
  ^Test(PtrTo|PtrToMethods|PtrPointTo|TypeFor|EmbeddedMethods|UnexportedMethods|NestedMethods|Method|MethodValue|StructOf|StructOfWithInterface|StructOfEmbeddedIfaceMethodCall|FuncOf|ChanOf|ChanOfDir|MapOf|ArrayOf|ArrayOfDirectIface|ArrayOfGenericAlg|StructOfDifferentPkgPath|StructOfAnonymous)$
  ```
- Initial generated corpus: **123 element types**, all successful builds and
  executions, identical stdout in all three modes. Includes two packages,
  pointers emitted statically in one and reached via `PointerTo`/`New` in the
  other; reconstructed map/chan/func/array/slice/struct types; reflection-created
  values used in maps, channels and functions; 32 concurrent pointer-cache readers;
  8 pointer levels; Unicode field names, tags, variadics, zero-size arrays;
  counts/lengths surrounding 8, 16, 32, 64, 128, 256; embedding and private names.
- Expanded corpus: **635 element type expressions**, including 512 deterministic
  randomly generated nested types (seed 140040190, depth 1–4). All three modes
  built and ran successfully with identical output. Additional probes cover
  statically emitted private-field pointer types, private-only promoted methods,
  embedded interfaces, generic pointer receiver methods, ambiguous and
  field-shadowed method sets, and named pointer types.
- Mixed-package builds: keep descriptors in package `p` only, then in package
  `q` only; skip elsewhere. Both built/ran successfully and matched true stock.
  Separate cache directories are used for every mode so environment-controlled
  compiler changes cannot accidentally reuse another mode's archives.

## Result

**No phase-specific correctness counterexample found.** Consequently no input is
mislabelled as a failing counterexample. The generator and runner are retained
as review artifacts, not changes to the existing test suite. The false-positive
harness condition above is a validation caveat, not a descriptor defect.

Reproduce (from repository root, after creating a complete private stock GOROOT
and copying `build/compile-sb` over its compiler):

```
bash driver/mkcompiler.sh
SB_GOROOT=/tmp/strand-ptr-goroot bash tests/sb_ptrto.sh
bash reviews/runtime-descriptors-2026-11-12/run.sh /tmp/strand-ptr-review /tmp/strand-ptr-goroot
bash reviews/runtime-descriptors-2026-11-12/stdlib.sh /tmp/strand-ptr-goroot
```

`run.sh` leaves generated named inputs (`inputs/main.go`, `inputs/methods.go`,
`inputs/p/types.go`, `inputs/q/types.go`), binaries, build diagnostics, stdout,
stderr, and per-mode caches in its work directory. It requires successful
compilation and execution before comparing output, unlike the original harness.
Stock mode uses the actual `/usr/local/go` compiler, not merely the patched
compiler with baseline disabled. `stdlib.sh` logs individual command statuses and returns failure if any suite
fails.
Tests were executed on Linux/amd64 only; plugin and cross-architecture behavior
were not tested. No claim of exhaustive equivalence.

Final verification of the saved `stdlib.sh` runner also succeeded with fresh
caches: stock/keep/skip each returned status 0 for both the targeted reflect
suite and the full internal/reflectlite + internal/abi suites (nine package
results, all `ok`).

Final generator revision: **660 element type expressions**, adding direct
anonymous interfaces, the empty interface, `error`, and runtime-special
`func(error) string`. Interface element descriptors in package `q` are obtained
from function signatures, avoiding accidental direct static pointer demand in
that package. All stock/keep/skip and both mixed-package runs again passed with
identical output.
