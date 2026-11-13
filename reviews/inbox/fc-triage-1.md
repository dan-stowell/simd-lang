# fc mixed-mode failure triage

Stock toolchain: Go 1.27.3 linux/amd64, CGO disabled, `-gcflags=all=-l`. Compiler model reviewed in `driver/fc/main.go` and `driver/fc/gen.go`. No files under driver/ or tools/ were modified by this triage. Fresh run logs and standalone reductions are under `/tmp/fc-triage-1`.

All 49 FAIL rows are in scope; the four NOFC rows are outside the failing-test cohort and are not classified. Sections were appended as investigations finished, then reordered for dependency priority.

Reproduction commands: `taskset -c 4-7 tools/fc/test.sh <pkg> -count=1` and `CGO_ENABLED=0 go test -count=1 -gcflags=all=-l <pkg>`. Full output was retained instead of truncating it at 80 lines, so later failures were also examined. Times below are end-to-end wall times (including build); test-process times remain in the logs. The batch runner had a 180-second outer limit per command; any limit hit is called out explicitly.

The shared `build/fc` was rebuilt during this run (observed mtime 2026-11-12 23:26:53 UTC). Later standalone reductions use `/tmp/fc-triage-1/fc.snapshot`, SHA-256 `5088d5544b1968aa1e1789c324a02b0e039c18eb6b4ef8604fb850a0e51d7647`. Earlier package timings are from the live build, so they are not a single immutable-build benchmark. Original intrinsic crashes disappearing on rerun are identified explicitly. Concurrent edits to driver/fc/call.go and gen.go were observed; this triage did not edit either file.

Classification caveats: ENV follows the requested rule even when stock and fc fail for different reasons. Four pre-codegen build rejections are grouped under CRASH as the nearest available build-failure category, and explicitly distinguished from binary crashes. One historical CRASH (go/types) now passes and is not an open reproduced defect. The crypto/x509 BUG label denotes a stock-compatibility difference with an explicit unspecified-evaluation-order caveat, not a proven language violation.

## bytes — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestNewBufferShallow (0.00s)
    buffer_test.go:109: allocations occurred while shallow copying
FAIL
FAIL	bytes	0.989s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/bytes.fc`. Wall times: fc 6.38 s; stock 1.27 s. Confidence: high for the baseline failure.

## strings — BUG

Stock full suite passes (`ok strings`, wall 1.89 s); fc full suite fails (wall 2.80 s). Functional failures exclude ALLOC classification:

```text
--- FAIL: TestIndexFunc
IndexFunc("\xc0a\xc0", not IsValidRune) = -1; want 0
--- FAIL: TestContainsFunc
ContainsFunc("a☺b☻c☹d", func('☻')) = false, want true
```

Reduced callback comparison in `/tmp/fc-triage-1/predicate` (module `triage/predicate`, Go 1.27):

```go
package predicate

func Match(r rune, f func(rune) bool) bool {
 return f(r) == true
}
```

External test:

```go
package predicate_test
import (
 "testing"
 "triage/predicate"
)
func TestMatch(t *testing.T) {
 f := func(r rune) bool { return r == '\u263b' }
 if got := predicate.Match('\u263b', f); !got { t.Fatalf("Match(U+263B) = %v, want true", got) }
}
```

`tools/fc/test.sh -C /tmp/fc-triage-1/predicate triage/predicate -count=1`: `Match(U+263B) = false, want true`. Stock `CGO_ENABLED=0 go test -C /tmp/fc-triage-1/predicate -count=1 -gcflags=all=-l ./...`: PASS. Initialize a local git repository in the reduction directory to avoid the environment's invalid `/tmp/.git` VCS discovery.

Cause visible in `driver/fc/gen.go`, `(*gen).loadTo`: the `lReg` case copies the entire register without narrowing/zero-extending bool results. `driver/fc/expr.go`, `(*gen).comparison` and `(*gen).condJump`, compare word operands using 8-byte CMP even for bool. Stock callbacks may return a valid bool in AL while upper bits retain the rune input. U+263B therefore produces a false equality with true. Confidence: high; standalone differential confirmed. Do not skip functional tests. Allocation failures also appear in the full log but do not justify skipping the package.

## strconv — ALLOC

Full stock suite passes (wall 1.80 s). Full fc suite fails only `TestAllocationsFromBytes` subtests (wall 2.38 s):

```text
--- FAIL: TestAllocationsFromBytes/Atoi
strconv_test.go:99: got 1 allocs, want 0 allocs
--- FAIL: TestAllocationsFromBytes/ParseBool
strconv_test.go:102: got 1 allocs, want 0 allocs
```

The same assertion fails for ParseInt, ParseUint, ParseFloat, ParseComplex, CanBackquote, AppendQuote, AppendQuoteToASCII, AppendQuoteToGraphic. Regexp: `^TestAllocationsFromBytes$`. Confidence: high. Full evidence: `/tmp/fc-triage-1/strconv.fc`.


Full log: `/tmp/fc-triage-1/strconv.fc`.
## sync — CRASH

Full stock suite passes (wall 3.39 s); fc crashes (wall 3.81 s). First 15 lines:

```text
--- FAIL: TestOnceFunc (0.00s)
    oncefunc_test.go:29: want 0 allocations per call to f, got 1
--- FAIL: TestOnceValue (0.00s)
    oncefunc_test.go:55: want 0 allocations per call to f, got 1
--- FAIL: TestOnceValues (0.00s)
    oncefunc_test.go:81: want 0 allocations per call to f, got 1
panic: wlock(10001)
	

goroutine 3518 [running]:
sync_test.reader(0x2d62b91ba060, 0x2d62b8fc77d0?, 0x2d62b93b0e1c, 0x2d62b8f4c9a0)
	/usr/local/go/src/sync/rwmutex_test.go:63 +0xf9
created by sync_test.HammerRWMutex in goroutine 3499
	/usr/local/go/src/sync/rwmutex_test.go:102 +0x226
