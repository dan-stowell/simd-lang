package generics

// Non-generic entry points: every instantiation below happens in this package.

func SumInts(xs []int) int             { return Sum(xs) }
func SumFloat32s(xs []float32) float32 { return Sum[float32](xs) }
func SumUint8s(xs []uint8) uint8       { return Sum(xs) }
func SumMyInts(xs []MyInt) MyInt       { return Sum(xs) }
func DotInt8(a, b []int8) int8         { return Dot(a, b) }
func DotFloat64(a, b []float64) float64 {
	return Dot(a, b)
}
func DotUint16(a, b []uint16) uint16 { return Dot[uint16](a, b) }

func SwapIntStr(a int, b string) (string, int)           { return Swap(a, b) }
func SwapPointAny(a Point, b any) (any, Point)           { return Swap(a, b) }
func SwapFloatPtr(a float64, b *Point) (*Point, float64) { return Swap[float64, *Point](a, b) }

func MapItoa(xs []int) []string { return Map(xs, Itoa) }
func MapLen(xs []string) []int  { return Map(xs, func(s string) int { return len(s) }) }
func MapPointX(xs []Point, add int) []int {
	return Map(xs, func(p Point) int { return p.X + add })
}
func MapToAny(xs []int) []any { return Map(xs, func(x int) any { return x }) }

func FilterMod(xs []int, m int) []int {
	return Filter(xs, func(x int) bool { return x%m == 0 })
}
func FilterLong(xs []string, n int) []string {
	return Filter(xs, func(s string) bool { return len(s) > n })
}

func ReduceJoin(xs []string, sep string) string {
	return Reduce(xs, "", func(acc string, s string) string {
		if acc == "" {
			return s
		}
		return acc + sep + s
	})
}
func ReduceSumLen(xs []string) int {
	return Reduce(xs, 0, func(n int, s string) int { return n + len(s) })
}
func ReducePoints(xs []Point) Point {
	return Reduce(xs, Point{}, func(a Point, p Point) Point { return Point{a.X + p.X, a.Y + p.Y} })
}

func IndexInt(xs []int, x int) int             { return Index(xs, x) }
func IndexStr(xs []string, x string) int       { return Index(xs, x) }
func IndexPoint(xs []Point, x Point) int       { return Index(xs, x) }
func IndexAny(xs []any, x any) int             { return Index(xs, x) }
func IndexPtr(xs []*Point, x *Point) int       { return Index(xs, x) }
func IndexFloat(xs []float64, x float64) int   { return Index(xs, x) }
func IndexArr(xs [][2]string, x [2]string) int { return Index(xs, x) }
func IndexStringer(xs []Stringer, x Stringer) int {
	return Index(xs, x)
}

func EqualAny(a, b any) bool                    { return Equal(a, b) }
func EqualStr(a, b string) bool                 { return Equal(a, b) }
func EqualPair(a, b Pair[string, float64]) bool { return Equal(a, b) }

func MaxInt(a, b int) int               { return Max(a, b) }
func MaxStr(a, b string) string         { return Max(a, b) }
func MaxFloat(a, b float64) float64     { return Max(a, b) }
func MaxMyStr(a, b MyStr) MyStr         { return Max(a, b) }
func MaxUint8(a, b uint8) uint8         { return Max(a, b) }
func Min3Int(a, b, c int) int           { return Min3(a, b, c) }
func Min3Float(a, b, c float64) float64 { return Min3(a, b, c) }
func Min3Str(a, b, c string) string     { return Min3(a, b, c) }
func ClampInt(x, lo, hi int) int        { return Clamp(x, lo, hi) }
func ClampStr(x, lo, hi string) string  { return Clamp(x, lo, hi) }
func LessInt8(a, b int8) bool           { return Less(a, b) }
func LessUint8(a, b uint8) bool         { return Less(a, b) }
func LessFloat32(a, b float32) bool     { return Less(a, b) }
func LessStr(a, b string) bool          { return Less(a, b) }

func Zeros() (int, string, Point, *Point, any, []int, float64, [3]bool) {
	return Zero[int](), Zero[string](), Zero[Point](), Zero[*Point](), Zero[any](), Zero[[]int](), Zero[float64](), Zero[[3]bool]()
}

func IsZeroInt(x int) bool       { return IsZero(x) }
func IsZeroStr(x string) bool    { return IsZero(x) }
func IsZeroPoint(x Point) bool   { return IsZero(x) }
func IsZeroPtr(x *Point) bool    { return IsZero(x) }
func IsZeroAny(x any) bool       { return IsZero(x) }
func IsZeroFloat(x float64) bool { return IsZero(x) }

func DoubleInt(x int) int       { return Double(x) }
func DoubleStr(x string) string { return Double(x) }
func DoubleMyStr(x MyStr) MyStr { return Double(x) }
func DoubleMyInt(x MyInt) MyInt { return Double(x) }

