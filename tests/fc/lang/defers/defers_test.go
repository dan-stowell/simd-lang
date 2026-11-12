package defers_test

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"testing"

	"fclang/defers"
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

func TestOrder(t *testing.T) {
	deq(t, "Order(0)", defers.Order(0), []int(nil))
	deq(t, "Order(4)", defers.Order(4), []int{3, 2, 1, 0})
	eq(t, "Three", defers.Three(), "r321")
	a, b := defers.ArgsAtDeferTime(5)
	eq(t, "ArgsAtDeferTime", a*100+b, 515)
	deq(t, "EvalOrder", defers.EvalOrder(), []string{"eval:a", "eval:b", "body", "b", "a"})
	eq(t, "FuncValueAtDeferTime", defers.FuncValueAtDeferTime(), "xfirst")
	for _, v := range []int{0, 7, -3} {
		want := 3000
		for i := 0; i < 20; i++ {
			want += v + i
		}
		eq(t, "BigArg", defers.BigArg(v), want-1)
	}
}

// the stock compiler's versions
func nilFuncDefer(f func()) (log []string) {
	defer func() {
		if r := recover(); r != nil {
			log = append(log, "recovered:"+defers.Msg(r))
		}
	}()
	log = append(log, "before")
	defer f()
	log = append(log, "after")
	return log
}

func nilIfaceDefer(c defers.Closer) (log []string) {
	defer func() {
		if r := recover(); r != nil {
			log = append(log, "recovered:"+defers.Msg(r))
		}
	}()
	log = append(log, "before")
	defer c.Close()
	log = append(log, "after")
	return log
}

func TestNilDefers(t *testing.T) {
	ran := false
	deq(t, "NilFuncDefer f", defers.NilFuncDefer(func() { ran = true }), []string{"before", "after"})
	eq(t, "NilFuncDefer ran", ran, true)
	nilDeref := "recovered:runtime error: invalid memory address or nil pointer dereference"
	// the panic comes when the function returns
	deq(t, "stock nilFuncDefer", nilFuncDefer(nil), []string{"before", "after", nilDeref})
	deq(t, "NilFuncDefer nil", defers.NilFuncDefer(nil), nilFuncDefer(nil))
	// the panic comes at the defer statement
	deq(t, "stock nilIfaceDefer", nilIfaceDefer(nil), []string{"before", nilDeref})
	deq(t, "NilIfaceDefer nil", defers.NilIfaceDefer(nil), nilIfaceDefer(nil))
	r := &defers.Rec{}
	deq(t, "NilIfaceDefer", defers.NilIfaceDefer(r), []string{"before", "after"})
	deq(t, "NilIfaceDefer effect", r.Log, []string{"close"})
}

func TestNamedResults(t *testing.T) {
	for _, x := range []int{0, 1, -5} {
		eq(t, "Double", defers.Double(x), 2*(x+1))
	}
	r, s := defers.ResultAfterPanic(4)
	eq(t, "ResultAfterPanic ok", fmt.Sprintln(r, s), "9 ok\n")
	r, s = defers.ResultAfterPanic(-4)
	eq(t, "ResultAfterPanic panic", fmt.Sprintln(r, s), "-4 negative\n")
	eq(t, "UnnamedAfterPanic no panic", defers.UnnamedAfterPanic(0), 7)
	eq(t, "UnnamedAfterPanic panic", defers.UnnamedAfterPanic(3), 0)
	eq(t, "WithCleanup ok", defers.WithCleanup(false), nil)
	err := defers.WithCleanup(true)
	eq(t, "WithCleanup fail", err, error(defers.E("failed")))
	eq(t, "WithCleanup text", err.Error(), "E:failed")
}

func TestLoops(t *testing.T) {
	for _, n := range []int{0, 1, 5, 1000} {
		sum, order := defers.LoopDefers(n)
		eq(t, "LoopDefers sum", sum, 1000+n*(n-1)/2)
		eq(t, "LoopDefers len", len(order), n)
		for i, k := range order {
			if k != n-1-i {
				t.Fatalf("LoopDefers(%d) order[%d] = %d", n, i, k)
			}
		}
	}
	for _, x := range []int{0, 1, -2} {
		want := x
		for i := 10; i >= 1; i-- {
			want = want*3 + i
		}
		eq(t, "ManyDefers", defers.ManyDefers(x), want)
	}
	for _, c := range []struct {
		a, b, c bool
		want    string
	}{
		{false, false, false, "-!"}, {true, false, false, "-!a"}, {false, true, false, "-!b"}, {true, true, false, "-!ba"},
		{false, false, true, "c!"}, {true, true, true, "c!ba"},
	} {
		eq(t, fmt.Sprint("Conditional ", c.a, c.b, c.c), defers.Conditional(c.a, c.b, c.c), c.want)
	}
	deq(t, "SafeDivAll", defers.SafeDivAll([]int{10, 5, -7, 0, 9}, []int{2, 0, 2, 0, -3}), []int{5, -1, -3, -1, -3})
}

