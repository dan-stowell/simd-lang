package misc_test

import (
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"reflect"
	"runtime"
	"testing"

	m "fclang/misc"
)

func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			_, isRT := r.(runtime.Error)
			if e, ok := r.(error); ok {
				msg = fmt.Sprintf("runtime=%v error: %s", isRT, e.Error())
			} else {
				msg = fmt.Sprintf("runtime=%v value: %v", isRT, r)
			}
		}
	}()
	f()
	return "no panic"
}

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", name, got, want)
	}
}

func TestBlankInit(t *testing.T) { // first: later tests bump the counter
	kept, count := m.BlankInit()
	eq(t, "BlankInit", []int{kept, count}, []int{3, 3})
}

func TestGotoLabels(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 10, 1000} {
		eq(t, "GotoLoop", m.GotoLoop(n), max(n, 0)*(max(n, 0)-1)/2)
	}
	eq(t, "GotoSkip", []string{m.GotoSkip(-3), m.GotoSkip(0), m.GotoSkip(4)}, []string{"neg", "zero", "pos"})
	eq(t, "GotoFound", []int{m.GotoFound([]int{3, 5, 7}, 7), m.GotoFound([]int{3, 5, 7}, 3), m.GotoFound([]int{3}, 4), m.GotoFound(nil, 0)}, []int{4, 0, -1, -1})
	for _, c := range []struct {
		s    string
		w, d int
	}{{"", 0, 0}, {"   ", 0, 0}, {"ab 12  c3d ", 3, 3}, {"7", 1, 1}, {" x", 1, 0}} {
		w, d := m.GotoMachine(c.s)
		eq(t, "GotoMachine "+c.s, []int{w, d}, []int{c.w, c.d})
	}
	eq(t, "LabeledSwitch", []int{m.LabeledSwitch(1, 3), m.LabeledSwitch(1, 20), m.LabeledSwitch(0, 3), m.LabeledSwitch(1, 0)}, []int{3, 110, -1, 0})
	for n := 0; n < 12; n++ {
		eq(t, fmt.Sprint("LabeledLoops ", n), m.LabeledLoops(n), labeledLoops(n))
	}
	eq(t, "LabeledLoops hand", m.LabeledLoops(3), 5)
	ch := make(chan int, 8)
	for _, v := range []int{1, 50, 2, 99, 3} {
		ch <- v
	}
	close(ch)
	eq(t, "LabeledSelect", m.LabeledSelect(ch, 10), 6)
}

func labeledLoops(n int) (pairs int) {
	for i := 0; i < n; i++ {
		stop, next := false, false
		for j := 0; j < n && !stop && !next; j++ {
			if j > i {
				next = true
			} else if i+j > n {
				stop = true
			} else if j == 2 {
			} else if i == 5 && j == 4 {
				break
			} else {
				pairs++
			}
		}
		if stop {
			break
		}
		if !next {
			pairs += 1000
		}
	}
	return
}

func TestShadow(t *testing.T) {
	for _, x := range []int{-50, 0, 1, 6, 100} {
		// by hand: inner x = 2x; y = x + 2x + 3 + (2x+1); then the if chain; then 0+10+20; then level 1 + x
		y := x + 2*x + 3 + 2*x + 1
		switch {
		case x+100 > 105:
			y += x + 100
		case (x+100)*3 > 0:
			y += (x + 100) * 3
		default:
			y -= (x + 100) * 3
		}
		y += 30 + 1 + x
		gx, gy := m.Shadow(x)
		eq(t, fmt.Sprint("Shadow ", x), []int{gx, gy}, []int{x, y})
		eq(t, "ShadowBuiltins", m.ShadowBuiltins(x), 47+x)
		r, err := m.ShadowResult(x)
		eq(t, "ShadowResult", []any{r, err}, []any{1, ""})
		eq(t, "ShadowParamInClosure", m.ShadowParamInClosure(x), 3*x+4)
	}
	eq(t, "ShadowSwitch", []int{m.ShadowSwitch(5), m.ShadowSwitch("abc"), m.ShadowSwitch(nil), m.ShadowSwitch(2.5)}, []int{6, 3, -1, -1})
}

