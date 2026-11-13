// static.go: the package level: global variables and their initialization, functions, the init task.
package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"sync"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

// compilePackage compiles everything; it returns the reasons it could not, if any.
func (c *compiler) compilePackage() (errs []string) {
	c.registerFiles()
	var errMu sync.Mutex
	try := func(what string, f func()) {
		defer func() {
			if r := recover(); r != nil {
				if b, ok := r.(bail); ok {
					errMu.Lock()
					errs = append(errs, what+": "+string(b))
					errMu.Unlock()
					return
				}
				fmt.Fprintf(os.Stderr, "fc: internal error in %s\n", what)
				panic(r)
			}
		}()
		f()
	}
	// global variables
	for _, f := range c.pkg.files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				var embeds []string
				if len(gd.Specs) == 1 {
					embeds = embedPatterns(gd.Doc, vs.Doc)
				} else {
					embeds = embedPatterns(vs.Doc)
				}
				for _, n := range vs.Names {
					if n.Name == "_" {
						continue
					}
					v := c.info.Defs[n].(*types.Var)
					try("var "+n.Name, func() {
						c.defGlobal(v)
						if len(embeds) > 0 {
							c.embedVar(v, embeds)
						}
					})
				}
			}
		}
	}
	// initializers: into the data when the value is known now, else into the init function
	type dyn struct {
		in *types.Initializer
	}
	var dyns []*types.Initializer
	for _, in := range c.info.InitOrder {
		in := in
		static := false
		if len(in.Lhs) == 1 && in.Lhs[0].Name() == "_" {
			try("var _", func() { static = c.isStatic(in.Rhs, in.Lhs[0].Type()) })
		}
		if len(in.Lhs) == 1 && in.Lhs[0].Name() != "_" {
			try("var "+in.Lhs[0].Name(), func() {
				v := in.Lhs[0]
				if c.isStatic(in.Rhs, v.Type()) {
					c.writeStatic(c.globalSym(v), 0, in.Rhs, v.Type())
					static = true
				}
			})
		}
		if !static {
			dyns = append(dyns, in)
			if len(in.Lhs) > 0 {
				c.dynInits = append(c.dynInits, in.Lhs[0].Name())
			}
		}
	}
	// functions: each is independent of the others; they are compiled on whatever cores are free
	par := &parallel{pool: c.fnSem, prio: c.pkg.srcBytes + 1, helpers: c.fnHelpers}
	var inits []*obj.LSym
	ninit := 0
	for _, fd := range c.funcDecls() {
		fd := fd
		fn, _ := c.info.Defs[fd.Name].(*types.Func)
		if fn == nil {
			continue
		}
		isInit := fd.Recv == nil && fd.Name.Name == "init"
		var initSym *obj.LSym
		if isInit {
			initSym = c.lookup(fmt.Sprintf("%s.init.%d", objabi.PathToPrefix(c.path), ninit), obj.ABIInternal, objabi.PathToPrefix(c.path))
			ninit++
			inits = append(inits, initSym)
		}
		if fd.Body == nil || fd.Name.Name == "_" {
			continue
		}
		sig := fn.Type().(*types.Signature)
		if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
			continue // compiled where it is instantiated
		}
		par.run(func() {
			try("func "+fn.FullName(), func() {
				sym := initSym
				if sym == nil {
					sym = c.funcSym(fn)
				}
				g := c.newGen(sym.Name, sig, c.pkg.infoFor(fd), nil)
				g.out.sym = sym
				g.nosplit = hasDirective(fd.Doc, "//go:nosplit") || intrinsicPkg[c.path] || (c.pr.inRuntime(c.pkg) && inlinable(fd))
				g.noWBDecl = hasDirective(fd.Doc, "//go:nowritebarrier") || hasDirective(fd.Doc, "//go:nowritebarrierrec")
				// (//go:systemstack, //go:nowritebarrier and its relatives are promises the source makes and
				// the stock compiler verifies; nothing to generate for them)
				for _, d := range []string{"//go:cgo_unsafe_args", "//go:wasmimport", "//go:wasmexport"} {
					if hasDirective(fd.Doc, d) {
						giveUp("directive %s", d)
					}
				}
				g.compileFunc(sig.Recv(), fd.Pos(), fd.Body.Rbrace, fd.Body, func() { g.block(fd.Body.List) })
			})
		})
	}
	par.wait()
	c.abiWrappers(try)
	// the init function: the dynamic initializers, in order
	pp := objabi.PathToPrefix(c.path)
	var fns []*obj.LSym
	if len(dyns) > 0 {
		try("package initializers", func() {
			sym := c.lookup(pp+".init", obj.ABIInternal, pp)
			g := c.newGen(sym.Name, types.NewSignatureType(nil, nil, nil, nil, nil, false), c.info, nil)
			g.out.sym = sym
			g.out.flags |= obj.PKGINIT
			pos := c.pkg.files[0].Package
			for _, in := range dyns {
				g.findHeapVars(in.Rhs)
			}
			g.compileFunc(nil, pos, pos, nil, func() {
				for _, in := range dyns {
					m := g.mark()
					g.pos(in.Rhs.Pos())
					switch {
					case len(in.Lhs) == 1 && in.Lhs[0].Name() == "_":
						if ce, ok := ast.Unparen(in.Rhs).(*ast.CallExpr); ok {
							g.call(ce)
						} else {
							g.expr(in.Rhs)
						}
					case len(in.Lhs) == 1:
						v := in.Lhs[0]
						g.exprTo(in.Rhs, loc{kind: lSym, sym: c.globalSym(v)}, v.Type())
					default:
						rs := g.multi(in.Rhs)
						for i, v := range in.Lhs {
							if v.Name() == "_" {
								continue
							}
							g.move(loc{kind: lSym, sym: c.globalSym(v)}, g.convertImplicit(rs[i].l, rs[i].t, v.Type()), v.Type())
						}
					}
					g.release(m)
				}
			})
			fns = append(fns, sym)
		})
	}
	fns = append(fns, inits...)
	c.ninit = len(inits)
	// instances of generic functions the code above (and each other) asked for
	for len(c.instQueue) > 0 {
		in := c.instQueue[0]
		c.instQueue = c.instQueue[1:]
		try("instance "+in.sym.Name, func() { c.compileInstance(in) })
	}
	// the init task
	var deps []*obj.LSym
	for _, d := range c.pkg.deps {
		if c.depInit != nil && c.depInit(d) {
			dp := objabi.PathToPrefix(plainPath(d.ImportPath))
			deps = append(deps, c.lookup(dp+"..inittask", obj.ABI0, dp))
		}
	}
	if len(fns) > 0 || len(deps) > 0 || c.forceInit {
		task := c.lookup(pp+"..inittask", obj.ABI0, pp)
		task.WriteInt(c.ctxt, 0, 4, 0)
		task.WriteInt(c.ctxt, 4, 4, int64(len(fns)))
		for i, f := range fns {
			task.WriteAddr(c.ctxt, int64(8+8*i), 8, f, 0)
		}
		for _, d := range deps {
			task.AddRel(c.ctxt, obj.Reloc{Type: objabi.R_INITORDER, Sym: d})
		}
		c.defData(task, objabi.SNOPTRDATA, int64(8+8*len(fns)))
	}
	c.hasInit = len(fns) > 0 || len(deps) > 0
	// a symbol with a //go:linkname directive may be referenced from other packages by name
	c.pr.linknames(c.pkg)
	for _, set := range []struct {
		names map[string]bool
		attr  obj.Attribute
	}{{c.pkg.linknamed, obj.AttrLinkname}, {c.pkg.linknamedStd, obj.AttrLinknameStd}} {
		for name := range set.names {
			switch o := c.tpkg.Scope().Lookup(name).(type) {
			case *types.Func:
				s := c.funcSym(o)
				s.Set(set.attr, true)
				c.lookup(s.Name, obj.ABI0, s.Pkg).Set(set.attr, true)
			case *types.Var:
				c.globalSym(o).Set(set.attr, true)
			}
		}
	}
	try("type descriptors", func() { c.writeDeclaredTypes() })
	if c.parity != nil {
		try("symbols the stock object defines", c.parity)
	}
	// instances of generic functions and the code descriptors need, each of which may ask for more
	for again := true; again; {
		again = false
		for len(c.instQueue) > 0 {
			in := c.instQueue[0]
			c.instQueue = c.instQueue[1:]
			try("instance "+in.sym.Name, func() { c.compileInstance(in) })
			again = true
		}
		try("code for type descriptors", func() {
			if c.unwrittenTypeCode() {
				again = true
			}
		})
	}
	if len(errs) == 0 {
		c.installFuncs()
	}
	return errs
}

