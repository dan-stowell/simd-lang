// Package slices_: slices and arrays.  make/len/cap, append, copy, slicing with every bounds panic, nil
// against empty, value semantics of arrays, conversions of slices to arrays, clear.  No imports.
package slices_

type Node struct {
	ID   int
	Name string
	Next *Node
	Vals []int
}

// ---- make, len, cap

func Make(n int) []int               { return make([]int, n) }
func MakeCap(n, c int) []int         { return make([]int, n, c) }
func MakeI64(n, c int64) []byte      { return make([]byte, n, c) }
func MakeU8(n uint8) []string        { return make([]string, n) }
func MakeLenCap(n, c int) (int, int) { s := make([]uint16, n, c); return len(s), cap(s) }
func MakeNodes(n int) []Node         { return make([]Node, n) }
func MakeBig(n int) int              { s := make([][64]int64, n); return len(s) * len(s[0]) }
func Len(s []int) int                { return len(s) }
func Cap(s []int) int                { return cap(s) }
func IsNil(s []int) bool             { return s == nil }
func LenNilEmpty() (a, b int, c, d bool) {
	var n []int
	e := []int{}
	return len(n), cap(e), n == nil, e == nil
}
func LenArray(a *[7]int) (int, int) { return len(a), cap(a) }
func NilSliceOps() (int, int, []int, int) {
	var s []int
	n := 0
	for range s {
		n++
	}
	return len(s[:]), len(s[0:0]), append(s, s...), n + copy(s, s)
}

// ---- indexing

func At(s []int, i int) int              { return s[i] }
func AtU8(s []int, i uint8) int          { return s[i] }
func AtI32(s []int, i int32) int         { return s[i] }
func AtU64(s []byte, i uint64) byte      { return s[i] }
func Set(s []int, i, v int)              { s[i] = v }
func SetStr(s []string, i int, v string) { s[i] = v }
func AddAt(s []int, i, v int)            { s[i] += v }
func IncAt(s []int8, i int)              { s[i]++ }
func ArrAt(a [5]int, i int) int          { return a[i] }
func ArrPtrAt(a *[5]int, i int) int      { return a[i] }
func ArrPtrSet(a *[5]int, i, v int)      { a[i] = v }
func ArrAtU8(a [256]byte, i uint8) byte  { return a[i] }
func At2(s [][]int, i, j int) int        { return s[i][j] }
func Swap(s []string, i, j int)          { s[i], s[j] = s[j], s[i] }
func Last(s []int) int                   { return s[len(s)-1] }
func NodeName(s []Node, i int) string    { return s[i].Name }
func NodeNext(s []Node, i int) int       { return s[i].Next.ID }
func NodeVal(s []*Node, i, j int) int    { return s[i].Vals[j] }

// ---- slicing, two and three indices

func Slice(s []int, i, j int) []int         { return s[i:j] }
func SliceFrom(s []int, i int) []int        { return s[i:] }
func SliceTo(s []int, j int) []int          { return s[:j] }
func SliceAll(s []int) []int                { return s[:] }
func Slice3(s []int, i, j, k int) []int     { return s[i:j:k] }
func Slice3To(s []int, j, k int) []int      { return s[:j:k] }
func SliceU(s []string, i, j uint) []string { return s[i:j] }
func SliceCap(s []int, i, j int) (int, int) {
	t := s[i:j]
	return len(t), cap(t)
}
func Slice3Cap(s []int, i, j, k int) (int, int) {
	t := s[i:j:k]
	return len(t), cap(t)
}
func Reslice(s []int, j int) []int           { return s[:j] } // j may exceed len(s), up to cap(s)
func SliceArr(a *[5]int, i, j int) []int     { return a[i:j] }
func SliceArr3(a *[5]int, i, j, k int) []int { return a[i:j:k] }
func SliceArrLocal(i, j int) []int {
	a := [6]int{10, 20, 30, 40, 50, 60}
	return a[i:j]
}
func SliceNilPtr(a *[5]int) []int          { return a[:] }
func SliceBytes(s []byte, i, j int) []byte { return s[i:j] }
func SliceNodes(s []Node, i, j int) []Node { return s[i:j] }
func SliceAlias(s []int, i, j int) int {
	t := s[i:j]
	t[0] = 99
	return s[i]
}
func SliceEmptyAtEnd(s []int) ([]int, bool) {
	t := s[len(s):]
	return t, t == nil
}

// ---- append

