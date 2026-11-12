package globals

import "errors"

type Rec struct {
	Name string
	Val  int
	Next *Rec
	Tags []string
	Attr map[string]int
	F    func(int) int
	Any  any
}

func (r Rec) String() string { return r.Name }
func (r *Rec) Bump() int     { r.Val++; return r.Val }

type Stringer interface{ String() string }

type Key struct {
	A int
	B string
}

type Inner struct {
	X int8
	Y float64
	S string
}

type Outer struct {
	In  Inner
	P   *Inner
	Arr [2]Inner
	Sl  []Inner
	M   map[string]Inner
}

type Entry struct {
	Sym    string
	Num    uint8
	Mass   float32
	Day    Weekday
	Period int16
	Link   *Entry
}

// scalars
var (
	I8      int8    = -128
	U64     uint64  = 1<<64 - 1
	F32     float32 = 1.5
	F64             = 2.5e-3
	S               = "hello"
	B               = true
	R               = 'x'
	C               = complex(1, -2)
	ZeroI   int
	ZeroS   string
	ZeroP   *Rec
	ZeroRec Rec
	counter int
)

// arrays, slices, maps
var (
	Arr     = [5]int{1, 2, 3}
	ArrIdx  = [...]string{2: "two", 5: "five"}
	Sl      = []int{10, 20, 30}
	SlSl    = [][]string{{"a"}, {"b", "c"}, nil, {}}
	M       = map[string]int{"one": 1, "two": 2, "three": 3}
	MKey    = map[Key]*Rec{{1, "a"}: {Name: "x", Val: 1}, {2, "b"}: {Name: "y", Tags: []string{"t1", "t2"}}}
	MNested = map[string]map[int][]string{"m": {1: {"p", "q"}, 2: nil}}
	Sparse  = [2048]int32{0: 1, 1000: -5, 2047: 9}
	Squares [256]int32
	Big     [10000]Rec
	Bytes   = []byte("bytes\x00\xff")
	Runes   = []rune("añ世")
)

// pointers, some into other globals
var (
	P        = &Rec{Name: "p", Val: 1, Next: &Rec{Name: "q", Val: 2, Tags: []string{"leaf"}}}
	PCounter = &counter
	PElem    = &Arr[2]
	PEntry   = &Table[3]
	PField   = &Nested.Arr[1].Y
	SlOfArr  = Arr[1:4]
	Ring     = &Rec{Name: "r0", Next: &Rec{Name: "r1"}}
	PP       = &P
)

// functions and interfaces
var (
	Fn                  = double
	FnLit               = func(x int) int { return x*3 + offset }
	Fns                 = []func(int) int{double, FnLit, func(x int) int { return -x }, nil}
	FnMap               = map[string]func(int) int{"double": double, "lit": FnLit}
	Method              = P.Bump // bound method value
	MethodExpr          = (*Rec).Bump
	Iface      any      = 42
	IfaceS     any      = "str"
	IfaceP     any      = P
	IfaceRec   any      = Rec{Name: "boxed", Val: 7}
	Err        error    = errors.New("boom")
	Str        Stringer = Rec{Name: "stringer"}
	StrDay     Stringer = Wednesday
	Ifaces              = []any{1, "two", 3.0, nil, Key{4, "four"}, &counter, [2]bool{true, false}, Friday}
	offset              = 1000
)

// nested composite literals
var Nested = Outer{
	In:  Inner{-1, 1.5, "in"},
	P:   &Inner{X: 2, S: "ptr"},
	Arr: [2]Inner{{3, 3.5, "a0"}, {4, 4.5, "a1"}},
	Sl:  []Inner{{S: "s0"}, {X: 5}},
	M:   map[string]Inner{"k": {6, 6.5, "mk"}},
}

var RecWithAll = Rec{
	Name: "all",
	Val:  len(Table),
	Next: P,
	Tags: []string{"x", "y", "z"},
	Attr: map[string]int{"w": 3},
	F:    double,
	Any:  []Key{{1, "one"}},
}

// initializers computed from other globals and function calls
var (
	LenArr    = len(Arr)
	FromFunc  = compute(7)
	SumTable  = sumNums()
	Closure   = makeCounter(100)
	Sub       = S[1:4]
	Joined    = S + "/" + Hello
	MLen      = len(M)
	FromConst = float64(KB) * Pi64
	Q, Rm     = divmod(47, 5)
	FirstSym  = Table[0].Sym + Table[len(Table)-1].Sym
)

func double(x int) int  { return x * 2 }
func compute(n int) int { return n*n + LenArr }
func sumNums() (s int) {
	for i := range Table {
		s += int(Table[i].Num)
	}
	return
}
func makeCounter(start int) func() int {
	n := start
	return func() int { n++; return n }
}
func divmod(a, b int) (int, int) { return a / b, a % b }

var Aux = [3]Entry{{Sym: "aux0"}, {Sym: "aux1", Link: &Aux2}, {Sym: "aux2"}}
var Aux2 = Entry{Sym: "aux-two", Num: 2}

