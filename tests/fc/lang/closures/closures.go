// Package closures exercises function literals, captured variables, loop-variable semantics,
// method values, method expressions and function values stored in data structures.
// Imports: none.
package closures

// ---- capture by reference ----

// Counter returns two closures sharing one captured variable.
func Counter(start int) (inc func() int, get func() int) {
	n := start
	inc = func() int { n++; return n }
	get = func() int { return n }
	return
}

// CaptureParam: the closure writes the parameter; the function sees the writes.
func CaptureParam(x int) (before, after int) {
	f := func() { x *= 2 }
	before = x
	f()
	f()
	return before, x
}

// CaptureAfter: the variable is changed after the closure is made; the closure sees the change.
func CaptureAfter(x int) int {
	y := x
	f := func() int { return y }
	y += 100
	return f()
}

// CaptureMany captures variables of several kinds and changes all of them.
func CaptureMany(i int, s string, f float64, xs []int) (int, string, float64, []int) {
	type pair struct{ a, b int }
	p := pair{i, i + 1}
	g := func(k int) {
		i += k
		s += "!"
		f *= 2
		xs = append(xs, k)
		p.a, p.b = p.b, p.a
	}
	g(1)
	g(2)
	return i + p.a*1000, s, f, xs
}

func Adder(a int) func(int) int { return func(b int) int { return a + b } }

func Compose(f, g func(int) int) func(int) int { return func(x int) int { return f(g(x)) } }

// Curry3 nests three levels; the innermost captures from every level.
func Curry3(a int) func(int) func(int) int {
	return func(b int) func(int) int {
		ab := a * b
		return func(c int) int {
			a++
			return ab + c + a
		}
	}
}

// MakeCounters: each closure has its own state.
func MakeCounters(n int) []func() int {
	fs := make([]func() int, n)
	for i := range fs {
		c := i * 10
		fs[i] = func() int { c++; return c }
	}
	return fs
}

// FibGen returns a generator of Fibonacci numbers.
func FibGen() func() uint64 {
	a, b := uint64(0), uint64(1)
	return func() uint64 {
		r := a
		a, b = b, a+b
		return r
	}
}

type Big struct {
	A [40]int
	S string
}

// CaptureBig captures a large struct by reference and returns a closure working on it.
func CaptureBig(seed int) (set func(i, v int), sum func() int, name func() string) {
	var b Big
	for i := range b.A {
		b.A[i] = seed + i
	}
	b.S = "big"
	set = func(i, v int) { b.A[i] = v; b.S += "+" }
	sum = func() int {
		t := 0
		for _, v := range b.A {
			t += v
		}
		return t
	}
	name = func() string { return b.S }
	return
}

// NamedResult: a deferred closure captures and changes a named result.
func NamedResult(x int) (r int, s string) {
	defer func() {
		r += 1000
		s += "-deferred"
	}()
	return x, "ret"
}

// ---- loop variables: one per iteration (Go 1.22) ----

func LoopVar3(n int) []func() int {
	var fs []func() int
	for i := 0; i < n; i++ {
		fs = append(fs, func() int { return i })
	}
	return fs
}

// LoopVar3Mod: a closure called in the body changes the variable the loop goes on with.
func LoopVar3Mod(n int) []int {
	var out []int
	for i := 0; i < n; i++ {
		f := func() { i++ }
		f()
		out = append(out, i)
	}
	return out
}

// LoopVar3Late: closures called after the loop change only their own copy.
func LoopVar3Late(n int) (first, second []int) {
	var fs []func() int
	for i := 0; i < n; i++ {
		fs = append(fs, func() int { i += 10; return i })
	}
	for _, f := range fs {
		first = append(first, f())
	}
	for _, f := range fs {
		second = append(second, f())
	}
	return
}

// LoopVar3Post: the copy for the next iteration is made before the post statement, so a closure made in
// iteration i still sees i (not i+1) afterwards.
func LoopVar3Post(n int) []int {
	var fs []func() int
	for i, j := 0, 100; i < n; i, j = i+1, j-1 {
		fs = append(fs, func() int { return i*1000 + j })
	}
	out := make([]int, 0, len(fs))
	for _, f := range fs {
		out = append(out, f())
	}
	return out
}

func LoopAddr(n int) []*int {
	var ps []*int
	for i := 0; i < n; i++ {
		ps = append(ps, &i)
	}
	return ps
}

func LoopRange(xs []string) []func() string {
	var fs []func() string
	for i, s := range xs {
		fs = append(fs, func() string {
			r := s
			for k := 0; k < i; k++ {
				r += s
			}
			return r
		})
	}
	return fs
}

func LoopRangeInt(n int) []func() int {
	var fs []func() int
	for i := range n {
		fs = append(fs, func() int { i *= 2; return i })
	}
	return fs
}

func LoopRangeMap(m map[string]int) []func() (string, int) {
	var fs []func() (string, int)
	for k, v := range m {
		fs = append(fs, func() (string, int) { return k, v })
	}
	return fs
}

func LoopRangeString(s string) []func() rune {
	var fs []func() rune
	for _, r := range s {
		fs = append(fs, func() rune { return r })
	}
	return fs
}

// ---- recursive closures ----

func Fib(n int) int {
	var fib func(int) int
	fib = func(k int) int {
		if k < 2 {
			return k
		}
		return fib(k-1) + fib(k-2)
	}
	return fib(n)
}

