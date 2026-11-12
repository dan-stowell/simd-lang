package globals_test

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
	"testing"

	g "fclang/globals"
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

func TestInitOrder(t *testing.T) {
	// Worked out by hand from the spec's rule: repeatedly initialize the earliest variable in
	// declaration order (files a.go, b.go, c.go) that has no uninitialized dependencies.
	eq(t, "Order", g.Order(), []string{"V3", "V6", "V4", "V5", "V2", "V1", "V7V8", "V9"})
	eq(t, "Vs", g.Vs(), [9]int{11, 10, 3, 106, 5, 6, 4, 12, 7})
	eq(t, "InitLog", g.InitLog(), []string{"a1", "a2", "b1", "b2", "c1"})
	eq(t, "SawAtInit", g.SawAtInit(), [4]int{11 + 7 + 3 + 24, 2, 30, 255*255 + 4})
}

func TestConsts(t *testing.T) {
	eq(t, "Iotas", g.Iotas(), []int{0, 1, 2, 4, 0, 0, 1, 10, 2, 20})
	eq(t, "Sizes", g.Sizes(), []int{1 << 10, 1 << 20, 1 << 30, 1 << 40})
	eq(t, "Colors", g.Colors(), []g.Color{-1, -2, -3})
	eq(t, "FlagBits", g.FlagBits(), []g.Flags{1, 2, 4, 16, 23})
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "?", "?"}
	for d, want := range names {
		eq(t, "DayName", g.DayName(g.Weekday(d)), want)
	}
	eq(t, "DayName 255", g.DayName(255), "?")
	eq(t, "NextDay", []g.Weekday{g.NextDay(g.Sunday), g.NextDay(g.Saturday), g.NextDay(255)}, []g.Weekday{1, 0, 0})
	eq(t, "HasFlag", []bool{g.HasFlag(g.FAll, g.FDir), g.HasFlag(g.FRead|g.FExec, g.FWrite), g.HasFlag(8, g.FAll)}, []bool{true, false, false})
	for _, x := range []int8{0, 1, -1, 127, -128} {
		eq(t, "AddMaxI8", g.AddMaxI8(x), int8(127+int(x)))
		eq(t, "SubMinI8", g.SubMinI8(x), int8(-128-int(x)))
	}
	eq(t, "AddMaxU16", g.AddMaxU16(2), uint16(1))
	eq(t, "AddMaxU64", g.AddMaxU64(5), uint64(4))
	eq(t, "DivMinI64", []int64{g.DivMinI64(-1), g.DivMinI64(2), g.DivMinI64(math.MinInt64)}, []int64{math.MinInt64, -1 << 62, 1})
	eq(t, "DivMinI64 zero", catch(func() { g.DivMinI64(0) }), catch(func() { _ = math.MinInt64 / int64(zero) }))
	eq(t, "ScalePi32", g.ScalePi32(2), float32(math.Pi)*2)
	eq(t, "ScalePi64", g.ScalePi64(2), math.Pi*2)
	eq(t, "ScaleRatio", g.ScaleRatio(3), (1.0/3)*three)
	eq(t, "ScaleHuge", g.ScaleHuge(0.5), math.Ldexp(1, 99))
	const hello = "héllo, wörld"
	for i := 0; i < len(hello); i++ {
		eq(t, "HelloAt", g.HelloAt(i), hello[i])
	}
	eq(t, "HelloAt oob", catch(func() { g.HelloAt(14) }), catch(func() { _ = hello[zero+14] }))
	a, b := g.HelloLen()
	eq(t, "HelloLen", []int{a, b}, []int{14, 14})
	eq(t, "LetterPlus", g.LetterPlus(1), 'μ')
	eq(t, "SmallFromHuge", g.SmallFromHuge(10), 40+0xff)
	eq(t, "YesAnd", []bool{g.YesAnd(true), g.YesAnd(false)}, []bool{true, false})
}

var (
	zero  = 0
	three = 3.0
)

