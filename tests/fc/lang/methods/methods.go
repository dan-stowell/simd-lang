// Package methods exercises method declarations and calls: value and pointer receivers, automatic
// address-of and dereference, promotion through embedding, method sets and interface satisfaction,
// methods on non-struct named types and calls through nil pointers.
// Imports: none.
package methods

// ---- value and pointer receivers ----

type Point struct{ X, Y int }

func (p Point) Sum() int            { return p.X + p.Y }
func (p Point) Scaled(k int) Point  { p.X *= k; p.Y *= k; return p }
func (p Point) SetXCopy(x int) int  { p.X = x; return p.X }
func (p *Point) Move(dx, dy int)    { p.X += dx; p.Y += dy }
func (p *Point) Scale(k int) *Point { p.X *= k; p.Y *= k; return p }
func (p *Point) IsNil() bool        { return p == nil }
func (p Point) Dot(q Point) int     { return p.X*q.X + p.Y*q.Y }
func (p Point) Both() (int, int)    { return p.X, p.Y }
func (p *Point) Swap() (int, int)   { p.X, p.Y = p.Y, p.X; return p.X, p.Y }
func (p Point) AddAll(ds ...int) int {
	s := p.X + p.Y
	for _, d := range ds {
		s += d
	}
	return s
}
func (p Point) String() string { return "(" + itoa(p.X) + "," + itoa(p.Y) + ")" }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ValueCalls: value-receiver methods called on a value, a pointer and an element.
func ValueCalls(p Point, k int) (int, int, int, Point) {
	pp := &p
	arr := []Point{p, p.Scaled(k)}
	return p.Sum(), pp.Sum(), arr[1].Sum(), pp.Scaled(k).Scaled(k)
}

// CopyReceiver: a value receiver is a copy; the caller's value does not change.
func CopyReceiver(p Point, x int) (int, int) {
	r := p.SetXCopy(x)
	return r, p.X
}

// AutoAddr: pointer-receiver methods called on addressable values (variable, field, element).
func AutoAddr(p Point, dx, dy int) (Point, Point, Point) {
	type holder struct{ P Point }
	h := holder{p}
	xs := []Point{p}
	p.Move(dx, dy)
	h.P.Move(dx*2, dy*2)
	xs[0].Move(dx*3, dy*3)
	return p, h.P, xs[0]
}

// Chain: methods returning the receiver pointer.
func Chain(p *Point, a, b int) int {
	return p.Scale(a).Scale(b).Sum()
}

func MultiResults(p Point) (int, int, int, int) {
	a, b := p.Both()
	c, d := p.Swap()
	return a, b, c, d
}

func Variadic(p Point, ds []int) (int, int, int) {
	return p.AddAll(), p.AddAll(1, 2, 3), p.AddAll(ds...)
}

func MapOfPointers(m map[string]*Point, k string, d int) int {
	m[k].Move(d, d)
	return m[k].Sum()
}

// ---- embedding ----

type Named struct {
	Point
	Name string
}

func (n Named) Title() string { return n.Name + n.String() }

type Deep struct {
	Named
	Z int
}

// Sum on Deep hides the promoted Point.Sum.
func (d Deep) Sum() int { return d.Named.Sum() + d.Z }

type PNamed struct {
	*Point
	Name string
}

type PDeep struct {
	*PNamed
	Extra int
}

type Mixed struct {
	Counter
	*Point
}

type Counter struct{ N int }

func (c *Counter) Inc() int { c.N++; return c.N }
func (c Counter) Get() int  { return c.N }

func PromotedValue(n Named) (int, string, string) {
	return n.Sum(), n.String(), n.Title()
}

func PromotedPtr(n Named, dx, dy int) Named {
	n.Move(dx, dy)
	pn := &n
	pn.Move(dx, dy)
	return n
}

func Depth2(d Deep, dx int) (int, int, string, Deep) {
	d.Move(dx, 0)
	return d.Sum(), d.Point.Sum(), d.Title(), d
}