type myErr struct{ code int }

func (e *myErr) Error() string { return fmt.Sprint("myErr ", e.code) }

func TestRecover(t *testing.T) {
	me := &myErr{3}
	for _, v := range []any{1, "s", 2.5, me, error(me), struct{ a, b int }{1, 2}, [2]string{"a", "b"}, defers.E("e"), errors.New("x")} {
		if got := defers.RecoverValue(v); got != v {
			t.Errorf("RecoverValue(%T %v) = %T %v", v, v, got, got)
		}
		if got := catch(func() { defers.Panic(v) }); got != fmt.Sprintf("%T: %v", v, v) {
			t.Errorf("Panic(%T %v): recovered %s", v, v, got)
		}
	}
	if got := defers.RecoverValue([]int{1, 2}); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("RecoverValue(slice) = %v", got)
	}
	// panic(nil) is recovered as a *runtime.PanicNilError
	stockNil := func() (r any) {
		defer func() { r = recover() }()
		var v any
		panic(v)
	}()
	gotNil := defers.RecoverValue(nil)
	eq(t, "panic(nil) type", fmt.Sprintf("%T", gotNil), "*runtime.PanicNilError")
	eq(t, "panic(nil) text", fmt.Sprint(gotNil), fmt.Sprint(stockNil))

	r, ran := defers.NoPanicRecover()
	eq(t, "NoPanicRecover", fmt.Sprint(r, ran), "<nil> true")
	eq(t, "RecoverOutsideDefer", defers.RecoverOutsideDefer(), nil)
	inner, outer := defers.IndirectRecover()
	eq(t, "IndirectRecover", fmt.Sprintln(inner, outer), "<nil> p\n")
	eq(t, "DeferRecoverDirect", defers.DeferRecoverDirect(), any("direct"))
	inner, outer = defers.NestedFuncRecover()
	eq(t, "NestedFuncRecover", fmt.Sprintln(inner, outer), "<nil> nested\n")
	eq(t, "Repanic", defers.Repanic("v"), any("re:v"))
	rv, order := defers.NestedPanic()
	eq(t, "NestedPanic value", rv, any("second"))
	deq(t, "NestedPanic order", order, []string{"first-defer", "second", "outer"})
	r1, r2 := defers.PanicAfterRecover()
	eq(t, "PanicAfterRecover", fmt.Sprintln(r1, r2), "early later\n")
	a, b := defers.RecoverTwice()
	eq(t, "RecoverTwice", fmt.Sprint(a, b), "1 <nil>")

	// panics that start in the stock compiler's code and are recovered by fc's
	var m map[string]int
	var xs []int
	eq(t, "CallAndRecover none", defers.CallAndRecover(func() {}), nil)
	eq(t, "CallAndRecover string", defers.CallAndRecover(func() { panic("stock") }), any("stock"))
	eq(t, "CallAndRecover nil map", fmt.Sprint(defers.CallAndRecover(func() { m["a"] = 1 })), "assignment to entry in nil map")
	eq(t, "CallAndRecover index", fmt.Sprint(defers.CallAndRecover(func() { _ = xs[3] })), "runtime error: index out of range [3] with length 0")
	// and nested: stock -> fc -> stock -> fc panic
	eq(t, "nested recover", catch(func() {
		got := defers.CallAndRecover(func() { defers.Panic("inner") })
		panic(fmt.Sprint("outer after ", got))
	}), "string: outer after inner")

	log, rv := defers.Through(3)
	deq(t, "Through log", log, []string{"unwind0", "unwind1", "unwind2", "unwind3"})
	eq(t, "Through value", rv, any("bottom"))
	count, rv := defers.Deep(10000)
	eq(t, "Deep count", count, 10001)
	eq(t, "Deep value", rv, any("deep"))
	eq(t, "ProtectEach", defers.ProtectEach(100), 5050)
}

