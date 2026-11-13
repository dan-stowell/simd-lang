// sched.go: who gets a core next. A build's wall clock is its longest chain: a package's full check is
// serial and proportional to its source, its back end follows, and writing its object is serial again.
// With slots handed out in arrival order MinIO's cmd (5.4 MB, a 1.3 s check) waited behind a thousand
// small packages for each step and then compiled almost alone at the end of the build, on one or two
// cores. So slots go to the biggest package waiting (longest processing time first), and a package's
// function jobs outrank the start of any smaller package.
package main

import (
	"container/heap"
	"os"
	"sync"
)

// prioPool is a counting semaphore whose waiters are served highest priority first, arrival order
// among equals.
type prioPool struct {
	mu   sync.Mutex
	free int
	seq  int64
	q    waitHeap
}

type waiter struct {
	prio, seq int64
	ch        chan struct{}
	granted   bool
	cancelled bool
}

type waitHeap []*waiter

func (h waitHeap) Len() int { return len(h) }
func (h waitHeap) Less(i, k int) bool {
	if h[i].prio != h[k].prio {
		return h[i].prio > h[k].prio
	}
	return h[i].seq < h[k].seq
}
func (h waitHeap) Swap(i, k int) { h[i], h[k] = h[k], h[i] }
func (h *waitHeap) Push(x any)   { *h = append(*h, x.(*waiter)) }
func (h *waitHeap) Pop() any {
	old := *h
	w := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	return w
}

func newPrioPool(n int) *prioPool { return &prioPool{free: n} }

// Priorities: parsing finds the graph and declarations unblock every importer, so both come before any
// full check; after that, the size of the package's source.
const (
	prioParse = int64(3) << 40
	prioDecl  = int64(2) << 40
)

// fifoSched (FC_FIFO=1): slots in arrival order, as before sched.go; for comparing the two.
var fifoSched = os.Getenv("FC_FIFO") != ""

// enqueue asks for a slot; the slot is the caller's when w.ch is closed.
func (p *prioPool) enqueue(prio int64) *waiter {
	if fifoSched {
		prio = 0
	}
	w := &waiter{prio: prio, ch: make(chan struct{})}
	p.mu.Lock()
	if p.free > 0 {
		p.free--
		w.granted = true
		close(w.ch)
	} else {
		p.seq++
		w.seq = p.seq
		heap.Push(&p.q, w)
	}
	p.mu.Unlock()
	return w
}

func (p *prioPool) acquire(prio int64) { <-p.enqueue(prio).ch }

// cancel withdraws w; if the slot was already granted it is given back.
func (p *prioPool) cancel(w *waiter) {
	p.mu.Lock()
	if w.granted {
		p.mu.Unlock()
		p.release()
		return
	}
	w.cancelled = true
	p.mu.Unlock()
}

func (p *prioPool) release() {
	p.mu.Lock()
	for p.q.Len() > 0 {
		w := heap.Pop(&p.q).(*waiter)
		if w.cancelled {
			continue
		}
		w.granted = true
		close(w.ch)
		p.mu.Unlock()
		return
	}
	p.free++
	p.mu.Unlock()
}

// parallel runs a package's jobs (function bodies to compile, shards of bodies to check): in the caller,
// and on up to n helper slots of the pool, which the helpers ask for at the package's priority. A few jobs
// wait for a helper; when that many are waiting the caller runs the next one itself, and at the end it
// runs what no helper took (the caller holds a slot, so nothing waits for a slot while holding one).
type parallel struct {
	pool    *prioPool
	prio    int64
	helpers int
	jobs    chan func()
	done    chan struct{}
	wg      sync.WaitGroup
}

func (p *parallel) start() {
	p.jobs = make(chan func(), p.helpers)
	p.done = make(chan struct{})
	for i := 0; i < p.helpers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			w := p.pool.enqueue(p.prio)
			select {
			case <-w.ch:
			case <-p.done:
				p.pool.cancel(w)
				return
			}
			defer p.pool.release()
			for f := range p.jobs {
				f()
			}
		}()
	}
}

func (p *parallel) run(f func()) {
	if p.pool != nil && p.helpers > 0 {
		if p.jobs == nil {
			p.start()
		}
		select {
		case p.jobs <- f:
			return
		default:
		}
	}
	f()
}

func (p *parallel) wait() {
	if p.jobs != nil {
		for more := true; more; {
			select {
			case f := <-p.jobs:
				f()
			default:
				more = false
			}
		}
		close(p.done)
		close(p.jobs)
		p.wg.Wait()
		p.jobs = nil
	}
}
