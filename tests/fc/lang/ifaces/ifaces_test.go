package ifaces_test

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"fclang/ifaces"
)

func eq[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func deq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

// catch runs f and describes its panic ("" when there is none).
func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("%T: %v", r, r)
		}
	}()
	f()
	return ""
}

// a Shape the stock compiler defines: its itabs are built by stock code or by the runtime
type tri struct{ b, h int }

func (t tri) Area() int    { return t.b * t.h / 2 }
func (t tri) Name() string { return "tri" }

type onlyName struct{}

func (onlyName) Name() string { return "only" }

func TestToAny(t *testing.T) {
	s := &ifaces.S{A: 1, B: "x"}
	xs := []int{1, 2, 3}
	m := map[string]int{"a": 1}
	ch := make(chan int, 1)
	for i, c := range []struct {
		got, want any
		typ       string
	}{
		{ifaces.Int(0), 0, "int"}, {ifaces.Int(255), 255, "int"}, {ifaces.Int(256), 256, "int"},
		{ifaces.Int(-1), -1, "int"}, {ifaces.Int(math.MinInt), math.MinInt, "int"},
		{ifaces.Int8(-128), int8(-128), "int8"}, {ifaces.Int8(127), int8(127), "int8"},
		{ifaces.Int16(-300), int16(-300), "int16"}, {ifaces.Int32(1 << 30), int32(1 << 30), "int32"},
		{ifaces.Int64(-1 << 62), int64(-1 << 62), "int64"}, {ifaces.Uint(1<<64 - 1), uint(1<<64 - 1), "uint"},
		{ifaces.Uint8(0), uint8(0), "uint8"}, {ifaces.Uint8(200), uint8(200), "uint8"},
		{ifaces.Uint16(65535), uint16(65535), "uint16"}, {ifaces.Uint32(1<<32 - 1), uint32(1<<32 - 1), "uint32"},
		{ifaces.Uint64(1 << 63), uint64(1 << 63), "uint64"}, {ifaces.Uintptr(7), uintptr(7), "uintptr"},
		{ifaces.Float32(1.5), float32(1.5), "float32"}, {ifaces.Float64(-2.25), -2.25, "float64"},
		{ifaces.Float64(0), 0.0, "float64"}, {ifaces.Float64(math.Inf(1)), math.Inf(1), "float64"},
		{ifaces.Complex128(complex(1, -2)), complex(1, -2), "complex128"},
		{ifaces.Bool(true), true, "bool"}, {ifaces.Bool(false), false, "bool"},
		{ifaces.String(""), "", "string"}, {ifaces.String("hello"), "hello", "string"},
		{ifaces.Array([3]int{1, 2, 3}), [3]int{1, 2, 3}, "[3]int"},
		{ifaces.Struct(ifaces.S{A: 5, B: "b"}), ifaces.S{A: 5, B: "b"}, "ifaces.S"},
		{ifaces.Ptr(s), s, "*ifaces.S"}, {ifaces.Ptr(nil), (*ifaces.S)(nil), "*ifaces.S"},
		{ifaces.Chan(ch), ch, "chan int"},
		{ifaces.ZeroStruct(), struct{}{}, "struct {}"}, {ifaces.ZeroArray(), [0]int{}, "[0]int"},
		{ifaces.Named(9), ifaces.MyInt(9), "ifaces.MyInt"}, {ifaces.NamedStr("n"), ifaces.MyStr("n"), "ifaces.MyStr"},
		{ifaces.Nested(7), 7, "int"}, {ifaces.Nested(nil), nil, "<nil>"}, {ifaces.Nested(ifaces.Int(3)), 3, "int"},
		{ifaces.ErrAny(ifaces.ErrNotFound), ifaces.ErrNotFound, "*errors.errorString"}, {ifaces.ErrAny(nil), nil, "<nil>"},
		{ifaces.Rune('x'), 'x', "int32"},
		{ifaces.MakeShape(0, 2, 3), ifaces.Rect{W: 2, H: 3}, "ifaces.Rect"}, {ifaces.MakeShape(1, 2, 3), ifaces.Sq(2), "ifaces.Sq"},
		{ifaces.MakeShape(3, 0, 0), ifaces.Empty{}, "ifaces.Empty"}, {ifaces.MakeShape(9, 0, 0), nil, "<nil>"},
		{ifaces.MakeShape(4, 1, 2), ifaces.Big{A: 1, B: 2, C: 1, D: 2, E: 1, S: "big"}, "ifaces.Big"},
	} {
		if c.got != c.want {
			t.Errorf("ToAny %d: got %v (%T), want %v (%T)", i, c.got, c.got, c.want, c.want)
		}
		eq(t, fmt.Sprintf("ToAny %d type", i), fmt.Sprintf("%T", c.got), c.typ)
	}
	if f := ifaces.Float64(math.NaN()).(float64); f == f {
		t.Error("NaN in interface")
	}
	if v := ifaces.Slice(xs).([]int); &v[0] != &xs[0] || len(v) != 3 || cap(v) != 3 {
		t.Error("Slice")
	}
	if v := ifaces.Slice(nil).([]int); v != nil {
		t.Error("Slice nil")
	}
	ifaces.Map(m).(map[string]int)["b"] = 2
	eq(t, "Map identity", len(m), 2)
	eq(t, "Func", ifaces.Func(func() int { return 42 }).(func() int)(), 42)
	eq(t, "Func nil", ifaces.Func(nil).(func() int) == nil, true)
	eq(t, "Func nil is not nil any", ifaces.Func(nil) == nil, false)

	var types []string
	for _, v := range ifaces.Consts() {
		types = append(types, fmt.Sprintf("%T=%v", v, v))
	}
	eq(t, "Consts", strings.Join(types, " "),
		"int=1 int32=97 float64=2.5 string=s bool=true int=1099511627776 <nil>=<nil> complex128=(0+2i) uint8=200 ifaces.MyInt=3")

	v, s2 := ifaces.CopyOnConvert(ifaces.S{A: 1, B: "a"})
	eq(t, "CopyOnConvert iface", v, any(ifaces.S{A: 1, B: "a"}))
	eq(t, "CopyOnConvert value", s2, ifaces.S{A: 2, B: "a!"})
}

