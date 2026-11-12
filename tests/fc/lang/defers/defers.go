// Package defers exercises defer, panic and recover, run-time panics recovered as runtime.Error, and
// runtime.Goexit.
// Imports: runtime (runtime.Error, runtime.Goexit), sync (a deferred mu.Unlock).
package defers

import (
	"runtime"
	"sync"
)

var sink int

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Msg describes a recovered value.
func Msg(r any) string {
	switch v := r.(type) {
	case nil:
		return ""
	case error:
		return v.Error()
	case string:
		return v
	case int:
		return itoa(v)
	}
	return "?"
}

type E string

func (e E) Error() string { return "E:" + string(e) }

// ---- order, argument evaluation ----

func Order(n int) (out []int) {
	for i := 0; i < n; i++ {
		defer func() { out = append(out, i) }()
	}
	return nil
}

func Three() (s string) {
	defer func() { s += "1" }()
	defer func() { s += "2" }()
	defer func() { s += "3" }()
	return "r"
}

func ArgsAtDeferTime(x int) (a, b int) {
	defer func(v int) { a = v }(x)
	x += 10
	b = x
	return
}

func record(log *[]string, s string) { *log = append(*log, s) }

func name(log *[]string, s string) string {
	record(log, "eval:"+s)
	return s
}

func EvalOrder() []string {
	var log []string
	func() {
		defer record(&log, name(&log, "a"))
		defer record(&log, name(&log, "b"))
		record(&log, "body")
	}()
	return log
}

// FuncValueAtDeferTime: the function value is evaluated by the defer statement.
func FuncValueAtDeferTime() (s string) {
	f := func() { s += "first" }
	defer f()
	f = func() { s += "second" }
	_ = f
	return "x"
}

// NilFuncDefer: a nil function value panics when the deferred call runs, not at the defer statement.
func NilFuncDefer(f func()) (log []string) {
	defer func() {
		if r := recover(); r != nil {
			log = append(log, "recovered:"+Msg(r))
		}
	}()
	log = append(log, "before")
	defer f()
	log = append(log, "after")
	return log
}

type Closer interface{ Close() }

// NilIfaceDefer: a method of a nil interface is looked up by the defer statement.
func NilIfaceDefer(c Closer) (log []string) {
	defer func() {
		if r := recover(); r != nil {
			log = append(log, "recovered:"+Msg(r))
		}
	}()
	log = append(log, "before")
	defer c.Close()
	log = append(log, "after")
	return log
}

type Big struct{ A [20]int }

// BigArg: a large argument is copied by the defer statement.
func BigArg(x int) (sum int) {
	var b Big
	for i := range b.A {
		b.A[i] = x + i
	}
	defer func(v Big, extra ...int) {
		for _, e := range v.A {
			sum += e
		}
		for _, e := range extra {
			sum += e
		}
	}(b, 1000, 2000)
	for i := range b.A {
		b.A[i] = 0
	}
	return -1
}

// ---- named results ----

func Double(x int) (r int) {
	defer func() { r *= 2 }()
	return x + 1
}

func ResultAfterPanic(x int) (r int, s string) {
	defer func() {
		if e := recover(); e != nil {
			s = Msg(e)
		}
	}()
	r = x
	if x < 0 {
		panic("negative")
	}
	r = x * 2
	return r + 1, "ok"
}

// UnnamedAfterPanic returns the zero value when the panic is recovered.
func UnnamedAfterPanic(x int) int {
	defer func() { recover() }()
	if x > 0 {
		panic(x)
	}
	return 7
}

func WithCleanup(fail bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = E(Msg(r))
		}
	}()
	if fail {
		panic("failed")
	}
	return nil
}

// ---- loops, many defers, conditional defers ----

func LoopDefers(n int) (sum int, order []int) {
	for i := 0; i < n; i++ {
		defer func(k int) {
			sum += k
			order = append(order, k)
		}(i)
	}
	return 1000, nil
}

