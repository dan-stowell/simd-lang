// Package ifaces exercises interface values: conversions to any and to non-empty interfaces, type
// assertions and switches, interface-to-interface conversions, equality, nil-ness and error values.
// Imports: errors (errors.New, errors.Is, errors.As).
package ifaces

import "errors"

// ---- types ----

type Shape interface {
	Area() int
	Name() string
}

type Namer interface{ Name() string }

type Scaler interface{ Scale(k int) }

type ScalableShape interface {
	Shape
	Scaler
}

type Rect struct{ W, H int }

func (r Rect) Area() int    { return r.W * r.H }
func (r Rect) Name() string { return "rect" }

type Sq int

func (s Sq) Area() int    { return int(s) * int(s) }
func (s Sq) Name() string { return "sq" }

type Circle struct {
	R     int
	Label string
}

func (c *Circle) Area() int    { return 3 * c.R * c.R }
func (c *Circle) Name() string { return "circle:" + c.Label }
func (c *Circle) Scale(k int)  { c.R *= k }

type Empty struct{}

func (Empty) Area() int    { return 0 }
func (Empty) Name() string { return "empty" }

type Big struct {
	A, B, C, D, E int
	S             string
}

func (b Big) Area() int    { return b.A + b.B + b.C + b.D + b.E }
func (b Big) Name() string { return b.S }

type FuncShape func() int

func (f FuncShape) Area() int    { return f() }
func (f FuncShape) Name() string { return "func" }

type MapShape map[string]int

func (m MapShape) Area() int    { return m["w"] * m["h"] }
func (m MapShape) Name() string { return "map" }

type S struct {
	A int
	B string
}

type MyInt int
type MyStr string
type Unc struct {
	N  int
	Xs []int
}

// ---- conversion to any ----

func Int(x int) any               { return x }
func Int8(x int8) any             { return x }
func Int16(x int16) any           { return x }
func Int32(x int32) any           { return x }
func Int64(x int64) any           { return x }
func Uint(x uint) any             { return x }
func Uint8(x uint8) any           { return x }
func Uint16(x uint16) any         { return x }
func Uint32(x uint32) any         { return x }
func Uint64(x uint64) any         { return x }
func Uintptr(x uintptr) any       { return x }
func Float32(x float32) any       { return x }
func Float64(x float64) any       { return x }
func Complex128(x complex128) any { return x }
func Bool(x bool) any             { return x }
func String(x string) any         { return x }
func Slice(x []int) any           { return x }
func Array(x [3]int) any          { return x }
func Struct(x S) any              { return x }
func Ptr(x *S) any                { return x }
func Func(x func() int) any       { return x }
func Map(x map[string]int) any    { return x }
func Chan(x chan int) any         { return x }
func ZeroStruct() any             { return struct{}{} }
func ZeroArray() any              { return [0]int{} }
func Named(x int) any             { return MyInt(x) }
func NamedStr(x string) any       { return MyStr(x) }
func Nested(x any) any            { return x }
func ErrAny(e error) any          { return e }
func Rune(x rune) any             { return x }

// Consts: untyped constants take their default types.
func Consts() []any {
	return []any{1, 'a', 2.5, "s", true, 1 << 40, nil, 2i, uint8(200), MyInt(3)}
}

// CopyOnConvert: the interface holds a copy of the value.
func CopyOnConvert(s S) (any, S) {
	v := any(s)
	s.A++
	s.B += "!"
	return v, s
}

// ---- conversion to non-empty interfaces ----

func MakeShape(kind, a, b int) Shape {
	switch kind {
	case 0:
		return Rect{a, b}
	case 1:
		return Sq(a)
	case 2:
		return &Circle{R: a, Label: "c"}
	case 3:
		return Empty{}
	case 4:
		return Big{a, b, a, b, a, "big"}
	case 5:
		return FuncShape(func() int { return a - b })
	case 6:
		return MapShape{"w": a, "h": b}
	}
	return nil
}

func AreaOf(s Shape) int    { return s.Area() }
func NameOf(n Namer) string { return n.Name() }

