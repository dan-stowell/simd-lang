package methods_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"fclang/methods"
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

type P = methods.Point

func TestReceivers(t *testing.T) {
	for _, c := range []struct {
		p P
		k int
	}{{P{3, 4}, 2}, {P{0, 0}, 5}, {P{-1, 7}, -3}} {
		a, b, s, q := methods.ValueCalls(c.p, c.k)
		eq(t, "ValueCalls a", a, c.p.X+c.p.Y)
		eq(t, "ValueCalls b", b, c.p.X+c.p.Y)
		eq(t, "ValueCalls arr", s, (c.p.X+c.p.Y)*c.k)
		eq(t, "ValueCalls scaled", q, P{c.p.X * c.k * c.k, c.p.Y * c.k * c.k})
	}
	r, x := methods.CopyReceiver(P{1, 2}, 99)
	eq(t, "CopyReceiver r", r, 99)
	eq(t, "CopyReceiver x", x, 1)

	p, h, e := methods.AutoAddr(P{1, 2}, 10, 20)
	eq(t, "AutoAddr var", p, P{11, 22})
	eq(t, "AutoAddr field", h, P{21, 42})
	eq(t, "AutoAddr elem", e, P{31, 62})

	q := P{1, 2}
	eq(t, "Chain", methods.Chain(&q, 3, 5), 45)
	eq(t, "Chain effect", q, P{15, 30})

	a, b, c, d := methods.MultiResults(P{3, 4})
	deq(t, "MultiResults", []int{a, b, c, d}, []int{3, 4, 4, 3})

	v0, v3, vs := methods.Variadic(P{1, 2}, []int{10})
	deq(t, "Variadic", []int{v0, v3, vs}, []int{3, 9, 13})
	v0, v3, vs = methods.Variadic(P{1, 2}, nil)
	deq(t, "Variadic nil", []int{v0, v3, vs}, []int{3, 9, 3})

	m := map[string]*P{"a": {1, 2}, "b": {5, 5}}
	eq(t, "MapOfPointers", methods.MapOfPointers(m, "a", 10), 23)
	eq(t, "MapOfPointers effect", *m["a"], P{11, 12})
	eq(t, "MapOfPointers missing", catch(func() { methods.MapOfPointers(m, "zz", 1) }), catch(func() { m["zz"].Move(1, 1) }))

	// the stock compiler calling fc's methods directly
	q = P{2, 3}
	eq(t, "direct Sum", q.Sum(), 5)
	q.Move(1, 1)
	eq(t, "direct Move", q, P{3, 4})
	eq(t, "direct Scale", *q.Scale(2), P{6, 8})
	eq(t, "direct Dot", q.Dot(P{1, 10}), 86)
	eq(t, "direct String", q.String(), "(6,8)")
	eq(t, "direct String negative", P{-10, 0}.String(), "(-10,0)")
	mv := q.Sum
	eq(t, "method value", mv(), 14)
	eq(t, "method expr", (*P).IsNil(nil), true)
}

func TestEmbedding(t *testing.T) {
	n := methods.Named{Point: P{1, 2}, Name: "n"}
	s, str, title := methods.PromotedValue(n)
	eq(t, "PromotedValue sum", s, 3)
	eq(t, "PromotedValue string", str, "(1,2)")
	eq(t, "PromotedValue title", title, "n(1,2)")
	eq(t, "PromotedPtr", methods.PromotedPtr(n, 1, 2), methods.Named{Point: P{3, 6}, Name: "n"})

	d := methods.Deep{Named: n, Z: 10}
	ds, ps, dt, d2 := methods.Depth2(d, 5)
	eq(t, "Depth2 sum", ds, 18)
	eq(t, "Depth2 inner sum", ps, 8)
	eq(t, "Depth2 title", dt, "n(6,2)")
	eq(t, "Depth2 value", d2, methods.Deep{Named: methods.Named{Point: P{6, 2}, Name: "n"}, Z: 10})
	eq(t, "Depth2 caller's copy", d.X, 1)

	p := P{1, 2}
	es, estr, ex := methods.EmbeddedPtr(&p, "e", 3)
	eq(t, "EmbeddedPtr sum", es, 15)
	eq(t, "EmbeddedPtr string", estr, "(7,8)")
	eq(t, "EmbeddedPtr scaled", ex, 14)
	eq(t, "EmbeddedPtr effect", p, P{7, 8})

	p = P{1, 1}
	g, cn, ms := methods.MixedEmbed(&p, 5)
	deq(t, "MixedEmbed", []int{g, cn, ms}, []int{7, 7, 4})

	// promoted methods called from the stock side
	eq(t, "stock Deep.Sum", d.Sum(), 13)
	eq(t, "stock Deep.Named.Sum", d.Named.Sum(), 3)
	eq(t, "stock Deep.Title", d.Title(), "n(1,2)")
	d.Move(1, 1)
	eq(t, "stock Deep.Move", d.Point, P{2, 3})
	pd := methods.PDeep{PNamed: &methods.PNamed{Point: &p, Name: "x"}}
	pd.Move(1, 1)
	eq(t, "stock PDeep.Move", p, P{3, 3})
	eq(t, "stock PDeep.Sum", pd.Sum(), 6)
}

