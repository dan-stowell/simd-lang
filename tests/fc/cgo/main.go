// fccgo: the program of fc's cgo test (tools/fc/cgotest.sh): its output must be the stock toolchain's.
package main

/*
static int twice(int x) { return 2 * x; }
*/
import "C"

import (
	"fmt"
	"runtime"

	"fccgo/cg"
)

func main() {
	fmt.Println("twice", C.twice(21))
	fmt.Println("add", cg.Add(2, 3), cg.Add(-7, 4))
	fmt.Println("scale", cg.Scale(1.5, 4), cg.Scale(-2, 0.25))
	fmt.Println("bias", cg.Bias())
	fmt.Println("counter", cg.Counter())
	cg.SetCounter(42)
	fmt.Println("counter", cg.Counter())
	fmt.Println("greeting", cg.Greeting())
	fmt.Println("global", cg.Global(23), cg.Global(5))
	fmt.Println("strlen", cg.Strlen("héllo"))
	fmt.Println("upper", cg.Upper("abc"))
	n, err := cg.Fails(2)
	fmt.Println("fails", n, err)
	n, err = cg.Fails(0)
	fmt.Println("fails", n, err)
	fmt.Println("points", cg.Points())
	fmt.Println("memory", cg.Memory())
	fmt.Println("callback", cg.SumSquares(10), cg.Calls())
	fmt.Println("pair", cg.PairDiff(3, 4))
	fmt.Println("parallel", cg.Parallel(8, 2000))
	runtime.GC()
	fmt.Println("calls", cg.Calls())
}
