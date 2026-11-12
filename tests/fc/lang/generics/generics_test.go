package generics_test

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"testing"

	g "fclang/generics"
)

// catch runs f and describes its panic (or "no panic").
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

// eq compares with DeepEqual; floats (incl. NaN and -0) are compared by formatting.
func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	if fmt.Sprintf("%#v", got) == fmt.Sprintf("%#v", want) && reflect.TypeOf(got) == reflect.TypeOf(want) {
		switch got.(type) {
		case float32, float64, []float32, []float64:
			return
		}
	}
	t.Errorf("%s: got %#v, want %#v", name, got, want)
}

func TestSumDot(t *testing.T) {
	for _, xs := range [][]int{nil, {}, {7}, {1, 2, 3, 4}, {math.MaxInt, 1}, {-5, 5, math.MinInt}} {
		eq(t, "SumInts", g.SumInts(xs), g.Sum(xs))
	}
	eq(t, "SumInts const", g.SumInts([]int{1, 2, 3, 4}), 10)
	eq(t, "SumFloat32s", g.SumFloat32s([]float32{0.1, 0.2, 0.3, 1e10}), g.Sum([]float32{0.1, 0.2, 0.3, 1e10}))
	eq(t, "SumUint8s", g.SumUint8s([]uint8{200, 100, 7}), uint8(51))
	eq(t, "SumMyInts", g.SumMyInts([]g.MyInt{1, -2, 30}), g.MyInt(29))
	eq(t, "DotInt8", g.DotInt8([]int8{100, 3, -7}, []int8{2, 50, 9}), g.Dot([]int8{100, 3, -7}, []int8{2, 50, 9}))
	eq(t, "DotInt8 const", g.DotInt8([]int8{100, 3}, []int8{2, 50}), int8(94)) // 350 mod 256
	eq(t, "DotFloat64", g.DotFloat64([]float64{1.5, 2, -3}, []float64{2, 0.25, 1e-3}), 1.5*2+2*0.25+-3*1e-3)
	eq(t, "DotUint16", g.DotUint16([]uint16{65535, 2}, []uint16{65535, 3}), uint16(7))
	if m := catch(func() { g.DotInt8([]int8{1, 2}, []int8{1}) }); m != catch(func() { g.Dot([]int8{1, 2}, []int8{1}) }) {
		t.Errorf("Dot index panic: %s", m)
	}
}

func TestSwapMapFilterReduce(t *testing.T) {
	s, i := g.SwapIntStr(3, "x")
	eq(t, "SwapIntStr", []any{s, i}, []any{"x", 3})
	a, p := g.SwapPointAny(g.Point{1, 2}, 4.5)
	eq(t, "SwapPointAny", []any{a, p}, []any{4.5, g.Point{1, 2}})
	pt := &g.Point{5, 6}
	pp, f := g.SwapFloatPtr(2.5, pt)
	if pp != pt || f != 2.5 {
		t.Error("SwapFloatPtr")
	}
	eq(t, "MapItoa", g.MapItoa([]int{0, -1, 42, math.MinInt}), []string{"0", "-1", "42", "-9223372036854775808"})
	eq(t, "MapItoa nil", g.MapItoa(nil), []string{})
	eq(t, "MapLen", g.MapLen([]string{"", "a", "héllo"}), []int{0, 1, 6})
	eq(t, "MapPointX", g.MapPointX([]g.Point{{1, 2}, {3, 4}}, 10), []int{11, 13})
	eq(t, "MapToAny", g.MapToAny([]int{1, 2}), []any{1, 2})
	eq(t, "FilterMod", g.FilterMod([]int{1, 2, 3, 4, 6, 9, -3}, 3), []int{3, 6, 9, -3})
	eq(t, "FilterMod none", g.FilterMod([]int{1, 2}, 5), []int(nil))
	eq(t, "FilterLong", g.FilterLong([]string{"a", "abc", "ab", "abcd"}, 2), []string{"abc", "abcd"})
	eq(t, "ReduceJoin", g.ReduceJoin([]string{"a", "b", "c"}, ", "), "a, b, c")
	eq(t, "ReduceJoin empty", g.ReduceJoin(nil, ","), "")
	eq(t, "ReduceSumLen", g.ReduceSumLen([]string{"ab", "", "cde"}), 5)
	eq(t, "ReducePoints", g.ReducePoints([]g.Point{{1, 2}, {3, 4}, {-10, 0}}), g.Point{-6, 6})
	if m := catch(func() { g.FilterMod([]int{1}, 0) }); m != catch(func() { g.Filter([]int{1}, func(x int) bool { return x%zero == 0 }) }) {
		t.Errorf("FilterMod divide panic: %s", m)
	}
}

