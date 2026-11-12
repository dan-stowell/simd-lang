// Package chk is the differential-check helper of the fclang tests.  It is never compiled by fc: test
// packages (stock-compiled) use it to call a function of the package under test and a reference function
// (normally the stock-compiled copy of the same source, see internal/mkref) with the same arguments, and to
// compare results, panics and the arguments' state afterwards, structurally (the two sides' named types are
// different types with the same structure).
package chk

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"unsafe"
)

var count atomic.Int64

// N is the number of comparisons made so far.
func N() int64 { return count.Load() }

// Main runs the tests and, with FCLANG_COUNT set, prints the number of comparisons.
func Main(m *testing.M) {
	rc := m.Run()
	if os.Getenv("FCLANG_COUNT") != "" {
		fmt.Printf("checks: %d\n", N())
	}
	os.Exit(rc)
}

// Try runs f and describes its panic: "" for none, else "panic <dynamic type>: <value>".
func Try(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = Describe(r)
		}
	}()
	f()
	return ""
}

// Describe formats a recovered panic value with its dynamic type; the reference package's "_test" suffix is dropped.
func Describe(r any) string {
	var s string
	if e, ok := r.(error); ok {
		s = fmt.Sprintf("panic %T: %s", r, e.Error())
	} else {
		s = fmt.Sprintf("panic %T: %v", r, r)
	}
	return strings.ReplaceAll(s, "_test.", ".")
}

// Opt adjusts Auto.
type Opt func(*cfg)

type cfg struct {
	pools map[int][]any
	limit int
}

// Arg gives parameter i its own pool of values (each is converted to the parameter's type).
func Arg(i int, vals ...any) Opt { return func(c *cfg) { c.pools[i] = vals } }

// Limit sets the number of argument tuples tried.
func Limit(n int) Opt { return func(c *cfg) { c.limit = n } }

var intPats = []uint64{0, 1, 2, 3, 5, 7, 8, 10, 63, 64, 65, 100, 127, 128, 255, 256, 1000, 32767, 32768, 65535, 65536,
	1<<31 - 1, 1 << 31, 1<<32 - 1, 1 << 32, 1<<63 - 1, 1 << 63, 1<<63 + 1, 1<<64 - 1, 1<<64 - 2, 1<<64 - 3, 1<<64 - 7, 1<<64 - 8, 1<<64 - 10,
	1<<64 - 128, 1<<64 - 129, 1<<64 - 32768, 1<<64 - 32769, 1<<64 - 1<<31, 1<<64 - 1<<31 - 1, 1<<64 - 1<<32,
	0x123456789abcdef0, 0xdeadbeefcafebabe, 0x5555555555555555, 0xaaaaaaaaaaaaaaaa}

var floatPats = []float64{0, math.Copysign(0, -1), 1, -1, 0.5, -0.5, 1.5, 2.5, -2.5, 3.14159, 0.1, 100, 127, 128, 255, 256, -128, -129.5,
	32767, 65535.75, 2147483647, 2147483648, -2147483648, 4294967295, 4294967296, 16777217, 9007199254740993,
	9.223372036854775e18, 1e10, -1e10, 1e-7, 123456.789, math.MaxFloat32, math.SmallestNonzeroFloat32, 1e300, -1e300, 1e-300,
	math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1), math.NaN()}

var stringPats = []string{"", "a", "b", "ab", "abc", "abd", "ABC", "0", "-123", "hello, world", "héllo, 世界", "\xff\xfe", "a\x00b",
	"12345678901234567890", "\xe4\xb8", "x\xf0\x9f\x98\x80y", strings.Repeat("0123456789", 10), " \t\n"}

var sliceLens = []int{-1, 0, 1, 2, 3, 8, 17, 64}

type gen struct {
	rng    *rand.Rand
	bad    string
	onPath map[reflect.Type]int // pointer types being filled: a recursive type recurs twice at most
}

func isBasic(k reflect.Kind) bool { return k >= reflect.Bool && k <= reflect.Complex128 }

