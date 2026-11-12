package floats_test

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"fclang/floats"
	"fclang/internal/chk"
)

func TestMain(m *testing.M) { chk.Main(m) }

var convArgs = []float64{0, math.Copysign(0, -1), 0.5, -0.5, 0.999, -0.999, 1, -1, 1.5, -1.5, 2.5, 100.7, -100.7, 127, 127.99, -128, -128.99,
	128, 255, 255.99, 256, 32767.5, -32768.5, 32768, 65535.9, 65536, 2147483647, 2147483647.5, -2147483648, -2147483648.5, 2147483648,
	4294967295, 4294967295.5, 4294967296, 1e10, -1e10, 9007199254740993, 9.223372036854775e18, -9.223372036854775808e18, 9223372036854775808,
	1.8446744073709550e19, 1e19, 1e-300, 5e-324, 16777216, 16777217, -16777217, 123456789.125}

// bounds (exclusive) within which a float truncates to a value of the integer type
var bounds = map[string][2]float64{
	"I8": {-129, 128}, "I16": {-32769, 32768}, "I32": {-2147483649, 2147483648}, "I64": {-9223372036854777856, 9223372036854775808},
	"Int": {-9223372036854777856, 9223372036854775808}, "U8": {-1, 256}, "U16": {-1, 65536}, "U32": {-1, 4294967296},
	"U64": {-1, 18446744073709551616}, "Uint": {-1, 18446744073709551616}, "Uptr": {-1, 18446744073709551616},
}

func inRange(to string, f32 bool) []any {
	b := bounds[to]
	var out []any
	for _, x := range convArgs {
		if f32 {
			x = float64(float32(x))
		}
		if x > b[0] && x < b[1] {
			out = append(out, x)
		}
	}
	return out
}

func isConv(name string) (to string, f32, ok bool) {
	if to, ok = strings.CutPrefix(name, "F64To"); ok {
		return to, false, true
	}
	to, ok = strings.CutPrefix(name, "F32To")
	return to, true, ok
}

// Every exported function against its stock-compiled copy.  Functions that convert a float to an integer get
// arguments the integer type can represent.
func TestAuto(t *testing.T) {
	i64 := inRange("I64", false)
	special := map[string][]chk.Opt{
		"Trunc":       {chk.Arg(0, i64...)},
		"Frac":        {chk.Arg(0, i64...)},
		"Floor":       {chk.Arg(0, i64...)},
		"MakeMixed":   {chk.Arg(0, i64...)},
		"Results":     {chk.Arg(0, inRange("I8", false)...)},
		"FormatFixed": {chk.Arg(1, 0, 1, 3, 6)},
		"RoundTrip":   {chk.Arg(0, 0, 1, -1, 1<<53+1, -1<<53-1, 1<<62+1, -1<<63, 123456789012345678, 1<<63-1024)},
		"RoundTripU":  {chk.Arg(0, 0, 1, 16777217, 1<<31, 1<<32-256, 123456789)},
	}
	for _, f := range autoFuncs {
		if to, f32, ok := isConv(f.Name); ok {
			special[f.Name] = []chk.Opt{chk.Arg(0, inRange(to, f32)...)}
		}
	}
	chk.All(t, autoFuncs, special)
}

// Conversions of floats the integer type cannot represent (NaN, infinities, too large, negative to unsigned):
// implementation-defined.  Differences from the stock compiler are logged; they fail only with FCLANG_IMPLDEF=1.
func TestConvOutOfRange(t *testing.T) {
	all := append([]float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e300, -1e300, -1, -0.999, -1e10, 300, -300, 70000, 3e9, 5e9, -3e9, 1e19, 2e19, 1.8446744073709552e19}, convArgs...)
	strict := os.Getenv("FCLANG_IMPLDEF") != ""
	for _, f := range autoFuncs {
		if _, f32, ok := isConv(f.Name); ok {
			g, r := reflect.ValueOf(f.Got), reflect.ValueOf(f.Ref)
			for _, x := range all {
				arg := reflect.ValueOf(x)
				if f32 {
					arg = reflect.ValueOf(float32(x))
				}
				got, want := g.Call([]reflect.Value{arg})[0], r.Call([]reflect.Value{arg})[0]
				if s := chk.Diff(got.Interface(), want.Interface()); s != "" {
					if strict {
						t.Errorf("%s(%v): %s", f.Name, x, s)
					} else {
						t.Logf("implementation-defined: %s(%v): %s", f.Name, x, s)
					}
				}
			}
		}
	}
}

func bits(x float64) uint64 { return math.Float64bits(x) }