var zero = 0

func TestIndexEqual(t *testing.T) {
	eq(t, "IndexInt", g.IndexInt([]int{5, 6, 7}, 7), 2)
	eq(t, "IndexInt miss", g.IndexInt([]int{5, 6, 7}, 8), -1)
	eq(t, "IndexStr", g.IndexStr([]string{"a", "bc", "b" + "c"}, "bc"), 1)
	eq(t, "IndexPoint", g.IndexPoint([]g.Point{{1, 2}, {2, 1}}, g.Point{2, 1}), 1)
	p1, p2 := &g.Point{1, 1}, &g.Point{1, 1}
	eq(t, "IndexPtr", g.IndexPtr([]*g.Point{p1, p2, nil}, p2), 1)
	eq(t, "IndexPtr nil", g.IndexPtr([]*g.Point{p1, p2, nil}, nil), 2)
	nan := math.NaN()
	eq(t, "IndexFloat nan", g.IndexFloat([]float64{1, nan, 2}, nan), -1)
	eq(t, "IndexFloat -0", g.IndexFloat([]float64{1, 0, 2}, math.Copysign(0, -1)), 1)
	eq(t, "IndexArr", g.IndexArr([][2]string{{"a", "b"}, {"a", "c"}}, [2]string{"a", "c"}), 1)
	anys := []any{1, "1", int8(1), g.Point{1, 2}, p1, nil, 1.0, g.MyInt(1), [2]int{1, 2}}
	for i, x := range anys {
		eq(t, fmt.Sprint("IndexAny ", i), g.IndexAny(anys, x), i)
	}
	eq(t, "IndexAny miss", g.IndexAny(anys, int16(1)), -1)
	eq(t, "IndexStringer", g.IndexStringer([]g.Stringer{g.MyInt(1), g.Point{1, 2}, p1}, g.Point{1, 2}), 1)
	// comparing interfaces holding an uncomparable dynamic type panics
	want := catch(func() { g.Index([]any{[]int{1}}, any([]int{1})) })
	if m := catch(func() { g.IndexAny([]any{[]int{1}}, []int{1}) }); m != want || m == "no panic" {
		t.Errorf("IndexAny uncomparable: %q want %q", m, want)
	}
	want = catch(func() { g.Equal[any](map[string]int{}, map[string]int{}) })
	if m := catch(func() { g.EqualAny(map[string]int{}, map[string]int{}) }); m != want || m == "no panic" {
		t.Errorf("EqualAny uncomparable: %q want %q", m, want)
	}
	eq(t, "EqualAny", g.EqualAny(1, 1), true)
	eq(t, "EqualAny types", g.EqualAny(1, int64(1)), false)
	eq(t, "EqualAny nil", g.EqualAny(nil, nil), true)
	eq(t, "EqualAny nilptr", g.EqualAny((*g.Point)(nil), nil), false)
	eq(t, "EqualAny slices different types", g.EqualAny([]int{1}, "x"), false)
	eq(t, "EqualStr", g.EqualStr("ab", "a"+string(rune('b'))), true)
	eq(t, "EqualPair", g.EqualPair(g.Pair[string, float64]{"a", 1}, g.Pair[string, float64]{"a", 1}), true)
	eq(t, "EqualPair nan", g.EqualPair(g.Pair[string, float64]{"a", nan}, g.Pair[string, float64]{"a", nan}), false)
}