func setter(v reflect.Value) reflect.Value {
	if v.CanSet() || !v.CanAddr() {
		return v
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}

func (g *gen) fill(v reflect.Value, depth int) {
	t := v.Type()
	switch t.Kind() {
	case reflect.Bool:
		v.SetBool(g.rng.IntN(2) == 1)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(intPats[g.rng.IntN(len(intPats))]))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.SetUint(intPats[g.rng.IntN(len(intPats))])
	case reflect.Float32, reflect.Float64:
		v.SetFloat(floatPats[g.rng.IntN(len(floatPats))])
	case reflect.Complex64, reflect.Complex128:
		v.SetComplex(complex(floatPats[g.rng.IntN(len(floatPats))], floatPats[g.rng.IntN(len(floatPats))]))
	case reflect.String:
		v.SetString(stringPats[g.rng.IntN(len(stringPats))])
	case reflect.Slice:
		n := sliceLens[g.rng.IntN(len(sliceLens))]
		if depth > 0 { // slices inside other values: short
			n = g.rng.IntN(5) - 1
		}
		if n < 0 {
			return
		}
		s := reflect.MakeSlice(t, n, n+g.rng.IntN(3))
		for i := 0; i < n; i++ {
			g.fill(s.Index(i), depth+1)
		}
		v.Set(s)
	case reflect.Array:
		n := t.Len()
		for i := 0; i < n && i < 256; i++ {
			g.fill(v.Index(i), depth+1)
		}
		for i := 256; i < n; i *= 2 { // the rest repeats the first 256
			reflect.Copy(v.Slice(i, n), v.Slice(0, i))
		}
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).Name == "_" {
				continue
			}
			g.fill(setter(v.Field(i)), depth+1)
		}
	case reflect.Pointer:
		if depth > 6 || g.onPath[t] >= 2 {
			return
		}
		if g.onPath == nil {
			g.onPath = map[reflect.Type]int{}
		}
		g.onPath[t]++
		p := reflect.New(t.Elem())
		g.fill(p.Elem(), depth+1)
		g.onPath[t]--
		v.Set(p)
	case reflect.Map:
		m := reflect.MakeMap(t)
		for i := g.rng.IntN(6); i > 0; i-- {
			k := reflect.New(t.Key()).Elem()
			e := reflect.New(t.Elem()).Elem()
			g.fill(k, depth+1)
			g.fill(e, depth+1)
			m.SetMapIndex(k, e)
		}
		v.Set(m)
	case reflect.Interface:
		if t.NumMethod() != 0 {
			g.bad = "interface with methods"
			return
		}
		switch g.rng.IntN(6) {
		case 0:
		case 1:
			v.Set(reflect.ValueOf(int(intPats[g.rng.IntN(len(intPats))])))
		case 2:
			v.Set(reflect.ValueOf(stringPats[g.rng.IntN(len(stringPats))]))
		case 3:
			v.Set(reflect.ValueOf(floatPats[g.rng.IntN(len(floatPats))]))
		case 4:
			v.Set(reflect.ValueOf(uint8(intPats[g.rng.IntN(len(intPats))])))
		case 5:
			v.Set(reflect.ValueOf(g.rng.IntN(2) == 1))
		}
	default:
		g.bad = t.Kind().String()
	}
}

