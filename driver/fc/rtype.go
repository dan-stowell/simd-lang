// rtype.go: type descriptors and interface tables: the entry points the code generator calls, and the names
// of types as the linker knows them. A port of cmd/compile/internal/reflectdata and of the type formatter in
// cmd/compile/internal/types/fmt.go to go/types; what is written must be, byte for byte and relocation for
// relocation, what the stock compiler writes for the same type (tools/fc/typediff.sh checks that).
//
//	rtype.go        the API, link names (LinkString, NameString), local-type numbering, method symbols
//	rtype_write.go  descriptors, uncommon types, method tables, name data, itabs
//	rtype_alg.go    sizes of pointer data, GC masks, equality and hash algorithms, map groups
//	rtype_diff.go   FC_TYPEDIFF: comparison with the stock compiler's object
package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"sync"

	"cmd/internal/obj"
)

// rtypeState is the part of the compiler that belongs to the descriptors.
type rtypeState struct {
	tsFast      sync.Map // types.Type -> *obj.LSym: descriptors already asked for by that type object
	rtMu        sync.Mutex
	rtDone      map[*obj.LSym]bool           // descriptors considered (written, or left to another object)
	rtMeth      map[string][]methodSig       // method tables, by type symbol name
	rtNoalg     map[types.Type]bool          // synthetic types with no equality and no hash (map groups, closures)
	rtGroup     map[*types.Struct]*types.Map // the map a group struct belongs to
	rtGroups    map[string]*types.Struct     // group struct by the map's link name
	rtVargen    map[*types.Package]map[token.Pos]int
	rtWrap      map[*obj.LSym]bool
	rtAlg       map[string]bool
	rtPtrTo     map[string]bool          // link names X such that the package mentions *X (stock: Type.IsPtrElem)
	rtPtrToInst map[*types.TypeName]bool // generic types G such that generic code mentions **G[...]
	rtPtrSet    bool
	rtNames     int // counter of package-qualified name data (stock: dnameCount)

	// What the code generator must write. The descriptors refer to these symbols; their bodies are code.
	needEq      []algReq     // type:.eq.<sig>: func(p, q unsafe.Pointer) bool, ABIInternal, DUPOK
	needHash    []algReq     // type:.hash.<sig>: func(p unsafe.Pointer, h uintptr) uintptr, ABIInternal, DUPOK
	needWrapper []wrapperReq // method wrappers and methods of instantiated types
}

// algReq asks for an equality or hash function. Since Go 1.27 these belong to an equality signature, not to
// a type: types with the same signature share the function (see eqSignature in rtype_alg.go for the grammar).
type algReq struct {
	sig string
	sym *obj.LSym
	typ types.Type // one type with this signature
}

// wrapperReq asks for the body of a method symbol a method table or an itab refers to and that is not a
// declared method of this package: recv.method reached through embedded fields, a pointer-receiver entry for
// a value method, or a method of an instantiated generic type.
type wrapperReq struct {
	sym      *obj.LSym
	recv     types.Type       // the wrapper's receiver type: T, *T, struct{...} or *struct{...}
	method   *types.Func      // the method called (of an instantiated type: the instance's method object)
	sel      *types.Selection // recv.method: Index() is the path through embedded fields, Indirect() etc.
	declPtr  bool             // the declared method has a pointer receiver
	instance bool             // the method itself, of an instantiated type (no embedding, same receiver)
	dupok    bool             // as the stock compiler: DUPOK unless recv's base type is declared in this package
}

func (c *compiler) rtInit() {
	if c.rtDone == nil {
		c.rtDone = map[*obj.LSym]bool{}
		c.rtMeth = map[string][]methodSig{}
		c.rtNoalg = map[types.Type]bool{}
		c.rtGroup = map[*types.Struct]*types.Map{}
		c.rtGroups = map[string]*types.Struct{}
		c.rtVargen = map[*types.Package]map[token.Pos]int{}
		c.rtWrap = map[*obj.LSym]bool{}
		c.rtAlg = map[string]bool{}
	}
}

