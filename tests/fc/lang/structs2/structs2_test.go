package structs2_test

import (
	"testing"

	"fclang/internal/chk"
	"fclang/structs2"
)

func TestMain(m *testing.M) { chk.Main(m) }

// Every exported function against its stock-compiled copy (structs2_ref_test.go).
func TestAuto(t *testing.T) {
	chk.All(t, autoFuncs, map[string][]chk.Opt{
		"KeyLookup": {chk.Arg(1, 0, 1, 2), chk.Arg(2, 0, 1, 2)},
	})
}

func TestByHand(t *testing.T) {
	m := structs2.LitMap(3, "k")
	if len(m) != 2 || m["k"] != (structs2.P2{3, 3}) || m["k2"] != (structs2.P2{0, 2}) {
		t.Error("LitMap", m)
	}
	mp := structs2.LitMapPtr(3, "k")
	if len(mp) != 2 || mp["k"].N != 3 || mp[""].S != "k" {
		t.Error("LitMapPtr")
	}
	mk := structs2.LitMapKey(1)
	if len(mk) != 1 || mk[structs2.P2{1, 1}] != "b" {
		t.Error("map literal with equal non-constant keys: the later one wins", mk)
	}
	if s, ok := structs2.KeyLookup(structs2.LitMapKey(5), 1, 5); s != "b" || !ok {
		t.Error("struct key lookup")
	}
	if structs2.KeyStr(map[structs2.WithStr]int{}, 1, "s") != 12 {
		t.Error("struct key with a string: equal contents, one entry")
	}
	lit := structs2.LitIface(4, "s")
	want := []string{"int", "string:s", "nil", "P2", "*P2", "other"}
	for i := range lit {
		if got := structs2.IfaceFieldKind(&lit[i]); got != want[i] {
			t.Errorf("LitIface[%d]: %s", i, got)
		}
	}
	if lit[0].V != 4 || lit[3].V != (structs2.P2{4, 4}) || lit[4].V.(*structs2.P2).X != 4 {
		t.Error("LitIface values")
	}
	if !structs2.EqWithIface(structs2.WithIface{"a", 1}, structs2.WithIface{"a", 1}) || structs2.EqWithIface(structs2.WithIface{"a", 1}, structs2.WithIface{"a", int8(1)}) ||
		!structs2.EqWithIface(structs2.WithIface{"a", structs2.P2{1, 2}}, structs2.WithIface{"a", structs2.P2{1, 2}}) || !structs2.EqWithIface(structs2.WithIface{}, structs2.WithIface{}) {
		t.Error("== with an interface field")
	}
	got := chk.Try(func() { structs2.EqWithIface(structs2.WithIface{"a", []int{1}}, structs2.WithIface{"a", []int{1}}) })
	if got != "panic runtime.errorString: runtime error: comparing uncomparable type []int" {
		t.Error("== with slices in the interface fields:", got)
	}
	if chk.Try(func() { structs2.EqWithIface(structs2.WithIface{"a", []int{1}}, structs2.WithIface{"b", []int{1}}) }) != "" {
		t.Error("== must stop at the first differing field (the strings differ: no panic)")
	}
	if structs2.StrsAndSlices("a", []byte("bc"), "def", []int{1}, "", nil) != 7 || structs2.StrsAndSlices("", nil, "", nil, "", 0) != 1000 {
		t.Error("StrsAndSlices")
	}
	if n, ok := structs2.StructInMap(map[string]structs2.P2{"k": {1, 2}}, "k"); n != 4 || !ok {
		t.Error("StructInMap")
	}
	if chk.Try(func() { structs2.PtrInMap(map[string]*structs2.P2{}, "k") }) != "panic runtime.errorString: runtime error: invalid memory address or nil pointer dereference" {
		t.Error("m[k].X++ with a missing key of a map of pointers")
	}
}