// A statically initialized table; some entries point into another global.
var Table = [...]Entry{
	{"H", 1, 1.008, Monday, 1, nil},
	{"He", 2, 4.0026, Tuesday, 1, &Aux[0]},
	{"Li", 3, 6.94, Wednesday, 2, &Aux[1]},
	{"Be", 4, 9.0122, Thursday, 2, nil},
	{"B", 5, 10.81, Friday, 2, nil},
	{"C", 6, 12.011, Saturday, 2, &Aux[2]},
	{"N", 7, 14.007, Sunday, 2, nil},
	{"O", 8, 15.999, Monday, 2, nil},
	{"F", 9, 18.998, Tuesday, 2, nil},
	{"Ne", 10, 20.180, Wednesday, 2, &Aux[1]},
	{"Na", 11, 22.990, Thursday, 3, &Aux[2]},
	{"Mg", 12, 24.305, Friday, 3, nil},
	{"Al", 13, 26.982, Saturday, 3, nil},
	{"Si", 14, 28.085, Sunday, 3, nil},
	{"P", 15, 30.974, Monday, 3, nil},
	{"S", 16, 32.06, Tuesday, 3, nil},
	{"Cl", 17, 35.45, Wednesday, 3, nil},
	{"Ar", 18, 39.948, Thursday, 3, &Aux[0]},
	{"K", 19, 39.098, Friday, 4, &Aux[1]},
	{"Ca", 20, 40.078, Saturday, 4, nil},
	{Sym: "Sc", Num: 21, Period: 4},
	{Sym: "Ti", Num: 22, Mass: 47.867},
	{Num: 23},
	{},
}

// ---- accessors (inputs are parameters so that nothing folds) ----

func Scalars() (int8, uint64, float32, float64, string, bool, rune, complex128) {
	return I8, U64, F32, F64, S, B, R, C
}
func ZerosOK() bool {
	return ZeroI == 0 && ZeroS == "" && ZeroP == nil && ZeroRec.Name == "" && ZeroRec.Next == nil &&
		ZeroRec.Tags == nil && ZeroRec.Attr == nil && ZeroRec.F == nil && ZeroRec.Any == nil
}
func ArrAt(i int) int                     { return Arr[i] }
func ArrIdxAt(i int) string               { return ArrIdx[i] }
func ArrIdxLen() int                      { return len(ArrIdx) }
func SlAt(i int) int                      { return Sl[i] }
func SlSlAt(i, j int) string              { return SlSl[i][j] }
func SlSlNil(i int) bool                  { return SlSl[i] == nil }
func MAt(k string) (int, bool)            { v, ok := M[k]; return v, ok }
func MKeyAt(a int, b string) *Rec         { return MKey[Key{a, b}] }
func MNestedAt(s string, i, j int) string { return MNested[s][i][j] }
func SparseAt(i int) int32                { return Sparse[i] }
func SparseSum() (s int64) {
	for _, v := range Sparse {
		s += int64(v)
	}
	return
}
func SquareAt(i int) int32 { return Squares[i] }
func BigAt(i int) *Rec     { return &Big[i] }
func BytesAt(i int) byte   { return Bytes[i] }
func RuneAt(i int) rune    { return Runes[i] }
func TableAt(i int) Entry  { return Table[i] }
func TableLen() int        { return len(Table) }
func LinkSym(i int) string {
	if l := Table[i].Link; l != nil {
		return l.Sym
	}
	return ""
}
func CallFn(i, x int) int           { return Fns[i](x) }
func CallFnMap(k string, x int) int { return FnMap[k](x) }
func CallFnVar(x int) int           { return Fn(x) + FnLit(x) }
func FnIsNil(i int) bool            { return Fns[i] == nil }
func IfaceAt(i int) any             { return Ifaces[i] }
func ErrText() string               { return Err.Error() }
func StrText() string               { return Str.String() + "," + StrDay.String() }
func RingWalk(n int) string {
	s := ""
	for r := Ring; n > 0; n, r = n-1, r.Next {
		s += r.Name
	}
	return s
}

// ---- mutation: globals alias each other as declared ----

func Count() int          { counter++; return counter }
func CountViaPtr() int    { *PCounter += 10; return counter }
func SetArr(i, v int)     { Arr[i] = v }
func ElemViaPtr() int     { return *PElem }
func SlOfArrAt(i int) int { return SlOfArr[i] }
func SetNestedY(v float64) float64 {
	old := *PField
	Nested.Arr[1].Y = v
	return old + *PField
}
func BumpP() (int, int, int) { return Method(), MethodExpr(P), (*PP).Val }
func NextClosure() int       { return Closure() }
func SetOffset(v int) int    { offset = v; return FnLit(1) }
func SwapGlobals() (int, string) {
	I8, F32 = int8(F32), float32(I8)
	ZeroI, ZeroS = len(S), S
	defer func() { I8, F32, ZeroI, ZeroS = -128, 1.5, 0, "" }()
	return int(I8)*1000 + int(F32) + ZeroI, ZeroS
}
func EntryViaPtr(mass float32) (float32, float32) {
	old := Table[3].Mass
	PEntry.Mass = mass
	now := Table[3].Mass
	PEntry.Mass = old
	return old, now
}
