package clo_test

import (
	"errors"
	"fmt"
	"testing"

	"fcbasic/clo"
)

func TestClosures(t *testing.T) {
	a := clo.Adder(10)
	if a(1) != 11 || a(5) != 16 {
		t.Error("Adder")
	}
	if got := clo.Apply(func(x int) int { return x * 2 }, []int{1, 2, 3}); got != 12 {
		t.Error("Apply", got)
	}
	fs := clo.Funcs(4)
	if fs[0]() != 0 || fs[3]() != 9 {
		t.Error("Funcs", fs[0](), fs[3]())
	}
	c := &clo.Counter{}
	inc, get, _ := clo.Bound(c)
	inc(3)
	inc(4)
	if c.Get() != 7 || get() != 0 {
		t.Error("Bound", c.Get(), get())
	}
	var log []int
	if r := clo.Defers(&log); r != 42 {
		t.Error("Defers result", r)
	}
	if fmt.Sprint(log) != "[105 2 1 0]" {
		t.Error("Defers log", log)
	}
	if m := clo.Recover(func() { panic("boom") }); m != "boom" {
		t.Error("Recover", m)
	}
	if m := clo.Recover(func() { clo.Div(1, 0) }); m != "runtime error: integer divide by zero" {
		t.Error("Recover div", m)
	}
	if m := clo.Recover(func() { panic(errors.New("e1")) }); m != "e1" {
		t.Error("Recover err", m)
	}
	if m := clo.Recover(func() {}); m != "none" {
		t.Error("Recover none", m)
	}
	if clo.Go(50) != 2450 {
		t.Error("Go")
	}
	if clo.Fact(10) != 3628800 {
		t.Error("Fact")
	}
}

func TestIfaces(t *testing.T) {
	ss := []clo.Shape{clo.Rect{2, 3}, &clo.Circle{1}, clo.BoxR(clo.Rect{1, 1})}
	tot, names := clo.Total(ss)
	if tot != 10 || names != "rect,circle,rect," {
		t.Error("Total", tot, names)
	}
	for _, c := range []struct {
		v    any
		want string
	}{{nil, "nil"}, {5, "int"}, {50, "big int"}, {"x", "string x"}, {clo.Rect{}, "shape rect"}, {errors.New("bad"), "error bad"}, {[]int{1}, "ints"}, {[2]int{}, "ints"}, {3.5, "unknown"}} {
		if got := clo.Describe(c.v); got != c.want {
			t.Errorf("Describe(%v) = %q, want %q", c.v, got, c.want)
		}
	}
	if r, ok := clo.AsRect(clo.Rect{4, 5}); !ok || r.W != 4 {
		t.Error("AsRect")
	}
	if _, ok := clo.AsRect(&clo.Circle{}); ok {
		t.Error("AsRect circle")
	}
	if clo.MustCircle(&clo.Circle{2}).R != 2 {
		t.Error("MustCircle")
	}
	if m := clo.Recover(func() { clo.MustCircle(clo.Rect{}) }); m != "interface conversion: clo.Shape is clo.Rect, not *clo.Circle" {
		t.Error("MustCircle panic:", m)
	}
	if clo.Box(7) != any(7) || clo.BoxS("hi") != any("hi") || !clo.Same(clo.Box(1000), 1000) || clo.Same(1, "1") {
		t.Error("Box")
	}
	if !clo.IsNil(nil) || clo.IsNil(clo.Rect{}) {
		t.Error("IsNil")
	}
}

func TestMaps(t *testing.T) {
	m := clo.Words("a bb a  ccc bb a")
	if len(m) != 3 || m["a"] != 3 || m["bb"] != 2 || m["ccc"] != 1 {
		t.Error("Words", m)
	}
	sum, cnt, has, gone := clo.MapOps(10)
	// m = {0:0,2:11,3:9,...,9:27}; v=11; sum = 11 + sum(k+v)
	want := 11
	for i := 0; i < 10; i++ {
		if i == 1 {
			continue
		}
		v := i * 3
		if i == 2 {
			v += 5
		}
		want += i + v
	}
	if sum != want || cnt != 9009 || !has || gone {
		t.Error("MapOps", sum, want, cnt, has, gone)
	}
	if clo.Lookup("a") != 3 || clo.Lookup("zz") != 0 {
		t.Error("Lookup")
	}
}
