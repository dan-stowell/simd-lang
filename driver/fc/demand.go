// demand.go: functions by demand. In a whole-program build (fc build -link) a function nothing refers to is
// work the linker throws away: for MinIO 24% of the functions and two thirds of the type descriptor bytes
// (every method of every declared type drags its signature's descriptors, its wrappers and what they
// mention). So the back end follows references the way the linker's flood does, while it generates code:
//
//   - roots: main.main, every init function and package initializer, every function with a //go:linkname
//     directive, and every function of an eager package: the runtime and what it imports (generated code
//     calls into them by name), and packages with assembly (assembly refers to Go functions by name);
//   - a reference from compiled code or from static data to a declared function (funcSym, methodSym) asks
//     for it; a //go:linkname reference asks for the function of that name;
//   - writing a type's descriptor asks for every method in its method table (the linker prunes methods no
//     interface can call; here a type that is written keeps them all: 84% of the functions kept, against
//     the linker's 77%; FCLINK_KEEPMETHODS in fclink measures the rule).
//
// A function is compiled by the package that declares it, so a package stays open after its first pass:
// it gives its slot back and waits for requests (Pkg.dq) until the whole program is quiet (no first pass
// running, no request outstanding: Program.pending), and only then finishes its object. See lazyFuncsOn.
package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"log"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// demandStat (FC_DEMANDSTAT=1, a measurement): who asks: the call chains of the requests, counted.
var demandStat = os.Getenv("FC_DEMANDSTAT") != ""
var demandWho = map[string]int{}
var demandWhoMu sync.Mutex

func noteDemand() {
	pcs := make([]uintptr, 24)
	n := runtime.Callers(3, pcs)
	fr := runtime.CallersFrames(pcs[:n])
	var names []string
	for {
		f, more := fr.Next()
		name := strings.TrimPrefix(f.Function, "main.")
		if len(names) == 0 || names[len(names)-1] != name {
			names = append(names, name)
		}
		if !more || len(names) == 9 {
			break
		}
	}
	demandWhoMu.Lock()
	demandWho[strings.Join(names, " < ")]++
	demandWhoMu.Unlock()
}

