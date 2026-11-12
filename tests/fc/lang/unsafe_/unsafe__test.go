package unsafe__test

import (
	"fmt"
	"math"
	"reflect"
	"runtime"
	"testing"
	"unsafe"

	u "fclang/unsafe_"
)

func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			_, isRT := r.(runtime.Error)
			if e, ok := r.(error); ok {
				msg = fmt.Sprintf("runtime=%v error: %s", isRT, e.Error())
			} else {
				msg = fmt.Sprintf("runtime=%v value: %v", isRT, r)
			}
		}
	}()
	f()
	return "no panic"
}

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", name, got, want)
	}
}

// mirror of u.T, laid out by the stock compiler
type mirror struct {
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

func TestSizes(t *testing.T) {
	var m mirror
	cases := []struct {
		k     string
		stock uintptr
		amd64 uintptr
	}{
		{"bool", unsafe.Sizeof(false), 1}, {"int8", unsafe.Sizeof(int8(0)), 1}, {"int16", unsafe.Sizeof(int16(0)), 2},
		{"int32", unsafe.Sizeof(int32(0)), 4}, {"int64", unsafe.Sizeof(int64(0)), 8}, {"int", unsafe.Sizeof(int(0)), 8},
		{"uintptr", unsafe.Sizeof(uintptr(0)), 8}, {"float32", unsafe.Sizeof(float32(0)), 4}, {"float64", unsafe.Sizeof(float64(0)), 8},
		{"complex64", unsafe.Sizeof(complex64(0)), 8}, {"complex128", unsafe.Sizeof(complex128(0)), 16},
		{"string", unsafe.Sizeof(""), 16}, {"slice", unsafe.Sizeof([]int(nil)), 24}, {"map", unsafe.Sizeof(map[int]int(nil)), 8},
		{"chan", unsafe.Sizeof((chan int)(nil)), 8}, {"func", unsafe.Sizeof((func())(nil)), 8}, {"iface", unsafe.Sizeof(any(nil)), 16},
		{"ptr", unsafe.Sizeof((*int)(nil)), 8}, {"unsafe.Pointer", unsafe.Sizeof(unsafe.Pointer(nil)), 8},
		{"T", unsafe.Sizeof(m), 112}, {"Small", unsafe.Sizeof(u.Small{}), 4}, {"Empty", unsafe.Sizeof(u.Empty{}), 0},
		{"WithEmpty", unsafe.Sizeof(u.WithEmpty{}), 8}, {"[3]Small", unsafe.Sizeof([3]u.Small{}), 12},
		{"[0]int", unsafe.Sizeof([0]int{}), 0}, {"[7]byte", unsafe.Sizeof([7]byte{}), 7},
	}
	for _, c := range cases {
		if got := u.SizeOf(c.k); got != c.stock || got != c.amd64 {
			t.Errorf("SizeOf(%s) = %d, stock %d, expected %d", c.k, got, c.stock, c.amd64)
		}
	}
	aligns := map[string]uintptr{
		"bool": 1, "int16": 2, "int32": 4, "int64": 8, "float32": 4, "complex64": 4, "complex128": 8, "string": 8,
		"T": unsafe.Alignof(m), "T.A": unsafe.Alignof(m.A), "T.F": unsafe.Alignof(m.F), "Small": 2, "Empty": 1, "[7]byte": 1,
	}
	for k, want := range aligns {
		eq(t, "AlignOf "+k, u.AlignOf(k), want)
	}
	eq(t, "AlignOf T", []uintptr{u.AlignOf("T"), u.AlignOf("T.A"), u.AlignOf("T.F")}, []uintptr{8, 1, 2})
	stock := []uintptr{
		unsafe.Offsetof(m.A), unsafe.Offsetof(m.B), unsafe.Offsetof(m.C), unsafe.Offsetof(m.D),
		unsafe.Offsetof(m.E), unsafe.Offsetof(m.S), unsafe.Offsetof(m.F), unsafe.Offsetof(m.P),
		unsafe.Offsetof(m.G), unsafe.Offsetof(m.H), unsafe.Offsetof(m.I), unsafe.Offsetof(m.J),
	}
	hand := []uintptr{0, 4, 8, 16, 24, 32, 48, 56, 64, 68, 72, 88}
	for i := range stock {
		if got := u.OffsetOf(i); got != stock[i] || got != hand[i] {
			t.Errorf("OffsetOf(%d) = %d, stock %d, expected %d", i, got, stock[i], hand[i])
		}
	}
	eq(t, "OffsetOf oob", catch(func() { u.OffsetOf(12) }), catch(func() { _ = stock[len(stock)] }))
}

func TestPointers(t *testing.T) {
	x := 42
	if p := u.RoundTrip(&x); p != &x || *p != 42 {
		t.Error("RoundTrip")
	}
	if u.RoundTrip(nil) != nil {
		t.Error("RoundTrip nil")
	}
	xs := make([]int64, 10)
	eq(t, "AddrOf", u.AddrOf(&xs[3]), uintptr(unsafe.Pointer(&xs[3])))
	eq(t, "PtrDiff", u.PtrDiff(xs, 2, 9), uintptr(56))
	eq(t, "PtrDiff neg", u.PtrDiff(xs, 1, 0), ^uintptr(7))
	eq(t, "IsAligned", []bool{u.IsAligned(unsafe.Pointer(&xs[1]), 8), u.IsAligned(unsafe.Add(unsafe.Pointer(&xs[1]), 4), 8), u.IsAligned(unsafe.Add(unsafe.Pointer(&xs[1]), 4), 4)}, []bool{true, false, true})
	eq(t, "AlignUp", []uintptr{u.AlignUp(0, 8), u.AlignUp(1, 8), u.AlignUp(8, 8), u.AlignUp(13, 4), u.AlignUp(17, 16)}, []uintptr{0, 8, 8, 16, 32})
	p, isnil, v := u.NilPointer()
	eq(t, "NilPointer", []any{p, isnil, v}, []any{unsafe.Pointer(nil), true, uintptr(0)})
	var a int32
	var b uint32
	eq(t, "SamePointer", []bool{u.SamePointer(&a, (*uint32)(unsafe.Pointer(&a))), u.SamePointer(&a, &b), u.SamePointer(nil, nil)}, []bool{true, false, true})
}

func TestAdd(t *testing.T) {
	arr := [8]int32{10, -11, 12, -13, 14, -15, 16, math.MinInt32}
	for i, want := range arr {
		eq(t, "AddIndex", u.AddIndex(&arr, i), want)
	}
	i16 := []int16{100, 200, 300, -400}
	eq(t, "AddBack", []int16{u.AddBack(&i16[3], 0), u.AddBack(&i16[3], 1), u.AddBack(&i16[3], 3), u.AddBack(&i16[0], -2)}, []int16{-400, 300, 100, 300})
	bs := []byte("0123456789")
	eq(t, "AddUintptr", []byte{u.AddUintptr(&bs[0], 0), u.AddUintptr(&bs[2], 7)}, []byte{'0', '9'})
	eq(t, "AddInt8Off", []byte{u.AddInt8Off(&bs[5], -5), u.AddInt8Off(&bs[5], 4), u.AddInt8Off(&bs[5], 0)}, []byte{'0', '9', '5'})
	u64 := [4]uint64{1, 1 << 63, 3, math.MaxUint64}
	for i, want := range u64 {
		eq(t, "ViaUintptrArith", u.ViaUintptrArith(&u64, uintptr(i)), want)
	}
}

func TestFields(t *testing.T) {
	n := int32(-5)
	v := u.T{A: 0xa1, B: 0xb2b3b4b5, C: 0xc6, D: 0xd0d1d2d3d4d5d6d7, E: 0xe8e9, S: "field", F: [3]uint16{1, 0x8002, 3}, G: 1.5, H: true, I: 7, J: []int8{-1}}
	eq(t, "FieldB", u.FieldB(&v), uint32(0xb2b3b4b5))
	eq(t, "FieldS", u.FieldS(&v), "field")
	eq(t, "FieldF", []uint16{u.FieldF(&v, 0), u.FieldF(&v, 1), u.FieldF(&v, 2)}, []uint16{1, 0x8002, 3})
	u.SetFieldD(&v, 0x1122334455667788)
	eq(t, "SetFieldD", v.D, uint64(0x1122334455667788))
	u.SetFieldP(&v, &n)
	if v.P != &n || *v.P != -5 {
		t.Error("SetFieldP")
	}
	reads := []struct {
		off  uintptr
		size int
		want uint64
	}{
		{0, 1, 0xa1}, {4, 4, 0xb2b3b4b5}, {4, 2, 0xb4b5}, {6, 2, 0xb2b3}, {7, 1, 0xb2}, {8, 1, 0xc6},
		{16, 8, 0x1122334455667788}, {20, 4, 0x11223344}, {24, 2, 0xe8e9}, {40, 8, 5}, {50, 2, 0x8002},
		{64, 4, uint64(math.Float32bits(1.5))}, {68, 1, 1}, {96, 8, 1}, {104, 8, 1},
	}
	for _, r := range reads {
		eq(t, fmt.Sprintf("ReadAt(%d,%d)", r.off, r.size), u.ReadAt(&v, r.off, r.size), r.want)
	}
	eq(t, "ReadAt P", u.ReadAt(&v, 56, 8), uint64(uintptr(unsafe.Pointer(&n))))
	ts := make([]u.T, 5)
	var want uint64
	for i := range ts {
		ts[i].A = uint8(i + 1)
		ts[i].D = uint64(i) << 60
		ts[i].B = 0xffffffff
		want += uint64(i+1) + uint64(i)<<60
	}
	eq(t, "SumD", u.SumD(ts), want)
	eq(t, "SumD empty", u.SumD(nil), uint64(0))
	if u.ContainerOf(&ts[3].D) != &ts[3] {
		t.Error("ContainerOf")
	}
	c := u.CopyT(&v)
	if c.A != v.A || c.B != v.B || c.C != v.C || c.D != v.D || c.E != v.E || c.S != "field" || c.P != nil || c.F != [3]uint16{} {
		t.Errorf("CopyT %+v", c)
	}
	eq(t, "nil field", catch(func() { u.FieldB(nil) }), catch(func() { sink32 = *(*uint32)(unsafe.Add(unsafe.Pointer(nilT), 4)) }))
}

var (
	nilT   *u.T
	sink32 uint32
	sinkS  []int16
)

func TestReinterpret(t *testing.T) {
	for _, f := range []float64{0, math.Copysign(0, -1), 1, -2.5, math.Inf(1), math.NaN(), math.SmallestNonzeroFloat64, math.MaxFloat64} {
		eq(t, "Float64Bits", u.Float64Bits(f), math.Float64bits(f))
		eq(t, "Float64FromBits", math.Float64bits(u.Float64FromBits(math.Float64bits(f))), math.Float64bits(f))
		eq(t, "Float32Bits", u.Float32Bits(float32(f)), math.Float32bits(float32(f)))
	}
	eq(t, "BytesOfUint32", u.BytesOfUint32(0x04030201), [4]byte{1, 2, 3, 4})
	eq(t, "Uint64OfBytes", u.Uint64OfBytes([8]byte{1, 2, 3, 4, 5, 6, 7, 0x80}), uint64(0x8007060504030201))
	eq(t, "LittleEndian", u.LittleEndian(), true)
	i := -1
	pu := u.IntAsUint(&i)
	*pu >>= 1
	eq(t, "IntAsUint", i, math.MaxInt)
	eq(t, "SmallToBytes", u.SmallToBytes(u.Small{X: 0x1234, Y: -2}), [4]byte{0x34, 0x12, 0xfe, 0})
	s := "hello, world"
	p, n := u.StringWords(s[2:7])
	if p != unsafe.Pointer(unsafe.StringData(s[2:])) || n != 5 {
		t.Error("StringWords")
	}
	xs := make([]int, 3, 10)
	l, c, same := u.SliceWords(xs[1:])
	eq(t, "SliceWords", []any{l, c, same}, []any{2, 9, true})
	l, c, same = u.SliceWords(nil)
	eq(t, "SliceWords nil", []any{l, c, same}, []any{0, 0, true})
	eq(t, "SameDynType", []bool{u.SameDynType(1, 2), u.SameDynType(1, "a"), u.SameDynType(nil, nil), u.SameDynType(&i, (*int)(nil)), u.SameDynType(int8(1), uint8(1))}, []bool{true, false, true, true, false})
}

func TestSliceString(t *testing.T) {
	a := []int16{1, 2, 3, 4, 5}
	s := u.SliceFrom(&a[1], 3)
	if len(s) != 3 || cap(s) != 3 || &s[0] != &a[1] || s[2] != 4 {
		t.Error("SliceFrom")
	}
	if s := u.SliceFrom(&a[1], 0); s == nil || len(s) != 0 {
		t.Error("SliceFrom zero len must be non-nil")
	}
	if s := u.SliceFrom(nil, 0); s != nil {
		t.Error("SliceFrom(nil, 0) must be nil")
	}
	if s := u.SliceFrom64(&a[0], 5); len(s) != 5 || s[4] != 5 {
		t.Error("SliceFrom64")
	}
	sm := []u.Small{{1, 2}, {3, 4}}
	if s := u.SliceFromInt8(&sm[0], 2); len(s) != 2 || s[1] != (u.Small{3, 4}) {
		t.Error("SliceFromInt8")
	}
	if u.SliceNil(0) != nil {
		t.Error("SliceNil(0)")
	}
	arr := [6]int32{0, 10, 20, 30, 40, 50}
	eq(t, "SliceOfArray", u.SliceOfArray(&arr, 2, 3), []int32{20, 30, 40})
	eq(t, "SliceEmpty", []int{u.SliceEmpty(&u.Empty{}, 1000), u.SliceEmpty(nil, 0)}, []int{1000, 0})

	var np *int16
	var ne *u.Empty
	var nb *byte
	neg, one, huge, top := -1, 1, uint64(1)<<63-1, uint64(1)<<63
	panics := []struct {
		name string
		got  func()
		want func()
	}{
		{"Slice len<0", func() { u.SliceFrom(&a[0], -1) }, func() { _ = unsafe.Slice(&a[0], neg) }},
		{"Slice nil len>0", func() { u.SliceFrom(nil, 1) }, func() { _ = unsafe.Slice(np, one) }},
		{"Slice nil len<0", func() { u.SliceFrom(nil, -1) }, func() { _ = unsafe.Slice(np, neg) }},
		{"Slice huge", func() { u.SliceFrom64(&a[0], huge) }, func() { sinkS = unsafe.Slice(&a[0], huge) }},
		{"Slice huge 2", func() { u.SliceFrom64(&a[0], 1<<63) }, func() { sinkS = unsafe.Slice(&a[0], top) }},
		{"Slice int8 neg", func() { u.SliceFromInt8(&sm[0], -1) }, func() { _ = unsafe.Slice(&sm[0], int8(neg)) }},
		{"SliceNil", func() { u.SliceNil(3) }, func() { _ = unsafe.Slice((*int)(nil), one+2) }},
		{"Slice empty nil", func() { u.SliceEmpty(nil, 1) }, func() { _ = unsafe.Slice(ne, one) }},
		{"Slice empty neg", func() { u.SliceEmpty(&u.Empty{}, -1) }, func() { _ = unsafe.Slice(&u.Empty{}, neg) }},
		{"SliceOfArray oob", func() { u.SliceOfArray(&arr, 6, 0) }, func() { _ = &arr[one+5] }},
		{"String nil", func() { u.StringOf(nil, 1) }, func() { _ = unsafe.String(nb, one) }},
		{"String neg", func() { u.StringOf(&[]byte("x")[0], -1) }, func() { _ = unsafe.String(&[]byte("x")[0], neg) }},
	}
	for _, p := range panics {
		got, want := catch(p.got), catch(p.want)
		if got != want || got == "no panic" {
			t.Errorf("%s: got %q, want %q", p.name, got, want)
		}
	}

	i32 := []int32{7, 8, 9}
	if u.DataOf(i32) != &i32[0] || u.DataOf(i32[1:]) != &i32[1] || u.DataOf(nil) != nil || u.DataOf(i32[:0]) != &i32[0] {
		t.Error("DataOf")
	}
	b := []byte("bytes!")
	str := u.BytesToString(b)
	eq(t, "BytesToString", str, "bytes!")
	b[0] = 'B' // the string aliases the bytes
	eq(t, "BytesToString alias", str, "Bytes!")
	eq(t, "BytesToString empty", u.BytesToString(nil), "")
	eq(t, "StringOf", u.StringOf(&b[1], 3), "yte")
	eq(t, "StringOf nil 0", u.StringOf(nil, 0), "")
	sb := u.StringBytes("héllo")
	eq(t, "StringBytes", sb, []byte("héllo"))
	eq(t, "StringBytes cap", cap(sb), 6)
	if u.StringBytes("") != nil && len(u.StringBytes("")) != 0 {
		t.Error("StringBytes empty")
	}
	eq(t, "SumStringBytes", u.SumStringBytes("abc\xff"), 97+98+99+255)
	w := []uint32{0x04030201, 0xfffefdfc}
	eq(t, "Uint32sAsBytes", u.Uint32sAsBytes(w), []byte{1, 2, 3, 4, 0xfc, 0xfd, 0xfe, 0xff})
	u.Uint32sAsBytes(w)[4] = 0
	eq(t, "Uint32sAsBytes write", w[1], uint32(0xfffefd00))
	eq(t, "Uint32sAsBytes nil", u.Uint32sAsBytes(nil), []byte(nil))
	src, dst := [5]uint16{1, 2, 3, 4, 5}, [5]uint16{}
	u.CopyBytes(unsafe.Pointer(&dst), unsafe.Pointer(&src[1]), 6)
	eq(t, "CopyBytes", dst, [5]uint16{2, 3, 4, 0, 0})
}

func TestPointerValues(t *testing.T) {
	x, y := 1, 2
	eq(t, "PointerKey", []int{u.PointerKey(&x, &y), u.PointerKey(&x, &x), u.PointerKey(nil, &x)}, []int{12, 31, 12})
	eq(t, "PointerInAny", []bool{u.PointerInAny(&x), u.PointerInAny(nil)}, []bool{true, true})
	eq(t, "Hold", u.Hold([]float64{1.5, 2.5, -1}), 3.0)
	eq(t, "Hold nil", u.Hold(nil), 0.0)
}
