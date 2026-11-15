package gotestfinds

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

type xer interface{ x() }
type I1 interface{ x() }
type I2 interface{ x() }
type X int

func (X) x() {}

// two named interface types with the same methods: one itab each, equal values
func TestInterfaceTypesSameMethods(t *testing.T) {
	a1, a2 := I1(X(0)), I2(X(0))
	b2 := I2(X(1))
	var a3 I1 = a2
	var u xer = a1
	var e any = X(0)
	if !(a1 == a2) || a1 == b2 || !(a1 == a3) || !(I2(a1) == a2) || !(u == a2) || !(a1 == e.(I2)) {
		t.Errorf("comparisons of I1 and I2 values: %v %v %v %v %v %v", a1 == a2, a1 == b2, a1 == a3, I2(a1) == a2, u == a2, a1 == e.(I2))
	}
	m := map[I1]int{a1: 1}
	if m[a3] != 1 || m[I1(a2)] != 1 {
		t.Errorf("map keyed by I1 does not find a value converted from I2")
	}
}

// for i, x[i] = range y is one assignment of two values per iteration
func TestRangeTupleAssign(t *testing.T) {
	x := []int{10, 20}
	y := []int{99}
	i := 1
	for i, x[i] = range y {
		break
	}
	if i != 0 || x[0] != 10 || x[1] != 99 {
		t.Errorf("slice: %d %v", i, x)
	}
	rs := []rune{'a', 'b'}
	i = 1
	for i, rs[i] = range "c" {
		break
	}
	if i != 0 || rs[0] != 'a' || rs[1] != 'c' {
		t.Errorf("string: %d %v", i, rs)
	}
	ys := []int{1, 2, 3}
	r := rune(1)
	for ys[r], r = range "\x02" {
		break
	}
	if r != 2 || ys[0] != 1 || ys[1] != 0 || ys[2] != 3 {
		t.Errorf("string, index first: %d %v", r, ys)
	}
	m := map[int]int{7: 8}
	k := 0
	xs := []int{0, 0}
	k = 1
	for k, xs[k] = range m {
	}
	if k != 7 || xs[1] != 8 {
		t.Errorf("map: %d %v", k, xs)
	}
}

type Z struct{}

func (Z) M() {}

type EZ struct{ *Z }
type FZ struct {
	n int
	*Z
}
type A0 [0]int

func (A0) M() {}

type S3 struct {
	i int
	j float64
	x [10]int
}
type TZ struct {
	z struct{}
	y int
}

var sinkF func()
var sinkP unsafe.Pointer
var zs Z
var as A0

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	f()
}

// a nil pointer faults where the stock compiler checks it, also when nothing is loaded through it
func TestNilChecksWithoutALoad(t *testing.T) {
	var ip *int
	var sp *S3
	var p *Z
	var e EZ
	var pe *EZ
	var f FZ
	var pa *A0
	var pt *TZ
	var qt *TZ
	mustPanic(t, "&*ip", func() { sinkP = unsafe.Pointer(&*ip) })
	mustPanic(t, "&sp.i", func() { sinkP = unsafe.Pointer(&sp.i) })
	mustPanic(t, "&sp.j", func() { sinkP = unsafe.Pointer(&sp.j) })
	mustPanic(t, "&sp.x[3]", func() { sinkP = unsafe.Pointer(&sp.x[3]) })
	mustPanic(t, "p.M()", func() { p.M() })
	mustPanic(t, "p.M", func() { sinkF = p.M })
	mustPanic(t, "e.M()", func() { e.M() })
	mustPanic(t, "e.M", func() { sinkF = e.M })
	mustPanic(t, "pe.M()", func() { pe.M() })
	mustPanic(t, "pe.M", func() { sinkF = pe.M })
	mustPanic(t, "f.M()", func() { f.M() })
	mustPanic(t, "zs = *p", func() { zs = *p })
	mustPanic(t, "_ = *p", func() { _ = *p })
	mustPanic(t, "pa.M()", func() { pa.M() })
	mustPanic(t, "as = *pa", func() { as = *pa })
	mustPanic(t, "any(*p)", func() { var i interface{ M() } = *p; _ = i })
	mustPanic(t, "defer p.M()", func() { defer p.M() })
	mustPanic(t, "pt.z == qt.z", func() { _ = pt.z == qt.z })
	mustPanic(t, "len(*f())", func() { _ = len(*nilArr()) })
	mustPanic(t, "range *f()", func() {
		for range *nilArr() {
		}
	})
	// and where it does not
	var np *[4]int
	if len(np) != 4 || len(*np) != 4 {
		t.Errorf("len of a nil pointer to an array")
	}
	for range np {
	}
}

