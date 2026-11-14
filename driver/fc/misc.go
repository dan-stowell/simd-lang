// misc.go: helpers, and the parts of the language not written yet (each gives the package up, by name).
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log"
	"path/filepath"
	"runtime"
	"strings"

	"cmd/internal/goobj"
	"cmd/internal/obj"
	"cmd/internal/objabi"
)

type goobjCUFileIndex = goobj.CUFileIndex

const objabiRCall = objabi.R_CALL

func (c *compiler) rtABI0(name string) *obj.LSym {
	return c.lookup("runtime."+name, obj.ABI0, "runtime")
}

// rtFunc is the declaration of a runtime function the compiler calls, for its signature: from the stock
// compiler's own list (typecheck/_builtin/runtime.go), else from package runtime's source.
func (c *compiler) rtFunc(name string) *types.Func {
	pr := c.pr
	pr.rtOnce.Do(func() {
		path := filepath.Join(runtime.GOROOT(), "src/cmd/compile/internal/typecheck/_builtin/runtime.go")
		f, err := parser.ParseFile(pr.fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			log.Fatalf("fc: %v", err)
		}
		conf := types.Config{Importer: pkgImporter{pr, &Pkg{}}, Sizes: pr.sizes}
		pr.rtDecls, err = conf.Check("runtime", pr.fset, []*ast.File{f}, nil)
		if err != nil {
			log.Fatalf("fc: %v", err)
		}
	})
	if fn, ok := pr.rtDecls.Scope().Lookup(name).(*types.Func); ok {
		return fn
	}
	rp := pr.byPath["runtime"]
	var rtp *types.Package
	if rp == c.pkg {
		rtp = c.tpkg
	} else if rp != nil {
		rtp, _ = pr.view(rp)
	}
	if rtp == nil {
		giveUp("package runtime is not loaded")
	}
	fn, ok := rtp.Scope().Lookup(name).(*types.Func)
	if !ok {
		giveUp("runtime.%s is not a function", name)
	}
	return fn
}

