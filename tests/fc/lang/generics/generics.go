// Package generics exercises type parameters: generic functions and generic types.
// Imports: cmp (for the cmp.Ordered constraint only).
//
// A generic function instantiated from another package is compiled as part of that
// other package, so every feature here is reached through the non-generic exported
// wrappers in wrappers.go, which instantiate inside this package. The generic
// functions are exported too, so the test can instantiate them itself (with the
// stock compiler) to get the expected values.
package generics

import "cmp"

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64
}

type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

type IntOrString interface{ ~int | ~string }

type Stringer interface{ String() string }

type StringerInt interface {
	~int
	String() string
}

type MyInt int

func (m MyInt) String() string { return "MyInt(" + Itoa(int(m)) + ")" }

type MyStr string

type IDs []int

type Point struct{ X, Y int }

func (p Point) String() string { return "(" + Itoa(p.X) + "," + Itoa(p.Y) + ")" }
func (p *Point) Scale(k int)   { p.X *= k; p.Y *= k }

type Vec struct{ A, B float64 }

func (v *Vec) Scale(k int) { v.A *= float64(k); v.B *= float64(k) }

type Shape interface{ Area() float64 }

type Rect struct{ W, H float64 }

func (r Rect) Area() float64 { return r.W * r.H }

type Sq float64

func (s Sq) Area() float64 { return float64(s * s) }

func Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	var buf [24]byte
	i := len(buf)
	u := uint(n)
	if neg {
		u = -u
	}
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ---- generic functions ----

func Identity[T any](x T) T { return x }

func Swap[A, B any](a A, b B) (B, A) { return b, a }

