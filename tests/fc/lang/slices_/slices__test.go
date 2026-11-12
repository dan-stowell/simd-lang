package slices__test

import (
	"reflect"
	"testing"

	"fclang/internal/chk"
	"fclang/slices_"
)

func TestMain(m *testing.M) { chk.Main(m) }

var (
	sizes = []any{0, 1, 2, 5, 17, 100, 1000, -1, 1 << 62}
	loops = []any{0, 1, 2, 5, 17, 100, 1000, -1}
	idx   = []any{0, 1, 2, 3, 4, 5, 8, 17, 18, 64, -1, 1 << 62}
)

// Every exported function against its stock-compiled copy (slices__ref_test.go).  chk makes slices of
// length 0..64 (nil included) with up to two spare elements of capacity, and compares results, panics, and
// the arguments after the call (so writes through slices and pointers are checked too).
func TestAuto(t *testing.T) {
	i1 := []chk.Opt{chk.Arg(1, idx...), chk.Limit(1500)}
	i12 := []chk.Opt{chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Limit(2500)}
	i123 := []chk.Opt{chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Arg(3, idx...), chk.Limit(4000)}
	chk.All(t, autoFuncs, map[string][]chk.Opt{
		"Make": {chk.Arg(0, sizes...)}, "MakeCap": {chk.Arg(0, sizes...), chk.Arg(1, sizes...)}, "MakeI64": {chk.Arg(0, sizes...), chk.Arg(1, sizes...)},
		"MakeLenCap": {chk.Arg(0, sizes...), chk.Arg(1, sizes...)}, "MakeNodes": {chk.Arg(0, sizes...)}, "MakeBig": {chk.Arg(0, sizes...)},
		"AppendN": nil, "AppendCaps": nil, "AppendCapsBytes": nil, "AppendCapOf": nil, // capacities: by hand, below
		"Grid":     {chk.Arg(0, 0, 1, 3, 17, -1), chk.Arg(1, 0, 1, 3, 17, -1)},
		"Triangle": {chk.Arg(0, 0, 1, 2, 5, 40, -1)}, "Chain": {chk.Arg(0, 0, 1, 2, 5, 100, -1)}, "NodesByValue": {chk.Arg(0, 0, 1, 2, 5, 100, -1)},
		"Sieve": {chk.Arg(0, loops...)},
		"At":    i1, "Set": i1, "AddAt": i1, "IncAt": i1, "SetStr": i1, "NodeName": i1, "NodeNext": i1, "SliceFrom": i1, "SliceTo": i1, "Reslice": i1,
		"AppendSelfTail": i1, "Delete": i1, "CopyOverlapUp": i1, "CopyOverlapDown": i1, "Insert": i1,
		"Slice": i12, "SliceCap": i12, "SliceU": i12, "SliceBytes": i12, "SliceNodes": i12, "SliceAlias": i12, "Swap": i12, "At2": i12, "NodeVal": i12, "Slice3To": i12, "ClearPart": i12,
		"Slice3": i123, "Slice3Cap": i123,
		"SliceArr":      {chk.Arg(1, 0, 1, 3, 5, 6, -1), chk.Arg(2, 0, 1, 3, 5, 6, -1)},
		"SliceArr3":     {chk.Arg(1, 0, 1, 3, 5, 6, -1), chk.Arg(2, 0, 1, 3, 5, 6, -1), chk.Arg(3, 0, 1, 3, 5, 6, -1), chk.Limit(1000)},
		"SliceArrLocal": {chk.Arg(0, 0, 1, 3, 6, 7, -1), chk.Arg(1, 0, 1, 3, 6, 7, -1)},
		"ArrAt":         {chk.Arg(1, 0, 1, 4, 5, -1, 1<<62)}, "ArrPtrAt": {chk.Arg(1, 0, 1, 4, 5, -1, 1<<62)}, "ArrPtrSet": {chk.Arg(1, 0, 1, 4, 5, -1, 1<<62)},
		"ArrElemSwap": {chk.Arg(1, 0, 1, 3, 4, -1), chk.Arg(2, 0, 1, 3, 4, -1)},
	})
}

type errCase struct {
	name string
	f    func()
	want string
}

