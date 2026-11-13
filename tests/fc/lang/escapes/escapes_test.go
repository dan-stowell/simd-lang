package escapes_test

import (
	"testing"

	"fclang/escapes"
)

// each case runs on a new goroutine: a small stack, which Grow makes move
func run(f func() int) int {
	c := make(chan int)
	go func() { c <- f() }()
	return <-c
}

func TestHeldAddresses(t *testing.T) {
	const n = 7
	cases := []struct {
		name string
		f    func(int) int
		want int
	}{
		{"DeferInLit", escapes.DeferInLit, n},
		{"DeferTop", escapes.DeferTop, n},
		{"SliceLit", escapes.SliceLit, 2 * n},
		{"Variadic", escapes.Variadic, 3 * n},
		{"MethodValue", escapes.MethodValue, n},
		{"PtrLit", escapes.PtrLit, 2 * n},
		{"Closure", escapes.Closure, n},
		{"Iface", escapes.Iface, n},
		{"New", escapes.New, n},
		{"GoArg", escapes.GoArg, n},
	}
	for round := 0; round < 20; round++ {
		for _, c := range cases {
			if got := run(func() int { return c.f(n) }); got != c.want {
				t.Fatalf("%s: got %d, want %d", c.name, got, c.want)
			}
		}
		a, b := 0, 0
		run(func() int { a, b = escapes.DeferMethod(n); return 0 })
		if a != n || b != n {
			t.Fatalf("DeferMethod: got %d %d, want %d %d", a, b, n, n)
		}
	}
}
