package chans_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"fclang/chans"
)

func eq[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func deq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

// catch runs f and describes its panic ("" when there is none).
func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("%T: %v", r, r)
		}
	}()
	f()
	return ""
}

func TestBasic(t *testing.T) {
	deq(t, "Echo", chans.Echo([]int{1, 2, 3, -4}), []int{2, 4, 6, -8})
	deq(t, "Echo empty", chans.Echo(nil), []int(nil))

	for _, n := range []int{0, 1, 2, 7, 100} {
		lens, c, sum := chans.FillDrain(n)
		deq(t, "FillDrain lens", lens, []int{0, n, n - n/2, n - n/2, 0})
		eq(t, "FillDrain cap", c, n)
		eq(t, "FillDrain sum", sum, n*(n+1)/2)
	}
	l, c := chans.LenCap(nil)
	eq(t, "LenCap nil", l+c, 0)
	sc := make(chan string, 5)
	sc <- "x"
	l, c = chans.LenCap(sc)
	eq(t, "LenCap", l*10+c, 15)
	l, c = chans.LenCap(make(chan string))
	eq(t, "LenCap unbuffered", l+c, 0)

	got, oks := chans.CloseRecv([]string{"a", "", "c"})
	deq(t, "CloseRecv values", got, []string{"a", "", "c", "", ""})
	deq(t, "CloseRecv oks", oks, []bool{true, true, true, false, false})

	for _, n := range []int{0, 1, 100} {
		sum, count := chans.RangeSum(n)
		eq(t, "RangeSum", sum, n*(n+1)/2)
		eq(t, "RangeSum closed", count, 0)
	}
	d, nb := chans.RecvExpr(10, 3)
	eq(t, "RecvExpr diff", d, 7)
	eq(t, "RecvExpr bool", nb, true)
	d, nb = chans.RecvExpr(3, 10)
	eq(t, "RecvExpr diff 2", d, -7)
	eq(t, "RecvExpr bool 2", nb, false)
}

func TestElements(t *testing.T) {
	const n = 60
	sumID, texts, dataLen := chans.Structs(n)
	wantSum, wantTexts := 0, ""
	for i := 0; i < n; i++ {
		wantSum += i
		if i > 0 {
			wantSum += (i - 1) * 1000
		}
		wantTexts += string(rune('a' + i%26))
	}
	eq(t, "Structs sum", sumID, wantSum)
	eq(t, "Structs texts", texts, wantTexts)
	eq(t, "Structs data", dataLen, n*(n-1)/2)

	want := 0
	for i := 0; i < 50; i++ {
		for j := 0; j < 32; j++ {
			want += i + j
		}
		want += 4
	}
	eq(t, "Wides", chans.Wides(50), want)

	p := new(int)
	vals := []any{1, "s", nil, 2.5, p, [2]int{1, 2}, struct{}{}, error(nil), chans.Msg{ID: 1}.ID}
	deq(t, "Ifaces", chans.Ifaces(vals), vals)
	deq(t, "ChanOfChans", chans.ChanOfChans([]int{1, 2, 3, -4}), []int{1, 4, 9, 16})
	eq(t, "Signal", chans.Signal(17), 17)
	eq(t, "Signal 0", chans.Signal(0), 0)

	// channels crossing between the compilers
	ch := make(chan int, 3)
	eq(t, "TrySend 1", chans.TrySend(ch, 1), true)
	ch <- 2
	v, ok := chans.RecvOnly(ch)
	eq(t, "RecvOnly", fmt.Sprint(v, ok), "1 true")
	eq(t, "stock recv", <-ch, 2)
	close(ch)
	v, ok = chans.RecvOnly(ch)
	eq(t, "RecvOnly closed", fmt.Sprint(v, ok), "0 false")
	a, b := chans.SameChan(ch, ch)
	eq(t, "SameChan same", a && b, true)
	a, b = chans.SameChan(ch, make(chan int))
	eq(t, "SameChan different", a || b, false)
	a, b = chans.SameChan(nil, nil)
	eq(t, "SameChan nil", a && b, true)
}

func TestPipelines(t *testing.T) {
	for _, n := range []int{0, 1, 10, 1000} {
		eq(t, "Pipeline", chans.Pipeline(n), n*(n+1)*(2*n+1)/6)
	}
	deq(t, "Primes", chans.Primes(25), []int{2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79, 83, 89, 97})
	sum, count := chans.FanIn(8, 100)
	eq(t, "FanIn count", count, 800)
	eq(t, "FanIn sum", sum, 100*(0+1+2+3+4+5+6+7)*1000+8*(99*100/2))
	sum, count = chans.FanIn(0, 5)
	eq(t, "FanIn none", sum+count, 0)
	eq(t, "PingPong", chans.PingPong(1000), 1000)
	eq(t, "Deep", chans.Deep(100000), 100000)
}

