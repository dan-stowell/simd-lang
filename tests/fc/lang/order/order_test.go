package order_test

import (
	"reflect"
	"testing"

	"fclang/internal/chk"
	"fclang/order"
)

func TestMain(m *testing.M) { chk.Main(m) }

// Every exported function against its stock-compiled copy (order_ref_test.go).  Loop bounds and recursion
// depths get small pools; everything else runs over chk's integer patterns.
func TestAuto(t *testing.T) {
	ns := []any{0, 1, 2, 3, 4, 5, 7, 8, 10, 51, 100, 1000, -1, -5}
	n := []chk.Opt{chk.Arg(0, ns...)}
	idx := []any{0, 1, 2, 3, 8, -1}
	chk.All(t, autoFuncs, map[string][]chk.Opt{
		"Fib":               n,
		"OrderAssign":       {chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Limit(800)},
		"OrderIndex":        {chk.Arg(1, idx...)},
		"SwapElems":         {chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Limit(800)},
		"AssignMapSlice":    {chk.Arg(1, idx...)},
		"ShortCircuitGuard": {chk.Arg(1, append([]any{1 << 62, -1 << 63}, idx...)...)},
	})
}

func TestByHand(t *testing.T) {
	// short-circuit: which operands were evaluated, in order
	for _, c := range []struct {
		a, b, c bool
		trace   string
		r       bool
	}{{true, true, false, "ab", true}, {true, false, true, "abc", true}, {false, true, true, "ac", true}, {false, false, false, "ac", false}, {true, false, false, "abc", false}} {
		if tr, r := order.ShortCircuit(c.a, c.b, c.c); tr != c.trace || r != c.r {
			t.Errorf("ShortCircuit(%v, %v, %v) = %q, %v", c.a, c.b, c.c, tr, r)
		}
	}
	if tr, r := order.ShortCircuit2(false, true, false, false); tr != "abcd" || !r {
		t.Error("ShortCircuit2", tr, r)
	}
	if tr, r := order.ShortCircuit2(true, true, true, true); tr != "ac" || !r {
		t.Error("ShortCircuit2", tr, r)
	}
	if tr, r := order.ShortCircuit2(false, false, true, true); tr != "ab" || r {
		t.Error("ShortCircuit2", tr, r)
	}
	if got := order.ShortCircuitIf(true, false, false); got != "a!xa.xa.x" {
		t.Error("ShortCircuitIf", got)
	}
	if got := order.ShortCircuitIf(false, true, true); got != "abc!xa" {
		t.Error("ShortCircuitIf", got)
	}
	if order.NilGuard(nil) || !order.NilGuardOr(nil) || order.ShortCircuitGuard(nil, 0) {
		t.Error("guards must not evaluate the guarded operand")
	}
	// calls and operands left to right
	if tr, r := order.OrderCall(1, 2, 3); tr != "abc" || r != 123 {
		t.Error("OrderCall", tr, r)
	}
	if tr, r := order.OrderBinary(10, 2, 3); tr != "abcdef" || r != 10-6+123 {
		t.Error("OrderBinary", tr, r)
	}
	// assignment: index and pointer operands on the left, then the right side, then the assignments in order
	if tr, xs := order.OrderAssign([]int{0, 0, 0}, 2, 0); tr != "xiyjvw" || !reflect.DeepEqual(xs, []int{20, 0, 10}) {
		t.Error("OrderAssign", tr, xs)
	}
	if tr, xs := order.OrderAssign([]int{0, 0, 0}, 1, 1); tr != "xiyjvw" || !reflect.DeepEqual(xs, []int{0, 20, 0}) {
		t.Error("OrderAssign to the same element: the second assignment wins", tr, xs)
	}
	if tr, v := order.OrderIndex([]int{3, 4}, 1); tr != "ipvsk" || v != 8 {
		t.Error("OrderIndex", tr, v)
	}
	if tr, xs := order.OrderOpAssign([]int{1, 2}); tr != "sivtj" || !reflect.DeepEqual(xs, []int{2, 7}) {
		t.Error("OrderOpAssign", tr, xs)
	}
	if tr, arr, sl := order.OrderLit(1, 2, 3); tr != "abcxy" || arr != [3]int{1, 2, 3} || !reflect.DeepEqual(sl, []int{2, 0, 1}) {
		t.Error("OrderLit", tr, arr, sl)
	}
	if got := order.OrderStr(1, -1); got != "pqr:+--" {
		t.Error("OrderStr", got)
	}
	if i, xs := order.IndexThenAssign([]int{0, 0}); i != 1 || !reflect.DeepEqual(xs, []int{2, 0}) {
		t.Error("i, x[i] = 1, 2 must set x[0]", i, xs)
	}
	if i, xs := order.AssignIndexAfter([]int{0, 0, 5}); i != 2 || !reflect.DeepEqual(xs, []int{7, 5, 0}) {
		t.Error("AssignIndexAfter", i, xs)
	}
	if a, b, p := order.PtrThenAssign(1, 2); a != 99 || b != 2 || p != 2 {
		t.Error("p, *p = &b, 99 must write through the old p", a, b, p)
	}
	if a, b := order.SwapInts(1, 2); a != 2 || b != 1 {
		t.Error("swap")
	}
	if a, b, c := order.Rotate3(1, 2, 3); a != 2 || b != 3 || c != 1 {
		t.Error("rotate")
	}
	if x, v := order.AssignMapSlice([]int{5, 6}, 1); x != 6*1000+7 || v != 1 {
		t.Error("AssignMapSlice", x, v)
	}
	if lo, hi := order.Named(-4); lo != -8 || hi != -4 {
		t.Error("named results")
	}
	if order.NamedShadow(3) != 4 || order.NamedShadow(0) != 1 || order.ResultParamAlias(3) != 12 {
		t.Error("named results and shadowing")
	}
}