func printDemandStat() {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range demandWho {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	for i, e := range all {
		if i == 40 {
			break
		}
		fmt.Printf("demand %7d  %s\n", e.v, e.k)
	}
}

// lazyFuncsOn: FC_LAZYFUNCS=1 turns functions by demand on, FC_EAGERFUNCS=1 off; unset, the default below.
func lazyFuncsOn() bool {
	if os.Getenv("FC_EAGERFUNCS") != "" {
		return false
	}
	return lazyFuncsDefault || os.Getenv("FC_LAZYFUNCS") != ""
}

// (on since 2026-11-16: MinIO warm 2.08-2.31 s by demand against 2.45-2.64 s, four order-balanced pairs,
// 14.3-15.5 core-s against 15.1-16.1; the Go test directory, the language corpus and samebytes pass with it)
const lazyFuncsDefault = true

// demand asks for fn to be compiled by its package, once.
func (pr *Program) demand(fn *types.Func) {
	if !pr.lazyFuncs || fn == nil || fn.Pkg() == nil {
		return
	}
	if _, ok := pr.fnWanted.Load(fn); ok {
		return
	}
	fn = pr.full(fn)
	p := pr.byTypes(fn.Pkg())
	if p == nil || p.eager || p.dch == nil {
		return
	}
	if _, loaded := pr.fnWanted.LoadOrStore(fn, true); loaded {
		return
	}
	select {
	case <-pr.quiet:
		log.Fatalf("fc: %s asked for after the back end finished", fn.FullName())
	default:
	}
	if demandStat {
		noteDemand()
	}
	pr.pending.Add(1)
	p.dmu.Lock()
	p.dq = append(p.dq, fn)
	n := len(p.dq)
	p.dmu.Unlock()
	if p.drainers.Load() > 0 && n < drainMore {
		return // (a thread is in this package's queue already and will find it; it wakes the package when it leaves)
	}
	select {
	case p.dch <- struct{}{}:
	default:
	}
}

// drainMore: a queue this long gets one more thread; drainChunk: how many requests a thread takes at once.
const drainMore, drainChunk = 48, 8

// demandName asks for the function a //go:linkname directive names: pkg.Name, pkg.Type.Method or
// pkg.(*Type).Method, the package path escaped as in symbol names.
func (pr *Program) demandName(sym string) {
	if !pr.lazyFuncs {
		return
	}
	// the package path ends at the first dot after the last slash
	slash := strings.LastIndex(sym, "/")
	dot := strings.Index(sym[slash+1:], ".")
	if dot < 0 {
		return
	}
	path, rest := sym[:slash+1+dot], sym[slash+1+dot+1:]
	if u, err := url.PathUnescape(path); err == nil {
		path = u
	}
	p := pr.byPath[path]
	if p == nil || p.eager || p.tpkg == nil {
		return
	}
	if !strings.Contains(rest, ".") {
		if fn, ok := p.tpkg.Scope().Lookup(rest).(*types.Func); ok {
			pr.demand(fn)
		}
		return
	}
	i := strings.LastIndex(rest, ".")
	tname, mname := strings.TrimSuffix(strings.TrimPrefix(rest[:i], "(*"), ")"), rest[i+1:]
	if tn, ok := p.tpkg.Scope().Lookup(tname).(*types.TypeName); ok {
		if n, ok := types.Unalias(tn.Type()).(*types.Named); ok {
			for k := 0; k < n.NumMethods(); k++ {
				if m := n.Method(k); m.Name() == mname {
					pr.demand(m)
				}
			}
		}
	}
}

// settle gives n counts of pending back; at zero the program is quiet.
func (pr *Program) settle(n int64) {
	if pr.pending.Add(-n) == 0 {
		pr.quietOnce.Do(func() { close(pr.quiet) })
	}
}

// serveDemands is the open state of a package compiled by demand: after the first pass (the caller holds
// a slot of the pool and one count of pending) it compiles what other packages ask for until the program
// is quiet. A package that waits (for requests, or for its last function before the type code) holds no
// slot, so the cores go to whoever has work. Requests are compiled by a thread that stays in the package's
// queue while there is anything in it (a second and further threads when the queue is long): a function
// per job, each taking a slot and landing on whatever thread was free, interleaved the packages and cost
// more than the functions it saved (JOURNAL 2026-11-15: one package's functions one after another find its
// types and symbols in the cache). Instances and the code for descriptors are compiled when no function of
// the package is being compiled (they share queues with the functions). It returns holding a slot.
func (c *compiler) serveDemands(compile func(fd *ast.FuncDecl, fn *types.Func, par *parallel), fixpoint func()) {
	p := c.pkg
	prio := p.srcBytes + 1
	c.fnSem.release()
	owed := int64(1)
	var taken atomic.Int64
	inline := &parallel{} // (runs the job in the calling goroutine)
	maxDrainers := int64(c.fnHelpers + 1)
	drain := func() {
		c.fnSem.acquire(prio)
		for {
			p.dmu.Lock()
			n := min(len(p.dq), drainChunk)
			batch := p.dq[:n:n]
			p.dq = p.dq[n:]
			p.dmu.Unlock()
			if n == 0 {
				break
			}
			taken.Add(int64(n))
			for _, fn := range batch {
				_, fd := c.pr.declOf(fn)
				if fd == nil || fd.Body == nil || fd.Name.Name == "_" {
					continue
				}
				if sig := fn.Type().(*types.Signature); sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
					continue
				}
				compile(fd, fn, inline)
			}
		}
		c.fnSem.release()
		p.drainers.Add(-1)
		select {
		case p.dch <- struct{}{}: // (the package looks at its queue again: a request may have come since)
		default:
		}
	}
	for {
		p.dmu.Lock()
		n := len(p.dq)
		p.dmu.Unlock()
		if n > 0 {
			want := min(int64(1+n/drainMore), maxDrainers)
			for p.drainers.Load() < want {
				p.drainers.Add(1)
				c.pr.runJob(drain)
			}
			<-p.dch // a long queue, or a thread left
			continue
		}
		if p.drainers.Load() > 0 {
			<-p.dch
			continue
		}
		owed += taken.Swap(0)
		c.fnSem.acquire(prio)
		fixpoint()
		c.fnSem.release()
		p.dmu.Lock()
		more := len(p.dq) > 0
		p.dmu.Unlock()
		if more {
			continue
		}
		c.pr.settle(owed)
		owed = 0
		select {
		case <-p.dch:
		case <-c.pr.quiet:
			c.fnSem.acquire(p.srcBytes)
			return
		}
	}
}

// runJob runs f on a goroutine kept for such jobs: a compile recurses deeply, and a fresh goroutine would
// grow its stack again for every function (a second of copying stacks over MinIO's 137,000).
func (pr *Program) runJob(f func()) {
	select {
	case w := <-pr.idleJobs:
		w <- f
	default:
		w := make(chan func(), 1)
		w <- f
		go func() {
			for f := range w {
				f()
				select {
				case pr.idleJobs <- w:
				default:
					return // (enough idle workers)
				}
			}
		}()
	}
}
