// Package control: if/else, the forms of for, labels, goto, switch, short-circuit evaluation and its
// order, order of evaluation in assignments, named results, recursion (deep enough to grow the stack).
// No imports.  Evaluation order is observed through a trace the functions build and return.
package control

// ---- if / else

func IfElse(x int) string {
	if x < 0 {
		return "neg"
	} else if x == 0 {
		return "zero"
	} else if x < 10 {
		return "small"
	} else if x < 100 {
		return "medium"
	} else {
		return "large"
	}
}
func IfInit(x int) int {
	if y := x * 2; y > 10 {
		return y
	} else if z := y + x; z > 5 {
		return z + y
	} else {
		return z - y
	}
}
func IfNoElse(x int) (r int) {
	r = 1
	if x > 0 {
		r += 10
	}
	if x > 5 {
		r += 100
	}
	if x == 7 {
		return -1
	}
	return
}
func IfNested(a, b, c bool) int {
	if a {
		if b {
			if c {
				return 7
			}
			return 6
		} else if c {
			return 5
		}
		return 4
	}
	if b {
		if !c {
			return 2
		}
		return 3
	}
	if c {
		return 1
	}
	return 0
}
func IfBoolExpr(a, b int) int {
	r := 0
	if a > 0 && b > 0 {
		r |= 1
	}
	if a > 0 || b > 0 {
		r |= 2
	}
	if !(a > 0) && !(b > 0) {
		r |= 4
	}
	if (a > 0) != (b > 0) {
		r |= 8
	}
	if a > 0 && (b > 0 || a > b) && !(a == 5 || b == 5) {
		r |= 16
	}
	if ok := a == b; ok {
		r |= 32
	}
	return r
}
func BoolValue(a, b int) (bool, bool, bool, bool) {
	x := a < b
	y := a < b && b < 10
	z := a < b || b < 10
	return x, y, z, !x == y
}

// ---- for

func For3(n int) (s int) {
	for i := 0; i < n; i++ {
		s += i
	}
	return
}
func ForCond(n int) (steps int) {
	for n > 1 {
		if n%2 == 0 {
			n /= 2
		} else {
			n = 3*n + 1
		}
		steps++
	}
	return
}
func ForEver(n int) (i int) {
	for {
		if i*i >= n {
			return
		}
		i++
	}
}
func ForRangeInt(n int) (s int) {
	for i := range n {
		s += i * i
	}
	return
}
func ForRangeIntNoVar(n int) (c int) {
	for range n {
		c++
	}
	return
}
func ForRangeInt8(n int8) (s int) {
	for i := range n {
		s += int(i)
	}
	return
}
func ForRangeIntOnce(n int) (s int) {
	for i := range n {
		n = 0 // the range expression was evaluated once
		s += i
	}
	return s + n
}
func ForRangeConst() (s int) {
	for i := range 10 {
		s += i
	}
	return
}
func ForMulti(n int) (s int) {
	for i, j := 0, n; i < j; i, j = i+1, j-1 {
		s += j - i
	}
	return
}
func ForNoPost(n int) (s int) {
	for i := 0; i < n; {
		s += i
		i += 3
	}
	return
}
func ForNoInit(i, n int) int {
	for ; i < n; i *= 2 {
		if i <= 0 {
			break
		}
	}
	return i
}
func ForDown(n int) (s uint) {
	for i := uint(n); i > 0; i-- {
		s += i
	}
	return
}
func ForContinue(n int) (s int) {
	for i := 0; i < n; i++ {
		if i%3 == 0 {
			continue
		}
		if i > 50 {
			break
		}
		s += i
	}
	return
}
func ForModifyVar(n int) (s int) {
	for i := 0; i < n; i++ {
		if i%4 == 0 {
			i++ // the body may change the loop variable
		}
		s += i
	}
	return
}
func ForNested(n, m int) (c int) {
	for i := 0; i < n; i++ {
		for j := i; j < m; j++ {
			if (i+j)%3 == 0 {
				continue
			}
			c += i*j + 1
		}
	}
	return
}
func LabeledBreak(n, m, stop int) (c int) {
outer:
	for i := 0; i < n; i++ {
		for j := 0; j < m; j++ {
			if i*j == stop {
				break outer
			}
			c++
		}
	}
	return
}
func LabeledContinue(n, m, skip int) (c int) {
outer:
	for i := 0; i < n; i++ {
		for j := 0; j < m; j++ {
			if j == skip+i {
				continue outer
			}
			c++
		}
		c += 100
	}
	return
}
func LabeledBreakSwitch(xs []int) (c int) {
loop:
	for _, x := range xs {
		switch {
		case x < 0:
			break loop
		case x == 0:
			break // the switch only
		case x > 100:
			continue loop
		}
		c += x + 1
	}
	return
}
func LabeledRange3(xs [][]int, stop int) (c int) {
a:
	for _, row := range xs {
	b:
		for _, x := range row {
			for k := 0; k < 3; k++ {
				switch {
				case x == stop:
					break a
				case x+k == stop:
					continue b
				case x < 0:
					continue a
				}
				c += x + k
			}
		}
	}
	return
}
func Goto(n int) (s int) {
	i := 0
loop:
	if i >= n {
		goto done
	}
	s += i
	i++
	goto loop
done:
	return s
}
func GotoForward(x int) int {
	if x < 0 {
		goto neg
	}
	if x == 0 {
		goto zero
	}
	return 1
neg:
	return -1
zero:
	return 0
}
func GotoOutOfBlock(xs []int) int {
	for i, x := range xs {
		if x < 0 {
			xs = xs[i:]
			goto found
		}
	}
	return -1
found:
	return len(xs)
}
func GotoRetry(n int) (tries int) {
retry:
	tries++
	if n > 0 {
		n /= 2
		goto retry
	}
	return
}

