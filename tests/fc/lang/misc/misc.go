// Package misc collects corner cases: labels and goto, shadowing, the blank identifier,
// constant expressions, iota, bit twiddling, print/println, variadic functions.
// Range-over-func iterators are in iter.go.
// Imports: none.
package misc

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [24]byte
	i := len(buf)
	u := uint(n)
	if n < 0 {
		u = -u
	}
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	if n < 0 {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ---- labels and goto ----

func GotoLoop(n int) int {
	i, s := 0, 0
loop:
	if i < n {
		s += i
		i++
		goto loop
	}
	return s
}

func GotoSkip(x int) string {
	if x < 0 {
		goto neg
	}
	if x == 0 {
		goto zero
	}
	return "pos"
neg:
	return "neg"
zero:
	return "zero"
}

func GotoFound(xs []int, target int) int {
	idx := 0
	for i, x := range xs {
		if x == target {
			idx = i
			goto found
		}
	}
	return -1
found:
	return idx * 2
}

// GotoMachine is a small state machine written with labels only.
func GotoMachine(s string) (words, digits int) {
	i := 0
start:
	if i >= len(s) {
		goto done
	}
	if s[i] == ' ' {
		i++
		goto start
	}
	words++
word:
	if i >= len(s) {
		goto done
	}
	if s[i] == ' ' {
		goto start
	}
	if s[i] >= '0' && s[i] <= '9' {
		digits++
	}
	i++
	goto word
done:
	return
}

func LabeledSwitch(x, y int) int {
	r := 0
outer:
	switch {
	case x > 0:
		for i := 0; i < 10; i++ {
			if i == y {
				break outer
			}
			r++
		}
		r += 100
	default:
		r = -1
	}
	return r
}

func LabeledLoops(n int) (pairs int) {
outer:
	for i := 0; i < n; i++ {
	inner:
		for j := 0; j < n; j++ {
			switch {
			case j > i:
				continue outer
			case i+j > n:
				break outer
			case j == 2:
				continue inner
			case i == 5 && j == 4:
				break inner
			}
			pairs++
		}
		pairs += 1000
	}
	return
}

func LabeledSelect(ch chan int, limit int) (sum int) {
loop:
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				break loop
			}
			if v > limit {
				continue loop
			}
			sum += v
		}
	}
	return
}

// ---- shadowing ----

var level = 1

func Shadow(x int) (int, int) {
	y := x
	{
		x := x * 2
		y += x
		{
			x := "str"
			y += len(x)
		}
		x++
		y += x
	}
	if x := x + 100; x > 105 {
		y += x
	} else if x := x * 3; x > 0 {
		y += x
	} else {
		y -= x
	}
	for i := 0; i < 3; i++ {
		i := i * 10
		y += i
	}
	level := level + x
	return x, y + level
}

func ShadowBuiltins(n int) int {
	len := func(s string) int { return 42 }
	true := false
	nil := 5
	int := n
	if true {
		return -1
	}
	return len("abc") + nil + int
}

func ShadowResult(x int) (r int, err string) {
	{
		r := x * 2
		err := "inner"
		_, _ = r, err
	}
	if x > 0 {
		r, err := x+1, "if"
		_, _ = r, err
	}
	r++
	return
}

func ShadowParamInClosure(x int) int {
	f := func(x int) int {
		g := func() int { x := x + 1; return x }
		return g() * 2
	}
	return f(x+1) + x
}

func ShadowSwitch(v any) int {
	switch v := v.(type) {
	case int:
		v := v + 1
		return v
	case string:
		return len(v)
	}
	return -1
}

// ---- blank identifier ----

var blankCount int

func bump() int { blankCount++; return blankCount }

var _ = bump()
var _, kept = bump(), bump()

type blankT struct {
	_ int32
	A int8
	_ [3]byte
	B int64
}

func (_ blankT) Sum() int      { return 0 }
func (t blankT) Get() int      { return int(t.A) + int(t.B) }
func two() (int, int)          { return bump(), 7 }
func BlankInit() (int, int)    { return kept, blankCount }
func BlankParam(_ int, b int, _ string) int { return b }

type getter interface{ Get() int }

