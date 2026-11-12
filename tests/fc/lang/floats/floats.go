// Package floats: float32 and float64 arithmetic, comparisons (NaN, infinities, negative zero), conversions
// to and from every integer type, min/max.  No imports.
//
// Conversions of values the target type cannot represent are implementation-defined; the functions named
// F64To*/F32To* are compared with the stock compiler's results for in-range values by TestConvInRange and
// for all values by TestConvOutOfRange (which only logs differences unless FCLANG_IMPLDEF=1).
package floats

// ---- arithmetic

func Add64(a, b float64) float64 { return a + b }
func Sub64(a, b float64) float64 { return a - b }
func Mul64(a, b float64) float64 { return a * b }
func Div64(a, b float64) float64 { return a / b }
func Neg64(a float64) float64    { return -a }
func Add32(a, b float32) float32 { return a + b }
func Sub32(a, b float32) float32 { return a - b }
func Mul32(a, b float32) float32 { return a * b }
func Div32(a, b float32) float32 { return a / b }
func Neg32(a float32) float32    { return -a }

func Compound64(a, b float64) float64 {
	a += b
	a *= b
	a -= 1.5
	a /= 3
	a++
	a--
	return a
}
func Compound32(a, b float32) float32 {
	a += b
	a *= b
	a -= 1.5
	a /= 3
	a++
	a--
	return a
}

// Go does not fuse x*y + z unless written without an explicit conversion; these pin both forms' rounding of
// the product (the explicit conversion forbids fusing, and amd64 without GOAMD64=v3 never fuses).
func MulAdd64(x, y, z float64) float64    { return float64(x*y) + z }
func MulAdd32(x, y, z float32) float32    { return float32(x*y) + z }
func MulAddPlain(x, y, z float64) float64 { return x*y + z }
func Poly(x float64) float64              { return ((2.5*x-1.25)*x+0.125)*x - 7 }
func Poly32(x float32) float32            { return ((2.5*x-1.25)*x+0.125)*x - 7 }
func Mixed(a float32, b float64) float64  { return float64(a)*b + float64(a/3) }
func Round32(x float64) float64           { return float64(float32(x)) }
func Widen(x float32) float64             { return float64(x) }
func Narrow(x float64) float32            { return float32(x) }
func ZeroMinus(x float64) float64         { return 0 - x }
func NegZero() float64 {
	z := 0.0
	return -z
}
func NegZero32() float32 {
	var z float32
	return -z
}
func MulMinusOne(x float64) float64 { return x * -1 }
func Half(x float64) float64        { return x / 2 }
func Recip(x float64) float64       { return 1 / x }
func Recip32(x float32) float32     { return 1 / x }
func ConstAdd(x float64) float64    { return x + 0.1 }
func ConstAdd32(x float32) float32  { return x + 0.1 }
func ConstBig(x float64) float64    { return x * 1.7976931348623157e308 }
func ConstTiny(x float64) float64   { return x * 5e-324 }
func ConstExact(x float64) float64  { return x + 1<<53 }
func IntConst(x float64) float64    { return x + 3 }

// ---- comparisons

func Lt64(a, b float64) bool { return a < b }
func Le64(a, b float64) bool { return a <= b }
func Gt64(a, b float64) bool { return a > b }
func Ge64(a, b float64) bool { return a >= b }
func Eq64(a, b float64) bool { return a == b }
func Ne64(a, b float64) bool { return a != b }
func Lt32(a, b float32) bool { return a < b }
func Le32(a, b float32) bool { return a <= b }
func Gt32(a, b float32) bool { return a > b }
func Ge32(a, b float32) bool { return a >= b }
func Eq32(a, b float32) bool { return a == b }
func Ne32(a, b float32) bool { return a != b }

func NotLt(a, b float64) bool { return !(a < b) }
func NotGe(a, b float64) bool { return !(a >= b) }
func NotEq(a, b float64) bool { return !(a == b) }
func IsNaN(x float64) bool    { return x != x }
func IsNaN32(x float32) bool  { return x != x }
func IsInf(x float64) bool    { return x > 1.7976931348623157e308 || x < -1.7976931348623157e308 }
func EqSelf(x float64) bool   { return x == x }
func LtZero(x float64) bool   { return x < 0 }
func GeZero(x float64) bool   { return x >= 0 }
func EqZero(x float64) bool   { return x == 0 }
func NeZero32(x float32) bool { return x != 0 }

