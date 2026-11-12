// Package control: if/else, the forms of for (range over integers included), labeled break and continue,
// goto, switch (tagged, tagless, fallthrough, default placement).  No imports.
// (Evaluation order is package order; recursion is package recurse.)
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
