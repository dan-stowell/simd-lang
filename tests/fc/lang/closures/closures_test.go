package closures_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"fclang/closures"
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

func TestCapture(t *testing.T) {
	for _, start := range []int{0, 5, -3, 1 << 40} {
		inc, get := closures.Counter(start)
		eq(t, "Counter get0", get(), start)
		eq(t, "Counter inc1", inc(), start+1)
		eq(t, "Counter inc2", inc(), start+2)
		eq(t, "Counter get2", get(), start+2)
		inc2, get2 := closures.Counter(start)
		inc2()
		eq(t, "Counter independent", get2(), start+1)
		eq(t, "Counter first unchanged", get(), start+2)
	}
	for _, x := range []int{0, 1, -7, 12345} {
		b, a := closures.CaptureParam(x)
		eq(t, "CaptureParam before", b, x)
		eq(t, "CaptureParam after", a, x*4)
		eq(t, "CaptureAfter", closures.CaptureAfter(x), x+100)
	}
	xs := []int{9}
	i, s, f, ys := closures.CaptureMany(10, "s", 1.5, xs)
	// i = 10+1+2 = 13; p swapped twice -> a = 10
	eq(t, "CaptureMany i", i, 13+10*1000)
	eq(t, "CaptureMany s", s, "s!!")
	eq(t, "CaptureMany f", f, 6.0)
	deq(t, "CaptureMany xs", ys, []int{9, 1, 2})

	eq(t, "Adder", closures.Adder(3)(4), 7)
	a5 := closures.Adder(5)
	eq(t, "Adder reuse", a5(1)+a5(2), 13)
	h := closures.Compose(closures.Adder(1), func(x int) int { return x * 10 })
	eq(t, "Compose", h(7), 71)

	c := closures.Curry3(2)
	c3 := c(3)
	eq(t, "Curry3 a", c3(4), 6+4+3)
	eq(t, "Curry3 b", c3(4), 6+4+4)
	// a was incremented twice: a new middle closure sees a == 4
	eq(t, "Curry3 c", c(3)(0), 12+0+5)

	fs := closures.MakeCounters(4)
	for i, f := range fs {
		eq(t, "MakeCounters 1", f(), i*10+1)
	}
	for i, f := range fs {
		eq(t, "MakeCounters 2", f(), i*10+2)
	}

	g := closures.FibGen()
	a, b := uint64(0), uint64(1)
	for i := 0; i < 93; i++ {
		eq(t, "FibGen", g(), a)
		a, b = b, a+b
	}

	set, sum, name := closures.CaptureBig(100)
	want := 0
	for i := 0; i < 40; i++ {
		want += 100 + i
	}
	eq(t, "CaptureBig sum", sum(), want)
	set(0, 0)
	set(39, 1)
	eq(t, "CaptureBig sum2", sum(), want-100-139+1)
	eq(t, "CaptureBig name", name(), "big++")

	r, rs := closures.NamedResult(7)
	eq(t, "NamedResult r", r, 1007)
	eq(t, "NamedResult s", rs, "ret-deferred")
}

func TestLoopVars(t *testing.T) {
	for _, n := range []int{0, 1, 5} {
		fs := closures.LoopVar3(n)
		eq(t, "LoopVar3 len", len(fs), n)
		for i, f := range fs {
			eq(t, "LoopVar3", f(), i)
		}
	}
	deq(t, "LoopVar3Mod", closures.LoopVar3Mod(7), []int{1, 3, 5, 7})
	first, second := closures.LoopVar3Late(3)
	deq(t, "LoopVar3Late first", first, []int{10, 11, 12})
	deq(t, "LoopVar3Late second", second, []int{20, 21, 22})
	deq(t, "LoopVar3Post", closures.LoopVar3Post(3), []int{100, 1099, 2098})

	ps := closures.LoopAddr(4)
	seen := map[*int]bool{}
	for i, p := range ps {
		// each pointer is to the iteration's own variable, whose final value is i
		eq(t, "LoopAddr value", *p, i)
		seen[p] = true
	}
	eq(t, "LoopAddr distinct", len(seen), 4)

	sf := closures.LoopRange([]string{"a", "bc", "", "d"})
	deq(t, "LoopRange", []string{sf[0](), sf[1](), sf[2](), sf[3]()}, []string{"a", "bcbc", "", "dddd"})

	rf := closures.LoopRangeInt(4)
	for i, f := range rf {
		eq(t, "LoopRangeInt 1", f(), i*2)
		eq(t, "LoopRangeInt 2", f(), i*4)
	}

	m := map[string]int{"one": 1, "two": 2, "three": 3, "four": 4}
	var got []string
	for _, f := range closures.LoopRangeMap(m) {
		k, v := f()
		got = append(got, fmt.Sprint(k, "=", v))
	}
	sort.Strings(got)
	deq(t, "LoopRangeMap", got, []string{"four=4", "one=1", "three=3", "two=2"})

	var rs []rune
	for _, f := range closures.LoopRangeString("aé\xff世") {
		rs = append(rs, f())
	}
	deq(t, "LoopRangeString", rs, []rune{'a', 'é', 0xfffd, '世'})
}