FAIL	sync	1.186s
```

Reduced underlying wrong-code case (in module `triage/predicate`):

```go
package predicate
import "sync/atomic"
func Negative(p *atomic.Int32) bool { return p.Add(1) < 0 }
```

External test:

```go
package predicate_test
import (
    "testing"
    "sync/atomic"
    "triage/predicate"
)
func TestNegative(t *testing.T) {
    var p atomic.Int32
    p.Store(-3)
    if got := predicate.Negative(&p); !got {
        t.Fatalf("Add(-3, 1) < 0 = %v, want true", got)
    }
}
```

fc: false; stock: true/PASS. `sync.(*RWMutex).RLock` uses the same `rw.readerCount.Add(1) < 0` condition to block readers while a writer is pending. A negative int32 returned in EAX is compared as a nonnegative 64-bit value, allowing readers through. `driver/fc/call.go`, `(*gen).emitCall`, returns a single scalar as `lReg` without extending it; `driver/fc/gen.go`, `(*gen).loadTo`, copies that register unchanged; `driver/fc/expr.go`, `(*gen).condJump`, uses 8-byte CMP. Confidence: high in the reduced defect and its applicability to RLock. The standalone reduction fails an assertion rather than crashing; the full package reproduces the crash. No skip proposed.

## reflect — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestMapIterSet (0.00s)
    all_test.go:390: wanted 0 alloc, got 1
FAIL
FAIL	reflect	1.929s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/reflect.fc`. Wall times: fc 3.79 s; stock 4.01 s. Confidence: high for the baseline failure.

## fmt — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestCountMallocs (0.03s)
    fmt_test.go:1532: Errorf("hello"): got 1 allocs, want 0
FAIL
FAIL	fmt	0.076s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/fmt.fc`. Wall times: fc 2.18 s; stock 1.39 s. Confidence: high for the baseline failure.

## os — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestLargeCopyViaNetwork (0.05s)
    copy_test.go:48: NewLocalListener error: tcp is not supported on linux/amd64
--- FAIL: TestSpliceFile (0.00s)
    --- FAIL: TestSpliceFile/Basic-TCP (0.00s)
        --- FAIL: TestSpliceFile/Basic-TCP/1 (0.00s)
            readfrom_linux_test.go:99: NewLocalListener error: tcp is not supported on linux/amd64
        --- FAIL: TestSpliceFile/Basic-TCP/42 (0.00s)
            readfrom_linux_test.go:99: NewLocalListener error: tcp is not supported on linux/amd64
        --- FAIL: TestSpliceFile/Basic-TCP/1025 (0.00s)
            readfrom_linux_test.go:99: NewLocalListener error: tcp is not supported on linux/amd64
        --- FAIL: TestSpliceFile/Basic-TCP/4097 (0.00s)
            readfrom_linux_test.go:99: NewLocalListener error: tcp is not supported on linux/amd64
```

Fresh fc log: `/tmp/fc-triage-1/os.fc`. Wall times: fc 22.89 s; stock 21.83 s. Confidence: high for the baseline failure.

## math/cmplx — BUG

Full stock suite passes (wall 0.42 s); fc fails (wall 1.07 s). Representative evidence:

```text
--- FAIL: TestAcos
cmath_test.go:964: Acos((0+0i)) = (1.5707963267948966-0i), want (1.5707963267948966-0i)
```

The misleading identical printed values arise from a miscompiled test helper, not evidence that Acos itself is inaccurate. `cmath_test.go:alike` compares `math.Signbit(a) == math.Signbit(b)`. Minimal differential:

```go
package predicate
import "math"
func SameSign(a, b float64) bool {
    return math.Signbit(a) == math.Signbit(b)
}
```

External test:

```go
package predicate_test
import (
    "testing"
    "triage/predicate"
)
func TestSameSign(t *testing.T) {
    if got := predicate.SameSign(-1, -1); !got {
        t.Fatalf("SameSign(-1, -1) = %v, want true", got)
    }
}
```

fc returns false; stock returns true/PASS. Same scalar-result normalization defect as strings and sync: `emitCall`, `loadTo`, and `comparison` in driver/fc/{call,gen,expr}.go. The left bool is stashed as one byte across the second call, while the right remains an unnormalized register result. Full reduced sources and execution logs: `/tmp/fc-triage-1/predicate`. Confidence: high. No skip proposed.

## math/big — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestNewIntAllocs (0.00s)
    int_test.go:2029: x.Add(x, NewInt(0)), wanted 0 allocations, got 1.000000
    int_test.go:2029: x.Add(x, NewInt(7)), wanted 0 allocations, got 2.000000
    int_test.go:2029: x.Add(x, NewInt(-7)), wanted 0 allocations, got 2.000000
    int_test.go:2029: x.Add(x, NewInt(1073741824)), wanted 0 allocations, got 2.000000
    int_test.go:2029: x.Add(x, NewInt(-1073741824)), wanted 0 allocations, got 2.000000
    int_test.go:2029: x.Add(x, NewInt(1125899906842624)), wanted 0 allocations, got 2.000000
    int_test.go:2029: x.Add(x, NewInt(-1125899906842624)), wanted 0 allocations, got 2.000000
FAIL
FAIL	math/big	8.317s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/math_big.fc`. Wall times: fc 4.48 s; stock 9.49 s. Confidence: high for the baseline failure.

## unicode/utf16 — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestAllocationsDecode (0.00s)
    utf16_test.go:138: Decode allocated 1 times
    utf16_test.go:138: Decode allocated 1 times
    utf16_test.go:138: Decode allocated 1 times
    utf16_test.go:138: Decode allocated 1 times
FAIL
FAIL	unicode/utf16	0.004s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/unicode_utf16.fc`. Wall times: fc 0.62 s; stock 1.33 s. Confidence: high for the baseline failure.

## encoding/binary — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 2.44 s; stock 0.87 s.

```text
--- FAIL: TestAppendAllocs (0.00s)
    binary_test.go:726: Append allocated 1 times instead of not allocating at all
FAIL
FAIL	encoding/binary	0.008s
FAIL
```