// typeLinkName is the name of t as the linker knows it (stock: Type.LinkString); t's descriptor is "type:"
// followed by it.
func (c *compiler) typeLinkName(t types.Type) string {
	l := &c.layout
	l.mu.Lock()
	name, ok := l.names[t]
	l.mu.Unlock()
	if ok {
		return name
	}
	var b strings.Builder
	c.tconv(&b, t, fmtID, false)
	name = b.String()
	l.mu.Lock()
	if l.names == nil {
		l.names = map[types.Type]string{}
	}
	l.names[t] = name
	l.mu.Unlock()
	return name
}

// typeNameString is t's name in the descriptor itself (stock: Type.NameString): package names, not paths.
func (c *compiler) typeNameString(t types.Type) string {
	var b strings.Builder
	c.tconv(&b, t, fmtIDName, false)
	return b.String()
}

// typeSymName is the descriptor's name after "type:" (stock: types.TypeSymName).
func (c *compiler) typeSymName(t types.Type) string {
	name := c.typeLinkName(t)
	if c.algKind(t) == algNoalg {
		name = "noalg." + name
	}
	return name
}

// typeSym is the symbol of t's type descriptor. The descriptor is defined in this object when the stock
// compiler would define it in the package's object.
func (c *compiler) typeSym(t types.Type) *obj.LSym {
	// asked for the same type object again (most requests): no name is built and no lock is taken
	if s, ok := c.tsFast.Load(t); ok {
		return s.(*obj.LSym)
	}
	c.rtMu.Lock()
	defer c.rtMu.Unlock()
	c.rtInit()
	t0 := t
	t = types.Unalias(t)
	s := c.writeType(t)
	if hasSym(t) {
		// stock WriteRuntimeTypes: a type with a name brings the pointer to it
		c.writeType(types.NewPointer(t))
	}
	c.tsFast.Store(t0, s)
	return s
}

// typeSymNoalg is the descriptor of a compiler-made type that has no equality (closure structs and the
// like): "type:noalg.<name>".
func (c *compiler) typeSymNoalg(t types.Type) *obj.LSym {
	c.rtMu.Lock()
	defer c.rtMu.Unlock()
	c.rtInit()
	c.rtNoalg[t] = true
	return c.writeType(t)
}

// itabSym is the symbol of the interface table for the concrete type t as the interface type iface.
func (c *compiler) itabSym(t, iface types.Type) *obj.LSym {
	c.rtMu.Lock()
	defer c.rtMu.Unlock()
	c.rtInit()
	return c.writeITab(types.Unalias(t), types.Unalias(iface))
}

// writeDeclaredTypes defines the descriptors the stock compiler writes whatever the code does: every type
// declared in the package (and the pointer to it), and in package runtime the basic types.
func (c *compiler) writeDeclaredTypes() {
	if c.path == "runtime" {
		c.writeBasicTypes()
	}
	for _, t := range c.declaredTypes() {
		c.typeSym(t)
	}
}

// eqFuncSym is the closure (a func(p, q unsafe.Pointer) bool value) that compares two values of the
// comparable type t: of a runtime function, or type:.eqfunc.<signature>. hashFuncSym likewise hashes.
func (c *compiler) eqFuncSym(t types.Type) *obj.LSym {
	c.rtMu.Lock()
	defer c.rtMu.Unlock()
	c.rtInit()
	return c.eqClosure(t)
}

func (c *compiler) hashFuncSym(t types.Type) *obj.LSym {
	c.rtMu.Lock()
	defer c.rtMu.Unlock()
	c.rtInit()
	return c.hashClosure(t)
}

// declaredTypes lists the package's defined types: package level by name, then local ones in source order.
// Generic types have no descriptor (their instances do), nor do types declared inside generic functions.
func (c *compiler) declaredTypes() []types.Type {
	var ts []types.Type
	scope := c.tpkg.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() || name == "_" {
			continue
		}
		if n, ok := tn.Type().(*types.Named); ok && n.TypeParams().Len() == 0 {
			ts = append(ts, n)
		}
	}
	var local []*types.TypeName
	for id, o := range c.info.Defs {
		tn, ok := o.(*types.TypeName)
		if !ok || tn.IsAlias() || id.Name == "_" || tn.Parent() == scope {
			continue
		}
		if n, ok := tn.Type().(*types.Named); ok && n.TypeParams().Len() == 0 && !mentionsTypeParamRT(n.Underlying(), nil) && !c.enclosedByGeneric(tn) {
			local = append(local, tn)
		}
	}
	sort.Slice(local, func(i, j int) bool { return local[i].Pos() < local[j].Pos() })
	for _, tn := range local {
		ts = append(ts, tn.Type())
	}
	return ts
}

