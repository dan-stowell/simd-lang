package strs_test

import (
	"reflect"
	"testing"

	"fclang/internal/chk"
	"fclang/strs"
)

func TestMain(m *testing.M) { chk.Main(m) }

var runes = []any{0, 65, 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xfffd, 0xffff, 0x10000, 0x10ffff, 0x110000, -1, 0x4e16, 1<<31 - 1, -1 << 31}
var idx = []any{0, 1, 2, 3, 4, 5, 6, 13, 14, 100, 101, -1, 1 << 62, -1 << 63}

// Every exported function against its stock-compiled copy (strs_ref_test.go), over chk's strings (empty,
// ASCII, multi-byte, invalid and truncated UTF-8, NUL, 100 bytes) and integers (so every index and slice
// expression is tried in and out of bounds, and the panics compared).
func TestAuto(t *testing.T) {
	chk.All(t, autoFuncs, map[string][]chk.Opt{
		"Repeat":     {chk.Arg(1, 0, 1, 2, 7, -1)},
		"FromRune":   {chk.Arg(0, runes...)},
		"CatRune":    {chk.Arg(1, runes...)},
		"EncodeRune": {chk.Arg(0, runes...)},
		"FromInt64":  {chk.Arg(0, append([]any{1 << 32, 1<<32 + 65}, runes...)...)},
		"Slice":      {chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Limit(3000)},
		"SliceTwice": {chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Arg(3, idx...), chk.Arg(4, idx...), chk.Limit(3000)},
		"CatSlices":  {chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Limit(2000)},
		"EqSlices":   {chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Limit(2000)},
		"SliceLen":   {chk.Arg(1, idx...), chk.Arg(2, idx...), chk.Limit(2000)},
		"SwitchMany": {chk.Arg(0, "zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "", "thre", "threee", "ONE", "eno")},
	})
}

func TestByHand(t *testing.T) {
	const s = "héllo, 世界"
	if strs.Len(s) != 14 || strs.ConstLen() != 14 || strs.At(s, 1) != 0xc3 || strs.Last(s) != 0x8c || strs.Slice(s, 1, 3) != "é" || strs.SliceFrom(s, 7) != " 世界" || strs.SliceTo(s, 0) != "" {
		t.Error("len, index, slice")
	}
	for _, c := range []struct {
		f    func()
		want string
	}{
		{func() { strs.At("abc", 3) }, "panic runtime.boundsError: runtime error: index out of range [3] with length 3"},
		{func() { strs.At("abc", -1) }, "panic runtime.boundsError: runtime error: index out of range [-1]"},
		{func() { strs.AtU64("abc", 1<<63) }, "panic runtime.boundsError: runtime error: index out of range [9223372036854775808] with length 3"},
		{func() { strs.AtI8("abc", -128) }, "panic runtime.boundsError: runtime error: index out of range [-128]"},
		{func() { strs.First("") }, "panic runtime.boundsError: runtime error: index out of range [0] with length 0"},
		{func() { strs.Last("") }, "panic runtime.boundsError: runtime error: index out of range [-1]"},
		{func() { strs.AtConst5("abc") }, "panic runtime.boundsError: runtime error: index out of range [5] with length 3"},
		{func() { strs.Slice("abc", 2, 1) }, "panic runtime.boundsError: runtime error: slice bounds out of range [2:1]"},
		{func() { strs.Slice("abc", 0, 4) }, "panic runtime.boundsError: runtime error: slice bounds out of range [:4] with length 3"},
		{func() { strs.Slice("abc", -1, 2) }, "panic runtime.boundsError: runtime error: slice bounds out of range [-1:]"},
		{func() { strs.Slice("abc", 4, 5) }, "panic runtime.boundsError: runtime error: slice bounds out of range [:5] with length 3"},
		{func() { strs.SliceFrom("abc", 4) }, "panic runtime.boundsError: runtime error: slice bounds out of range [4:3]"},
		{func() { strs.SliceTo("abc", -2) }, "panic runtime.boundsError: runtime error: slice bounds out of range [:-2]"},
		{func() { strs.SliceConst("abcd") }, "panic runtime.boundsError: runtime error: slice bounds out of range [:5] with length 4"},
		{func() { strs.SliceFromConst("ab") }, "panic runtime.boundsError: runtime error: slice bounds out of range [3:2]"},
		{func() { strs.LitIndex(16) }, "panic runtime.boundsError: runtime error: index out of range [16] with length 16"},
	} {
		if got := chk.Try(c.f); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
	if strs.Cat8("a", "b", "", "d", "é", "f", "g", "h") != "abdéfgh" || strs.Cat2("", "") != "" || strs.CatAssign("x", "y") != "xy-xy-" || strs.CatConst("") != "<>" {
		t.Error("concatenation")
	}
	if !strs.Lt("", "a") || !strs.Lt("a", "ab") || !strs.Lt("ab", "b") || strs.Lt("b", "ab") || !strs.Lt("a\x00", "a\x01") || !strs.Lt("z", "\xff") || strs.Cmp("abc", "abc") != 0 || !strs.Ge("b", "a") || !strs.Le("a", "a") {
		t.Error("ordering is bytewise, shorter prefix first")
	}
	rs, is := strs.Runes("a\xffé\xe4\xb8世")
	if !reflect.DeepEqual(rs, []rune{'a', 0xfffd, 'é', 0xfffd, 0xfffd, '世'}) || !reflect.DeepEqual(is, []int{0, 1, 2, 4, 5, 6}) {
		t.Error("range with invalid UTF-8:", rs, is)
	}
	if strs.FromRune(0xd800) != "�" || strs.FromRune(-1) != "�" || strs.FromRune(0x110000) != "�" || strs.FromRune('世') != "世" || strs.FromByte(0xe9) != "é" || strs.FromByte1(0xe9) != "\xe9" {
		t.Error("string(rune)")
	}
	if string(strs.ToRunes("\xff")) != "�" || strs.FromRunes([]rune{0xd800, 'a', -5}) != "�a�" || len(strs.ToBytes("")) != 0 {
		t.Error("string <-> []rune")
	}
	if a, b := strs.BytesCopyIndependent("abc"); a != "abc" || b != "xxx" {
		t.Error("string([]byte) must copy")
	}
	if strs.RangeReassign("ab") != "abchanged" {
		t.Error("range evaluates its operand once")
	}
	if strs.Switch("ab") != 2 || strs.Switch("x") != -1 || strs.SwitchNoDefault("abd") != 12 || strs.SwitchNoDefault("zzz") != 0 || strs.SwitchTagless("hello!") != "greeting" || strs.SwitchMany("eleven") != 11 {
		t.Error("switch")
	}
	if strs.Hex(0xdeadbeef) != "0xdeadbeef" || strs.Quote("a\"b\n") != `"a\"b\x0a"` || strs.Reverse("héllo") != "olléh" || strs.Upper("héllo") != "HéLLO" {
		t.Error("utilities")
	}
}