func TestOrdered(t *testing.T) {
	nan, inf, negz := math.NaN(), math.Inf(1), math.Copysign(0, -1)
	for _, c := range [][2]int{{1, 2}, {2, 1}, {-1, -1}, {math.MinInt, math.MaxInt}} {
		eq(t, "MaxInt", g.MaxInt(c[0], c[1]), max(c[0], c[1]))
	}
	for _, c := range [][2]string{{"a", "b"}, {"b", "a"}, {"", "a"}, {"abc", "abd"}, {"ab", "abc"}} {
		eq(t, "MaxStr", g.MaxStr(c[0], c[1]), max(c[0], c[1]))
		eq(t, "MaxMyStr", g.MaxMyStr(g.MyStr(c[0]), g.MyStr(c[1])), g.MyStr(max(c[0], c[1])))
		eq(t, "LessStr", g.LessStr(c[0], c[1]), c[0] < c[1])
	}
	fl := []float64{0, negz, 1, -1, inf, -inf, nan, 1e-300}
	for _, a := range fl {
		for _, b := range fl {
			eq(t, fmt.Sprint("MaxFloat ", a, b), g.MaxFloat(a, b), max(a, b))
			eq(t, fmt.Sprint("Min3Float ", a, b), g.Min3Float(a, b, 0.5), min(a, b, 0.5))
			eq(t, fmt.Sprint("LessFloat32 ", a, b), g.LessFloat32(float32(a), float32(b)), float32(a) < float32(b))
		}
	}
	if math.Signbit(g.MaxFloat(negz, 0)) || !math.Signbit(g.Min3Float(0, negz, 0)) {
		t.Error("max/min signed zero")
	}
	eq(t, "MaxUint8", g.MaxUint8(200, 100), uint8(200))
	eq(t, "Min3Int", g.Min3Int(3, -1, 2), -1)
	eq(t, "Min3Str", g.Min3Str("b", "a", "c"), "a")
	eq(t, "ClampInt", []int{g.ClampInt(5, 0, 3), g.ClampInt(-5, 0, 3), g.ClampInt(2, 0, 3)}, []int{3, 0, 2})
	eq(t, "ClampStr", g.ClampStr("z", "b", "m"), "m")
	eq(t, "LessInt8", []bool{g.LessInt8(-1, 1), g.LessInt8(1, -1), g.LessInt8(-128, 127)}, []bool{true, false, true})
	eq(t, "LessUint8", []bool{g.LessUint8(255, 1), g.LessUint8(1, 255), g.LessUint8(0, 0)}, []bool{false, true, false})
}

func TestZero(t *testing.T) {
	i, s, p, pp, a, sl, f, arr := g.Zeros()
	if i != 0 || s != "" || p != (g.Point{}) || pp != nil || a != nil || sl != nil || f != 0 || arr != [3]bool{} {
		t.Error("Zeros")
	}
	eq(t, "IsZeroInt", []bool{g.IsZeroInt(0), g.IsZeroInt(1)}, []bool{true, false})
	eq(t, "IsZeroStr", []bool{g.IsZeroStr(""), g.IsZeroStr("a")}, []bool{true, false})
	eq(t, "IsZeroPoint", []bool{g.IsZeroPoint(g.Point{}), g.IsZeroPoint(g.Point{0, 1})}, []bool{true, false})
	eq(t, "IsZeroPtr", []bool{g.IsZeroPtr(nil), g.IsZeroPtr(&g.Point{})}, []bool{true, false})
	eq(t, "IsZeroAny", []bool{g.IsZeroAny(nil), g.IsZeroAny(0), g.IsZeroAny((*int)(nil))}, []bool{true, false, false})
	eq(t, "IsZeroFloat", []bool{g.IsZeroFloat(0), g.IsZeroFloat(math.Copysign(0, -1)), g.IsZeroFloat(math.NaN())}, []bool{true, true, false})
}