func TestBlank(t *testing.T) {
	eq(t, "BlankParam", m.BlankParam(1, 2, "x"), 2)
	for _, xs := range [][]int{{1}, {1, 2, 3}} {
		v, bumps := m.Blank(xs)
		eq(t, "Blank", []int{v, bumps}, []int{7 + 3*len(xs) + 3, 4})
	}
	var none []int
	eq(t, "Blank empty", catch(func() { m.Blank(nil) }), catch(func() { sink = none[len(none)] }))
}

var sink int

func TestConsts(t *testing.T) {
	bigC := new(big.Int).Mod(new(big.Int).Lsh(big.NewInt(1), 200), big.NewInt(1000007)).Int64()
	eq(t, "BigConsts", m.BigConsts(), []int{1024, 4, int(bigC), 8, 98, -1, -3, -1, 6, 0xc3, 255})
	eq(t, "BigUint", m.BigUint(), uint64(math.MaxUint64))
	eq(t, "ConstBools", m.ConstBools(), []bool{true, true, true, true, true})
	eq(t, "ConstFloats", m.ConstFloats(), []float64{10, 1, math.Ldexp(1, 200), 1 << 62, math.Ldexp(1, 64), 0.3, 1.0 / 3})
	eq(t, "WithMask", []uint64{m.WithMask(0), m.WithMask(math.MaxUint64), m.WithMask(0x1234)}, []uint64{0xf, math.MaxUint64 - 0xf, 0x123b})
	eq(t, "FloatFromBig", m.FloatFromBig(1.5), 12.0)
	a, b := 0.1, 0.2
	eq(t, "VarFloatSum", m.VarFloatSum(a, b), a+b == 0.3)
	eq(t, "VarFloatSum is false", m.VarFloatSum(a, b), false)
	lens, calls := m.LenConsts("héllo")
	eq(t, "LenConsts", lens, []int{5, 5, 5, 3, 7, 6, 5, 5, 10, 4, 5, 6, 8, 6, 5})
	eq(t, "LenConsts calls", calls, 2)
	eq(t, "UntypedKinds", m.UntypedKinds(3), []string{
		"int", "float64", "int32", "string", "bool", "complex128", "nil",
		"int", "int", "int32", "int32", "float64", "float64",
		"complex128", "int", "float64", "int", "int32", "bool",
		"string", "int", "uint8", "uint", "float64", "int32",
	})
	eq(t, "UntypedValues", m.UntypedValues(3), []any{8, 8, int32(98), 0, 0.5, 8, true, byte('b'), 33, 1.0})
	eq(t, "Iotas", m.Iotas(), []int{1, 2, 8, 11, 0, 1, 4, 9, 2, 3, 0, 0, 1, -1, 3, -3, 7, 10, 11, 32, 48, 'd', 'e'})
	eq(t, "IotaStrings", m.IotaStrings(), []string{"s", "s", "e"})
	eq(t, "IotaFloats", m.IotaFloats(), []float64{0, 0.5, 1})
}