// Branches on comparisons (each of the six, in if and in a loop condition), unordered operands included.
func Classify(a, b float64) int {
	r := 0
	if a < b {
		r |= 1
	}
	if a <= b {
		r |= 2
	}
	if a > b {
		r |= 4
	}
	if a >= b {
		r |= 8
	}
	if a == b {
		r |= 16
	}
	if a != b {
		r |= 32
	}
	if !(a < b) {
		r |= 64
	}
	if a < b || a > b {
		r |= 128
	}
	if a <= b && a >= b {
		r |= 256
	}
	return r
}
func Classify32(a, b float32) int {
	r := 0
	if a < b {
		r |= 1
	}
	if a <= b {
		r |= 2
	}
	if a > b {
		r |= 4
	}
	if a >= b {
		r |= 8
	}
	if a == b {
		r |= 16
	}
	if a != b {
		r |= 32
	}
	return r
}
func Cmp3(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	case a == b:
		return 0
	}
	return 2
}
func SwitchFloat(x float64) string {
	switch x {
	case 0:
		return "zero"
	case 1.5:
		return "1.5"
	case -2:
		return "-2"
	}
	return "other"
}
func CountBelow(xs []float64, lim float64) (n int) {
	for i := 0; i < len(xs) && xs[i] < lim; i++ {
		n++
	}
	return
}
func Abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
func Sign(x float64) float64 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return x
}

// ---- min and max builtins

func Min64(a, b float64) float64      { return min(a, b) }
func Max64(a, b float64) float64      { return max(a, b) }
func Min32(a, b float32) float32      { return min(a, b) }
func Max32(a, b float32) float32      { return max(a, b) }
func Min3(a, b, c float64) float64    { return min(a, b, c) }
func Max3(a, b, c float64) float64    { return max(a, b, c) }
func MinConst(a float64) float64      { return min(a, 2.5) }
func MaxConst32(a float32) float32    { return max(a, -0.5, 100) }
func Clamp(x, lo, hi float64) float64 { return min(max(x, lo), hi) }
func MinInt(a, b int) int             { return min(a, b) }
func MaxInt3(a, b, c int64) int64     { return max(a, b, c) }
func MinU8(a, b uint8) uint8          { return min(a, b) }
func MaxU64(a, b uint64) uint64       { return max(a, b) }
func MinStr(a, b string) string       { return min(a, b) }
func MaxStr3(a, b, c string) string   { return max(a, b, c) }
func MinI8Const(a int8) int8          { return min(a, -5) }

// ---- integer to float

func I8ToF64(x int8) float64      { return float64(x) }
func I16ToF64(x int16) float64    { return float64(x) }
func I32ToF64(x int32) float64    { return float64(x) }
func I64ToF64(x int64) float64    { return float64(x) }
func IntToF64(x int) float64      { return float64(x) }
func U8ToF64(x uint8) float64     { return float64(x) }
func U16ToF64(x uint16) float64   { return float64(x) }
func U32ToF64(x uint32) float64   { return float64(x) }
func U64ToF64(x uint64) float64   { return float64(x) }
func UintToF64(x uint) float64    { return float64(x) }
func UptrToF64(x uintptr) float64 { return float64(x) }
func I8ToF32(x int8) float32      { return float32(x) }
func I16ToF32(x int16) float32    { return float32(x) }
func I32ToF32(x int32) float32    { return float32(x) }
func I64ToF32(x int64) float32    { return float32(x) }
func IntToF32(x int) float32      { return float32(x) }
func U8ToF32(x uint8) float32     { return float32(x) }
func U16ToF32(x uint16) float32   { return float32(x) }
func U32ToF32(x uint32) float32   { return float32(x) }
func U64ToF32(x uint64) float32   { return float32(x) }
func UintToF32(x uint) float32    { return float32(x) }
func UptrToF32(x uintptr) float32 { return float32(x) }

// ---- float to integer (truncation toward zero)

func F64ToI8(x float64) int8      { return int8(x) }
func F64ToI16(x float64) int16    { return int16(x) }
func F64ToI32(x float64) int32    { return int32(x) }
func F64ToI64(x float64) int64    { return int64(x) }
func F64ToInt(x float64) int      { return int(x) }
func F64ToU8(x float64) uint8     { return uint8(x) }
func F64ToU16(x float64) uint16   { return uint16(x) }
func F64ToU32(x float64) uint32   { return uint32(x) }
func F64ToU64(x float64) uint64   { return uint64(x) }
func F64ToUint(x float64) uint    { return uint(x) }
func F64ToUptr(x float64) uintptr { return uintptr(x) }
func F32ToI8(x float32) int8      { return int8(x) }
func F32ToI16(x float32) int16    { return int16(x) }
func F32ToI32(x float32) int32    { return int32(x) }
func F32ToI64(x float32) int64    { return int64(x) }
func F32ToInt(x float32) int      { return int(x) }
func F32ToU8(x float32) uint8     { return uint8(x) }
func F32ToU16(x float32) uint16   { return uint16(x) }
func F32ToU32(x float32) uint32   { return uint32(x) }
func F32ToU64(x float32) uint64   { return uint64(x) }
func F32ToUint(x float32) uint    { return uint(x) }
func F32ToUptr(x float32) uintptr { return uintptr(x) }

