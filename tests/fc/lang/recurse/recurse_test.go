package recurse_test

import (
	"testing"

	"fclang/internal/chk"
	"fclang/recurse"
)

func TestMain(m *testing.M) { chk.Main(m) }

// Every exported function against its stock-compiled copy (recurse_ref_test.go).  Loop bounds and recursion
// depths get small pools; everything else runs over chk's integer patterns.
func TestAuto(t *testing.T) {
	chk.All(t, autoFuncs, map[string][]chk.Opt{
		"Fact":         {chk.Arg(0, 0, 1, 2, 5, 20, 21, 25, 66, 1000)},
		"FibRec":       {chk.Arg(0, 0, 1, 2, 3, 10, 20, 24, -1)},
		"Ackermann":    {chk.Arg(0, 0, 1, 2, 3), chk.Arg(1, 0, 1, 2, 3, 5)},
		"IsEven":       {chk.Arg(0, 0, 1, 2, 3, 100, 9999, 100001)},
		"IsOdd":        {chk.Arg(0, 0, 1, 2, 3, 100, 9999, 100001)},
		"Hanoi":        {chk.Arg(0, 0, 1, 2, 3, 10, 16), chk.Arg(1, 1), chk.Arg(2, 2), chk.Arg(3, 3)},
		"Deep":         {chk.Arg(0, 0, 1, 2, 15, 16, 17, 1000, 100000), chk.Arg(1, 0, 1, -7, 1<<62)},
		"DeepStruct":   {chk.Arg(0, 0, 1, 2, 100, 100000), chk.Limit(40)},
		"DeepSlice":    {chk.Arg(0, 0, 1, 2, 100, 100000), chk.Limit(40)},
		"Permutations": {chk.Arg(0, 0, 1, 2, 3, 6, 8, 10, 11, -1)},
		"Queens":       {chk.Arg(0, 0, 1, 2, 3, 4, 6, 8, -1)},
	})
}

func TestByHand(t *testing.T) {
	// deep recursion (the stack grows from 8 KiB to tens of MiB and is copied each time)
	if got := recurse.Deep(100000, 5); got != 5+100000 {
		t.Error("Deep", got)
	}
	if got := recurse.DeepStruct(100000, recurse.Acc{}); got.Sum != 100000*100001/2 || got.Count != 100000 || got.Name != "n" {
		t.Error("DeepStruct", got.Sum, got.Count)
	}
	done := make(chan int64)
	go func() { done <- recurse.Deep(200000, 1) }() // on a fresh goroutine's small stack
	if got := <-done; got != 200001 {
		t.Error("Deep on a new goroutine", got)
	}
	if recurse.Fact(20) != 2432902008176640000 || recurse.FibRec(20) != 6765 || recurse.Ackermann(2, 3) != 9 || recurse.Hanoi(10, 1, 2, 3) != 1023 || recurse.Permutations(5) != 120 || recurse.Queens(8) != 92 || !recurse.IsEven(10000) {
		t.Error("recursion")
	}
}