func (c *compiler) staticSym(kind string) *obj.LSym {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tmpN++
	// (obj's table of static symbols has no lock of its own)
	return c.ctxt.LookupStatic(objabi.PathToPrefix(c.path) + "..fc" + kind + "." + itoa(c.tmpN))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// isIntrinsic: a function the compiler must expand itself (it has no body anywhere).
func (c *compiler) isIntrinsic(fn *types.Func) bool {
	if fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() {
	case "runtime":
		switch fn.Name() {
		case "getcallerpc", "getcallersp", "getclosureptr", "getg":
			return true
		}
	case "internal/runtime/sys":
		switch fn.Name() {
		case "GetCallerPC", "GetCallerSP", "GetClosurePtr":
			return true
		}
	case "internal/abi":
		switch fn.Name() {
		case "FuncPCABI0", "FuncPCABIInternal", "EscapeNonString":
			return true
		}
	case "crypto/internal/constanttime":
		return fn.Name() == "boolToUint8" // its body panics: "must be intrinsicified"
	}
	return false
}

// intrinsic expands a call of one of those functions.
func (g *gen) intrinsic(fn *types.Func, e *ast.CallExpr) []loc {
	ax := []loc{{kind: lReg, reg: AX}}
	switch fn.Name() {
	case "EscapeNonString":
		g.expr(e.Args[0]) // a hint to escape analysis; here everything escapes already
		return nil
	case "boolToUint8":
		g.loadTo(AX, g.expr(e.Args[0]), types.Typ[types.Bool])
		return ax
	case "getcallerpc", "GetCallerPC":
		g.a.load(AX, mb(BP, 8), 8, false)
		return ax
	case "getcallersp", "GetCallerSP":
		g.a.lea(AX, mb(BP, 16))
		return ax
	case "getclosureptr", "GetClosurePtr":
		if g.ctxSlot.kind == lNone {
			giveUp("getclosureptr outside a closure")
		}
		g.a.load(AX, mb(BP, g.ctxSlot.off), 8, false)
		return ax
	case "getg":
		g.a.movRR(AX, R14)
		return ax
	case "FuncPCABI0", "FuncPCABIInternal":
		id := identOf(ast.Unparen(e.Args[0]))
		if id == nil {
			giveUp("%s of an expression", fn.Name())
		}
		f, ok := useOf(id).(*types.Func)
		if !ok {
			// a function value: its code pointer is the first word of the closure object. An interface
			// (FuncPCABIInternal(f) with f any, as runtime/pprof's tests do) holds the function value in
			// its data word.
			t := g.typeOf(e.Args[0])
			l := g.expr(e.Args[0])
			if types.IsInterface(t) {
				l = g.inMem(l, t)
				g.a.load(AX, g.m(l, R13).plus(8), 8, false)
			} else {
				g.loadTo(AX, l, unsafePtr)
			}
			g.a.load(AX, mb(AX, 0), 8, false)
			return ax
		}
		sym := g.c.funcSym(f)
		if fn.Name() == "FuncPCABI0" {
			sym = g.c.lookup(sym.Name, obj.ABI0, sym.Pkg)
		}
		g.a.lea(AX, msym(sym, 0))
		return ax
	}
	giveUp("compiler intrinsic %s", fn.FullName())
	return nil
}

// findHeapVars marks the local variables that must live on the heap: their address is taken (explicitly,
// by a method call with a pointer receiver, or by slicing an array), or a closure captures them.
func (g *gen) findHeapVars(body ast.Node) {
	_ = g.info
	var root func(e ast.Expr) *types.Var
	root = func(e ast.Expr) *types.Var {
		switch e := e.(type) {
		case *ast.Ident:
			if v, ok := useOf(e).(*types.Var); ok && !v.IsField() {
				return v
			}
			if v, ok := defOf(e).(*types.Var); ok && !v.IsField() {
				return v
			}
		case *ast.ParenExpr:
			return root(e.X)
		case *ast.SelectorExpr:
			if sel := selnOf(e); sel != nil && sel.Kind() == types.FieldVal {
				// only while no pointer is followed on the way
				t := g.typeOf(e.X)
				for _, i := range sel.Index() {
					if _, ok := t.Underlying().(*types.Pointer); ok {
						return nil
					}
					t = t.Underlying().(*types.Struct).Field(i).Type()
				}
				return root(e.X)
			}
		case *ast.IndexExpr:
			if _, ok := g.typeOf(e.X).Underlying().(*types.Array); ok {
				return root(e.X)
			}
		}
		return nil
	}
	if g.addrVars == nil {
		g.addrVars = map[*types.Var]bool{}
	}
	mark := func(v *types.Var) {
		if v != nil && v.Pkg() != nil && v.Parent() != v.Pkg().Scope() {
			g.addrVars[v] = true
			// in the frame only when the analysis says its address stays inside this activation
			if g.esc == nil || g.esc.varOnHeap(v) {
				g.heapVars[v] = true
			}
		}
	}
	var lits []*ast.FuncLit
	var rangeBodies []*ast.BlockStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				mark(root(n.X))
			}
		case *ast.SliceExpr:
			if _, ok := g.typeOf(n.X).Underlying().(*types.Array); ok {
				mark(root(n.X))
			}
		case *ast.SelectorExpr:
			if sel := selnOf(n); sel != nil && sel.Kind() == types.MethodVal {
				fn := sel.Obj().(*types.Func)
				if _, ptr := fn.Type().(*types.Signature).Recv().Type().(*types.Pointer); ptr {
					// x.M() with a pointer receiver: &x, unless a pointer is followed first
					t := g.typeOf(n.X)
					path := sel.Index()
					viaPtr := false
					for _, i := range path[:len(path)-1] {
						if _, ok := t.Underlying().(*types.Pointer); ok {
							viaPtr = true
							break
						}
						t = t.Underlying().(*types.Struct).Field(i).Type()
					}
					if _, ok := t.Underlying().(*types.Pointer); !ok && !viaPtr {
						mark(root(n.X))
					}
				}
			}
		case *ast.CallExpr:
			if id := identOf(ast.Unparen(n.Fun)); id != nil && (id.Name == "getclosureptr" || id.Name == "GetClosurePtr") && g.entry == nil {
				g.entry = g.saveCtx // the function reads its context register: keep it from the start
			}
		case *ast.FuncLit:
			lits = append(lits, n)
		case *ast.RangeStmt:
			if _, ok := g.typeOf(n.X).Underlying().(*types.Signature); ok {
				// the body becomes a function: it captures like a literal, and a return inside it
				// reaches this function's results through the heap
				rangeBodies = append(rangeBodies, n.Body)
				if g.deferTok == nil && g.rf == nil {
					ast.Inspect(n.Body, func(m ast.Node) bool {
						switch m.(type) {
						case *ast.FuncLit:
							return false
						case *ast.DeferStmt:
							g.deferTok = types.NewVar(n.Pos(), nil, "", g.c.anyType())
							g.heapVars[g.deferTok] = true
						}
						return true
					})
				}
				if n.Tok == token.ASSIGN {
					for _, kv := range []ast.Expr{n.Key, n.Value} {
						if kv != nil {
							ast.Inspect(kv, func(m ast.Node) bool {
								if id, ok := m.(*ast.Ident); ok {
									if v, ok := useOf(id).(*types.Var); ok && !v.IsField() {
										mark(v)
									}
								}
								return true
							})
						}
					}
				}
				for i := 0; i < g.sig.Results().Len(); i++ {
					g.heapVars[g.sig.Results().At(i)] = true
				}
			}
		}
		return true
	})
	for _, b := range rangeBodies {
		ast.Inspect(b, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if v, ok := useOf(id).(*types.Var); ok && !v.IsField() {
					if v.Pos() < b.Pos() || v.Pos() >= b.End() {
						mark(v)
					}
				}
			}
			return true
		})
	}
	for _, lit := range lits {
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if v, ok := useOf(id).(*types.Var); ok && !v.IsField() {
					if v.Pos() < lit.Pos() || v.Pos() >= lit.End() {
						mark(v)
					}
				}
			}
			return true
		})
	}
}