func TestUnionAndMethods(t *testing.T) {
	eq(t, "DoubleInt", g.DoubleInt(21), 42)
	eq(t, "DoubleInt wrap", g.DoubleInt(math.MaxInt), -2)
	eq(t, "DoubleStr", g.DoubleStr("ab"), "abab")
	eq(t, "DoubleMyStr", g.DoubleMyStr("xy"), g.MyStr("xyxy"))
	eq(t, "DoubleMyInt", g.DoubleMyInt(-4), g.MyInt(-8))
	eq(t, "JoinMyInts", g.JoinMyInts([]g.MyInt{1, -2}, "+"), "MyInt(1)+MyInt(-2)")
	eq(t, "JoinPoints", g.JoinPoints([]g.Point{{1, 2}, {3, 4}, {5, 6}}, ""), "(1,2)(3,4)(5,6)")
	eq(t, "JoinStringers", g.JoinStringers([]g.Stringer{g.MyInt(7), g.Point{8, 9}, &g.Point{1, 0}}, " "), "MyInt(7) (8,9) (1,0)")
	eq(t, "JoinStringers nil", catch(func() { g.JoinStringers([]g.Stringer{nil}, "") }), catch(func() { g.Join([]g.Stringer{nil}, "") }))
	eq(t, "DescribeMyInt", g.DescribeMyInt(41), "MyInt(41)=42")
}

func TestConvert(t *testing.T) {
	eq(t, "IntToFloat", g.IntToFloat(1<<53+1), float64(1<<53))
	eq(t, "FloatToInt8", []int8{g.FloatToInt8(-3.99), g.FloatToInt8(127.5), g.FloatToInt8(-128)}, []int8{-3, 127, -128})
	for _, u := range []uint64{0, 1, 1 << 24, 1<<24 + 1, 1 << 63, 1<<64 - 1, 1<<63 + 1<<39, 1<<63 + 1<<40} {
		eq(t, fmt.Sprint("Uint64ToFloat32 ", u), g.Uint64ToFloat32(u), float32(u))
	}
	eq(t, "Int64ToUint8", []uint8{g.Int64ToUint8(-1), g.Int64ToUint8(256), g.Int64ToUint8(0x1ff)}, []uint8{255, 0, 255})
	eq(t, "MyIntToInt16", g.MyIntToInt16(0x18001), int16(-32767))
	eq(t, "Int8ToUint64", g.Int8ToUint64(-2), uint64(1<<64-2))
	eq(t, "Float32ToFloat64", g.Float32ToFloat64(0.1), float64(float32(0.1)))
}

func TestIntegerOps(t *testing.T) {
	for n := uint(0); n < 70; n += 3 {
		eq(t, "ShlInt8", g.ShlInt8(-77, n), g.ShiftL(int8(-77), n))
		eq(t, "ShlUint32", g.ShlUint32(0xdeadbeef, n), uint32(0xdeadbeef)<<n)
		eq(t, "ShlInt64", g.ShlInt64(-0x123456789, n), int64(-0x123456789)<<n)
		eq(t, "ShrInt8", g.ShrInt8(-77, n), int8(-77)>>n)
		eq(t, "ShrUint64", g.ShrUint64(1<<63+12345, n), uint64(1<<63+12345)>>n)
		eq(t, "ShrInt16", g.ShrInt16(-30000, n), int16(-30000)>>n)
	}
	eq(t, "DivInt8", []int8{g.DivInt8(-128, -1), g.DivInt8(-7, 2), g.DivInt8(7, -2)}, []int8{-128, -3, -3})
	eq(t, "DivInt64", g.DivInt64(math.MinInt64, -1), int64(math.MinInt64))
	eq(t, "DivUint8", g.DivUint8(255, 2), uint8(127))
	eq(t, "ModInt32", []int32{g.ModInt32(-7, 3), g.ModInt32(7, -3), g.ModInt32(math.MinInt32, -1)}, []int32{-1, 1, 0})
	eq(t, "ModUint64", g.ModUint64(1<<64-1, 10), uint64(5))
	eq(t, "NegInt8", []int8{g.NegInt8(-128), g.NegInt8(5)}, []int8{-128, -5})
	eq(t, "NegUint16", g.NegUint16(1), uint16(65535))
	if !math.Signbit(g.NegFloat(0)) || g.NegFloat(-2.5) != 2.5 {
		t.Error("NegFloat")
	}
	want := catch(func() { g.Div(1, zero) })
	for name, f := range map[string]func(){
		"DivInt8":   func() { g.DivInt8(1, 0) },
		"DivInt64":  func() { g.DivInt64(1, 0) },
		"DivUint8":  func() { g.DivUint8(1, 0) },
		"ModInt32":  func() { g.ModInt32(1, 0) },
		"ModUint64": func() { g.ModUint64(1, 0) },
	} {
		if m := catch(f); m != want {
			t.Errorf("%s by zero: %q want %q", name, m, want)
		}
	}
}

