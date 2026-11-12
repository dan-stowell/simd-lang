package lib

type point struct {
	x, y int
}

var table = [8]uint8{1, 2, 3, 5, 8, 13, 21, 34}

var counter int

const big = 1 << 40

func Add(a, b int) int { return a + b }

func Fib(n int) int {
	a, b := 0, 1
	for i := 0; i < n; i++ {
		a, b = b, a+b
	}
	return a
}

func Sum(xs []int) (s int) {
	for _, x := range xs {
		s += x
	}
	return
}

func Table(i int) uint8 { return table[i] }

func Count() int {
	counter++
	return counter
}

func Dist(ax, ay, bx, by int) int {
	p := point{ax, ay}
	q := point{x: bx, y: by}
	dx := p.x - q.x
	if dx < 0 {
		dx = -dx
	}
	dy := p.y - q.y
	if dy < 0 {
		dy = -dy
	}
	return dx + dy
}

func Bytes(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' {
			n++
		}
	}
	return n
}

func Runes(s string) (n int, last rune) {
	for _, r := range s {
		n++
		last = r
	}
	return
}

func Ops(a, b int64, c uint32, d int8) int64 {
	x := a*b + a/b - a%b
	x ^= int64(c) << 3
	x |= int64(d)
	x &^= 0xf0
	x += big
	return x >> 2
}

func Float(a, b float64) float64 {
	c := a*b + a/b - b
	if c < 0 {
		c = -c
	}
	return c + float64(int(a))
}

func Switch(n int) string {
	switch {
	case n < 0:
		return "neg"
	case n == 0:
		return "zero"
	}
	switch n {
	case 1, 2, 3:
		return "small"
	case 4:
		fallthrough
	case 5:
		return "medium"
	default:
		return "large"
	}
}

func Append(xs []int, n int) []int {
	for i := range n {
		xs = append(xs, i, i*i)
	}
	return xs
}

func Slice(xs []int, i, j int) []int { return xs[i:j] }

func Concat(a, b string) string { return a + "-" + b }

func Eq(a, b string) bool { return a == b }

func Less(a, b string) bool { return a < b }

func Div(a, b int) int { return a / b }

func Shift(a uint64, n uint) uint64 { return a<<n | a>>(64-n) }
