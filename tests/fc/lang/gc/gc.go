// Package gc holds allocation-heavy code whose data must survive garbage collection
// and stack growth: it is meant to shake out stack-map and write-barrier mistakes.
// Imports: runtime (GC), sync (WaitGroup, Mutex).
package gc

import (
	"runtime"
	"sync"
)

type Node struct {
	Left, Right *Node
	Key         int
	Name        string
	Data        []byte
}

type Cell struct {
	Val  int
	Next *Cell
	Tag  any
}

// Mixed interleaves pointer and scalar words.
type Mixed struct {
	A  int
	P  *int
	B  uint64
	S  string
	C  float64
	I  any
	D  [3]int
	Sl []*int
	Q  *Mixed
}

type Big struct {
	Pad  [20]uint64
	P    [20]*int
	S    [4]string
	Tail *Big
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [24]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// churn allocates and drops garbage in the size classes the tests use, filled with
// recognizable junk, so that memory freed while still in use gets overwritten.
func churn(n int) int {
	keep := make([][]*Node, 4)
	t := 0
	for i := 0; i < n; i++ {
		b := make([]byte, 8+(i%16)*8)
		for j := range b {
			b[j] = 0xAA
		}
		s := make([]*Node, 1+i%8)
		s[0] = &Node{Key: -1, Name: "junk", Data: b}
		keep[i%4] = s
		p := new(int)
		*p = -12345
		t += len(b) + *p
	}
	return t + len(keep)
}

// Collect runs a full collection and then reuses whatever it freed.
func Collect() {
	runtime.GC()
	churn(300)
}

func withBackgroundGC(f func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				runtime.GC()
				churn(50)
			}
		}
	}()
	f()
	close(done)
	wg.Wait()
}

func newInt(x int) *int {
	p := new(int)
	*p = x
	return p
}

func gcInt(x int) *int {
	Collect()
	return newInt(x)
}

func mkNode(seed int) *Node {
	d := make([]byte, seed%29)
	for i := range d {
		d[i] = byte(seed + i)
	}
	return &Node{Key: seed, Name: "n" + itoa(seed), Data: d}
}

func okNode(n *Node, seed int) bool {
	if n == nil || n.Key != seed || n.Name != "n"+itoa(seed) || len(n.Data) != seed%29 {
		return false
	}
	for i, c := range n.Data {
		if c != byte(seed+i) {
			return false
		}
	}
	return true
}

// ---- trees and lists ----

func buildTree(seed, depth, every int) *Node {
	if depth == 0 {
		return nil
	}
	n := mkNode(seed)
	n.Left = buildTree(2*seed+1, depth-1, every)
	if every > 0 && seed%every == 0 {
		Collect()
	}
	n.Right = buildTree(2*seed+2, depth-1, every)
	return n
}

func treeSum(n *Node) uint64 {
	if n == nil {
		return 1
	}
	h := uint64(n.Key)*31 + uint64(len(n.Name))
	for _, c := range []byte(n.Name) {
		h = h*131 + uint64(c)
	}
	for _, c := range n.Data {
		h = h*137 + uint64(c)
	}
	return h ^ (treeSum(n.Left)*1000003 + treeSum(n.Right)*7)
}

func Tree(seed, depth, every int) uint64 {
	t := buildTree(seed, depth, every)
	Collect()
	return treeSum(t)
}

func tagFor(i int) any {
	switch i % 4 {
	case 0:
		return i + 1000
	case 1:
		return itoa(i)
	case 2:
		return &Cell{Val: i}
	}
	return nil
}

func tagVal(t any) int {
	switch v := t.(type) {
	case int:
		return v
	case string:
		return len(v)
	case *Cell:
		return v.Val * 2
	}
	return 3
}

func List(n, every int) (sum, length int) {
	var head *Cell
	for i := 0; i < n; i++ {
		head = &Cell{i, head, tagFor(i)}
		if every > 0 && i%every == 0 {
			Collect()
		}
	}
	var prev *Cell
	for head != nil {
		next := head.Next
		head.Next = prev
		prev = head
		head = next
	}
	Collect()
	for c := prev; c != nil; c = c.Next {
		length++
		sum += c.Val*length + tagVal(c.Tag)
	}
	return
}

// ---- values live only in temporaries across a call that collects ----

func deref2(p, q *int) int { return *p*3 + *q }

