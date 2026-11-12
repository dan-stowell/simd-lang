// Package chans exercises channels, select and goroutines.
// Imports: sync (WaitGroup, Mutex, Once).
package chans

import "sync"

// ---- send, receive, close ----

// Echo sends each value to a goroutine over an unbuffered channel and reads the doubled value back.
func Echo(xs []int) []int {
	in, out := make(chan int), make(chan int)
	go func() {
		for v := range in {
			out <- v * 2
		}
		close(out)
	}()
	var res []int
	for _, x := range xs {
		in <- x
		res = append(res, <-out)
	}
	close(in)
	if _, ok := <-out; ok {
		return nil
	}
	return res
}

// FillDrain reports len and cap of a buffered channel at each stage.
func FillDrain(n int) (lens []int, c int, sum int) {
	ch := make(chan int, n)
	c = cap(ch)
	lens = append(lens, len(ch))
	for i := 0; i < n; i++ {
		ch <- i + 1
	}
	lens = append(lens, len(ch))
	for i := 0; i < n/2; i++ {
		sum += <-ch
	}
	lens = append(lens, len(ch))
	close(ch)
	lens = append(lens, len(ch))
	for v := range ch {
		sum += v
	}
	lens = append(lens, len(ch))
	return
}

func LenCap(ch chan string) (int, int) { return len(ch), cap(ch) }

// CloseRecv: values buffered before close are still received; then zero values with ok == false.
func CloseRecv(vals []string) (got []string, oks []bool) {
	ch := make(chan string, len(vals))
	for _, v := range vals {
		ch <- v
	}
	close(ch)
	for i := 0; i < len(vals)+2; i++ {
		v, ok := <-ch
		got = append(got, v)
		oks = append(oks, ok)
	}
	return
}

func RangeSum(n int) (sum, count int) {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for i := 1; i <= n; i++ {
			ch <- i
		}
	}()
	for v := range ch {
		sum += v
	}
	for range ch { // closed: no iterations
		count++
	}
	return
}

// RecvExpr uses receive operations inside larger expressions.
func RecvExpr(a, b int) (int, bool) {
	ch := make(chan int, 2)
	ch <- a
	ch <- b
	d := <-ch - <-ch
	bch := make(chan bool, 1)
	bch <- a < b
	return d, !<-bch
}

// ---- element types ----

type Msg struct {
	ID   int
	Text string
	Data []byte
	Next *Msg
}

type Wide struct {
	A [32]int
	S string
}

func Structs(n int) (sumID int, texts string, dataLen int) {
	ch := make(chan Msg, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for m := range ch {
			sumID += m.ID
			texts += m.Text
			dataLen += len(m.Data)
			if m.Next != nil {
				sumID += m.Next.ID * 1000
			}
		}
	}()
	var prev *Msg
	for i := 0; i < n; i++ {
		m := Msg{ID: i, Text: string(rune('a' + i%26)), Data: make([]byte, i), Next: prev}
		ch <- m
		prev = &Msg{ID: i}
	}
	close(ch)
	<-done
	return
}

func Wides(n int) (sum int) {
	ch := make(chan Wide) // unbuffered: copied directly between the goroutines' stacks
	go func() {
		for i := 0; i < n; i++ {
			var w Wide
			for j := range w.A {
				w.A[j] = i + j
			}
			w.S = "wide"
			ch <- w
		}
		close(ch)
	}()
	for w := range ch {
		for _, v := range w.A {
			sum += v
		}
		sum += len(w.S)
	}
	return
}

func Ifaces(vals []any) (out []any) {
	ch := make(chan any, len(vals))
	for _, v := range vals {
		ch <- v
	}
	close(ch)
	for v := range ch {
		out = append(out, v)
	}
	return
}

type request struct {
	x     int
	reply chan int
}

// ChanOfChans: each request carries its own reply channel.
func ChanOfChans(xs []int) []int {
	reqs := make(chan request)
	go func() {
		for r := range reqs {
			r.reply <- r.x * r.x
		}
	}()
	out := make([]int, len(xs))
	for i, x := range xs {
		r := request{x, make(chan int)}
		reqs <- r
		out[i] = <-r.reply
	}
	close(reqs)
	return out
}

// Signal uses chan struct{} (zero-size elements).
func Signal(n int) int {
	sig := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		sig <- struct{}{}
	}
	count := 0
	for len(sig) > 0 {
		<-sig
		count++
	}
	return count
}

// ---- directional channels, pipelines ----

func gen(n int) <-chan int {
	ch := make(chan int)
	go func() {
		for i := 1; i <= n; i++ {
			ch <- i
		}
		close(ch)
	}()
	return ch
}

