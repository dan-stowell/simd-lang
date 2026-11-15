package cg

import "C"

import "sync/atomic"

var calls atomic.Int64

//export goSquare
func goSquare(x C.int) C.int {
	calls.Add(1)
	return x * x
}

//export goPair
func goPair(a, b C.int) (C.int, C.int) {
	return a * 10, b
}

// Calls: how many times C called goSquare.
func Calls() int64 { return calls.Load() }