func TestShapes(t *testing.T) {
	wantArea := []int{12, 9, 27, 0, 17, -1, 12}
	wantName := []string{"rect", "sq", "circle:c", "empty", "big", "func", "map"}
	var all []ifaces.Shape
	for kind := 0; kind <= 6; kind++ {
		s := ifaces.MakeShape(kind, 3, 4)
		all = append(all, s)
		eq(t, "stock Area", s.Area(), wantArea[kind])
		eq(t, "stock Name", s.Name(), wantName[kind])
		eq(t, "AreaOf", ifaces.AreaOf(s), wantArea[kind])
		eq(t, "NameOf", ifaces.NameOf(s), wantName[kind])
		eq(t, "AreaFunc", ifaces.AreaFunc(s)(), wantArea[kind])
		eq(t, "ShapeToNamer", ifaces.ShapeToNamer(s).Name(), wantName[kind])
		eq(t, "NamerToShape", ifaces.NamerToShape(s).Area(), wantArea[kind])
		eq(t, "ShapeToAny type", reflect.TypeOf(ifaces.ShapeToAny(s)), reflect.TypeOf(s))
		eq(t, "ToScalable", ifaces.ToScalable(s) != nil, kind == 2)
	}
	deq(t, "CallAll", ifaces.CallAll(3, 4), []int{12, 12, 4, 9, 9, 2, 27, 27, 8, 0, 0, 5, 17, 17, 3, -1, -1, 4, 12, 12, 3})

	eq(t, "AreaOf stock type", ifaces.AreaOf(tri{3, 4}), 6)
	eq(t, "NameOf stock type", ifaces.NameOf(onlyName{}), "only")
	all = append(all, nil, tri{10, 10})
	total, names := ifaces.SumAreas(all)
	eq(t, "SumAreas total", total, 12+9+27+0+17-1+12+50)
	eq(t, "SumAreas names", names, "rect,sq,circle:c,empty,big,func,map,<nil>,tri,")

	c := &ifaces.Circle{R: 2, Label: "x"}
	eq(t, "ScaleAndArea", ifaces.ScaleAndArea(c, 3), 108)
	eq(t, "ScaleAndArea effect", c.R, 6)
	eq(t, "ToScalable identity", ifaces.ToScalable(c), ifaces.ScalableShape(c))
	eq(t, "AreaOf nil", catch(func() { ifaces.AreaOf(nil) }), catch(func() { stockArea(nil) }))
}

func stockArea(s ifaces.Shape) int { return s.Area() }