Skip regexp: `^(TestAppendAllocs)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/encoding_binary.fc`.
## encoding/json/jsontext — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestTokenStringAllocations (0.00s)
    token_test.go:34: Token.String allocated 1 times, want 0
FAIL
FAIL	encoding/json/jsontext	1.123s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/encoding_json_jsontext.fc`. Wall times: fc 3.38 s; stock 2.34 s. Confidence: high for the baseline failure.

## bufio — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.22 s; stock 0.68 s.

```text
--- FAIL: TestReadStringAllocs (0.00s)
    bufio_test.go:604: Unexpected number of allocations, got 2.000000, want 1
FAIL
FAIL	bufio	0.058s
FAIL
```

Skip regexp: `^(TestReadStringAllocs)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/bufio.fc`.
## container/list — BUG

Full stock suite passes (wall 0.42 s); fc fails (wall 0.97 s):

```text
--- FAIL: TestExtending
list_test.go:153: elt[0].Value = 1, want 1
--- FAIL: TestZeroList
list_test.go:153: elt[0].Value = 1, want 1
```

Reduced from the package's `checkList` helper; module `triage/predicate`:

```go
package predicate
func Check(x any, es []any) bool {
    le := x.(int)
    if le != es[0] { return false }
    return true
}
```

External test:

```go
package predicate_test
import (
    "testing"
    "triage/predicate"
)
func TestCheck(t *testing.T) {
    if !predicate.Check(1, []any{1}) {
        t.Fatal("Check(1, []any{1}) = false, want true")
    }
}
```

fc returns false; stock returns true/PASS. Command: `tools/fc/test.sh -C /tmp/fc-triage-1/predicate triage/predicate -count=1 -run TestCheck`. A simpler comparison against an interface parameter passes; indexing the interface slice is essential to this reduction. Suspected cause: `driver/fc/ops.go`, `(*gen).compareLocs`, converts/boxes the left concrete operand before preserving the right `lRegInd` operand; boxing can clobber the register holding the slice element address. Confidence: high in the wrong-code reduction, medium in the proposed cause. No skip proposed.

## context — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: ExampleAfterFunc_connection (0.00s)
got:
listen tcp 127.0.0.1:0: socket: operation not permitted
want:
context deadline exceeded
FAIL
FAIL	context	0.048s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/context.fc`. Wall times: fc 1.84 s; stock 1.29 s. Confidence: high for the baseline failure.

## crypto/ed25519 — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestAllocations (0.01s)
    ed25519_test.go:382: expected zero allocations, got 35.0
FAIL
FAIL	crypto/ed25519	0.211s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/crypto_ed25519.fc`. Wall times: fc 2.39 s; stock 1.63 s. Confidence: high for the baseline failure.

## crypto/md5 — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.02 s; stock 0.52 s.

```text
--- FAIL: TestAllocations (0.00s)
    md5_test.go:264: allocs = 4, want 0
FAIL
FAIL	crypto/md5	0.028s
FAIL
```

Skip regexp: `^(TestAllocations)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/crypto_md5.fc`.
## crypto/mldsa — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestAllocations (0.23s)
    mldsa_test.go:209: expected 7 allocations, got 91.0
FAIL
FAIL	crypto/mldsa	43.656s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/crypto_mldsa.fc`. Wall times: fc 38.73 s; stock 43.97 s. Confidence: high for the baseline failure.
## crypto/rand — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestAllocations (0.00s)
    rand_test.go:166: allocs = 1, want 0
FAIL
FAIL	crypto/rand	0.258s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/crypto_rand.fc`. Wall times: fc 1.28 s; stock 0.72 s. Confidence: high for the baseline failure.

## crypto/rsa — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestAllocations (0.15s)
    rsa_test.go:267: expected less than 10 allocations, got 5353.0
FAIL
FAIL	crypto/rsa	27.098s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/crypto_rsa.fc`. Wall times: fc 28.54 s; stock 27.87 s. Confidence: high for the baseline failure.

## crypto/sha1 — CRASH

Stock full suite passes. Wall times: fc 1.07 s; stock 0.47 s. First 15 available failure lines:

```text
runtime: frame crypto/sha1.blockAVX2 untyped args 0x1734d8225c40+0x20
fatal error: missing stackmap

runtime stack:
runtime.throw({0x6510d8?, 0x1?})
	/usr/local/go/src/runtime/panic.go:1243 +0x48 fp=0x7ffcef60a7a8 sp=0x7ffcef60a778 pc=0x499fe8
runtime.(*stkframe).getStackMap(0x7ffcef60a908, 0x0?)
	/usr/local/go/src/runtime/stkframe.go:222 +0x405 fp=0x7ffcef60a848 sp=0x7ffcef60a7a8 pc=0x47a5e5
runtime.adjustframe(0x7ffcef60a908, 0x7ffcef60a968)
	/usr/local/go/src/runtime/stack.go:762 +0x85 fp=0x7ffcef60a8e8 sp=0x7ffcef60a848 pc=0x478305
runtime.copystack(0x1734d83025a0, 0x1000)
	/usr/local/go/src/runtime/stack.go:1006 +0x2dc fp=0x7ffcef60a9c8 sp=0x7ffcef60a8e8 pc=0x478c3c
runtime.newstack()
	/usr/local/go/src/runtime/stack.go:1216 +0x4a6 fp=0x7ffcef60aaf0 sp=0x7ffcef60a9c8 pc=0x4791e6
runtime.morestack()
```

Reproduced with frozen compiler and `-run '^TestGolden$'`. The failing path is `testGolden → Sum → (*digest).Write → block → blockAVX2`; stack growth in the assembly function fails because argument stack-map metadata is missing. This is not allocation-only, despite the input log first reporting TestAllocations. Inspect `driver/fc/abiwrap.go`, `(*compiler).wrapToABI0`, and archive replacement/metadata handling in `driver/fc/main.go`; exact root cause remains unproven. Narrowed package test retained in `/tmp/fc-triage-1/crypto_sha1.narrow`. Confidence: high for CRASH.

