package slices_

import "testing"

var aoX []int

func aoF() int {
	aoX[:1][0] = 3
	return 2
}

type aoPair struct {
	a, b int
	s    string
}

//go:noinline
func aoSwap(x []aoPair) []aoPair { return append(x[:0], x[1], x[0]) }

//go:noinline
func aoIface(x []any, p *int) []any { return append(x[:0], x[2], *p, x[0], len(x)) }

// append's operands are all evaluated before any element is stored
func TestAppendOperandOrder(t *testing.T) {
	aoX = make([]int, 0, 10)
	aoX = append(aoX, 1, aoF())
	if aoX[0] != 1 || aoX[1] != 2 {
		t.Errorf("append(x, 1, f()): %v", aoX)
	}
	x := []int{10, 20, 30}
	x = append(x[:0], x[2], x[1], x[0])
	if x[0] != 30 || x[1] != 20 || x[2] != 10 {
		t.Errorf("reverse through append: %v", x)
	}
	p := aoSwap([]aoPair{{1, 2, "a"}, {3, 4, "b"}})
	if p[0] != (aoPair{3, 4, "b"}) || p[1] != (aoPair{1, 2, "a"}) {
		t.Errorf("swap through append: %v", p)
	}
	n := 7
	y := aoIface([]any{"u", "v", "w", "z"}, &n)
	if y[0] != "w" || y[1] != 7 || y[2] != "u" || y[3] != 4 {
		t.Errorf("interfaces through append: %v", y)
	}
	// the direct forms: constants and plain variables, one element, growth
	a, b := 4, 5
	var z []int
	z = append(z, a, b, 6)
	z = append(z, len(z))
	z = append(z, z[0], z[1])
	if len(z) != 6 || z[0] != 4 || z[1] != 5 || z[2] != 6 || z[3] != 3 || z[4] != 4 || z[5] != 5 {
		t.Errorf("direct: %v", z)
	}
	calls := 0
	next := func() int { calls++; return calls }
	var w []int
	for i := 0; i < 5; i++ {
		w = append(w, next(), next()*10)
	}
	for i := 0; i < 5; i++ {
		if w[2*i] != 2*i+1 || w[2*i+1] != (2*i+2)*10 {
			t.Fatalf("calls in order: %v", w)
		}
	}
}