// the stock compiler's versions of the assertions
func sInt(v any) int                            { return v.(int) }
func sString(v any) string                      { return v.(string) }
func sS(v any) ifaces.S                         { return v.(ifaces.S) }
func sShape(v any) ifaces.Shape                 { return v.(ifaces.Shape) }
func sRect(s ifaces.Shape) ifaces.Rect          { return s.(ifaces.Rect) }
func sCircle(s ifaces.Shape) *ifaces.Circle     { return s.(*ifaces.Circle) }
func sScaler(s ifaces.Shape) ifaces.Scaler      { return s.(ifaces.Scaler) }
func sError(v any) error                        { return v.(error) }
func sMyInt(v any) ifaces.MyInt                 { return v.(ifaces.MyInt) }
func sNamerToShape(n ifaces.Namer) ifaces.Shape { return n.(ifaces.Shape) }

func TestAssertions(t *testing.T) {
	vals := []any{nil, 0, 1, int64(1), "s", "", 1.5, ifaces.MyInt(1), ifaces.S{A: 1}, &ifaces.S{A: 2}, []int{1},
		ifaces.Rect{W: 1, H: 2}, ifaces.Sq(3), &ifaces.Circle{R: 1}, ifaces.Circle{R: 1}, tri{1, 2}, onlyName{},
		errors.New("e"), ifaces.Code(3), struct{}{}, ifaces.Empty{}, uint8(1), true, (*ifaces.Circle)(nil)}
	check := func(name string, i int, got, want func()) {
		t.Helper()
		w := catch(want)
		g := catch(got)
		if g != w {
			t.Errorf("%s(vals[%d] = %T): panic %q, stock %q", name, i, vals[i], g, w)
		}
	}
	panics := 0
	for i, v := range vals {
		if catch(func() { sInt(v) }) != "" {
			panics++
		}
		check("AsInt", i, func() { ifaces.AsInt(v) }, func() { sInt(v) })
		check("AsString", i, func() { ifaces.AsString(v) }, func() { sString(v) })
		check("AsS", i, func() { ifaces.AsS(v) }, func() { sS(v) })
		check("AsShape", i, func() { ifaces.AsShape(v) }, func() { sShape(v) })
		check("AsError", i, func() { ifaces.AsError(v) }, func() { sError(v) })
		check("AsMyInt", i, func() { ifaces.AsMyInt(v) }, func() { sMyInt(v) })
		if n, ok := v.(ifaces.Namer); ok {
			check("NamerToShape", i, func() { ifaces.NamerToShape(n) }, func() { sNamerToShape(n) })
		}
		s, _ := v.(ifaces.Shape)
		check("AsRect", i, func() { ifaces.AsRect(s) }, func() { sRect(s) })
		check("AsCircle", i, func() { ifaces.AsCircle(s) }, func() { sCircle(s) })
		check("AsScaler", i, func() { ifaces.AsScaler(s) }, func() { sScaler(s) })

		// comma-ok forms never panic and give the zero value on failure
		{
			g, gok := ifaces.AsIntOk(v)
			w, wok := v.(int)
			eq(t, "AsIntOk", fmt.Sprint(g, gok), fmt.Sprint(w, wok))
		}
		{
			g, gok := ifaces.AsStringOk(v)
			w, wok := v.(string)
			eq(t, "AsStringOk", fmt.Sprint(g, gok), fmt.Sprint(w, wok))
		}
		{
			g, gok := ifaces.AsPtrOk(v)
			w, wok := v.(*ifaces.S)
			eq(t, "AsPtrOk", g == w && gok == wok, true)
		}
		{
			g, gok := ifaces.AsSliceOk(v)
			w, wok := v.([]int)
			eq(t, "AsSliceOk", len(g) == len(w) && gok == wok, true)
		}
		{
			g, gok := ifaces.AsFloatOk(v)
			w, wok := v.(float64)
			eq(t, "AsFloatOk", g == w && gok == wok, true)
		}
		{
			g, gok := ifaces.AsShapeOk(v)
			w, wok := v.(ifaces.Shape)
			eq(t, "AsShapeOk", g == w && gok == wok, true)
		}
		{
			g, gok := ifaces.AsRectOk(s)
			w, wok := s.(ifaces.Rect)
			eq(t, "AsRectOk", g == w && gok == wok, true)
		}
		{
			g, gok := ifaces.AsScalerOk(s)
			w, wok := s.(ifaces.Scaler)
			eq(t, "AsScalerOk", g == w && gok == wok, true)
		}
	}
	if panics != len(vals)-2 {
		t.Errorf("stock: %d of %d int assertions panicked", panics, len(vals))
	}
	// spot checks of the texts, so a change in the stock runtime is noticed
	eq(t, "text 1", catch(func() { ifaces.AsInt("s") }), "*runtime.TypeAssertionError: interface conversion: interface {} is string, not int")
	eq(t, "text 2", catch(func() { ifaces.AsInt(nil) }), "*runtime.TypeAssertionError: interface conversion: interface {} is nil, not int")
	eq(t, "text 3", catch(func() { ifaces.AsShape(1) }), "*runtime.TypeAssertionError: interface conversion: int is not ifaces.Shape: missing method Area")
	eq(t, "text 4", catch(func() { ifaces.AsRect(ifaces.Sq(1)) }), "*runtime.TypeAssertionError: interface conversion: ifaces.Shape is ifaces.Sq, not ifaces.Rect")
	eq(t, "text 5", catch(func() { ifaces.AsShape(nil) }), "*runtime.TypeAssertionError: interface conversion: interface is nil, not ifaces.Shape")
	eq(t, "text 6", catch(func() { ifaces.AsScaler(ifaces.Rect{}) }), "*runtime.TypeAssertionError: interface conversion: ifaces.Rect is not ifaces.Scaler: missing method Scale")
	if e := func() (e error) {
		defer func() { e = recover().(error) }()
		ifaces.AsInt("s")
		return nil
	}(); e == nil {
		t.Error("the panic value is not an error")
	}
	eq(t, "AsAny", ifaces.AsAny(ifaces.Sq(2)), any(ifaces.Sq(2)))
	eq(t, "AsAny nil", catch(func() { ifaces.AsAny(nil) }), catch(func() { _ = ifaces.Shape(nil).(any) }))
}

