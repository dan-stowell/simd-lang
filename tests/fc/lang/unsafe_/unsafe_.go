// Package unsafe_ exercises package unsafe.
// Imports: unsafe.
package unsafe_

import "unsafe"

type T struct {
	A uint8
	B uint32
	C uint8
	D uint64
	E uint16
	S string
	F [3]uint16
	P *int32
	G float32
	H bool
	I any
	J []int8
}

type Small struct {
	X int16
	Y int8
}

type Empty struct{}

type WithEmpty struct {
	A int32
	E Empty // a zero-size final field still gets padding after it
}

// SizeOf returns unsafe.Sizeof for the kind named by k.
func SizeOf(k string) uintptr {
	switch k {
	case "bool":
		return unsafe.Sizeof(false)
	case "int8":
		return unsafe.Sizeof(int8(0))
	case "int16":
		return unsafe.Sizeof(int16(0))
	case "int32":
		return unsafe.Sizeof(int32(0))
	case "int64":
		return unsafe.Sizeof(int64(0))
	case "int":
		return unsafe.Sizeof(int(0))
	case "uintptr":
		return unsafe.Sizeof(uintptr(0))
	case "float32":
		return unsafe.Sizeof(float32(0))
	case "float64":
		return unsafe.Sizeof(float64(0))
	case "complex64":
		return unsafe.Sizeof(complex64(0))
	case "complex128":
		return unsafe.Sizeof(complex128(0))
	case "string":
		return unsafe.Sizeof("")
	case "slice":
		return unsafe.Sizeof([]int(nil))
	case "map":
		return unsafe.Sizeof(map[int]int(nil))
	case "chan":
		return unsafe.Sizeof((chan int)(nil))
	case "func":
		return unsafe.Sizeof((func())(nil))
	case "iface":
		return unsafe.Sizeof(any(nil))
	case "ptr":
		return unsafe.Sizeof((*int)(nil))
	case "unsafe.Pointer":
		return unsafe.Sizeof(unsafe.Pointer(nil))
	case "T":
		return unsafe.Sizeof(T{})
	case "Small":
		return unsafe.Sizeof(Small{})
	case "Empty":
		return unsafe.Sizeof(Empty{})
	case "WithEmpty":
		return unsafe.Sizeof(WithEmpty{})
	case "[3]Small":
		return unsafe.Sizeof([3]Small{})
	case "[0]int":
		return unsafe.Sizeof([0]int{})
	case "[7]byte":
		return unsafe.Sizeof([7]byte{})
	}
	return ^uintptr(0)
}

func AlignOf(k string) uintptr {
	var t T
	switch k {
	case "bool":
		return unsafe.Alignof(false)
	case "int16":
		return unsafe.Alignof(int16(0))
	case "int32":
		return unsafe.Alignof(int32(0))
	case "int64":
		return unsafe.Alignof(int64(0))
	case "float32":
		return unsafe.Alignof(float32(0))
	case "complex64":
		return unsafe.Alignof(complex64(0))
	case "complex128":
		return unsafe.Alignof(complex128(0))
	case "string":
		return unsafe.Alignof("")
	case "T":
		return unsafe.Alignof(t)
	case "T.A":
		return unsafe.Alignof(t.A)
	case "T.F":
		return unsafe.Alignof(t.F)
	case "Small":
		return unsafe.Alignof(Small{})
	case "Empty":
		return unsafe.Alignof(Empty{})
	case "[7]byte":
		return unsafe.Alignof([7]byte{})
	}
	return ^uintptr(0)
}

// OffsetOf returns the offset of field number i of T.
func OffsetOf(i int) uintptr {
	var t T
	offs := [...]uintptr{
		unsafe.Offsetof(t.A), unsafe.Offsetof(t.B), unsafe.Offsetof(t.C), unsafe.Offsetof(t.D),
		unsafe.Offsetof(t.E), unsafe.Offsetof(t.S), unsafe.Offsetof(t.F), unsafe.Offsetof(t.P),
		unsafe.Offsetof(t.G), unsafe.Offsetof(t.H), unsafe.Offsetof(t.I), unsafe.Offsetof(t.J),
	}
	return offs[i]
}