func square(in <-chan int, out chan<- int) {
	for v := range in {
		out <- v * v
	}
	close(out)
}

func Pipeline(n int) (sum int) {
	sq := make(chan int, 3)
	go square(gen(n), sq)
	for v := range sq {
		sum += v
	}
	return
}

// Primes is the classic concurrent sieve: a chain of filter goroutines, all stopped through done.
func Primes(n int) []int {
	done := make(chan struct{})
	defer close(done)
	ch := make(chan int)
	go func(out chan<- int) {
		for i := 2; ; i++ {
			select {
			case out <- i:
			case <-done:
				return
			}
		}
	}(ch)
	var ps []int
	for len(ps) < n {
		p := <-ch
		ps = append(ps, p)
		next := make(chan int)
		go func(in <-chan int, out chan<- int, p int) {
			for {
				select {
				case v := <-in:
					if v%p != 0 {
						select {
						case out <- v:
						case <-done:
							return
						}
					}
				case <-done:
					return
				}
			}
		}(ch, next, p)
		ch = next
	}
	return ps
}

func FanIn(nsrc, per int) (sum, count int) {
	out := make(chan int)
	var wg sync.WaitGroup
	for s := 0; s < nsrc; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				out <- s*1000 + i
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	for v := range out {
		sum += v
		count++
	}
	return
}

// ---- select ----

func TrySend(ch chan int, v int) bool {
	select {
	case ch <- v:
		return true
	default:
		return false
	}
}

func TryRecv(ch chan int) (int, bool, bool) {
	select {
	case v, ok := <-ch:
		return v, ok, true
	default:
		return 0, false, false
	}
}

// SelectReady: with several ready cases any one may be chosen; every value arrives exactly once.
func SelectReady(n int) (fromA, fromB, sum int) {
	a, b := make(chan int, n), make(chan int, n)
	for i := 0; i < n; i++ {
		a <- i
		b <- i * 1000
	}
	for i := 0; i < 2*n; i++ {
		select {
		case v := <-a:
			fromA++
			sum += v
		case v := <-b:
			fromB++
			sum += v
		}
	}
	return
}

// SelectNil: nil channels are never chosen.
func SelectNil(v int) (string, int) {
	var nilRecv <-chan int
	var nilSend chan<- int
	ready := make(chan int, 1)
	ready <- v
	select {
	case x := <-nilRecv:
		return "nil recv", x
	case nilSend <- v:
		return "nil send", 0
	case x := <-ready:
		return "ready", x
	}
}

func SelectNilDefault() string {
	var c chan int
	select {
	case <-c:
		return "recv"
	case c <- 1:
		return "send"
	default:
		return "default"
	}
}

// SelectSendRecv moves values through a one-slot buffer with send and receive cases in one select.
func SelectSendRecv(n int) (sent, received, sum int) {
	ch := make(chan int, 1)
	for received < n {
		select {
		case ch <- sent:
			sent++
		case v := <-ch:
			received++
			sum += v
		}
	}
	return
}

// Merge reads two channels until both are closed, setting each to nil when it is.
func Merge(a, b <-chan int) (out []int) {
	for a != nil || b != nil {
		select {
		case v, ok := <-a:
			if !ok {
				a = nil
				continue
			}
			out = append(out, v)
		case v, ok := <-b:
			if !ok {
				b = nil
				continue
			}
			out = append(out, v)
		}
	}
	return
}

// BreakLoop leaves a for/select loop with a labeled break; a plain break leaves only the select.
func BreakLoop(ch <-chan int, stop int) (sum, iters int) {
loop:
	for {
		iters++
		select {
		case v, ok := <-ch:
			if !ok {
				break loop
			}
			if v == stop {
				break
			}
			sum += v
		}
	}
	return
}

// SelectAssign receives into existing variables, an element and a field.
func SelectAssign(ch chan int) (int, bool, []int, Msg) {
	var v int
	var ok bool
	xs := make([]int, 2)
	var m Msg
	select {
	case v, ok = <-ch:
	}
	select {
	case xs[1] = <-ch:
	}
	select {
	case m.ID = <-ch:
	default:
		m.ID = -1
	}
	return v, ok, xs, m
}

var order []string

func pick(name string, ch chan int) chan int {
	order = append(order, name)
	return ch
}

func value(name string, v int) int {
	order = append(order, name)
	return v
}

// SelectEval: all channel operands and send values are evaluated once, in source order, on entering
// the select, whichever case is chosen.
func SelectEval() ([]string, int) {
	order = nil
	a, b, c := make(chan int, 1), make(chan int, 1), make(chan int, 1)
	b <- 7
	a <- 0 // a is full: its send case cannot go
	var got int
	select {
	case pick("a", a) <- value("va", 1):
		got = -1
	case got = <-pick("b", b):
	case pick("c", nil) <- value("vc", 3):
		got = -3
	}
	_ = c
	return order, got
}