Standalone reproduction, module `triage/asmmap`:

```go
package asmmap
func Large(p *int)
func Call(p *int) { Large(p) }
```

`p_amd64.s`:

```asm
#include "textflag.h"
TEXT ·Large(SB), $4096-8
 MOVQ p+0(FP), AX
 MOVQ $42, (AX)
 RET
```

External test:

```go
package asmmap_test
import (
 "testing"
 "triage/asmmap"
)
func TestCall(t *testing.T) {
 done := make(chan int)
 go func() { var x int; asmmap.Call(&x); done <- x }()
 if x := <-done; x != 42 { t.Fatalf("got %d, want 42", x) }
}
```

Frozen fc fails with `frame triage/asmmap.Large untyped args` / `fatal error: missing stackmap`; stock passes and writes 42. Run with `FC_BIN=/tmp/fc-triage-1/fc.snapshot tools/fc/test.sh -C /tmp/fc-triage-1/asmmap triage/asmmap -count=1`. This removes cryptography entirely: a pointer argument plus a stack-growing assembly frame suffices. Full failure: `/tmp/fc-triage-1/asmmap.fc`.

## crypto/sha256 — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestAllocations (0.00s)
    sha256_test.go:370: allocs = 8, want 0
--- FAIL: TestAllocatonsWithTypeAsserts (0.00s)
    sha256_test.go:485: allocs = 3; want = 0
FAIL
FAIL	crypto/sha256	0.038s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/crypto_sha256.fc`. Wall times: fc 1.07 s; stock 0.62 s. Confidence: high for the baseline failure.

## crypto/sha3 — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestAllocations (0.00s)
    --- FAIL: TestAllocations/New (0.00s)
        sha3_test.go:237: expected zero allocations, got 3.0
    --- FAIL: TestAllocations/NewSHAKE (0.00s)
        sha3_test.go:249: expected zero allocations, got 2.0
    --- FAIL: TestAllocations/Sum (0.00s)
        sha3_test.go:258: expected zero allocations, got 2.0
    --- FAIL: TestAllocations/SumSHAKE (0.00s)
        sha3_test.go:267: expected zero allocations, got 2.0
FAIL
FAIL	crypto/sha3	4.106s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/crypto_sha3.fc`. Wall times: fc 4.74 s; stock 4.43 s. Confidence: high for the baseline failure.

## crypto/sha512 — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestAllocations (0.00s)
    sha512_test.go:939: allocs = 14, want 0
FAIL
FAIL	crypto/sha512	0.068s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/crypto_sha512.fc`. Wall times: fc 1.02 s; stock 0.42 s. Confidence: high for the baseline failure.

## crypto/tls — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
Failed to open local listener: listen tcp6 [::1]:0: socket: operation not permitted
FAIL	crypto/tls	0.058s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/crypto_tls.fc`. Wall times: fc 3.69 s; stock 2.44 s. Confidence: high for the baseline failure.

## crypto/x509 — BUG

Classification is a **stock-compatibility defect, not a proven Go-language miscompile**; see the evaluation-order caveat below. Full stock suite passes (wall 36.07 s); fc fails (wall 28.58 s):

```text
--- FAIL: TestOIDMarshal
    oid_test.go:216: (*OID).UnmarshalText("0.1.2.3") = ; want = 0.1.2.3
```

The test's message is misleading: line 216 checks the result of `ParseOID`, not the separately unmarshaled value. The small package function is:

```go
func ParseOID(oid string) (OID, error) {
    var o OID
    return o, o.unmarshalOIDText(oid)
}
```

Standalone reduction in module `triage/predicate`:

```go
package predicate

type Value struct { Text string }
func (v *Value) set(s string) error { v.Text = s; return nil }
func Parse(s string) (Value, error) {
    var v Value
    return v, v.set(s)
}
```

External test:

```go
package predicate_test
import (
    "testing"
    "triage/predicate"
)
func TestParse(t *testing.T) {
    v, err := predicate.Parse("0.1.2.3")
    if err != nil || v.Text != "0.1.2.3" {
        t.Fatalf("got %q, %v; want 0.1.2.3, nil", v.Text, err)
    }
}
```

Frozen fc returns `Value{Text:""}, nil`; stock returns `Value{Text:"0.1.2.3"}, nil`. `driver/fc/stmt.go`, `(*gen).returnStmt`, eagerly copies each expression using `g.keep` before evaluating the next expression. Stock evaluates the mutating call before copying the variable result.

**Specification limit:** Go's “Order of evaluation” section explicitly leaves the ordering of ordinary operand reads relative to function calls unspecified, including in return statements. Its example `[]int{a, f()}` permits either the old or updated `a`. Therefore this reduction alone does not prove fc violates Go. The standard-library function depends on stock's choice; a portable implementation would call the mutator first, then return the updated value. BUG is used for the observed mixed-mode compatibility failure because none of the supplied categories describes an unspecified-order dependency. This is not an allocation or optimizing-compiler assertion. Confidence: high in the differential and its cause; low in calling it a compiler correctness bug. No skip proposed.

## database/sql — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.87 s; stock 2.5 s.

```text
--- FAIL: TestRawBytesAllocs (0.00s)
    convert_test.go:418: allocs = 14; want 0
--- FAIL: TestGrabConnAllocs (0.00s)
    sql_test.go:5295: Conn.grabConn allocated 2 objects; want 0
FAIL
FAIL	database/sql	0.184s
FAIL
```

Skip regexp: `^(TestRawBytesAllocs|TestGrabConnAllocs)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/database_sql.fc`.
## go/types — CRASH (original log; no longer reproduced)

Original log: `/tmp/fcbatch.s2QLzW/go_types`. First 15 lines:

```text
--- FAIL: TestHasher (0.00s)
    --- FAIL: TestHasher/Hasher (0.00s)
panic: intrinsic [recovered, repanicked]

goroutine 627 [running]:
testing.tRunner.func1.2({0x9c7db0, 0x380b9ba8e80})
    /usr/local/go/src/testing/testing.go:2125 +0x23e
testing.tRunner.func1()
    /usr/local/go/src/testing/testing.go:2128 +0x3ef
panic({0x9c7db0?, 0x380b9ba8e80?})
    /usr/local/go/src/runtime/panic.go:859 +0x136
internal/abi.EscapeNonString[...]()
    /usr/local/go/src/internal/abi/escape.go:48 +0x4a
hash/maphash.WriteComparable[...]()
    /usr/local/go/src/hash/maphash/maphash.go:307 +0x23
```

Fresh full fc suite PASSES (wall 19.90 s); full stock suite PASSES (wall 17.57 s). Thus CRASH describes only the supplied failure, not the present compiler. No current failure to reduce; no skip proposed. The intrinsic panic in the input has disappeared in the live build (hash/maphash likewise now reaches only allocation failures). Confidence: high in historical crash and fresh pass; do not treat as an open reproduced defect.

## hash/maphash — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 62.56 s; stock 13.96 s.

```text
--- FAIL: TestComparableAllocations (0.00s)
    maphash_test.go:441: got 2 allocs, want 0
    maphash_test.go:453: got 2 allocs, want 0
FAIL
FAIL	hash/maphash	61.474s
FAIL
```

Skip regexp: `^(TestComparableAllocations)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/hash_maphash.fc`.
## internal/abi — CRASH

Stock full suite passes. Wall times: fc 0.37 s; stock 3.51 s. First 15 available failure lines:

```text
# internal/abi [internal/abi.test]
/usr/local/go/src/internal/abi/type.go:709:6: nameLen escapes to heap, not allowed in runtime
/usr/local/go/src/internal/abi/type.go:710:6: tagLen escapes to heap, not allowed in runtime
FAIL	internal/abi [build failed]
FAIL
```

This input contradicts the premise that compilation succeeded: failure occurs before fc code generation, in the patched stock export-data compiler (`FC_ESC_COMPILE`), which forces escapes while runtime packages prohibit heap escapes. CRASH is used here as the closest build-failure category; it is a pre-link compiler rejection, not a test-binary crash. No allocation-test skip can fix it. No standalone wrong-code case is claimed. Confidence: high in the build-stage diagnosis; the supplied taxonomy has no exact compile-rejection category.

## internal/chacha8rand — CRASH

Stock full suite passes. Wall times: fc 2.13 s; stock 3.25 s. First 15 available failure lines:

```text
signal: segmentation fault
FAIL	internal/chacha8rand	0.001s
FAIL
```

Segmentation fault occurs before any test assertion is reported. This package also provides runtime random state, so replacement affects runtime startup, not just the test calls. The frozen build also segfaults with `-run '^$'`, confirming that no test body is required. No standalone reduction or exact cause established. Confidence: high for CRASH, low for cause.

## internal/runtime/cgroup — CRASH

Stock full suite passes. Wall times: fc 0.26 s; stock 3.35 s. First 15 available failure lines:

```text
# internal/runtime/cgroup [internal/runtime/cgroup.test]
/usr/local/go/src/internal/runtime/cgroup/cgroup_linux.go:156:6: b escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/cgroup/cgroup_linux.go:188:6: b escapes to heap, not allowed in runtime
FAIL	internal/runtime/cgroup [build failed]
FAIL
```

This input contradicts the premise that compilation succeeded: failure occurs before fc code generation, in the patched stock export-data compiler (`FC_ESC_COMPILE`), which forces escapes while runtime packages prohibit heap escapes. CRASH is used here as the closest build-failure category; it is a pre-link compiler rejection, not a test-binary crash. No allocation-test skip can fix it. No standalone wrong-code case is claimed. Confidence: high in the build-stage diagnosis; the supplied taxonomy has no exact compile-rejection category.

## internal/runtime/maps — CRASH

Stock full suite passes. Wall times: fc 0.32 s; stock 3.65 s. First 15 available failure lines:

```text
# internal/runtime/maps [internal/runtime/maps.test]
/usr/local/go/src/internal/runtime/maps/table.go:109:3: g escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/table.go:196:3: g escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/table.go:230:3: g escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/table.go:394:3: g escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/group.go:346:35: newGroup escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/group.go:346:45: oldGroup escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/map.go:466:2: g escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/map.go:557:2: g escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/map.go:622:2: g escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/map.go:631:2: g escapes to heap, not allowed in runtime
/usr/local/go/src/internal/runtime/maps/map.go:631:2: too many errors
FAIL	internal/runtime/maps [build failed]
FAIL
```

This input contradicts the premise that compilation succeeded: failure occurs before fc code generation, in the patched stock export-data compiler (`FC_ESC_COMPILE`), which forces escapes while runtime packages prohibit heap escapes. CRASH is used here as the closest build-failure category; it is a pre-link compiler rejection, not a test-binary crash. No allocation-test skip can fix it. No standalone wrong-code case is claimed. Confidence: high in the build-stage diagnosis; the supplied taxonomy has no exact compile-rejection category.

## internal/strconv — CRASH

Stock full suite passes. Wall times: fc 0.37 s; stock 6.25 s. First 15 available failure lines:

```text
# internal/strconv [internal/strconv.test]
/usr/local/go/src/internal/strconv/atof.go:622:6: dec escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/atof.go:680:6: dec escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/uscale.go:93:6: pre escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/uscale.go:116:6: pre escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/uscale.go:142:6: pre escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/uscale.go:204:6: pre escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/ftoa.go:139:7: buf escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/ftoa.go:184:8: buf escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/itoa.go:74:7: a escapes to heap, not allowed in runtime
/usr/local/go/src/internal/strconv/uscale.go:204:6: too many errors
FAIL	internal/strconv [build failed]
FAIL
```