func TestTypeSwitch(t *testing.T) {
	n := 1
	for _, c := range []struct {
		v    any
		want string
	}{
		{nil, "nil"}, {1, "int"}, {int8(1), "int8/16"}, {int16(1), "int8/16"}, {int32(1), "int32"}, {'r', "int32"},
		{int64(1), "int64"}, {uint(1), "unsigned"}, {uint8(1), "unsigned"}, {uint16(1), "unsigned"},
		{uint32(1), "unsigned"}, {uint64(1), "unsigned"}, {uintptr(1), "unsigned"}, {float32(1), "float"},
		{1.0, "float"}, {complex64(1), "complex"}, {1i, "complex"}, {true, "bool"}, {"s", "string"},
		{[]int{1}, "[]int"}, {[]int(nil), "[]int"}, {[]string{}, "[]string"}, {[3]int{}, "[3]int"}, {[4]int{}, "other"},
		{map[string]int{}, "map"}, {map[int]string{}, "other"}, {make(chan int), "chan"}, {make(<-chan int), "other"},
		{func() int { return 0 }, "func"}, {func() {}, "other"}, {&ifaces.S{}, "*S"}, {ifaces.S{}, "S"},
		{struct{}{}, "struct{}"}, {ifaces.MyInt(1), "MyInt"}, {errors.New("x"), "error"}, {ifaces.Code(1), "error"},
		{ifaces.Rect{}, "Shape"}, {tri{}, "Shape"}, {&ifaces.Circle{}, "Shape"}, {ifaces.Circle{}, "other"},
		{&n, "*int"}, {ifaces.MyStr("s"), "other"}, {onlyName{}, "other"}, {[]any{1}, "other"},
	} {
		eq(t, fmt.Sprintf("Classify(%T)", c.v), ifaces.Classify(c.v), c.want)
	}
	for _, c := range []struct {
		v    any
		want string
	}{
		{41, "int:42"}, {-1, "int:0"}, {"héllo", "string:6:héllo"}, {"", "string:0:"}, {true, "true"}, {false, "false"},
		{[]int{1, 2}, "slice:2"}, {ifaces.S{A: 7, B: "seven"}, "S:7seven"}, {&ifaces.S{A: -7, B: "x"}, "*S:-7x"},
		{(*ifaces.S)(nil), "*S:nil"}, {int8(1), "int8"}, {uint8(1), "uint8"},
		{&ifaces.Circle{R: 1, Label: "c"}, "scalable:circle:c:12"}, {ifaces.Rect{W: 2, H: 5}, "shape:rect:10"},
		{tri{4, 4}, "shape:tri:8"}, {ifaces.Sq(3), "shape:sq:9"}, {errors.New("boom"), "error:boom"},
		{ifaces.Code(5), "error:code 5"}, {nil, "nil"}, {1.5, "default"}, {onlyName{}, "default"},
	} {
		eq(t, fmt.Sprintf("Describe(%T)", c.v), ifaces.Describe(c.v), c.want)
	}
	eq(t, "DefaultFirst Rect", ifaces.DefaultFirst(ifaces.Rect{W: 3}), "Rect3")
	eq(t, "DefaultFirst Circle", ifaces.DefaultFirst(&ifaces.Circle{Label: "z"}), "CircleOrSq:circle:z")
	eq(t, "DefaultFirst Sq", ifaces.DefaultFirst(ifaces.Sq(1)), "CircleOrSq:sq")
	eq(t, "DefaultFirst tri", ifaces.DefaultFirst(tri{}), "default")
	eq(t, "DefaultFirst nil", ifaces.DefaultFirst(nil), "default")
	eq(t, "SwitchNoMatch int", ifaces.SwitchNoMatch(5), 5)
	eq(t, "SwitchNoMatch string", ifaces.SwitchNoMatch("abc"), 3)
	eq(t, "SwitchNoMatch other", ifaces.SwitchNoMatch(1.5), -1)
	eq(t, "SwitchNoMatch nil", ifaces.SwitchNoMatch(nil), -1)
}

