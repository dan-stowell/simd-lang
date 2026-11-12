package gc_test

import (
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"testing"

	"fclang/gc"
)

func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			_, isRT := r.(runtime.Error)
			if e, ok := r.(error); ok {
				msg = fmt.Sprintf("runtime=%v error: %s", isRT, e.Error())
			} else {
				msg = fmt.Sprintf("runtime=%v value: %v", isRT, r)
			}
		}
	}()
	f()
	return "no panic"
}

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", name, got, want)
	}
}

var itoa = strconv.Itoa

// The expectations below are computed arithmetically, without building anything.

func wantTree(seed, depth int) uint64 {
	if depth == 0 {
		return 1
	}
	name := "n" + itoa(seed)
	h := uint64(seed)*31 + uint64(len(name))
	for i := 0; i < len(name); i++ {
		h = h*131 + uint64(name[i])
	}
	for i := 0; i < seed%29; i++ {
		h = h*137 + uint64(byte(seed+i))
	}
	return h ^ (wantTree(2*seed+1, depth-1)*1000003 + wantTree(2*seed+2, depth-1)*7)
}

func wantList(n int) (sum int) {
	for i := 0; i < n; i++ {
		sum += i * (i + 1)
		switch i % 4 {
		case 0:
			sum += i + 1000
		case 1:
			sum += len(itoa(i))
		case 2:
			sum += 2 * i
		case 3:
			sum += 3
		}
	}
	return
}

func wantDeep(n int) (s uint64) {
	// len(s) + i + m.A + *m.P + m.B + len(m.S) + int(m.C) + len(m.I) + m.D[1] + *m.Sl[0] + m.Q.A + sum(sl) + sum(arr)
	ps := uint64(len("param-"+itoa(n)) + (n + 1000) + n + 7 + 9 + 2 + 2 + 5 + 2 + 11 + 13 + (0 + 1 + 4 + 9 + 16) + (1 + 2 + 3 + 4))
	for k := 0; k <= n; k++ {
		s = s*31 + uint64(k) + ps
	}
	return
}

func wantAppend(n int) (sum int) {
	for i := 0; i < n; i++ {
		for j := 0; j <= i%17; j++ {
			sum += i * j
		}
		sum += i + i + i + len(itoa(i)) + i/2
	}
	return
}

func wantBig(x int) (s int) {
	for i := 0; i < 20; i++ {
		s += x + i + x*i
	}
	for i := 0; i < 4; i++ {
		s += len(itoa(x + i))
	}
	return
}

func TestTreeList(t *testing.T) {
	for _, c := range []struct{ seed, depth, every int }{{1, 1, 0}, {1, 6, 0}, {3, 10, 97}, {0, 12, 501}, {7, 14, 4001}} {
		eq(t, fmt.Sprint("Tree ", c), gc.Tree(c.seed, c.depth, c.every), wantTree(c.seed, c.depth))
	}
	for _, c := range []struct{ n, every int }{{0, 0}, {1, 1}, {10, 3}, {1000, 100}, {50000, 9000}} {
		sum, n := gc.List(c.n, c.every)
		eq(t, fmt.Sprint("List ", c), []int{sum, n}, []int{wantList(c.n), c.n})
	}
}

func TestTemporaries(t *testing.T) {
	for _, c := range [][2]int{{1, 2}, {-5, 100}, {123456, -7}} {
		x, y := c[0], c[1]
		eq(t, "TempPtrs", gc.TempPtrs(x, y), (3*x+y)+1000*(3*y+x))
		eq(t, "TempClosure", gc.TempClosure(x, y), x+y)
		eq(t, "TempMethods", gc.TempMethods(x, y, 9), (x+y+9)*10+2)
		eq(t, "TempBig", gc.TempBig(x, y), wantBig(x)+y)
	}
	for _, c := range [][2]string{{"", ""}, {"a", "b"}, {"a long enough string to need the heap, ", "and another one that is long as well"}} {
		a, b := c[0], c[1]
		eq(t, "TempStrings", gc.TempStrings(a, b), a+b+"|"+b+a+".|"+a+"."+b+".")
	}
	for _, c := range [][2]int{{1, 0}, {10, 9}, {1000, 500}} {
		eq(t, "TempIndex", gc.TempIndex(c[0], c[1]), c[1]*c[1]*1001)
		eq(t, "TempMap", gc.TempMap(c[0], c[1]), 7*c[1])
	}
	var s []int
	var np *int
	eq(t, "TempIndex oob", catch(func() { gc.TempIndex(3, 3) }), catch(func() { sink = make([]int, len(s)+3)[len(s)+3] }))
	eq(t, "TempMap miss", catch(func() { gc.TempMap(3, 3) }), catch(func() { sink = *np }))
	for _, x := range []int{0, 5, -1000, 1 << 40} {
		eq(t, "TempIfaces", gc.TempIfaces(x, "abc"), x+1000+4)
		eq(t, "TempMulti", gc.TempMulti(x), 2*x+1+len(itoa(x))+len(itoa(x+1)))
		l, l2 := len(itoa(x))+1, len(itoa(x+1))+1
		last := x%10 + 1
		if x >= 0 {
			eq(t, "Results", gc.Results(x), x+l+last*last+l+x+x+x+(x+1)+l2+(x+1))
		}
		eq(t, "Defers", gc.Defers(x), x+1+2*x+len(itoa(x)))
		eq(t, "BigCopies", gc.BigCopies(x), 9*wantBig(x)+1)
	}
}