func TestInterfaces(t *testing.T) {
	eq(t, "SumOf Point", methods.SumOf(P{1, 2}), 3)
	eq(t, "SumOf *Point", methods.SumOf(&P{1, 2}), 3)
	eq(t, "SumOf Deep", methods.SumOf(methods.Deep{Z: 4}), 4)
	eq(t, "SumOf MyInt", methods.SumOf(methods.MyInt(-8)), -8)
	eq(t, "SumOf IntSlice", methods.SumOf(methods.IntSlice{1, 2, 3}), 6)
	eq(t, "SumOf Arr", methods.SumOf(methods.Arr{1, 2, 3, 4}), 10)
	eq(t, "SumOf *Arr", methods.SumOf(&methods.Arr{1, 2, 3, 4}), 10)
	p := P{1, 2}
	eq(t, "MoveAndSum", methods.MoveAndSum(&p, 5), 13)
	n := methods.Named{Point: P{0, 1}}
	eq(t, "MoveAndSum *Named", methods.MoveAndSum(&n, 5), 11)
	eq(t, "MoveAndSum PNamed", methods.MoveAndSum(methods.PNamed{Point: &p}, 1), 15)

	p = P{3, 4}
	want := []int{7, 7, 7, 7, 107, 107, 7, 7, 7, 7, 3, 7}
	ss := methods.Summers(p)
	eq(t, "Summers len", len(ss), len(want))
	for i, s := range ss {
		eq(t, fmt.Sprintf("Summers[%d] %T", i, s), s.Sum(), want[i])
	}
	wantTypes := "methods.Point *methods.Point methods.Named *methods.Named methods.Deep *methods.Deep methods.PNamed *methods.PNamed methods.PDeep methods.Mixed methods.MyInt methods.IntSlice"
	var types []string
	for _, s := range ss {
		types = append(types, fmt.Sprintf("%T", s))
	}
	eq(t, "Summers types", strings.Join(types, " "), wantTypes)

	p = P{0, 0}
	n = methods.Named{}
	d := methods.Deep{}
	methods.MoveAll(methods.Movers(&p, &n, &d), 1)
	eq(t, "MoveAll p", p, P{4, -4})
	eq(t, "MoveAll n", n.Point, P{1, -1})
	eq(t, "MoveAll d", d.Point, P{1, -1})

	for _, c := range []struct {
		v    any
		want bool
	}{
		{P{}, false}, {&P{}, true}, {methods.Named{}, false}, {&methods.Named{}, true},
		{methods.Deep{}, false}, {&methods.Deep{}, true}, {methods.PNamed{}, true}, {&methods.PNamed{}, true},
		{methods.PDeep{}, true}, {methods.Mixed{}, true}, {methods.MyInt(1), false}, {new(methods.MyInt), false},
		{1, false}, {nil, false}, {"s", false},
	} {
		eq(t, fmt.Sprintf("IsSumMover %T", c.v), methods.IsSumMover(c.v), c.want)
		// the run-time method tables fc wrote, as the stock compiler's code sees them
		_, ok := c.v.(methods.SumMover)
		eq(t, fmt.Sprintf("stock assert %T", c.v), ok, c.want)
	}

	for _, c := range []struct {
		v    any
		want string
	}{
		{P{1, 2}, "(1,2)"}, {&P{1, 2}, "(1,2)"}, {methods.Named{Point: P{3, 4}}, "(3,4)"},
		{methods.MyInt(5), "MyInt(5)"}, {methods.MyInt(-5), "MyInt(-5)"}, {new(methods.MyInt), "MyInt(0)"},
		{methods.PNamed{Point: &P{5, 6}}, "(5,6)"}, {7, "?"}, {methods.IntSlice{1}, "?"},
	} {
		eq(t, fmt.Sprintf("Str %T", c.v), methods.Str(c.v), c.want)
		if c.want != "?" {
			eq(t, fmt.Sprintf("fmt.Sprint %T", c.v), fmt.Sprint(c.v), c.want)
		}
	}
	a, b := methods.EmbeddedIface(P{1, 2})
	eq(t, "EmbeddedIface", a+b, 6)
	a, b = methods.EmbeddedIface(methods.MyInt(4))
	eq(t, "EmbeddedIface MyInt", a+b, 8)

	// method counts and names as reflection reads them from fc's type descriptors
	for _, c := range []struct {
		v    any
		want string
	}{
		{P{}, "AddAll Both Dot Scaled SetXCopy String Sum"},
		{&P{}, "AddAll Both Dot IsNil Move Scale Scaled SetXCopy String Sum Swap"},
		{methods.Deep{}, "AddAll Both Dot Scaled SetXCopy String Sum Title"},
		{methods.PDeep{}, "AddAll Both Dot IsNil Move Scale Scaled SetXCopy String Sum Swap"},
		{methods.MyInt(0), "Double IsNeg String Sum"},
		{new(methods.MyInt), "Double Inc IsNeg String Sum"},
		{methods.BinOp(nil), "Apply Flip"},
	} {
		rt := reflect.TypeOf(c.v)
		var names []string
		for i := 0; i < rt.NumMethod(); i++ {
			names = append(names, rt.Method(i).Name)
		}
		eq(t, "reflect methods of "+rt.String(), strings.Join(names, " "), c.want)
	}
	// calling through reflection uses the method tables' function pointers
	out := reflect.ValueOf(P{3, 4}).MethodByName("Dot").Call([]reflect.Value{reflect.ValueOf(P{10, 100})})
	eq(t, "reflect call", out[0].Int(), 430)
}