func TempPtrs(x, y int) int {
	return deref2(newInt(x), gcInt(y)) + 1000*deref2(gcInt(y), newInt(x))
}

func gcStr(s string) string {
	Collect()
	return s + "."
}
func cat3(a, b, c string) string { return a + "|" + b + "|" + c }

func TempStrings(a, b string) string { return cat3(a+b, gcStr(b+a), gcStr(a)+gcStr(b)) }

func mkSlice(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i * i
	}
	return s
}
func gcIdx(i int) int {
	Collect()
	return i
}

func TempIndex(n, i int) int { return mkSlice(n)[gcIdx(i)] + mkSlice(n)[gcIdx(i):][0]*1000 }

func mkMap(n int) map[string]*int {
	m := map[string]*int{}
	for i := 0; i < n; i++ {
		m[itoa(i)] = newInt(i * 7)
	}
	return m
}
func gcKey(i int) string {
	Collect()
	return itoa(i)
}

func TempMap(n, k int) int { return *mkMap(n)[gcKey(k)] }

func box(x int) any { return x + 1000 }
func gcBox(s string) any {
	Collect()
	return s + "!"
}
func unbox(a, b any) int { return a.(int) + len(b.(string)) }

func TempIfaces(x int, s string) int { return unbox(box(x), gcBox(s)) }

type acc struct {
	v   *int
	log []string
}

func newAcc(x int) *acc { return &acc{v: newInt(x)} }
func (a *acc) Add(p *int) *acc {
	*a.v += *p
	a.log = append(a.log, itoa(*p))
	return a
}
func (a *acc) Val() int { return *a.v*10 + len(a.log) }

func TempMethods(x, y, z int) int { return newAcc(x).Add(gcInt(y)).Add(gcInt(z)).Val() }

func mkAdder(p *int) func(int) int { return func(x int) int { return *p + x } }

func TempClosure(x, y int) int { return mkAdder(newInt(x))(*gcInt(y)) }

func twoPtrs(x int) (*int, *string) {
	s := itoa(x)
	return newInt(x), &s
}

func TempMulti(x int) int {
	p, s := twoPtrs(x)
	Collect()
	q, t := twoPtrs(x + 1)
	Collect()
	return *p + *q + len(*s) + len(*t)
}

func results(x int) (p *int, s string, sl []int, e any, m Mixed) {
	p = newInt(x)
	s = itoa(x) + "r"
	sl = mkSlice(x%10 + 2)
	e = s
	m = Mixed{A: x, P: p, S: s, Sl: []*int{p}}
	Collect()
	return
}

func Results(x int) int {
	p, s, sl, e, m := results(x)
	Collect()
	p2, s2, _, _, m2 := results(x + 1)
	Collect()
	return *p + len(s) + sl[len(sl)-1] + len(e.(string)) + m.A + *m.P + *m.Sl[0] + *p2 + len(s2) + *m2.P
}

// ---- big structs with pointers, copied around ----

func mkBig(x int) Big {
	var b Big
	for i := range b.Pad {
		b.Pad[i] = uint64(x + i)
		b.P[i] = newInt(x * i)
	}
	for i := range b.S {
		b.S[i] = itoa(x + i)
	}
	return b
}

func bigSum(b *Big) (s int) {
	for i := range b.Pad {
		s += int(b.Pad[i]) + *b.P[i]
	}
	for i := range b.S {
		s += len(b.S[i])
	}
	if b.Tail != nil {
		s += 1 + bigSum(b.Tail)
	}
	return
}

func useBig(b Big, p *int) int {
	Collect()
	return bigSum(&b) + *p
}

func TempBig(x, y int) int { return useBig(mkBig(x), gcInt(y)) }

func BigCopies(x int) int {
	b := mkBig(x)
	p := new(Big)
	*p = b
	Collect()
	sl := make([]Big, 3)
	sl[1] = b
	sl[2] = *p
	sl[2].Tail = p
	var a any = b
	m := map[int]Big{x: b}
	ch := make(chan Big, 1)
	ch <- b
	arr := [2]Big{b, *p}
	b = Big{}
	Collect()
	fromCh := <-ch
	mb := m[x]
	ab := a.(Big)
	return bigSum(p) + bigSum(&sl[1]) + bigSum(&sl[2]) + bigSum(&ab) + bigSum(&mb) + bigSum(&fromCh) + bigSum(&arr[0]) + bigSum(&arr[1]) + int(b.Pad[0])
}