// enclosedByGeneric: is the local type declared inside a generic function (one descriptor per instance)?
func (c *compiler) enclosedByGeneric(tn *types.TypeName) bool {
	for s := tn.Parent(); s != nil && s != c.tpkg.Scope(); s = s.Parent() {
		for _, n := range s.Names() {
			if o, ok := s.Lookup(n).(*types.TypeName); ok {
				if _, ok := o.Type().(*types.TypeParam); ok {
					return true
				}
			}
		}
	}
	return false
}

func (c *compiler) writeBasicTypes() {
	list := []types.Type{}
	for _, k := range []types.BasicKind{types.Int8, types.Uint8, types.Int16, types.Uint16, types.Int32, types.Uint32,
		types.Int64, types.Uint64, types.Int, types.Uint, types.Uintptr, types.Complex64, types.Complex128,
		types.Float32, types.Float64, types.Bool, types.String, types.UnsafePointer} {
		list = append(list, types.Typ[k])
	}
	errT := types.Universe.Lookup("error").Type()
	list = append(list, types.NewInterfaceType(nil, nil), errT)
	c.rtMu.Lock()
	defer c.rtMu.Unlock()
	c.rtInit()
	for _, t := range list {
		c.writeType(types.NewPointer(t))
		c.writeType(types.NewPointer(types.NewSlice(t)))
	}
	fn := types.NewSignatureType(nil, nil, nil, types.NewTuple(types.NewParam(0, nil, "", errT)),
		types.NewTuple(types.NewParam(0, nil, "", types.Typ[types.String])), false)
	c.writeType(types.NewPointer(fn))
}

// hasSym: does the stock compiler's type have a symbol (a name)? Defined types and the predeclared ones.
func hasSym(t types.Type) bool {
	switch types.Unalias(t).(type) {
	case *types.Named, *types.Basic:
		return true
	}
	return false
}

// ---- direct interfaces, not-in-heap types