func TestBits(t *testing.T) {
	vals := []uint64{0, 1, 2, 3, 0x80, 0xff, 0x100, 0xdeadbeef, 1 << 31, 1 << 32, 1<<63 - 1, 1 << 63, math.MaxUint64, 0x0123456789abcdef, 0xaaaaaaaaaaaaaaaa}
	for _, x := range vals {
		n := fmt.Sprintf("%#x", x)
		eq(t, "PopCount "+n, m.PopCount(x), bits.OnesCount64(x))
		eq(t, "PopCountSWAR "+n, m.PopCountSWAR(x), bits.OnesCount64(x))
		eq(t, "LeadingZeros "+n, m.LeadingZeros(x), bits.LeadingZeros64(x))
		eq(t, "TrailingZeros "+n, m.TrailingZeros(uint32(x)), bits.TrailingZeros32(uint32(x)))
		eq(t, "Reverse8 "+n, m.Reverse8(uint8(x)), bits.Reverse8(uint8(x)))
		eq(t, "Reverse64 "+n, m.Reverse64(x), bits.Reverse64(x))
		eq(t, "ByteSwap "+n, m.ByteSwap(uint32(x)), bits.ReverseBytes32(uint32(x)))
		eq(t, "IsPow2 "+n, m.IsPow2(x), bits.OnesCount64(x) == 1)
		eq(t, "Parity "+n, m.Parity(x), uint(bits.OnesCount64(x)&1))
		eq(t, "Gray "+n, m.UnGray(m.Gray(uint32(x))), uint32(x))
		eq(t, "AndNot "+n, m.AndNot(uint8(x), uint8(x>>4)), uint8(x)&^uint8(x>>4))
		for _, k := range []int{0, 1, 7, 31, 32, 63, 64, 65, -1, -70} {
			eq(t, fmt.Sprint("RotL ", n, k), m.RotL(x, k), bits.RotateLeft64(x, k))
			if k >= 0 {
				eq(t, fmt.Sprint("RotL32 ", n, k), m.RotL32(uint32(x), uint(k)), bits.RotateLeft32(uint32(x), k))
				eq(t, fmt.Sprint("RotL16 ", n, k), m.RotL16(uint16(x), uint8(k)), bits.RotateLeft16(uint16(x), k))
			}
		}
		for _, b := range []uint{1, 8, 13, 32, 64} {
			want := int64(x << (64 - b))
			want >>= 64 - b
			eq(t, fmt.Sprint("SignExtend ", n, b), m.SignExtend(x, b), want)
		}
		ax := int64(x)
		if ax < 0 {
			ax = -ax
		}
		eq(t, "Abs "+n, m.Abs(int64(x)), ax)
		a, b := int32(x), int32(x>>32)
		lo, hi := m.MinMaxBranchless(a, b)
		eq(t, "MinMaxBranchless "+n, []int32{lo, hi}, []int32{min(a, b), max(a, b)})
	}
	eq(t, "NextPow2", []uint32{m.NextPow2(1), m.NextPow2(2), m.NextPow2(3), m.NextPow2(1000), m.NextPow2(1<<31 + 1), m.NextPow2(0)}, []uint32{1, 2, 4, 1024, 0, 0})
	eq(t, "Gray", []uint32{m.Gray(0), m.Gray(1), m.Gray(2), m.Gray(3), m.Gray(4)}, []uint32{0, 1, 3, 2, 6})
	eq(t, "Interleave", []uint32{m.Interleave(0xffff, 0), m.Interleave(0, 0xffff), m.Interleave(0b101, 0b011)}, []uint32{0x55555555, 0xaaaaaaaa, 0b011011})
	s, c, g, has := m.SetClearToggle(0x00f0, 4)
	eq(t, "SetClearToggle", []any{s, c, g, has}, []any{uint16(0xf0), uint16(0xe0), uint16(0xe0), true})
	s, c, g, has = m.SetClearToggle(0x00f0, 16) // shifted out entirely
	eq(t, "SetClearToggle 16", []any{s, c, g, has}, []any{uint16(0xf0), uint16(0xf0), uint16(0xf0), false})
}

func TestPrints(t *testing.T) {
	x := 41
	eq(t, "Prints", m.Prints(x, "str", 2.5, &x, []int{1, 2}, map[string]int{"k": 1}, "iface"), 42)
	eq(t, "Prints nils", m.Prints(-1, "", math.Inf(-1), nil, nil, nil, nil), 0)
}