func stockTry(f func()) (msg string, isRuntime bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if re, ok := r.(runtime.Error); ok {
			msg, isRuntime = re.Error(), true
			return
		}
		msg = defers.Msg(r)
	}()
	f()
	return "no panic", false
}

var sink int

func TestRuntimePanics(t *testing.T) {
	var nm map[string]int
	var np *int
	var npair *defers.Pair
	var nf func()
	var nc chan int
	closed := make(chan int)
	close(closed)
	xs := []int{1, 2, 3}
	n := 0
	check := func(name, wantText string, got func() (string, bool), stock func()) {
		t.Helper()
		gm, gr := got()
		wm, wr := stockTry(stock)
		if gm != wm || gr != wr {
			t.Errorf("%s: got %q (runtime.Error: %v), stock %q (%v)", name, gm, gr, wm, wr)
		}
		if wantText != "" && wm != wantText {
			t.Errorf("%s: stock says %q, the test expects %q", name, wm, wantText)
		}
		n++
	}
	check("NilMapWrite", "assignment to entry in nil map", func() (string, bool) { return defers.NilMapWrite(nil, "k") }, func() { nm["k"] = 1 })
	check("NilMapWrite ok", "no panic", func() (string, bool) { return defers.NilMapWrite(map[string]int{}, "k") }, func() {})
	check("NilDeref", "runtime error: invalid memory address or nil pointer dereference", func() (string, bool) { return defers.NilDeref(nil) }, func() { *np = 1 })
	check("NilDeref ok", "no panic", func() (string, bool) { return defers.NilDeref(new(int)) }, func() {})
	check("NilField", "", func() (string, bool) { return defers.NilField(nil) }, func() { sink = len(npair.B) })
	for _, i := range []int{-1, 0, 2, 3, 4, 1 << 40, -1 << 63} {
		check(fmt.Sprint("Index ", i), "", func() (string, bool) { return defers.Index(xs, i) }, func() { sink = xs[i] })
		check(fmt.Sprint("IndexStr ", i), "", func() (string, bool) { return defers.IndexStr("abc", i) }, func() { sink = int("abc"[i]) })
		check(fmt.Sprint("IndexArr ", i), "", func() (string, bool) { return defers.IndexArr(i) }, func() { var a [4]int; sink = a[i] })
		for _, j := range []int{-1, 0, 2, 3, 4} {
			check(fmt.Sprint("SliceBounds ", i, j), "", func() (string, bool) { return defers.SliceBounds(xs, i, j) }, func() { sink = len(xs[i:j]) })
		}
		check(fmt.Sprint("MakeLen ", i), "", func() (string, bool) { return defers.MakeLen(i % (1 << 20)) }, func() { sink = len(make([]int, i%(1<<20))) })
		check(fmt.Sprint("Shift ", i), "", func() (string, bool) { return defers.Shift(1, i) }, func() { sink = 1 << i })
	}
	check("Index text", "runtime error: index out of range [5] with length 3", func() (string, bool) { return defers.Index(xs, 5) }, func() { sink = xs[n-n+5] })
	check("Index text neg", "runtime error: index out of range [-1]", func() (string, bool) { return defers.Index(xs, -1) }, func() { sink = xs[n-n-1] })
	check("SliceBounds text", "runtime error: slice bounds out of range [:4] with capacity 3", func() (string, bool) { return defers.SliceBounds(xs, 1, 4) }, func() { sink = len(xs[1 : n-n+4]) })
	check("SliceBounds text 2", "runtime error: slice bounds out of range [3:2]", func() (string, bool) { return defers.SliceBounds(xs, 3, 2) }, func() { sink = len(xs[n-n+3 : 2]) })
	check("Shift text", "runtime error: negative shift amount", func() (string, bool) { return defers.Shift(1, -1) }, func() { sink = 1 << (n - n - 1) })
	for _, d := range []int{0, 1, -1} {
		check(fmt.Sprint("Divide ", d), "", func() (string, bool) { return defers.Divide(7, d) }, func() { sink = 7 / d })
		check(fmt.Sprint("Modulo ", d), "", func() (string, bool) { return defers.Modulo(7, d) }, func() { sink = 7 % d })
		check(fmt.Sprint("Divide min ", d), "", func() (string, bool) { return defers.Divide(-1<<63, d) }, func() { sink = (-1 << 63) / d })
	}
	check("Divide text", "runtime error: integer divide by zero", func() (string, bool) { return defers.Divide(1, 0) }, func() { sink = 1 / (n - n) })
	for _, v := range []any{1, "s", nil, int64(1), errors.New("e"), defers.E("x")} {
		check(fmt.Sprintf("Assert %T", v), "", func() (string, bool) { return defers.Assert(v) }, func() { sink = v.(int) })
		check(fmt.Sprintf("AssertIface %T", v), "", func() (string, bool) { return defers.AssertIface(v) }, func() { sink = len(v.(error).Error()) })
	}
	check("Assert text", "interface conversion: interface {} is string, not int", func() (string, bool) { return defers.Assert("s") }, func() { sink = any("s").(int) })
	check("NilFuncCall", "", func() (string, bool) { return defers.NilFuncCall(nil) }, func() { nf() })
	check("NilFuncCall ok", "no panic", func() (string, bool) { return defers.NilFuncCall(func() {}) }, func() {})
	for _, l := range []int{0, 3, 4, 5} {
		s := make([]int, l)
		check(fmt.Sprint("ToArray ", l), "", func() (string, bool) { return defers.ToArray(s) }, func() { sink = [4]int(s)[0] })
	}
	check("ToArray text", "runtime error: cannot convert slice with length 3 to array or pointer to array with length 4", func() (string, bool) { return defers.ToArray(xs) }, func() { sink = [4]int(xs)[0] })
	check("MakeLen text", "runtime error: makeslice: len out of range", func() (string, bool) { return defers.MakeLen(-1) }, func() { sink = len(make([]int, n-n-1)) })
	check("CloseChan nil", "close of nil channel", func() (string, bool) { return defers.CloseChan(nil) }, func() { close(nc) })
	check("CloseChan closed", "close of closed channel", func() (string, bool) { return defers.CloseChan(closed) }, func() { close(closed) })
	for _, v := range []any{"custom", 5, errors.New("an error"), defers.E("e"), 2.5} {
		check(fmt.Sprintf("Custom %T", v), "", func() (string, bool) { return defers.Custom(v) }, func() { panic(v) })
	}
	check("Custom nil", "", func() (string, bool) { return defers.Custom(nil) }, func() { var v any; panic(v) })
	if n < 100 {
		t.Errorf("only %d checks", n)
	}

	eq(t, "DerefOr nil", defers.DerefOr(nil, -9), -9)
	v := 4
	eq(t, "DerefOr", defers.DerefOr(&v, -9), 4)
	eq(t, "IndexOr ok", defers.IndexOr(xs, 2, -1), 6)
	eq(t, "IndexOr high", defers.IndexOr(xs, 3, -1), -1)
	eq(t, "IndexOr negative", defers.IndexOr(xs, -1, -1), -1)
	eq(t, "IndexOr nil", defers.IndexOr(nil, 0, -1), -1)
}

