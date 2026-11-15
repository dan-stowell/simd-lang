// Package lines: the file and line runtime.Caller, a traceback and runtime.FuncForPC report for code of this
// package. A comment "// at <name>" marks the line the stock compiler reports for case <name> (lines_test.go
// reads them from this file).
package lines

import (
	"reflect"
	"runtime"
	"strings"
)

type I interface{ M() }

type S struct{}

// Line is the line of the call of Line.
func (S) Line() int {
	_, _, l, _ := runtime.Caller(1)
	return l
}

func caller() int {
	_, _, l, _ := runtime.Caller(1)
	return l
}

// panicLine runs f and gives the line of the frame that panicked: the first one past the runtime's panic.
func panicLine(f func()) (line int) {
	defer func() {
		recover()
		pcs := make([]uintptr, 64)
		frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs)])
		inPanic := false
		for {
			fr, more := frames.Next()
			if fr.Function == "runtime.gopanic" {
				inPanic = true
			} else if inPanic && !strings.HasPrefix(fr.Function, "runtime.") {
				line = fr.Line
				return
			}
			if !more {
				line = -1
				return
			}
		}
	}()
	f()
	return 0
}

// SplitMethodCall: a method called with its name on the line after the receiver's.
func SplitMethodCall() int {
	var s S
	return s.
		Line() // at SplitMethodCall
}

// SplitArgCall: a call among the arguments of a call, on a line of its own.
func SplitArgCall() (int, int) {
	return pair(caller(), // at SplitArgCallOuter
		caller()) // at SplitArgCallInner
}

func pair(a, b int) (int, int) { return a, b }

// NilIfaceSplit: a method of a nil interface value called over two lines faults at the method's line.
func NilIfaceSplit() int {
	return panicLine(func() {
		var v I
		v.
			M() // at NilIfaceSplit
	})
}

var sink int

// IndexSplit: a failing bounds check is at the line of its '['.
func IndexSplit() int {
	return panicLine(func() {
		a := []int{1}
		i := 5
		sink = 1 +
			a[i] // at IndexSplit
	})
}

// SliceSplit: the same for a slice expression.
func SliceSplit() int {
	return panicLine(func() {
		a := []int{1}
		i := 5
		sink = len("x") + len(
			a[i:]) // at SliceSplit
	})
}

var nilT *struct{ c chan int }
var ch chan int

// SelectCase: a nil pointer dereferenced in a select's case faults at the case's line.
func SelectCase() int {
	return panicLine(func() {
		select {
		case <-nilT.c: // at SelectCase
		case <-ch:
		}
	})
}

func endsInReturn() int {
	x := caller()
	return x + 1
} // (no code here: the epilogue is the return's)

// EndLines: the lines FuncForPC reports over the code of a function that ends in a return.
func EndLines() map[int]bool {
	pc := reflect.ValueOf(endsInReturn).Pointer()
	f := runtime.FuncForPC(pc)
	seen := map[int]bool{}
	for ; runtime.FuncForPC(pc) == f; pc++ {
		if _, l := f.FileLine(pc); l != 0 {
			seen[l] = true
		}
	}
	return seen
}