// ---- goroutines, WaitGroup, Mutex ----

func ParallelSum(xs []int, workers int) int {
	var wg sync.WaitGroup
	sums := make([]int, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := w; i < len(xs); i += workers {
				sums[w] += xs[i]
			}
		}(w)
	}
	wg.Wait()
	total := 0
	for _, s := range sums {
		total += s
	}
	return total
}

type Counter struct {
	mu sync.Mutex
	n  int
	m  map[int]int
}

func (c *Counter) Inc(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	c.m[id]++
}

func MutexCounter(goroutines, per int) (int, int) {
	c := &Counter{m: map[int]int{}}
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				c.Inc(g)
			}
		}()
	}
	wg.Wait()
	return c.n, len(c.m)
}

func worker(id int, jobs <-chan int, results chan<- [2]int, wg *sync.WaitGroup) {
	defer wg.Done()
	for j := range jobs {
		results <- [2]int{j, j*j + 0*id}
	}
}

// Pool starts workers with `go f(args)`; results come back in any order.
func Pool(njobs, nworkers int) map[int]int {
	jobs := make(chan int, njobs)
	results := make(chan [2]int, njobs)
	var wg sync.WaitGroup
	for w := 0; w < nworkers; w++ {
		wg.Add(1)
		go worker(w, jobs, results, &wg)
	}
	for j := 0; j < njobs; j++ {
		jobs <- j
	}
	close(jobs)
	wg.Wait()
	close(results)
	out := map[int]int{}
	for r := range results {
		out[r[0]] = r[1]
	}
	return out
}

type Acc struct {
	mu  sync.Mutex
	sum int
}

func (a *Acc) Add(v int, wg *sync.WaitGroup) {
	defer wg.Done()
	a.mu.Lock()
	a.sum += v
	a.mu.Unlock()
}

// GoMethod starts goroutines from a method value, a method call and an interface method.
func GoMethod(n int) int {
	var a Acc
	var wg sync.WaitGroup
	f := a.Add
	var adder interface {
		Add(int, *sync.WaitGroup)
	} = &a
	for i := 1; i <= n; i++ {
		wg.Add(3)
		go a.Add(i, &wg)
		go f(i*10, &wg)
		go adder.Add(i*100, &wg)
	}
	wg.Wait()
	return a.sum
}

// Semaphore limits concurrency with a buffered channel and reports the highest concurrency seen.
func Semaphore(tasks, limit int) (done, maxActive int) {
	sem := make(chan struct{}, limit)
	var mu sync.Mutex
	var wg sync.WaitGroup
	active := 0
	for i := 0; i < tasks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			for k := 0; k < 100; k++ {
				_ = k * k
			}
			mu.Lock()
			active--
			done++
			mu.Unlock()
			<-sem
		}()
	}
	wg.Wait()
	return
}

func OnceInit(goroutines int) (calls, sum int) {
	var once sync.Once
	var wg sync.WaitGroup
	val := 0
	results := make(chan int, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			once.Do(func() {
				calls++
				val = 42
			})
			results <- val
		}()
	}
	wg.Wait()
	close(results)
	for v := range results {
		sum += v
	}
	return
}

// PingPong passes a counter between two goroutines n times.
func PingPong(n int) int {
	ping, pong := make(chan int), make(chan int)
	go func() {
		for v := range ping {
			pong <- v + 1
		}
		close(pong)
	}()
	v := 0
	for i := 0; i < n; i++ {
		ping <- v
		v = <-pong
	}
	close(ping)
	<-pong
	return v
}

// Deep runs a deep recursion in a new goroutine (its stack starts small and must grow).
func Deep(n int) int {
	res := make(chan int)
	go func() { res <- depth(n) }()
	return <-res
}

func depth(n int) int {
	var pad [16]int
	pad[n%16] = n
	if n == 0 {
		return 0
	}
	return depth(n-1) + 1 + pad[(n+1)%16]
}

// ---- panics ----

func SendClosed(ch chan int) { ch <- 1 }
func Close(ch chan int)      { close(ch) }
func SelectSendClosed(ch chan int) bool {
	select {
	case ch <- 1:
		return true
	default:
		return false
	}
}

// RecvOnly converts to a receive-only channel and reads what is there.
func RecvOnly(ch chan int) (v int, ok bool) {
	var r <-chan int = ch
	v, ok = <-r
	return
}

func SameChan(a, b chan int) (bool, bool) {
	var r <-chan int = a
	return a == b, r == b
}