// defGlobal defines the symbol of a package-level variable.
func (c *compiler) defGlobal(v *types.Var) {
	s := c.globalSym(v)
	t := v.Type()
	kind := objabi.SNOPTRBSS
	if c.hasPointers(t) {
		kind = objabi.SBSS
		s.Gotype = c.typeSym(t)
	}
	c.defData(s, kind, c.sizeof(t))
}

// isStatic: can the value of e, as a t, be written into the data at compile time?
func (c *compiler) isStatic(e ast.Expr, t types.Type) bool { return c.static(nil, 0, e, t, false) }

// writeStatic writes the static value of e at s+off.
func (c *compiler) writeStatic(s *obj.LSym, off int64, e ast.Expr, t types.Type) {
	c.static(s, off, e, t, true)
}

// defStaticVar defines an anonymous static variable of type t (the target of a pointer or the backing
// array of a slice in a static initializer).
func (c *compiler) defStaticVar(t types.Type, readonly bool) *obj.LSym {
	s := c.staticSym("stmp")
	switch {
	case readonly:
		c.defData(s, objabi.SRODATA, c.sizeof(t))
	case c.hasPointers(t):
		s.Gotype = c.typeSym(t)
		c.defData(s, objabi.SBSS, c.sizeof(t))
	default:
		c.defData(s, objabi.SNOPTRBSS, c.sizeof(t))
	}
	return s
}