func TestScalarsAndAggregates(t *testing.T) {
	i8, u64, f32, f64, s, b, r, c := g.Scalars()
	eq(t, "Scalars", []any{i8, u64, f32, f64, s, b, r, c}, []any{int8(-128), uint64(math.MaxUint64), float32(1.5), 2.5e-3, "hello", true, 'x', complex(1, -2)})
	eq(t, "ZerosOK", g.ZerosOK(), true)
	for i, want := range []int{1, 2, 3, 0, 0} {
		eq(t, "ArrAt", g.ArrAt(i), want)
	}
	eq(t, "ArrAt oob", catch(func() { g.ArrAt(5) }), catch(func() { _ = [5]int{}[zero+5] }))
	eq(t, "ArrIdx", []any{g.ArrIdxLen(), g.ArrIdxAt(0), g.ArrIdxAt(2), g.ArrIdxAt(5)}, []any{6, "", "two", "five"})
	eq(t, "SlAt", []int{g.SlAt(0), g.SlAt(2)}, []int{10, 30})
	eq(t, "SlSlAt", []string{g.SlSlAt(0, 0), g.SlSlAt(1, 1)}, []string{"a", "c"})
	eq(t, "SlSlNil", []bool{g.SlSlNil(1), g.SlSlNil(2), g.SlSlNil(3)}, []bool{false, true, false})
	v, ok := g.MAt("two")
	eq(t, "MAt", []any{v, ok}, []any{2, true})
	v, ok = g.MAt("init") // written by the fourth init function
	eq(t, "MAt init", []any{v, ok}, []any{4, true})
	v, ok = g.MAt("nope")
	eq(t, "MAt miss", []any{v, ok}, []any{0, false})
	if r := g.MKeyAt(2, "b"); r == nil || r.Name != "y" || len(r.Tags) != 2 || r.Tags[1] != "t2" {
		t.Error("MKeyAt")
	}
	if g.MKeyAt(1, "a").Val != 1 || g.MKeyAt(1, "b") != nil {
		t.Error("MKeyAt 2")
	}
	eq(t, "MNestedAt", g.MNestedAt("m", 1, 1), "q")
	eq(t, "MNestedAt oob", catch(func() { g.MNestedAt("zz", 1, 0) }), catch(func() { _ = []string(nil)[zero] }))
	eq(t, "SparseAt", []int32{g.SparseAt(0), g.SparseAt(1), g.SparseAt(1000), g.SparseAt(2047)}, []int32{1, 0, -5, 9})
	eq(t, "SparseSum", g.SparseSum(), int64(5))
	for _, i := range []int{0, 1, 16, 255} {
		eq(t, "SquareAt", g.SquareAt(i), int32(i*i))
	}
	if g.BigAt(0).Name != "first" || g.BigAt(9999).Name != "last" || g.BigAt(9999).Val != 10000 || g.BigAt(9999).Next != g.BigAt(0) || g.BigAt(5000).Name != "" {
		t.Error("BigAt")
	}
	eq(t, "BytesAt", []byte{g.BytesAt(0), g.BytesAt(5), g.BytesAt(6)}, []byte{'b', 0, 0xff})
	eq(t, "RuneAt", []rune{g.RuneAt(0), g.RuneAt(1), g.RuneAt(2)}, []rune{'a', 'ñ', '世'})
	eq(t, "Runes len", len(g.Runes), 3)
}

func TestTable(t *testing.T) {
	eq(t, "TableLen", g.TableLen(), 24)
	e := g.TableAt(5)
	if e.Sym != "C" || e.Num != 6 || e.Mass != 12.011 || e.Day != g.Saturday || e.Period != 2 || e.Link != &g.Aux[2] {
		t.Errorf("TableAt(5) = %+v", e)
	}
	eq(t, "TableAt(20)", g.TableAt(20), g.Entry{Sym: "Sc", Num: 21, Period: 4})
	eq(t, "TableAt(21)", g.TableAt(21), g.Entry{Sym: "Ti", Num: 22, Mass: 47.867})
	eq(t, "TableAt(23)", g.TableAt(23), g.Entry{})
	sum, mass := 0, float32(0)
	for i := 0; i < g.TableLen(); i++ {
		sum += int(g.TableAt(i).Num)
		mass += g.TableAt(i).Mass
	}
	eq(t, "nums", sum, 276)
	eq(t, "SumTable", g.SumTable, 276)
	var want float32
	for _, m := range []float32{1.008, 4.0026, 6.94, 9.0122, 10.81, 12.011, 14.007, 15.999, 18.998, 20.180, 22.990, 24.305, 26.982, 28.085, 30.974, 32.06, 35.45, 39.948, 39.098, 40.078, 0, 47.867, 0, 0} {
		want += m
	}
	eq(t, "mass", mass, want)
	for i, want := range map[int]string{0: "", 1: "aux0", 2: "aux1", 5: "aux2", 9: "aux1", 10: "aux2", 17: "aux0", 18: "aux1", 23: ""} {
		eq(t, fmt.Sprint("LinkSym ", i), g.LinkSym(i), want)
	}
	if g.Aux[1].Link != &g.Aux2 || g.Aux2.Num != 2 || g.PEntry != &g.Table[3] {
		t.Error("Aux links")
	}
	old, now := g.EntryViaPtr(1.25)
	eq(t, "EntryViaPtr", []float32{old, now, g.Table[3].Mass}, []float32{9.0122, 1.25, 9.0122})
}