func EvenOdd(n int) (even bool, calls int) {
	var isEven, isOdd func(int) bool
	isEven = func(k int) bool {
		calls++
		if k == 0 {
			return true
		}
		return isOdd(k - 1)
	}
	isOdd = func(k int) bool {
		calls++
		if k == 0 {
			return false
		}
		return isEven(k - 1)
	}
	return isEven(n), calls
}

type Tree struct {
	L, R *Tree
	V    int
}

// Walk uses a recursive closure that captures an accumulator and a callback.
func Walk(t *Tree, visit func(int)) (count int) {
	var walk func(*Tree)
	walk = func(t *Tree) {
		if t == nil {
			return
		}
		walk(t.L)
		count++
		visit(t.V)
		walk(t.R)
	}
	walk(t)
	return
}

// ---- method values ----

type Acc struct {
	N    int
	Name string
}

func (a Acc) Get() int              { return a.N }
func (a Acc) Label(p string) string { return p + a.Name }
func (a *Acc) Add(d int) int        { a.N += d; return a.N }

type Getter interface{ Get() int }

// MethodValueVal: a value-receiver method value copies the receiver when it is made.
func MethodValueVal(a Acc) (bound, now int) {
	f := a.Get
	a.N += 100
	return f(), a.N
}

// MethodValuePtr: a pointer-receiver method value keeps the pointer.
func MethodValuePtr(a Acc, d1, d2 int) (int, int, int) {
	f := a.Add
	r1 := f(d1)
	r2 := f(d2)
	return r1, r2, a.N
}

// MethodValueThroughPtr: p.Get with a value receiver copies *p when the method value is made.
func MethodValueThroughPtr(p *Acc) (bound, now int) {
	f := p.Get
	p.N += 7
	return f(), p.N
}

func MethodValueIface(g Getter) func() int { return g.Get }

func MethodValueLabel(a Acc) func(string) string { return a.Label }

func BoundAdd(p *Acc) func(int) int { return p.Add }

// ---- method expressions ----

func MethodExprVal(a Acc) int {
	f := Acc.Get
	return f(a)
}

func MethodExprPtr(p *Acc, d int) int {
	f := (*Acc).Add
	return f(p, d)
}

// MethodExprPtrVal: (*Acc).Get is in the method set of *Acc.
func MethodExprPtrVal(p *Acc) int {
	f := (*Acc).Get
	return f(p)
}

func MethodExprIface(g Getter) int {
	f := Getter.Get
	return f(g)
}

func MethodExprs() (func(Acc) int, func(*Acc, int) int, func(Acc, string) string) {
	return Acc.Get, (*Acc).Add, Acc.Label
}

// ---- function values in data structures ----

type Op struct {
	Name string
	F    func(int, int) int
}

func sub(a, b int) int { return a - b }

func Ops() []Op {
	return []Op{
		{"add", func(a, b int) int { return a + b }},
		{"sub", sub},
		{"mul", func(a, b int) int { return a * b }},
		{Name: "nil"},
	}
}

var opTable = map[string]func(int, int) int{
	"add": func(a, b int) int { return a + b },
	"sub": sub,
	"max": func(a, b int) int {
		if a > b {
			return a
		}
		return b
	},
}

func Apply(name string, a, b int) (int, bool) {
	f, ok := opTable[name]
	if !ok {
		return 0, false
	}
	return f(a, b), true
}

func Pipeline(x int, fs ...func(int) int) int {
	for _, f := range fs {
		x = f(x)
	}
	return x
}

func Each(xs []int, f func(i, v int)) {
	for i, v := range xs {
		f(i, v)
	}
}

// SumVia calls Each with a closure that captures a local.
func SumVia(xs []int) (sum, weighted int) {
	Each(xs, func(i, v int) {
		sum += v
		weighted += i * v
	})
	return
}

func Filter(xs []int, keep func(int) bool) []int {
	var out []int
	for _, x := range xs {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

func Between(lo, hi int) func(int) bool { return func(x int) bool { return lo <= x && x <= hi } }

var saved func() int

// Save stores a closure in a package-level variable; CallSaved calls it later.
func Save(x int) {
	saved = func() int { x++; return x }
}

func CallSaved() int { return saved() }

// ---- nil, immediate calls, variadic ----

func IsNil(f func()) bool { return f == nil }

func NotNil(f func(int) int) bool { return nil != f }

func CallOrDefault(f func() int, d int) int {
	if f != nil {
		return f()
	}
	return d
}

// CallNil calls f even when it is nil (a nil pointer dereference panic).
func CallNil(f func() int) int { return f() }

func Immediate(x int) int {
	return func(y int) int { return y * y }(x) + func() int { return x + 1 }()
}

func ImmediateMulti(a, b int) (int, int) {
	return func() (int, int) { return b, a }()
}

func VariadicClosure(base int) func(...int) int {
	return func(xs ...int) int {
		t := base
		for _, x := range xs {
			t += x
		}
		base = t
		return t
	}
}

// Swapper returns closures over two pointers' targets.
func Swapper(p, q *int) func() {
	return func() { *p, *q = *q, *p }
}

// Memo wraps f with a cache held in a captured map.
func Memo(f func(int) int) (func(int) int, func() int) {
	cache := map[int]int{}
	calls := 0
	return func(x int) int {
			if v, ok := cache[x]; ok {
				return v
			}
			calls++
			v := f(x)
			cache[x] = v
			return v
		}, func() int {
			return calls
		}
}