// ---- switch

func Switch(x int) string {
	switch x {
	case 0:
		return "zero"
	case 1, 2, 3:
		return "few"
	case 4:
		return "four"
	case -1:
		return "minus one"
	case 1000000:
		return "million"
	default:
		return "many"
	}
}
func SwitchDefaultFirst(x int) int {
	switch x {
	default:
		return -1
	case 1:
		return 10
	case 2:
		return 20
	}
}
func SwitchDefaultMiddle(x int) (r int) {
	switch x {
	case 1:
		r = 10
	default:
		r = -1
		fallthrough
	case 2:
		r += 20
	case 3:
		r = 30
	}
	return
}
func SwitchNoMatch(x int) (r int) {
	r = 5
	switch x {
	case 1:
		r = 10
	case 2:
		r = 20
	}
	return
}
func SwitchFallthrough(x int) (r int) {
	switch x {
	case 0:
		r += 1
		fallthrough
	case 1:
		r += 10
		fallthrough
	case 2:
		r += 100
	case 3:
		r += 1000
		fallthrough
	default:
		r += 10000
	}
	return
}
func SwitchTagless(x, y int) string {
	switch {
	case x < y:
		return "lt"
	case x == y && x == 0:
		return "both zero"
	case x == y:
		return "eq"
	}
	return "gt"
}
func SwitchInit(x int) int {
	switch y := x % 5; y {
	case 0:
		return 100
	case 1, 4:
		return y * 10
	}
	switch y := x * 2; {
	case y > 100:
		return 1
	}
	return 0
}
func SwitchBreak(x int) (r int) {
	switch {
	case x > 0:
		r = 1
		if x > 10 {
			break
		}
		r = 2
	case x < 0:
		r = -1
	}
	return r
}
func SwitchDense(x int) int {
	switch x {
	case 0:
		return 5
	case 1:
		return 3
	case 2:
		return 8
	case 3:
		return 13
	case 4:
		return 2
	case 5:
		return 99
	case 6:
		return 7
	case 7:
		return 1
	case 8:
		return 42
	case 9:
		return 17
	case 10:
		return 23
	case 11:
		return 4
	}
	return -1
}
func SwitchSparse(x int64) int {
	switch x {
	case -1 << 63:
		return 1
	case -1000000007:
		return 2
	case -1:
		return 3
	case 0:
		return 4
	case 255:
		return 5
	case 65536:
		return 6
	case 1 << 40:
		return 7
	case 1<<63 - 1:
		return 8
	}
	return 0
}
func SwitchU8(x uint8) int {
	switch x {
	case 0, 255:
		return 1
	case 'a', 'e', 'i', 'o', 'u':
		return 2
	case ' ', '\t', '\n':
		return 3
	}
	if '0' <= x && x <= '9' {
		return 4
	}
	return 0
}
func SwitchNonConst(x, a, b int) int {
	switch x {
	case a:
		return 1
	case b:
		return 2
	case a + b:
		return 3
	}
	return 0
}
func SwitchBool(a, b bool) int {
	switch a {
	case b:
		return 1
	case true:
		return 2
	}
	return 3
}
func SwitchInLoop(xs []int) (evens, odds, zeros int) {
	for _, x := range xs {
		switch {
		case x == 0:
			zeros++
			continue
		case x%2 == 0:
			evens++
		default:
			odds++
		}
	}
	return
}

