package order

import "testing"

type saS struct {
	a, b int
	s, t string
}

type saN struct {
	in  saS
	arr [3]int
	p   *saS
}

//go:noinline
func saID[T any](x T) T { return x }

// a composite value assigned to a variable its own operands read: every operand is read before anything is stored
func TestSelfAssign(t *testing.T) {
	x := [2]int{1, 2}
	x = [2]int{x[1], x[0]}
	if x != [2]int{2, 1} {
		t.Errorf("array literal: %v", x)
	}
	s := saS{1, 2, "a", "b"}
	s = saS{s.b, s.a, s.t, s.s}
	if s != (saS{2, 1, "b", "a"}) {
		t.Errorf("struct literal: %v", s)
	}
	s = saS{b: s.a, a: s.b, t: s.s, s: s.t}
	if s != (saS{1, 2, "a", "b"}) {
		t.Errorf("keyed struct literal: %v", s)
	}
	p := &saS{1, 2, "a", "b"}
	*p = saS{p.b, p.a, p.t, p.s}
	if *p != (saS{2, 1, "b", "a"}) {
		t.Errorf("through a pointer: %v", *p)
	}
	n := saN{in: saS{1, 2, "a", "b"}, arr: [3]int{1, 2, 3}}
	n.in = saS{n.in.b, n.in.a, n.in.t, n.in.s}
	n.arr = [3]int{n.arr[2], n.arr[1], n.arr[0]}
	if n.in != (saS{2, 1, "b", "a"}) || n.arr != [3]int{3, 2, 1} {
		t.Errorf("fields: %v", n)
	}
	n = saN{in: saS{n.in.b, n.in.a, "x", "y"}, arr: [3]int{n.arr[1], n.arr[2], n.arr[0]}, p: &n.in}
	if n.in != (saS{1, 2, "x", "y"}) || n.arr != [3]int{2, 1, 3} {
		t.Errorf("nested: %v %v", n.in, n.arr)
	}
	sl := []saS{{1, 2, "a", "b"}, {3, 4, "c", "d"}}
	sl[0] = saS{sl[0].b, sl[1].a, sl[0].t, sl[1].s}
	if sl[0] != (saS{2, 3, "b", "c"}) {
		t.Errorf("slice element: %v", sl[0])
	}
	m := map[string]saS{"k": {1, 2, "a", "b"}}
	m["k"] = saS{m["k"].b, m["k"].a, m["k"].t, m["k"].s}
	if m["k"] != (saS{2, 1, "b", "a"}) {
		t.Errorf("map element: %v", m["k"])
	}
	a, b := 1, 2
	a, b = b, a
	sl[0].a, sl[0].b = sl[0].b, sl[0].a
	x[0], x[1] = x[1], x[0]
	if a != 2 || b != 1 || sl[0].a != 3 || sl[0].b != 2 || x != [2]int{1, 2} {
		t.Errorf("tuple assignments: %d %d %v %v", a, b, sl[0], x)
	}
	c := complex(1, 2)
	c = complex(imag(c), real(c))
	if c != complex(2, 1) {
		t.Errorf("complex: %v", c)
	}
	var e any = saS{1, 2, "a", "b"}
	e = saS{e.(saS).b, e.(saS).a, "", ""}
	if e != (saS{2, 1, "", ""}) {
		t.Errorf("interface: %v", e)
	}
	y := [2]saS{{1, 2, "a", "b"}, {3, 4, "c", "d"}}
	y = [2]saS{y[1], y[0]}
	y[0], y[1] = saS{y[1].b, y[0].a, "p", "q"}, saID(y[0])
	if y[0] != (saS{2, 3, "p", "q"}) || y[1] != (saS{3, 4, "c", "d"}) {
		t.Errorf("array of structs: %v", y)
	}
	str := []string{"a", "b", "c"}
	str = append(str[:1], str[2], str[1])
	if str[0] != "a" || str[1] != "c" || str[2] != "b" {
		t.Errorf("strings through append: %v", str)
	}
}
