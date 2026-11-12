// Package complexes exercises complex64 and complex128.
// Imports: none.
package complexes

func Add128(a, b complex128) complex128 { return a + b }
func Sub128(a, b complex128) complex128 { return a - b }
func Mul128(a, b complex128) complex128 { return a * b }
func Div128(a, b complex128) complex128 { return a / b }
func Neg128(a complex128) complex128    { return -a }
func Pos128(a complex128) complex128    { return +a }

func Add64(a, b complex64) complex64 { return a + b }
func Sub64(a, b complex64) complex64 { return a - b }
func Mul64(a, b complex64) complex64 { return a * b }
func Div64(a, b complex64) complex64 { return a / b }
func Neg64(a complex64) complex64    { return -a }

func Make128(r, i float64) complex128 { return complex(r, i) }
func Make64(r, i float32) complex64   { return complex(r, i) }
func Real128(c complex128) float64    { return real(c) }
func Imag128(c complex128) float64    { return imag(c) }
func Real64(c complex64) float32      { return real(c) }
func Imag64(c complex64) float32      { return imag(c) }
func Parts128(c complex128) (float64, float64) {
	return real(c), imag(c)
}
func FromInts(r, i int) complex128 { return complex(float64(r), float64(i)) }

func Eq128(a, b complex128) bool { return a == b }
func Ne128(a, b complex128) bool { return a != b }
func Eq64(a, b complex64) bool   { return a == b }
func Ne64(a, b complex64) bool   { return a != b }
func IsZero128(c complex128) bool { return c == 0 }
func IsI64(c complex64) bool      { return c == 1i }

func To64(c complex128) complex64  { return complex64(c) }
func To128(c complex64) complex128 { return complex128(c) }

type C128 complex128

func Named(c complex128) C128     { return C128(c) * C128(c) }
func (c C128) Twice() C128        { return c + c }
func NamedTwice(c complex128) C128 { return C128(c).Twice() }

func Conj(c complex128) complex128 { return complex(real(c), -imag(c)) }
func Swap(c complex128) complex128 { return complex(imag(c), real(c)) }
func Abs2(c complex128) float64    { return real(c)*real(c) + imag(c)*imag(c) }
func Abs2f(c complex64) float32    { return real(c)*real(c) + imag(c)*imag(c) }

// arithmetic with constants
func AddConst(c complex128) complex128         { return c + (1 + 2i) }
func MulI(c complex128) complex128             { return c * 1i }
func MulI64(c complex64) complex64             { return c * 1i }
func AddReal(c complex128) complex128          { return c + 2.5 }
func RSubConst(c complex64) complex64          { return (3 - 0.5i) - c }
func Scale(c complex128, f float64) complex128 { return c * complex(f, 0) }
func Recip(c complex128) complex128            { return 1 / c }
func Half(c complex64) complex64               { return c / 2 }

const (
	K1 = 1i * 1i
	K2 = (1 + 2i) / (3 - 4i)
	K3 = (0.1 + 0.2i) * (0.3 + 0.4i)
	K4 complex64 = 1.0 / 3i
)

func Consts() []complex128 { return []complex128{K1, K2, K3, complex128(K4), real(K2), imag(K3) * 1i} }

func Compound(a, b complex128) complex128 {
	c := a
	c += b
	c *= a
	c -= b
	c /= b
	return c
}

func Compound64(a, b complex64) complex64 {
	c := a
	c *= b
	c += a
	c /= b
	c -= 1
	return c
}

func PowInt(c complex128, n int) complex128 {
	r := complex128(1)
	for ; n > 0; n-- {
		r *= c
	}
	return r
}

// Poly evaluates a polynomial by Horner's rule.
func Poly(x complex128, coefs []complex128) complex128 {
	var r complex128
	for _, k := range coefs {
		r = r*x + k
	}
	return r
}

func Mandel(c complex128, limit int) int {
	z := complex128(0)
	for i := 0; i < limit; i++ {
		z = z*z + c
		if real(z)*real(z)+imag(z)*imag(z) > 4 {
			return i
		}
	}
	return limit
}

func Sum(cs []complex128) (s complex128) {
	for _, c := range cs {
		s += c
	}
	return
}

func Sum64(cs []complex64) (s complex64) {
	for i := range cs {
		s += cs[i]
	}
	return
}

func Dot(a, b [3]complex64) complex64 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func ScaleInPlace(cs []complex128, k complex128) {
	for i := range cs {
		cs[i] *= k
	}
}

type Pt struct {
	Tag byte
	Z   complex64
	W   complex128
	N   int32
}

func MakePt(z complex64, w complex128) Pt { return Pt{'p', z, w, 7} }
func PtSum(p Pt) complex128               { return complex128(p.Z) + p.W + complex(float64(p.N), float64(p.Tag)) }
func PtEq(a, b Pt) bool                   { return a == b }
func PtSet(p *Pt, z complex64) complex64 {
	old := p.Z
	p.Z = z
	p.W = -p.W
	return old
}

// ManyArgs mixes complex and scalar parameters past the register limit.
func ManyArgs(a complex64, b complex128, f float32, c complex64, n int, d complex128, e complex128, g float64, h complex64, i complex128, j complex128, k complex128) complex128 {
	return complex128(a) + b*complex(float64(f), 0) + complex128(c)*complex(float64(n), 0) + d - e + complex(g, -g) + complex128(h) + i*j + k
}

func Two(c complex128) (complex128, complex64, float64) {
	return c * c, complex64(c), imag(c)
}

func Box(c complex128) any  { return c }
func Box64(c complex64) any { return c }
func Unbox(x any) complex128 {
	switch v := x.(type) {
	case complex128:
		return v
	case complex64:
		return complex128(v) + 100
	case float64:
		return complex(v, 0)
	case int:
		return complex(0, float64(v))
	}
	return -1
}
func AnyEq(a, b any) bool { return a == b }

func MapCount(keys []complex128) (distinct int, hits int) {
	m := map[complex128]int{}
	for _, k := range keys {
		m[k]++
	}
	for _, k := range keys {
		if _, ok := m[k]; ok {
			hits++
		}
	}
	return len(m), hits
}

func Closure(c complex128) func(complex128) complex128 {
	acc := c
	return func(d complex128) complex128 {
		acc = acc*d + c
		return acc
	}
}

func Chan(cs []complex64) (s complex64) {
	ch := make(chan complex64, len(cs))
	for _, c := range cs {
		ch <- c
	}
	close(ch)
	for c := range ch {
		s += c * c
	}
	return
}

func ZeroValues() (complex64, complex128, bool) {
	var a complex64
	var b complex128
	var arr [2]complex128
	return a, b, arr[0] == 0 && arr[1] == complex(0, 0)
}
