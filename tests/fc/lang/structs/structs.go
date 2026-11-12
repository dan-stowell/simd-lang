// Package structs: field access through pointers and embedding, copies and comparisons, composite literals,
// zero values, large structs and large field offsets, nil dereferences, parameters and results of every
// size (in registers, on the stack, mixed).  No imports.
package structs

// ---- shapes

type P2 struct{ X, Y int }
type P3 struct{ X, Y, Z int32 }
type Small struct { // 16 bytes, mixed
	A int8
	B uint16
	C int32
	D float64
}
type Padded struct {
	A byte
	B int64
	C byte
	D int16
	E byte
}
type WithStr struct {
	N int
	S string
	F float64
}
type WithBlank struct {
	A int
	_ int
	B string
	_ [2]float64
	C int8
}
type F4 struct{ A, B, C, D float64 }
type F5 struct{ A, B, C, D, E float64 }
type I4 struct{ A, B, C, D int64 }
type I5 struct{ A, B, C, D, E int64 }
type Mixed struct {
	I int64
	F float64
	S string
	P *int
	B bool
	G float32
}
type WithArr struct {
	A [2]int // arrays of more than one element are never passed in registers
	B int
}
type WithArr1 struct {
	A [1]int
	B [1]string
}
type Empty struct{}
type HasEmpty struct {
	A int
	E Empty
	B int
	Z Empty
}
type Big struct { // 200 bytes
	H    int
	Body [23]int64
	T    string
}
type Huge struct { // field offsets beyond a page
	Pad0 [4096]byte
	A    int64
	Pad1 [70000]byte
	B    int32
	S    string
	P    *Huge
}

// ---- embedding

type Base struct {
	ID   int
	Name string
}
type Mid struct {
	Base
	Level int
}
type Top struct {
	Mid
	Extra string
	ID    int64 // shadows Mid.Base.ID
}
type PtrEmb struct {
	*Base
	N int
}
type PtrEmb2 struct {
	*PtrEmb
	M int
}
type TwoEmb struct {
	P2
	Small
	WithStr
}

// ---- field access

func GetX(p P2) int              { return p.X }
func GetY(p *P2) int             { return p.Y }
func SetY(p *P2, v int)          { p.Y = v }
func AddXY(p *P2) int            { p.X += p.Y; p.Y++; return p.X }
func SmallSum(s Small) float64   { return float64(s.A) + float64(s.B) + float64(s.C) + s.D }
func SmallSet(s *Small, x int64) { s.A = int8(x); s.B = uint16(x); s.C = int32(x); s.D = float64(x) }
func PaddedSum(p *Padded) int64  { return int64(p.A) + p.B + int64(p.C) + int64(p.D) + int64(p.E) }
func MakePadded(x int64) Padded {
	return Padded{byte(x), x, byte(x >> 8), int16(x >> 16), byte(x >> 32)}
}
func BaseID(t *Top) int    { return t.Base.ID }
func TopID(t *Top) int64   { return t.ID }
func MidName(t Top) string { return t.Name }
func TopLevel(t *Top) int  { return t.Level }
func SetDeep(t *Top, id int, n string) {
	t.Mid.Base.ID = id
	t.Name = n
	t.ID = int64(id) * 2
	t.Level++
}
func PtrEmbID(p PtrEmb) int        { return p.ID }
func PtrEmbName(p *PtrEmb2) string { return p.Name }
func PtrEmbSet(p *PtrEmb2, n string, v int) {
	p.Name = n
	p.ID = v
	p.N = v + 1
	p.M = v + 2
}
func TwoEmbSum(t *TwoEmb) float64 {
	return float64(t.X+t.Y+t.N) + t.D + t.F + float64(t.A) + float64(len(t.S))
}
func FieldPtr(p *P2) *int { return &p.Y }
func FieldPtrWrite(p *P2, v int) int {
	q := &p.X
	*q = v
	return p.X + *FieldPtr(p)
}
func NestedPtr(t *Top) *string { return &t.Mid.Base.Name }
func ArrayField(w *WithArr, i int) int {
	w.A[i] += w.B
	return w.A[0] + w.A[1]
}
func StructInArray(a *[3]P2, i int) int { a[i].X = a[i].Y * 2; return a[i].X }
func StructInSlice(s []P2, i int) int   { s[i].X++; return s[i].X + s[i].Y }
func AnonStruct(x int, s string) (int, string) {
	v := struct {
		N int
		S string
	}{x, s}
	p := &v
	p.N *= 2
	w := v
	w.S += "!"
	return v.N, w.S + v.S
}
func LocalType(x int) int {
	type pair struct{ a, b int }
	p := pair{x, x * 2}
	q := &pair{b: 5}
	q.a = p.b
	return p.a + q.a + q.b
}