func TestNamedTypes(t *testing.T) {
	for _, x := range []int{5, -5, 0, -2} {
		d, neg, m, s := methods.IntMethods(x)
		eq(t, "IntMethods double", d, methods.MyInt(2*x))
		eq(t, "IntMethods neg", neg, x+2 < 0)
		eq(t, "IntMethods inc", m, methods.MyInt(x+2))
		eq(t, "IntMethods string", s, fmt.Sprintf("MyInt(%d)", x+2))
	}
	n0, n1, s, sum := methods.SliceMethods([]int{1, 2, 3}, 10)
	eq(t, "SliceMethods n0", n0, 3)
	eq(t, "SliceMethods n1", n1, 5)
	deq(t, "SliceMethods s", s, []int{-1, 2, 3, 10, 11})
	eq(t, "SliceMethods sum", sum, 25)
	_, n1, s, sum = methods.SliceMethods(nil, 10)
	eq(t, "SliceMethods nil n1", n1, 2)
	deq(t, "SliceMethods nil s", s, []int{-1, 11})
	eq(t, "SliceMethods nil sum", sum, 10)

	m := map[string]int{"a": 1, "b": 2}
	old, now, total := methods.MapMethods(m, "a", 10)
	deq(t, "MapMethods", []int{old, now, total}, []int{1, 10, 12})
	old, now, total = methods.MapMethods(m, "c", 1)
	deq(t, "MapMethods new", []int{old, now, total}, []int{0, 1, 13})
	eq(t, "MapMethods effect", len(m), 3)

	a, b := methods.FuncMethods(10, 3)
	eq(t, "FuncMethods", a*100+b, 693)

	f, ln, tw, fl, cv, wv := methods.OtherMethods(100, "ab", true, 9)
	eq(t, "Celsius", f, 212.0)
	eq(t, "Name.Len", ln, 3)
	eq(t, "Name.Twice", tw, "ab!ab!")
	eq(t, "Flag.Not", fl, false)
	eq(t, "Ch.SendRecv", cv, 9)
	eq(t, "Wrapper.Load", wv, 9)

	arr := methods.Arr{1, 2, 3, 4}
	a0, a1, cs := methods.ArrMethods(arr, 2, 30)
	eq(t, "ArrMethods a", a0, arr)
	eq(t, "ArrMethods b", a1, methods.Arr{1, 2, 30, 4})
	eq(t, "ArrMethods c", cs, 38)
	eq(t, "ArrMethods bounds", catch(func() { methods.ArrMethods(arr, 4, 0) }), catch(func() { arr.SetP(len(arr)+cs-38, 0) }))
}