func fib(n int) int {
	a, b := 0, 1
	for i := 0; i < n; i++ {
		a, b = b, a+b
	}
	return a
}

func TestRecursive(t *testing.T) {
	for _, n := range []int{0, 1, 2, 10, 20} {
		eq(t, "Fib", closures.Fib(n), fib(n))
	}
	for _, n := range []int{0, 1, 2, 7, 1000, 100001} {
		even, calls := closures.EvenOdd(n)
		eq(t, "EvenOdd", even, n%2 == 0)
		eq(t, "EvenOdd calls", calls, n+1)
	}
	tree := &closures.Tree{
		L: &closures.Tree{L: &closures.Tree{V: 1}, V: 2, R: &closures.Tree{V: 3}},
		V: 4,
		R: &closures.Tree{V: 5, R: &closures.Tree{V: 6}},
	}
	var vs []int
	n := closures.Walk(tree, func(v int) { vs = append(vs, v) })
	eq(t, "Walk count", n, 6)
	deq(t, "Walk order", vs, []int{1, 2, 3, 4, 5, 6})
	eq(t, "Walk nil", closures.Walk(nil, nil), 0)
}

type testGetter int

func (g testGetter) Get() int { return int(g) * 2 }

func TestMethodValues(t *testing.T) {
	a := closures.Acc{N: 5, Name: "acc"}
	bound, now := closures.MethodValueVal(a)
	eq(t, "MethodValueVal bound", bound, 5)
	eq(t, "MethodValueVal now", now, 105)

	r1, r2, n := closures.MethodValuePtr(a, 3, 4)
	eq(t, "MethodValuePtr r1", r1, 8)
	eq(t, "MethodValuePtr r2", r2, 12)
	eq(t, "MethodValuePtr n", n, 12)
	eq(t, "caller's copy", a.N, 5)

	bound, now = closures.MethodValueThroughPtr(&a)
	eq(t, "MethodValueThroughPtr bound", bound, 5)
	eq(t, "MethodValueThroughPtr now", now, 12)
	eq(t, "pointee changed", a.N, 12)

	eq(t, "MethodValueIface Acc", closures.MethodValueIface(a)(), 12)
	eq(t, "MethodValueIface *Acc", closures.MethodValueIface(&a)(), 12)
	eq(t, "MethodValueIface test type", closures.MethodValueIface(testGetter(21))(), 42)
	f := closures.MethodValueIface(&a)
	a.N = 1 // *Acc in the interface: Get reads the current value
	eq(t, "MethodValueIface ptr sees change", f(), 1)
	f = closures.MethodValueIface(a)
	a.N = 2 // Acc in the interface: a copy
	eq(t, "MethodValueIface value is a copy", f(), 1)

	eq(t, "MethodValueLabel", closures.MethodValueLabel(a)("the "), "the acc")
	add := closures.BoundAdd(&a)
	add(10)
	eq(t, "BoundAdd", add(10), 22)
	eq(t, "BoundAdd target", a.N, 22)

	// making a method value panics at once when the receiver cannot be evaluated
	var np *closures.Acc
	var ng closures.Getter
	want := catch(func() { _ = np.Get })
	if want == "" {
		t.Fatal("stock: no panic for nil.Get method value")
	}
	eq(t, "MethodValueThroughPtr(nil)", catch(func() { closures.MethodValueThroughPtr(nil) }), want)
	want = catch(func() { _ = ng.Get })
	eq(t, "MethodValueIface(nil)", catch(func() { closures.MethodValueIface(nil) }), want)
	// a pointer-receiver method value of a nil pointer is made without a panic; calling it panics inside Add
	nf := closures.BoundAdd(nil)
	eq(t, "BoundAdd(nil) call", catch(func() { nf(1) }), catch(func() { np.Add(1) }))
}