func Trunc(x float64) float64 { return float64(int64(x)) }
func Frac(x float64) float64  { return x - float64(int64(x)) }
func RoundTrip(x int64) int64 { return int64(float64(x)) }
func RoundTripU(x uint32) uint32 {
	return uint32(float32(x))
}
func Floor(x float64) int64 {
	i := int64(x)
	if float64(i) > x {
		i--
	}
	return i
}

// ---- floats in memory, in aggregates, through calls

type Vec struct{ X, Y, Z float64 }
type Vec32 struct{ X, Y, Z, W float32 }
type Mixed8 struct {
	A float32
	B int8
	C float64
	D uint16
	E float32
}

func Dot(a, b Vec) float64     { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func Cross(a, b Vec) Vec       { return Vec{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X} }
func Scale(v *Vec, k float64)  { v.X *= k; v.Y *= k; v.Z *= k }
func Dot32(a, b Vec32) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W }
func SumMixed(m Mixed8) float64 {
	return float64(m.A) + float64(m.B) + m.C + float64(m.D) + float64(m.E)
}
func MakeMixed(x float64) Mixed8 {
	return Mixed8{float32(x), int8(int64(x) & 127), x * 2, uint16(int64(x) & 0xffff), float32(x) / 2}
}
func Sum(xs []float64) (s float64) {
	for _, x := range xs {
		s += x
	}
	return
}
func Sum32(xs []float32) (s float32) {
	for _, x := range xs {
		s += x
	}
	return
}
func Kahan(xs []float64) float64 {
	var s, c float64
	for _, x := range xs {
		y := x - c
		t := s + y
		c = (t - s) - y
		s = t
	}
	return s
}
func Scale32(xs []float32, k float32) {
	for i := range xs {
		xs[i] *= k
	}
}
func Mean(xs []float64) float64 { return Sum(xs) / float64(len(xs)) }
func MinMax(xs []float64) (lo, hi float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	lo, hi = xs[0], xs[0]
	for _, x := range xs[1:] {
		if x < lo {
			lo = x
		}
		if x > hi {
			hi = x
		}
	}
	return
}
func Sqrt(x float64) float64 {
	if x < 0 || x != x {
		return x - x
	}
	if x == 0 || x > 1e308 {
		return x
	}
	z := x
	if z < 1 {
		z = 1
	}
	for i := 0; i < 2000; i++ {
		n := (z + x/z) / 2
		if n >= z {
			break
		}
		z = n
	}
	return z
}
func Many(a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, p, q float64) float64 {
	return a - b + c - d + e - f + g - h + i - j + k - l + m - n + o - p + q*2
}
func Many32(a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, p, q float32) float32 {
	return a - b + c - d + e - f + g - h + i - j + k - l + m - n + o - p + q*2
}
func Interleave(a int, x float64, b int8, y float32, c uint64, z float64) float64 {
	return float64(a) + x*float64(b) + float64(y)*float64(c) - z
}
func Swap(a, b float64) (float64, float64)           { return b, a }
func Swap32(a float32, b float64) (float64, float32) { return b, a }
func Results(x float64) (float64, int, float32, bool, float64) {
	return x * 2, int(int8(x)), float32(x) / 3, x > 0, -x
}
func CallMany(x float64) float64 {
	return Many(x, 1, x, 2, x, 3, x, 4, x, 5, x, 6, x, 7, x, 8, x)
}
func Horner(cs []float64, x float64) float64 {
	r := 0.0
	for i := len(cs) - 1; i >= 0; i-- {
		r = r*x + cs[i]
	}
	return r
}
func Temps(a, b, c, d float64) float64 {
	return ((a+b)*(c+d) - (a-b)*(c-d)) / ((a*c + b*d) + ((a+1)*(b+2)+(c+3)*(d+4))*((a-1)*(b-2)+(c-3)*(d-4)))
}
func FormatFixed(x float64, digits int) string {
	if x != x {
		return "NaN"
	}
	neg := x < 0
	if neg {
		x = -x
	}
	if x > 1e18 {
		return "big"
	}
	ip := uint64(x)
	fr := x - float64(ip)
	var buf []byte
	if ip == 0 {
		buf = append(buf, '0')
	}
	for ip > 0 {
		buf = append(buf, byte(ip%10)+'0')
		ip /= 10
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	if digits > 0 {
		buf = append(buf, '.')
		for ; digits > 0; digits-- {
			fr *= 10
			d := int(fr)
			buf = append(buf, byte(d)+'0')
			fr -= float64(d)
		}
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}
