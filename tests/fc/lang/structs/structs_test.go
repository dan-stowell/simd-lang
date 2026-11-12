package structs_test

import (
	"math"
	"testing"

	"fclang/internal/chk"
	"fclang/structs"
)

func TestMain(m *testing.M) { chk.Main(m) }

// Every exported function against its stock-compiled copy (structs_ref_test.go) over generated arguments:
// chk fills structs field by field (unexported and embedded ones too), allocates what pointers point to, and
// compares results, panics and what the arguments point to after the call.
func TestAuto(t *testing.T) {
	few := []chk.Opt{chk.Limit(12)} // 74 KiB arguments
	chk.All(t, autoFuncs, map[string][]chk.Opt{
		"Build":   {chk.Arg(0, 0, 1, 2, 10, 1000, -1)},
		"HugeGet": few, "HugeSet": few, "HugeA": few, "HugeB": few, "HugeSetB": few, "HugeAddrB": few, "HugeChain": few, "CopyHuge": few,
		"HugePad":      {chk.Arg(1, 0, 1, 69999, 70000, -1), chk.Limit(12)},
		"NilArrayElem": {chk.Arg(1, 0, 99999, 100000, -1), chk.Limit(6)},
		"ArrayField":   {chk.Arg(1, 0, 1, 2, -1)},
		"StructInArray": {chk.Arg(1, 0, 1, 2, 3, -1)},
		"StructInSlice": {chk.Arg(1, 0, 1, 2, 3, 8, -1), chk.Limit(600)},
	})
}

const nilDeref = "panic runtime.errorString: runtime error: invalid memory address or nil pointer dereference"

// Nil pointers: at small offsets (the fault is the check), at offsets beyond the guard page (the compiler
// must check explicitly), for loads, stores, copies, and address-of.
func TestNil(t *testing.T) {
	for _, c := range []struct {
		name string
		f    func()
	}{
		{"p.X", func() { structs.NilX(nil) }},
		{"p.Y = 1", func() { structs.NilSetY(nil) }},
		{"*p (200 bytes)", func() { structs.NilCopy(nil) }},
		{"t.Mid.Base.Name", func() { structs.NilDeep(nil) }},
		{"embedded nil pointer", func() { structs.NilEmbPtr(structs.PtrEmb{}) }},
		{"&p.Y", func() { structs.NilAddr(nil) }},
		{"_ = *p", func() { structs.NilDiscard(nil) }},
		{"b.T at offset 192", func() { structs.NilBigField(nil) }},
		{"a[0] of a nil *[100000]int64", func() { structs.NilArrayElem(nil, 0) }},
		{"a[99999] of a nil *[100000]int64", func() { structs.NilArrayElem(nil, 99999) }},
		{"(*pp).X, pp nil", func() { structs.NilMethodless(nil) }},
		{"(*pp).X, *pp nil", func() { structs.NilMethodless(new(*structs.P2)) }},
		{"h.A at offset 4096", func() { structs.HugeA(nil) }},
		{"h.B at offset 74104", func() { structs.HugeB(nil) }},
		{"h.B = v", func() { structs.HugeSetB(nil, 1) }},
		{"&h.B", func() { structs.HugeAddrB(nil) }},
		{"h.Pad1[i]", func() { structs.HugePad(nil, 69999) }},
		{"h.A, h.B, h.S", func() { structs.HugeGet(nil) }},
		{"h.P.P.A, h.P nil", func() { structs.HugeChain(new(structs.Huge)) }},
		{"*c = *h, h nil", func() { structs.CopyHuge(nil) }},
		{"p.Name through two embedded nil pointers", func() { structs.PtrEmbName(&structs.PtrEmb2{}) }},
		{"p.Name, inner embedded pointer nil", func() { structs.PtrEmbName(&structs.PtrEmb2{PtrEmb: &structs.PtrEmb{}}) }},
		{"*dst = *src, src nil", func() { structs.AssignThrough(new(structs.WithStr), nil) }},
		{"*dst = *src, dst nil", func() { structs.AssignThrough(nil, new(structs.WithStr)) }},
		{"list walk off the end", func() { structs.ListNth(structs.Build(3), 3) }},
	} {
		if got := chk.Try(c.f); got != nilDeref {
			t.Errorf("%s: got %q", c.name, got)
		}
	}
	if chk.Try(func() { structs.HugePad(nil, 70000) }) == "" {
		t.Error("h.Pad1[70000], h nil: no panic")
	}
}

