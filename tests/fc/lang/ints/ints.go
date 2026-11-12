// Package ints: every integer type and width.  The per-type one-liners (arithmetic, comparison, shifts,
// compound assignment, the 11x11 conversions) are in ints_gen.go, written by gen.py; this file has the
// hand-written cases.  No imports.
package ints

// ---- constants near the limits of the types

const (
	MaxI8  = 1<<7 - 1
	MinI8  = -1 << 7
	MaxI16 = 1<<15 - 1
	MinI16 = -1 << 15
	MaxI32 = 1<<31 - 1
	MinI32 = -1 << 31
	MaxI64 = 1<<63 - 1
	MinI64 = -1 << 63
	MaxU8  = 1<<8 - 1
	MaxU16 = 1<<16 - 1
	MaxU32 = 1<<32 - 1
	MaxU64 = 1<<64 - 1
)

func AddMaxI8(x int8) int8       { return x + MaxI8 }
func AddMinI8(x int8) int8       { return x + MinI8 }
func AddMaxI16(x int16) int16    { return x + MaxI16 }
func AddMinI16(x int16) int16    { return x + MinI16 }
func AddMaxI32(x int32) int32    { return x + MaxI32 }
func AddMinI32(x int32) int32    { return x + MinI32 }
func AddMaxI64(x int64) int64    { return x + MaxI64 }
func AddMinI64(x int64) int64    { return x + MinI64 }
func AddMaxU8(x uint8) uint8     { return x + MaxU8 }
func AddMaxU16(x uint16) uint16  { return x + MaxU16 }
func AddMaxU32(x uint32) uint32  { return x + MaxU32 }
func AddMaxU64(x uint64) uint64  { return x + MaxU64 }
func IsMinI64(x int64) bool      { return x == MinI64 }
func IsMaxI64(x int64) bool      { return x == MaxI64 }
func IsMinI32(x int32) bool      { return x == MinI32 }
func IsMaxU64(x uint64) bool     { return x == MaxU64 }
func IsMaxU32(x uint32) bool     { return x == MaxU32 }
func LtBig(x uint64) bool        { return x < 1<<63+5 }
func GtBigNeg(x int64) bool      { return x > -1<<63+5 }
func MulBig(x uint64) uint64     { return x * 0x9e3779b97f4a7c15 }
func AndBig(x uint64) uint64     { return x & 0xff00ff00ff00ff00 }
func OrBig(x int64) int64        { return x | -0x8000000000000000 }
func XorBig(x int64) int64       { return x ^ 0x5555555555555555 }
func Imm32Edge(x int64) int64    { return x + 0x7fffffff }
func Imm32Over(x int64) int64    { return x + 0x80000000 }
func Imm32Neg(x int64) int64     { return x - 0x80000000 }
func Imm32NegOver(x int64) int64 { return x - 0x80000001 }
func CmpImm32Over(x int64) bool  { return x < 0x100000000 }
func AndImm32(x uint64) uint64   { return x & 0xffffffff }
func AndImm32Hi(x uint64) uint64 { return x & 0x80000000 }

// ---- division, remainder and multiplication by constants (strength reduction, magic numbers)