func JoinMyInts(xs []MyInt, sep string) string       { return Join(xs, sep) }
func JoinPoints(xs []Point, sep string) string       { return Join(xs, sep) }
func JoinStringers(xs []Stringer, sep string) string { return Join(xs, sep) }
func DescribeMyInt(x MyInt) string                   { return Describe(x) }

func IntToFloat(x int) float64           { return Convert[int, float64](x) }
func FloatToInt8(x float64) int8         { return Convert[float64, int8](x) }
func Uint64ToFloat32(x uint64) float32   { return Convert[uint64, float32](x) }
func Int64ToUint8(x int64) uint8         { return Convert[int64, uint8](x) }
func MyIntToInt16(x MyInt) int16         { return Convert[MyInt, int16](x) }
func Int8ToUint64(x int8) uint64         { return Convert[int8, uint64](x) }
func Float32ToFloat64(x float32) float64 { return Convert[float32, float64](x) }

func ShlInt8(x int8, n uint) int8       { return ShiftL(x, n) }
func ShlUint32(x uint32, n uint) uint32 { return ShiftL(x, n) }
func ShlInt64(x int64, n uint) int64    { return ShiftL(x, n) }
func ShrInt8(x int8, n uint) int8       { return ShiftR(x, n) }
func ShrUint64(x uint64, n uint) uint64 { return ShiftR(x, n) }
func ShrInt16(x int16, n uint) int16    { return ShiftR(x, n) }
func DivInt8(a, b int8) int8            { return Div(a, b) }
func DivInt64(a, b int64) int64         { return Div(a, b) }
func DivUint8(a, b uint8) uint8         { return Div(a, b) }
func ModInt32(a, b int32) int32         { return Mod(a, b) }
func ModUint64(a, b uint64) uint64      { return Mod(a, b) }
func NegInt8(x int8) int8               { return Neg(x) }
func NegFloat(x float64) float64        { return Neg(x) }
func NegUint16(x uint16) uint16         { return Neg(x) }

func KindAny(x any) string           { return Kind(x) }
func KindInt(x int) string           { return Kind(x) }
func KindStr(x string) string        { return Kind(x) }
func KindMyInt(x MyInt) string       { return Kind(x) }
func KindPoint(x Point) string       { return Kind(x) }
func KindPtr(x *Point) string        { return Kind(x) }
func KindSlice(x []int) string       { return Kind(x) }
func KindErr(x error) string         { return Kind(x) }
func KindFloat(x float64) string     { return Kind(x) }
func KindStringer(x Stringer) string { return Kind(x) }

func PtrInt(x int) int {
	p := Ptr(x)
	*p++
	return Deref(p) + x
}
func PtrPoint(p Point) Point {
	q := Ptr(p)
	q.Scale(3)
	return Deref(q)
}
func PtrPtr(p *Point) bool { return Deref(Ptr(p)) == p }
func PtrStr(s string) string {
	p := Ptr(s)
	*p += "!"
	return *p + s
}

func SliceOfInts(a, b, c int) []int    { return SliceOf(a, b, c) }
func SliceOfNone() int                 { return len(SliceOf[string]()) }
func SliceOfAny(a int, b string) []any { return SliceOf[any](a, b, nil) }
func SliceOfSpread(xs []Point) []Point { return SliceOf(xs...) }
func LenIDs(ids IDs) int               { return Len(ids) }
func ReverseIDs(ids IDs) IDs           { return Reverse(ids) }
func ReverseStrs(xs []string) []string { return Reverse(xs) }

func SortInts(xs []int) []int {
	out := append([]int(nil), xs...)
	Sort(out)
	return out
}
func SortStrs(xs []string) []string {
	out := append([]string(nil), xs...)
	Sort(out)
	return out
}
func SortFloat32s(xs []float32) []float32 {
	out := append([]float32(nil), xs...)
	Sort(out)
	return out
}
func SortPointsByY(xs []Point) []Point {
	out := append([]Point(nil), xs...)
	SortBy(out, func(a, b Point) bool { return a.Y < b.Y })
	return out
}
func SortedKeysStrInt(m map[string]int) []string { return SortedKeys(m) }
func SortedKeysIntPoint(m map[int]*Point) []int  { return SortedKeys(m) }

func TwiceDouble(x int) int { return ApplyTwice(Double[int], x) }
func TwiceStr(s string) string {
	f := Double[string]
	return ApplyTwice(f, Identity(s))
}
func FuncTable(x int) []int {
	fs := []func(int) int{Identity[int], Double[int], MakeAdder(x), Neg[int]}
	out := make([]int, len(fs))
	for i, f := range fs {
		out[i] = f(x + i)
	}
	return out
}
func AddN(k, x int) int          { return MakeAdder(k)(x) }
func AddNF(k, x float32) float32 { return MakeAdder(k)(x) }
func Accumulate(xs []int) []int  { return Map(xs, Accumulator[int]()) }
func AccumulateF(xs []float64) []float64 {
	acc := Accumulator[float64]()
	out := []float64{}
	for _, x := range xs {
		out = append(out, acc(x))
	}
	return out
}

