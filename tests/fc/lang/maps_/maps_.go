// Package maps_ exercises maps: every kind of key, lookup, assignment, deletion, compound assignment,
// composite values, nil maps, range, literals, clear and large maps.
// Imports: none.
package maps_

// ---- basic operations on map[int]int ----

func Squares(n int) map[int]int {
	m := make(map[int]int)
	for i := 0; i < n; i++ {
		m[i] = i * i
	}
	return m
}

func Get(m map[int]int, k int) int           { return m[k] }
func GetOk(m map[int]int, k int) (int, bool) { v, ok := m[k]; return v, ok }
func Has(m map[int]int, k int) bool          { _, ok := m[k]; return ok }
func Set(m map[int]int, k, v int)            { m[k] = v }
func Del(m map[int]int, k int)               { delete(m, k) }
func Len(m map[int]int) int                  { return len(m) }
func AddTo(m map[int]int, k, v int) int      { m[k] += v; return m[k] }
func Incr(m map[int]int, k int) int          { m[k]++; return m[k] }
func Decr(m map[int]int, k int)              { m[k]-- }
func IsNil(m map[int]int) bool               { return m == nil }
func Clear(m map[int]int)                    { clear(m) }
func MakeHint(n int) map[int]int             { return make(map[int]int, n) }

// OpAssign applies every compound operator to m[k].
func OpAssign(m map[int]int, k, v int) int {
	m[k] += v
	m[k] *= 3
	m[k] -= 1
	m[k] <<= 2
	m[k] |= 1
	m[k] ^= v
	m[k] %= 1000003
	m[k] /= 2
	m[k] &= 0xffff
	m[k] >>= 1
	m[k] &^= 4
	return m[k]
}

// GetOkExisting assigns the comma-ok results to variables that already exist.
func GetOkExisting(m map[string]int, keys []string) (sum, found int) {
	var v int
	var ok bool
	for _, k := range keys {
		v, ok = m[k]
		if ok {
			found++
		}
		sum += v
	}
	return
}

// Swap exchanges two entries with a tuple assignment.
func Swap(m map[string]int, a, b string) {
	m[a], m[b] = m[b], m[a]
}

var trace []string

func key(s string) string { trace = append(trace, "key:"+s); return s }
func val(v int) int       { trace = append(trace, "val"); return v }

// EvalOrder: in m[f()] = g() the index operand is evaluated before the right-hand side.
func EvalOrder(m map[string]int) []string {
	trace = nil
	m[key("a")] = val(1)
	m[key("b")], m[key("c")] = val(2), val(3)
	m[key("a")] += val(10)
	return trace
}

// ---- key kinds ----

type Pt struct{ X, Y int }

type Rec struct {
	Name string
	ID   int32
	Ok   bool
	F    float64
}

type IKey struct {
	K any
	N int
}