This input contradicts the premise that compilation succeeded: failure occurs before fc code generation, in the patched stock export-data compiler (`FC_ESC_COMPILE`), which forces escapes while runtime packages prohibit heap escapes. CRASH is used here as the closest build-failure category; it is a pre-link compiler rejection, not a test-binary crash. No allocation-test skip can fix it. No standalone wrong-code case is claimed. Confidence: high in the build-stage diagnosis; the supplied taxonomy has no exact compile-rejection category.

## internal/zstd — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.63 s; stock 0.72 s.

```text
--- FAIL: TestAlloc (0.33s)
    zstd_test.go:268: got 12590 allocs, want 0
FAIL
FAIL	internal/zstd	0.582s
FAIL
```

Skip regexp: `^(TestAlloc)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/internal_zstd.fc`.
## log — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.23 s; stock 0.52 s.

```text
--- FAIL: TestDiscard (0.00s)
    log_test.go:215: got 5 allocs, want at most 1
FAIL
FAIL	log	0.075s
FAIL
```

Skip regexp: `^(TestDiscard)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/log.fc`.
## log/slog — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.63 s; stock 0.82 s.

```text
--- FAIL: TestAttrNoAlloc (0.00s)
    attr_test.go:40: got 5 allocs, want zero
--- FAIL: TestJSONAllocs (0.00s)
    --- FAIL: TestJSONAllocs/message (0.00s)
        json_handler_test.go:194: got 21 allocs, want 0
    --- FAIL: TestJSONAllocs/attrs (0.00s)
        json_handler_test.go:205: got 76 allocs, want 2
--- FAIL: TestAlloc (0.00s)
    --- FAIL: TestAlloc/Info (0.00s)
        logger_test.go:337: got 2 allocs, want 0
... (remaining allocation assertions in full log)
```

Skip regexp: `^(TestAttrNoAlloc|TestJSONAllocs|TestAlloc|TestTextHandlerAlloc|TestValueNoAlloc|TestAnyLevelAlloc)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/log_slog.fc`.
## log/slog/internal/buffer — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 0.92 s; stock 0.32 s.

```text
--- FAIL: TestAlloc (0.00s)
    buffer_test.go:38: got 1 allocs, want 0
FAIL
FAIL	log/slog/internal/buffer	0.006s
FAIL
```

Skip regexp: `^(TestAlloc)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/log_slog_internal_buffer.fc`.
## mime — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.07 s; stock 0.47 s.

```text
--- FAIL: TestLookupMallocs (0.01s)
    type_test.go:183: allocs = 5; want 0
FAIL
FAIL	mime	0.015s
FAIL
```

Skip regexp: `^(TestLookupMallocs)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/mime.fc`.
## net/http/internal — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.27 s; stock 0.42 s.

```text
--- FAIL: TestChunkReaderAllocs (0.00s)
    chunked_test.go:137: mallocs = 4; want 1
FAIL
FAIL	net/http/internal	0.354s
FAIL
```

Skip regexp: `^(TestChunkReaderAllocs)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/net_http_internal.fc`.
## net/netip — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 2.17 s; stock 1.47 s.

```text
--- FAIL: TestNoAllocs (0.01s)
    --- FAIL: TestNoAllocs/AddrFrom16 (0.00s)
        netip_test.go:2235: allocs = 1; want 0
    --- FAIL: TestNoAllocs/ParseAddr/4 (0.00s)
        netip_test.go:2235: allocs = 1; want 0
    --- FAIL: TestNoAllocs/ParseAddr/6 (0.00s)
        netip_test.go:2235: allocs = 2; want 0
    --- FAIL: TestNoAllocs/MustParseAddr (0.00s)
        netip_test.go:2235: allocs = 1; want 0
    --- FAIL: TestNoAllocs/IPv6LinkLocalAllNodes (0.00s)
... (remaining allocation assertions in full log)
```

Skip regexp: `^(TestNoAllocs|TestAddrStringAllocs)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/net_netip.fc`.
## net/textproto — ALLOC

Full stock suite passes; full fc suite fails only allocation-count assertions. Wall times: fc 1.13 s; stock 0.42 s.

```text
--- FAIL: TestCommonHeaders (0.00s)
    reader_test.go:471: canonicalMIMEHeaderKey allocs = 1; want 0
FAIL
FAIL	net/textproto	0.020s
FAIL
```

Skip regexp: `^(TestCommonHeaders)$`. Confidence: high.


Full log: `/tmp/fc-triage-1/net_textproto.fc`.
## regexp — BUG

Full stock suite passes (wall 13.27 s); fc fails (wall 25.62 s). Evidence:

```text
--- FAIL: TestRE2Exhaustive
exec_test.go:172: testdata/re2-exhaustive.txt:5730872: `\b`.FindSubmatchIndex(``) = [0 0], want []
--- FAIL: TestRE2Search
```

This is incorrect word-boundary matching, not merely slowness. `regexp/exec.go`, `(lazyFlag).match`, compares the two results of `syntax.IsWordChar`. Reduced in module `triage/predicate`:

```go
package predicate
import "regexp/syntax"
func Boundary(r1, r2 rune) bool {
    return syntax.IsWordChar(r1) != syntax.IsWordChar(r2)
}
```

External test:

```go
package predicate_test
import (
    "testing"
    "triage/predicate"
)
func TestBoundary(t *testing.T) {
    if predicate.Boundary(-1, -1) {
        t.Fatal("Boundary(-1, -1) = true, want false")
    }
}
```

fc returns true for two EOF runes; stock returns false/PASS. Same bool register-result normalization issue as math/cmplx: `driver/fc/call.go:emitCall`, `gen.go:loadTo`, `expr.go:comparison`. The first bool is stashed and narrowed, while the second remains in its return register. Confidence: high. No skip proposed.

