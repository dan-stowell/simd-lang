package misc

// Range-over-func iterators (Go 1.23): the 0-, 1- and 2-value forms, with break,
// continue, return, goto and defer inside the loop body.

type SeqInt func(yield func(int) bool)
type Seq2 func(yield func(int, string) bool)

func Upto(n int) SeqInt {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func Times(n int) func(func() bool) {
	return func(yield func() bool) {
		for i := 0; i < n; i++ {
			if !yield() {
				return
			}
		}
	}
}

func Enum(xs []string) Seq2 {
	return func(yield func(int, string) bool) {
		for i, x := range xs {
			if !yield(i, x) {
				return
			}
		}
	}
}

// iterLog records what iterators do around the loop body.
var iterLog string

func IterLog() string {
	s := iterLog
	iterLog = ""
	return s
}

// Logged is Upto with a deferred cleanup and a record of how the loop ended.
func Logged(n int) SeqInt {
	return func(yield func(int) bool) {
		defer func() { iterLog += "cleanup;" }()
		for i := 0; i < n; i++ {
			if !yield(i) {
				iterLog += "stopped@" + itoa(i) + ";"
				return
			}
		}
		iterLog += "exhausted;"
	}
}

// Bad ignores yield's result: continuing after the body has exited must panic.
func Bad(n int) SeqInt {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			yield(i)
		}
	}
}

func RangeSum(n int) (s int) {
	for x := range Upto(n) {
		s += x
	}
	return
}

func RangeZero(n int) (c int) {
	for range Times(n) {
		c++
	}
	return
}

func RangeNoVar(n int) (c int) {
	for range Upto(n) {
		c += 2
	}
	return
}

func RangeTwo(xs []string) (out string) {
	for i, s := range Enum(xs) {
		out += itoa(i) + "=" + s + ";"
	}
	return
}

func RangeKeyOnly(xs []string) (sum int) {
	for i := range Enum(xs) {
		sum += i + 1
	}
	return
}

func RangeAssign(n int) (int, string) {
	x, s := -1, "none"
	for x = range Upto(n) {
	}
	var i int
	for i, s = range Enum([]string{"a", "b", "c"}[:min(n, 3)]) {
		_ = i
	}
	return x, s
}

func RangeBreak(n, stop int) (s int, log string) {
	for x := range Logged(n) {
		if x == stop {
			break
		}
		s += x
	}
	return s, IterLog()
}

func RangeContinue(n int) (s int) {
	for x := range Upto(n) {
		if x%2 == 1 {
			continue
		}
		s += x
	}
	return
}

func RangeReturn(n, target int) (int, bool, string) {
	find := func() (int, bool) {
		for x := range Logged(n) {
			if x*x >= target {
				return x, true
			}
		}
		return -1, false
	}
	x, ok := find()
	return x, ok, IterLog()
}

func RangeGoto(n, stop int) int {
	s := 0
	for x := range Upto(n) {
		if x == stop {
			goto done
		}
		s += x
	}
	s += 1000
done:
	return s
}

func RangeNested(n, m, stop int) (c int, log string) {
outer:
	for i := range Logged(n) {
		for j := range Logged(m) {
			if j > i {
				continue outer
			}
			if i+j == stop {
				break outer
			}
			c++
		}
		c += 100
	}
	return c, IterLog()
}

// A return from the innermost of three nested range-over-func loops.
func RangeNestedReturn(n int, target int) (a, b, c int) {
	for i := range Upto(n) {
		for j := range Upto(n) {
			for k, s := range Enum([]string{"x", "yy", "zzz"}) {
				if i*100+j*10+k+len(s) == target {
					return i, j, k
				}
			}
		}
	}
	return -1, -1, -1
}

// Defers in the loop body run when the enclosing function returns, last first.
func RangeDefer(n int) (log string) {
	for x := range Upto(n) {
		defer func() { log += itoa(x) }()
	}
	log += "end;"
	return
}

func RangeDeferReturn(n, stop int) (r int) {
	defer func() { r += 1000 }()
	for x := range Upto(n) {
		defer func() { r = r*2 + x }()
		if x == stop {
			return 1
		}
	}
	return 2
}

func RangeMixed(n int) (s int) {
outer:
	for i := 0; i < n; i++ {
		for x := range Upto(n) {
			if x > i {
				continue outer
			}
			for k := 0; k < 3; k++ {
				if k == 2 {
					break
				}
				if i == 4 && x == 2 {
					break outer
				}
				s++
			}
		}
		s += 100
	}
	return
}

func RangeClosures(n int) (sum int) {
	var fs []func() int
	for x := range Upto(n) {
		fs = append(fs, func() int { return x * x })
	}
	for _, f := range fs {
		sum += f()
	}
	return
}

func RangeBad(n, stop int) (s int) {
	for x := range Bad(n) {
		if x == stop {
			break
		}
		s += x
	}
	return
}

// A panic in the loop body passes through the iterator (its deferred call runs).
func RangePanic(n, at int) (s int, msg string, log string) {
	defer func() {
		if r := recover(); r != nil {
			msg = r.(string)
		}
		log = IterLog()
	}()
	for x := range Logged(n) {
		if x == at {
			panic("body@" + itoa(x))
		}
		s += x
	}
	return
}

type tree struct {
	left, right *tree
	val         int
}

func (t *tree) insert(v int) *tree {
	if t == nil {
		return &tree{val: v}
	}
	if v < t.val {
		t.left = t.left.insert(v)
	} else {
		t.right = t.right.insert(v)
	}
	return t
}

func (t *tree) walk(yield func(int) bool) bool {
	return t == nil || t.left.walk(yield) && yield(t.val) && t.right.walk(yield)
}

func (t *tree) All() SeqInt { return func(yield func(int) bool) { t.walk(yield) } }

func TreeFirst(xs []int, k int) (out []int) {
	var t *tree
	for _, x := range xs {
		t = t.insert(x)
	}
	for v := range t.All() {
		if len(out) == k {
			break
		}
		out = append(out, v)
	}
	return
}

func fib() SeqInt {
	return func(yield func(int) bool) {
		a, b := 0, 1
		for yield(a) {
			a, b = b, a+b
		}
	}
}

func filter(s SeqInt, pred func(int) bool) SeqInt {
	return func(yield func(int) bool) {
		for v := range s {
			if pred(v) && !yield(v) {
				return
			}
		}
	}
}

func mapSeq(s SeqInt, f func(int) int) SeqInt {
	return func(yield func(int) bool) {
		s(func(v int) bool { return yield(f(v)) })
	}
}

// FibChain: the first k even Fibonacci numbers, plus add, from an infinite generator.
func FibChain(k, add int) (out []int) {
	if k <= 0 {
		return
	}
	for v := range mapSeq(filter(fib(), func(v int) bool { return v%2 == 0 }), func(v int) int { return v + add }) {
		out = append(out, v)
		if len(out) == k {
			break
		}
	}
	return
}

// The function value ranged over may be any expression, evaluated once.
func RangeFuncValue(n int) (s, evals int) {
	get := func() SeqInt { evals++; return Upto(n) }
	for x := range get() {
		s += x
	}
	var seq func(func(int, string) bool) = Enum([]string{"p", "q"})
	for i, v := range seq {
		s += i + len(v)
	}
	return
}