var _ getter = blankT{}
var _ = [...]int{1, 2, 3}

func Blank(xs []int) (int, int) {
	before := blankCount
	_, b := two()
	_ = bump()
	c := 0
	for range xs {
		c++
	}
	for _ = range xs {
		c++
	}
	for _, _ = range xs {
		c++
	}
	var _ = bump()
	_, _ = xs[0], bump() // the index expression is still evaluated
	t := blankT{A: 1, B: 2}
	u := blankT{A: 1, B: 2}
	if t != u {
		return -1, -1
	}
	return b + c + t.Get() + t.Sum(), blankCount - before
}

// ---- constant expressions ----

const (
	big    = 1 << 200
	bigA   = big >> 190
	bigB   = big / (1 << 198)
	bigC   = big % 1000007
	bigD   = (big - 1) >> 136 & 0xffffffffffffffff
	fHuge  = 1e400 / 1e399
	exact  = 0.1+0.2 == 0.3 // exact in constant arithmetic
	third  = 1 / 3.0 * 3
	shiftF = 1.0 << 3
	runeK  = 'a' + 1
	notZ   = ^0
	negDiv = -7 / 2
	negMod = -7 % 2
	mask64 = 1<<64 - 1
	hello  = "héllo"
	helloN = len(hello)
)

func BigConsts() []int {
	return []int{bigA, bigB, bigC, shiftF, runeK, notZ, negDiv, negMod, helloN, int(hello[1]), int(uint8(^uint8(0)))}
}
func BigUint() uint64             { return bigD }
func ConstBools() []bool          { return []bool{exact, third == 1, fHuge == 10, big > mask64, 'a' < "b"[0]} }
func ConstFloats() []float64      { return []float64{fHuge, third, big, 1 << 62, mask64, 0.1 + 0.2, 1 / 3.0} }
func WithMask(x uint64) uint64    { return x&mask64 ^ (mask64 >> 60) }
func FloatFromBig(x float64) float64 { return x * big / (big >> 3) }
func VarFloatSum(a, b float64) bool { return a+b == 0.3 } // not exact at run time

// ---- len constants ----

var lenCalls int

func arrFn() [4]int      { lenCalls++; return [4]int{} }
func arrPtrFn() *[5]int  { lenCalls++; return new([5]int) }

func LenConsts(s string) ([]int, int) {
	const n = len("hello")
	var arr [len("abc") + 2]int8
	var grid [3][7]bool
	sparse := [...]int{5: 1}
	p := &arr
	before := lenCalls
	out := []int{
		n, len(arr), cap(arr), len(grid), len(grid[1]), len(sparse), len(p), cap(p), len([10]int{}),
		len(arrFn()),    // a function call: not constant, the call happens
		len(arrPtrFn()), // likewise
		len(s), len(s + "xy"), len([]byte(s)), len([]rune(s)),
	}
	return out, lenCalls - before
}

// ---- default types of untyped constants ----

func kindOf(x any) string {
	switch x.(type) {
	case nil:
		return "nil"
	case int:
		return "int"
	case int32:
		return "int32"
	case uint8:
		return "uint8"
	case float64:
		return "float64"
	case complex128:
		return "complex128"
	case string:
		return "string"
	case bool:
		return "bool"
	case uint:
		return "uint"
	}
	return "?"
}

func UntypedKinds(s uint) []string {
	return []string{
		kindOf(1), kindOf(1.0), kindOf('a'), kindOf("s"), kindOf(true), kindOf(1i), kindOf(nil),
		kindOf(1 << 3), kindOf(1.0 << 3), kindOf('a' + 1), kindOf(1 + 'a'), kindOf(1 + 1.0), kindOf('a' * 1.0),
		kindOf(1i * 1i), kindOf(1 / 2), kindOf(2.0 / 1), kindOf(1 << s), kindOf('a' << s), kindOf(1 == 2),
		kindOf("a" + "b"), kindOf(len("abc")), kindOf("abc"[0]), kindOf(s + 1), kindOf(real(1i)), kindOf(-'a'),
	}
}