func Append1(s []int, v int) []int                  { return append(s, v) }
func Append3(s []int, a, b, c int) []int            { return append(s, a, b, c) }
func AppendSlice(s, t []int) []int                  { return append(s, t...) }
func AppendSelf(s []int) []int                      { return append(s, s...) }
func AppendSelfTail(s []int, i int) []int           { return append(s[:i], s...) }
func AppendNil(s []int) []int                       { return append(s, nil...) }
func AppendStr(b []byte, s string) []byte           { return append(b, s...) }
func AppendStrs(s []string, a, b string) []string   { return append(s, a, b+a) }
func AppendNode(s []Node, n Node) []Node            { return append(s, n) }
func AppendPtr(s []*Node, n *Node) []*Node          { return append(s, n, n) }
func AppendByte(b []byte, c byte) []byte            { return append(b, c, c+1) }
func AppendArr(s [][4]int32, a [4]int32) [][4]int32 { return append(s, a) }
func AppendIface(s []any, a int, b string) []any    { return append(s, a, b, nil, s) }
func AppendN(n int) (s []int, grows int) {
	c := 0
	for i := 0; i < n; i++ {
		s = append(s, i*i)
		if cap(s) != c {
			grows++
			c = cap(s)
		}
	}
	return
}
func AppendCaps(n int) (caps []int) {
	var s []int64
	for i := 0; i < n; i++ {
		s = append(s, 1)
		if len(caps) == 0 || caps[len(caps)-1] != cap(s) {
			caps = append(caps, cap(s))
		}
	}
	return
}
func AppendCapsBytes(n int) (caps []int) {
	var s []byte
	for i := 0; i < n; i++ {
		s = append(s, 1)
		if len(caps) == 0 || caps[len(caps)-1] != cap(s) {
			caps = append(caps, cap(s))
		}
	}
	return
}
func AppendCapOf(n, extra int) int {
	s := make([]int32, n)
	t := make([]int32, extra)
	return cap(append(s, t...))
}
func AppendShares(s []int) (bool, int) {
	// appending within capacity writes into the same array; beyond it, a new one
	t := append(s[:len(s):len(s)], 1)
	u := append(s[:0], 7)
	return len(s) > 0 && &t[0] == &s[0], u[0] + len(u)
}
func AppendInPlace(s []int, v int) int {
	t := append(s[:1], v)
	return s[1] + t[1]
}
func Insert(s []int, i, v int) []int {
	s = append(s, 0)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}
func Delete(s []int, i int) []int { return append(s[:i], s[i+1:]...) }
func Filter(s []int, mod int) []int {
	out := s[:0]
	for _, x := range s {
		if x%mod != 0 {
			out = append(out, x)
		}
	}
	return out
}
func Variadic(xs ...int) (n, sum int) {
	for _, x := range xs {
		sum += x
	}
	return len(xs), sum
}
func VariadicIsNil(xs ...string) bool { return xs == nil }
func CallVariadic(s []int, a, b int) (int, int, int, int, bool, bool) {
	n0, _ := Variadic()
	n1, s1 := Variadic(a)
	_, s2 := Variadic(a, b, a)
	n3, s3 := Variadic(s...)
	_ = n1 + n3
	return n0, s1, s2, s3, VariadicIsNil(), VariadicIsNil(nil...)
}
func VariadicAlias(s []int) int {
	bump(s...)
	return s[0]
}
func bump(xs ...int) { xs[0]++ }

// ---- copy

func Copy(dst, src []int) int            { return copy(dst, src) }
func CopyStr(dst []byte, s string) int   { return copy(dst, s) }
func CopyOverlapUp(s []int, n int) int   { return copy(s[n:], s) }
func CopyOverlapDown(s []int, n int) int { return copy(s, s[n:]) }
func CopyNodes(dst, src []Node) int      { return copy(dst, src) }
func CopyPtrs(dst, src []*Node) int      { return copy(dst, src) }
func CopyStrs(dst, src []string) int     { return copy(dst, src) }
func CopyArr(dst *[5]int, src []int) int { return copy(dst[:], src) }
func Clone(s []int) []int {
	if s == nil {
		return nil
	}
	c := make([]int, len(s))
	copy(c, s)
	return c
}
func CloneAppend(s []int) []int { return append([]int(nil), s...) }

// ---- clear

func Clear(s []int)                  { clear(s) }
func ClearNodes(s []Node)            { clear(s) }
func ClearPart(s []string, i, j int) { clear(s[i:j]) }
func ClearMap(m map[string]int) int {
	clear(m)
	return len(m)
}

// ---- range

func Sum(s []int) (t int) {
	for _, x := range s {
		t += x
	}
	return
}
func SumIdx(s []int) (t int) {
	for i := range s {
		t += i * s[i]
	}
	return
}
func RangeCopyOfLen(s []int) (n int) {
	for i := range s {
		s = append(s, i) // the range saw the old length
		n++
	}
	return n + len(s)
}
func RangeArrayByValue(a [4]int) (sum int) {
	for i, x := range a { // ranges over a copy
		a[3] = 100
		sum += x + i
	}
	return sum + a[3]
}
func RangeArrayPtr(a *[4]int) (sum int) {
	for _, x := range a { // no copy
		a[3] = 100
		sum += x
	}
	return
}
func RangeSliceSeesWrites(s []int) (sum int) {
	for i, x := range s {
		if i+1 < len(s) {
			s[i+1] += 1000
		}
		sum += x
	}
	return
}
func RangeNodes(s []Node) (names string, total int) {
	for _, n := range s {
		n.ID = -1 // a copy
		names += n.Name
		for _, v := range n.Vals {
			total += v
		}
	}
	return
}
func RangeModify(s []Node) {
	for i := range s {
		s[i].ID *= 2
		s[i].Name += "!"
	}
}