func stockEq(a, b any) bool { return a == b }

func TestEquality(t *testing.T) {
	s1, s2 := &ifaces.S{A: 1}, &ifaces.S{A: 1}
	ch := make(chan int)
	f := func() int { return 1 }
	m := map[string]int{}
	ab := "a"
	ab += "b"
	vals := []any{
		nil, 0, 1, 2, int64(1), int8(1), uint8(1), uint(1), 1 << 40, 1 << 40, "", "ab", ab, "abc", 1.5, 1.5, float32(1.5),
		0.0, math.Copysign(0, -1), math.NaN(), math.Inf(1), true, false, complex(1, 2), complex(1, 2),
		ifaces.S{A: 1, B: "a"}, ifaces.S{A: 1, B: "a"}, ifaces.S{A: 1, B: "b"}, s1, s1, s2, (*ifaces.S)(nil),
		ifaces.Rect{W: 1, H: 2}, ifaces.Rect{W: 1, H: 2}, ifaces.Rect{W: 2, H: 1}, ifaces.Sq(1), ifaces.MyInt(1), ifaces.MyStr("ab"),
		[3]int{1, 2, 3}, [3]int{1, 2, 3}, [3]int{1, 2, 4}, struct{}{}, ifaces.Empty{}, [0]int{}, ch, ch, make(chan int),
		ifaces.ErrNotFound, ifaces.ErrPerm, ifaces.Code(1), ifaces.Code(1), ifaces.Big{S: "x"}, ifaces.Big{S: "x"}, ifaces.Big{S: "y"},
		[]int{1}, []int(nil), m, f, ifaces.MakeUnc(1), ifaces.Unc{}, ifaces.FuncShape(f), ifaces.MapShape(nil),
		[2]any{1, "x"}, [2]any{1, "x"}, [2]any{1, []int{}}, struct{ F any }{[]int{}}, struct{ F any }{1},
		[2]float64{math.NaN(), 0}, tri{1, 2}, tri{1, 2},
	}
	trues, panics := 0, 0
	for i, a := range vals {
		for j, b := range vals {
			var want, got, gotNe bool
			wp := catch(func() { want = stockEq(a, b) })
			gp := catch(func() { got = ifaces.EqAny(a, b) })
			np := catch(func() { gotNe = ifaces.NeAny(a, b) })
			if got != want || gp != wp {
				t.Errorf("EqAny(vals[%d] %T, vals[%d] %T) = %v %q, stock %v %q", i, a, j, b, got, gp, want, wp)
			}
			if np != wp || (wp == "" && gotNe == want) {
				t.Errorf("NeAny(vals[%d] %T, vals[%d] %T) = %v %q, stock == %v %q", i, a, j, b, gotNe, np, want, wp)
			}
			if want {
				trues++
			}
			if wp != "" {
				panics++
			}
		}
	}
	if trues < len(vals) || panics < 10 {
		t.Errorf("stock: %d equal pairs, %d panics", trues, panics)
	}
	eq(t, "uncomparable text", catch(func() { ifaces.EqAny([]int{1}, []int{1}) }), "runtime.errorString: runtime error: comparing uncomparable type []int")
	eq(t, "uncomparable text 2", catch(func() { ifaces.EqAny(ifaces.MakeUnc(1), ifaces.MakeUnc(1)) }), "runtime.errorString: runtime error: comparing uncomparable type ifaces.Unc")
	eq(t, "uncomparable text 3", catch(func() { ifaces.EqAny(func() {}, func() {}) }), "runtime.errorString: runtime error: comparing uncomparable type func()")

	shapes := []ifaces.Shape{nil, ifaces.Rect{W: 1, H: 2}, ifaces.Rect{W: 1, H: 2}, ifaces.Sq(2), ifaces.Sq(2), ifaces.Sq(3),
		&ifaces.Circle{}, ifaces.NilCircle(), (*ifaces.Circle)(nil), ifaces.Empty{}, tri{1, 1}, tri{1, 1},
		ifaces.FuncShape(f), ifaces.MapShape{}}
	for i, a := range shapes {
		for j, b := range shapes {
			var want, got, got2 bool
			wp := catch(func() { want = a == b })
			gp := catch(func() { got = ifaces.EqShape(a, b) })
			gp2 := catch(func() { got2 = ifaces.EqShapeAny(a, b) })
			if got != want || gp != wp || got2 != want || gp2 != wp {
				t.Errorf("EqShape(%d, %d) = %v %v %q %q, stock %v %q", i, j, got, got2, gp, gp2, want, wp)
			}
		}
		eq(t, "EqRect", ifaces.EqRect(a, ifaces.Rect{W: 1, H: 2}), i == 1 || i == 2)
		eq(t, "IsNilShape", ifaces.IsNilShape(a), i == 0)
	}
	for _, v := range vals[:20] {
		eq(t, fmt.Sprintf("EqInt(%T %v)", v, v), ifaces.EqInt(v, 1), v == 1)
		eq(t, fmt.Sprintf("EqStr(%T %v)", v, v), ifaces.EqStr(v, "ab"), v == "ab")
		eq(t, "IsNil", ifaces.IsNil(v), v == nil)
	}
	eq(t, "EqErr same", ifaces.EqErr(ifaces.ErrPerm, ifaces.ErrPerm), true)
	eq(t, "EqErr different", ifaces.EqErr(ifaces.ErrPerm, errors.New("permission denied")), false)
	eq(t, "EqErr codes", ifaces.EqErr(ifaces.Code(1), ifaces.Code(1)), true)
	eq(t, "EqErr nil", ifaces.EqErr(nil, nil), true)

	p := ifaces.KeyPair{K: "k", V: ifaces.Sq(1)}
	eq(t, "EqPair same", ifaces.EqPair(p, ifaces.KeyPair{K: "k", V: ifaces.Sq(1)}), true)
	eq(t, "EqPair key", ifaces.EqPair(p, ifaces.KeyPair{K: 1, V: ifaces.Sq(1)}), false)
	eq(t, "EqPair value", ifaces.EqPair(p, ifaces.KeyPair{K: "k"}), false)
	u := ifaces.KeyPair{K: []int{}}
	eq(t, "EqPair uncomparable", catch(func() { ifaces.EqPair(u, u) }), catch(func() { _ = stockEq(u, u) }))
	eq(t, "EqArr", ifaces.EqArr([3]any{1, "a", nil}, [3]any{1, "a", nil}), true)
	eq(t, "EqArr diff", ifaces.EqArr([3]any{1, "a", nil}, [3]any{1, "a", 0}), false)
	// arrays compare element by element and stop at the first difference: no panic here
	arrA, arrB := [3]any{1, []int{}, 2}, [3]any{2, []int{}, 2}
	eq(t, "EqArr early out", catch(func() { ifaces.EqArr(arrA, arrB) }), catch(func() { _ = arrA == arrB }))
	arrB[0] = 1
	eq(t, "EqArr panic", catch(func() { ifaces.EqArr(arrA, arrB) }), catch(func() { _ = arrA == arrB }))
}