func TestVariadic(t *testing.T) {
	n, isNil := m.CallNone()
	eq(t, "CallNone", []any{n, isNil}, []any{0, true})
	n, isNil = m.CallSpreadNil()
	eq(t, "CallSpreadNil", []any{n, isNil}, []any{0, true})
	n, isNil = m.CallSpreadEmpty()
	eq(t, "CallSpreadEmpty", []any{n, isNil}, []any{0, false})
	eq(t, "Sum", []int{m.Sum(), m.Sum(5), m.Sum(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12), m.Sum([]int{4, 5}...)}, []int{0, 5, 78, 9})
	eq(t, "CallSome", m.CallSome(1, 2, 3), 7)
	eq(t, "CallSpread", []int{m.CallSpread(nil), m.CallSpread([]int{7, 8})}, []int{0, 15})
	xs := []int{1, 2}
	listed, spread := m.SpreadAliases(xs, 9)
	eq(t, "SpreadAliases", []int{listed, spread, xs[0]}, []int{1, 9, 9})
	eq(t, "KindsNone", m.KindsNone(), "0:")
	eq(t, "KindsNil", m.KindsNil(), "1:nil,")
	eq(t, "KindsNilSpread", m.KindsNilSpread(), "0:")
	eq(t, "KindsMixed", m.KindsMixed(1, "s"), "8:int,string,nil,float64,int32,bool,uint8,complex128,")
	eq(t, "KindsSlice", m.KindsSlice([]any{1, "a"}), "1:?,2:int,string,")
	eq(t, "Kinds", m.Kinds(uint(1), nil, true), "3:uint,nil,bool,")
	eq(t, "Prefix", m.Prefix("p"), "")
	eq(t, "CallPrefix", m.CallPrefix("-", "a", "b"), "|-a|-a -b")
	eq(t, "VariadicMethod", m.VariadicMethod(",", []string{"x", "y"}), "a;a,x,y;b,c,d;e,x,y")
	eq(t, "VariadicMethod nil", m.VariadicMethod(",", nil), "a;a;b,c,d;e")
	eq(t, "VariadicClosure", m.VariadicClosure(10, []int{1, 2, 3}), 10+22+19)
	x, y := m.AppendForms([]int{1}, []int{2, 3}, "hi")
	eq(t, "AppendForms", []any{x, y}, []any{[]int{1, 2, 3, 1, 2, 1, 2, 3, 1, 2}, []byte("<hi>")})
	eq(t, "VariadicRec", []int{m.VariadicRec(0), m.VariadicRec(3), m.VariadicRec(100)}, []int{0, 3006, 100*1000 + 5050})
}

func TestRangeFunc(t *testing.T) {
	eq(t, "RangeSum", []int{m.RangeSum(0), m.RangeSum(5), m.RangeSum(1000)}, []int{0, 10, 499500})
	eq(t, "RangeZero", []int{m.RangeZero(0), m.RangeZero(4)}, []int{0, 4})
	eq(t, "RangeNoVar", m.RangeNoVar(3), 6)
	eq(t, "RangeTwo", []string{m.RangeTwo(nil), m.RangeTwo([]string{"a", "b"})}, []string{"", "0=a;1=b;"})
	eq(t, "RangeKeyOnly", m.RangeKeyOnly([]string{"a", "b", "c"}), 6)
	x, s := m.RangeAssign(4)
	eq(t, "RangeAssign", []any{x, s}, []any{3, "c"})
	x, s = m.RangeAssign(0)
	eq(t, "RangeAssign 0", []any{x, s}, []any{-1, "none"})
	eq(t, "RangeContinue", m.RangeContinue(6), 6)
	eq(t, "RangeGoto", []int{m.RangeGoto(5, 3), m.RangeGoto(5, 9)}, []int{3, 1010})
	eq(t, "RangeClosures", m.RangeClosures(4), 14)
	eq(t, "TreeFirst", [][]int{m.TreeFirst([]int{5, 2, 8, 1, 9, 3}, 4), m.TreeFirst([]int{5, 2, 8, 1, 9, 3}, 10), m.TreeFirst([]int{5}, 0), m.TreeFirst(nil, 3)},
		[][]int{{1, 2, 3, 5}, {1, 2, 3, 5, 8, 9}, nil, nil})
	eq(t, "FibChain", [][]int{m.FibChain(5, 1), m.FibChain(0, 1), m.FibChain(1, -1)}, [][]int{{1, 3, 9, 35, 145}, nil, {-1}})
	sum, evals := m.RangeFuncValue(4)
	eq(t, "RangeFuncValue", []int{sum, evals}, []int{9, 1})
	for n := 0; n < 8; n++ {
		eq(t, fmt.Sprint("RangeMixed ", n), m.RangeMixed(n), rangeMixed(n))
	}
}