// globalAddr: the symbol and offset of an addressable expression made of a package-level variable, fields
// and constant indexes.
func (c *compiler) globalAddr(e ast.Expr) (*obj.LSym, int64, bool) {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		if v, ok := c.info.Uses[e].(*types.Var); ok && v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			return c.globalSym(v), 0, true
		}
	case *ast.SelectorExpr:
		sel := c.info.Selections[e]
		if sel == nil {
			if v, ok := c.info.Uses[e.Sel].(*types.Var); ok && v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
				return c.globalSym(v), 0, true
			}
			return nil, 0, false
		}
		if sel.Kind() != types.FieldVal {
			return nil, 0, false
		}
		s, off, ok := c.globalAddr(e.X)
		if !ok {
			return nil, 0, false
		}
		t := c.info.Types[e.X].Type
		for _, i := range sel.Index() {
			st, ok := t.Underlying().(*types.Struct)
			if !ok {
				return nil, 0, false // through a pointer
			}
			off += c.fieldOffsets(st)[i]
			t = st.Field(i).Type()
		}
		return s, off, true
	case *ast.IndexExpr:
		arr, ok := c.info.Types[e.X].Type.Underlying().(*types.Array)
		iv := c.info.Types[e.Index].Value
		if !ok || iv == nil {
			return nil, 0, false
		}
		s, off, ok := c.globalAddr(e.X)
		if !ok {
			return nil, 0, false
		}
		i, _ := constant.Int64Val(constant.ToInt(iv))
		if i < 0 || i >= arr.Len() {
			return nil, 0, false
		}
		return s, off + i*c.sizeof(arr.Elem()), true
	}
	return nil, 0, false
}

