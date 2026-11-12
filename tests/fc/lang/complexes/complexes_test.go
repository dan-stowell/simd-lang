package complexes_test

import (
	"fmt"
	"math"
	"testing"

	c "fclang/complexes"
)

// same reports whether two floats are the same value: bit-identical, or both NaN.
func same(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b) || (a != a && b != b)
}

func eq128(t *testing.T, name string, got, want complex128) {
	t.Helper()
	if !same(real(got), real(want)) || !same(imag(got), imag(want)) {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func eq64(t *testing.T, name string, got, want complex64) {
	t.Helper()
	eq128(t, name, complex128(got), complex128(want))
}

var (
	inf  = math.Inf(1)
	nan  = math.NaN()
	negz = math.Copysign(0, -1)
)

var parts = []float64{0, negz, 1, -1, 0.1, -2.5, 3e10, 1e-200, 1e200, math.MaxFloat64, math.SmallestNonzeroFloat64, inf, -inf, nan}

func values128() (vs []complex128) {
	for i, r := range parts {
		for j, im := range parts {
			if (i+j)%3 != 2 || i == j {
				vs = append(vs, complex(r, im))
			}
		}
	}
	return
}

func values64() (vs []complex64) {
	for _, v := range values128() {
		vs = append(vs, complex64(v))
	}
	return
}

func TestArith128(t *testing.T) {
	vs := values128()
	for _, a := range vs {
		eq128(t, fmt.Sprint("Neg ", a), c.Neg128(a), -a)
		eq128(t, fmt.Sprint("Pos ", a), c.Pos128(a), a)
		eq128(t, fmt.Sprint("Conj ", a), c.Conj(a), complex(real(a), -imag(a)))
		eq128(t, fmt.Sprint("Swap ", a), c.Swap(a), complex(imag(a), real(a)))
		eq128(t, fmt.Sprint("AddConst ", a), c.AddConst(a), a+(1+2i))
		eq128(t, fmt.Sprint("MulI ", a), c.MulI(a), a*1i)
		eq128(t, fmt.Sprint("AddReal ", a), c.AddReal(a), a+2.5)
		eq128(t, fmt.Sprint("Recip ", a), c.Recip(a), 1/a)
		eq128(t, fmt.Sprint("Scale ", a), c.Scale(a, -1.5), a*complex(-1.5, 0))
		if !same(c.Abs2(a), real(a)*real(a)+imag(a)*imag(a)) || !same(c.Real128(a), real(a)) || !same(c.Imag128(a), imag(a)) {
			t.Error("Abs2/Real/Imag", a)
		}
		if c.IsZero128(a) != (a == 0) {
			t.Error("IsZero128", a)
		}
		for _, b := range vs {
			n := fmt.Sprint(a, b)
			eq128(t, "Add "+n, c.Add128(a, b), a+b)
			eq128(t, "Sub "+n, c.Sub128(a, b), a-b)
			eq128(t, "Mul "+n, c.Mul128(a, b), a*b)
			eq128(t, "Div "+n, c.Div128(a, b), a/b)
			eq128(t, "Compound "+n, c.Compound(a, b), compound(a, b))
			if c.Eq128(a, b) != (a == b) || c.Ne128(a, b) != (a != b) {
				t.Error("Eq128/Ne128", a, b)
			}
		}
	}
	// by hand
	eq128(t, "Mul hand", c.Mul128(1+2i, 3-4i), 11+2i)
	eq128(t, "Div hand", c.Div128(11+2i, 3-4i), 1+2i)
	eq128(t, "Div zero", c.Div128(1+1i, 0), complex(inf, inf))
	eq128(t, "Div 0/0", c.Div128(0, 0), complex(nan, nan))
	eq128(t, "Neg zero", c.Neg128(0), complex(negz, negz))
	eq128(t, "Make128", c.Make128(1.5, negz), complex(1.5, negz))
	eq128(t, "FromInts", c.FromInts(-3, 1<<53+1), complex(-3, float64(1<<53)))
	r, i := c.Parts128(complex(nan, -inf))
	if !same(r, nan) || !same(i, -inf) {
		t.Error("Parts128")
	}
}

func compound(a, b complex128) complex128 {
	x := a
	x += b
	x *= a
	x -= b
	x /= b
	return x
}

func compound64(a, b complex64) complex64 {
	x := a
	x *= b
	x += a
	x /= b
	x -= 1
	return x
}

func TestArith64(t *testing.T) {
	vs := values64()
	for _, a := range vs {
		eq64(t, fmt.Sprint("Neg64 ", a), c.Neg64(a), -a)
		eq64(t, fmt.Sprint("MulI64 ", a), c.MulI64(a), a*1i)
		eq64(t, fmt.Sprint("RSubConst ", a), c.RSubConst(a), (3-0.5i)-a)
		eq64(t, fmt.Sprint("Half ", a), c.Half(a), a/2)
		eq128(t, fmt.Sprint("To128 ", a), c.To128(a), complex128(a))
		if !same(float64(c.Abs2f(a)), float64(real(a)*real(a)+imag(a)*imag(a))) || !same(float64(c.Real64(a)), float64(real(a))) || !same(float64(c.Imag64(a)), float64(imag(a))) {
			t.Error("Abs2f/Real64/Imag64", a)
		}
		if c.IsI64(a) != (a == 1i) {
			t.Error("IsI64", a)
		}
		for _, b := range vs {
			n := fmt.Sprint(a, b)
			eq64(t, "Add64 "+n, c.Add64(a, b), a+b)
			eq64(t, "Sub64 "+n, c.Sub64(a, b), a-b)
			eq64(t, "Mul64 "+n, c.Mul64(a, b), a*b)
			eq64(t, "Div64 "+n, c.Div64(a, b), a/b)
			eq64(t, "Compound64 "+n, c.Compound64(a, b), compound64(a, b))
			if c.Eq64(a, b) != (a == b) || c.Ne64(a, b) != (a != b) {
				t.Error("Eq64/Ne64", a, b)
			}
		}
	}
	for _, a := range values128() {
		eq64(t, fmt.Sprint("To64 ", a), c.To64(a), complex64(a))
	}
	if !c.IsI64(1i) || c.IsI64(1) {
		t.Error("IsI64 hand")
	}
	eq64(t, "Make64", c.Make64(0.1, -0.2), complex(float32(0.1), float32(-0.2)))
	eq64(t, "Mul64 hand", c.Mul64(1+2i, 3-4i), 11+2i)
	eq64(t, "To64 round", c.To64(complex(1+1e-10, 1e-50)), complex(1, 0))
	eq64(t, "To64 overflow", c.To64(complex(1e300, -1e300)), complex64(complex(inf, -inf)))
}

func TestConstsAndNamed(t *testing.T) {
	got := c.Consts()
	want := []complex128{-1, (1 + 2i) / (3 - 4i), (0.1 + 0.2i) * (0.3 + 0.4i), complex128(complex64(1.0 / 3i)), real((1 + 2i) / (3 - 4i)), imag((0.1+0.2i)*(0.3+0.4i)) * 1i}
	if len(got) != len(want) {
		t.Fatal("Consts length")
	}
	for i := range want {
		eq128(t, fmt.Sprint("Consts ", i), got[i], want[i])
	}
	eq128(t, "K2 by hand", got[1], complex(-0.2, 0.4))
	eq128(t, "Named", complex128(c.Named(1+2i)), -3+4i)
	eq128(t, "NamedTwice", complex128(c.NamedTwice(1.5-2i)), 3-4i)
	a, b, ok := c.ZeroValues()
	if a != 0 || b != 0 || !ok {
		t.Error("ZeroValues")
	}
}

func TestLoops(t *testing.T) {
	for n := 0; n < 12; n++ {
		want := complex128(1)
		for i := 0; i < n; i++ {
			want *= 1.5 - 0.5i
		}
		eq128(t, fmt.Sprint("PowInt ", n), c.PowInt(1.5-0.5i, n), want)
	}
	eq128(t, "PowInt i^4", c.PowInt(1i, 4), complex(1, negz))
	eq128(t, "Poly", c.Poly(2+1i, []complex128{1, -2i, 3}), ((2+1i)*1+-2i)*(2+1i)+3)
	eq128(t, "Poly empty", c.Poly(2, nil), 0)
	for _, m := range []struct {
		c    complex128
		want int
	}{{0, 50}, {2 + 2i, 0}, {-1, 50}, {0.5, 4}, {-0.75 + 0.2i, mandel(-0.75+0.2i, 50)}, {0.3 + 0.5i, mandel(0.3+0.5i, 50)}} {
		if got := c.Mandel(m.c, 50); got != m.want {
			t.Errorf("Mandel(%v) = %d, want %d", m.c, got, m.want)
		}
	}
	vs := []complex128{1 + 2i, -3.5 + 0.25i, 1e10 - 1e-10i, 0}
	eq128(t, "Sum", c.Sum(vs), ((1+2i)+(-3.5+0.25i))+(1e10-1e-10i))
	eq128(t, "Sum nil", c.Sum(nil), 0)
	eq64(t, "Sum64", c.Sum64([]complex64{1 + 2i, 0.5 - 4i}), 1.5-2i)
	eq64(t, "Dot", c.Dot([3]complex64{1, 1i, 2 + 2i}, [3]complex64{3, 1i, 1 - 1i}), 3-1+4)
	c.ScaleInPlace(vs, 2i)
	eq128(t, "ScaleInPlace 0", vs[0], (1+2i)*2i)
	eq128(t, "ScaleInPlace 1", vs[1], (-3.5+0.25i)*2i)
	eq64(t, "Chan", c.Chan([]complex64{1i, 2, 1 + 1i}), -1+4+2i)
	f := c.Closure(1 + 1i)
	eq128(t, "Closure 1", f(2), 3+3i)
	eq128(t, "Closure 2", f(1i), (3+3i)*1i+(1+1i))
}

func mandel(k complex128, limit int) int {
	z := complex128(0)
	for i := 0; i < limit; i++ {
		z = z*z + k
		if real(z)*real(z)+imag(z)*imag(z) > 4 {
			return i
		}
	}
	return limit
}

func TestAggregates(t *testing.T) {
	p := c.MakePt(1-2i, 0.5+0.25i)
	if p.Tag != 'p' || p.Z != 1-2i || p.W != 0.5+0.25i || p.N != 7 {
		t.Errorf("MakePt %+v", p)
	}
	eq128(t, "PtSum", c.PtSum(p), (1-2i)+(0.5+0.25i)+complex(7, 'p'))
	q := p
	if !c.PtEq(p, q) {
		t.Error("PtEq equal")
	}
	q.W = complex(real(q.W), nan)
	if c.PtEq(p, q) || c.PtEq(q, q) {
		t.Error("PtEq NaN")
	}
	q = p
	q.Z = complex(float32(negz), 0) + q.Z
	if !c.PtEq(p, q) {
		t.Error("PtEq")
	}
	eq64(t, "PtSet", c.PtSet(&p, 9i), 1-2i)
	if p.Z != 9i || p.W != -0.5-0.25i {
		t.Error("PtSet effect")
	}
	args := func(a complex64, b complex128, f float32, cc complex64, n int, d, e complex128, g float64, h complex64, i, j, k complex128) complex128 {
		return complex128(a) + b*complex(float64(f), 0) + complex128(cc)*complex(float64(n), 0) + d - e + complex(g, -g) + complex128(h) + i*j + k
	}
	eq128(t, "ManyArgs", c.ManyArgs(1+2i, 3-4i, 0.5, -1i, 3, 7, 8i, 2.25, 0.5+0.5i, 1+1i, 2-3i, -100),
		args(1+2i, 3-4i, 0.5, -1i, 3, 7, 8i, 2.25, 0.5+0.5i, 1+1i, 2-3i, -100))
	sq, c64, im := c.Two(3 - 2i)
	if sq != 5-12i || c64 != 3-2i || im != -2 {
		t.Error("Two", sq, c64, im)
	}
}

func TestInterfacesAndMaps(t *testing.T) {
	if v, ok := c.Box(1 + 2i).(complex128); !ok || v != 1+2i {
		t.Error("Box")
	}
	if v, ok := c.Box64(1 + 2i).(complex64); !ok || v != 1+2i {
		t.Error("Box64")
	}
	eq128(t, "Unbox 128", c.Unbox(complex128(3i)), 3i)
	eq128(t, "Unbox 64", c.Unbox(complex64(3i)), 100+3i)
	eq128(t, "Unbox float", c.Unbox(2.5), 2.5)
	eq128(t, "Unbox int", c.Unbox(4), 4i)
	eq128(t, "Unbox other", c.Unbox("x"), -1)
	for _, k := range []struct {
		a, b any
		want bool
	}{
		{1 + 2i, 1 + 2i, true}, {1 + 2i, complex64(1 + 2i), false}, {complex(nan, 0), complex(nan, 0), false},
		{complex(0, negz), complex(negz, 0), true}, {complex64(1i), complex64(1i), true}, {0i, 0.0, false},
	} {
		if got := c.AnyEq(k.a, k.b); got != k.want {
			t.Errorf("AnyEq(%v, %v) = %v", k.a, k.b, got)
		}
	}
	d, h := c.MapCount([]complex128{1, 1i, 1, complex(0, negz), 0, 1 + 1i, 1i})
	if d != 4 || h != 7 {
		t.Error("MapCount", d, h)
	}
	// NaN keys never match, each insert makes a new entry
	d, h = c.MapCount([]complex128{complex(nan, 1), complex(nan, 1), complex(1, nan), 2})
	if d != 4 || h != 1 {
		t.Error("MapCount NaN", d, h)
	}
}