// isDirectIface: a value of type t is stored in an interface's data word itself: it is one word and that
// word is a pointer (stock: types.IsDirectIface, size == ptrdata == 8).
func isDirectIface(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Pointer:
		return !notInHeap(u.Elem())
	case *types.Chan, *types.Map, *types.Signature:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	case *types.Array:
		return u.Len() == 1 && isDirectIface(u.Elem())
	case *types.Struct:
		// one pointer-shaped field; zero-size fields may come before it (after it they would add padding)
		n := u.NumFields()
		if n == 0 || !isDirectIface(u.Field(n-1).Type()) {
			return false
		}
		for i := 0; i < n-1; i++ {
			if !isZeroSize(u.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

func isZeroSize(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Array:
		return u.Len() == 0 || isZeroSize(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if !isZeroSize(u.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

// notInHeap: values of t are never in the garbage-collected heap, so a pointer to one is not a pointer to
// the collector (stock: Type.NotInHeap: internal/runtime/sys.nih and whatever contains one).
func notInHeap(t types.Type) bool {
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok {
		o := n.Obj()
		if o.Name() == "nih" && o.Pkg() != nil && o.Pkg().Path() == "internal/runtime/sys" {
			return true
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Array:
		return notInHeap(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if notInHeap(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

// ---- names

type fmtMode uint8

const (
	fmtID     fmtMode = iota // stock fmtTypeID: package paths; unique per type
	fmtIDName                // stock fmtTypeIDName: package names, no numbering of local types
)

func mentionsTypeParamRT(t types.Type, seen map[types.Type]bool) bool {
	if seen == nil {
		seen = map[types.Type]bool{}
	}
	t = types.Unalias(t)
	if seen[t] {
		return false
	}
	seen[t] = true
	switch u := t.(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for i := 0; i < u.TypeArgs().Len(); i++ {
			if mentionsTypeParamRT(u.TypeArgs().At(i), seen) {
				return true
			}
		}
		return u.TypeParams().Len() > 0 && u.TypeArgs().Len() == 0
	case *types.Pointer:
		return mentionsTypeParamRT(u.Elem(), seen)
	case *types.Slice:
		return mentionsTypeParamRT(u.Elem(), seen)
	case *types.Array:
		return mentionsTypeParamRT(u.Elem(), seen)
	case *types.Chan:
		return mentionsTypeParamRT(u.Elem(), seen)
	case *types.Map:
		return mentionsTypeParamRT(u.Key(), seen) || mentionsTypeParamRT(u.Elem(), seen)
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if mentionsTypeParamRT(u.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Tuple:
		for i := 0; i < u.Len(); i++ {
			if mentionsTypeParamRT(u.At(i).Type(), seen) {
				return true
			}
		}
	case *types.Signature:
		return mentionsTypeParamRT(u.Params(), seen) || mentionsTypeParamRT(u.Results(), seen)
	case *types.Interface:
		for i := 0; i < u.NumMethods(); i++ {
			if mentionsTypeParamRT(u.Method(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

// basicName is the name of a basic type in descriptors: byte is uint8, rune is int32.
func basicName(u *types.Basic) string {
	if u.Info()&types.IsUntyped != 0 || u.Kind() == types.Invalid {
		giveUp("type descriptor for %v", u)
	}
	if u.Kind() == types.UnsafePointer {
		return "unsafe.Pointer"
	}
	return types.Typ[u.Kind()].Name()
}

// qual is a package as it qualifies a name.
func (c *compiler) qual(p *types.Package, mode fmtMode) string {
	if p == nil {
		return ""
	}
	if mode == fmtIDName {
		return p.Name()
	}
	return c.pkgPrefix(p)
}

// namedSymName is the stock compiler's symbol name of a defined type, without the package: the name, the
// type arguments (always as link names), and for a type declared in a function "·N".
func (c *compiler) namedSymName(u *types.Named, mode fmtMode) string {
	o := u.Obj()
	name := o.Name()
	if u.TypeParams().Len() > 0 && u.TypeArgs().Len() == 0 {
		giveUp("type descriptor for the generic type %v", u)
	}
	if n := u.TypeArgs().Len(); n > 0 {
		var b strings.Builder
		b.WriteString(name)
		b.WriteByte('[')
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			c.tconv(&b, u.TypeArgs().At(i), fmtID, false)
		}
		b.WriteByte(']')
		name = b.String()
	}
	if mode == fmtID && o.Pkg() != nil && o.Parent() != o.Pkg().Scope() {
		if gen := c.vargen(o); gen != 0 {
			name += "·" + strconv.Itoa(gen)
		}
	}
	return name
}

// vargen is the number the stock compiler gives a type declared inside a function: a count, over the
// package's files in order, of the type declarations inside function bodies (noder: declCollector).
func (c *compiler) vargen(o *types.TypeName) int {
	m := c.rtVargen[o.Pkg()]
	if m == nil {
		var p *Pkg
		if samePkg(o.Pkg(), c.tpkg) {
			p = c.pkg
		} else {
			p = c.pr.byPath[o.Pkg().Path()]
		}
		if p == nil || len(p.files) == 0 {
			giveUp("local type %v of a package without source", o)
		}
		m = map[token.Pos]int{}
		gen := 0
		for _, f := range p.files {
			var walk func(n ast.Node, within bool)
			walk = func(n ast.Node, within bool) {
				ast.Inspect(n, func(x ast.Node) bool {
					switch x := x.(type) {
					case *ast.BlockStmt:
						if !within {
							for _, st := range x.List {
								walk(st, true)
							}
							return false
						}
					case *ast.TypeSpec:
						if within && !x.Assign.IsValid() {
							gen++
							m[x.Name.Pos()] = gen
						}
					}
					return true
				})
			}
			walk(f, false)
		}
		c.rtVargen[o.Pkg()] = m
	}
	return m[o.Pos()]
}

// tconv writes t's name (stock: types.tconv2). short is the stock formatter's 'S' verb: a defined type
// without its package, a function type without "func".
func (c *compiler) tconv(b *strings.Builder, t types.Type, mode fmtMode, short bool) {
	t = types.Unalias(t)
	switch u := t.(type) {
	case *types.Basic:
		b.WriteString(basicName(u))
	case *types.Named:
		o := u.Obj()
		if o.Pkg() != nil && !short {
			b.WriteString(c.qual(o.Pkg(), mode))
			b.WriteByte('.')
		}
		b.WriteString(c.namedSymName(u, mode))
	case *types.Pointer:
		b.WriteByte('*')
		c.tconv(b, u.Elem(), mode, short)
	case *types.Array:
		b.WriteByte('[')
		b.WriteString(strconv.FormatInt(u.Len(), 10))
		b.WriteByte(']')
		c.tconv(b, u.Elem(), mode, false)
	case *types.Slice:
		b.WriteString("[]")
		c.tconv(b, u.Elem(), mode, false)
	case *types.Chan:
		switch u.Dir() {
		case types.RecvOnly:
			b.WriteString("<-chan ")
			c.tconv(b, u.Elem(), mode, false)
		case types.SendOnly:
			b.WriteString("chan<- ")
			c.tconv(b, u.Elem(), mode, false)
		default:
			b.WriteString("chan ")
			if e, ok := types.Unalias(u.Elem()).(*types.Chan); ok && e.Dir() == types.RecvOnly {
				b.WriteByte('(')
				c.tconv(b, e, mode, false)
				b.WriteByte(')')
			} else {
				c.tconv(b, u.Elem(), mode, false)
			}
		}
	case *types.Map:
		b.WriteString("map[")
		c.tconv(b, u.Key(), mode, false)
		b.WriteByte(']')
		c.tconv(b, u.Elem(), mode, false)
	case *types.Interface:
		if !u.IsMethodSet() {
			giveUp("type descriptor for the constraint %v", u)
		}
		ms := ifaceMethods(u)
		if len(ms) == 0 {
			b.WriteString("interface {}")
			break
		}
		b.WriteString("interface {")
		for i, m := range ms {
			if i != 0 {
				b.WriteByte(';')
			}
			b.WriteByte(' ')
			if !m.Exported() {
				if q := c.qual(m.Pkg(), mode); q != "" {
					b.WriteString(q)
					b.WriteByte('.')
				}
			}
			b.WriteString(m.Name())
			c.tconv(b, m.Type(), mode, true)
		}
		b.WriteString(" }")
	case *types.Signature:
		if u.TypeParams().Len() > 0 {
			giveUp("type descriptor for a generic function type")
		}
		if !short {
			b.WriteString("func")
		}
		c.tconvParams(b, u.Params(), u.Variadic(), mode)
		switch u.Results().Len() {
		case 0:
		case 1:
			b.WriteByte(' ')
			c.tconv(b, u.Results().At(0).Type(), mode, false)
		default:
			b.WriteByte(' ')
			c.tconvParams(b, u.Results(), false, mode)
		}
	case *types.Struct:
		if m := c.rtGroup[u]; m != nil {
			b.WriteString("map.group[")
			c.tconv(b, m.Key(), mode, false)
			b.WriteByte(']')
			c.tconv(b, m.Elem(), mode, false)
			break
		}
		b.WriteString("struct {")
		for i := 0; i < u.NumFields(); i++ {
			if i != 0 {
				b.WriteByte(';')
			}
			b.WriteByte(' ')
			c.fldconv(b, u.Field(i), u.Tag(i), mode)
		}
		if u.NumFields() != 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('}')
	case *types.TypeParam:
		giveUp("type descriptor for the type parameter %v", u)
	default:
		giveUp("type descriptor for %v", t)
	}
}

func (c *compiler) tconvParams(b *strings.Builder, ps *types.Tuple, variadic bool, mode fmtMode) {
	b.WriteByte('(')
	for i := 0; i < ps.Len(); i++ {
		if i != 0 {
			b.WriteString(", ")
		}
		pt := ps.At(i).Type()
		if variadic && i == ps.Len()-1 {
			b.WriteString("...")
			switch s := pt.Underlying().(type) {
			case *types.Slice:
				pt = s.Elem()
			default: // append([]byte, string...)
				pt = types.Typ[types.Uint8]
			}
		}
		c.tconv(b, pt, mode, false)
	}
	b.WriteByte(')')
}

// fldconv writes a struct field (stock: fldconv with the 'L' verb).
func (c *compiler) fldconv(b *strings.Builder, f *types.Var, tag string, mode fmtMode) {
	name := f.Name()
	sep := " "
	show := true
	if f.Embedded() {
		if mode == fmtID {
			sep = " = "
			// the field's name is left out when it is the name embedding its type gives
			typ := types.Unalias(f.Type())
			if p, ok := typ.(*types.Pointer); ok {
				typ = types.Unalias(p.Elem())
			}
			switch tt := typ.(type) {
			case *types.Named:
				tname := c.namedSymName(tt, fmtID)
				if tname == name && (token.IsExported(tname) || samePkg(tt.Obj().Pkg(), f.Pkg())) {
					show = false
				}
			}
		} else {
			show = false
		}
	}
	if show {
		if !token.IsExported(name) && mode != fmtIDName {
			if q := c.qual(f.Pkg(), mode); q != "" {
				b.WriteString(q)
				b.WriteByte('.')
			}
		}
		b.WriteString(name)
		b.WriteString(sep)
	}
	c.tconv(b, f.Type(), mode, false)
	if tag != "" {
		b.WriteByte(' ')
		b.WriteString(strconv.Quote(tag))
	}
}

// ---- method tables

// methodSig is one entry of a type's method table (stock: reflectdata.typeSig).
type methodSig struct {
	fn    *types.Func
	isym  *obj.LSym        // the function an interface table calls: the receiver is one word
	tsym  *obj.LSym        // the function with the receiver type itself
	mtype *types.Signature // the method's type without the receiver
	typ   *types.Signature // with the receiver as its first parameter
}

// methodLess is the order of method tables: exported names first, then by name, then by package path
// (stock: types.CompareSyms).
func methodLess(a, b *types.Func) bool {
	if a.Exported() != b.Exported() {
		return a.Exported()
	}
	if a.Name() != b.Name() {
		return a.Name() < b.Name()
	}
	if !a.Exported() {
		return a.Pkg().Path() < b.Pkg().Path()
	}
	return false
}

// recvBase is the type whose methods t has: t or what it points to, when that is a defined type (not an
// interface, not a pointer) or a struct (stock: types.ReceiverBaseType).
func recvBase(t types.Type) (base types.Type, ptr bool) {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t, ptr = types.Unalias(p.Elem()), true
	}
	switch u := t.(type) {
	case *types.Named:
		switch u.Underlying().(type) {
		case *types.Interface, *types.Pointer:
			return nil, false
		}
		return t, ptr
	case *types.Struct:
		return t, ptr
	}
	return nil, false
}

// methodSymName is the linker's name of the method m with the receiver type recv (stock: ir.MethodSym).
func (c *compiler) methodSymName(recv types.Type, m *types.Func) (name, pkg string) {
	// the receiver's package is that of the defined type recv is or points to; a struct, or the pointer
	// to a pointer that carries a not-in-heap type in an interface, is in the pseudo-package "go"
	base := types.Unalias(recv)
	ptr := false
	if p, ok := base.(*types.Pointer); ok {
		base, ptr = types.Unalias(p.Elem()), true
	}
	prefix := "go"
	var rpkg *types.Package
	if n, ok := base.(*types.Named); ok {
		rpkg = n.Obj().Pkg()
		prefix = c.pkgPrefix(rpkg)
	}
	var b strings.Builder
	b.WriteString(prefix)
	if rpkg == nil {
		b.WriteByte(':') // a reserved pseudo-package: "go:"
	} else {
		b.WriteByte('.')
	}
	if ptr {
		b.WriteByte('(')
		c.tconv(&b, recv, fmtID, true)
		b.WriteByte(')')
	} else {
		c.tconv(&b, recv, fmtID, true)
	}
	if !m.Exported() && !samePkg(m.Pkg(), rpkg) {
		b.WriteByte('.')
		b.WriteString(c.pkgPrefix(m.Pkg()))
	}
	b.WriteByte('.')
	b.WriteString(m.Name())
	return b.String(), prefix
}

// methodSym is the symbol of recv.m as sel selects it; it records the need for a body when the symbol is
// not a method this or another package declares.
func (c *compiler) methodSym(recv types.Type, sel *types.Selection) *obj.LSym {
	m := sel.Obj().(*types.Func)
	name, pkg := c.methodSymName(recv, m)
	s := c.lookup(name, obj.ABIInternal, pkg)
	base, ptr := recvBase(recv)
	if base == nil { // **T, T not in the heap
		base, _ = recvBase(types.Unalias(recv).(*types.Pointer).Elem())
		ptr = true
	}
	if !c.needEmit(base) || c.rtWrap[s] {
		return s
	}
	c.rtWrap[s] = true
	sig := m.Type().(*types.Signature)
	declPtr := false
	if sig.Recv() != nil {
		_, declPtr = types.Unalias(sig.Recv().Type()).(*types.Pointer)
	}
	direct := len(sel.Index()) == 1 && declPtr == ptr && types.Identical(sel.Recv(), recv)
	named, _ := base.(*types.Named)
	inst := named != nil && named.TypeArgs().Len() > 0
	if direct && !inst {
		return s // a declared method of this package
	}
	c.needWrapper = append(c.needWrapper, wrapperReq{sym: s, recv: recv, method: m, sel: sel, declPtr: declPtr,
		instance: direct && inst, dupok: named == nil || inst})
	return s
}

// methods lists the methods of the non-interface type t in method-table order.
func (c *compiler) methods(t types.Type) []methodSig {
	base, _ := recvBase(t)
	if base == nil {
		return nil
	}
	key := c.typeSymName(t)
	if ms, ok := c.rtMeth[key]; ok {
		return ms
	}
	mset := types.NewMethodSet(t)
	sels := make([]*types.Selection, 0, mset.Len())
	for i := 0; i < mset.Len(); i++ {
		sels = append(sels, mset.At(i))
	}
	sort.Slice(sels, func(i, j int) bool { return methodLess(sels[i].Obj().(*types.Func), sels[j].Obj().(*types.Func)) })
	ms := make([]methodSig, 0, len(sels))
	irecv := t
	if !isDirectIface(t) {
		irecv = types.NewPointer(t)
	}
	for _, sel := range sels {
		fn := sel.Obj().(*types.Func)
		sig := fn.Type().(*types.Signature)
		if sig.TypeParams().Len() > 0 {
			continue // a generic method is no part of the method table: it cannot be called through an interface
		}
		var isel *types.Selection
		if irecv == t {
			isel = sel
		} else {
			isel = types.NewMethodSet(irecv).Lookup(fn.Pkg(), fn.Name())
			if isel == nil {
				// irecv is **T (a pointer to a not-in-heap type is not a pointer to the collector, so
				// the interface holds a pointer to it): the wrapper loads *T and calls on
				isel = sel
			}
		}
		ps := make([]*types.Var, 0, sig.Params().Len()+1)
		ps = append(ps, types.NewParam(0, nil, "", t))
		for i := 0; i < sig.Params().Len(); i++ {
			ps = append(ps, sig.Params().At(i))
		}
		ms = append(ms, methodSig{
			fn:    fn,
			isym:  c.methodSym(irecv, isel),
			tsym:  c.methodSym(t, sel),
			mtype: types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic()),
			typ:   types.NewSignatureType(nil, nil, nil, types.NewTuple(ps...), sig.Results(), sig.Variadic()),
		})
	}
	c.rtMeth[key] = ms
	return ms
}

// methodSymFor is methodSym for callers outside the descriptor writer (it takes the lock).
func (c *compiler) methodSymFor(recv types.Type, sel *types.Selection) *obj.LSym {
	c.rtMu.Lock()
	defer c.rtMu.Unlock()
	c.rtInit()
	return c.methodSym(recv, sel)
}