// basicPool is every pattern of a basic kind, converted to t, without duplicates; nil for other kinds.
func basicPool(t reflect.Type) []reflect.Value {
	var out []reflect.Value
	seen := map[any]bool{}
	add := func(v reflect.Value, key any) {
		if !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}
	switch t.Kind() {
	case reflect.Bool:
		for _, b := range []bool{false, true} {
			v := reflect.New(t).Elem()
			v.SetBool(b)
			add(v, b)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		for _, p := range intPats {
			v := reflect.New(t).Elem()
			v.SetInt(int64(p))
			add(v, v.Int())
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		for _, p := range intPats {
			v := reflect.New(t).Elem()
			v.SetUint(p)
			add(v, v.Uint())
		}
	case reflect.Float32, reflect.Float64:
		for _, p := range floatPats {
			v := reflect.New(t).Elem()
			v.SetFloat(p)
			add(v, math.Float64bits(v.Float()))
		}
	case reflect.String:
		for _, p := range stringPats {
			v := reflect.New(t).Elem()
			v.SetString(p)
			add(v, p)
		}
	}
	return out
}

// Clone copies v, deeply, into a new value of type to (a type of the same structure).
func Clone(v reflect.Value, to reflect.Type) reflect.Value {
	out := reflect.New(to).Elem()
	if v.IsValid() { // an addressable copy, so that unexported fields can be read out of it
		a := reflect.New(v.Type()).Elem()
		a.Set(v)
		v = a
	}
	clone(out, v, map[unsafe.Pointer]reflect.Value{})
	return out
}

func clone(dst, v reflect.Value, memo map[unsafe.Pointer]reflect.Value) {
	if !v.IsValid() {
		return
	}
	to := dst.Type()
	if v.Kind() == reflect.Interface && to.Kind() != reflect.Interface {
		v = v.Elem()
		if !v.IsValid() {
			return
		}
	}
	isNum := func(k reflect.Kind) bool { return k >= reflect.Int && k <= reflect.Complex128 }
	switch k := to.Kind(); {
	case isNum(k):
		if !isNum(v.Kind()) {
			panic(fmt.Sprintf("chk: cannot make a %v from a %v", to, v.Type()))
		}
		dst.Set(v.Convert(to))
	case k == reflect.Bool:
		dst.SetBool(v.Bool())
	case k == reflect.String:
		dst.SetString(v.String())
	case k == reflect.Slice:
		if v.IsNil() {
			return
		}
		s := reflect.MakeSlice(to, v.Len(), v.Cap())
		if to.Elem() == v.Type().Elem() && isBasic(to.Elem().Kind()) {
			reflect.Copy(s, v)
		} else {
			for i := 0; i < v.Len(); i++ {
				clone(s.Index(i), v.Index(i), memo)
			}
		}
		dst.Set(s)
	case k == reflect.Array:
		if to.Elem() == v.Type().Elem() && isBasic(to.Elem().Kind()) && v.CanAddr() {
			reflect.Copy(dst, v)
			return
		}
		for i := 0; i < v.Len(); i++ {
			clone(dst.Index(i), v.Index(i), memo)
		}
	case k == reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			clone(setter(dst.Field(i)), setter(v.Field(i)), memo)
		}
	case k == reflect.Pointer:
		if v.IsNil() {
			return
		}
		if p, ok := memo[v.UnsafePointer()]; ok && p.Type() == to {
			dst.Set(p)
			return
		}
		p := reflect.New(to.Elem())
		memo[v.UnsafePointer()] = p
		clone(p.Elem(), v.Elem(), memo)
		dst.Set(p)
	case k == reflect.Map:
		if v.IsNil() {
			return
		}
		m := reflect.MakeMap(to)
		for it := v.MapRange(); it.Next(); {
			key := reflect.New(to.Key()).Elem()
			e := reflect.New(to.Elem()).Elem()
			clone(key, it.Key(), memo)
			clone(e, it.Value(), memo)
			m.SetMapIndex(key, e)
		}
		dst.Set(m)
	case k == reflect.Interface:
		if v.Kind() == reflect.Interface {
			v = v.Elem()
		}
		if !v.IsValid() {
			return
		}
		// the dynamic value: copied at its own type (so only types that both sides share work here)
		c := reflect.New(v.Type()).Elem()
		clone(c, v, memo)
		dst.Set(c)
	default: // func, chan, unsafe.Pointer: shared, and only when the types agree
		dst.Set(v)
	}
}

type differ struct {
	seen map[[2]unsafe.Pointer]bool
}

// Diff compares two values structurally; "" when equal, else where they differ.
func Diff(a, b any) string {
	d := differ{seen: map[[2]unsafe.Pointer]bool{}}
	return d.diff(reflect.ValueOf(a), reflect.ValueOf(b), "")
}

func typeName(t reflect.Type) string { return strings.ReplaceAll(t.String(), "_test.", ".") }

func (d *differ) diff(a, b reflect.Value, path string) string {
	if !a.IsValid() || !b.IsValid() {
		if a.IsValid() != b.IsValid() {
			return fmt.Sprintf("%s: one is nil: %v / %v", path, a, b)
		}
		return ""
	}
	if a.Kind() != b.Kind() {
		return fmt.Sprintf("%s: kinds %v / %v", path, a.Kind(), b.Kind())
	}
	ne := func() string { return fmt.Sprintf("%s: %#v / %#v", path, show(a), show(b)) }
	switch a.Kind() {
	case reflect.Bool:
		if a.Bool() != b.Bool() {
			return ne()
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if a.Int() != b.Int() {
			return ne()
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if a.Uint() != b.Uint() {
			return ne()
		}
	case reflect.Float32, reflect.Float64:
		if !sameFloat(a.Float(), b.Float()) {
			return ne()
		}
	case reflect.Complex64, reflect.Complex128:
		x, y := a.Complex(), b.Complex()
		if !sameFloat(real(x), real(y)) || !sameFloat(imag(x), imag(y)) {
			return ne()
		}
	case reflect.String:
		if a.String() != b.String() {
			return ne()
		}
	case reflect.Slice:
		if a.IsNil() != b.IsNil() {
			return fmt.Sprintf("%s: nil slice / non-nil slice (%v / %v)", path, a.IsNil(), b.IsNil())
		}
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: len %d / %d", path, a.Len(), b.Len())
		}
		for i := 0; i < a.Len(); i++ {
			if s := d.diff(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i)); s != "" {
				return s
			}
		}
	case reflect.Array:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: array len %d / %d", path, a.Len(), b.Len())
		}
		if a.Type().Elem().Kind() == reflect.Uint8 && a.CanAddr() && b.CanAddr() && bytes.Equal(a.Bytes(), b.Bytes()) {
			return ""
		}
		for i := 0; i < a.Len(); i++ {
			if s := d.diff(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i)); s != "" {
				return s
			}
		}
	case reflect.Struct:
		if a.NumField() != b.NumField() {
			return fmt.Sprintf("%s: struct types %v / %v", path, a.Type(), b.Type())
		}
		for i := 0; i < a.NumField(); i++ {
			if a.Type().Field(i).Name == "_" {
				continue
			}
			if s := d.diff(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name); s != "" {
				return s
			}
		}
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Sprintf("%s: nil pointer / non-nil pointer (%v / %v)", path, a.IsNil(), b.IsNil())
			}
			return ""
		}
		k := [2]unsafe.Pointer{a.UnsafePointer(), b.UnsafePointer()}
		if d.seen[k] {
			return ""
		}
		d.seen[k] = true
		return d.diff(a.Elem(), b.Elem(), "(*"+path+")")
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			if a.IsNil() != b.IsNil() {
				return fmt.Sprintf("%s: nil interface / non-nil (%v / %v)", path, show(a), show(b))
			}
			return ""
		}
		if x, y := typeName(a.Elem().Type()), typeName(b.Elem().Type()); x != y {
			return fmt.Sprintf("%s: dynamic types %s / %s", path, x, y)
		}
		return d.diff(a.Elem(), b.Elem(), path+".(…)")
	case reflect.Map:
		if a.IsNil() != b.IsNil() {
			return fmt.Sprintf("%s: nil map / non-nil map (%v / %v)", path, a.IsNil(), b.IsNil())
		}
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: map len %d / %d", path, a.Len(), b.Len())
		}
		for it := a.MapRange(); it.Next(); {
			found := false
			last := ""
			for jt := b.MapRange(); jt.Next(); {
				if (&differ{seen: map[[2]unsafe.Pointer]bool{}}).diff(it.Key(), jt.Key(), "") == "" {
					last = d.diff(it.Value(), jt.Value(), fmt.Sprintf("%s[%v]", path, show(it.Key())))
					if last == "" {
						found = true
						break
					}
				}
			}
			if !found {
				if last != "" {
					return last
				}
				return fmt.Sprintf("%s: key %v only on one side", path, show(it.Key()))
			}
		}
	case reflect.Func, reflect.UnsafePointer:
		if a.IsNil() != b.IsNil() {
			return fmt.Sprintf("%s: nil / non-nil %v", path, a.Kind())
		}
	case reflect.Chan:
		if a.IsNil() != b.IsNil() || a.Len() != b.Len() || a.Cap() != b.Cap() {
			return fmt.Sprintf("%s: channels differ (nil %v/%v len %d/%d cap %d/%d)", path, a.IsNil(), b.IsNil(), a.Len(), b.Len(), a.Cap(), b.Cap())
		}
	}
	return ""
}