func ManyDefers(x int) (s int) {
	defer func() { s = s*3 + 1 }()
	defer func() { s = s*3 + 2 }()
	defer func() { s = s*3 + 3 }()
	defer func() { s = s*3 + 4 }()
	defer func() { s = s*3 + 5 }()
	defer func() { s = s*3 + 6 }()
	defer func() { s = s*3 + 7 }()
	defer func() { s = s*3 + 8 }()
	defer func() { s = s*3 + 9 }()
	defer func() { s = s*3 + 10 }()
	return x
}

func Conditional(a, b, c bool) (s string) {
	if a {
		defer func() { s += "a" }()
	}
	if b {
		defer func() { s += "b" }()
	}
	defer func() { s += "!" }()
	if c {
		return "c"
	}
	return "-"
}

func safeDiv(a, b int) (q int) {
	defer func() {
		if recover() != nil {
			q = -1
		}
	}()
	return a / b
}

func SafeDivAll(as, bs []int) (out []int) {
	for i := range as {
		out = append(out, safeDiv(as[i], bs[i]))
	}
	return
}

// ---- recover ----

func RecoverValue(v any) (r any) {
	defer func() { r = recover() }()
	panic(v)
}

func NoPanicRecover() (r any, ran bool) {
	defer func() {
		r = recover()
		ran = true
	}()
	return "unset", false
}

func RecoverOutsideDefer() any { return recover() }

func helperRecover() any { return recover() }

// IndirectRecover: recover called by a function that the deferred function calls does not recover.
func IndirectRecover() (inner, outer any) {
	defer func() { outer = recover() }()
	func() {
		defer func() { inner = helperRecover() }()
		panic("p")
	}()
	return
}

// DeferRecoverDirect: `defer recover()` does not recover either.
func DeferRecoverDirect() (outer any) {
	defer func() { outer = recover() }()
	func() {
		defer recover()
		panic("direct")
	}()
	return nil
}

// NestedFuncRecover: recover in a function literal called inside the deferred function does not recover.
func NestedFuncRecover() (inner, outer any) {
	defer func() { outer = recover() }()
	func() {
		defer func() {
			func() { inner = recover() }()
		}()
		panic("nested")
	}()
	return
}

func Repanic(v string) (r any) {
	defer func() { r = recover() }()
	func() {
		defer func() {
			e := recover()
			panic("re:" + e.(string))
		}()
		panic(v)
	}()
	return
}

// NestedPanic: a deferred function panics while the first panic is under way; the newer one is recovered.
func NestedPanic() (r any, order []string) {
	defer func() {
		r = recover()
		order = append(order, "outer")
	}()
	defer func() {
		order = append(order, "second")
		panic("second")
	}()
	defer func() { order = append(order, "first-defer") }()
	panic("first")
}

func PanicAfterRecover() (r1, r2 any) {
	defer func() { r2 = recover() }()
	defer func() { panic("later") }()
	defer func() { r1 = recover() }()
	panic("early")
}

func RecoverTwice() (a, b any) {
	defer func() {
		a = recover()
		b = recover()
	}()
	panic(1)
}

// CallAndRecover recovers whatever f panics with (f may be compiled by another compiler).
func CallAndRecover(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return nil
}

// Panic panics with v in a frame that has no defers.
func Panic(v any) { panic(v) }

func level(n int, log *[]string) {
	defer record(log, "unwind"+itoa(n))
	if n == 0 {
		panic("bottom")
	}
	level(n-1, log)
	record(log, "not reached")
}

func Through(n int) (log []string, r any) {
	defer func() { r = recover() }()
	level(n, &log)
	return
}

func deep(n int, count *int) int {
	defer func() { *count++ }()
	if n == 0 {
		panic("deep")
	}
	return deep(n-1, count) + 1
}

func Deep(n int) (count int, r any) {
	defer func() { r = recover() }()
	deep(n, &count)
	return
}

// ProtectEach recovers at every level of a recursion and keeps going.
func ProtectEach(n int) (total int) {
	defer func() {
		if r := recover(); r != nil {
			total += r.(int)
		}
	}()
	if n == 0 {
		return 0
	}
	total = ProtectEach(n - 1)
	panic(n)
}

// ---- run-time panics, recovered here ----