func TestPanics(t *testing.T) {
	s := []int{1, 2, 3}
	const be = "panic runtime.boundsError: runtime error: "
	for _, c := range []errCase{
		{"s[3]", func() { slices_.At(s, 3) }, be + "index out of range [3] with length 3"},
		{"s[-1]", func() { slices_.At(s, -1) }, be + "index out of range [-1]"},
		{"s[uint64 max]", func() { slices_.AtU64([]byte{1}, 1<<64-1) }, be + "index out of range [18446744073709551615] with length 1"},
		{"nil[0]", func() { slices_.At(nil, 0) }, be + "index out of range [0] with length 0"},
		{"s[1:5]", func() { slices_.Slice(s, 1, 5) }, be + "slice bounds out of range [:5] with capacity 3"},
		{"s[2:1]", func() { slices_.Slice(s, 2, 1) }, be + "slice bounds out of range [2:1]"},
		{"s[-1:2]", func() { slices_.Slice(s, -1, 2) }, be + "slice bounds out of range [-1:]"},
		{"s[4:]", func() { slices_.SliceFrom(s, 4) }, be + "slice bounds out of range [4:3]"},
		{"s[:-1]", func() { slices_.SliceTo(s, -1) }, be + "slice bounds out of range [:-1]"},
		{"s[0:1:4]", func() { slices_.Slice3(s, 0, 1, 4) }, be + "slice bounds out of range [::4] with capacity 3"},
		{"s[0:3:2]", func() { slices_.Slice3(s, 0, 3, 2) }, be + "slice bounds out of range [:3:2]"},
		{"s[2:1:3]", func() { slices_.Slice3(s, 2, 1, 3) }, be + "slice bounds out of range [2:1:]"},
		{"s[0:1:-1]", func() { slices_.Slice3(s, 0, 1, -1) }, be + "slice bounds out of range [::-1]"},
		{"arr[:6]", func() { slices_.SliceArr(&[5]int{}, 0, 6) }, be + "slice bounds out of range [:6] with length 5"},
		{"arr[5]", func() { slices_.ArrAt([5]int{}, 5) }, be + "index out of range [5] with length 5"},
		{"[4]int(s)", func() { slices_.ToArray(s) }, be + "cannot convert slice with length 3 to array or pointer to array with length 4"},
		{"(*[4]int)(nil)", func() { slices_.ToArrayPtr(nil) }, be + "cannot convert slice with length 0 to array or pointer to array with length 4"},
		{"nil array pointer [:]", func() { slices_.SliceNilPtr(nil) }, "panic runtime.errorString: runtime error: invalid memory address or nil pointer dereference"},
		{"nil array pointer [i]", func() { slices_.ArrPtrAt(nil, 1) }, "panic runtime.errorString: runtime error: invalid memory address or nil pointer dereference"},
		{"make(-1)", func() { slices_.Make(-1) }, "panic runtime.errorString: runtime error: makeslice: len out of range"},
		{"make(1, 0)", func() { slices_.MakeCap(1, 0) }, "panic runtime.errorString: runtime error: makeslice: cap out of range"},
		{"make(huge)", func() { slices_.MakeCap(0, 1<<62) }, "panic runtime.errorString: runtime error: makeslice: cap out of range"},
	} {
		if got := chk.Try(c.f); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestByHand(t *testing.T) {
	if a, b, c, d := slices_.LenNilEmpty(); a != 0 || b != 0 || !c || d {
		t.Error("nil and empty")
	}
	if a, b, c, d := slices_.NilSliceOps(); a != 0 || b != 0 || c != nil || d != 0 {
		t.Error("operations on a nil slice")
	}
	if e, isNil := slices_.SliceEmptyAtEnd([]int{1, 2}); len(e) != 0 || isNil {
		t.Error("s[len(s):] is empty, not nil")
	}
	if slices_.Slice(nil, 0, 0) != nil || slices_.AppendNil(nil) != nil || slices_.AppendSlice(nil, []int{}) != nil || slices_.Clone(nil) != nil {
		t.Error("nil stays nil")
	}
	s := make([]int, 3, 10)
	if r := slices_.Reslice(s, 10); len(r) != 10 || cap(r) != 10 {
		t.Error("reslice up to cap")
	}
	if l, c := slices_.Slice3Cap(s, 1, 2, 7); l != 1 || c != 6 {
		t.Error("3-index cap", l, c)
	}
	if l, c := slices_.SliceCap(s, 2, 3); l != 1 || c != 8 {
		t.Error("2-index cap", l, c)
	}
	x := []int{1, 2, 3}
	if got := slices_.AppendSelf(x); !reflect.DeepEqual(got, []int{1, 2, 3, 1, 2, 3}) {
		t.Error("self-append", got)
	}
	y := make([]int, 3, 8)
	copy(y, x)
	if got := slices_.AppendSelfTail(y, 1); !reflect.DeepEqual(got, []int{1, 1, 2, 3}) {
		t.Error("append(s[:1], s...) must behave as a memmove", got)
	}
	z := []int{1, 2, 3, 4, 5}
	if n := slices_.CopyOverlapUp(z, 2); n != 3 || !reflect.DeepEqual(z, []int{1, 2, 1, 2, 3}) {
		t.Error("overlapping copy up", z)
	}
	z = []int{1, 2, 3, 4, 5}
	if n := slices_.CopyOverlapDown(z, 2); n != 3 || !reflect.DeepEqual(z, []int{3, 4, 5, 4, 5}) {
		t.Error("overlapping copy down", z)
	}
	if string(slices_.AppendStr([]byte("ab"), "cdé")) != "abcdé" || slices_.CopyStr(make([]byte, 2), "xyz") != 2 {
		t.Error("string into []byte")
	}
	if a, b := slices_.ArrAssign([4]int{1, 2, 3, 4}); a != [4]int{1, 2, 3, 4} || b != [4]int{-1, 2, 3, 4} {
		t.Error("array assignment copies")
	}
	if n, a := slices_.ArrCall([4]int{1, 2, 3, 4}); n != 51 || a != [4]int{1, 2, 3, 4} {
		t.Error("array parameter is a copy")
	}
	if slices_.RangeArrayByValue([4]int{1, 2, 3, 4}) != 1+2+3+4+0+1+2+3+100 {
		t.Error("range over an array ranges over a copy")
	}
	if slices_.RangeArrayPtr(&[4]int{1, 2, 3, 4}) != 1+2+3+100 {
		t.Error("range over an array pointer sees writes")
	}
	if slices_.RangeSliceSeesWrites([]int{1, 2, 3}) != 1+1002+1003 {
		t.Error("range over a slice sees writes")
	}
	if slices_.RangeCopyOfLen([]int{5, 6, 7}) != 3+6 {
		t.Error("range evaluates the length once")
	}
	if slices_.ArrLit(3) != [5]int{0, 6, 0, 0, 3} || slices_.ArrOfArr(1) != [2][3]int{{1, 1, 1}, {0, 2, 0}} {
		t.Error("array literals")
	}
	if !slices_.ArrEqF([2]float64{1, 0}, [2]float64{1, -0.0}) || slices_.ArrEq([4]int{1, 2, 3, 4}, [4]int{1, 2, 3, 5}) {
		t.Error("array ==")
	}
	if slices_.ToArrayAlias([]int{1, 2, 3}) != 55 || slices_.ToArray([]int{1, 2, 3, 4, 5}) != [4]int{1, 2, 3, 4} || !slices_.ToArrayPtr0(nil) || slices_.ToArrayPtr0([]int{}) {
		t.Error("slice to array conversions")
	}
	// growth: capacities after append are the implementation's business (the stock compiler starts some
	// appends in a 32-byte stack buffer, so a first append to a nil []int64 can give capacity 4 where
	// growslice alone gives 1); only amortized growth is required here
	for _, caps := range [][]int{slices_.AppendCaps(300000), slices_.AppendCapsBytes(300000)} {
		for i := range caps {
			if i > 0 && caps[i] <= caps[i-1] {
				t.Error("capacities must grow:", caps)
			}
		}
		if len(caps) > 60 || caps[len(caps)-1] < 300000 {
			t.Error("growth is not amortized:", caps)
		}
	}
	if s, grows := slices_.AppendN(1000); len(s) != 1000 || s[999] != 999*999 || grows > 30 {
		t.Error("AppendN", len(s), grows)
	}
	if c := slices_.AppendCapOf(5, 1000); c < 1005 {
		t.Error("AppendCapOf", c)
	}
	if n, sum := slices_.Variadic(); n != 0 || sum != 0 {
		t.Error("variadic with no arguments")
	}
	if !slices_.VariadicIsNil() || slices_.VariadicIsNil([]string{}...) || slices_.VariadicAlias([]int{4}) != 5 {
		t.Error("variadic: no arguments is nil; a spread slice is passed as is")
	}
}