// ---- copies

func Copy(p P2) (P2, P2) {
	q := p
	q.X = -1
	return p, q
}
func CopyViaPtr(p *P2) P2 {
	q := *p
	p.X = 100
	return q
}
func AssignThrough(dst, src *WithStr) { *dst = *src }
func CopyBig(b *Big) Big {
	c := *b
	c.Body[22] = -1
	c.T += "?"
	return c
}
func CopyMixed(m Mixed) Mixed {
	n := m
	n.I++
	n.S += n.S
	return n
}
func SwapStructs(a, b *Mixed) { *a, *b = *b, *a }
func SelfAssign(p *Big) int {
	*p = *p
	return p.H
}
func CopyInLoop(s []WithStr) (out []WithStr) {
	for _, w := range s {
		w.N++
		out = append(out, w)
	}
	return
}
func CopyHuge(h *Huge) (int64, int32, string) {
	c := new(Huge)
	*c = *h
	c.A++
	return c.A + h.A, c.B, c.S
}

// ---- comparison

func EqP2(a, b P2) bool               { return a == b }
func NeP3(a, b P3) bool               { return a != b }
func EqSmall(a, b Small) bool         { return a == b }
func EqPadded(a, b Padded) bool       { return a == b }
func EqWithStr(a, b WithStr) bool     { return a == b }
func EqWithBlank(a, b WithBlank) bool { return a == b }
func EqF4(a, b F4) bool               { return a == b }
func EqMixed(a, b Mixed) bool         { return a == b }
func EqBig(a, b *Big) bool            { return *a == *b }
func EqTop(a, b Top) bool             { return a == b }
func EqEmpty(a, b Empty) bool         { return a == b }
func EqHasEmpty(a, b HasEmpty) bool   { return a == b }
func EqWithArr(a, b WithArr) bool     { return a == b }
func EqAnon(a, b struct {
	X int
	S string
}) bool {
	return a == b
}
func EqPtrs(a, b *P2) (bool, bool) { return a == b, a != nil && b != nil && *a == *b }
func EqLit(p P2) bool              { return p == P2{1, 2} }
func EqZero(w WithStr) bool        { return w == WithStr{} }
func EqBlank(x int) bool {
	a := WithBlank{A: x, B: "b", C: 2}
	b := WithBlank{C: 2, B: "b"}
	b.A = x
	return a == b
}
func PadGarbage(x int64) bool {
	// two values equal field by field, built differently
	a := MakePadded(x)
	var b Padded
	b.E, b.D, b.C, b.B, b.A = byte(x>>32), int16(x>>16), byte(x>>8), x, byte(x)
	return a == b
}

// ---- composite literals and zero values