func try(f func()) (msg string, isRuntime bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if re, ok := r.(runtime.Error); ok {
			msg, isRuntime = re.Error(), true
			return
		}
		msg = Msg(r)
	}()
	f()
	return "no panic", false
}

type Pair struct {
	A int
	B string
}

func NilMapWrite(m map[string]int, k string) (string, bool) { return try(func() { m[k] = 1 }) }
func NilDeref(p *int) (string, bool)                        { return try(func() { *p = 1 }) }
func NilField(p *Pair) (string, bool)                       { return try(func() { sink = len(p.B) }) }
func Index(xs []int, i int) (string, bool)                  { return try(func() { sink = xs[i] }) }
func IndexStr(s string, i int) (string, bool)               { return try(func() { sink = int(s[i]) }) }
func SliceBounds(xs []int, i, j int) (string, bool)         { return try(func() { sink = len(xs[i:j]) }) }
func Divide(a, b int) (string, bool)                        { return try(func() { sink = a / b }) }
func Modulo(a, b int) (string, bool)                        { return try(func() { sink = a % b }) }
func Assert(v any) (string, bool)                           { return try(func() { sink = v.(int) }) }
func AssertIface(v any) (string, bool)                      { return try(func() { sink = len(v.(error).Error()) }) }
func NilFuncCall(f func()) (string, bool)                   { return try(func() { f() }) }
func ToArray(xs []int) (string, bool)                       { return try(func() { sink = [4]int(xs)[0] }) }
func MakeLen(n int) (string, bool)                          { return try(func() { sink = len(make([]int, n)) }) }
func CloseChan(c chan int) (string, bool)                   { return try(func() { close(c) }) }
func Shift(x, s int) (string, bool)                         { return try(func() { sink = x << s }) }
func Custom(v any) (string, bool)                           { return try(func() { panic(v) }) }

func IndexArr(i int) (string, bool) {
	var a [4]int
	return try(func() { sink = a[i] })
}

// The faulting operation in the same frame as the defer.
func DerefOr(p *int, d int) (v int) {
	defer func() {
		if recover() != nil {
			v = d
		}
	}()
	return *p
}

func IndexOr(xs []int, i, d int) (v int) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); ok {
				v = d
			}
		}
	}()
	v = xs[i]
	return v * 2
}

// ---- Goexit ----

func Goexit() (log []string, recovered any, finished bool) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			recovered = recover()
			log = append(log, "d1")
		}()
		defer func() { log = append(log, "d2") }()
		func() {
			defer func() { log = append(log, "inner") }()
			runtime.Goexit()
		}()
		finished = true
	}()
	<-done
	return
}

// ---- method values, closures, builtins ----

type Rec struct {
	Log []string
	N   int
}

func (r *Rec) Add(s string)     { r.Log = append(r.Log, s) }
func (r Rec) Snapshot(out *int) { *out = r.N }
func (r *Rec) Close()           { r.Add("close") }

func MethodDefers(n int) (log []string, snap, snap2 int) {
	r := &Rec{N: n}
	func() {
		defer r.Add("ptr-method")
		f := r.Add
		defer f("method-value")
		defer r.Snapshot(&snap) // the receiver is copied here
		defer Rec.Snapshot(*r, &snap2)
		var c Closer = r
		defer c.Close()
		r.N = n + 100
		r.Add("body")
	}()
	return r.Log, snap, snap2
}

func BuiltinDefers(m map[string]int, k string) (lenInside int, closed, unlocked bool) {
	ch := make(chan int, 1)
	var mu sync.Mutex
	func() {
		mu.Lock()
		defer mu.Unlock()
		defer close(ch)
		defer delete(m, k)
		lenInside = len(m)
		ch <- 1
	}()
	<-ch
	_, ok := <-ch
	closed = !ok
	unlocked = mu.TryLock()
	return
}

func DeferPanic() (r any) {
	defer func() { r = recover() }()
	defer panic("deferred panic")
	return "normal"
}

func DeferClear(m map[int]int) int {
	defer clear(m)
	return len(m)
}

func DeferPrint() int {
	defer print()
	defer println()
	return 1
}