// ---- pointer <-> uintptr ----

func RoundTrip(p *int) *int {
	u := uintptr(unsafe.Pointer(p))
	return (*int)(unsafe.Pointer(u))
}

func AddrOf(p *int64) uintptr { return uintptr(unsafe.Pointer(p)) }

func PtrDiff(xs []int64, i, j int) uintptr {
	return uintptr(unsafe.Pointer(&xs[j])) - uintptr(unsafe.Pointer(&xs[i]))
}

func IsAligned(p unsafe.Pointer, a uintptr) bool { return uintptr(p)&(a-1) == 0 }

func AlignUp(n, a uintptr) uintptr { return (n + a - 1) &^ (a - 1) }

func NilPointer() (unsafe.Pointer, bool, uintptr) {
	var p unsafe.Pointer
	return p, p == nil, uintptr(p)
}

func SamePointer(a *int32, b *uint32) bool { return unsafe.Pointer(a) == unsafe.Pointer(b) }

// ---- unsafe.Add ----

func AddIndex(xs *[8]int32, i int) int32 {
	return *(*int32)(unsafe.Add(unsafe.Pointer(xs), i*4))
}

func AddBack(p *int16, n int) int16 {
	return *(*int16)(unsafe.Add(unsafe.Pointer(p), -n*int(unsafe.Sizeof(*p))))
}

func AddUintptr(p *byte, off uintptr) byte { return *(*byte)(unsafe.Add(unsafe.Pointer(p), off)) }

func AddInt8Off(p *byte, off int8) byte { return *(*byte)(unsafe.Add(unsafe.Pointer(p), off)) }

func ViaUintptrArith(xs *[4]uint64, i uintptr) uint64 {
	return *(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(xs)) + i*unsafe.Sizeof(xs[0])))
}

// ---- struct fields through computed offsets ----

func FieldB(t *T) uint32 {
	return *(*uint32)(unsafe.Add(unsafe.Pointer(t), unsafe.Offsetof(t.B)))
}

func SetFieldD(t *T, v uint64) {
	*(*uint64)(unsafe.Add(unsafe.Pointer(t), unsafe.Offsetof(t.D))) = v
}

func FieldS(t *T) string {
	return *(*string)(unsafe.Pointer(uintptr(unsafe.Pointer(t)) + unsafe.Offsetof(t.S)))
}

func FieldF(t *T, i int) uint16 {
	return *(*uint16)(unsafe.Add(unsafe.Pointer(t), unsafe.Offsetof(t.F)+uintptr(i)*unsafe.Sizeof(t.F[0])))
}

func SetFieldP(t *T, p *int32) {
	*(**int32)(unsafe.Add(unsafe.Pointer(t), unsafe.Offsetof(t.P))) = p
}

// ReadAt reads size bytes (1, 2, 4 or 8) at the given offset, zero-extended.
func ReadAt(t *T, off uintptr, size int) uint64 {
	p := unsafe.Add(unsafe.Pointer(t), off)
	switch size {
	case 1:
		return uint64(*(*uint8)(p))
	case 2:
		return uint64(*(*uint16)(p))
	case 4:
		return uint64(*(*uint32)(p))
	}
	return *(*uint64)(p)
}

// SumD walks a slice of T by Sizeof steps.
func SumD(ts []T) (sum uint64) {
	if len(ts) == 0 {
		return 0
	}
	p := unsafe.Pointer(&ts[0])
	for i := 0; i < len(ts); i++ {
		sum += (*T)(p).D + uint64((*T)(p).A)
		p = unsafe.Add(p, unsafe.Sizeof(ts[0]))
	}
	return
}

func ContainerOf(pd *uint64) *T {
	return (*T)(unsafe.Add(unsafe.Pointer(pd), -int(unsafe.Offsetof(T{}.D))))
}

// ---- reinterpretation ----