func LitKeyed(x int, s string) WithStr   { return WithStr{S: s, N: x} }
func LitUnkeyed(x int, s string) WithStr { return WithStr{x, s, 0.5} }
func LitPtr(x int) *P2                   { return &P2{x, -x} }
func LitNested(x int, s string) Top {
	return Top{Mid: Mid{Base: Base{ID: x, Name: s}, Level: 3}, Extra: s + s, ID: int64(x) << 32}
}
func LitNestedPtr(x int) *PtrEmb2 { return &PtrEmb2{&PtrEmb{&Base{x, "b"}, x + 1}, x + 2} }
func LitSlice(x int) []P2         { return []P2{{x, 1}, {Y: x}, {}} }
func LitSlicePtr(x int) []*P2     { return []*P2{{x, 1}, {Y: x}, nil, {}} }
func LitArray(x int) [3]Small     { return [3]Small{{A: int8(x)}, 2: {D: float64(x)}} }
func LitMixed(x int64, p *int) Mixed {
	return Mixed{I: x, F: float64(x) / 4, S: "m", P: p, B: x > 0, G: float32(x)}
}
func LitBig(x int64) Big {
	return Big{H: 1, Body: [23]int64{0: x, 22: -x, 11: x * 2}, T: "tail"}
}
func Zero() (P2, Small, WithStr, Mixed, Top, *P2) {
	var a P2
	var b Small
	var c WithStr
	var d Mixed
	var e Top
	var f *P2
	return a, b, c, d, e, f
}
func ZeroBig() Big      { return Big{} }
func NewZero() *WithStr { return new(WithStr) }
func ResetTo(p *Mixed)  { *p = Mixed{} }
func ZeroHuge() (int64, int32, int, bool) {
	h := new(Huge)
	return h.A, h.B, len(h.S) + int(h.Pad1[69999]), h.P == nil
}

// ---- large offsets, nil dereferences

func HugeGet(h *Huge) (int64, int32, string) { return h.A, h.B, h.S }
func HugeSet(h *Huge, a int64, b int32, s string) {
	h.A, h.B, h.S = a, b, s
	h.Pad1[len(h.Pad1)-1] = byte(a)
	h.P = h
}
func HugeA(h *Huge) int64                        { return h.A } // offset 4096: needs an explicit nil check
func HugeB(h *Huge) int32                        { return h.B } // offset > 64 KiB
func HugeSetB(h *Huge, v int32)                  { h.B = v }
func HugeAddrB(h *Huge) *int32                   { return &h.B } // nil check without a load
func HugePad(h *Huge, i int) byte                { return h.Pad1[i] }
func HugeChain(h *Huge) int64                    { return h.P.P.A }
func NilX(p *P2) int                             { return p.X }
func NilSetY(p *P2)                              { p.Y = 1 }
func NilCopy(p *Big) Big                         { return *p }
func NilDeep(t *Top) string                      { return t.Mid.Base.Name }
func NilEmbPtr(p PtrEmb) int                     { return p.ID } // p.Base may be nil
func NilAddr(p *P2) *int                         { return &p.Y } // &p.Y with p nil panics
func NilDiscard(p *P2)                           { _ = *p }
func NilBigField(b *Big) string                  { return b.T }
func NilArrayElem(a *[100000]int64, i int) int64 { return a[i] }
func NilMethodless(pp **P2) int                  { return (*pp).X }

// ---- parameters and results

