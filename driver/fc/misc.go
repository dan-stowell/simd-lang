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
	if rp == nil || rp.tpkg == nil {
		giveUp("package runtime is not loaded")
	}
	fn, ok := rp.tpkg.Scope().Lookup(name).(*types.Func)
	if !ok {
		giveUp("runtime.%s is not a function", name)
	}
	return fn
}

func (c *compiler) staticSym(kind string) *obj.LSym {
	c.mu.Lock()
	c.tmpN++
	n := c.tmpN
	c.mu.Unlock()
	return c.ctxt.LookupStatic(objabi.PathToPrefix(c.path) + "..fc" + kind + "." + itoa(n))
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
		case "getcallerpc", "getcallersp", "getclosureptr", "KeepAlive":
			return fn.Name() != "KeepAlive"
		}
	case "internal/runtime/sys":
		switch fn.Name() {
		case "GetCallerPC", "GetCallerSP", "GetClosurePtr":
			return true
		}
	}
	return false
}

// findHeapVars marks the local variables that must live on the heap: their address is taken (explicitly,
// by a method call with a pointer receiver, or by slicing an array), or a closure captures them.
func (g *gen) findHeapVars(body ast.Node) {
	info := g.info
	var root func(e ast.Expr) *types.Var
	root = func(e ast.Expr) *types.Var {
		switch e := e.(type) {
		case *ast.Ident:
			if v, ok := info.Uses[e].(*types.Var); ok && !v.IsField() {
				return v
			}
			if v, ok := info.Defs[e].(*types.Var); ok && !v.IsField() {
				return v
			}
		case *ast.ParenExpr:
			return root(e.X)
		case *ast.SelectorExpr:
			if sel := info.Selections[e]; sel != nil && sel.Kind() == types.FieldVal {
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
	mark := func(v *types.Var) {
		if v != nil && v.Pkg() != nil && v.Parent() != v.Pkg().Scope() {
			g.heapVars[v] = true
		}
	}
	var lits []*ast.FuncLit
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
			if sel := info.Selections[n]; sel != nil && sel.Kind() == types.MethodVal {
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
		case *ast.FuncLit:
			lits = append(lits, n)
		}
		return true
	})
	for _, lit := range lits {
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if v, ok := info.Uses[id].(*types.Var); ok && !v.IsField() {
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
					if !strings.HasPrefix(cm.Text, "//go:linkname ") {
						continue
					}
					fs := strings.Fields(cm.Text)
					if len(fs) == 3 {
						p.linkname[fs[1]] = fs[2]
					}
				}
			}
		}
	})
	return p.linkname
}

// ---- not written yet

func (g *gen) funcValue(fn *types.Func) loc {
	if g.c.isIntrinsic(fn) {
		giveUp("compiler intrinsic %s as a value", fn.FullName())
	}
	if sig := fn.Type().(*types.Signature); sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
		giveUp("generic function value")
	}
	target := g.c.funcSym(fn)
	s := g.c.lookup(target.Name+"·f", obj.ABI0, target.Pkg)
	g.c.mu.Lock()
	if !s.OnList() {
		s.WriteAddr(g.c.ctxt, 0, 8, target, 0)
		s.Type = objabi.SRODATA
		s.Size = 8
		s.Set(obj.AttrDuplicateOK, true)
		s.Set(obj.AttrOnList, true)
		g.c.ctxt.Data = append(g.c.ctxt.Data, s)
	}
	g.c.mu.Unlock()
	g.a.lea(AX, msym(s, 0))
	return loc{kind: lReg, reg: AX}
}

func (g *gen) methodValue(e *ast.SelectorExpr, sel *types.Selection) loc {
	giveUp("method value")
	return loc{}
}
func (g *gen) methodExpr(e *ast.SelectorExpr, sel *types.Selection) loc {
	giveUp("method expression")
	return loc{}
}
func (g *gen) mapIndex(e *ast.IndexExpr, commaOK bool) []loc { giveUp("map index"); return nil }
func (g *gen) mapOperands(e *ast.IndexExpr) (loc, loc)       { giveUp("map index"); return loc{}, loc{} }
func (g *gen) mapIndexLocs(e *ast.IndexExpr, m, k loc, commaOK bool) []loc {
	giveUp("map index")
	return nil
}
func (g *gen) mapAssign(e *ast.IndexExpr) loc                  { giveUp("map assignment"); return loc{} }
func (g *gen) mapAssignLocs(e *ast.IndexExpr, m, k loc) loc    { giveUp("map assignment"); return loc{} }
func (g *gen) mapDelete(e *ast.CallExpr)                       { giveUp("delete") }
func (g *gen) clearCall(e *ast.CallExpr)                       { giveUp("clear") }
func (g *gen) printCall(e *ast.CallExpr, nl bool)              { giveUp("print") }
func (g *gen) recoverCall(e *ast.CallExpr) loc                 { giveUp("recover"); return loc{} }
func (g *gen) funcLit(e *ast.FuncLit) loc                      { giveUp("function literal"); return loc{} }
func (g *gen) typeAssert(e *ast.TypeAssertExpr, ok bool) []loc { giveUp("type assertion"); return nil }
func (g *gen) recv(e *ast.UnaryExpr, ok bool) []loc            { giveUp("channel receive"); return nil }
func (g *gen) ifaceToIface(l loc, from, to types.Type) loc {
	giveUp("interface conversion")
	return loc{}
}
func (g *gen) typeSwitch(s *ast.TypeSwitchStmt, name string) { giveUp("type switch") }
func (g *gen) selectStmt(s *ast.SelectStmt, name string)     { giveUp("select") }
func (g *gen) deferStmt(s *ast.DeferStmt)                    { giveUp("defer") }
func (g *gen) goStmt(s *ast.GoStmt)                          { giveUp("go statement") }
func (g *gen) send(s *ast.SendStmt)                          { giveUp("channel send") }
func (g *gen) rangeMap(s *ast.RangeStmt, name string, set func(ast.Expr, *types.Var, loc, types.Type)) {
	giveUp("range over a map")
}
func (g *gen) rangeChan(s *ast.RangeStmt, name string, set func(ast.Expr, *types.Var, loc, types.Type)) {
	giveUp("range over a channel")
}