type tp struct{ a, b int }

func (p tp) Sum() int { return p.a + p.b }

// in a package-level slice so that the stock compiler does not devirtualize the call
var localSummers = []interface{ Sum() int }{(*tp)(nil)}

func callValue(v any) int { return v.(interface{ Value() int }).Value() }

func TestNil(t *testing.T) {
	eq(t, "BuildList nil Len", methods.BuildList(nil).Len(), 0)
	eq(t, "BuildList nil Total", methods.BuildList(nil).Total(), 0)
	l := methods.BuildList([]int{1, 2, 3})
	eq(t, "BuildList Len", l.Len(), 3)
	eq(t, "BuildList Total", l.Total(), 6)
	eq(t, "BuildList head", l.Val, 3)
	big := make([]int, 100000)
	for i := range big {
		big[i] = i
	}
	l = methods.BuildList(big)
	eq(t, "big Len (deep recursion)", l.Len(), 100000)
	eq(t, "big Total", l.Total(), 99999*100000/2)

	eq(t, "NilPtrMethod nil", methods.NilPtrMethod(nil), true)
	eq(t, "NilPtrMethod", methods.NilPtrMethod(&P{}), false)

	var np *P
	var nn *methods.Node
	var ns methods.Summer
	nilDeref := catch(func() { _ = np.Sum() })
	if !strings.HasPrefix(nilDeref, "runtime.errorString: runtime error: invalid memory address") {
		t.Fatalf("stock nil dereference panic is %q", nilDeref)
	}
	eq(t, "NilValueMethod", catch(func() { methods.NilValueMethod(nil) }), nilDeref)
	eq(t, "NilValueMethod ok", methods.NilValueMethod(&P{1, 2}), 3)
	eq(t, "NilNodeValue", catch(func() { methods.NilNodeValue(nil) }), catch(func() { _ = nn.Value() }))
	eq(t, "NilIface", catch(func() { methods.NilIface(nil) }), catch(func() { ns.Sum() }))
	eq(t, "NilEmbedded", catch(func() { methods.NilEmbedded("x") }), catch(func() { _ = methods.PNamed{}.Sum() }))
	eq(t, "NilEmbeddedIface", catch(func() { methods.NilEmbeddedIface() }), catch(func() { _ = methods.Logger{}.Sum() }))

	// A value method called through an interface holding a nil pointer goes through the generated
	// (*T).M wrapper, which panics with a message naming the method (runtime.panicwrap).
	stock := catch(func() { localSummers[0].Sum() })
	if !strings.Contains(stock, "fclang/methods_test.tp.Sum") {
		t.Fatalf("stock panicwrap message is %q", stock)
	}
	want := strings.NewReplacer("fclang/methods_test.tp", "fclang/methods.Point", "*tp", "*Point").Replace(stock)
	eq(t, "wrapper of nil pointer", catch(func() { methods.SumOf(np) }), want)
	want = strings.NewReplacer("fclang/methods_test.tp.Sum", "fclang/methods.Node.Value", "*tp", "*Node").Replace(stock)
	eq(t, "wrapper of nil pointer 2", catch(func() { callValue(nn) }), want)

	isNil, called := methods.IfaceNilPtr()
	eq(t, "IfaceNilPtr == nil", isNil, false)
	eq(t, "IfaceNilPtr call", called, true)
}

func TestLarge(t *testing.T) {
	a, b, c, d := methods.LargeMethods(1, "abc")
	deq(t, "LargeMethods", []int{a, b, c, d}, []int{468, 1467, 1467, 469})
	var l methods.Large
	l.Fill(10).Name = "four"
	eq(t, "stock Large.Total", l.Total(), 30*10+435+4)
	eq(t, "stock Large.With", l.With(29, 0).Total(), 30*10+435+4-39)
	eq(t, "stock Large unchanged", l.A[0], 10)
	var tt methods.Totaler = l
	eq(t, "stock Totaler", tt.Total(), 30*10+435+4)
}