//go:noinline
func nilArr() *[4]int { return nil }

//go:noinline
func three() (int16, float64, string) { return -42, 42.5, "x" }

//go:noinline
func twoF() (float64, float64) { return 5, 7 }

//go:noinline
func sliceAnd() ([]string, string) { return []string{"foo"}, "bar" }

//go:noinline
func bytesAnd(a []byte) ([]byte, []byte) { return a, []byte("abc") }

//go:noinline
func mapAnd(m map[int]int) (map[int]int, int) { return m, 0 }

// a call with several results as the arguments of a builtin
func TestBuiltinOfMultiValueCall(t *testing.T) {
	if c := complex(twoF()); c != 5+7i {
		t.Errorf("complex(f()) = %v", c)
	}
	if s := append(sliceAnd()); len(s) != 2 || s[0] != "foo" || s[1] != "bar" {
		t.Errorf("append(f()) = %v", s)
	}
	a := []byte{1, 2, 3}
	if n := copy(bytesAnd(a)); n != 3 || string(a) != "abc" {
		t.Errorf("copy(f()) = %d %q", n, a)
	}
	m := map[int]int{0: 0}
	delete(mapAnd(m))
	if len(m) != 0 {
		t.Errorf("delete(f()): %v", m)
	}
	print(three()) // (compiles, runs)
	println()
}

// shifts whose count is an untyped constant of another kind, or itself a shift of an untyped constant
func TestShiftCounts(t *testing.T) {
	x := 1
	got := []int{x << 1, x << 1., x << (1 + 0i), x << 0i, x << (1 << x), x << (1. << x), x << ((1 + 0i) << x), x << (0i << x)}
	want := []int{2, 2, 2, 1, 4, 4, 4, 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shifts: %v, want %v", got, want)
	}
	v := uint(48)
	if r := v >> ((1 >> v) + (1 >> v)); r != 48 {
		t.Errorf("v >> ((1 >> v) + (1 >> v)) = %d", r)
	}
}

//go:noinline
func strIdx(i uint8) byte { return ""[i>>9] }

func TestConstantStringOperands(t *testing.T) {
	mustPanic(t, `""[i]`, func() { strIdx(7) })
	n := 0
	for range "abc" {
		n++
	}
	if n != 3 || unsafe.String(nil, 0) != "" {
		t.Errorf("range over a constant string: %d", n)
	}
}

// m[k] = v with v faulting: the fault comes before the insertion
func TestMapAssignAfterValue(t *testing.T) {
	m := map[int]int{}
	func() {
		defer func() { recover() }()
		var p *int
		m[0] = *p
	}()
	if len(m) != 0 {
		t.Errorf("m[0] = *nil inserted the key")
	}
}

// a blank field of a literal stays zero
var blankG = struct{ a, _, c int }{1, 2, 3}

func TestBlankFieldStaysZero(t *testing.T) {
	if i := reflect.ValueOf(blankG).Field(1).Int(); i != 0 {
		t.Errorf("static: blank field holds %d", i)
	}
	two := 2
	l := struct{ a, _, c int }{1, two, 3}
	if i := reflect.ValueOf(l).Field(1).Int(); i != 0 {
		t.Errorf("dynamic: blank field holds %d", i)
	}
}

// assigning between two views of one array is a memmove
func TestOverlappingArrayAssign(t *testing.T) {
	for _, up := range []bool{true, false} {
		var x [64]byte
		for i := range x {
			x[i] = byte(i)
		}
		y := x
		if up {
			copy(x[4:36], x[2:34])
			*(*[32]byte)(y[4:36]) = *(*[32]byte)(y[2:34])
		} else {
			copy(x[2:34], x[4:36])
			*(*[32]byte)(y[2:34]) = *(*[32]byte)(y[4:36])
		}
		if x != y {
			t.Errorf("32 bytes, up=%v: %v", up, y)
		}
	}
	for _, up := range []bool{true, false} {
		x := make([]byte, 1000)
		for i := range x {
			x[i] = byte(i * 7)
		}
		y := append([]byte(nil), x...)
		if up {
			copy(x[13:516], x[2:505])
			*(*[503]byte)(y[13:516]) = *(*[503]byte)(y[2:505])
		} else {
			copy(x[2:505], x[13:516])
			*(*[503]byte)(y[2:505]) = *(*[503]byte)(y[13:516])
		}
		if string(x) != string(y) {
			t.Errorf("503 bytes, up=%v", up)
		}
	}
}

type T1r struct{}

func (*T1r) M() { recovered = recover() }

type Mer interface{ M() }

var recovered any