func Sum[T Number](xs []T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func Dot[T Number](a, b []T) T {
	var s T
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func Filter[T any](xs []T, pred func(T) bool) []T {
	var out []T
	for _, x := range xs {
		if pred(x) {
			out = append(out, x)
		}
	}
	return out
}

func Reduce[T, A any](xs []T, init A, f func(A, T) A) A {
	acc := init
	for _, x := range xs {
		acc = f(acc, x)
	}
	return acc
}

func Index[T comparable](xs []T, x T) int {
	for i, y := range xs {
		if y == x {
			return i
		}
	}
	return -1
}

func Equal[T comparable](a, b T) bool { return a == b }

func Max[T cmp.Ordered](a, b T) T { return max(a, b) }

func Min3[T cmp.Ordered](a, b, c T) T { return min(a, b, c) }

func Clamp[T cmp.Ordered](x, lo, hi T) T { return min(max(x, lo), hi) }

func Less[T cmp.Ordered](a, b T) bool { return a < b }

func Zero[T any]() T {
	var z T
	return z
}

func IsZero[T comparable](x T) bool {
	var z T
	return x == z
}

func Double[T IntOrString](x T) T { return x + x }

func Join[T Stringer](xs []T, sep string) string {
	s := ""
	for i, x := range xs {
		if i > 0 {
			s += sep
		}
		s += x.String()
	}
	return s
}

func Describe[T StringerInt](x T) string { return x.String() + "=" + Itoa(int(x)+1) }

func Convert[T, U Number](x T) U { return U(x) }

func ShiftL[T Integer](x T, n uint) T { return x << n }
func ShiftR[T Integer](x T, n uint) T { return x >> n }
func Div[T Integer](a, b T) T         { return a / b }
func Mod[T Integer](a, b T) T         { return a % b }
func Neg[T Number](x T) T             { return -x }

func Kind[T any](x T) string {
	switch v := any(x).(type) {
	case nil:
		return "nil"
	case int:
		return "int:" + Itoa(v)
	case string:
		return "string:" + v
	case MyInt, MyStr:
		return "named"
	case Point:
		return "Point" + v.String()
	case *Point:
		if v == nil {
			return "*Point(nil)"
		}
		return "*Point" + v.String()
	case []int:
		return "[]int:" + Itoa(len(v))
	case Stringer:
		return "Stringer:" + v.String()
	case error:
		return "error:" + v.Error()
	}
	return "other"
}

func Ptr[T any](x T) *T   { return &x }
func Deref[T any](p *T) T { return *p }

func SliceOf[T any](xs ...T) []T { return xs }

func Len[S ~[]E, E any](s S) int { return len(s) }

func Reverse[S ~[]E, E any](s S) S {
	out := make(S, len(s))
	for i, x := range s {
		out[len(s)-1-i] = x
	}
	return out
}

func Sort[T cmp.Ordered](xs []T) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

func SortBy[T any](xs []T, less func(a, b T) bool) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && less(xs[j], xs[j-1]); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

func SortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	Sort(keys)
	return keys
}

func ApplyTwice[T any](f func(T) T, x T) T { return f(f(x)) }

func MakeAdder[T Number](k T) func(T) T { return func(x T) T { return x + k } }

func Accumulator[T Number]() func(T) T {
	var s T
	return func(x T) T {
		s += x
		return s
	}
}

func Memo[K comparable, V any](f func(K) V) (func(K) V, func() int) {
	m := map[K]V{}
	calls := 0
	get := func(k K) V {
		if v, ok := m[k]; ok {
			return v
		}
		calls++
		v := f(k)
		m[k] = v
		return v
	}
	return get, func() int { return calls }
}

func ScaleAll[T any, PT interface {
	*T
	Scale(int)
}](xs []T, k int) {
	for i := range xs {
		PT(&xs[i]).Scale(k)
	}
}

func TotalArea[S Shape](xs []S) float64 {
	t := 0.0
	for _, x := range xs {
		t += x.Area()
	}
	return t
}

// ---- generic types ----

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func MakePair[K comparable, V any](k K, v V) Pair[K, V] { return Pair[K, V]{k, v} }

func (p Pair[K, V]) Get() (K, V)    { return p.Key, p.Val }
func (p Pair[K, V]) KeyIs(k K) bool { return p.Key == k }
func (p *Pair[K, V]) Set(v V) V {
	old := p.Val
	p.Val = v
	return old
}

type node[T any] struct {
	val  T
	next *node[T]
}

type List[T any] struct {
	head, tail *node[T]
	n          int
}

func (l *List[T]) Push(v T) *List[T] {
	nd := &node[T]{val: v}
	if l.tail == nil {
		l.head = nd
	} else {
		l.tail.next = nd
	}
	l.tail = nd
	l.n++
	return l
}

func (l *List[T]) PushFront(v T) {
	l.head = &node[T]{v, l.head}
	if l.tail == nil {
		l.tail = l.head
	}
	l.n++
}

func (l *List[T]) Len() int { return l.n }

func (l *List[T]) Each(f func(int, T)) {
	i := 0
	for nd := l.head; nd != nil; nd = nd.next {
		f(i, nd.val)
		i++
	}
}

func (l *List[T]) Slice() []T {
	out := make([]T, 0, l.n)
	l.Each(func(_ int, v T) { out = append(out, v) })
	return out
}

func (l *List[T]) Reversed() *List[T] {
	r := &List[T]{}
	l.Each(func(_ int, v T) { r.PushFront(v) })
	return r
}

func ListMap[T, U any](l *List[T], f func(T) U) *List[U] {
	r := new(List[U])
	l.Each(func(_ int, v T) { r.Push(f(v)) })
	return r
}

type tnode[T cmp.Ordered] struct {
	val         T
	left, right *tnode[T]
}

type Tree[T cmp.Ordered] struct {
	root *tnode[T]
	n    int
}

func (t *Tree[T]) Insert(v T) bool {
	pp := &t.root
	for *pp != nil {
		switch {
		case v < (*pp).val:
			pp = &(*pp).left
		case v > (*pp).val:
			pp = &(*pp).right
		default:
			return false
		}
	}
	*pp = &tnode[T]{val: v}
	t.n++
	return true
}

func (t *Tree[T]) Has(v T) bool {
	for nd := t.root; nd != nil; {
		if v == nd.val {
			return true
		}
		if v < nd.val {
			nd = nd.left
		} else {
			nd = nd.right
		}
	}
	return false
}

func (t *Tree[T]) walk(nd *tnode[T], f func(T)) {
	if nd == nil {
		return
	}
	t.walk(nd.left, f)
	f(nd.val)
	t.walk(nd.right, f)
}

func (t *Tree[T]) Sorted() []T {
	out := make([]T, 0, t.n)
	t.walk(t.root, func(v T) { out = append(out, v) })
	return out
}

func height[T cmp.Ordered](nd *tnode[T]) int {
	if nd == nil {
		return 0
	}
	return 1 + max(height(nd.left), height(nd.right))
}

func (t *Tree[T]) Height() int { return height(t.root) }

func (t *Tree[T]) Min() (T, bool) {
	if t.root == nil {
		var z T
		return z, false
	}
	nd := t.root
	for nd.left != nil {
		nd = nd.left
	}
	return nd.val, true
}

type Stack[T any] []T

func (s *Stack[T]) Push(v T) { *s = append(*s, v) }
func (s *Stack[T]) Pop() (T, bool) {
	if len(*s) == 0 {
		var z T
		return z, false
	}
	v := (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
	return v, true
}

type Set[T comparable] map[T]struct{}

func (s Set[T]) Add(v T) bool {
	if _, ok := s[v]; ok {
		return false
	}
	s[v] = struct{}{}
	return true
}
func (s Set[T]) Has(v T) bool { _, ok := s[v]; return ok }
func (s Set[T]) Len() int     { return len(s) }

// Named embeds a generic type; List's methods are promoted.
type Named[T any] struct {
	List[T]
	Name string
}

func (n *Named[T]) Label() string { return n.Name + "#" + Itoa(n.Len()) }

type Num[T Number] struct{ V T }

func (n Num[T]) Add(o Num[T]) Num[T]            { return Num[T]{n.V + o.V} }
func (n Num[T]) Scale(k T) Num[T]               { return Num[T]{n.V * k} }
func (n Num[T]) AddScaled(o Num[T], k T) Num[T] { return n.Add(o.Scale(k)) }

type Opt[T any] struct {
	v  T
	ok bool
}

func Some[T any](v T) Opt[T] { return Opt[T]{v, true} }
func None[T any]() Opt[T]    { return Opt[T]{} }
func (o Opt[T]) Or(d T) T {
	if o.ok {
		return o.v
	}
	return d
}