// ---- deep recursion holding pointers of every shape ----

func paramSum(s string, sl []int, i any, m *Mixed, arr *[4]*int) uint64 {
	t := len(s) + i.(int) + m.A + *m.P + int(m.B) + len(m.S) + int(m.C) + len(m.I.(string)) + m.D[1] + *m.Sl[0] + m.Q.A
	for _, v := range sl {
		t += v
	}
	for _, p := range arr {
		t += *p
	}
	return uint64(t)
}

func Deep(n int, s string, sl []int, i any, m Mixed, arr [4]*int, every int) (sum uint64, bad int) {
	var pad [40]int
	pad[n%40] = n
	p := newInt(n)
	str := itoa(n)
	loc := []int{n, n + 1}
	var bx any = str
	lm := Mixed{A: n, P: p, B: uint64(n) << 32, S: str, C: float64(n) / 2, I: p, D: [3]int{n, n, n}, Sl: []*int{p, m.P}, Q: &m}
	la := [4]*int{p, arr[1], arr[2], p}
	if n == 0 {
		Collect()
	} else {
		if every > 0 && n%every == 0 {
			Collect()
		}
		sum, bad = Deep(n-1, s, sl, i, m, arr, every)
	}
	if *p != n || str != itoa(n) || loc[0] != n || loc[1] != n+1 || bx.(string) != str || pad[n%40] != n {
		bad++
	}
	if lm.A != n || lm.P != p || lm.B != uint64(n)<<32 || lm.S != str || lm.C != float64(n)/2 || lm.I.(*int) != p ||
		lm.D[2] != n || lm.Sl[0] != p || lm.Sl[1] != m.P || lm.Q != &m || lm.Q.S != m.S {
		bad++
	}
	if la[0] != p || la[1] != arr[1] || la[2] != arr[2] || *la[3] != n {
		bad++
	}
	return sum*31 + uint64(n) + paramSum(s, sl, i, &m, &arr), bad
}

func DeepRun(n, every int, inGoroutine bool) (sum uint64, bad int) {
	run := func() {
		m := Mixed{A: n, P: newInt(7), B: 9, S: "ms", C: 2.5, I: "iface", D: [3]int{1, 2, 3}, Sl: []*int{newInt(11)}, Q: &Mixed{A: 13}}
		arr := [4]*int{newInt(1), newInt(2), newInt(3), newInt(4)}
		sum, bad = Deep(n, "param-"+itoa(n), mkSlice(5), any(n+1000), m, arr, every)
	}
	if !inGoroutine {
		run()
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		run()
	}()
	wg.Wait()
	return
}

func grow(n int) int {
	var pad [128]int
	pad[n%128] = n
	if n == 0 {
		return 0
	}
	return grow(n-1) + pad[n%128]
}

// StackPtrs keeps pointers and slices into its own frame across stack growth.
func StackPtrs(depth int) (int, int) {
	done := make(chan [2]int)
	go func() {
		var buf [32]int
		for i := range buf {
			buf[i] = i * depth
		}
		p := &buf[5]
		s := buf[8:16]
		pp := &p
		g := grow(depth)
		*p += 1
		s[0] += 2
		**pp += 4
		done <- [2]int{buf[5] + buf[8] + *p + s[0] + len(s) + cap(s), g}
	}()
	r := <-done
	return r[0], r[1]
}

// ---- pointer writes into heap objects of every shape ----

type Holder struct {
	F   *Node
	Arr [4]*Node
	Sl  []*Node
	M   map[int]*Node
	I   any
	S   string
	Fn  func() *Node
	In  struct {
		X int
		P *Node
	}
	Ch  chan *Node
	PP  **Node
	Str []string
	Ifs []any
}