// recover in a function deferred through reflect (reflect's call helpers do not count as its caller)
func TestRecoverThroughReflect(t *testing.T) {
	func() {
		f := reflect.ValueOf(&T1r{}).Method(0).Interface().(func())
		defer f()
		panic(9)
	}()
	if recovered != 9 {
		t.Errorf("deferred method value from reflect: recovered %v", recovered)
	}
	recovered = nil
	func() {
		f := reflect.TypeOf(&T1r{}).Method(0).Func.Interface().(func(*T1r))
		defer f(&T1r{})
		panic(10)
	}()
	if recovered != 10 {
		t.Errorf("deferred method func from reflect: recovered %v", recovered)
	}
}

var zx, zy [0]int
var zp = new([0]int)

func TestZeroSizeGlobals(t *testing.T) {
	if &zx != zp || &zy != zp {
		t.Errorf("package-level variables of no size are not where new puts one")
	}
}

type bigT struct {
	A [1 << 20]byte
	B string
}

//go:noinline
func bigFill(f *bigT) { *f = bigT{B: "hello"} }

// a big literal of constants assigned through a pointer is not first built in the frame
func TestBigLiteralInPlace(t *testing.T) {
	done := make(chan uintptr)
	go func() {
		f := new(bigT)
		f.A[5] = 1
		var m0 [1]uintptr
		lo := uintptr(unsafe.Pointer(&m0))
		bigFill(f)
		if f.B != "hello" || f.A[5] != 0 {
			lo = 0
		}
		done <- lo
	}()
	if <-done == 0 {
		t.Errorf("*f = bigT{B: \"hello\"}: wrong value")
	}
}

func kaSetup() unsafe.Pointer {
	s := "ok"
	runtime.SetFinalizer(&s, func(p *string) { *p = "FAIL" })
	return unsafe.Pointer(&s)
}

//go:noinline
//go:uintptrescapes
func kaUse(p, q uintptr) string {
	runtime.GC()
	runtime.GC()
	return *(*string)(unsafe.Pointer(p)) + *(*string)(unsafe.Pointer(q))
}

//go:noinline
//go:uintptrescapes
func kaLater(res chan string, p, q uintptr) {
	res <- kaUse(p, q)
}

// uintptr(unsafe.Pointer(x)) in a call's arguments keeps x's object until the call returns
func TestUintptrArgumentKeepsObject(t *testing.T) {
	if s := kaUse(uintptr(kaSetup()), uintptr(kaSetup())); s != "okok" {
		t.Errorf("direct call: %q", s)
	}
	res := make(chan string, 1)
	func() {
		defer kaLater(res, uintptr(kaSetup()), uintptr(kaSetup()))
	}()
	if s := <-res; s != "okok" {
		t.Errorf("deferred call: %q", s)
	}
	go kaLater(res, uintptr(kaSetup()), uintptr(kaSetup()))
	if s := <-res; s != "okok" {
		t.Errorf("go statement: %q", s)
	}
}

type T3 struct{ a, b, c int }

//go:noinline
func boom(n int) int {
	if n > 0 {
		panic("boom")
	}
	return n
}

func litResult(n int) (r T3) {
	defer func() { recover() }()
	r = T3{1, 2, 3}
	r = T3{a: 7, b: boom(n), c: 9}
	return
}

type W3 struct {
	t T3
	k int
}

func litResultField(n int) (w W3) {
	defer func() { recover() }()
	w.t = T3{1, 2, 3}
	w.t = T3{a: 7, b: boom(n), c: 9}
	return
}

func litResultArray(n int) (r [3]int) {
	defer func() { recover() }()
	r = [3]int{1, 2, 3}
	r = [3]int{7, boom(n), 9}
	return
}

func litReturn(n int) (r T3) {
	defer func() { recover() }()
	r = T3{1, 2, 3}
	return T3{a: 7, b: boom(n), c: 9}
}

// r = T{a, f()} leaves the result r alone when f panics and a deferred function recovers
// (found by reading fc's in-place literals, not by a test of the Go distribution)
func TestLiteralInResultWhenAnElementPanics(t *testing.T) {
	want := T3{1, 2, 3}
	if r := litResult(1); r != want {
		t.Errorf("r = T{..panic..}: %v", r)
	}
	if w := litResultField(1); w.t != want {
		t.Errorf("w.t = T{..panic..}: %v", w.t)
	}
	if r := litResultArray(1); r != [3]int{1, 2, 3} {
		t.Errorf("r = [3]int{..panic..}: %v", r)
	}
	if r := litReturn(1); r != want {
		t.Errorf("return T{..panic..}: %v", r)
	}
	if r := litResult(0); r != (T3{7, 0, 9}) {
		t.Errorf("r = T{...}: %v", r)
	}
}