func SumAreas(ss []Shape) (total int, names string) {
	for _, s := range ss {
		if s == nil {
			names += "<nil>,"
			continue
		}
		total += s.Area()
		names += s.Name() + ","
	}
	return
}

// ---- type assertions ----

func AsInt(v any) int                   { return v.(int) }
func AsIntOk(v any) (int, bool)         { x, ok := v.(int); return x, ok }
func AsString(v any) string             { return v.(string) }
func AsStringOk(v any) (string, bool)   { x, ok := v.(string); return x, ok }
func AsS(v any) S                       { return v.(S) }
func AsPtrOk(v any) (*S, bool)          { x, ok := v.(*S); return x, ok }
func AsSliceOk(v any) ([]int, bool)     { x, ok := v.([]int); return x, ok }
func AsFloatOk(v any) (float64, bool)   { x, ok := v.(float64); return x, ok }
func AsShape(v any) Shape               { return v.(Shape) }
func AsShapeOk(v any) (Shape, bool)     { x, ok := v.(Shape); return x, ok }
func AsRect(s Shape) Rect               { return s.(Rect) }
func AsRectOk(s Shape) (Rect, bool)     { x, ok := s.(Rect); return x, ok }
func AsCircle(s Shape) *Circle          { return s.(*Circle) }
func AsScaler(s Shape) Scaler           { return s.(Scaler) }
func AsScalerOk(s Shape) (Scaler, bool) { x, ok := s.(Scaler); return x, ok }
func AsError(v any) error               { return v.(error) }
func AsMyInt(v any) MyInt               { return v.(MyInt) }
func AsAny(s Shape) any                 { return s.(any) }

// ---- interface to interface ----

func ShapeToNamer(s Shape) Namer { return s }
func ShapeToAny(s Shape) any     { return s }
func NamerToShape(n Namer) Shape { return n.(Shape) }
func ToScalable(s Shape) ScalableShape {
	if ss, ok := s.(ScalableShape); ok {
		return ss
	}
	return nil
}

func ScaleAndArea(s ScalableShape, k int) int {
	var sc Scaler = s
	sc.Scale(k)
	var sh Shape = s
	return sh.Area()
}

// ---- type switches ----

func Classify(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case int:
		return "int"
	case int8, int16:
		return "int8/16"
	case int32:
		return "int32"
	case int64:
		return "int64"
	case uint, uint8, uint16, uint32, uint64, uintptr:
		return "unsigned"
	case float32, float64:
		return "float"
	case complex64, complex128:
		return "complex"
	case bool:
		return "bool"
	case string:
		return "string"
	case []int:
		return "[]int"
	case []string:
		return "[]string"
	case [3]int:
		return "[3]int"
	case map[string]int:
		return "map"
	case chan int:
		return "chan"
	case func() int:
		return "func"
	case *S:
		return "*S"
	case S:
		return "S"
	case struct{}:
		return "struct{}"
	case MyInt:
		return "MyInt"
	case error:
		return "error"
	case Shape:
		return "Shape"
	case *int:
		return "*int"
	}
	return "other"
}

// Describe uses the variable bound by the switch.
func Describe(v any) string {
	switch x := v.(type) {
	case int:
		return "int:" + itoa(x+1)
	case string:
		return "string:" + itoa(len(x)) + ":" + x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case []int:
		return "slice:" + itoa(len(x))
	case S:
		return "S:" + itoa(x.A) + x.B
	case *S:
		if x == nil {
			return "*S:nil"
		}
		return "*S:" + itoa(x.A) + x.B
	case int8, uint8:
		// x is still an interface here
		if _, ok := x.(int8); ok {
			return "int8"
		}
		return "uint8"
	case ScalableShape:
		x.Scale(2)
		return "scalable:" + x.Name() + ":" + itoa(x.Area())
	case Shape:
		return "shape:" + x.Name() + ":" + itoa(x.Area())
	case error:
		return "error:" + x.Error()
	case nil:
		if x == nil {
			return "nil"
		}
		return "nil?"
	default:
		if x != nil {
			return "default"
		}
		return "default?"
	}
}

