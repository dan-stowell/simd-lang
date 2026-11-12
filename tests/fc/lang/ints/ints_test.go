package ints_test

import (
	"testing"

	"fclang/internal/chk"
	"fclang/ints"
)

func TestMain(m *testing.M) { chk.Main(m) }

var small = []any{0, 1, 2, 3, 7, 100, 1000}

// Every exported function against its stock-compiled copy (ints_ref_test.go, ints_gen_ref_test.go), over the
// integer patterns of chk (limits of every width, small values of both signs, bit patterns): all pairs for
// the binary functions, including x/0, MinInt/-1 and negative shift counts (panics are compared).
func TestAuto(t *testing.T) {
	chk.All(t, autoFuncs, map[string][]chk.Opt{
		"Xorshift": {chk.Arg(1, small...)},
	})
}

func TestByHand(t *testing.T) {
	const minI64 = -1 << 63
	if q := ints.DivI64(minI64, -1); q != minI64 {
		t.Errorf("MinInt64 / -1 = %d", q)
	}
	if r := ints.RemI64(minI64, -1); r != 0 {
		t.Errorf("MinInt64 %% -1 = %d", r)
	}
	if q := ints.DivI32(-1<<31, -1); q != -1<<31 {
		t.Errorf("MinInt32 / -1 = %d", q)
	}
	if q := ints.DivI8(-128, -1); q != -128 {
		t.Errorf("MinInt8 / -1 = %d", q)
	}
	if q, r := ints.DivMod(-7, 2); q != -3 || r != -1 {
		t.Errorf("-7 divmod 2 = %d, %d", q, r)
	}
	if q, r := ints.DivMod(7, -2); q != -3 || r != 1 {
		t.Errorf("7 divmod -2 = %d, %d", q, r)
	}
	if got := chk.Try(func() { ints.DivI64(1, 0) }); got != "panic runtime.errorString: runtime error: integer divide by zero" {
		t.Errorf("1/0: %s", got)
	}
	if got := chk.Try(func() { ints.RemU8(1, 0) }); got != "panic runtime.errorString: runtime error: integer divide by zero" {
		t.Errorf("1%%0: %s", got)
	}
	if got := chk.Try(func() { ints.ShlSI64(1, -1) }); got != "panic runtime.errorString: runtime error: negative shift amount" {
		t.Errorf("1<<-1: %s", got)
	}
	if got := chk.Try(func() { ints.ShrCountI8(1, -128) }); got != "panic runtime.errorString: runtime error: negative shift amount" {
		t.Errorf("1>>int8(-128): %s", got)
	}
	if ints.ShlI64(1, 64) != 0 || ints.ShrI64(-5, 64) != -1 || ints.ShrI64(5, 1000) != 0 || ints.ShrU64(1<<63, 64) != 0 || ints.ShlI8(1, 8) != 0 || ints.ShrI8(-128, 200) != -1 {
		t.Error("shift counts at or beyond the width")
	}
	if ints.ShlU32(1, 1<<32) != 0 || ints.ShlCountU64(1, 1<<40+1) != 0 || ints.ShrCountU64(-1, 1<<63) != -1 {
		t.Error("shift counts whose low bits are small")
	}
	if ints.AddI8(127, 1) != -128 || ints.SubI16(-32768, 1) != 32767 || ints.MulI32(65536, 65536) != 0 || ints.AddU64(1<<64-1, 2) != 1 || ints.NegI64(minI64) != minI64 || ints.SubU8(0, 1) != 255 {
		t.Error("wraparound")
	}
	if ints.ConvI8ToU64(-1) != 1<<64-1 || ints.ConvU8ToI8(200) != -56 || ints.ConvI64ToI8(0x1234567890abcdef) != -0x11 || ints.ConvU32ToI64(1<<32-1) != 1<<32-1 || ints.ConvI32ToU16(-2) != 65534 || ints.ConvI16ToUptr(-1) != ^uintptr(0) {
		t.Error("conversions")
	}
	if !ints.LtI64(-1, 0) || ints.LtU64(1<<64-1, 0) || !ints.LtAsUnsigned(0, -1) || !ints.LtI8(-128, 127) || ints.LtU8(128, 127) || !ints.GeU32(1<<31, 1<<31-1) || ints.GeI32(-1<<31, 1<<31-1) {
		t.Error("comparisons")
	}
	if s := ints.Itoa(minI64); s != "-9223372036854775808" {
		t.Error("Itoa", s)
	}
	if n, ok := ints.Atoi("-9223372036854775808"); n != minI64 || !ok {
		t.Error("Atoi", n, ok)
	}
	if ints.PopCount(0xdeadbeef) != 24 || ints.SwarPop(0xdeadbeef) != 24 || ints.LeadingZeros(1) != 63 || ints.Bswap64(0x0102030405060708) != 0x0807060504030201 || ints.Gcd(1071, 462) != 21 {
		t.Error("bit utilities")
	}
}