// ---- evaluation order (the trace records the order of calls)

type Trace struct{ S string }

func (t *Trace) b(name string, v bool) bool {
	t.S += name
	return v
}
func (t *Trace) i(name string, v int) int {
	t.S += name
	return v
}
func (t *Trace) p(name string, p *int) *int {
	t.S += name
	return p
}
func (t *Trace) s(name string, s []int) []int {
	t.S += name
	return s
}

func ShortCircuit(a, b, c bool) (string, bool) {
	var t Trace
	r := t.b("a", a) && t.b("b", b) || t.b("c", c)
	return t.S, r
}
func ShortCircuit2(a, b, c, d bool) (string, bool) {
	var t Trace
	r := (t.b("a", a) || t.b("b", b)) && (t.b("c", c) || !t.b("d", d))
	return t.S, r
}
func ShortCircuitIf(a, b, c bool) string {
	var t Trace
	if t.b("a", a) || t.b("b", b) && t.b("c", c) {
		t.S += "!"
	}
	for i := 0; t.b("x", i < 2) && t.b("a", a); i++ {
		t.S += "."
	}
	return t.S
}
func ShortCircuitGuard(xs []int, i int) bool { return i >= 0 && i < len(xs) && xs[i] > 0 }
func NilGuard(p *int) bool                   { return p != nil && *p > 0 }
func NilGuardOr(p *int) bool                 { return p == nil || *p == 0 }

func OrderCall(a, b, c int) (string, int) {
	var t Trace
	r := three(t.i("a", a), t.i("b", b), t.i("c", c))
	return t.S, r
}
func three(a, b, c int) int { return a*100 + b*10 + c }
func OrderBinary(a, b, c int) (string, int) {
	var t Trace
	r := t.i("a", a) - t.i("b", b)*t.i("c", c) + three(t.i("d", 1), t.i("e", 2), t.i("f", 3))
	return t.S, r
}
func OrderAssign(xs []int, a, b int) (string, []int) {
	var t Trace
	t.s("x", xs)[t.i("i", a)], t.s("y", xs)[t.i("j", b)] = t.i("v", 10), t.i("w", 20)
	return t.S, xs
}
func OrderIndex(xs []int, a int) (string, int) {
	var t Trace
	*t.p("p", &xs[t.i("i", a)]) = t.i("v", 5) + t.s("s", xs)[t.i("k", 0)]
	return t.S, xs[a]
}
func OrderReturn(a, b int) (string, int, int) {
	var t Trace
	x, y := pair(&t, a, b)
	return t.S, x, y
}
func pair(t *Trace, a, b int) (int, int) { return t.i("a", a), t.i("b", b) }
func OrderOpAssign(xs []int) (string, []int) {
	var t Trace
	t.s("s", xs)[t.i("i", 1)] += t.i("v", 5)
	t.s("t", xs)[t.i("j", 0)]++
	return t.S, xs
}
func OrderLit(a, b, c int) (string, [3]int, []int) {
	var t Trace
	arr := [3]int{t.i("a", a), t.i("b", b), t.i("c", c)}
	sl := []int{2: t.i("x", a), 0: t.i("y", b)}
	return t.S, arr, sl
}
func OrderStr(a, b int) string {
	var t Trace
	s := name(&t, "p", a) + name(&t, "q", b) + name(&t, "r", a+b)
	return t.S + ":" + s
}
func name(t *Trace, n string, v int) string {
	t.S += n
	if v > 0 {
		return "+"
	}
	return "-"
}

// ---- multiple assignment

func SwapInts(a, b int) (int, int) {
	a, b = b, a
	return a, b
}
func Rotate3(a, b, c int) (int, int, int) {
	a, b, c = b, c, a
	return a, b, c
}
func SwapElems(xs []int, i, j int) {
	xs[i], xs[j] = xs[j], xs[i]
}
func IndexThenAssign(xs []int) (int, []int) {
	i := 0
	i, xs[i] = 1, 2 // sets xs[0]: the index was evaluated before any assignment
	return i, xs
}
func AssignIndexAfter(xs []int) (int, []int) {
	i := 0
	xs[i], i = 7, 2
	xs[i], xs[i-1] = xs[i-1], xs[i]
	return i, xs
}
func PtrThenAssign(a, b int) (int, int, int) {
	p := &a
	p, *p = &b, 99 // writes a: *p's operand was evaluated before p changed
	return a, b, *p
}
func Fib(n int) int {
	a, b := 0, 1
	for i := 0; i < n; i++ {
		a, b = b, a+b
	}
	return a
}
func AssignStructFields(x, y int) (int, int) {
	var p struct{ a, b int }
	p.a, p.b = x, y
	p.a, p.b = p.b, p.a+p.b
	return p.a, p.b
}
func AssignFromCall(a, b int) (int, int) {
	b, a = SwapInts(a, b)
	return a, b
}
func AssignMapSlice(m []int, k int) (int, int) {
	var x, y int
	x, m[k], y = m[k], x+1, m[k]+1
	return x*1000 + y, m[k]
}
func BlankAssign(a, b int) int {
	_, x := SwapInts(a, b)
	_ = x
	var _ = a
	_, _ = a, b
	return x
}