func EmbeddedPtr(p *Point, name string, d int) (int, string, int) {
	pn := PNamed{p, name}
	pn.Move(d, d)
	pd := PDeep{&pn, 1}
	pd.Move(d, d)
	return pn.Sum(), pd.String(), pd.Scaled(2).X
}

func MixedEmbed(p *Point, n int) (int, int, int) {
	m := Mixed{Counter{n}, p}
	m.Inc()
	m.Inc()
	m.Move(1, 1)
	return m.Get(), m.N, m.Sum()
}

// ---- interfaces and method sets ----

type Summer interface{ Sum() int }
type Mover interface{ Move(dx, dy int) }
type SumMover interface {
	Summer
	Mover
}
type Stringer interface{ String() string }

func SumOf(s Summer) int { return s.Sum() }

func MoveAndSum(m SumMover, d int) int {
	m.Move(d, d)
	return m.Sum()
}

// Summers returns values of different dynamic types that all satisfy Summer.
func Summers(p Point) []Summer {
	n := Named{p, "n"}
	d := Deep{n, 100}
	pn := PNamed{&p, "pn"}
	return []Summer{p, &p, n, &n, d, &d, pn, &pn, PDeep{&pn, 0}, Mixed{Counter{}, &p}, MyInt(p.X), IntSlice{p.X, p.Y}}
}

func Movers(p *Point, n *Named, d *Deep) []Mover {
	return []Mover{p, n, d, PNamed{p, ""}, &PNamed{p, ""}, PDeep{&PNamed{p, ""}, 0}}
}

func MoveAll(ms []Mover, d int) {
	for _, m := range ms {
		m.Move(d, -d)
	}
}

// IsSumMover reports whether the dynamic type of v has both methods.
func IsSumMover(v any) bool {
	_, ok := v.(SumMover)
	return ok
}

func Str(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}

type Logger struct {
	Summer
	Prefix string
}

// EmbeddedIface: methods of an embedded interface are promoted.
func EmbeddedIface(s Summer) (int, int) {
	l := Logger{s, "log"}
	var s2 Summer = l
	return l.Sum(), s2.Sum()
}

// ---- methods on non-struct named types ----

type MyInt int

func (m MyInt) Double() MyInt  { return m * 2 }
func (m MyInt) IsNeg() bool    { return m < 0 }
func (m MyInt) Sum() int       { return int(m) }
func (m *MyInt) Inc()          { *m++ }
func (m MyInt) String() string { return "MyInt(" + itoa(int(m)) + ")" }

type IntSlice []int

func (s IntSlice) Len() int { return len(s) }
func (s IntSlice) Sum() int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
func (s *IntSlice) Push(v int) { *s = append(*s, v) }
func (s IntSlice) SetFirst(v int) {
	if len(s) > 0 {
		s[0] = v
	}
}

type StrMap map[string]int

func (m StrMap) Get(k string) int    { return m[k] }
func (m StrMap) Set(k string, v int) { m[k] = v }
func (m StrMap) Total() int {
	t := 0
	for _, v := range m {
		t += v
	}
	return t
}

type BinOp func(int, int) int

func (f BinOp) Apply(a, b int) int { return f(a, b) }
func (f BinOp) Flip() BinOp        { return func(a, b int) int { return f(b, a) } }

type Celsius float64

func (c Celsius) Fahrenheit() float64 { return float64(c)*9/5 + 32 }

type Name string

func (n Name) Len() int         { return len(n) }
func (n Name) Twice() Name      { return n + n }
func (n *Name) Append(s string) { *n += Name(s) }

type Arr [4]int

func (a Arr) Set(i, v int) Arr { a[i] = v; return a }
func (a *Arr) SetP(i, v int)   { a[i] = v }
func (a Arr) Sum() int         { return a[0] + a[1] + a[2] + a[3] }

type Flag bool

func (f Flag) Not() Flag { return !f }

type Ch chan int

func (c Ch) SendRecv(v int) int { c <- v; return <-c }

type PtrTo *int // a named pointer type cannot have methods; used only as a field below

