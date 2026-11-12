// static.go: the package level: global variables and their initialization, functions, the init task.
package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

// compilePackage compiles everything; it returns the reasons it could not, if any.
func (c *compiler) compilePackage() (errs []string) {
	c.registerFiles()
	try := func(what string, f func()) {
		defer func() {
			if r := recover(); r != nil {
				if b, ok := r.(bail); ok {
					errs = append(errs, what+": "+string(b))
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
			if hasDirective(gd.Doc, "//go:embed") {
				errs = append(errs, "go:embed")
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				if hasDirective(vs.Doc, "//go:embed") {
					errs = append(errs, "go:embed")
				}
				for _, n := range vs.Names {
					if n.Name == "_" {
						continue
					}
					v := c.info.Defs[n].(*types.Var)
					try("var "+n.Name, func() { c.defGlobal(v) })
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
		}
	}
	// functions
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
		try("func "+fn.FullName(), func() {
			sym := initSym
			if sym == nil {
				sym = c.funcSym(fn)
			}
			g := c.newGen(sym.Name, sig, c.info, nil)
			g.out.sym = sym
			g.nosplit = hasDirective(fd.Doc, "//go:nosplit")
			for _, d := range []string{"//go:systemstack", "//go:nowritebarrier", "//go:nowritebarrierrec", "//go:yeswritebarrierrec", "//go:uintptrescapes", "//go:uintptrkeepalive", "//go:norace", "//go:nocheckptr", "//go:registerparams", "//go:notinheap", "//go:cgo_unsafe_args", "//go:wasmimport", "//go:wasmexport"} {
				if hasDirective(fd.Doc, d) && d != "//go:norace" && d != "//go:nocheckptr" && d != "//go:uintptrkeepalive" && d != "//go:uintptrescapes" {
					giveUp("directive %s", d)
				}
			}
			g.compileFunc(sig.Recv(), fd.Pos(), fd.Body.Rbrace, fd.Body, func() { g.block(fd.Body.List) })
		})
	}
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
			dp := objabi.PathToPrefix(d.ImportPath)
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
func (c *compiler) isStatic(e ast.Expr, t types.Type) bool {
	e = ast.Unparen(e)
	tv := c.info.Types[e]
	if isInterface(t) {
		return tv.IsNil()
	}
	if tv.Value != nil {
		if b, ok := t.Underlying().(*types.Basic); ok {
			return b.Info()&types.IsComplex == 0
		}
		return isWord(t)
	}
	if tv.IsNil() {
		return true
	}
	if cl, ok := e.(*ast.CompositeLit); ok {
		switch u := t.Underlying().(type) {
		case *types.Struct:
			for i, el := range cl.Elts {
				ft := types.Type(nil)
				val := el
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					ft = c.info.Uses[kv.Key.(*ast.Ident)].(*types.Var).Type()
					val = kv.Value
				} else {
					ft = u.Field(i).Type()
				}
				if !c.isStatic(val, ft) {
					return false
				}
			}
			return true
		case *types.Array:
			for _, el := range cl.Elts {
				val := el
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					val = kv.Value
				}
				if !c.isStatic(val, u.Elem()) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// writeStatic writes the static value of e at s+off.
func (c *compiler) writeStatic(s *obj.LSym, off int64, e ast.Expr, t types.Type) {
	e = ast.Unparen(e)
	tv := c.info.Types[e]
	if tv.IsNil() {
		return
	}
	if tv.Value != nil {
		c.writeConst(s, off, tv.Value, t)
		return
	}
	cl := e.(*ast.CompositeLit)
	switch u := t.Underlying().(type) {
	case *types.Struct:
		offs := c.fieldOffsets(u)
		for i, el := range cl.Elts {
			fi := i
			val := el
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				f := c.info.Uses[kv.Key.(*ast.Ident)].(*types.Var)
				for j := 0; j < u.NumFields(); j++ {
					if u.Field(j) == f {
						fi = j
					}
				}
				val = kv.Value
			}
			c.writeStatic(s, off+offs[fi], val, u.Field(fi).Type())
		}
	case *types.Array:
		es := c.sizeof(u.Elem())
		var i int64
		for _, el := range cl.Elts {
			val := el
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				v, _ := constant.Int64Val(constant.ToInt(c.info.Types[kv.Key].Value))
				i = v
				val = kv.Value
			}
			c.writeStatic(s, off+i*es, val, u.Elem())
			i++
		}
	}
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
	default:
		giveUp("static constant of type %v", t)
	}
}