func writeShapes(rounds, every int) (bad int) {
	h := &Holder{Sl: make([]*Node, 4), M: map[int]*Node{}, Ch: make(chan *Node, 1), PP: new(*Node), Str: make([]string, 2), Ifs: make([]any, 2)}
	for k := 0; k < rounds; k++ {
		h.F = mkNode(k)
		h.Arr[k%4] = mkNode(k + 1)
		h.Sl[k%4] = mkNode(k + 2)
		h.M[k%8] = mkNode(k + 3)
		h.I = mkNode(k + 4)
		h.S = "s" + itoa(k+5)
		n6 := mkNode(k + 6)
		h.Fn = func() *Node { return n6 }
		h.In.P = mkNode(k + 7)
		select {
		case <-h.Ch:
		default:
		}
		h.Ch <- mkNode(k + 8)
		*h.PP = mkNode(k + 9)
		h.Str[k%2] = itoa(k + 10)
		h.Ifs[k%2] = itoa(k + 11)
		if k%32 == 0 {
			churn(8)
		}
		if every > 0 && k%every == 0 {
			Collect()
		}
		ok := okNode(h.F, k) && okNode(h.Arr[k%4], k+1) && okNode(h.Sl[k%4], k+2) && okNode(h.M[k%8], k+3) &&
			okNode(h.I.(*Node), k+4) && h.S == "s"+itoa(k+5) && okNode(h.Fn(), k+6) && okNode(h.In.P, k+7) &&
			okNode(*h.PP, k+9) && h.Str[k%2] == itoa(k+10) && h.Ifs[k%2].(string) == itoa(k+11)
		if k > 0 {
			ok = ok && okNode(h.Arr[(k-1)%4], k) && okNode(h.Sl[(k-1)%4], k+1) && okNode(h.M[(k-1)%8], k+2) && h.Str[(k-1)%2] == itoa(k+9)
		}
		if !ok {
			bad++
		}
	}
	if !okNode(<-h.Ch, rounds+7) {
		bad++
	}
	return
}

func WriteShapes(rounds, every int, bg bool) (bad int) {
	if bg {
		withBackgroundGC(func() { bad = writeShapes(rounds, every) })
		return
	}
	return writeShapes(rounds, every)
}

// Shuffle moves pointers around inside a heap array while collections run.
func Shuffle(n, rounds int, bg bool) (sum uint64, bad int) {
	run := func() {
		arr := make([]*Node, n)
		for i := range arr {
			arr[i] = mkNode(i)
		}
		r := uint32(12345)
		for k := 0; k < rounds; k++ {
			r = r*1664525 + 1013904223
			i := int(r>>8) % n
			r = r*1664525 + 1013904223
			j := int(r>>8) % n
			t := arr[i]
			arr[i] = nil
			if k%64 == 0 {
				churn(4)
			}
			if !bg && k%(rounds/8+1) == 0 {
				Collect()
			}
			arr[i] = arr[j]
			arr[j] = t
		}
		seen := make([]bool, n)
		for _, nd := range arr {
			if nd == nil || nd.Key < 0 || nd.Key >= n || seen[nd.Key] || !okNode(nd, nd.Key) {
				bad++
				continue
			}
			seen[nd.Key] = true
			sum += uint64(nd.Key)
		}
	}
	if bg {
		withBackgroundGC(run)
	} else {
		run()
	}
	return
}

// ---- containers filled in loops ----

func AppendLoop(n, every int) (sum int) {
	var all [][]int
	var ptrs []*int
	var ms []Mixed
	for i := 0; i < n; i++ {
		var s []int
		for j := 0; j <= i%17; j++ {
			s = append(s, i*j)
		}
		all = append(all, s)
		ptrs = append(ptrs, newInt(i))
		ms = append(ms, Mixed{A: i, P: ptrs[i], S: itoa(i), Sl: ptrs[i/2 : i/2+1]})
		if every > 0 && i%every == 0 {
			Collect()
		}
	}
	Collect()
	for i := 0; i < n; i++ {
		for _, v := range all[i] {
			sum += v
		}
		sum += *ptrs[i] + ms[i].A + *ms[i].P + len(ms[i].S) + *ms[i].Sl[0]
	}
	return
}

func MapPtrs(n, every int) (sum, size, strLen, bad int) {
	m := map[int]*Node{}
	ms := map[string][]string{}
	mp := map[*Node]int{}
	for i := 0; i < n; i++ {
		nd := mkNode(i)
		m[i] = nd
		k := itoa(i % 100)
		ms[k] = append(ms[k], nd.Name)
		mp[nd] = i
		if every > 0 && i%every == 0 {
			Collect()
		}
	}
	for i := 0; i < n; i += 3 {
		delete(m, i)
	}
	Collect()
	for i := 0; i < n; i++ {
		nd, ok := m[i]
		if i%3 == 0 {
			if ok {
				bad++
			}
			continue
		}
		if !okNode(nd, i) || mp[nd] != i {
			bad++
		}
		sum += i
	}
	for nd, i := range mp {
		if !okNode(nd, i) {
			bad++
		}
	}
	for k, v := range ms {
		for _, s := range v {
			strLen += len(s) + len(k)
		}
	}
	return sum, len(m), strLen, bad
}