## runtime — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestInlineUnwinder (0.00s)
    symtabinl_test.go:86: tiuTest+0x0: tiuTest:9
    symtabinl_test.go:86: tiuTest+0xe: tiuTest:10
    symtabinl_test.go:86: tiuTest+0x15: tiuTest:11
    symtabinl_test.go:86: tiuTest+0x1a: tiuTest:12
    symtabinl_test.go:86: tiuTest+0x21: tiuTest:13
    symtabinl_test.go:86: tiuTest+0x27: tiuTest:9
    symtabinl_test.go:97: missing stack tiuInlined1:3 tiuTest:10
    symtabinl_test.go:97: missing stack tiuInlined1:3 tiuInlined2:6 tiuTest:11
    symtabinl_test.go:97: missing stack tiuInlined2:7 tiuTest:11
--- FAIL: TestMkmalloc (0.03s)
    malloc_test.go:850: test output:
```

Fresh fc log: `/tmp/fc-triage-1/runtime.fc`. Wall times: fc 1.02 s; stock 96.51 s. Confidence: high for the baseline failure.

## runtime/pprof — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestGenericsInlineLocations (0.14s)
    protomem_test.go:229: expected a location with at least 3 functions
        runtime/pprof.nonRecursiveGenericAllocFunction[go.shape.struct {},go.shape.struct { runtime/pprof.buf [128]uint8 }];runtime/pprof.nonRecursiveGenericAllocFunction[go.shape.struct { runtime/pprof.buf [128]uint8 },go.shape.struct {}];runtime/pprof.storeAlloc
        got
        runtime/pprof.storeAlloc
FAIL
FAIL	runtime/pprof	73.631s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/runtime_pprof.fc`. Wall times: fc 1.38 s; stock 74.33 s. Confidence: high for the baseline failure.

A standalone linker reduction was completed while the stock baseline was still running. It is retained here as supplementary evidence; the package classification remains ENV under the requested baseline rule. Module `triage/pull`:

```go
package pull
import "iter"
func First() (int, int, bool) {
 next, stop := iter.Pull2(func(yield func(int, int) bool) { yield(1, 2) })
 defer stop()
 return next()
}
```

External test:

```go
package pull_test
import (
 "testing"
 "triage/pull"
)
func TestFirst(t *testing.T) {
 a, b, ok := pull.First()
 if a != 1 || b != 2 || !ok { t.Fatalf("got %d %d %v", a, b, ok) }
}
```

Frozen fc fails to link:

```text
# triage/pull.test
iter.Pull2[int,int]: relocation target iter.newcoro not defined
iter.Pull2[int,int].func1.func1: relocation target iter.coroswitch not defined
iter.Pull2[int,int].func2: relocation target iter.coroswitch not defined
iter.Pull2[int,int].func3: relocation target iter.coroswitch not defined
FAIL	triage/pull [build failed]
FAIL
```

Stock standalone test passes, returning `(1, 2, true)`. Cause visible in `driver/fc/misc.go`, `(*Program).linknames`: `//go:linknamestd` records only the local name and discards its third-field target. `iter/iter.go` declares `//go:linknamestd newcoro runtime.newcoro` and similarly coroswitch, but instantiated fc code instead references nonexistent `iter.newcoro` and `iter.coroswitch`. Confidence: high in this separate reduced linker defect. No skip proposed.

## testing — ENV

Stock baseline fails; classified ENV and investigation stopped under the requested rule. This does not establish that additional fc failures are environmental.

```text
--- FAIL: TestBenchmarkBLoopAllocs (0.25s)
    --- FAIL: TestBenchmarkBLoopAllocs/call-result (0.13s)
        benchmark_test.go:201: want 0 allocs, got 1
FAIL
FAIL	testing	4.009s
FAIL
```

Fresh fc log: `/tmp/fc-triage-1/testing.fc`. Wall times: fc 7.29 s; stock 4.69 s. Confidence: high for the baseline failure.

## text/scanner — BUG

Stock full suite passes (wall 0.37 s); fc fails (wall 1.07 s):

```text
--- FAIL: TestIssue50909
scanner_test.go:932: got "hello world!" (n = 10); want "hello world!" (n = 3)
```

The scan loop uses `s.Scan() != EOF`, where EOF is rune(-1). It fails to terminate because a negative rune register result is not sign-extended before 64-bit comparison. Reduced in module `triage/predicate`:

```go
package predicate
func NotEOF(f func() rune) bool { return f() != -1 }
```

External test:

```go
package predicate_test
import (
    "testing"
    "triage/predicate"
)
func TestNotEOF(t *testing.T) {
    if predicate.NotEOF(func() rune { return -1 }) {
        t.Fatal("NotEOF = true, want false")
    }
}
```

fc returns true; stock returns false/PASS. Same defect as sync: `emitCall` in call.go / `loadTo` in gen.go retain unextended scalar registers, and `comparison` / `condJump` in expr.go use 8-byte comparisons. Confidence: high. No skip proposed.

## text/template — CRASH

Fresh stock full suite passes (`ok text/template 0.396s`); frozen-fc full suite crashes. First 15 lines:

```text
fatal error: unreachable method called. linker bug?

goroutine 20 gp=0x3bd44304960 m=0 mp=0x813fe0 [running]:
runtime.throw({0x625715?, 0x3bd44316d18?})
	/usr/local/go/src/runtime/panic.go:1243 +0x48 fp=0x3bd4449a930 sp=0x3bd4449a900 pc=0x495c28
runtime.unreachableMethod()
	/usr/local/go/src/runtime/iface.go:729 +0x1f fp=0x3bd4449a950 sp=0x3bd4449a930 pc=0x422abf
runtime.call16(0x3bd444851d0, 0x3bd443028a0, 0x0, 0x0, 0x0, 0x8, 0x3bd4449ab68)
	/usr/local/go/src/runtime/asm_amd64.s:804 +0x3a fp=0x3bd4449a970 sp=0x3bd4449a950 pc=0x49ac3a
runtime.reflectcall(0x79c728?, 0x3bd4435a008?, 0x1?, 0x4449ab68?, 0x3bd?, 0x1?, 0x1?)
	<autogenerated>:1 +0x2d fp=0x3bd4449a9b0 sp=0x3bd4449a970 pc=0x49e7ed
reflect.Value.call({0x79c728?, 0x3bd4435a008?, 0x10?}, {0x615517, 0x4}, {0x832d60, 0x0, 0x457218?})
	/usr/local/go/src/reflect/value.go:586 +0xbac fp=0x3bd4449aee0 sp=0x3bd4449a9b0 pc=0x4e69ec
reflect.Value.Call({0x79c728?, 0x3bd4435a008?, 0x3bd44316cb8?}, {0x832d60, 0x0, 0x0})
	/usr/local/go/src/reflect/value.go:369 +0x92 fp=0x3bd4449af48 sp=0x3bd4449aee0 pc=0x4e5df2
```