func TestGoexit(t *testing.T) {
	for i := 0; i < 20; i++ {
		log, rec, finished := defers.Goexit()
		deq(t, "Goexit log", log, []string{"inner", "d2", "d1"})
		eq(t, "Goexit recover", rec, nil)
		eq(t, "Goexit finished", finished, false)
	}
}

func TestMethodsAndBuiltins(t *testing.T) {
	log, snap, snap2 := defers.MethodDefers(5)
	deq(t, "MethodDefers log", log, []string{"body", "close", "method-value", "ptr-method"})
	eq(t, "MethodDefers snap", snap, 5)
	eq(t, "MethodDefers snap2", snap2, 5)

	m := map[string]int{"a": 1, "b": 2}
	n, closed, unlocked := defers.BuiltinDefers(m, "a")
	eq(t, "BuiltinDefers len", n, 2)
	eq(t, "BuiltinDefers closed", closed, true)
	eq(t, "BuiltinDefers unlocked", unlocked, true)
	deq(t, "BuiltinDefers delete", m, map[string]int{"b": 2})

	eq(t, "DeferPanic", defers.DeferPanic(), any("deferred panic"))
	im := map[int]int{1: 1, 2: 2}
	eq(t, "DeferClear", defers.DeferClear(im), 2)
	eq(t, "DeferClear effect", len(im), 0)
	eq(t, "DeferPrint", defers.DeferPrint(), 1)
}
