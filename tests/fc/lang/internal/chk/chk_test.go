package chk

import (
	"math"
	"testing"
)

type a struct {
	x int8
	S []string
	P *a
	M map[string]float64
	I any
}
type b struct {
	x int8
	S []string
	P *b
	M map[string]float64
	I any
}

func TestDiff(t *testing.T) {
	mk := func() (a, b) {
		return a{1, []string{"x"}, &a{x: 2}, map[string]float64{"n": math.NaN()}, 3}, b{1, []string{"x"}, &b{x: 2}, map[string]float64{"n": math.NaN()}, 3}
	}
	x, y := mk()
	if s := Diff(x, y); s != "" {
		t.Error(s)
	}
	for i, f := range []func(y *b){
		func(y *b) { y.x = 9 }, func(y *b) { y.S[0] = "y" }, func(y *b) { y.S = nil }, func(y *b) { y.P.x = 3 }, func(y *b) { y.P = nil },
		func(y *b) { y.M["n"] = 1 }, func(y *b) { y.M["m"] = 1 }, func(y *b) { y.I = int8(3) }, func(y *b) { y.I = nil }, func(y *b) { y.S = append(y.S, "") },
	} {
		x, y := mk()
		f(&y)
		if Diff(x, y) == "" {
			t.Error("no difference found in case", i)
		}
	}
	if Diff(0.0, math.Copysign(0, -1)) == "" || Diff([]int{}, []int(nil)) == "" || Diff(int8(1), int16(1)) == "" {
		t.Error("basic differences missed")
	}
	c := Clone(reflectValue(x), reflectType(y)).Interface().(b)
	if s := Diff(x, c); s != "" {
		t.Error("clone:", s)
	}
	if c.P == nil || c.x != 1 {
		t.Error("clone lost fields")
	}
}