func TestNested(t *testing.T) {
	n := g.Nested
	eq(t, "In", n.In, g.Inner{-1, 1.5, "in"})
	eq(t, "P", *n.P, g.Inner{X: 2, S: "ptr"})
	eq(t, "Arr", n.Arr, [2]g.Inner{{3, 3.5, "a0"}, {4, 4.5, "a1"}})
	eq(t, "Sl", n.Sl, []g.Inner{{S: "s0"}, {X: 5}})
	eq(t, "M", n.M, map[string]g.Inner{"k": {6, 6.5, "mk"}})
	r := g.RecWithAll
	if r.Name != "all" || r.Val != 24 || r.Next != g.P || len(r.Tags) != 3 || r.Attr["w"] != 3 || r.F(4) != 8 {
		t.Errorf("RecWithAll %+v", r)
	}
	eq(t, "RecWithAll.Any", r.Any, any([]g.Key{{1, "one"}}))
	if g.P.Name != "p" || g.P.Next.Name != "q" || g.P.Next.Tags[0] != "leaf" || g.P.Next.Next != nil || *g.PP != g.P {
		t.Error("P")
	}
	eq(t, "RingWalk", g.RingWalk(5), "r0r1r0r1r0")
}

func TestFuncsAndIfaces(t *testing.T) {
	eq(t, "CallFn", []int{g.CallFn(0, 7), g.CallFn(1, 7), g.CallFn(2, 7)}, []int{14, 1021, -7})
	eq(t, "FnIsNil", []bool{g.FnIsNil(0), g.FnIsNil(3)}, []bool{false, true})
	eq(t, "CallFn nil", catch(func() { g.CallFn(3, 1) }), catch(func() { var f func(int) int; f(1) }))
	eq(t, "CallFnMap", []int{g.CallFnMap("double", 4), g.CallFnMap("lit", 4)}, []int{8, 1012})
	eq(t, "CallFnMap miss", catch(func() { g.CallFnMap("none", 1) }), catch(func() { var f func(int) int; f(1) }))
	eq(t, "CallFnVar", g.CallFnVar(5), 10+1015)
	eq(t, "SetOffset", g.SetOffset(7), 10)
	eq(t, "SetOffset back", g.SetOffset(1000), 1003)
	want := []any{1, "two", 3.0, nil, g.Key{4, "four"}, g.PCounter, [2]bool{true, false}, g.Friday}
	for i, w := range want {
		eq(t, fmt.Sprint("IfaceAt ", i), g.IfaceAt(i), w)
	}
	eq(t, "Iface", []any{g.Iface, g.IfaceS}, []any{42, "str"})
	if g.IfaceP != any(g.P) {
		t.Error("IfaceP")
	}
	if r, ok := g.IfaceRec.(g.Rec); !ok || r.Name != "boxed" || r.Val != 7 {
		t.Error("IfaceRec")
	}
	eq(t, "ErrText", g.ErrText(), "boom")
	eq(t, "StrText", g.StrText(), "stringer,Wed")
}

func TestComputed(t *testing.T) {
	eq(t, "computed", []any{g.LenArr, g.FromFunc, g.Sub, g.Joined, g.MLen, g.FromConst, g.Q, g.Rm, g.FirstSym},
		[]any{5, 54, "ell", "hello/héllo, wörld", 3, 1024 * math.Pi, 9, 2, "H"})
}

// Mutations: run once, in a fixed order, since they change package state.
func TestMutation(t *testing.T) {
	eq(t, "Count", []int{g.Count(), g.Count(), g.CountViaPtr(), g.Count()}, []int{1, 2, 12, 13})
	if g.IfaceAt(5).(*int) != g.PCounter || *g.IfaceAt(5).(*int) != 13 {
		t.Error("counter through interface")
	}
	g.SetArr(2, 77)
	eq(t, "aliases", []int{g.ElemViaPtr(), g.SlOfArrAt(1), g.ArrAt(2), g.SlOfArrAt(0), len(g.SlOfArr), cap(g.SlOfArr)}, []int{77, 77, 77, 2, 3, 4})
	g.SetArr(2, 3)
	eq(t, "SetNestedY", g.SetNestedY(10), 14.5)
	eq(t, "PField", *g.PField, 10.0)
	a, b, c := g.BumpP()
	eq(t, "BumpP", []int{a, b, c, g.RecWithAll.Next.Val}, []int{2, 3, 3, 3})
	eq(t, "NextClosure", []int{g.NextClosure(), g.NextClosure()}, []int{101, 102})
	n, s := g.SwapGlobals()
	eq(t, "SwapGlobals", []any{n, s}, []any{1000 - 128 + 5, "hello"})
	i8, _, f32, _, _, _, _, _ := g.Scalars()
	eq(t, "restored", []any{i8, f32, g.ZeroI, g.ZeroS}, []any{int8(-128), float32(1.5), 0, ""})
}
