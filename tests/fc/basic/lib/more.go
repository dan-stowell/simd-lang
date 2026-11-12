package lib

func Count3(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func Pairs(xs []string) func(yield func(int, string) bool) {
	return func(yield func(int, string) bool) {
		for i, x := range xs {
			if !yield(i, x) {
				return
			}
		}
	}
}

func SumSeq(n, stop int) (s int) {
	for i := range Count3(n) {
		if i == stop {
			break
		}
		if i%2 == 1 {
			continue
		}
		s += i
	}
	return
}

func FindSeq(xs []string, want string) (int, bool) {
	for i, x := range Pairs(xs) {
		if x == want {
			return i, true
		}
	}
	return -1, false
}

func Nested(n int) int {
	total := 0
outer:
	for i := range Count3(n) {
		for j := range Count3(n) {
			if j > i {
				continue outer
			}
			if i+j > 12 {
				break outer
			}
			total += i * j
		}
	}
	return total
}

type vec struct{ x, y int }

func (v vec) add(w vec) vec { return vec{v.x + w.x, v.y + w.y} }
func (v *vec) scale(k int)  { v.x *= k; v.y *= k }

func MethodExprs(a, b, k int) int {
	f := vec.add
	g := (*vec).scale
	v := f(vec{a, b}, vec{1, 1})
	g(&v, k)
	return v.x*1000 + v.y
}

func Cplx(a, b, c, d float64) (float64, float64, bool) {
	x := complex(a, b)
	y := complex(c, d)
	z := x*y + x/y - y
	return real(z), imag(z), x == y
}

func Print(n int, s string) {
	print("fc print: ", n, " ", s, " ", 1.5, " ", true, "\n")
	println("fc println:", n, s)
}