func TestSelect(t *testing.T) {
	ch := make(chan int, 2)
	eq(t, "TrySend 1", chans.TrySend(ch, 10), true)
	eq(t, "TrySend 2", chans.TrySend(ch, 20), true)
	eq(t, "TrySend full", chans.TrySend(ch, 30), false)
	eq(t, "TrySend nil", chans.TrySend(nil, 1), false)
	eq(t, "TrySend unbuffered", chans.TrySend(make(chan int), 1), false)
	for _, want := range []string{"10 true true", "20 true true", "0 false false"} {
		v, ok, sel := chans.TryRecv(ch)
		eq(t, "TryRecv", fmt.Sprint(v, ok, sel), want)
	}
	close(ch)
	v, ok, sel := chans.TryRecv(ch)
	eq(t, "TryRecv closed", fmt.Sprint(v, ok, sel), "0 false true")
	v, ok, sel = chans.TryRecv(nil)
	eq(t, "TryRecv nil", fmt.Sprint(v, ok, sel), "0 false false")

	const n = 500
	fromA, fromB, sum := chans.SelectReady(n)
	eq(t, "SelectReady A", fromA, n)
	eq(t, "SelectReady B", fromB, n)
	eq(t, "SelectReady sum", sum, 1001*n*(n-1)/2)

	s, x := chans.SelectNil(5)
	eq(t, "SelectNil", fmt.Sprint(s, ":", x), "ready:5")
	eq(t, "SelectNilDefault", chans.SelectNilDefault(), "default")

	sent, received, total := chans.SelectSendRecv(100)
	eq(t, "SelectSendRecv received", received, 100)
	if sent != 100 && sent != 101 {
		t.Errorf("SelectSendRecv sent %d", sent)
	}
	eq(t, "SelectSendRecv sum", total, 99*100/2)

	a, b := make(chan int, 5), make(chan int)
	for i := 0; i < 5; i++ {
		a <- i
	}
	close(a)
	go func() {
		for i := 10; i < 15; i++ {
			b <- i
		}
		close(b)
	}()
	merged := chans.Merge(a, b)
	sort.Ints(merged)
	deq(t, "Merge", merged, []int{0, 1, 2, 3, 4, 10, 11, 12, 13, 14})
	deq(t, "Merge nil nil", chans.Merge(nil, nil), []int(nil))

	c := make(chan int, 6)
	for _, v := range []int{1, 2, 99, 3, 99, 4} {
		c <- v
	}
	close(c)
	total, iters := chans.BreakLoop(c, 99)
	eq(t, "BreakLoop sum", total, 10)
	eq(t, "BreakLoop iters", iters, 7)

	c = make(chan int, 3)
	c <- 1
	c <- 2
	c <- 3
	v, ok, xs, m := chans.SelectAssign(c)
	eq(t, "SelectAssign", fmt.Sprint(v, ok, xs, m.ID), "1 true [0 2] 3")
	c <- 4
	c <- 5
	v, ok, xs, m = chans.SelectAssign(c)
	eq(t, "SelectAssign default", fmt.Sprint(v, ok, xs, m.ID), "4 true [0 5] -1")
	close(c)
	v, ok, xs, m = chans.SelectAssign(c)
	eq(t, "SelectAssign closed", fmt.Sprint(v, ok, xs, m.ID), "0 false [0 0] 0")

	order, got := chans.SelectEval()
	deq(t, "SelectEval order", order, []string{"a", "va", "b", "c", "vc"})
	eq(t, "SelectEval value", got, 7)
}

func TestGoroutines(t *testing.T) {
	xs := make([]int, 10000)
	want := 0
	for i := range xs {
		xs[i] = i * 3
		want += xs[i]
	}
	for _, w := range []int{1, 2, 7, 64} {
		eq(t, "ParallelSum", chans.ParallelSum(xs, w), want)
	}
	n, ids := chans.MutexCounter(16, 2000)
	eq(t, "MutexCounter n", n, 32000)
	eq(t, "MutexCounter ids", ids, 16)

	pool := chans.Pool(500, 8)
	eq(t, "Pool len", len(pool), 500)
	for j, sq := range pool {
		if sq != j*j {
			t.Fatalf("Pool[%d] = %d", j, sq)
		}
	}
	eq(t, "GoMethod", chans.GoMethod(100), 111*100*101/2)

	done, maxActive := chans.Semaphore(200, 4)
	eq(t, "Semaphore done", done, 200)
	if maxActive < 1 || maxActive > 4 {
		t.Errorf("Semaphore: %d active at once", maxActive)
	}
	calls, sum := chans.OnceInit(50)
	eq(t, "OnceInit calls", calls, 1)
	eq(t, "OnceInit sum", sum, 50*42)
}

func TestPanics(t *testing.T) {
	closed := make(chan int, 1)
	close(closed)
	var nilCh chan int
	want := catch(func() { closed <- 1 })
	eq(t, "send on closed text", want, "runtime.plainError: send on closed channel")
	eq(t, "SendClosed", catch(func() { chans.SendClosed(closed) }), want)
	eq(t, "SelectSendClosed", catch(func() { chans.SelectSendClosed(closed) }), want)
	want = catch(func() { close(closed) })
	eq(t, "close of closed text", want, "runtime.plainError: close of closed channel")
	eq(t, "Close closed", catch(func() { chans.Close(closed) }), want)
	want = catch(func() { close(nilCh) })
	eq(t, "close of nil text", want, "runtime.plainError: close of nil channel")
	eq(t, "Close nil", catch(func() { chans.Close(nil) }), want)
	ok := make(chan int)
	eq(t, "Close ok", catch(func() { chans.Close(ok) }), "")
	_, open := <-ok
	eq(t, "Close effect", open, false)
	full := make(chan int, 1)
	full <- 1
	eq(t, "SelectSendClosed on full", chans.SelectSendClosed(full), false)
}