// DefaultFirst: the default clause may come first; a matching later case still wins.
func DefaultFirst(s Shape) string {
	switch t := s.(type) {
	default:
		return "default"
	case Rect:
		return "Rect" + itoa(t.W)
	case *Circle, Sq:
		return "CircleOrSq:" + t.Name()
	}
}

// SwitchNoMatch has no default: nothing happens when no case matches.
func SwitchNoMatch(v any) (r int) {
	r = -1
	switch x := v.(type) {
	case int:
		r = x
	case string:
		r = len(x)
	}
	return
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ---- equality ----

func EqAny(a, b any) bool            { return a == b }
func NeAny(a, b any) bool            { return a != b }
func EqShape(a, b Shape) bool        { return a == b }
func EqShapeAny(a Shape, b any) bool { return a == b }
func EqInt(v any, x int) bool        { return v == x }
func EqStr(v any, s string) bool     { return v == s }
func EqRect(s Shape, r Rect) bool    { return s == r }
func EqErr(a, b error) bool          { return a == b }
func IsNil(v any) bool               { return v == nil }
func IsNilShape(s Shape) bool        { return s == nil }
func IsNilErr(e error) bool          { return e == nil }

// MakeUnc returns an interface whose dynamic type is not comparable.
func MakeUnc(n int) any { return Unc{n, nil} }

type KeyPair struct {
	K any
	V Shape
}

// EqPair compares structs with interface fields.
func EqPair(a, b KeyPair) bool { return a == b }

// EqArr compares arrays of interfaces.
func EqArr(a, b [3]any) bool { return a == b }

// ---- nil pointers in interfaces ----

func NilCircle() Shape {
	var c *Circle
	return c
}

func NilShape() Shape { return nil }

// MaybeErr shows the classic mistake: a nil *PathErr in an error is not a nil error.
func MaybeErr(fail, typed bool) error {
	var p *PathErr
	if fail {
		p = &PathErr{"open", "/x", ErrNotFound}
	}
	if typed {
		return p
	}
	if p == nil {
		return nil
	}
	return p
}

// ---- errors ----

var ErrNotFound = errors.New("not found")
var ErrPerm = errors.New("permission denied")

type PathErr struct {
	Op, Path string
	Err      error
}

func (e *PathErr) Error() string {
	if e == nil {
		return "<nil PathErr>"
	}
	return e.Op + " " + e.Path + ": " + e.Err.Error()
}
func (e *PathErr) Unwrap() error { return e.Err }

type Code int

func (c Code) Error() string { return "code " + itoa(int(c)) }

// Is makes Code(0) match any Code.
func (c Code) Is(target error) bool {
	t, ok := target.(Code)
	return ok && (t == 0 || t == c)
}

func Find(key string) error {
	switch key {
	case "":
		return nil
	case "missing":
		return ErrNotFound
	case "secret":
		return &PathErr{"open", key, ErrPerm}
	case "deep":
		return &PathErr{"stat", key, &PathErr{"open", "inner", ErrNotFound}}
	case "code":
		return Code(42)
	case "wrapped-code":
		return &PathErr{"exec", key, Code(7)}
	}
	return errors.New("unknown key " + key)
}

func IsNotFound(err error) bool    { return errors.Is(err, ErrNotFound) }
func IsPerm(err error) bool        { return errors.Is(err, ErrPerm) }
func IsCode(err error, c int) bool { return errors.Is(err, Code(c)) }

func PathOf(err error) (string, bool) {
	var pe *PathErr
	if errors.As(err, &pe) {
		return pe.Op + "@" + pe.Path, true
	}
	return "", false
}

func CodeOf(err error) int {
	var c Code
	if errors.As(err, &c) {
		return int(c)
	}
	return -1
}

func ErrText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func Join(a, b error) error { return errors.Join(a, b) }

// ---- method values and calls through interfaces of every receiver shape ----

func AreaFunc(s Shape) func() int { return s.Area }

func CallAll(a, b int) []int {
	var out []int
	for kind := 0; kind <= 6; kind++ {
		s := MakeShape(kind, a, b)
		f := s.Area
		out = append(out, s.Area(), f(), len(s.Name()))
	}
	return out
}