func MemoFib(n int) (int, int) {
	var fib func(int) int
	var calls func() int
	fib, calls = Memo(func(k int) int {
		if k < 2 {
			return k
		}
		return fib(k-1) + fib(k-2)
	})
	return fib(n), calls()
}
func MemoLen(keys []string) (total, calls int) {
	f, c := Memo(func(s string) int { return len(s) })
	for _, k := range keys {
		total += f(k)
	}
	return total, c()
}

func ScalePoints(xs []Point, k int) []Point {
	out := append([]Point(nil), xs...)
	ScaleAll(out, k)
	return out
}
func ScaleVecs(xs []Vec, k int) []Vec {
	out := append([]Vec(nil), xs...)
	ScaleAll[Vec](out, k)
	return out
}

func AreaRects(xs []Rect) float64   { return TotalArea(xs) }
func AreaSqs(xs []Sq) float64       { return TotalArea(xs) }
func AreaShapes(xs []Shape) float64 { return TotalArea(xs) }

func PairIntStr(k int, v string, probe int) (int, string, bool) {
	p := MakePair(k, v)
	a, b := p.Get()
	return a, b, p.KeyIs(probe)
}
func PairSet(k string, v1, v2 Point) (Point, Point, string) {
	p := &Pair[string, Point]{Key: k, Val: v1}
	old := p.Set(v2)
	return old, p.Val, p.Key
}
func PairAnyKey(k, probe any) bool {
	return Pair[any, int]{k, 1}.KeyIs(probe)
}
func PairOfPairs(a, b int, s string) (int, int, string) {
	p := MakePair(MakePair(a, s), MakePair(s, b))
	return p.Key.Key, p.Val.Val, p.Key.Val + p.Val.Key
}

func ListInts(n int) []int {
	var l List[int]
	for i := 0; i < n; i++ {
		l.Push(i).Push(i * i)
	}
	l.PushFront(-1)
	return l.Reversed().Slice()
}
func ListLens(xs []string) []int {
	l := &List[string]{}
	for _, x := range xs {
		l.Push(x)
	}
	return ListMap(l, func(s string) int { return len(s) }).Slice()
}
func ListPoints(xs []Point) (int, Point) {
	var l List[*Point]
	for i := range xs {
		l.PushFront(&xs[i])
	}
	var sum Point
	l.Each(func(i int, p *Point) {
		sum.X += p.X * (i + 1)
		sum.Y += p.Y
	})
	return l.Len(), sum
}

func TreeSortInts(xs []int) ([]int, int, int) {
	var t Tree[int]
	dups := 0
	for _, x := range xs {
		if !t.Insert(x) {
			dups++
		}
	}
	return t.Sorted(), t.Height(), dups
}
func TreeSortStrs(xs []string) []string {
	t := new(Tree[string])
	for _, x := range xs {
		t.Insert(x)
	}
	return t.Sorted()
}
func TreeMinFloat(xs []float64) (float64, bool) {
	var t Tree[float64]
	for _, x := range xs {
		t.Insert(x)
	}
	return t.Min()
}
func TreeHasInt(xs []int, v int) bool {
	var t Tree[int]
	for _, x := range xs {
		t.Insert(x)
	}
	return t.Has(v)
}

func StackReverse(xs []string) []string {
	var s Stack[string]
	for _, x := range xs {
		s.Push(x)
	}
	var out []string
	for {
		v, ok := s.Pop()
		if !ok {
			break
		}
		out = append(out, v)
	}
	return out
}
func StackPopEmpty() (Point, bool) {
	var s Stack[Point]
	return s.Pop()
}

func SetCount(xs []int) int {
	s := Set[int]{}
	for _, x := range xs {
		s.Add(x)
	}
	return s.Len()
}
func SetHasStr(xs []string, probe string) (bool, int) {
	s := make(Set[string])
	added := 0
	for _, x := range xs {
		if s.Add(x) {
			added++
		}
	}
	return s.Has(probe), added
}
func SetPoints(xs []Point) int {
	s := Set[Point]{}
	for _, x := range xs {
		s.Add(x)
	}
	return len(s)
}

func NamedLabel(name string, xs []int) (string, []int) {
	n := &Named[int]{Name: name}
	for _, x := range xs {
		n.Push(x)
	}
	n.PushFront(len(xs))
	return n.Label(), n.Slice()
}

func NumCalcInt32(a, b, k int32) int32 {
	return Num[int32]{a}.AddScaled(Num[int32]{b}, k).V
}
func NumCalcFloat64(a, b, k float64) float64 {
	return Num[float64]{a}.AddScaled(Num[float64]{b}, k).V
}

func OptOr(v int, some bool, d int) int {
	if some {
		return Some(v).Or(d)
	}
	return None[int]().Or(d)
}
func OptOrStr(v string, some bool, d string) string {
	o := None[string]()
	if some {
		o = Some(v)
	}
	return o.Or(d)
}