func TestKind(t *testing.T) {
	p := &g.Point{3, 4}
	cases := []struct {
		x    any
		want string
	}{
		{nil, "nil"}, {5, "int:5"}, {"s", "string:s"}, {g.MyInt(1), "named"}, {g.MyStr("q"), "named"},
		{g.Point{1, 2}, "Point(1,2)"}, {p, "*Point(3,4)"}, {(*g.Point)(nil), "*Point(nil)"},
		{[]int{1, 2}, "[]int:2"}, {errors.New("bad"), "error:bad"}, {2.5, "other"}, {int8(1), "other"},
		{g.Sq(1), "other"}, {stringer{}, "Stringer:S!"},
	}
	for _, c := range cases {
		eq(t, fmt.Sprintf("KindAny(%v)", c.x), g.KindAny(c.x), c.want)
		eq(t, fmt.Sprintf("Kind(%v)", c.x), g.Kind(c.x), c.want)
	}
	eq(t, "KindInt", g.KindInt(-9), "int:-9")
	eq(t, "KindStr", g.KindStr(""), "string:")
	eq(t, "KindMyInt", g.KindMyInt(3), "named")
	eq(t, "KindPoint", g.KindPoint(g.Point{7, 8}), "Point(7,8)")
	eq(t, "KindPtr", g.KindPtr(p), "*Point(3,4)")
	eq(t, "KindPtr nil", g.KindPtr(nil), "*Point(nil)")
	eq(t, "KindSlice", g.KindSlice(nil), "[]int:0")
	eq(t, "KindErr", g.KindErr(errors.New("e")), "error:e")
	eq(t, "KindErr nil", g.KindErr(nil), "nil")
	eq(t, "KindFloat", g.KindFloat(1), "other")
	eq(t, "KindStringer", g.KindStringer(g.MyInt(2)), "named")
	eq(t, "KindStringer other", g.KindStringer(stringer{}), "Stringer:S!")
	eq(t, "KindStringer nil", g.KindStringer(nil), "nil")
}

type stringer struct{}

func (stringer) String() string { return "S!" }

func TestPtrSlices(t *testing.T) {
	eq(t, "PtrInt", g.PtrInt(20), 41)
	eq(t, "PtrPoint", g.PtrPoint(g.Point{1, -2}), g.Point{3, -6})
	eq(t, "PtrPtr", g.PtrPtr(&g.Point{}), true)
	eq(t, "PtrStr", g.PtrStr("hi"), "hi!hi")
	eq(t, "SliceOfInts", g.SliceOfInts(3, 1, 2), []int{3, 1, 2})
	eq(t, "SliceOfNone", g.SliceOfNone(), 0)
	eq(t, "SliceOfAny", g.SliceOfAny(1, "b"), []any{1, "b", nil})
	pts := []g.Point{{1, 2}, {3, 4}}
	sp := g.SliceOfSpread(pts)
	if &sp[0] != &pts[0] || len(sp) != 2 {
		t.Error("SliceOfSpread must alias its argument")
	}
	eq(t, "LenIDs", g.LenIDs(g.IDs{1, 2, 3}), 3)
	eq(t, "ReverseIDs", g.ReverseIDs(g.IDs{1, 2, 3}), g.IDs{3, 2, 1})
	eq(t, "ReverseStrs", g.ReverseStrs([]string{"a", "b"}), []string{"b", "a"})
	eq(t, "ReverseStrs empty", g.ReverseStrs(nil), []string{})
	eq(t, "SortInts", g.SortInts([]int{5, -1, 3, 3, 0, math.MinInt}), []int{math.MinInt, -1, 0, 3, 3, 5})
	eq(t, "SortStrs", g.SortStrs([]string{"b", "", "abc", "ab", "B"}), []string{"", "B", "ab", "abc", "b"})
	eq(t, "SortFloat32s", g.SortFloat32s([]float32{2.5, -1, 0, 1e30, -1e30}), []float32{-1e30, -1, 0, 2.5, 1e30})
	eq(t, "SortPointsByY", g.SortPointsByY([]g.Point{{1, 9}, {2, -1}, {3, 5}, {4, 5}}), []g.Point{{2, -1}, {3, 5}, {4, 5}, {1, 9}})
	eq(t, "SortedKeysStrInt", g.SortedKeysStrInt(map[string]int{"q": 1, "a": 2, "m": 3}), []string{"a", "m", "q"})
	eq(t, "SortedKeysIntPoint", g.SortedKeysIntPoint(map[int]*g.Point{3: nil, -1: {}, 2: nil}), []int{-1, 2, 3})
}