func DivC3(x int64) int64         { return x / 3 }
func DivC7(x int64) int64         { return x / 7 }
func DivCNeg5(x int64) int64      { return x / -5 }
func DivC1(x int64) int64         { return x / 1 }
func DivCNeg1(x int64) int64      { return x / -1 }
func RemCNeg1(x int64) int64      { return x % -1 }
func DivC8(x int64) int64         { return x / 8 }
func RemC8(x int64) int64         { return x % 8 }
func DivCNeg8(x int64) int64      { return x / -8 }
func RemC10(x int64) int64        { return x % 10 }
func RemCNeg10(x int64) int64     { return x % -10 }
func DivCMin(x int64) int64       { return x / MinI64 }
func RemCMin(x int64) int64       { return x % MinI64 }
func UDivC3(x uint64) uint64      { return x / 3 }
func UDivC7(x uint64) uint64      { return x / 7 }
func UDivC10(x uint64) uint64     { return x / 10 }
func URemC10(x uint64) uint64     { return x % 10 }
func UDivC16(x uint64) uint64     { return x / 16 }
func URemC16(x uint64) uint64     { return x % 16 }
func UDivCPrime(x uint64) uint64  { return x / 1000000007 }
func URemCPrime(x uint64) uint64  { return x % 1000000007 }
func UDivCHuge(x uint64) uint64   { return x / (1<<63 + 1) }
func DivC3I32(x int32) int32      { return x / 3 }
func RemC7I32(x int32) int32      { return x % 7 }
func DivC10U32(x uint32) uint32   { return x / 10 }
func RemC10U32(x uint32) uint32   { return x % 10 }
func DivC3I8(x int8) int8         { return x / 3 }
func RemC3I8(x int8) int8         { return x % 3 }
func DivC10U8(x uint8) uint8      { return x / 10 }
func DivC100U16(x uint16) uint16  { return x / 100 }
func DivCNeg7I16(x int16) int16   { return x / -7 }
func MulC10(x int64) int64        { return x * 10 }
func MulCNeg1(x int64) int64      { return x * -1 }
func MulC0(x int64) int64         { return x * 0 }
func MulC3I8(x int8) int8         { return x * 3 }
func MulC255U8(x uint8) uint8     { return x * 255 }
func MulCRep(x uint64) uint64     { return x * 0x0101010101010101 }
func MulC1000U32(x uint32) uint32 { return x * 1000 }
func ConstDivVar(x int64) int64   { return 1000000 / x }
func ConstRemVar(x int64) int64   { return 1000000 % x }
func MinDivVar(x int64) int64     { return MinI64 / x }
func MinRemVar(x int64) int64     { return MinI64 % x }
func MinDivVarI32(x int32) int32  { return MinI32 / x }
func MinDivVarI8(x int8) int8     { return MinI8 / x }
func DivMod(a, b int64) (q, r int64) {
	return a / b, a % b
}
func DivModU32(a, b uint32) (q, r uint32) {
	return a / b, a % b
}

// ---- shifts: constant counts, counts of every type, counts at and beyond the width

func ShlC63(x int64) int64              { return x << 63 }
func ShrC63(x int64) int64              { return x >> 63 }
func ShrC64(x int64) int64              { return x >> 64 }
func ShlC64(x int64) int64              { return x << 64 }
func UShrC63(x uint64) uint64           { return x >> 63 }
func UShrC64(x uint64) uint64           { return x >> 64 }
func ShrC7I8(x int8) int8               { return x >> 7 }
func ShrC8I8(x int8) int8               { return x >> 8 }
func ShlC7I8(x int8) int8               { return x << 7 }
func ShrC100I16(x int16) int16          { return x >> 100 }
func ShlC31I32(x int32) int32           { return x << 31 }
func ShrC31U32(x uint32) uint32         { return x >> 31 }
func ShlCountU8(x int32, n uint8) int32 { return x << n }
func ShrCountU8(x int32, n uint8) int32 { return x >> n }
func ShlCountU16(x uint64, n uint16) uint64 {
	return x << n
}
func ShrCountU64(x int64, n uint64) int64     { return x >> n }
func ShlCountU64(x uint8, n uint64) uint8     { return x << n }
func ShrCountI8(x uint32, n int8) uint32      { return x >> n }
func ShlCountI64(x int16, n int64) int16      { return x << n }
func ShrCountI16(x int64, n int16) int64      { return x >> n }
func ShlCountUptr(x uint16, n uintptr) uint16 { return x << n }
func OneShl(n uint) uint64                    { return 1 << n }
func OneShlI32(n uint) int32                  { return 1 << n }
func OneShlInt(n int) int                     { return 1 << n }
func MinusOneShr(n uint) int64                { return -1 >> n }
func ShiftMasked(x uint64, n uint) uint64     { return x << (n & 63) }
func ShiftMaskedI32(x int32, n uint) int32    { return x >> (n & 31) }
func ShiftBoth(x uint64, a, b uint) uint64    { return x<<a | x>>b }
func ShiftExpr(x int64, a, b uint8) int64     { return x << (a + b) }
func ShiftAssign(x uint32, n uint) uint32 {
	x <<= n
	x >>= n / 2
	return x
}
func RotL64(x uint64, k uint) uint64 { return x<<(k&63) | x>>((64-k)&63) }
func RotL32(x uint32, k uint) uint32 { return x<<(k&31) | x>>((32-k)&31) }
func RotL16(x uint16, k uint) uint16 { return x<<(k&15) | x>>((16-k)&15) }
func RotL8(x uint8, k uint) uint8    { return x<<(k&7) | x>>((8-k)&7) }