// ---- named results, bare return

func Named(x int) (lo, hi int) {
	lo, hi = x, x
	if x < 0 {
		lo = 2 * x
		return
	}
	hi = 2 * x
	return
}
func NamedShadow(x int) (r int) {
	r = 1
	{
		r := 2
		r += x
		_ = r
	}
	if x > 0 {
		return r + x
	}
	return
}
func NamedExplicit(x int) (a, b int) {
	a, b = 1, 2
	if x > 0 {
		return b, a
	}
	return a + x, b + x
}
func NamedLoop(xs []int) (n, sum int, found bool) {
	for n = 0; n < len(xs); n++ {
		if xs[n] < 0 {
			found = true
			return
		}
		sum += xs[n]
	}
	return
}
func ResultParamAlias(x int) (y int) {
	y = x
	x = y + 1
	y = x * 2
	return y + x
}

// ---- recursion

func Fact(n uint64) uint64 {
	if n <= 1 {
		return 1
	}
	return n * Fact(n-1)
}
func FibRec(n int) int {
	if n < 2 {
		return n
	}
	return FibRec(n-1) + FibRec(n-2)
}
func Ackermann(m, n int) int {
	if m == 0 {
		return n + 1
	}
	if n == 0 {
		return Ackermann(m-1, 1)
	}
	return Ackermann(m-1, Ackermann(m, n-1))
}
func IsEven(n int) bool {
	if n == 0 {
		return true
	}
	return IsOdd(n - 1)
}
func IsOdd(n int) bool {
	if n == 0 {
		return false
	}
	return IsEven(n - 1)
}
func Hanoi(n int, from, to, via int) (moves int) {
	if n == 0 {
		return 0
	}
	return Hanoi(n-1, from, via, to) + 1 + Hanoi(n-1, via, to, from)
}

// Deep recursion: every frame holds a buffer, so the stack grows many times (and is copied: the pointer
// into the frame must be adjusted).
func Deep(n int, seed int64) int64 {
	var buf [16]int64
	for i := range buf {
		buf[i] = seed + int64(i)
	}
	p := &buf[n&15]
	if n == 0 {
		return *p
	}
	r := Deep(n-1, seed+1)
	return r + *p - buf[n&15] + buf[0] - seed
}
func DeepStruct(n int, acc Acc) Acc {
	if n == 0 {
		return acc
	}
	acc.Sum += int64(n)
	acc.Count++
	acc.Name = "n"
	r := DeepStruct(n-1, acc)
	r.Count += acc.Count - acc.Count
	return r
}

type Acc struct {
	Sum   int64
	Count int
	Name  string
	Pad   [6]int64
}

func DeepSlice(n int, xs []int) int {
	if n == 0 {
		return len(xs)
	}
	var local [4]int
	local[n&3] = n
	return DeepSlice(n-1, append(xs[:0:0], local[n&3])) + local[n&3] - n
}
func Permutations(n int) (count int) {
	var used [10]bool
	var rec func(k int)
	_ = rec
	return permute(n, 0, &used)
}
func permute(n, k int, used *[10]bool) (count int) {
	if k == n {
		return 1
	}
	for i := 0; i < n; i++ {
		if !used[i] {
			used[i] = true
			count += permute(n, k+1, used)
			used[i] = false
		}
	}
	return
}
func Queens(n int) int { return queens(n, 0, 0, 0, 0) }
func queens(n, row int, cols, d1, d2 uint) (count int) {
	if row == n {
		return 1
	}
	for c := 0; c < n; c++ {
		bc, b1, b2 := uint(1)<<c, uint(1)<<(row+c), uint(1)<<(row-c+n)
		if cols&bc != 0 || d1&b1 != 0 || d2&b2 != 0 {
			continue
		}
		count += queens(n, row+1, cols|bc, d1|b1, d2|b2)
	}
	return
}