func TestFuncValues(t *testing.T) {
	eq(t, "TwiceDouble", g.TwiceDouble(3), 12)
	eq(t, "TwiceStr", g.TwiceStr("ab"), "abababab")
	eq(t, "FuncTable", g.FuncTable(10), []int{10, 22, 22, -13})
	eq(t, "AddN", g.AddN(5, 6), 11)
	eq(t, "AddNF", g.AddNF(0.5, 0.25), float32(0.75))
	eq(t, "Accumulate", g.Accumulate([]int{1, 2, 3, 4}), []int{1, 3, 6, 10})
	eq(t, "AccumulateF", g.AccumulateF([]float64{0.5, 0.25, -1}), []float64{0.5, 0.75, -0.25})
	v, calls := g.MemoFib(80)
	eq(t, "MemoFib", []int{v, calls}, []int{23416728348467685, 81})
	tot, c := g.MemoLen([]string{"a", "bb", "a", "ccc", "bb", "a"})
	eq(t, "MemoLen", []int{tot, c}, []int{10, 3})
	eq(t, "ScalePoints", g.ScalePoints([]g.Point{{1, 2}, {-3, 0}}, -2), []g.Point{{-2, -4}, {6, 0}})
	eq(t, "ScaleVecs", g.ScaleVecs([]g.Vec{{1.5, 2}}, 4), []g.Vec{{6, 8}})
	eq(t, "AreaRects", g.AreaRects([]g.Rect{{2, 3}, {0.5, 4}}), 8.0)
	eq(t, "AreaSqs", g.AreaSqs([]g.Sq{1, 2, 3}), 14.0)
	eq(t, "AreaShapes", g.AreaShapes([]g.Shape{g.Rect{2, 3}, g.Sq(2), &g.Rect{1, 1}}), 11.0)
}