// rangeMixed is RangeMixed written with ordinary loops.
func rangeMixed(n int) (s int) {
outer:
	for i := 0; i < n; i++ {
		for x := 0; x < n; x++ {
			if x > i {
				continue outer
			}
			for k := 0; k < 3; k++ {
				if k == 2 {
					break
				}
				if i == 4 && x == 2 {
					break outer
				}
				s++
			}
		}
		s += 100
	}
	return
}

func TestRangeFuncExits(t *testing.T) {
	v, log := m.RangeBreak(5, 3)
	eq(t, "RangeBreak", []any{v, log}, []any{3, "stopped@3;cleanup;"})
	v, log = m.RangeBreak(3, 10)
	eq(t, "RangeBreak none", []any{v, log}, []any{3, "exhausted;cleanup;"})
	x, ok, log := m.RangeReturn(10, 20)
	eq(t, "RangeReturn", []any{x, ok, log}, []any{5, true, "stopped@5;cleanup;"})
	x, ok, log = m.RangeReturn(3, 100)
	eq(t, "RangeReturn none", []any{x, ok, log}, []any{-1, false, "exhausted;cleanup;"})
	c, log := m.RangeNested(4, 4, 100)
	eq(t, "RangeNested", []any{c, log}, []any{110, "stopped@1;cleanup;stopped@2;cleanup;stopped@3;cleanup;exhausted;cleanup;exhausted;cleanup;"})
	c, log = m.RangeNested(4, 4, 3)
	eq(t, "RangeNested break", []any{c, log}, []any{4, "stopped@1;cleanup;stopped@2;cleanup;stopped@1;cleanup;stopped@2;cleanup;"})
	i, j, k := m.RangeNestedReturn(5, 235)
	eq(t, "RangeNestedReturn", []int{i, j, k}, []int{2, 3, 2})
	i, j, k = m.RangeNestedReturn(5, 1)
	eq(t, "RangeNestedReturn first", []int{i, j, k}, []int{0, 0, 0})
	i, j, k = m.RangeNestedReturn(5, 999)
	eq(t, "RangeNestedReturn none", []int{i, j, k}, []int{-1, -1, -1})
	eq(t, "RangeDefer", []string{m.RangeDefer(0), m.RangeDefer(3)}, []string{"end;", "end;210"})
	eq(t, "RangeDeferReturn", []int{m.RangeDeferReturn(5, 2), m.RangeDeferReturn(5, 9), m.RangeDeferReturn(0, 0)}, []int{1018, 1162, 1002})
	sum, msg, log := m.RangePanic(5, 3)
	eq(t, "RangePanic", []any{sum, msg, log}, []any{3, "body@3", "cleanup;"})
	sum, msg, log = m.RangePanic(3, 9)
	eq(t, "RangePanic none", []any{sum, msg, log}, []any{3, "", "exhausted;cleanup;"})

	eq(t, "RangeBad ok", m.RangeBad(3, 9), 3)
	want := catch(func() {
		for x := range bad(5) {
			if x == 2 {
				break
			}
		}
	})
	got := catch(func() { m.RangeBad(5, 2) })
	if got != want || got == "no panic" {
		t.Errorf("RangeBad: got %q, want %q", got, want)
	}
}

func bad(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			yield(i)
		}
	}
}