// static checks (write false) or writes (write true, after a successful check) the value of e as a t at
// s+off.
func (c *compiler) static(s *obj.LSym, off int64, e ast.Expr, t types.Type, write bool) bool {
	e = ast.Unparen(e)
	tv := c.info.Types[e]
	if isInterface(t) {
		if tv.IsNil() {
			return true
		}
		et := tv.Type
		if et == nil || isInterface(et) {
			return false
		}
		if b, ok := et.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			et = types.Default(et)
		}
		if mentionsTypeParam(et) || !c.static(nil, 0, e, et, false) {
			return false
		}
		if write {
			tsym := c.typeSym(et)
			word := tsym
			if !isEmptyIface(t) {
				word = c.itabSym(et, t)
			}
			s.WriteAddr(c.ctxt, off, 8, word, 0)
			s.AddRel(c.ctxt, obj.Reloc{Type: objabi.R_USEIFACE, Sym: tsym})
			switch {
			case c.sizeof(et) == 0:
				s.WriteAddr(c.ctxt, off+8, 8, c.rtVar("zerobase"), 0)
			case isDirectIface(et):
				c.static(s, off+8, e, et, true)
			default:
				tmp := c.defStaticVar(et, true)
				c.static(tmp, 0, e, et, true)
				s.WriteAddr(c.ctxt, off+8, 8, tmp, 0)
			}
		}
		return true
	}
	if tv.Value != nil {
		if _, ok := t.Underlying().(*types.Basic); !ok && !isWord(t) {
			return false
		}
		if write {
			c.writeConst(s, off, tv.Value, t)
		}
		return true
	}
	if tv.IsNil() {
		return true
	}
	switch e := e.(type) {
	case *ast.CompositeLit:
		switch u := t.Underlying().(type) {
		case *types.Struct:
			offs := c.fieldOffsets(u)
			for i, el := range e.Elts {
				fi := i
				val := el
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					f, _ := c.info.Uses[kv.Key.(*ast.Ident)].(*types.Var)
					fi = -1
					for j := 0; j < u.NumFields(); j++ {
						if u.Field(j) == f {
							fi = j
						}
					}
					if fi < 0 {
						return false // a promoted field
					}
					val = kv.Value
				}
				if !c.static(s, off+offs[fi], val, u.Field(fi).Type(), write) {
					return false
				}
			}
			return true
		case *types.Array:
			return c.staticElems(s, off, e, u.Elem(), write)
		case *types.Slice:
			n := int64(0)
			var i int64
			for _, el := range e.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					iv := c.info.Types[kv.Key].Value
					if iv == nil {
						return false
					}
					i, _ = constant.Int64Val(constant.ToInt(iv))
				}
				i++
				if i > n {
					n = i
				}
			}
			if !c.staticElems(nil, 0, e, u.Elem(), false) {
				return false
			}
			if write {
				var data *obj.LSym
				if n == 0 {
					data = c.rtVar("zerobase")
				} else {
					data = c.defStaticVar(types.NewArray(u.Elem(), n), false)
					c.staticElems(data, 0, e, u.Elem(), true)
				}
				s.WriteAddr(c.ctxt, off, 8, data, 0)
				s.WriteInt(c.ctxt, off+8, 8, n)
				s.WriteInt(c.ctxt, off+16, 8, n)
			}
			return true
		case *types.Pointer:
			// &T{...} with the & elided
			if !c.static(nil, 0, e, u.Elem(), false) {
				return false
			}
			if write {
				tmp := c.defStaticVar(u.Elem(), false)
				c.static(tmp, 0, e, u.Elem(), true)
				s.WriteAddr(c.ctxt, off, 8, tmp, 0)
			}
			return true
		}
		return false
	case *ast.UnaryExpr:
		if e.Op != token.AND {
			return false
		}
		if cl, ok := ast.Unparen(e.X).(*ast.CompositeLit); ok {
			et := c.info.Types[cl].Type
			if et == nil || mentionsTypeParam(et) || !c.static(nil, 0, cl, et, false) {
				return false
			}
			if write {
				tmp := c.defStaticVar(et, false)
				c.static(tmp, 0, cl, et, true)
				s.WriteAddr(c.ctxt, off, 8, tmp, 0)
			}
			return true
		}
		if as, aoff, ok := c.globalAddr(e.X); ok {
			if write {
				s.WriteAddr(c.ctxt, off, 8, as, aoff)
			}
			return true
		}
		return false
	case *ast.Ident, *ast.SelectorExpr:
		id := identOf(e)
		if se, ok := e.(*ast.SelectorExpr); ok && c.info.Selections[se] != nil {
			return false
		}
		fn, ok := c.info.Uses[id].(*types.Func)
		if !ok || c.isIntrinsic(fn) {
			return false
		}
		if sig := fn.Type().(*types.Signature); sig.TypeParams().Len() > 0 || sig.Recv() != nil {
			return false
		}
		if _, generic := c.info.Instances[id]; generic {
			return false
		}
		if write {
			s.WriteAddr(c.ctxt, off, 8, c.staticClosure(c.funcSym(fn)), 0)
		}
		return true
	case *ast.FuncLit:
		// at package level a function literal captures nothing
		if write {
			s.WriteAddr(c.ctxt, off, 8, c.staticFuncLit(e), 0)
		}
		return true
	case *ast.CallExpr:
		// a conversion that keeps the representation
		if ftv := c.info.Types[e.Fun]; ftv.IsType() && len(e.Args) == 1 {
			// []byte("constant"): the bytes are data (the runtime reads such variables before any
			// initializer has run)
			if sl, ok := t.Underlying().(*types.Slice); ok {
				if b, ok := sl.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 {
					if v := c.info.Types[e.Args[0]].Value; v != nil && v.Kind() == constant.String {
						if write {
							str := constant.StringVal(v)
							n := int64(len(str))
							data := c.rtVar("zerobase")
							if n > 0 {
								data = c.defStaticVar(types.NewArray(sl.Elem(), n), false)
								data.WriteBytes(c.ctxt, 0, []byte(str))
							}
							s.WriteAddr(c.ctxt, off, 8, data, 0)
							s.WriteInt(c.ctxt, off+8, 8, n)
							s.WriteInt(c.ctxt, off+16, 8, n)
						}
						return true
					}
				}
			}
			at := c.info.Types[e.Args[0]].Type
			if at != nil && !isInterface(at) && (types.Identical(at.Underlying(), t.Underlying()) || isPtrWord(at) && isPtrWord(t)) {
				return c.static(s, off, e.Args[0], at, write)
			}
		}
		return false
	}
	return false
}