func CountInt8(xs []int8) map[int8]int {
	m := map[int8]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountUint16(xs []uint16) map[uint16]int {
	m := map[uint16]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountInt32(xs []int32) map[int32]int {
	m := map[int32]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountUint64(xs []uint64) map[uint64]int {
	m := map[uint64]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountBool(xs []bool) map[bool]int {
	m := map[bool]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountStrings(xs []string) map[string]int {
	m := map[string]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountFloats(xs []float64) map[float64]int {
	m := map[float64]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountFloat32(xs []float32) map[float32]int {
	m := map[float32]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountComplex(xs []complex128) map[complex128]int {
	m := map[complex128]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountPts(xs []Pt) map[Pt]int {
	m := map[Pt]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountRecs(xs []Rec) map[Rec]int {
	m := map[Rec]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountArrays(xs [][2]string) map[[2]string]int {
	m := map[[2]string]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountAny(xs []any) map[any]int {
	m := map[any]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountIKeys(xs []IKey) map[IKey]int {
	m := map[IKey]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountPtrs(xs []*int) map[*int]int {
	m := map[*int]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

func CountChans(xs []chan int) map[chan int]int {
	m := map[chan int]int{}
	for _, x := range xs {
		m[x]++
	}
	return m
}

type BigKey [200]byte

func CountBigKeys(seeds []byte) map[BigKey]int {
	m := map[BigKey]int{}
	for _, s := range seeds {
		var k BigKey
		for i := range k {
			k[i] = s + byte(i)
		}
		m[k]++
	}
	return m
}

// NaN: every insertion with a NaN key makes a new entry, which can be found only by range.
func NaN(nan float64, n int) (length int, found bool, afterDelete int, sum int, afterClear int) {
	m := map[float64]int{}
	for i := 0; i < n; i++ {
		m[nan] = i
	}
	length = len(m)
	_, found = m[nan]
	delete(m, nan)
	afterDelete = len(m)
	for k, v := range m {
		if k == k {
			sum = -1000000
		}
		sum += v
	}
	clear(m)
	afterClear = len(m)
	return
}

// Zeros: +0 and -0 are the same key; assigning through -0 overwrites the stored key (stock: -0 afterwards).
func Zeros(pz, nz float64) (int, int, float64) {
	m := map[float64]int{}
	m[pz] = 1
	m[nz] += 10
	var k0 float64
	for k := range m {
		k0 = k
	}
	return len(m), m[nz], 1 / k0
}

// AnyGet looks v up in a map with interface keys (panics when the dynamic type is not hashable).
func AnyGet(m map[any]int, k any) (int, bool) {
	v, ok := m[k]
	return v, ok
}

func AnySet(m map[any]int, k any, v int) { m[k] = v }
func AnyDel(m map[any]int, k any)        { delete(m, k) }

// ---- composite values ----

func GroupByLen(words []string) map[int][]string {
	m := map[int][]string{}
	for _, w := range words {
		m[len(w)] = append(m[len(w)], w)
	}
	return m
}

func Nested(pairs [][2]string) map[string]map[string]int {
	m := map[string]map[string]int{}
	for _, p := range pairs {
		inner := m[p[0]]
		if inner == nil {
			inner = map[string]int{}
			m[p[0]] = inner
		}
		inner[p[1]]++
	}
	return m
}

// StructValues: a struct value in a map is replaced as a whole.
func StructValues(names []string) map[string]Pt {
	m := map[string]Pt{}
	for i, n := range names {
		p := m[n]
		p.X++
		p.Y += i
		m[n] = p
	}
	return m
}

// PtrValues: fields are updated through pointer values.
func PtrValues(names []string) map[string]*Pt {
	m := map[string]*Pt{}
	for i, n := range names {
		if m[n] == nil {
			m[n] = &Pt{}
		}
		m[n].X++
		m[n].Y += i
	}
	return m
}

type Wide struct {
	A [20]int
	S string
}

// WideValues stores values larger than 128 bytes.
func WideValues(n int) map[int]Wide {
	m := map[int]Wide{}
	for i := 0; i < n; i++ {
		var w Wide
		for j := range w.A {
			w.A[j] = i*100 + j
		}
		w.S = "w"
		m[i] = w
	}
	return m
}

func WideSum(m map[int]Wide, k int) int {
	t := 0
	for _, v := range m[k].A {
		t += v
	}
	return t + len(m[k].S)
}

func FuncValues(x int) map[string]int {
	fs := map[string]func(int) int{
		"double": func(v int) int { return v * 2 },
		"neg":    func(v int) int { return -v },
		"same":   nil,
	}
	out := map[string]int{}
	for name, f := range fs {
		if f == nil {
			out[name] = x
		} else {
			out[name] = f(x)
		}
	}
	return out
}

func StringOps(m map[string]string, k, suffix string) string {
	m[k] += suffix
	m[k] += suffix
	return m[k]
}

func Set_(xs []string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}

// ---- nil maps ----

func NilRead(k string) (int, bool, int, int) {
	var m map[string]int
	v, ok := m[k]
	n := 0
	for range m {
		n++
	}
	delete(m, k)
	clear(m)
	return v, ok, len(m), n
}

func NilWrite(m map[string]int, k string, v int) { m[k] = v }
func NilIncr(m map[string]int, k string)         { m[k]++ }
func NilWriteInt(m map[int]int, k int)           { m[k] = 1 }

// ---- range ----

func Keys(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func Sums(m map[int]int) (ksum, vsum, n int) {
	for k, v := range m {
		ksum += k
		vsum += v
	}
	for range m {
		n++
	}
	return
}

// DeleteAllInRange deletes each entry as it is visited.
func DeleteAllInRange(m map[int]int) (visited int) {
	for k := range m {
		delete(m, k)
		visited++
	}
	return
}

// DeleteOthersInRange deletes k^1 when visiting k: a deleted entry must not be produced later, so
// exactly one entry of each pair {2i, 2i+1} is visited.
func DeleteOthersInRange(m map[int]int) (visited []int) {
	for k := range m {
		delete(m, k^1)
		visited = append(visited, k)
	}
	return
}

// InsertInRange adds entries during iteration; each new one may or may not be produced.
func InsertInRange(m map[int]int, limit int) (visited int) {
	for k := range m {
		if k+1000 < limit {
			m[k+1000] = k
		}
		visited++
	}
	return
}

// ---- literals ----

func Literals(a, b string, x int) (map[string]int, map[Pt]string, map[string]Pt, map[string]*Pt, map[int][]int, map[string]map[string]int) {
	return map[string]int{a: x, b: x + 1, "const": 3},
		map[Pt]string{{1, 2}: a, {X: x}: b},
		map[string]Pt{a: {x, x}, b: {Y: 5}},
		map[string]*Pt{a: {x, x}, b: nil},
		map[int][]int{x: {1, 2, 3}, x + 1: nil, x + 2: {}},
		map[string]map[string]int{a: {b: x}, b: {}}
}

var Global = map[string][]int{"primes": {2, 3, 5, 7}, "evens": {2, 4}, "none": nil}

func FromGlobal(k string) (int, bool) {
	xs, ok := Global[k]
	t := 0
	for _, x := range xs {
		t += x
	}
	return t, ok
}

// ---- reference semantics ----

func Alias(n int) (int, int) {
	a := map[int]int{}
	b := a
	for i := 0; i < n; i++ {
		b[i] = i
	}
	fill(a, n, 2*n)
	return len(a), len(b)
}

func fill(m map[int]int, lo, hi int) {
	for i := lo; i < hi; i++ {
		m[i] = i
	}
}

// ---- large maps ----

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Big builds a map of n entries, checks it, deletes every other entry and checks again.
func Big(n int) (m map[int]int, sumAll, lenHalf, sumHalf, missing int) {
	m = make(map[int]int)
	for i := 0; i < n; i++ {
		m[i*7] = i
	}
	for i := 0; i < n; i++ {
		v, ok := m[i*7]
		if !ok || v != i {
			missing++
		}
		sumAll += v
	}
	for i := 0; i < n; i += 2 {
		delete(m, i*7)
	}
	lenHalf = len(m)
	for _, v := range m {
		sumHalf += v
	}
	for i := 0; i < n; i++ {
		if _, ok := m[i*7]; ok != (i%2 == 1) {
			missing++
		}
	}
	return
}

func BigStrings(n int) (m map[string]int, bad int) {
	m = map[string]int{}
	for i := 0; i < n; i++ {
		m["key-"+itoa(i)] = i
	}
	for i := n - 1; i >= 0; i-- {
		if m["key-"+itoa(i)] != i {
			bad++
		}
	}
	if _, ok := m["key-"+itoa(n)]; ok {
		bad++
	}
	return
}

// GrowWhileHolding keeps pointers into values (through pointer values) while the map grows.
func GrowWhileHolding(n int) (sum int) {
	m := map[int]*Pt{}
	var held []*Pt
	for i := 0; i < n; i++ {
		p := &Pt{i, -i}
		m[i] = p
		if i%64 == 0 {
			held = append(held, p)
		}
	}
	for i, p := range held {
		if m[i*64] != p {
			return -1
		}
		sum += p.X - p.Y
	}
	return
}

// WordFreq splits on spaces and counts.
func WordFreq(text string) map[string]int {
	m := map[string]int{}
	start := -1
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == ' ' {
			if start >= 0 {
				m[text[start:i]]++
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	return m
}

// BytesKey looks a []byte up by converting it to a string in the index expression.
func BytesKey(m map[string]int, b []byte) (int, bool) {
	v, ok := m[string(b)]
	return v, ok
}
