# Instruction-encoding correctness review

Scope: `e689e6d`, `driver/x86/sbspan.go` and changes eleven/twelve in
`driver/early_export.py`. Reference: local stock `/usr/local/go`,
`go1.27.3 linux/amd64`. Read CHARTER.md and TRACE.md. Built the compiler
with `driver/mkcompiler.sh`; restored its generated `early_export.diff` so
implementation and existing tests remain unchanged. Review in progress.

## Confirmed: linking without `-w` panics [P1]

Minimal input: `cases/no-w.go` (an empty main).

Compile this one file with the baseline compiler, `-p main -dwarf=false -l`,
then use **the stock linker and stock runtime archives**, without `-w`.
The linker exits 2:

```
panic: trying to get oreader for invalid sym 0
cmd/link/internal/loader.(*Loader).Relocs
cmd/link/internal/ld.(*dwctxt).writedebugaddr
```

This is not a branch-sizing failure. `SB_KEEP_DWARF=1` alone fixes it;
`SB_ASM_MULTIPASS=1` alone does not. `-w` fixes it, as does compiling with
DWARF enabled. Stock and the old path (`SB_ASM_MULTIPASS=1 SB_KEEP_DWARF=1`)
link and run successfully in every tested combination. See
`logs/no-w-matrix.txt` and `logs/no-w-new-full.log`.

Change twelve returns before `dwarfSym` creates the per-function DWARF
symbols when `DebugInfo == nil`. In stock `cmd/link/internal/ld/dwarf.go`,
`writedebugaddr` (lines 2151–2165) unconditionally appends the function's
`infosym` and calls `Relocs` on it; missing info is symbol zero. Therefore
`-dwarf=false` is no longer compatible with the stock linker's default
DWARF generation, although stock supports that combination. This also
reproduces in a complete `build/sb` build (no custom object construction).

Reproduction from the repository root:

```sh
r=$PWD/reviews/instruction-encoding-2026-11-12
t=$(mktemp -d)
go list -deps -export -f '{{if .Export}}packagefile {{.ImportPath}}={{.Export}}{{end}}' runtime > "$t/importcfg"
SB_BASELINE=1 build/compile-sb -p main -dwarf=false -l -o "$t/main.a" "$r/cases/no-w.go"
go tool link -importcfg "$t/importcfg" -o "$t/main" "$t/main.a"
# Control: recompile with SB_KEEP_DWARF=1, or link the same object with -w.
```

## Existing differential/regression tests

- `tests/sb_stdtest.sh`, all 29 default packages: **27 pass** in both new
  and old paths; identical failures in `bytes.TestNewBufferShallow` and
  `unicode/utf16.TestAllocationsDecode` (allocation-count assertions).
  Both compile 46,493 functions by baseline and 826 by SSA. These are not
  regressions from the phase under review. Stock `go test -short -count=1
  -gcflags=all='-dwarf=false -l' -ldflags=-w bytes unicode/utf16` also
  reproduces both failures. Logs: `logs/std-{new,old}.txt` and
  `logs/stock-allocation-tests.txt`.
- `tests/sb_early.sh` on the minimal main module: PASS. Idle patched compiler:
  60 files and binary byte-identical to stock. Early export: 2,954 matching
  symbol names/sizes/types and matching section sizes. `logs/early.txt`.
- `tests/sb_driver.sh`: PASS both verbose-output and idle-mode regressions.
  `logs/driver.txt`.

## Generated-program differential checks

All linked with the stock linker, `-w`, and stock runtime archives; no
fork server, sharding, or other phase replacement. Each source was compiled
three ways: stock; `SB_BASELINE=1`; and
`SB_BASELINE=1 SB_ASM_MULTIPASS=1 SB_KEEP_DWARF=1`. Compile flags:
`-p main -dwarf=false -l`. Exit status and combined output were compared.

- Existing `tests/sbc_gen.py`, seeds 1–6, 150 functions/seed: **10,800
  matching results** (including recovered nil-pointer panics), 900 generated
  integer/boolean/struct/pointer functions. `SB_ASMSTAT=1` confirms the new
  path was active: all 11,772 assembled functions used one pass, while the
  old path used 2–5 passes. `logs/generated.json` records counts per seed.
  Sources can be regenerated with `python3 tests/sbc_gen.py SEED 150`.
- A generated boundary sweep of 81 functions with 0–80 repeated arithmetic
  and array-store groups inside a forward conditional, backward loops,
  switches, and stack slots on both sides of displacement 128: all
  **25,515 calls** produce the same accumulated checksum in all three
  modes. New path: 82 functions, one pass each; old path: 3–7 passes.
  `logs/boundaries.json`. This is a successful stress corpus, not a
  counterexample.
- Stock Go's assembler regression packages, rebuilt from the patched
  GOROOT: `go test cmd/internal/obj/x86 cmd/asm/internal/asm` PASS with
  `SB_BASELINE=1`; both also PASS with the old-path toggles and `-count=1`.
  `logs/asm-tests-{new,old}.txt`.

## Source audit (not a proof)

- No stale-index `Prog.Pc` use found: span6 assigns the current instruction's
  byte PC before calling `asmins`; backward targets have already been
  visited. Forward-fixup list entries were emitted earlier in that pass,
  so their PCs are byte offsets, not `sbPresize` indices. Ordinary instruction
  targets clear their `Rel` lists, and each pass resets relocation/output
  storage. Final jump-table
  relocations use the PCs after the relaxation loop completes.
- The branch relaxation loop was not removed. Clearing `branchShort` for
  predicted-long forward branches does not itself stop a remaining short
  branch from forcing another pass. Disabling JCC padding removes
  `Isize`-driven padding passes, not displacement-driven relaxation.
- pcln generation is a separate post-assembly operation, called before
  `populateDWARF` (`obj/plist.go:172–173`); the early return does not bypass
  it.

## Runtime metadata differential checks

Two additional generated programs match stock and old-path deterministic
output and exit status, with stock dependencies and `-w`:

- 96 varying-layout functions, 1,056 exact `runtime.Caller` and
  `CallersFrames` checks, and per-byte pcln scans.
- Depth-120 recursion with 64 pointer slots per frame, stack growth and
  forced GC; 40 pointer-payload panic/recover checks with `debug.Stack`.
- A 64-entry **actual SSA indirect jump table**, confirmed in objdump, all
  cases and defaults exercised. This is not baseline's linear switch.
- 192 nil/bounds/division panic-recovery checks, including exact fault
  source lines and GC in recovery; 100 SSA defer/recover calls.
- Concurrent SSA pointer loops, forced GC and all-goroutine tracebacks,
  with default async preemption and `asyncpreemptoff=1` controls.
  Snapshots did **not** capture literal `asyncPreempt` frames; no claim
  of direct async-preemption-frame validation.

See `logs/metadata.txt` for source hashes, recipes, outputs, path-selection
checks, and this coverage limitation. No pcln/stack-map or jump-table
regression was reproduced.