func Strings(n, every int) (bad, baseLen, total int) {
	var ss, subs []string
	var rs [][]rune
	var bs [][]byte
	base := ""
	for i := 0; i < n; i++ {
		b := []byte(itoa(i))
		b = append(b, '-')
		s := string(b) + itoa(i*i)
		ss = append(ss, s)
		subs = append(subs, s[1:])
		rs = append(rs, []rune(s))
		bs = append(bs, b)
		base += s[:1]
		if every > 0 && i%every == 0 {
			Collect()
		}
	}
	Collect()
	for i := 0; i < n; i++ {
		want := itoa(i) + "-" + itoa(i*i)
		if ss[i] != want || subs[i] != want[1:] || string(rs[i]) != want || string(bs[i]) != itoa(i)+"-" || base[i] != want[0] {
			bad++
		}
		total += len(ss[i])
	}
	return bad, len(base), total
}

func Closures(n, every int) (sum int) {
	var fs []func() int
	shared := newInt(0)
	for i := 0; i < n; i++ {
		p := newInt(i)
		loc := i * 2
		fs = append(fs, func() int {
			*shared++
			loc++
			return *p + loc
		})
		if every > 0 && i%every == 0 {
			Collect()
		}
	}
	Collect()
	for _, f := range fs {
		sum += f()
	}
	Collect()
	for _, f := range fs {
		sum += f()
	}
	return sum + *shared
}

type obj struct {
	Pad [3]int
	D   [4]int
	S   string
	N   Node
}

// Interior keeps objects alive only through pointers into their middles.
func Interior(n int) (sum, bad int) {
	ips := make([]*int, n)
	sps := make([]*string, n)
	kps := make([]*int, n)
	subs := make([][]byte, n)
	for i := 0; i < n; i++ {
		o := &obj{D: [4]int{i, i + 1, i + 2, i + 3}, S: itoa(i)}
		o.N.Key = i * 3
		ips[i] = &o.D[2]
		sps[i] = &o.S
		kps[i] = &o.N.Key
		big := make([]byte, 1000)
		big[500] = byte(i)
		big[999] = byte(i + 1)
		subs[i] = big[500:501]
		if i%100 == 0 {
			Collect()
		}
	}
	Collect()
	for i := 0; i < n; i++ {
		sum += *ips[i] + len(*sps[i]) + *kps[i] + int(subs[i][0])
		if cap(subs[i]) != 500 || subs[i][:500][499] != byte(i+1) || *sps[i] != itoa(i) {
			bad++
		}
	}
	return
}

func Defers(x int) (res int) {
	p := newInt(x)
	s := []string{itoa(x)}
	defer func() {
		r := recover()
		Collect()
		q := r.(*int)
		res = *q + *p + len(s[0])
	}()
	defer Collect()
	defer func(q *int) { *q *= 2 }(p)
	Collect()
	panic(gcInt(x + 1))
}

func Chans(n int) (sum, bad int) {
	ch := make(chan *Node, n)
	for i := 0; i < n; i++ {
		ch <- mkNode(i)
	}
	Collect()
	close(ch)
	i := 0
	for nd := range ch {
		if !okNode(nd, i) {
			bad++
		}
		i++
	}
	out := make(chan []string)
	go func() {
		for i := 0; i < n; i++ {
			out <- []string{itoa(i), itoa(i * 1000)}
			if i%50 == 0 {
				Collect()
			}
		}
		close(out)
	}()
	for v := range out {
		sum += len(v[0]) + len(v[1])
	}
	return sum + i, bad
}

// Concurrent runs several of the above in parallel goroutines.
func Concurrent(workers int) (trees []uint64, deeps []uint64, lists []int, appends []int, bad int) {
	trees = make([]uint64, workers)
	deeps = make([]uint64, workers)
	lists = make([]int, workers)
	appends = make([]int, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trees[w] = Tree(w+1, 8, 60)
			d, b := DeepRun(500+w, 200, false)
			deeps[w] = d
			lists[w], _ = List(300+w, 100)
			appends[w] = AppendLoop(200+w, 70)
			_, b2 := Shuffle(200, 2000, false)
			b3 := writeShapes(300, 100)
			mu.Lock()
			bad += b + b2 + b3
			mu.Unlock()
		}()
	}
	wg.Wait()
	return
}