var sink int

func TestDeep(t *testing.T) {
	for _, c := range []struct {
		n, every int
		goroutine bool
	}{{0, 0, false}, {1, 1, false}, {50, 7, true}, {3000, 100, true}, {20000, 5000, false}, {100000, 0, true}} {
		sum, bad := gc.DeepRun(c.n, c.every, c.goroutine)
		eq(t, fmt.Sprint("DeepRun ", c), []any{sum, bad}, []any{wantDeep(c.n), 0})
	}
	for _, d := range []int{0, 1, 100, 5000, 100000} {
		v, g := gc.StackPtrs(d)
		eq(t, fmt.Sprint("StackPtrs ", d), []int{v, g}, []int{26*d + 46, d * (d + 1) / 2})
	}
}

func TestWrites(t *testing.T) {
	eq(t, "WriteShapes", gc.WriteShapes(2000, 100, false), 0)
	eq(t, "WriteShapes bg", gc.WriteShapes(30000, 0, true), 0)
	for _, c := range []struct {
		n, rounds int
		bg        bool
	}{{1, 10, false}, {100, 5000, false}, {5000, 200000, true}, {50000, 300000, true}} {
		sum, bad := gc.Shuffle(c.n, c.rounds, c.bg)
		eq(t, fmt.Sprint("Shuffle ", c), []any{sum, bad}, []any{uint64(c.n * (c.n - 1) / 2), 0})
	}
}

func TestContainers(t *testing.T) {
	for _, c := range []struct{ n, every int }{{0, 0}, {1, 1}, {100, 7}, {5000, 400}} {
		n := c.n
		eq(t, fmt.Sprint("AppendLoop ", c), gc.AppendLoop(n, c.every), wantAppend(n))

		wsum, wsize, wlen := 0, 0, 0
		for i := 0; i < n; i++ {
			if i%3 != 0 {
				wsum += i
				wsize++
			}
			wlen += len("n"+itoa(i)) + len(itoa(i%100))
		}
		sum, size, strLen, bad := gc.MapPtrs(n, c.every)
		eq(t, fmt.Sprint("MapPtrs ", c), []int{sum, size, strLen, bad}, []int{wsum, wsize, wlen, 0})

		total, csum, isum, chsum := 0, 2*n, 0, n
		for i := 0; i < n; i++ {
			total += len(itoa(i)) + 1 + len(itoa(i*i))
			csum += 6*i + 3
			isum += (i + 2) + len(itoa(i)) + 3*i + i%256
			chsum += len(itoa(i)) + len(itoa(i*1000))
		}
		b, baseLen, tot := gc.Strings(n, c.every)
		eq(t, fmt.Sprint("Strings ", c), []int{b, baseLen, tot}, []int{0, n, total})
		eq(t, fmt.Sprint("Closures ", c), gc.Closures(n, c.every), csum)
		s, b := gc.Interior(n)
		eq(t, fmt.Sprint("Interior ", c), []int{s, b}, []int{isum, 0})
		if n <= 1000 {
			s, b = gc.Chans(n)
			eq(t, fmt.Sprint("Chans ", c), []int{s, b}, []int{chsum, 0})
		}
	}
}

func TestConcurrent(t *testing.T) {
	const workers = 6
	trees, deeps, lists, appends, bad := gc.Concurrent(workers)
	for w := 0; w < workers; w++ {
		eq(t, fmt.Sprint("worker ", w), []any{trees[w], deeps[w], lists[w], appends[w]},
			[]any{wantTree(w+1, 8), wantDeep(500 + w), wantList(300 + w), wantAppend(200 + w)})
	}
	eq(t, "bad", bad, 0)
}