func TestNil(t *testing.T) {
	eq(t, "NilShape", ifaces.NilShape() == nil, true)
	nc := ifaces.NilCircle()
	eq(t, "NilCircle != nil", nc != nil, true)
	eq(t, "NilCircle type", fmt.Sprintf("%T", nc), "*ifaces.Circle")
	eq(t, "NilCircle value", nc.(*ifaces.Circle) == nil, true)
	eq(t, "IsNilShape(NilCircle)", ifaces.IsNilShape(nc), false)
	eq(t, "IsNil(NilCircle)", ifaces.IsNil(nc), false)
	eq(t, "AreaOf(NilCircle)", catch(func() { ifaces.AreaOf(nc) }), catch(func() { stockArea(nc) }))

	eq(t, "MaybeErr nil", ifaces.MaybeErr(false, false) == nil, true)
	e := ifaces.MaybeErr(false, true)
	eq(t, "MaybeErr typed nil", e != nil, true)
	eq(t, "MaybeErr typed nil IsNilErr", ifaces.IsNilErr(e), false)
	eq(t, "MaybeErr typed nil text", e.Error(), "<nil PathErr>")
	eq(t, "MaybeErr fail", ifaces.MaybeErr(true, false).Error(), "open /x: not found")
	eq(t, "MaybeErr fail typed", ifaces.ErrText(ifaces.MaybeErr(true, true)), "open /x: not found")
	eq(t, "IsNilErr", ifaces.IsNilErr(nil), true)
}