// staticElems: the elements of an array or slice literal.
func (c *compiler) staticElems(s *obj.LSym, off int64, cl *ast.CompositeLit, et types.Type, write bool) bool {
	es := c.sizeof(et)
	var i int64
	for _, el := range cl.Elts {
		val := el
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			iv := c.info.Types[kv.Key].Value
			if iv == nil {
				return false
			}
			i, _ = constant.Int64Val(constant.ToInt(iv))
			val = kv.Value
		}
		if !c.static(s, off+i*es, val, et, write) {
			return false
		}
		i++
	}
	return true
}

// staticFuncLit compiles a function literal of a package-level initializer and returns its closure symbol.
func (c *compiler) staticFuncLit(e *ast.FuncLit) *obj.LSym {
	c.mu.Lock()
	if c.globGen == nil {
		c.globGen = c.newGen(objabi.PathToPrefix(c.path)+".glob.", types.NewSignatureType(nil, nil, nil, nil, nil, false), c.info, nil)
	}
	g := c.globGen
	c.mu.Unlock()
	sig := c.info.Types[e].Type.(*types.Signature)
	cg := g.child(g.childName("func"), sig)
	cg.compileFunc(nil, e.Pos(), e.Body.Rbrace, e.Body, func() { cg.block(e.Body.List) })
	return c.staticClosure(cg.out.sym)
}