func TestByHand(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	negz := math.Copysign(0, -1)
	if bits(floats.NegZero()) != 1<<63 || math.Float32bits(floats.NegZero32()) != 1<<31 || bits(floats.Neg64(0)) != 1<<63 || bits(floats.Neg64(negz)) != 0 {
		t.Error("negation of zero")
	}
	if bits(floats.ZeroMinus(0)) != 0 || bits(floats.MulMinusOne(0)) != 1<<63 || bits(floats.Add64(negz, negz)) != 1<<63 || bits(floats.Add64(negz, 0)) != 0 {
		t.Error("signs of zero results")
	}
	if !math.IsNaN(floats.Neg64(nan)) || floats.Neg64(inf) != -inf || !math.IsNaN(floats.Sub64(inf, inf)) || !math.IsNaN(floats.Div64(0, 0)) || floats.Div64(1, 0) != inf || floats.Div64(-1, 0) != -inf || floats.Div64(1, negz) != -inf {
		t.Error("NaN and infinity arithmetic")
	}
	// Classify bits: 1 <, 2 <=, 4 >, 8 >=, 16 ==, 32 !=, 64 !(a<b), 128 a<b||a>b, 256 a<=b&&a>=b
	for _, c := range []struct {
		a, b float64
		want int
	}{{1, 2, 1 | 2 | 32 | 128}, {2, 1, 4 | 8 | 32 | 64 | 128}, {1, 1, 2 | 8 | 16 | 64 | 256}, {nan, 1, 32 | 64}, {1, nan, 32 | 64}, {nan, nan, 32 | 64},
		{0, negz, 2 | 8 | 16 | 64 | 256}, {-inf, inf, 1 | 2 | 32 | 128}, {inf, inf, 2 | 8 | 16 | 64 | 256}} {
		if got := floats.Classify(c.a, c.b); got != c.want {
			t.Errorf("Classify(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := floats.Classify32(float32(c.a), float32(c.b)); got != c.want&63 {
			t.Errorf("Classify32(%v, %v) = %d, want %d", c.a, c.b, got, c.want&63)
		}
	}
	if floats.Cmp3(nan, 1) != 2 || floats.Cmp3(1, 2) != -1 || !floats.IsNaN(nan) || floats.IsNaN(inf) || floats.EqSelf(nan) || !floats.NotLt(nan, 1) || !floats.NotGe(nan, 1) || !floats.NotEq(nan, nan) {
		t.Error("unordered comparisons")
	}
	// min and max: a NaN operand gives NaN; -0 is smaller than +0
	if !math.IsNaN(floats.Min64(nan, 1)) || !math.IsNaN(floats.Max64(1, nan)) || !math.IsNaN(floats.Min3(1, nan, 2)) || bits(floats.Min64(0, negz)) != 1<<63 || bits(floats.Min64(negz, 0)) != 1<<63 ||
		bits(floats.Max64(negz, 0)) != 0 || bits(floats.Max64(0, negz)) != 0 || floats.Min64(inf, -inf) != -inf || floats.Max3(1, 3, 2) != 3 || floats.MinStr("b", "ab") != "ab" || floats.MaxStr3("a", "c", "b") != "c" {
		t.Error("min/max")
	}
	if floats.F64ToI64(-1.9) != -1 || floats.F64ToI64(1.9) != 1 || floats.F64ToU64(1.8446744073709550e19) != 18446744073709549568 || floats.F64ToU64(9223372036854775808) != 1<<63 ||
		floats.F32ToU64(1.8446743e19) != 18446742974197923840 || floats.F64ToU8(255.99) != 255 || floats.F64ToI8(-128.99) != -128 || floats.F64ToU32(4294967295.5) != 4294967295 {
		t.Error("float to integer")
	}
	if floats.U64ToF64(1<<64-1) != 18446744073709551616 || floats.U64ToF64(1<<63+1025) != 9223372036854777856 || floats.U64ToF32(1<<64-1) != 18446744073709551616 || floats.I64ToF64(-1<<63) != -9223372036854775808 ||
		floats.U64ToF64(1<<63) != 9223372036854775808 || floats.I64ToF32(16777217) != 16777216 || floats.U32ToF32(4294967295) != 4294967296 || floats.I64ToF64(9007199254740993) != 9007199254740992 ||
		floats.U64ToF32(0x8000008000000001) != 9223373136366403584 || floats.I64ToF32(0x4000004000000001) != 4611686568183201792 {
		t.Error("integer to float (rounding to nearest even, uint64 above 2^63, double rounding)")
	}
	if floats.Narrow(1e300) != float32(inf) || floats.Narrow(1e-300) != 0 || floats.Widen(0.1) != 0.10000000149011612 || floats.Round32(16777217) != 16777216 || !math.IsNaN(float64(floats.Narrow(nan))) {
		t.Error("float32 <-> float64")
	}
	if floats.Add32(16777216, 1) != 16777216 || floats.Add64(0.1, 0.2) != 0.30000000000000004 || floats.Mul32(1.1, 1.1) != 1.21 || floats.Div32(1, 3) != 0.33333334 {
		t.Error("rounding in the operation's own precision")
	}
}