type Wrapper struct {
	P PtrTo
}

func (w Wrapper) Load() int { return *w.P }

func IntMethods(x int) (MyInt, bool, MyInt, string) {
	m := MyInt(x)
	d := m.Double()
	m.Inc()
	(&m).Inc()
	return d, m.IsNeg(), m, m.String()
}

func SliceMethods(xs []int, v int) (int, int, []int, int) {
	s := IntSlice(xs)
	n0 := s.Len()
	s.Push(v)
	s.Push(v + 1)
	s.SetFirst(-1)
	return n0, s.Len(), s, s.Sum()
}

func MapMethods(m map[string]int, k string, v int) (int, int, int) {
	sm := StrMap(m)
	old := sm.Get(k)
	sm.Set(k, v)
	return old, sm.Get(k), sm.Total()
}

func FuncMethods(a, b int) (int, int) {
	sub := BinOp(func(x, y int) int { return x - y })
	return sub.Apply(a, b), sub.Flip().Apply(a, b)
}

func OtherMethods(c float64, s string, f bool, v int) (float64, int, Name, Flag, int, int) {
	n := Name(s)
	n.Append("!")
	ch := make(Ch, 1)
	w := Wrapper{&v}
	return Celsius(c).Fahrenheit(), n.Len(), n.Twice(), Flag(f).Not(), ch.SendRecv(v), w.Load()
}

func ArrMethods(a Arr, i, v int) (Arr, Arr, int) {
	b := a.Set(i, v) // a unchanged
	c := a
	c.SetP(i, v+1)
	return a, b, c.Sum()
}

// ---- nil receivers ----

type Node struct {
	Val  int
	Next *Node
}

// Len is fine on a nil list.
func (n *Node) Len() int {
	if n == nil {
		return 0
	}
	return 1 + n.Next.Len()
}

func (n *Node) Push(v int) *Node { return &Node{v, n} }

func (n *Node) Total() int {
	t := 0
	for ; n != nil; n = n.Next {
		t += n.Val
	}
	return t
}

func (n Node) Value() int { return n.Val }

func BuildList(vs []int) *Node {
	var n *Node
	for _, v := range vs {
		n = n.Push(v)
	}
	return n
}

func NilPtrMethod(p *Point) bool { return p.IsNil() }

// NilValueMethod calls a value-receiver method through a pointer: a nil pointer panics.
func NilValueMethod(p *Point) int { return p.Sum() }

// NilNodeValue: same, through a method expression's worth of dereference.
func NilNodeValue(n *Node) int { return n.Value() }

// NilIface calls a method on a nil interface value.
func NilIface(s Summer) int { return s.Sum() }

// NilEmbedded: a promoted method through a nil embedded pointer.
func NilEmbedded(name string) int {
	pn := PNamed{nil, name}
	return pn.Sum()
}

// NilEmbeddedIface: a promoted method of a nil embedded interface.
func NilEmbeddedIface() int {
	var l Logger
	return l.Sum()
}

// IfaceNilPtr: an interface holding a nil *Point; IsNil runs with a nil receiver.
func IfaceNilPtr() (bool, bool) {
	var p *Point
	var i interface{ IsNil() bool } = p
	return i == nil, i.IsNil()
}

// ---- large receivers ----

type Large struct {
	A    [30]int
	Name string
	F    float64
}

func (l Large) Total() int {
	t := 0
	for _, v := range l.A {
		t += v
	}
	l.A[0] = -1 // the copy
	return t + len(l.Name)
}

func (l *Large) Fill(v int) *Large {
	for i := range l.A {
		l.A[i] = v + i
	}
	return l
}

func (l Large) With(i, v int) Large { l.A[i] = v; return l }

type Totaler interface{ Total() int }

func LargeMethods(v int, name string) (int, int, int, int) {
	var l Large
	l.Name = name
	l.Fill(v)
	l2 := l.With(0, 1000)
	var t Totaler = l2
	var tp Totaler = &l
	return l.Total(), l2.Total(), t.Total(), tp.Total() + l.A[0]
}
