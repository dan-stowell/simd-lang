package lib_test

import (
	"testing"

	"fcbasic/lib"
)

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	f()
}

func TestAll(t *testing.T) {
	if lib.Add(2, 3) != 5 {
		t.Error("Add")
	}
	if lib.Fib(50) != 12586269025 {
		t.Error("Fib", lib.Fib(50))
	}
	if lib.Sum([]int{1, 2, 3, 4}) != 10 || lib.Sum(nil) != 0 {
		t.Error("Sum")
	}
	if lib.Table(5) != 13 {
		t.Error("Table")
	}
	mustPanic(t, "Table(8)", func() { lib.Table(8) })
	mustPanic(t, "Table(-1)", func() { lib.Table(-1) })
	if lib.Count() != 1 || lib.Count() != 2 {
		t.Error("Count")
	}
	if lib.Dist(1, 2, 4, -2) != 7 {
		t.Error("Dist")
	}
	if lib.Bytes("Hello, World") != 8 {
		t.Error("Bytes")
	}
	if n, last := lib.Runes("héllo, 世界"); n != 9 || last != '界' {
		t.Error("Runes", n, last)
	}
	if got := lib.Ops(1234567, -89, 77, -3); got != ops(1234567, -89, 77, -3) {
		t.Error("Ops", got)
	}
	if got := lib.Float(3.5, -2.25); got != float(3.5, -2.25) {
		t.Error("Float", got)
	}
	for n, want := range map[int]string{-5: "neg", 0: "zero", 2: "small", 4: "medium", 5: "medium", 9: "large"} {
		if got := lib.Switch(n); got != want {
			t.Error("Switch", n, got)
		}
	}
	xs := lib.Append(nil, 100)
	if len(xs) != 200 || xs[199] != 99*99 || xs[198] != 99 {
		t.Error("Append", len(xs))
	}
	if s := lib.Slice(xs, 10, 20); len(s) != 10 || s[0] != 5 || cap(s) != cap(xs)-10 {
		t.Error("Slice")
	}
	mustPanic(t, "Slice", func() { lib.Slice(xs, 20, 10) })
	mustPanic(t, "Slice cap", func() { lib.Slice(xs, 0, 100000) })
	if lib.Concat("ab", "cd") != "ab-cd" {
		t.Error("Concat")
	}
	if !lib.Eq("abc", "abc") || lib.Eq("abc", "abd") || lib.Eq("abc", "ab") {
		t.Error("Eq")
	}
	if !lib.Less("abc", "abd") || lib.Less("b", "a") {
		t.Error("Less")
	}
	if lib.Div(7, -2) != -3 {
		t.Error("Div")
	}
	mustPanic(t, "Div", func() { lib.Div(1, 0) })
	if lib.Shift(0x8000000000000001, 4) != 0x18 || lib.Shift(1, 64) != 1 {
		t.Error("Shift")
	}
}

func ops(a, b int64, c uint32, d int8) int64 {
	x := a*b + a/b - a%b
	x ^= int64(c) << 3
	x |= int64(d)
	x &^= 0xf0
	x += 1 << 40
	return x >> 2
}

func float(a, b float64) float64 {
	c := a*b + a/b - b
	if c < 0 {
		c = -c
	}
	return c + float64(int(a))
}
