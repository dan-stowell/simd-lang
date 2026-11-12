package clo

type Counter struct {
	n    int
	name string
}

func (c *Counter) Inc(d int) int { c.n += d; return c.n }
func (c Counter) Get() int       { return c.n }
func (c Counter) Name() string   { return c.name }

func Adder(base int) func(int) int {
	return func(x int) int { base += x; return base }
}

func Apply(f func(int) int, xs []int) (s int) {
	for _, x := range xs {
		s += f(x)
	}
	return
}

func Funcs(n int) []func() int {
	var fs []func() int
	for i := 0; i < n; i++ {
		fs = append(fs, func() int { return i * i })
	}
	return fs
}

func Bound(c *Counter) (func(int) int, func() int, func() string) {
	return c.Inc, c.Get, c.Name
}

func Defers(log *[]int) (r int) {
	defer func() { r *= 2 }()
	for i := 0; i < 3; i++ {
		defer func() { *log = append(*log, i) }()
	}
	c := &Counter{n: 5}
	defer c.Inc(10)
	defer add(log, 100+c.n)
	c.n = 7
	return 21
}

func add(log *[]int, v int) { *log = append(*log, v) }

func Recover(f func()) (msg string) {
	defer func() {
		if e := recover(); e != nil {
			if s, ok := e.(string); ok {
				msg = s
			} else if err, ok := e.(error); ok {
				msg = err.Error()
			} else {
				msg = "other"
			}
		}
	}()
	f()
	return "none"
}

func Div(a, b int) int { return a / b }

func Go(n int) int {
	ch := make(chan int)
	for i := 0; i < n; i++ {
		go func(k int) { ch <- k * 2 }(i)
	}
	s := 0
	for i := 0; i < n; i++ {
		s += <-ch
	}
	return s
}

func Fact(n int) int {
	var f func(int) int
	f = func(k int) int {
		if k <= 1 {
			return 1
		}
		return k * f(k-1)
	}
	return f(n)
}

type Shape interface {
	Area() float64
	Name() string
}

type Rect struct{ W, H float64 }
type Circle struct{ R float64 }

func (r Rect) Area() float64    { return r.W * r.H }
func (r Rect) Name() string     { return "rect" }
func (c *Circle) Area() float64 { return 3 * c.R * c.R }
func (c *Circle) Name() string  { return "circle" }

func Total(ss []Shape) (t float64, names string) {
	for _, s := range ss {
		t += s.Area()
		names += s.Name() + ","
	}
	return
}

func Describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int:
		if x > 10 {
			return "big int"
		}
		return "int"
	case string:
		return "string " + x
	case Shape:
		return "shape " + x.Name()
	case error:
		return "error " + x.Error()
	case []int, [2]int:
		return "ints"
	}
	return "unknown"
}

func AsRect(s Shape) (Rect, bool) {
	r, ok := s.(Rect)
	return r, ok
}

func MustCircle(s Shape) *Circle { return s.(*Circle) }

func Box(n int) any      { return n }
func BoxS(s string) any  { return s }
func BoxR(r Rect) Shape  { return r }
func Same(a, b any) bool { return a == b }
func IsNil(s Shape) bool { return s == nil }

func Words(text string) map[string]int {
	m := map[string]int{}
	start := -1
	for i := 0; i <= len(text); i++ {
		if i < len(text) && text[i] != ' ' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			m[text[start:i]]++
			start = -1
		}
	}
	return m
}

func MapOps(n int) (sum, cnt int, has, gone bool) {
	m := make(map[int]int, 4)
	for i := 0; i < n; i++ {
		m[i] = i * 3
	}
	m[2] += 5
	delete(m, 1)
	_, gone = m[1]
	v, has := m[2]
	sum += v
	for k, v := range m {
		sum += k + v
		cnt++
	}
	return sum, len(m)*1000 + cnt, has, gone
}

var Table = map[string][]int{"a": {1, 2}, "b": {3}}

func Lookup(k string) int {
	s := 0
	for _, v := range Table[k] {
		s += v
	}
	return s
}