Reduced package `triage/reflectcall`:

```go
package reflectcall
import "reflect"
func Invoke(x any, name string) string {
 return reflect.ValueOf(x).MethodByName(name).Call(nil)[0].String()
}
```

External test:

```go
package reflectcall_test
import (
 "testing"
 "triage/reflectcall"
)
type Item struct{}
func (Item) Hello() string { return "hello" }
func TestInvoke(t *testing.T) {
 if got := reflectcall.Invoke(Item{}, "Hello"); got != "hello" { t.Fatalf("got %q", got) }
}
```

`FC_BIN=/tmp/fc-triage-1/fc.snapshot tools/fc/test.sh -C /tmp/fc-triage-1/reflectcall triage/reflectcall -count=1` crashes with the same `unreachable method called` fatal error. Stock passes and returns "hello". The method exists in the type metadata but its code was eliminated by the linker.

Cause: missing reflection reachability metadata at the fc call site. Stock `cmd/compile/internal/walk/expr.go:usemethod` marks dynamic `MethodByName` callers with `obj.AttrReflectMethod` (or emits `R_USENAMEDMETHOD` for constant names); no equivalent handling was found in driver/fc. Relevant fc work belongs in call lowering/object-symbol emission (`driver/fc/call.go`, `(*gen).call`, and `driver/fc/obj.go`). Confidence: high for crash and reduced trigger, medium-high for missing reachability marker. No skip proposed.

## unique — LIVENESS

Full stock suite passes (wall 0.82 s); fc fails (wall 7.55 s). The functional failures require collection before the test function returns:

```text
--- FAIL: TestCanonMap/LoadOrStore/Sequential
canonmap_test.go:66: expected key 0 to be missing from map, got 0x16560aa7c7e0
```

`testCanonMap` explicitly clears `refs`, drains the cleanup queue, then requires weak map entries to disappear. fc's function-wide pointer maps retain temporary roots. TestCanonMapBadHash and TestCanonMapTruncHash fail in the same way; TestHandle, TestMakeClonesStrings and TestNestedHandle also check weak/cleanup reclamation. The run additionally fails only allocation assertions in TestMakeAllocs, so LIVENESS is the single package classification (not ALLOC).

Proposed liveness regexp: `^(TestCanonMap|TestCanonMapBadHash|TestCanonMapTruncHash|TestHandle|TestMakeClonesStrings|TestNestedHandle)$`; separate allocation regexp: `^TestMakeAllocs$`. Confidence: high for the liveness/alloc assertion categories, medium-high that retaining local roots explains every collection failure. Full log: `/tmp/fc-triage-1/unique.fc`. No wrong-value or crash failure remains in this fresh run.


## Counts and proposed skips

| Class | Packages |
|---|---:|
| ALLOC | 14 |
| LIVENESS | 1 |
| INLINE/OPT | 0 |
| BUG | 6 |
| CRASH | 9 |
| TIMEOUT/SLOW | 0 |
| ENV | 19 |
| **Total** | **49** |

CRASH includes four pre-codegen build rejections and one historical failure now passing; BUG includes one unspecified-evaluation-order compatibility difference (crypto/x509). These qualifications are not additional classes. All 49 FAIL rows were rerun with both compilers; four NOFC rows were excluded.

Proposed additions to `tests/fc/skips.txt` (report only; file not modified):

```text
strconv ^TestAllocationsFromBytes$ asserts allocation counts requiring escape analysis/optimization
encoding/binary ^TestAppendAllocs$ asserts allocation counts requiring escape analysis/optimization
bufio ^TestReadStringAllocs$ asserts allocation counts requiring escape analysis/optimization
crypto/md5 ^TestAllocations$ asserts allocation counts requiring escape analysis/optimization
database/sql ^(TestRawBytesAllocs|TestGrabConnAllocs)$ asserts allocation counts requiring escape analysis/optimization
hash/maphash ^TestComparableAllocations$ asserts allocation counts requiring escape analysis/optimization
internal/zstd ^TestAlloc$ asserts allocation counts requiring escape analysis/optimization
log ^TestDiscard$ asserts allocation counts requiring escape analysis/optimization
log/slog ^(TestAttrNoAlloc|TestJSONAllocs|TestAlloc|TestTextHandlerAlloc|TestValueNoAlloc|TestAnyLevelAlloc)$ asserts allocation counts requiring escape analysis/optimization
log/slog/internal/buffer ^TestAlloc$ asserts allocation counts requiring escape analysis/optimization
mime ^TestLookupMallocs$ asserts allocation counts requiring escape analysis/optimization
net/http/internal ^TestChunkReaderAllocs$ asserts allocation counts requiring escape analysis/optimization
net/netip ^(TestNoAllocs|TestAddrStringAllocs)$ asserts allocation counts requiring escape analysis/optimization
net/textproto ^TestCommonHeaders$ asserts allocation counts requiring escape analysis/optimization
unique ^(TestCanonMap|TestCanonMapBadHash|TestCanonMapTruncHash|TestHandle|TestMakeClonesStrings|TestNestedHandle)$ waits for weak references/cleanup while fc retains local roots
unique ^TestMakeAllocs$ asserts allocation counts requiring escape analysis/optimization
```

No functional wrong-code, crash, build-failure, ENV, or no-longer-reproduced test is proposed for skipping.