func Ret2(x int) P2                        { return P2{x, x + 1} }
func RetSmall(x int64) Small               { return Small{int8(x), uint16(x), int32(x), float64(x)} }
func RetF4(x float64) F4                   { return F4{x, x + 1, x + 2, x + 3} }
func RetF5(x float64) F5                   { return F5{x, x + 1, x + 2, x + 3, x + 4} }
func RetI4(x int64) I4                     { return I4{x, x + 1, x + 2, x + 3} }
func RetI5(x int64) I5                     { return I5{x, x + 1, x + 2, x + 3, x + 4} }
func RetMixed(x int64, p *int) Mixed       { return LitMixed(x, p) }
func RetWithArr(x int) WithArr             { return WithArr{[2]int{x, x * 2}, x * 3} }
func RetWithArr1(x int, s string) WithArr1 { return WithArr1{[1]int{x}, [1]string{s}} }
func RetEmpty() Empty                      { return Empty{} }
func RetHasEmpty(x int) HasEmpty           { return HasEmpty{A: x, B: -x} }
func RetBig(x int64) Big                   { return LitBig(x) }
func RetTwo(x int) (P2, WithStr)           { return P2{x, x}, WithStr{x, "w", 1} }
func RetMany(x int) (a int, p P2, s string, f float64, q Small, ok bool, e *int) {
	return x, P2{x, x + 1}, "s", float64(x) / 2, RetSmall(int64(x)), x > 0, nil
}
func TakeI4(a I4) int64                   { return a.A + 2*a.B + 3*a.C + 4*a.D }
func TakeI5(a I5) int64                   { return a.A + 2*a.B + 3*a.C + 4*a.D + 5*a.E }
func TakeF4(a F4) float64                 { return a.A + 2*a.B + 3*a.C + 4*a.D }
func TakeMixed(m Mixed) float64           { return float64(m.I) + m.F + float64(len(m.S)) + float64(m.G) }
func TakeEmpty(a int, e Empty, b int) int { return a - b }
func TakeWithArr(w WithArr, x int) int    { return w.A[0] + w.A[1] + w.B + x }
func TakeBig(b Big, x int64) int64 {
	b.Body[0] += x
	return b.Body[0] + b.Body[22] + int64(b.H) + int64(len(b.T))
}
func TakeStructs(a P2, b Small, c WithStr, d P3, e F4, f I4) float64 {
	return float64(a.X+a.Y) + SmallSum(b) + float64(c.N) + c.F + float64(len(c.S)) + float64(d.X+d.Y+d.Z) + TakeF4(e) + float64(TakeI4(f))
}
func Ints10(a, b, c, d, e, f, g, h, i, j int) int {
	return a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g + 8*h + 9*i + 10*j
}
func Ints12(a int8, b int16, c int32, d int64, e uint8, f uint16, g uint32, h uint64, i int, j uint, k int8, l int64) int64 {
	return int64(a) + int64(b)*2 + int64(c)*3 + d*4 + int64(e)*5 + int64(f)*6 + int64(g)*7 + int64(h)*8 + int64(i)*9 + int64(j)*10 + int64(k)*11 + l*12
}
func Floats16(a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, p float64) float64 {
	return a + 2*b + 3*c + 4*d + 5*e + 6*f + 7*g + 8*h + 9*i + 10*j + 11*k + 12*l + 13*m + 14*n + 15*o + 16*p
}
func MixOverflow(a int, x float64, s string, b []int, p P2, c int, y float32, q I4, d int8, z float64, t string, e, f, g int) float64 {
	return float64(a+len(s)+len(b)+p.X+p.Y+c+int(d)+len(t)+e+f+g) + x + float64(y) + z + float64(TakeI4(q))
}
func CallAll(x int) float64 {
	r := float64(Ints10(x, x+1, x+2, x+3, x+4, x+5, x+6, x+7, x+8, x+9))
	r += Floats16(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, float64(x))
	r += MixOverflow(x, 0.5, "abc", []int{1, 2}, P2{x, 1}, 3, 1.5, RetI4(int64(x)), -4, 2.5, "t", 5, 6, 7)
	r += TakeStructs(Ret2(x), RetSmall(int64(x)), LitKeyed(x, "k"), P3{1, 2, 3}, RetF4(float64(x)), RetI4(2))
	r += float64(TakeI5(RetI5(int64(x)))) + float64(TakeBig(RetBig(int64(x)), 9))
	a, p, s, f, q, ok, e := RetMany(x)
	if ok && e == nil {
		r += float64(a+p.X+p.Y+len(s)) + f + SmallSum(q)
	}
	return r
}
func ResultsNamed(x int) (p P2, w WithStr) {
	p.X = x
	w.S = "named"
	p.Y = p.X * 2
	w.N = len(w.S)
	return
}
func ChainCalls(x int) int { return Ret2(Ret2(x).Y).X + GetX(Ret2(x+5)) + GetY(LitPtr(x)) }

// ---- a linked structure through struct pointers

type List struct {
	Val  int
	Next *List
}

func Build(n int) *List {
	var head *List
	for i := n; i > 0; i-- {
		head = &List{i, head}
	}
	return head
}
func ListSum(l *List) (s int) {
	for ; l != nil; l = l.Next {
		s += l.Val
	}
	return
}
func ListReverse(l *List) *List {
	var prev *List
	for l != nil {
		l.Next, prev, l = prev, l, l.Next
	}
	return prev
}
func ListNth(l *List, n int) int {
	for ; n > 0; n-- {
		l = l.Next
	}
	return l.Val
}
