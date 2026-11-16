//go:build go1.21

// A file whose language version is before go1.22 (its //go:build line lowers the module's): the variables a
// for statement declares are one per loop, not one per iteration.

package gotestfinds

import "testing"

func TestOldLoopVars(t *testing.T) {
	var fs []func() int
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { return i })
	}
	for k, f := range fs {
		if got := f(); got != 3 {
			t.Errorf("three-clause closure %d: got %d, want 3", k, got)
		}
	}

	var ps []*int
	for i := 0; i < 3; i++ {
		ps = append(ps, &i)
	}
	if ps[0] != ps[2] || *ps[0] != 3 {
		t.Errorf("three-clause pointers: %p %p, *=%d", ps[0], ps[2], *ps[0])
	}

	var gs []func() (int, int)
	for i, v := range []int{10, 20, 30} {
		gs = append(gs, func() (int, int) { return i, v })
	}
	for k, g := range gs {
		if i, v := g(); i != 2 || v != 30 {
			t.Errorf("range closure %d: got %d, %d, want 2, 30", k, i, v)
		}
	}

	var qs []*string
	for _, s := range []string{"a", "b", "c"} {
		qs = append(qs, &s)
	}
	if qs[0] != qs[2] || *qs[0] != "c" {
		t.Errorf("range pointers: %q %q", *qs[0], *qs[2])
	}

	m := map[string]int{"x": 1}
	var hs []func() string
	for k := range m {
		hs = append(hs, func() string { return k })
	}
	if hs[0]() != "x" {
		t.Errorf("map range closure: %q", hs[0]())
	}

	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3
	close(ch)
	var cs []func() int
	for x := range ch {
		cs = append(cs, func() int { return x })
	}
	if cs[0]() != 3 {
		t.Errorf("channel range closure: %d, want 3", cs[0]())
	}
}
