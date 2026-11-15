// Package cg: a cgo package for fc's cgo test (tools/fc/cgotest.sh): C functions taking and returning ints,
// doubles, pointers and structs, a callback from C into an exported Go function, C strings and memory,
// errno, and a C static variable read and written.
package cg

/*
#cgo CFLAGS: -DCG_BIAS=3
#include <stdlib.h>
#include <string.h>
#include "cg.h"

static int counter = 5;
static int get_counter(void) { return counter; }
static void set_counter(int v) { counter = v; }
static int bias(void) { return CG_BIAS; }
*/
import "C"

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

// Point is C's point seen from Go.
type Point = C.point

func Add(a, b int) int           { return int(C.add(C.int(a), C.int(b))) }
func Scale(v, k float64) float64 { return float64(C.scale(C.double(v), C.double(k))) }
func Counter() int               { return int(C.get_counter()) }
func SetCounter(v int)           { C.set_counter(C.int(v)) }
func Bias() int                  { return int(C.bias()) }
func Greeting() string           { return C.GoString(C.greeting) }

// Global: a C global variable read and written from Go directly.
func Global(v int) int {
	old := int(C.shared_global)
	C.shared_global = C.int(v)
	return old*1000 + int(C.read_global())
}
func SumSquares(n int) int64 { return int64(C.sum_via_callback(C.int(n))) }
func PairDiff(a, b int) int  { return int(C.pair_diff(C.int(a), C.int(b))) }

func Strlen(s string) int {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return int(C.strlen(cs))
}

// Fails: errno from C as the second result.
func Fails(code int) (int, error) {
	n, err := C.fails(C.int(code))
	return int(n), err
}

// Points: structs by value and by pointer.
func Points() string {
	name := C.CString("pt")
	defer C.free(unsafe.Pointer(name))
	p := C.mkpoint(4, 2.5, name)
	s := fmt.Sprintf("x=%d y=%g name=%s big=%d sum=%g", p.x, p.y, C.GoString(&p.name[0]), p.big, C.point_sum(&p))
	C.point_bump(&p, 10)
	s += fmt.Sprintf(" bumped x=%d y=%g sum=%g", p.x, p.y, float64(C.point_sum(&p)))
	return s
}

// Memory: C.malloc, writes through Go, C.GoBytes, C.free; a C string made by C.
func Memory() string {
	const n = 32
	p := C.malloc(n)
	b := unsafe.Slice((*byte)(p), n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	got := C.GoBytes(p, 10)
	C.free(p)
	a, c := C.CString("left"), C.CString("right")
	j := C.join(a, c)
	s := C.GoString(j)
	sn := C.GoStringN(j, 3)
	C.free(unsafe.Pointer(a))
	C.free(unsafe.Pointer(c))
	C.free(unsafe.Pointer(j))
	cb := C.CBytes([]byte("xyz\x00"))
	l := C.strlen((*C.char)(cb))
	C.free(cb)
	return fmt.Sprintf("%s %s %s %d", got, s, sn, l)
}

// Parallel: C calls (and callbacks into Go) from many goroutines at once.
func Parallel(workers, n int) int64 {
	var wg sync.WaitGroup
	sums := make([]int64, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				sums[w] += int64(C.add(C.int(i), C.int(w)))
				if i%100 == 0 {
					sums[w] += int64(C.sum_via_callback(3))
				}
			}
		}()
	}
	wg.Wait()
	var t int64
	for _, s := range sums {
		t += s
	}
	return t
}

// Upper: a Go string through C and back.
func Upper(s string) string {
	cs := C.CString(strings.ToUpper(s))
	defer C.free(unsafe.Pointer(cs))
	return C.GoString(cs)
}
