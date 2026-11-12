package maps__test

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"

	"fclang/maps_"
)

func eq[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func deq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

// catch runs f and describes its panic ("" when there is none).
func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("%T: %v", r, r)
		}
	}()
	f()
	return ""
}

// count is the stock compiler's version of the Count functions.
func count[K comparable](xs []K) map[K]int {
	m := map[K]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func TestBasic(t *testing.T) {
	m := maps_.Squares(10)
	eq(t, "Squares len", len(m), 10)
	for i := -1; i <= 10; i++ {
		v, ok := maps_.GetOk(m, i)
		in := i >= 0 && i < 10
		eq(t, "GetOk ok", ok, in)
		eq(t, "Has", maps_.Has(m, i), in)
		if in {
			eq(t, "GetOk v", v, i*i)
			eq(t, "Get", maps_.Get(m, i), i*i)
			eq(t, "stock read", m[i], i*i)
		} else {
			eq(t, "GetOk zero", v, 0)
			eq(t, "Get zero", maps_.Get(m, i), 0)
		}
	}
	eq(t, "Squares(0)", len(maps_.Squares(0)), 0)
	eq(t, "Squares(0) not nil", maps_.Squares(0) != nil, true)
	eq(t, "IsNil nil", maps_.IsNil(nil), true)
	eq(t, "IsNil empty", maps_.IsNil(map[int]int{}), false)
	eq(t, "Len nil", maps_.Len(nil), 0)
	maps_.Del(nil, 1)
	maps_.Clear(nil)

	// a long sequence of mixed operations, against the same operations done by the stock compiler
	for _, fcMade := range []bool{true, false} {
		var got map[int]int
		if fcMade {
			got = maps_.MakeHint(0)
		} else {
			got = map[int]int{}
		}
		want := map[int]int{}
		seed := uint32(12345)
		for i := 0; i < 30000; i++ {
			seed = seed*1664525 + 1013904223
			k := int(seed>>8) % 600
			v := int(int32(seed)) >> 20
			switch seed >> 29 {
			case 0, 1:
				maps_.Set(got, k, v)
				want[k] = v
			case 2:
				maps_.Del(got, k)
				delete(want, k)
			case 3:
				want[k] += v
				eq(t, "AddTo", maps_.AddTo(got, k, v), want[k])
			case 4:
				want[k]++
				eq(t, "Incr", maps_.Incr(got, k), want[k])
			case 5:
				maps_.Decr(got, k)
				want[k]--
			case 6:
				g, gok := maps_.GetOk(got, k)
				w, wok := want[k]
				if g != w || gok != wok {
					t.Fatalf("GetOk(%d) = %d %v, want %d %v", k, g, gok, w, wok)
				}
			case 7:
				eq(t, "Len", maps_.Len(got), len(want))
				eq(t, "Has", maps_.Has(got, k), func() bool { _, ok := want[k]; return ok }())
			}
		}
		deq(t, "after mixed operations", got, want)
		maps_.Clear(got)
		eq(t, "Clear", len(got), 0)
		maps_.Set(got, 1, 2)
		eq(t, "after Clear", got[1], 2)
	}
	h := maps_.MakeHint(1000)
	eq(t, "MakeHint", len(h), 0)
	h[5] = 6
	eq(t, "MakeHint use", maps_.Get(h, 5), 6)

	for _, v := range []int{0, 1, -5, 123456789} {
		x := 7
		x += v
		x *= 3
		x -= 1
		x <<= 2
		x |= 1
		x ^= v
		x %= 1000003
		x /= 2
		x &= 0xffff
		x >>= 1
		x &^= 4
		eq(t, "OpAssign", maps_.OpAssign(map[int]int{1: 7}, 1, v), x)
	}

	sm := map[string]int{"a": 1, "b": 2, "c": 3}
	sum, found := maps_.GetOkExisting(sm, []string{"a", "x", "c", "", "a"})
	eq(t, "GetOkExisting sum", sum, 5)
	eq(t, "GetOkExisting found", found, 3)
	maps_.Swap(sm, "a", "b")
	deq(t, "Swap", sm, map[string]int{"a": 2, "b": 1, "c": 3})
	maps_.Swap(sm, "c", "c")
	maps_.Swap(sm, "a", "new")
	deq(t, "Swap with absent", sm, map[string]int{"a": 0, "b": 1, "c": 3, "new": 2})

	deq(t, "EvalOrder", maps_.EvalOrder(map[string]int{}),
		[]string{"key:a", "val", "key:b", "key:c", "val", "val", "key:a", "val"})
}

func TestKeyKinds(t *testing.T) {
	deq(t, "int8", maps_.CountInt8([]int8{-128, 127, 0, -128, -1, 0}), count([]int8{-128, 127, 0, -128, -1, 0}))
	deq(t, "uint16", maps_.CountUint16([]uint16{0, 65535, 256, 1, 256}), count([]uint16{0, 65535, 256, 1, 256}))
	deq(t, "int32", maps_.CountInt32([]int32{math.MinInt32, math.MaxInt32, 0, 0, -1}), count([]int32{math.MinInt32, math.MaxInt32, 0, 0, -1}))
	deq(t, "uint64", maps_.CountUint64([]uint64{0, 1 << 63, 1<<64 - 1, 1 << 63, 1 << 32}), count([]uint64{0, 1 << 63, 1<<64 - 1, 1 << 63, 1 << 32}))
	deq(t, "bool", maps_.CountBool([]bool{true, false, true}), count([]bool{true, false, true}))
	long := strings.Repeat("long", 100)
	ss := []string{"", "a", "b", "a", long, long[:399], strings.Repeat("long", 100), "a\x00", "a"}
	deq(t, "string", maps_.CountStrings(ss), count(ss))
	fs := []float64{0, 1, 1.5, -1.5, math.Inf(1), math.Inf(-1), 1.5, 1e300, 5e-324, math.Copysign(0, -1)}
	deq(t, "float64", maps_.CountFloats(fs), count(fs))
	f32 := []float32{0, 1, 1.5, 1.5, float32(math.Inf(1)), float32(math.Copysign(0, -1))}
	deq(t, "float32", maps_.CountFloat32(f32), count(f32))
	cs := []complex128{0, 1i, 1, complex(1, 1), 1i, complex(0, math.Copysign(0, -1))}
	deq(t, "complex", maps_.CountComplex(cs), count(cs))
	ps := []maps_.Pt{{}, {X: 1}, {Y: 1}, {X: 1}, {X: 1, Y: 1}, {}}
	deq(t, "struct", maps_.CountPts(ps), count(ps))
	rs := []maps_.Rec{{}, {Name: "a"}, {Name: "a"}, {Name: "a", ID: 1}, {Name: "a", Ok: true}, {F: 0}, {F: math.Copysign(0, -1)}, {F: 1}, {Name: ss[4]}, {Name: ss[6]}}
	deq(t, "struct with string, padding and float", maps_.CountRecs(rs), count(rs))
	as := [][2]string{{"a", "b"}, {"b", "a"}, {"a", "b"}, {"", ""}, {"ab", ""}, {"a", "b"}}
	deq(t, "array", maps_.CountArrays(as), count(as))
	x, y := 1, 1
	ptrs := []*int{&x, &y, nil, &x, nil, new(int)}
	deq(t, "pointer", maps_.CountPtrs(ptrs), count(ptrs))
	c1, c2 := make(chan int), make(chan int)
	chans := []chan int{c1, c2, c1, nil}
	deq(t, "chan", maps_.CountChans(chans), count(chans))
	anys := []any{1, "a", 1, int64(1), 1.0, maps_.Pt{X: 1, Y: 2}, maps_.Pt{X: 1, Y: 2}, nil, nil, true, [2]int{1, 2}, &x, &x, &y,
		uint8(1), "1", struct{}{}, [0]int{}, 'a', c1, float32(1), maps_.Rec{Name: "r"}, error(nil), fmt.Stringer(nil)}
	deq(t, "any", maps_.CountAny(anys), count(anys))
	iks := []maps_.IKey{{}, {K: 1}, {K: 1}, {K: "1"}, {K: 1, N: 1}, {K: maps_.Pt{}}, {K: maps_.Pt{}}, {K: nil, N: 0}}
	deq(t, "struct with interface", maps_.CountIKeys(iks), count(iks))

	bk := maps_.CountBigKeys([]byte{1, 2, 1, 255, 1})
	eq(t, "BigKey len", len(bk), 3)
	var k maps_.BigKey
	for i := range k {
		k[i] = 1 + byte(i)
	}
	eq(t, "BigKey lookup", bk[k], 3)
	k[199]++
	eq(t, "BigKey lookup miss", bk[k], 0)

	length, found, afterDelete, sum, afterClear := maps_.NaN(math.NaN(), 5)
	deq(t, "NaN", []any{length, found, afterDelete, sum, afterClear}, []any{5, false, 5, 10, 0})

	// the stock compiler's Zeros
	pz, nz := 0.0, math.Copysign(0, -1)
	zm := map[float64]int{}
	zm[pz] = 1
	zm[nz] += 10
	var k0 float64
	for k := range zm {
		k0 = k
	}
	n, v, inv := maps_.Zeros(pz, nz)
	deq(t, "Zeros", []any{n, v, inv}, []any{len(zm), zm[nz], 1 / k0})
	eq(t, "Zeros (the key of an existing float entry is overwritten on assignment)", inv, math.Inf(-1))

	// interface keys
	am := maps_.CountAny(anys)
	stock := count(anys)
	for _, key := range append(anys, 2, "b", int8(1), maps_.Pt{}, new(int), 1.5, uint(1), false, [2]int{2, 1}, [1]int{1}) {
		g, gok := maps_.AnyGet(am, key)
		w, wok := stock[key]
		if g != w || gok != wok {
			t.Errorf("AnyGet(%T %v) = %v %v, want %v %v", key, key, g, gok, w, wok)
		}
	}
	empty := map[any]int{}
	for i, bad := range []any{[]int{1}, map[int]int{}, func() {}, [1][]int{}, struct{ F any }{[]int{}}, maps_.IKey{K: []string{}}, [2]any{1, func() {}}} {
		w := catch(func() { _ = stock[bad] })
		if w == "" {
			t.Fatalf("stock: no panic for unhashable key %d", i)
		}
		eq(t, fmt.Sprintf("AnyGet unhashable %d", i), catch(func() { maps_.AnyGet(am, bad) }), w)
		eq(t, fmt.Sprintf("AnyGet unhashable %d in empty map", i), catch(func() { maps_.AnyGet(empty, bad) }), catch(func() { _ = empty[bad] }))
		eq(t, fmt.Sprintf("AnyGet unhashable %d in nil map", i), catch(func() { maps_.AnyGet(nil, bad) }), catch(func() { _ = map[any]int(nil)[bad] }))
		eq(t, fmt.Sprintf("AnySet unhashable %d", i), catch(func() { maps_.AnySet(am, bad, 1) }), catch(func() { stock[bad] = 1 }))
		eq(t, fmt.Sprintf("AnyDel unhashable %d", i), catch(func() { maps_.AnyDel(am, bad) }), catch(func() { delete(stock, bad) }))
	}
	eq(t, "unhashable text", catch(func() { maps_.AnyGet(am, []int{1}) }), "runtime.errorString: runtime error: hash of unhashable type []int")
	maps_.AnySet(am, "new", 5)
	maps_.AnyDel(am, 1)
	maps_.AnyDel(am, "absent")
	stock["new"] = 5
	delete(stock, 1)
	deq(t, "AnySet/AnyDel", am, stock)
}

func TestValues(t *testing.T) {
	deq(t, "GroupByLen", maps_.GroupByLen([]string{"a", "bb", "c", "", "dd", "eee", "f"}),
		map[int][]string{0: {""}, 1: {"a", "c", "f"}, 2: {"bb", "dd"}, 3: {"eee"}})
	deq(t, "Nested", maps_.Nested([][2]string{{"x", "a"}, {"x", "b"}, {"y", "a"}, {"x", "a"}}),
		map[string]map[string]int{"x": {"a": 2, "b": 1}, "y": {"a": 1}})
	names := []string{"p", "q", "p", "r", "p"}
	deq(t, "StructValues", maps_.StructValues(names), map[string]maps_.Pt{"p": {X: 3, Y: 6}, "q": {X: 1, Y: 1}, "r": {X: 1, Y: 3}})
	deq(t, "PtrValues", maps_.PtrValues(names), map[string]*maps_.Pt{"p": {X: 3, Y: 6}, "q": {X: 1, Y: 1}, "r": {X: 1, Y: 3}})
	wm := maps_.WideValues(50)
	eq(t, "WideValues len", len(wm), 50)
	for _, k := range []int{0, 1, 25, 49} {
		eq(t, "WideSum", maps_.WideSum(wm, k), 2000*k+190+1)
		eq(t, "WideValues stock read", wm[k].A[19], k*100+19)
	}
	eq(t, "WideSum absent", maps_.WideSum(wm, 50), 0)
	deq(t, "FuncValues", maps_.FuncValues(5), map[string]int{"double": 10, "neg": -5, "same": 5})
	sm := map[string]string{"a": "x"}
	eq(t, "StringOps", maps_.StringOps(sm, "a", "-"), "x--")
	eq(t, "StringOps new", maps_.StringOps(sm, "b", "+"), "++")
	deq(t, "StringOps effect", sm, map[string]string{"a": "x--", "b": "++"})
	deq(t, "Set_", maps_.Set_([]string{"a", "b", "a"}), map[string]struct{}{"a": {}, "b": {}})
	deq(t, "WordFreq", maps_.WordFreq("a b  a c a "), map[string]int{"a": 3, "b": 1, "c": 1})
	deq(t, "WordFreq empty", maps_.WordFreq("   "), map[string]int{})
	v, ok := maps_.BytesKey(map[string]int{"abc": 7}, []byte("abc"))
	eq(t, "BytesKey", fmt.Sprint(v, ok), "7 true")
	v, ok = maps_.BytesKey(map[string]int{"abc": 7}, []byte("abd"))
	eq(t, "BytesKey miss", fmt.Sprint(v, ok), "0 false")
}

func TestNil(t *testing.T) {
	v, ok, n, iters := maps_.NilRead("k")
	deq(t, "NilRead", []any{v, ok, n, iters}, []any{0, false, 0, 0})
	var sm map[string]int
	var im map[int]int
	want := catch(func() { sm["k"] = 1 })
	eq(t, "nil write text", want, "runtime.plainError: assignment to entry in nil map")
	eq(t, "NilWrite", catch(func() { maps_.NilWrite(nil, "k", 1) }), want)
	eq(t, "NilIncr", catch(func() { maps_.NilIncr(nil, "k") }), catch(func() { sm["k"]++ }))
	eq(t, "NilWriteInt", catch(func() { maps_.NilWriteInt(nil, 1) }), catch(func() { im[1] = 1 }))
	eq(t, "Set nil", catch(func() { maps_.Set(nil, 1, 1) }), want)
	eq(t, "AddTo nil", catch(func() { maps_.AddTo(nil, 1, 1) }), want)
	eq(t, "AnySet nil", catch(func() { maps_.AnySet(nil, 1, 1) }), want)
	if e := func() (r any) {
		defer func() { r = recover() }()
		maps_.NilWrite(nil, "k", 1)
		return nil
	}(); e == nil {
		t.Error("no panic")
	} else if _, ok := e.(interface{ RuntimeError() }); !ok {
		t.Errorf("nil map write panic is a %T, not a runtime.Error", e)
	}
	ok2 := map[string]int{}
	maps_.NilWrite(ok2, "k", 1)
	maps_.NilIncr(ok2, "k")
	eq(t, "NilWrite on a map", ok2["k"], 2)
}

func TestRange(t *testing.T) {
	ks := maps_.Keys(map[string]int{"b": 1, "a": 2, "c": 3, "": 4})
	sort.Strings(ks)
	deq(t, "Keys", ks, []string{"", "a", "b", "c"})
	eq(t, "Keys nil", len(maps_.Keys(nil)), 0)
	ksum, vsum, n := maps_.Sums(maps_.Squares(100))
	deq(t, "Sums", []int{ksum, vsum, n}, []int{4950, 328350, 100})
	ksum, vsum, n = maps_.Sums(nil)
	deq(t, "Sums nil", []int{ksum, vsum, n}, []int{0, 0, 0})

	for _, size := range []int{0, 1, 8, 9, 1000} {
		m := maps_.Squares(size)
		eq(t, "DeleteAllInRange visited", maps_.DeleteAllInRange(m), size)
		eq(t, "DeleteAllInRange len", len(m), 0)

		m = maps_.Squares(2 * size)
		visited := maps_.DeleteOthersInRange(m)
		eq(t, "DeleteOthersInRange visited", len(visited), size)
		eq(t, "DeleteOthersInRange len", len(m), size)
		seen := map[int]bool{}
		for _, k := range visited {
			if seen[k/2] {
				t.Errorf("DeleteOthersInRange: both of pair %d were produced", k/2)
			}
			seen[k/2] = true
			if _, ok := m[k]; !ok {
				t.Errorf("DeleteOthersInRange: visited key %d is gone", k)
			}
		}

		m = maps_.Squares(size)
		nv := maps_.InsertInRange(m, 5000)
		if nv < size || nv > len(m) {
			t.Errorf("InsertInRange(%d): %d visited, final length %d", size, nv, len(m))
		}
		for k := range m {
			if k >= 1000 {
				if _, ok := m[k-1000]; !ok {
					t.Errorf("InsertInRange: %d without %d", k, k-1000)
				}
			} else if k >= size {
				t.Errorf("InsertInRange: unexpected key %d", k)
			}
		}
	}
}

// lits is the stock compiler's version of maps_.Literals.
func lits(a, b string, x int) (map[string]int, map[maps_.Pt]string, map[string]maps_.Pt, map[string]*maps_.Pt, map[int][]int, map[string]map[string]int) {
	return map[string]int{a: x, b: x + 1, "const": 3},
		map[maps_.Pt]string{{X: 1, Y: 2}: a, {X: x}: b},
		map[string]maps_.Pt{a: {X: x, Y: x}, b: {Y: 5}},
		map[string]*maps_.Pt{a: {X: x, Y: x}, b: nil},
		map[int][]int{x: {1, 2, 3}, x + 1: nil, x + 2: {}},
		map[string]map[string]int{a: {b: x}, b: {}}
}

func TestLiterals(t *testing.T) {
	for _, c := range []struct {
		a, b string
		x    int
	}{{"a", "b", 7}, {"k", "k", 1}, {"const", "", 0}, {"a", "const", 1}} {
		g1, g2, g3, g4, g5, g6 := maps_.Literals(c.a, c.b, c.x)
		w1, w2, w3, w4, w5, w6 := lits(c.a, c.b, c.x)
		deq(t, "Literals 1", g1, w1)
		deq(t, "Literals 2", g2, w2)
		deq(t, "Literals 3", g3, w3)
		deq(t, "Literals 4", g4, w4)
		deq(t, "Literals 5", g5, w5)
		deq(t, "Literals 6", g6, w6)
	}
	g1, _, _, _, _, _ := maps_.Literals("k", "k", 1)
	deq(t, "Literals: the later of two equal keys wins", g1, map[string]int{"k": 2, "const": 3})

	deq(t, "Global", maps_.Global, map[string][]int{"primes": {2, 3, 5, 7}, "evens": {2, 4}, "none": nil})
	for _, c := range []struct {
		k    string
		want int
		ok   bool
	}{{"primes", 17, true}, {"evens", 6, true}, {"none", 0, true}, {"zz", 0, false}} {
		got, ok := maps_.FromGlobal(c.k)
		eq(t, "FromGlobal "+c.k, got, c.want)
		eq(t, "FromGlobal ok "+c.k, ok, c.ok)
	}
	maps_.Global["added"] = []int{100}
	got, ok := maps_.FromGlobal("added")
	eq(t, "FromGlobal added", fmt.Sprint(got, ok), "100 true")
	delete(maps_.Global, "added")

	a, b := maps_.Alias(10)
	eq(t, "Alias", a*100+b, 2020)
}

func TestLarge(t *testing.T) {
	const n = 100000
	m, sumAll, lenHalf, sumHalf, missing := maps_.Big(n)
	eq(t, "Big sumAll", sumAll, n*(n-1)/2)
	eq(t, "Big lenHalf", lenHalf, n/2)
	eq(t, "Big sumHalf", sumHalf, (n/2)*(n/2))
	eq(t, "Big missing", missing, 0)
	eq(t, "Big len", len(m), n/2)
	bad := 0
	for i := 1; i < n; i += 2 {
		if m[i*7] != i {
			bad++
		}
	}
	eq(t, "Big stock read", bad, 0)

	sm, sbad := maps_.BigStrings(n)
	eq(t, "BigStrings bad", sbad, 0)
	eq(t, "BigStrings len", len(sm), n)
	eq(t, "BigStrings stock read", sm[fmt.Sprintf("key-%d", n-1)], n-1)

	want := 0
	for i := 0; i < 10000; i += 64 {
		want += 2 * i
	}
	eq(t, "GrowWhileHolding", maps_.GrowWhileHolding(10000), want)
}
