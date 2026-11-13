// Package escapes: objects that do not escape and hold the addresses of frame variables. Each function
// makes one, forces the stack to move (Grow), and then writes through it: if the object was allocated in the
// heap while the variable stayed in the frame, the write goes to the old stack and the result is wrong.
package escapes

// Grow recurses deep enough to make the goroutine's stack move.
func Grow(n int) int {
	var pad [256]byte
	if n == 0 {
		return int(pad[0])
	}
	pad[n%256] = byte(n)
	return Grow(n-1) + int(pad[n%256]) - int(byte(n))
}

type Rec struct {
	Log []string
	N   int
}

func (r Rec) Snapshot(out *int) { *out = r.N }
func (r *Rec) Set(out *int)     { *out = r.N }

type Box struct {
	P *int
	Q []*int
}

func set(out *int, v int) { *out = v }

func setAll(v int, outs ...*int) {
	Grow(200)
	for _, p := range outs {
		*p = v
	}
}

// DeferInLit: a deferred call in a function literal binds the address of the enclosing function's result.
func DeferInLit(n int) (snap int) {
	func() {
		defer set(&snap, n)
		Grow(200)
	}()
	return snap
}

// DeferMethod: the same with a method whose receiver is copied.
func DeferMethod(n int) (snap, snap2 int) {
	r := &Rec{N: n}
	func() {
		defer r.Snapshot(&snap)
		defer Rec.Snapshot(*r, &snap2)
		Grow(200)
		r.N = n + 100
	}()
	return snap, snap2
}

// DeferTop: a deferred call at the top of a function binds the address of a local.
func DeferTop(n int) (out int) {
	var x int
	defer func() { out = x }()
	defer set(&x, n)
	Grow(200)
	return 0
}

// SliceLit: a slice literal of addresses of locals.
func SliceLit(n int) int {
	var a, b int
	ps := []*int{&a, &b}
	Grow(200)
	for _, p := range ps {
		*p = n
	}
	return a + b
}

// Variadic: the slice of variadic arguments.
func Variadic(n int) int {
	var a, b, c int
	setAll(n, &a, &b, &c)
	return a + b + c
}

// MethodValue: a method value bound to the address of a local.
func MethodValue(n int) int {
	var out int
	r := Rec{N: n}
	f := r.Set
	Grow(200)
	f(&out)
	return out
}

// PtrLit: &T{...} holding the address of a local, and a literal inside it.
func PtrLit(n int) int {
	var a, b int
	bx := &Box{P: &a, Q: []*int{&b}}
	Grow(200)
	*bx.P = n
	*bx.Q[0] = n
	return a + b
}

// Closure: a function literal that does not escape, called after the stack moved.
func Closure(n int) int {
	var a int
	f := func() { a = n }
	Grow(200)
	f()
	return a
}

// Iface: a value boxed in an interface holds the address of a local.
func Iface(n int) int {
	var a int
	var v any = Box{P: &a}
	Grow(200)
	*v.(Box).P = n
	return a
}

// New: new(T) pointing at a local through a later store (which flows to the heap: a must move there).
func New(n int) int {
	var a int
	bx := new(Box)
	bx.P = &a
	Grow(200)
	*bx.P = n
	return a
}

// GoArg: a goroutine gets the address of a local.
func GoArg(n int) int {
	var a int
	done := make(chan bool)
	go func(p *int) {
		Grow(200)
		*p = n
		done <- true
	}(&a)
	Grow(200)
	<-done
	return a
}