// ---- signed against unsigned views of the same bits

func LtAsUnsigned(a, b int64) bool   { return uint64(a) < uint64(b) }
func LtAsSigned(a, b uint64) bool    { return int64(a) < int64(b) }
func LtAsUnsigned8(a, b int8) bool   { return uint8(a) < uint8(b) }
func LtAsSigned8(a, b uint8) bool    { return int8(a) < int8(b) }
func LtAsUnsigned32(a, b int32) bool { return uint32(a) < uint32(b) }
func InRange(x, lo, hi int64) bool   { return lo <= x && x < hi }
func InRangeU(x, n int) bool         { return uint(x) < uint(n) }
func Sign(x int64) int {
	if x < 0 {
		return -1
	}
	if x > 0 {
		return 1
	}
	return 0
}
func Cmp3U(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
func AbsI8(x int8) int8 {
	if x < 0 {
		return -x
	}
	return x
}
func AddOverflows(a, b int64) bool {
	c := a + b
	return (c > a) != (b > 0)
}
func MulHiLo(a, b uint32) (hi, lo uint32) {
	p := uint64(a) * uint64(b)
	return uint32(p >> 32), uint32(p)
}

// ---- mixed widths in one expression; extension when loading small integers from memory

func Mix(a int8, b uint16, c int32, d uint64) int64 {
	return int64(a)*int64(b) + int64(c) - int64(d>>3) ^ int64(int8(b)) + int64(uint8(a))
}
func Mix2(a uint8, b int16, c uint32, d int64) uint64 {
	x := uint64(a) << 56
	x |= uint64(uint16(b)) << 40
	x ^= uint64(c) * 3
	x += uint64(d)
	return x - uint64(int64(b)) + uint64(int32(c))
}
func Narrow(x int64) (int8, int16, int32, uint8, uint16, uint32) {
	return int8(x), int16(x), int32(x), uint8(x), uint16(x), uint32(x)
}
func Widen(a int8, b int16, c int32, d uint8, e uint16, f uint32) (int64, int64, int64, int64, int64, int64) {
	return int64(a), int64(b), int64(c), int64(d), int64(e), int64(f)
}
func Chain(x int64) int64 { return int64(int8(uint16(int32(x)))) }
func ChainU(x int64) uint64 {
	return uint64(uint8(int16(uint32(x)))) + uint64(int16(uint8(x))) + uint64(int32(int8(x)))
}

type Small struct {
	A int8
	B uint8
	C int16
	D uint16
	E int32
	F uint32
	G int64
	H uint64
}

func SumSmall(s *Small) int64 {
	return int64(s.A) + int64(s.B) + int64(s.C) + int64(s.D) + int64(s.E) + int64(s.F) + s.G + int64(s.H)
}
func FillSmall(s *Small, x int64) {
	s.A = int8(x)
	s.B = uint8(x)
	s.C = int16(x)
	s.D = uint16(x)
	s.E = int32(x)
	s.F = uint32(x)
	s.G = x
	s.H = uint64(x)
}
func BumpSmall(s *Small) {
	s.A++
	s.B--
	s.C += 1000
	s.D -= 1000
	s.E *= 3
	s.F /= 3
	s.G <<= 1
	s.H >>= 1
}
func SumI8s(xs []int8) (s int) {
	for _, x := range xs {
		s += int(x)
	}
	return
}
func SumU8s(xs []uint8) (s int) {
	for _, x := range xs {
		s += int(x)
	}
	return
}
func SumI16s(xs []int16) (s int64) {
	for i := range xs {
		s += int64(xs[i])
	}
	return
}
func SumU32s(xs []uint32) (s uint64) {
	for i := 0; i < len(xs); i++ {
		s += uint64(xs[i])
	}
	return
}
func SumI32s(xs []int32) (s int64) {
	for i := len(xs) - 1; i >= 0; i-- {
		s += int64(xs[i])
	}
	return
}
func IncAll(p8 *int8, pu8 *uint8, p16 *int16, pu16 *uint16, p32 *int32, pu32 *uint32, p64 *int64, pu64 *uint64) {
	*p8++
	*pu8++
	*p16++
	*pu16++
	*p32++
	*pu32++
	*p64++
	*pu64++
}
func DecAll(p8 *int8, pu8 *uint8, p16 *int16, pu16 *uint16, p32 *int32, pu32 *uint32, p64 *int64, pu64 *uint64) {
	*p8--
	*pu8--
	*p16--
	*pu16--
	*p32--
	*pu32--
	*p64--
	*pu64--
}
func IncElems(xs []uint8, ys []int16) {
	for i := range xs {
		xs[i]++
	}
	for i := range ys {
		ys[i]--
	}
}

// ---- loops whose counters wrap

func StepsToZeroU8(start, step uint8) (n int) {
	i := start
	for {
		i += step
		n++
		if i == 0 || n > 1000 {
			return
		}
	}
}
func CountI8Up(start int8) (n int) {
	for i := start; i > 0; i++ {
		n++
	}
	return
}
func SumToU16(n uint16) (s uint16) {
	for i := uint16(0); i < n; i++ {
		s += i * i
	}
	return
}

// ---- bit utilities

func PopCount(x uint64) (n int) {
	for x != 0 {
		x &= x - 1
		n++
	}
	return
}
func LeadingZeros(x uint64) int {
	n := 0
	for i := 63; i >= 0; i-- {
		if x>>uint(i)&1 != 0 {
			break
		}
		n++
	}
	return n
}
func TrailingZeros32(x uint32) int {
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
func Bswap64(x uint64) uint64 {
	var r uint64
	for i := 0; i < 8; i++ {
		r = r<<8 | x&0xff
		x >>= 8
	}
	return r
}
func Reverse8(x uint8) uint8 {
	x = x>>4 | x<<4
	x = x>>2&0x33 | x<<2&0xcc
	x = x>>1&0x55 | x<<1&0xaa
	return x
}
func Parity(x uint32) uint32 {
	x ^= x >> 16
	x ^= x >> 8
	x ^= x >> 4
	x ^= x >> 2
	x ^= x >> 1
	return x & 1
}
func SwarPop(x uint64) uint64 {
	x -= x >> 1 & 0x5555555555555555
	x = x&0x3333333333333333 + x>>2&0x3333333333333333
	x = (x + x>>4) & 0x0f0f0f0f0f0f0f0f
	return x * 0x0101010101010101 >> 56
}
func SignExtend(x uint64, bits uint) int64 { return int64(x<<(64-bits)) >> (64 - bits) }
func BitField(x uint64, lo, n uint) uint64 { return x >> lo & (1<<n - 1) }
func SetBit(x uint64, i uint, v bool) uint64 {
	if v {
		return x | 1<<i
	}
	return x &^ (1 << i)
}
func Gcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
func PowMod(b, e, m uint64) uint64 {
	r := uint64(1)
	b %= m
	for e > 0 {
		if e&1 == 1 {
			r = r * b % m
		}
		b = b * b % m
		e >>= 1
	}
	return r
}
func Hash(xs []byte) uint32 {
	h := uint32(2166136261)
	for _, c := range xs {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}
func Xorshift(s uint64, n int) uint64 {
	for i := 0; i < n; i++ {
		s ^= s << 13
		s ^= s >> 7
		s ^= s << 17
	}
	return s
}
func Atoi(s string) (n int64, ok bool) {
	neg := false
	i := 0
	if len(s) > 0 && s[0] == '-' {
		neg = true
		i = 1
	}
	if i == len(s) {
		return 0, false
	}
	for ; i < len(s); i++ {
		c := s[i] - '0'
		if c > 9 {
			return 0, false
		}
		n = n*10 + int64(c)
	}
	if neg {
		n = -n
	}
	return n, true
}
func Itoa(x int64) string {
	if x == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	u := uint64(x)
	if x < 0 {
		u = -u
	}
	for u > 0 {
		i--
		buf[i] = byte(u%10) + '0'
		u /= 10
	}
	if x < 0 {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
func UintptrMath(p uintptr, n int) uintptr { return (p+uintptr(n)*8+7)&^7 - 1 }
func BoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func Bools(a, b int) int {
	return BoolToInt(a < b) | BoolToInt(a == b)<<1 | BoolToInt(a > b)<<2 | BoolToInt(!(a <= b))<<3 | BoolToInt(a != b && a >= b)<<4
}