func TestTypes(t *testing.T) {
	a, b, ok := g.PairIntStr(4, "four", 4)
	eq(t, "PairIntStr", []any{a, b, ok}, []any{4, "four", true})
	_, _, ok = g.PairIntStr(4, "four", 5)
	eq(t, "PairIntStr miss", ok, false)
	old, cur, k := g.PairSet("k", g.Point{1, 2}, g.Point{3, 4})
	eq(t, "PairSet", []any{old, cur, k}, []any{g.Point{1, 2}, g.Point{3, 4}, "k"})
	eq(t, "PairAnyKey", []bool{g.PairAnyKey(1, 1), g.PairAnyKey(1, "1"), g.PairAnyKey(nil, nil), g.PairAnyKey("a", "a")}, []bool{true, false, true, true})
	x, y, s := g.PairOfPairs(1, 2, "s")
	eq(t, "PairOfPairs", []any{x, y, s}, []any{1, 2, "ss"})

	eq(t, "ListInts", g.ListInts(3), []int{4, 2, 1, 1, 0, 0, -1})
	eq(t, "ListInts 0", g.ListInts(0), []int{-1})
	eq(t, "ListLens", g.ListLens([]string{"a", "", "abc"}), []int{1, 0, 3})
	n, sum := g.ListPoints([]g.Point{{1, 10}, {2, 20}, {3, 30}})
	eq(t, "ListPoints", []any{n, sum}, []any{3, g.Point{3*1 + 2*2 + 1*3, 60}})

	sorted, h, dups := g.TreeSortInts([]int{5, 3, 8, 1, 4, 5, 9, 3, 10})
	eq(t, "TreeSortInts", []any{sorted, h, dups}, []any{[]int{1, 3, 4, 5, 8, 9, 10}, 4, 2})
	sorted, h, dups = g.TreeSortInts(nil)
	eq(t, "TreeSortInts nil", []any{sorted, h, dups}, []any{[]int{}, 0, 0})
	var big []int
	for i := 0; i < 2000; i++ {
		big = append(big, (i*7919)%2003)
	}
	sorted, _, _ = g.TreeSortInts(big)
	eq(t, "TreeSortInts big", sorted, g.SortInts(big))
	eq(t, "TreeSortStrs", g.TreeSortStrs([]string{"pear", "apple", "fig", "apple"}), []string{"apple", "fig", "pear"})
	m, ok := g.TreeMinFloat([]float64{2.5, -1e9, 3})
	eq(t, "TreeMinFloat", []any{m, ok}, []any{-1e9, true})
	m, ok = g.TreeMinFloat(nil)
	eq(t, "TreeMinFloat empty", []any{m, ok}, []any{0.0, false})
	eq(t, "TreeHasInt", []bool{g.TreeHasInt([]int{2, 1, 3}, 3), g.TreeHasInt([]int{2, 1, 3}, 4), g.TreeHasInt(nil, 0)}, []bool{true, false, false})

	eq(t, "StackReverse", g.StackReverse([]string{"a", "b", "c"}), []string{"c", "b", "a"})
	pt, ok := g.StackPopEmpty()
	eq(t, "StackPopEmpty", []any{pt, ok}, []any{g.Point{}, false})
	eq(t, "SetCount", g.SetCount([]int{1, 2, 1, 3, 2, 1}), 3)
	has, added := g.SetHasStr([]string{"a", "b", "a"}, "b")
	eq(t, "SetHasStr", []any{has, added}, []any{true, 2})
	has, _ = g.SetHasStr([]string{"a"}, "")
	eq(t, "SetHasStr miss", has, false)
	eq(t, "SetPoints", g.SetPoints([]g.Point{{1, 2}, {2, 1}, {1, 2}}), 2)
	label, xs := g.NamedLabel("lst", []int{7, 8})
	eq(t, "NamedLabel", []any{label, xs}, []any{"lst#3", []int{2, 7, 8}})
	eq(t, "NumCalcInt32", g.NumCalcInt32(math.MaxInt32, 3, 5), int32(math.MinInt32+14))
	eq(t, "NumCalcFloat64", g.NumCalcFloat64(1.5, 2, 0.25), 2.0)
	eq(t, "OptOr", []int{g.OptOr(1, true, 9), g.OptOr(1, false, 9)}, []int{1, 9})
	eq(t, "OptOrStr", []string{g.OptOrStr("v", true, "d"), g.OptOrStr("v", false, "d")}, []string{"v", "d"})
}

// The same generic code instantiated by the stock compiler (here) and inside the package must agree.
func TestCrossInstantiation(t *testing.T) {
	var l g.List[string]
	l.Push("x").Push("y")
	l.PushFront("w")
	eq(t, "List[string] here", l.Reversed().Slice(), []string{"y", "x", "w"})
	var tr g.Tree[int]
	for _, x := range []int{5, 3, 8, 1, 4, 5, 9, 3, 10} {
		tr.Insert(x)
	}
	s, h, _ := g.TreeSortInts([]int{5, 3, 8, 1, 4, 5, 9, 3, 10})
	eq(t, "Tree here vs there", []any{tr.Sorted(), tr.Height()}, []any{s, h})
	// values of generic types cross the package boundary
	p := g.MakePair("k", 2.5)
	eq(t, "EqualPair cross", g.EqualPair(p, g.Pair[string, float64]{"k", 2.5}), true)
}