func (c *compiler) writeConst(s *obj.LSym, off int64, v constant.Value, t types.Type) {
	size := int(c.sizeof(t))
	b, _ := t.Underlying().(*types.Basic)
	switch {
	case b == nil:
		i, _ := constant.Int64Val(constant.ToInt(v))
		if i != 0 {
			s.WriteInt(c.ctxt, off, size, i)
		}
	case b.Info()&types.IsBoolean != 0:
		if constant.BoolVal(v) {
			s.WriteInt(c.ctxt, off, 1, 1)
		}
	case b.Info()&types.IsInteger != 0 || b.Kind() == types.UnsafePointer:
		v = constant.ToInt(v)
		i, ok := constant.Int64Val(v)
		if !ok {
			u, _ := constant.Uint64Val(v)
			i = int64(u)
		}
		if i != 0 {
			s.WriteInt(c.ctxt, off, size, i)
		}
	case b.Info()&types.IsFloat != 0:
		f, _ := constant.Float64Val(constant.ToFloat(v))
		if size == 4 {
			s.WriteFloat32(c.ctxt, off, float32(f))
		} else {
			s.WriteFloat64(c.ctxt, off, f)
		}
	case b.Info()&types.IsString != 0:
		str := constant.StringVal(v)
		if str != "" {
			s.WriteAddr(c.ctxt, off, 8, c.stringSym(str), 0)
			s.WriteInt(c.ctxt, off+8, 8, int64(len(str)))
		}
	case b.Info()&types.IsComplex != 0:
		re, _ := constant.Float64Val(constant.Real(v))
		im, _ := constant.Float64Val(constant.Imag(v))
		if size == 8 {
			s.WriteFloat32(c.ctxt, off, float32(re))
			s.WriteFloat32(c.ctxt, off+4, float32(im))
		} else {
			s.WriteFloat64(c.ctxt, off, re)
			s.WriteFloat64(c.ctxt, off+8, im)
		}
	default:
		giveUp("static constant of type %v", t)
	}
}

// intrinsicPkg: packages whose functions the stock compiler replaces by instructions (bit counting,
// multi-word arithmetic). fc calls their Go bodies, and the runtime calls them inside regions that must
// not be preempted (the bulk write barrier walks pointer masks with TrailingZeros64): a call there must
// not be a preemption point, so these functions have no stack check.
var intrinsicPkg = map[string]bool{"internal/runtime/sys": true, "internal/runtime/math": true, "math/bits": true}

// inlinable: would the stock compiler plausibly inline fd (a small body without loops, defers or
// closures, not marked //go:noinline)? The runtime calls such helpers ((*mspan).base, markBits.setMarked,
// (*Type).Pointers) from regions that must not be preempted and counts on their having no stack check,
// which inlining gives the stock compiler. fc does not inline: in the runtime's packages it compiles them
// without a stack check instead.
func inlinable(fd *ast.FuncDecl) bool {
	if fd.Body == nil || hasDirective(fd.Doc, "//go:noinline") || hasDirective(fd.Doc, "//go:systemstack") {
		return false
	}
	n, ok := 0, true
	ast.Inspect(fd.Body, func(x ast.Node) bool {
		switch x.(type) {
		case nil:
			return false
		case *ast.ForStmt, *ast.RangeStmt, *ast.DeferStmt, *ast.GoStmt, *ast.SelectStmt, *ast.FuncLit, *ast.LabeledStmt:
			ok = false
		}
		n++
		return ok && n <= 120
	})
	return ok && n <= 120
}
