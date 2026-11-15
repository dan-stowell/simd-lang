package closures

import (
	"runtime"
	"testing"
)

type bvNode struct {
	p *byte
	n int
}

//go:noinline
func bvMake(n int) *bvNode { return &bvNode{n: n} }

// a pointer variable that is never assigned again: the closure holds the pointer itself
func bvAdders(k int) []func() int {
	var out []func() int
	for i := 0; i < k; i++ {
		v := bvMake(i)
		m := map[int]int{i: i * 10}
		out = append(out, func() int { v.n++; return v.n + m[i] })
	}
	return out
}

// assigned after its declaration: by reference, the closure sees the new value
func bvReassigned() (func() int, func()) {
	v := bvMake(1)
	get := func() int { return v.n }
	set := func() { v = bvMake(7) }
	return get, set
}

// captured through two levels, by parameters and by a range variable
func bvNested(t *bvNode, list []*bvNode) func() func() int {
	return func() func() int {
		s := 0
		for _, e := range list {
			f := func() int { return e.n + t.n }
			s += f()
		}
		return func() int { return s + t.n }
	}
}

func bvAddr() int {
	v := bvMake(3)
	f := func() int { return v.n }
	pp := &v
	*pp = bvMake(4)
	return f()
}

func bvChan(c chan int, f func(int) int) func() {
	return func() { c <- f(<-c) }
}

func TestCaptureByValue(t *testing.T) {
	fs := bvAdders(4)
	runtime.GC()
	for i, f := range fs {
		if got := f(); got != i+1+i*10 {
			t.Errorf("adder %d: %d", i, got)
		}
		if got := f(); got != i+2+i*10 {
			t.Errorf("adder %d again: %d", i, got)
		}
	}
	get, set := bvReassigned()
	if get() != 1 {
		t.Error("before set")
	}
	set()
	if get() != 7 {
		t.Error("after set")
	}
	g := bvNested(bvMake(100), []*bvNode{bvMake(1), bvMake(2)})
	runtime.GC()
	if got := g()(); got != 303 {
		t.Errorf("nested: %d", got)
	}
	if got := bvAddr(); got != 4 {
		t.Errorf("addr: %d", got)
	}
	c := make(chan int, 1)
	h := bvChan(c, func(x int) int { return x * 2 })
	c <- 21
	h()
	if got := <-c; got != 42 {
		t.Errorf("chan: %d", got)
	}
	done := make(chan *bvNode)
	v := bvMake(9)
	go func() { runtime.GC(); done <- v }()
	if (<-done).n != 9 {
		t.Error("goroutine")
	}
}

// what runtime.AddCleanup checks: the closure object holds the pointer it must not hold
func TestCleanupSeesCapture(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("AddCleanup did not see the captured pointer")
		}
	}()
	v := &bvNode{}
	runtime.AddCleanup(v, func(int) { t.Log(v.n) }, 0)
}