func TestByHand(t *testing.T) {
	if p, q := structs.Copy(structs.P2{1, 2}); p != (structs.P2{1, 2}) || q != (structs.P2{-1, 2}) {
		t.Error("assignment copies")
	}
	top := structs.LitNested(7, "n")
	if top.Mid.Base.ID != 7 || top.ID != 7<<32 || top.Name != "n" || top.Level != 3 || top.Extra != "nn" || structs.BaseID(&top) != 7 || structs.TopID(&top) != 7<<32 {
		t.Error("nested literal, shadowed promoted field")
	}
	structs.SetDeep(&top, 9, "m")
	if top.Base.ID != 9 || top.ID != 18 || top.Name != "m" || top.Level != 4 || *structs.NestedPtr(&top) != "m" {
		t.Error("SetDeep")
	}
	pe := structs.LitNestedPtr(5)
	structs.PtrEmbSet(pe, "x", 1)
	if pe.Name != "x" || pe.Base.ID != 1 || pe.PtrEmb.N != 2 || pe.M != 3 {
		t.Error("embedded pointers")
	}
	nan := math.NaN()
	if structs.EqF4(structs.F4{1, 2, 3, nan}, structs.F4{1, 2, 3, nan}) || !structs.EqF4(structs.F4{0, 1, 2, 3}, structs.F4{math.Copysign(0, -1), 1, 2, 3}) {
		t.Error("struct == compares floats as floats (NaN != NaN, +0 == -0), not bytes")
	}
	s1, s2 := "ab", "a"
	s2 += "b"
	if !structs.EqWithStr(structs.WithStr{1, s1, 2}, structs.WithStr{1, s2, 2}) || structs.EqWithStr(structs.WithStr{1, "ab", 2}, structs.WithStr{1, "ac", 2}) {
		t.Error("struct == compares strings by content")
	}
	if !structs.EqBlank(3) || !structs.PadGarbage(0x0102030405060708) || !structs.EqEmpty(structs.Empty{}, structs.Empty{}) {
		t.Error("struct == with blank fields, padding, no fields")
	}
	a, b, c, d, e, f := structs.Zero()
	if a != (structs.P2{}) || b != (structs.Small{}) || c != (structs.WithStr{}) || d != (structs.Mixed{}) || e != (structs.Top{}) || f != nil {
		t.Error("zero values")
	}
	if za, zb, zc, zd := structs.ZeroHuge(); za != 0 || zb != 0 || zc != 0 || !zd {
		t.Error("new(Huge) is zeroed")
	}
	h := new(structs.Huge)
	structs.HugeSet(h, 0x1234, -5, "s")
	if h.A != 0x1234 || h.B != -5 || h.S != "s" || h.Pad1[69999] != 0x34 || h.P != h || structs.HugeChain(h) != 0x1234 || *structs.HugeAddrB(h) != -5 {
		t.Error("fields at large offsets")
	}
	if structs.Ints10(1, 2, 3, 4, 5, 6, 7, 8, 9, 10) != 385 || structs.Floats16(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16) != 1496 {
		t.Error("arguments beyond the registers")
	}
	if structs.Ints12(-1, -2, -3, -4, 5, 6, 7, 8, -9, 10, -11, -12) != -1-4-9-16+25+36+49+64-81+100-121-144 {
		t.Error("small integer arguments are extended by the callee")
	}
	x := 5
	if got := structs.MixOverflow(1, 0.5, "ab", []int{1, 2, 3}, structs.P2{4, 5}, 6, 1.5, structs.I4{1, 1, 1, 1}, -7, 2.5, "t", 8, 9, 10); got != float64(1+2+3+4+5+6-7+1+8+9+10)+0.5+1.5+2.5+10 {
		t.Error("MixOverflow", got)
	}
	if m := structs.RetMixed(8, &x); m.I != 8 || m.F != 2 || m.S != "m" || m.P != &x || !m.B || m.G != 8 {
		t.Error("RetMixed")
	}
	if p, w := structs.ResultsNamed(4); p != (structs.P2{4, 8}) || w != (structs.WithStr{N: 5, S: "named"}) {
		t.Error("named struct results")
	}
	l := structs.ListReverse(structs.Build(100))
	if l.Val != 100 || structs.ListSum(l) != 5050 || structs.ListNth(l, 99) != 1 {
		t.Error("list")
	}
}