func TestErrors(t *testing.T) {
	for _, c := range []struct {
		key, text      string
		notFound, perm bool
		path           string
		code           int
		is42, is7, is0 bool
	}{
		{"", "<nil>", false, false, "", -1, false, false, false},
		{"missing", "not found", true, false, "", -1, false, false, false},
		{"secret", "open secret: permission denied", false, true, "open@secret", -1, false, false, false},
		{"deep", "stat deep: open inner: not found", true, false, "stat@deep", -1, false, false, false},
		{"code", "code 42", false, false, "", 42, true, false, true},
		{"wrapped-code", "exec wrapped-code: code 7", false, false, "exec@wrapped-code", 7, false, true, true},
		{"zz", "unknown key zz", false, false, "", -1, false, false, false},
	} {
		err := ifaces.Find(c.key)
		eq(t, c.key+" text", ifaces.ErrText(err), c.text)
		eq(t, c.key+" IsNotFound", ifaces.IsNotFound(err), c.notFound)
		eq(t, c.key+" stock errors.Is", errors.Is(err, ifaces.ErrNotFound), c.notFound)
		eq(t, c.key+" IsPerm", ifaces.IsPerm(err), c.perm)
		p, ok := ifaces.PathOf(err)
		eq(t, c.key+" PathOf", p, c.path)
		eq(t, c.key+" PathOf ok", ok, c.path != "")
		var pe *ifaces.PathErr
		eq(t, c.key+" stock errors.As", errors.As(err, &pe), c.path != "")
		eq(t, c.key+" CodeOf", ifaces.CodeOf(err), c.code)
		eq(t, c.key+" IsCode 42", ifaces.IsCode(err, 42), c.is42)
		eq(t, c.key+" IsCode 7", ifaces.IsCode(err, 7), c.is7)
		eq(t, c.key+" IsCode 0", ifaces.IsCode(err, 0), c.is0)
		if err != nil {
			eq(t, c.key+" fmt", fmt.Sprintf("%v", err), c.text)
		}
	}
	j := ifaces.Join(ifaces.ErrNotFound, ifaces.Code(3))
	eq(t, "Join text", j.Error(), "not found\ncode 3")
	eq(t, "Join IsNotFound", ifaces.IsNotFound(j), true)
	eq(t, "Join CodeOf", ifaces.CodeOf(j), 3)
	eq(t, "Join nil", ifaces.Join(nil, nil) == nil, true)
	// an error type of the test package through fc's code
	eq(t, "ErrText stock error", ifaces.ErrText(fmt.Errorf("wrap: %w", ifaces.ErrPerm)), "wrap: permission denied")
	eq(t, "IsPerm stock wrap", ifaces.IsPerm(fmt.Errorf("wrap: %w", ifaces.ErrPerm)), true)
	eq(t, "PathOf stock wrap", ifaces.CodeOf(fmt.Errorf("wrap: %w", ifaces.Code(9))), 9)
}