func UntypedValues(s uint) []any {
	return []any{1 << 3, 1.0 << 3, 'a' + 1, 1 / 2, 1 / 2.0, 1 << s, 5 % 3 * 1i == 2i, "abc"[1], 07 + 0x10 + 0b11 + 0o7, 1_000 * 1e-3}
}

// ---- iota ----

type Bits uint8

const (
	B0 Bits = 1 << iota
	B1
	_
	B3
	BAll = B0 | B1 | B3
)

const (
	sq0 = iota * iota
	sq1
	sq2
	sq3
)

const (
	rep0 = "s"
	rep1
	rep2 = iota
	rep3
)

const (
	ia, ib = iota, -iota
	ic, id
	_, _
	ie, ig
)

const (
	half0 = iota * 0.5
	half1
	half2
	ch0 = 'a' + iota
	ch1
)

const single = iota + 7

func Iotas() []int {
	const (
		l0 = iota + 10
		l1
		l2 = iota << 4
		l3
	)
	return []int{int(B0), int(B1), int(B3), int(BAll), sq0, sq1, sq2, sq3, rep2, rep3, ia, ib, ic, id, ie, ig, single, l0, l1, l2, l3, ch0, ch1}
}
func IotaStrings() []string  { return []string{rep0, rep1, string(rune(ch1))} }
func IotaFloats() []float64  { return []float64{half0, half1, half2} }

// ---- bit twiddling ----

func PopCount(x uint64) int {
	n := 0
	for ; x != 0; x &= x - 1 {
		n++
	}
	return n
}

func PopCountSWAR(x uint64) int {
	x -= x >> 1 & 0x5555555555555555
	x = x&0x3333333333333333 + x>>2&0x3333333333333333
	x = (x + x>>4) & 0x0f0f0f0f0f0f0f0f
	return int(x * 0x0101010101010101 >> 56)
}

func LeadingZeros(x uint64) int {
	if x == 0 {
		return 64
	}
	n := 0
	for s := uint(32); s > 0; s >>= 1 {
		if x>>(64-s) == 0 {
			n += int(s)
			x <<= s
		}
	}
	return n
}

func TrailingZeros(x uint32) int {
	if x == 0 {
		return 32
	}
	n := 0
	for x&1 == 0 {
		x >>= 1
		n++
	}
	return n
}

func Reverse8(b uint8) uint8 {
	b = b>>4 | b<<4
	b = b>>2&0x33 | b<<2&0xcc
	b = b>>1&0x55 | b<<1&0xaa
	return b
}

func Reverse64(x uint64) (r uint64) {
	for i := 0; i < 64; i++ {
		r = r<<1 | x&1
		x >>= 1
	}
	return
}

func RotL(x uint64, k int) uint64   { return x<<(uint(k)&63) | x>>(-uint(k)&63) }
func RotL32(x uint32, k uint) uint32 { return x<<(k%32) | x>>((32-k%32)%32) }
func RotL16(x uint16, k uint8) uint16 { return x<<(k&15) | x>>(16-k&15) }
func ByteSwap(x uint32) uint32 {
	return x>>24 | x>>8&0xff00 | x<<8&0xff0000 | x<<24
}
func IsPow2(x uint64) bool { return x != 0 && x&(x-1) == 0 }
func NextPow2(x uint32) uint32 {
	x--
	x |= x >> 1
	x |= x >> 2
	x |= x >> 4
	x |= x >> 8
	x |= x >> 16
	return x + 1
}
func Parity(x uint64) uint {
	x ^= x >> 32
	x ^= x >> 16
	x ^= x >> 8
	x ^= x >> 4
	return uint(0x6996>>(x&0xf)) & 1
}
func Abs(x int64) int64 {
	m := x >> 63
	return (x ^ m) - m
}
func SignExtend(x uint64, bits uint) int64 { return int64(x<<(64-bits)) >> (64 - bits) }
func Gray(x uint32) uint32                 { return x ^ x>>1 }
func UnGray(g uint32) uint32 {
	for s := uint(1); s < 32; s <<= 1 {
		g ^= g >> s
	}
	return g
}
func Interleave(x, y uint16) (z uint32) {
	for i := uint(0); i < 16; i++ {
		z |= uint32(x)&(1<<i)<<i | uint32(y)&(1<<i)<<(i+1)
	}
	return
}
func AndNot(x, y uint8) uint8      { return x &^ y }
func SetClearToggle(x uint16, b uint) (uint16, uint16, uint16, bool) {
	return x | 1<<b, x &^ (1 << b), x ^ 1<<b, x&(1<<b) != 0
}
func MinMaxBranchless(a, b int32) (int32, int32) {
	d := int64(a) - int64(b)
	m := int32(d >> 63)
	return b + (a-b)&m, a - (a-b)&m
}