func TestMethodExprs(t *testing.T) {
	a := closures.Acc{N: 9, Name: "x"}
	eq(t, "MethodExprVal", closures.MethodExprVal(a), 9)
	eq(t, "MethodExprPtr", closures.MethodExprPtr(&a, 6), 15)
	eq(t, "MethodExprPtr effect", a.N, 15)
	eq(t, "MethodExprPtrVal", closures.MethodExprPtrVal(&a), 15)
	eq(t, "MethodExprIface", closures.MethodExprIface(a), 15)
	eq(t, "MethodExprIface test type", closures.MethodExprIface(testGetter(4)), 8)
	get, add, label := closures.MethodExprs()
	eq(t, "MethodExprs get", get(a), 15)
	eq(t, "MethodExprs add", add(&a, -5), 10)
	eq(t, "MethodExprs label", label(a, "<")+">", "<x>")
	eq(t, "MethodExprPtrVal(nil)", catch(func() { closures.MethodExprPtrVal(nil) }), catch(func() {
		var p *closures.Acc
		_ = (*closures.Acc).Get(p)
	}))
}

func TestFuncValues(t *testing.T) {
	ops := closures.Ops()
	want := map[string]int{"add": 17, "sub": 7, "mul": 60}
	for _, op := range ops {
		if op.Name == "nil" {
			eq(t, "Ops nil", op.F == nil, true)
			continue
		}
		eq(t, "Ops "+op.Name, op.F(12, 5), want[op.Name])
	}
	for _, c := range []struct {
		name string
		a, b int
		want int
		ok   bool
	}{
		{"add", 2, 3, 5, true}, {"sub", 2, 3, -1, true}, {"max", 2, 3, 3, true}, {"max", 9, 3, 9, true},
		{"div", 1, 1, 0, false}, {"", 1, 1, 0, false},
	} {
		got, ok := closures.Apply(c.name, c.a, c.b)
		eq(t, "Apply "+c.name, got, c.want)
		eq(t, "Apply ok "+c.name, ok, c.ok)
	}
	eq(t, "Pipeline none", closures.Pipeline(5), 5)
	eq(t, "Pipeline", closures.Pipeline(5, closures.Adder(1), func(x int) int { return x * x }, closures.Adder(-6)), 30)
	eq(t, "Pipeline spread nil", closures.Pipeline(5, nil...), 5)

	sum, w := closures.SumVia([]int{3, 4, 5})
	eq(t, "SumVia sum", sum, 12)
	eq(t, "SumVia weighted", w, 14)
	sum, w = closures.SumVia(nil)
	eq(t, "SumVia nil", sum+w, 0)

	deq(t, "Filter", closures.Filter([]int{1, 5, 10, 15, 20, -3}, closures.Between(5, 15)), []int{5, 10, 15})
	deq(t, "Filter none", closures.Filter([]int{1, 2}, closures.Between(5, 4)), []int(nil))

	closures.Save(40)
	eq(t, "CallSaved 1", closures.CallSaved(), 41)
	eq(t, "CallSaved 2", closures.CallSaved(), 42)
	closures.Save(0)
	eq(t, "CallSaved 3", closures.CallSaved(), 1)
}

func TestNilAndImmediate(t *testing.T) {
	eq(t, "IsNil nil", closures.IsNil(nil), true)
	eq(t, "IsNil f", closures.IsNil(func() {}), false)
	eq(t, "NotNil nil", closures.NotNil(nil), false)
	eq(t, "NotNil f", closures.NotNil(closures.Adder(1)), true)
	eq(t, "CallOrDefault nil", closures.CallOrDefault(nil, 7), 7)
	eq(t, "CallOrDefault f", closures.CallOrDefault(func() int { return 3 }, 7), 3)
	eq(t, "CallNil ok", closures.CallNil(func() int { return 3 }), 3)
	var nf func() int
	want := catch(func() { nf() })
	if want == "" {
		t.Fatal("stock: calling a nil func did not panic")
	}
	eq(t, "CallNil nil", catch(func() { closures.CallNil(nil) }), want)

	for _, x := range []int{0, 3, -4} {
		eq(t, "Immediate", closures.Immediate(x), x*x+x+1)
	}
	a, b := closures.ImmediateMulti(1, 2)
	eq(t, "ImmediateMulti", a*10+b, 21)

	v := closures.VariadicClosure(100)
	eq(t, "Variadic none", v(), 100)
	eq(t, "Variadic some", v(1, 2, 3), 106)
	eq(t, "Variadic spread", v([]int{4}...), 110)

	x, y := 1, 2
	sw := closures.Swapper(&x, &y)
	sw()
	eq(t, "Swapper", x*10+y, 21)
	sw()
	eq(t, "Swapper twice", x*10+y, 12)
	z := 5
	closures.Swapper(&z, &z)()
	eq(t, "Swapper self", z, 5)

	calls := 0
	m, n := closures.Memo(func(x int) int { calls++; return x * x })
	eq(t, "Memo", m(4)+m(4)+m(5)+m(4), 16*3+25)
	eq(t, "Memo calls", n(), 2)
	eq(t, "Memo inner calls", calls, 2)
}
