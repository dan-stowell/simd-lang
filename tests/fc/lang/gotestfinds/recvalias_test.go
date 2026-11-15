package gotestfinds

import "testing"

// A receiver declared through an alias of a pointer type (`type PA = *ra; func (p PA) M()`) is a method of
// *ra: fc named the symbol from the alias without looking through it (an internal error), and gave up on a
// generic method declared that way ($GOROOT/test/fixedbugs/issue81165.go).

type ra struct{ n int }
type PA = *ra

func (p PA) Inc()                  { p.n++ }
func (p PA) Get() int              { return p.n }
func (p PA) Add[V ~int](v V)       { p.n += int(v) }
func (p PA) Twice[V any](v V) [2]V { p.n++; return [2]V{v, v} }

type myInt int

type incer interface{ Inc() }

type embedsRA struct {
	ra
	tag string
}

func TestAliasPointerReceiver(t *testing.T) {
	var x ra
	x.Inc()    // &x
	f := x.Inc // method value, bound to &x
	f()
	PA.Inc(&x)       // method expression through the alias
	(*ra).Inc(&x)    // and through the pointer type
	var i incer = &x // *ra's method set
	i.Inc()
	x.Add(30) // generic method, through the alias receiver
	x.Add(myInt(5))
	if x.n != 40 || x.Get() != 40 {
		t.Errorf("n = %d, Get() = %d, want 40", x.n, x.Get())
	}
	if got := x.Twice("s"); got != [2]string{"s", "s"} || x.n != 41 {
		t.Errorf("Twice: %v, n = %d", got, x.n)
	}
	var e embedsRA
	e.Inc() // promoted from the embedded field
	e.Add(2)
	if e.n != 3 {
		t.Errorf("embedded: n = %d, want 3", e.n)
	}
}