// ---- print and println (they only need to compile and run; output goes to stderr) ----

func Prints(x int, s string, f float64, p *int, sl []int, m map[string]int, e any) int {
	print()
	print(x, s, f, true, uint8(x), int64(-x), 'r', 2+3i, "\n")
	println()
	println(x, s, f, false, p, sl, m, e, float32(f), uintptr(x), complex64(1i))
	println("misc.Prints", len(sl), cap(sl), len(m))
	return x + 1
}

// ---- variadic functions ----

func Sum(xs ...int) (s int) {
	for _, x := range xs {
		s += x
	}
	return
}
func Count(xs ...int) (int, bool) { return len(xs), xs == nil }

func CallNone() (int, bool)        { return Count() }
func CallSpreadNil() (int, bool)   { return Count(nil...) }
func CallSpreadEmpty() (int, bool) { return Count([]int{}...) }
func CallSome(a, b, c int) int     { return Sum(a, b, c) + Sum(a) + Sum() }
func CallSpread(xs []int) int      { return Sum(xs...) }

func setFirst(v int, xs ...int) {
	if len(xs) > 0 {
		xs[0] = v
	}
}

// A spread slice is passed as is (aliased); listed arguments get a fresh slice.
func SpreadAliases(xs []int, v int) (int, int) {
	a, b := xs[0], xs[1]
	setFirst(v, a, b)
	listed := xs[0]
	setFirst(v, xs...)
	return listed, xs[0]
}

func Kinds(args ...any) string {
	s := itoa(len(args)) + ":"
	for _, a := range args {
		s += kindOf(a) + ","
	}
	return s
}
func KindsNone() string                 { return Kinds() }
func KindsNil() string                  { return Kinds(nil) }       // one nil argument
func KindsNilSpread() string            { return Kinds(nil...) }    // no arguments
func KindsMixed(i int, s string) string { return Kinds(i, s, nil, 2.5, 'x', i > 0, uint8(i), 1i) }
func KindsSlice(xs []any) string        { return Kinds(xs) + Kinds(xs...) } // one argument, then len(xs)

func Prefix(p string, xs ...string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += " "
		}
		out += p + x
	}
	return out
}
func CallPrefix(p string, a, b string) string { return Prefix(p) + "|" + Prefix(p, a) + "|" + Prefix(p, a, b) }

type joiner struct{ sep string }

func (j joiner) Join(first string, rest ...string) string {
	for _, r := range rest {
		first += j.sep + r
	}
	return first
}

func VariadicMethod(sep string, xs []string) string {
	j := joiner{sep}
	f := j.Join
	g := joiner.Join
	return j.Join("a") + ";" + j.Join("a", xs...) + ";" + f("b", "c", "d") + ";" + g(j, "e", xs...)
}

func VariadicClosure(base int, xs []int) int {
	f := func(scale int, vs ...int) int {
		t := base
		for _, v := range vs {
			t += v * scale
		}
		return t
	}
	return f(1) + f(2, xs...) + f(3, 1, 2)
}

func AppendForms(a []int, b []int, s string) ([]int, []byte) {
	x := append(a, b...)
	x = append(x)
	x = append(x, 1, 2)
	x = append(x, x...)
	y := append([]byte("<"), s...)
	y = append(y, '>')
	return x, y
}

func variadicRec(depth int, xs ...int) int {
	if depth == 0 {
		return len(xs)*1000 + Sum(xs...)
	}
	return variadicRec(depth-1, append(xs, depth)...)
}
func VariadicRec(depth int) int { return variadicRec(depth) }