func Float64Bits(f float64) uint64     { return *(*uint64)(unsafe.Pointer(&f)) }
func Float64FromBits(b uint64) float64 { return *(*float64)(unsafe.Pointer(&b)) }
func Float32Bits(f float32) uint32     { return *(*uint32)(unsafe.Pointer(&f)) }
func BytesOfUint32(x uint32) [4]byte   { return *(*[4]byte)(unsafe.Pointer(&x)) }
func Uint64OfBytes(b [8]byte) uint64   { return *(*uint64)(unsafe.Pointer(&b)) }
func LittleEndian() bool {
	x := uint16(1)
	return *(*byte)(unsafe.Pointer(&x)) == 1
}
func IntAsUint(p *int) *uint { return (*uint)(unsafe.Pointer(p)) }

func SmallToBytes(s Small) [4]byte {
	s2 := s
	b := (*[4]byte)(unsafe.Pointer(&s2))
	return [4]byte{b[0], b[1], b[2], 0} // byte 3 is padding
}

func StringWords(s string) (unsafe.Pointer, int) {
	w := (*[2]uintptr)(unsafe.Pointer(&s))
	return unsafe.Pointer(unsafe.StringData(s)), int(w[1])
}

func SliceWords(xs []int) (int, int, bool) {
	w := (*[3]uintptr)(unsafe.Pointer(&xs))
	return int(w[1]), int(w[2]), w[0] == uintptr(unsafe.Pointer(unsafe.SliceData(xs)))
}

func SameDynType(a, b any) bool {
	wa := (*[2]unsafe.Pointer)(unsafe.Pointer(&a))
	wb := (*[2]unsafe.Pointer)(unsafe.Pointer(&b))
	return wa[0] == wb[0]
}

// ---- Slice, SliceData, String, StringData ----

func SliceFrom(p *int16, n int) []int16      { return unsafe.Slice(p, n) }
func SliceFrom64(p *int16, n uint64) []int16 { return unsafe.Slice(p, n) }
func SliceFromInt8(p *Small, n int8) []Small { return unsafe.Slice(p, n) }
func SliceNil(n int) []int                   { return unsafe.Slice((*int)(nil), n) }
func SliceOfArray(a *[6]int32, lo, n int) []int32 {
	return unsafe.Slice(&a[lo], n)
}
func SliceEmpty(p *Empty, n int) int { return len(unsafe.Slice(p, n)) }

func DataOf(xs []int32) *int32 { return unsafe.SliceData(xs) }

func BytesToString(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }
func StringOf(p *byte, n int) string { return unsafe.String(p, n) }

func StringBytes(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }

func SumStringBytes(s string) (sum int) {
	p := unsafe.StringData(s)
	for i := 0; i < len(s); i++ {
		sum += int(*(*byte)(unsafe.Add(unsafe.Pointer(p), i)))
	}
	return
}

func Uint32sAsBytes(xs []uint32) []byte {
	if len(xs) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(xs))), len(xs)*int(unsafe.Sizeof(xs[0])))
}

func CopyBytes(dst, src unsafe.Pointer, n int) {
	copy(unsafe.Slice((*byte)(dst), n), unsafe.Slice((*byte)(src), n))
}

func CopyT(src *T) (dst T) {
	CopyBytes(unsafe.Pointer(&dst.A), unsafe.Pointer(&src.A), int(unsafe.Offsetof(src.S)))
	dst.S = src.S
	return
}

// ---- unsafe.Pointer as an ordinary value ----

func PointerKey(a, b *int) int {
	m := map[unsafe.Pointer]int{unsafe.Pointer(a): 1}
	m[unsafe.Pointer(b)] += 2
	return m[unsafe.Pointer(a)]*10 + len(m)
}

func PointerInAny(p *int) bool {
	var x any = unsafe.Pointer(p)
	q, ok := x.(unsafe.Pointer)
	return ok && (*int)(q) == p
}

type holder struct {
	p unsafe.Pointer
	n int
}

func Hold(xs []float64) float64 {
	h := holder{unsafe.Pointer(unsafe.SliceData(xs)), len(xs)}
	hs := []holder{h, {nil, 0}}
	s := 0.0
	for _, v := range unsafe.Slice((*float64)(hs[0].p), hs[0].n) {
		s += v
	}
	return s
}