func sameFloat(x, y float64) bool {
	if x != x || y != y {
		return x != x && y != y
	}
	return math.Float64bits(x) == math.Float64bits(y)
}

// show renders a value for a message without tripping over unexported fields.
func show(v reflect.Value) string {
	if !v.IsValid() {
		return "nil"
	}
	s := fmt.Sprintf("%#v", fmtSafe(v))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func fmtSafe(v reflect.Value) any {
	if v.CanInterface() {
		return v.Interface()
	}
	switch v.Kind() {
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.String:
		return v.String()
	}
	return v.String()
}

func showArgs(args []reflect.Value) string {
	var sb strings.Builder
	for i, a := range args {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(show(a))
	}
	return sb.String()
}

func call(f reflect.Value, args []reflect.Value) (out []reflect.Value, pan string) {
	defer func() {
		if r := recover(); r != nil {
			pan = Describe(r)
		}
	}()
	if f.Type().IsVariadic() {
		return f.CallSlice(args), ""
	}
	return f.Call(args), ""
}

// one calls got and ref with copies of args (values of got's parameter types, or convertible to them).
func one(t *testing.T, name string, got, ref reflect.Value, args []reflect.Value) bool {
	t.Helper()
	count.Add(1)
	gt, rt := got.Type(), ref.Type()
	ga := make([]reflect.Value, len(args))
	ra := make([]reflect.Value, len(args))
	for i, a := range args {
		ga[i] = Clone(a, gt.In(i))
		ra[i] = Clone(a, rt.In(i))
	}
	gout, gpan := call(got, ga)
	rout, rpan := call(ref, ra)
	if gpan != rpan {
		t.Errorf("%s(%s): panic: got %q, want %q", name, showArgs(args), gpan, rpan)
		return false
	}
	d := differ{seen: map[[2]unsafe.Pointer]bool{}}
	for i := range rout {
		if gpan != "" {
			break
		}
		if s := d.diff(gout[i], rout[i], fmt.Sprintf("result %d", i)); s != "" {
			t.Errorf("%s(%s): got / want differ at %s", name, showArgs(args), s)
			return false
		}
	}
	for i := range ra {
		if s := d.diff(ga[i], ra[i], fmt.Sprintf("arg %d", i)); s != "" {
			t.Errorf("%s(%s): after the call, got / want differ at %s", name, showArgs(args), s)
			return false
		}
	}
	return true
}

func funcs(t *testing.T, name string, got, ref any) (g, r reflect.Value, ok bool) {
	t.Helper()
	g, r = reflect.ValueOf(got), reflect.ValueOf(ref)
	if g.Kind() != reflect.Func || r.Kind() != reflect.Func || g.Type().NumIn() != r.Type().NumIn() || g.Type().NumOut() != r.Type().NumOut() {
		t.Errorf("%s: the function and its reference have different shapes: %T / %T", name, got, ref)
		return g, r, false
	}
	return g, r, true
}

// Call compares got(args...) with ref(args...): results, panic, and the arguments afterwards.
func Call(t *testing.T, name string, got, ref any, args ...any) {
	t.Helper()
	g, r, ok := funcs(t, name, got, ref)
	if !ok {
		return
	}
	if len(args) != g.Type().NumIn() {
		t.Errorf("%s: %d arguments for %d parameters", name, len(args), g.Type().NumIn())
		return
	}
	vs := make([]reflect.Value, len(args))
	for i, a := range args {
		vs[i] = reflect.ValueOf(a)
	}
	one(t, name, g, r, vs)
}

// Do compares what two thunks return (or how they panic).
func Do(t *testing.T, name string, got, ref func() any) {
	t.Helper()
	count.Add(1)
	var g, r any
	gp := Try(func() { g = got() })
	rp := Try(func() { r = ref() })
	if gp != rp {
		t.Errorf("%s: panic: got %q, want %q", name, gp, rp)
		return
	}
	if s := Diff(g, r); s != "" {
		t.Errorf("%s: got / want differ at %s", name, s)
	}
}

// Want compares a value with an expected one.
func Want(t *testing.T, name string, got, want any) {
	t.Helper()
	count.Add(1)
	if s := Diff(got, want); s != "" {
		t.Errorf("%s: got / want differ at %s", name, s)
	}
}

// Supported reports whether Auto can make arguments for f by itself.
func Supported(f any) bool {
	t := reflect.TypeOf(f)
	g := gen{rng: rand.New(rand.NewPCG(1, 2))}
	for i := 0; i < t.NumIn(); i++ {
		for j := 0; j < 20; j++ {
			g.fill(reflect.New(t.In(i)).Elem(), 0)
		}
	}
	return g.bad == ""
}

// Auto compares got and ref over generated arguments: every combination of the pools when the parameters are
// of basic types (or have pools given with Arg) and the product is small, a pseudo-random sample otherwise.
func Auto(t *testing.T, name string, got, ref any, opts ...Opt) {
	t.Helper()
	g, r, ok := funcs(t, name, got, ref)
	if !ok {
		return
	}
	c := cfg{pools: map[int][]any{}, limit: 400}
	for _, o := range opts {
		o(&c)
	}
	gt := g.Type()
	n := gt.NumIn()
	pools := make([][]reflect.Value, n)
	product := 1
	for i := 0; i < n; i++ {
		if vs, ok := c.pools[i]; ok {
			for _, v := range vs {
				pools[i] = append(pools[i], Clone(reflect.ValueOf(v), gt.In(i)))
			}
		} else {
			pools[i] = basicPool(gt.In(i))
		}
		if product > 0 && len(pools[i]) > 0 && product*len(pools[i]) <= 2500 {
			product *= len(pools[i])
		} else {
			product = 0
		}
	}
	args := make([]reflect.Value, n)
	if product > 0 {
		idx := make([]int, n)
		for {
			for i := range args {
				args[i] = pools[i][idx[i]]
			}
			if !one(t, name, g, r, args) {
				return
			}
			i := n - 1
			for ; i >= 0; i-- {
				if idx[i]++; idx[i] < len(pools[i]) {
					break
				}
				idx[i] = 0
			}
			if i < 0 {
				return
			}
		}
	}
	h := fnv.New64a()
	h.Write([]byte(name))
	gn := gen{rng: rand.New(rand.NewPCG(h.Sum64(), 0x9e3779b97f4a7c15))}
	for k := 0; k < c.limit; k++ {
		for i := range args {
			if len(pools[i]) > 0 {
				args[i] = pools[i][gn.rng.IntN(len(pools[i]))]
			} else {
				args[i] = reflect.New(gt.In(i)).Elem()
				gn.fill(args[i], 0)
			}
		}
		if gn.bad != "" {
			t.Errorf("%s: cannot make arguments (%s): test it by hand", name, gn.bad)
			return
		}
		if !one(t, name, g, r, args) {
			return
		}
	}
}

// Fn is a row of the table internal/mkref writes: an exported function of the package and its reference copy.
type Fn struct {
	Name     string
	Got, Ref any
}

// All runs Auto over a table.  special gives options by function name; a nil entry skips the function
// (it is tested by hand).  A function Auto cannot make arguments for, and that is not in special, is an error.
func All(t *testing.T, fns []Fn, special map[string][]Opt) {
	for _, f := range fns {
		opts, ok := special[f.Name]
		if ok && opts == nil {
			continue
		}
		t.Run(f.Name, func(t *testing.T) { Auto(t, f.Name, f.Got, f.Ref, opts...) })
	}
	for name := range special {
		found := false
		for _, f := range fns {
			found = found || f.Name == name
		}
		if !found {
			t.Errorf("special[%q]: no such function", name)
		}
	}
}