// linknames of a package: local name -> symbol name, from its //go:linkname directives.
func (pr *Program) linknames(p *Pkg) map[string]string {
	p.lnOnce.Do(func() {
		p.linkname = map[string]string{}
		for _, f := range p.files {
			for _, cg := range f.Comments {
				for _, cm := range cg.List {
					if strings.HasPrefix(cm.Text, "//go:linknamestd ") {
						if fs := strings.Fields(cm.Text); len(fs) >= 2 {
							if p.linknamedStd == nil {
								p.linknamedStd = map[string]bool{}
							}
							p.linknamedStd[fs[1]] = true
							if len(fs) == 3 {
								p.linkname[fs[1]] = fs[2]
							}
						}
						continue
					}
					if !strings.HasPrefix(cm.Text, "//go:linkname ") {
						continue
					}
					fs := strings.Fields(cm.Text)
					if len(fs) == 3 {
						p.linkname[fs[1]] = fs[2]
					}
					if len(fs) >= 2 {
						if p.linknamed == nil {
							p.linknamed = map[string]bool{}
						}
						p.linknamed[fs[1]] = true
					}
				}
			}
		}
	})
	return p.linkname
}

// ---- not written yet

func (g *gen) funcValue(fn *types.Func, id *ast.Ident) loc {
	if g.c.isIntrinsic(fn) {
		giveUp("compiler intrinsic %s as a value", fn.FullName())
	}
	target, _ := g.funcRef(fn, id)
	g.a.lea(AX, msym(g.c.staticClosure(target), 0))
	return loc{kind: lReg, reg: AX}
}