// ---- arrays: value semantics

func ArrAssign(a [4]int) ([4]int, [4]int) {
	b := a
	b[0] = -1
	return a, b
}
func ArrParam(a [4]int) int { a[1] = 50; return a[0] + a[1] }
func ArrCall(a [4]int) (int, [4]int) {
	n := ArrParam(a)
	return n, a
}
func ArrReturn(x int) [4]int                      { return [4]int{x, x + 1, x + 2, x + 3} }
func ArrEq(a, b [4]int) bool                      { return a == b }
func ArrNe(a, b [3]string) bool                   { return a != b }
func ArrEqF(a, b [2]float64) bool                 { return a == b }
func ArrEqBig(a, b [100]int32) bool               { return a == b }
func ArrEqNodes(a, b [2]Pt) bool                  { return a == b }
func ArrZero() (a [3]int, s [2]string, p [2]*int) { return }
func ArrLit(x int) [5]int                         { return [...]int{4: x, 1: x * 2} }
func ArrOfArr(x int) [2][3]int                    { return [2][3]int{{x, x, x}, {1: x + 1}} }
func ArrInStruct(x int) (Box, Box) {
	b := Box{A: [3]int{x, x, x}}
	c := b
	c.A[1] = 0
	return b, c
}
func ArrPtrShare(a *[4]int) int {
	p := a
	p[2] = 77
	return a[2]
}
func ArrDeref(a *[4]int) [4]int { b := *a; b[0]++; return b }
func ArrBig(x int64) (sum int64) {
	var a [1000]int64
	for i := range a {
		a[i] = x + int64(i)
	}
	b := a
	b[999] = 0
	for _, v := range b {
		sum += v
	}
	return sum + a[999]
}
func ArrSwap(a, b [3]string) ([3]string, [3]string) {
	a, b = b, a
	return a, b
}
func ArrElemSwap(a [4]int, i, j int) [4]int {
	a[i], a[j] = a[j], a[i]
	return a
}
func Matrix(n int) (tr int) {
	var m [8][8]int
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			m[i][j] = i*n + j
		}
	}
	for i := range m {
		tr += m[i][i]
	}
	return
}

type Pt struct{ X, Y int }
type Box struct {
	A [3]int
	N string
}

// ---- slice to array conversions

func ToArray(s []int) [4]int     { return [4]int(s) }
func ToArrayPtr(s []int) *[4]int { return (*[4]int)(s) }
func ToArray0(s []int) [0]int    { return [0]int(s) }
func ToArrayPtr0(s []int) bool   { return (*[0]int)(s) == nil }
func ToArrayAlias(s []int) int {
	p := (*[2]int)(s)
	p[1] = 55
	return s[1]
}
func ToArrayBytes(s []byte) [8]byte { return [8]byte(s) }

// ---- slices of slices, of pointers; growth with pointers inside

func Grid(n, m int) [][]int {
	g := make([][]int, n)
	for i := range g {
		g[i] = make([]int, m)
		for j := range g[i] {
			g[i][j] = i*10 + j
		}
	}
	return g
}
func Triangle(n int) (out [][]int) {
	for i := 0; i < n; i++ {
		var row []int
		for j := 0; j <= i; j++ {
			row = append(row, i-j)
		}
		out = append(out, row)
	}
	return
}
func Chain(n int) []*Node {
	var out []*Node
	var prev *Node
	for i := 0; i < n; i++ {
		nd := &Node{ID: i, Name: string(rune('a' + i%26)), Next: prev}
		nd.Vals = append(nd.Vals, i, i+1)
		out = append(out, nd)
		prev = nd
	}
	return out
}
func ChainSum(s []*Node) (sum int) {
	if len(s) == 0 {
		return 0
	}
	for n := s[len(s)-1]; n != nil; n = n.Next {
		sum += n.ID + n.Vals[1]
	}
	return
}
func NodesByValue(n int) []Node {
	var out []Node
	for i := 0; i < n; i++ {
		out = append(out, Node{ID: i, Name: "n", Vals: []int{i}})
		if i > 0 {
			out[i].Next = &out[i-1] // into the current backing array: stale once it grows
		}
	}
	return out
}
func Flatten(g [][]int) (out []int) {
	for _, r := range g {
		out = append(out, r...)
	}
	return
}
func Reverse(s []int) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
func Sort(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
func BinSearch(s []int, x int) int {
	lo, hi := 0, len(s)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if s[m] < x {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}
func Stack(ops []int) (out []int) {
	var st []int
	for _, op := range ops {
		if op >= 0 {
			st = append(st, op)
		} else if len(st) > 0 {
			out = append(out, st[len(st)-1])
			st = st[:len(st)-1]
		}
	}
	return append(out, st...)
}
func Sieve(n int) (primes []int) {
	comp := make([]bool, n+1)
	for i := 2; i <= n; i++ {
		if !comp[i] {
			primes = append(primes, i)
			for j := i * i; j <= n; j += i {
				comp[j] = true
			}
		}
	}
	return
}
