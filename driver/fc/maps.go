// maps.go: map operations, through the runtime's generic entry points (keys are passed by address).
package main

import (
	"go/ast"
	"go/types"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

func (g *gen) mapType(e ast.Expr) *types.Map { return g.typeOf(e).Underlying().(*types.Map) }

// mapOperands evaluates the map and the key of m[k]; both are safe from later evaluation and the key is in
// memory.
func (g *gen) mapOperands(e *ast.IndexExpr) (ml, kl loc) {
	mt := g.typeOf(e.X)
	kt := mt.Underlying().(*types.Map).Key()
	ml = g.keep(g.expr(e.X), mt)
	kl = g.keep(g.inMem(g.exprAs(e.Index, kt), kt), kt)
	if !kl.isMem() {
		kl = g.inMem(kl, kt)
	}
	return
}

// mapArgs loads the registers for a runtime map call: AX = map type, BX = map, CX = &key.
func (g *gen) mapArgs(mt types.Type, ml, kl loc) {
	g.addrTo(CX, kl)
	g.loadTo(BX, ml, mt)
	g.a.lea(AX, msym(g.c.typeSym(mt), 0))
}

func (g *gen) mapIndex(e *ast.IndexExpr, commaOK bool) []loc {
	ml, kl := g.mapOperands(e)
	return g.mapIndexLocs(e, ml, kl, commaOK)
}

func (g *gen) mapIndexLocs(e *ast.IndexExpr, ml, kl loc, commaOK bool) []loc {
	mt := g.typeOf(e.X)
	et := mt.Underlying().(*types.Map).Elem()
	fat := g.c.sizeof(et) > 1024 // larger than the runtime's own zero value: pass one
	g.mapArgs(mt, ml, kl)
	if fat {
		g.a.lea(DI, msym(g.c.zeroSym(g.c.sizeof(et)), 0))
	}
	if !commaOK {
		if fat {
			g.callRT("mapaccess1_fat")
		} else {
			g.callRT("mapaccess1")
		}
		return []loc{{kind: lRegInd, reg: AX}}
	}
	if fat {
		g.callRT("mapaccess2_fat")
	} else {
		g.callRT("mapaccess2")
	}
	p := g.ptrTemp()
	g.a.store(mb(BP, p.off), AX, 8)
	okl := g.wordTemp()
	g.a.extend(BX, 1, false)
	g.a.store(mb(BP, okl.off), BX, 8)
	return []loc{{kind: lInd, slot: p.off}, okl}
}

func (g *gen) mapAssign(e *ast.IndexExpr) loc {
	ml, kl := g.mapOperands(e)
	return g.stash(g.mapAssignLocs(e, ml, kl), g.typeOf(e))
}

// mapAssignLocs: the element slot for m[k], created if absent. Valid until the next map operation.
func (g *gen) mapAssignLocs(e *ast.IndexExpr, ml, kl loc) loc {
	mt := g.typeOf(e.X)
	g.mapArgs(mt, ml, kl)
	g.callRT("mapassign")
	return loc{kind: lRegInd, reg: AX}
}

func (g *gen) mapDelete(e *ast.CallExpr) {
	mt := g.typeOf(e.Args[0])
	kt := mt.Underlying().(*types.Map).Key()
	ml := g.keep(g.expr(e.Args[0]), mt)
	kl := g.inMem(g.keep(g.inMem(g.exprAs(e.Args[1], kt), kt), kt), kt)
	g.mapArgs(mt, ml, kl)
	g.callRT("mapdelete")
}

func (g *gen) clearCall(e *ast.CallExpr) {
	g.clearLoc(g.expr(e.Args[0]), g.typeOf(e.Args[0]))
}

func (g *gen) clearLoc(l loc, t types.Type) {
	switch u := t.Underlying().(type) {
	case *types.Map:
		g.loadTo(BX, l, t)
		g.a.lea(AX, msym(g.c.typeSym(t), 0))
		g.callRT("mapclear")
	case *types.Slice:
		if l.kind == lZero {
			return
		}
		es := g.c.sizeof(u.Elem())
		l = g.stash(l, t)
		m := g.m(l, R13)
		g.a.load(BX, m.plus(8), 8, false)
		g.a.load(AX, m, 8, false)
		if es != 1 {
			g.a.imulImm(BX, BX, int32(es))
		}
		if g.c.hasPointers(u.Elem()) {
			g.callRT("memclrHasPointers")
		} else {
			g.callRT("memclrNoHeapPointers")
		}
	default:
		giveUp("clear of %v", t)
	}
}

func (g *gen) mapLit(e *ast.CompositeLit, dst loc, t types.Type) {
	mt := t.Underlying().(*types.Map)
	g.a.lea(AX, msym(g.c.typeSym(t), 0))
	g.a.movImm(BX, int64(len(e.Elts)))
	g.a.movImm(CX, 0)
	g.callRT("makemap")
	mp := g.ptrTemp()
	g.a.store(mb(BP, mp.off), AX, 8)
	// entries whose key and value are constants go first, as the stock compiler orders them (it matters
	// when a later, computed key equals a constant one)
	var elts []ast.Expr
	for _, pass := range []bool{true, false} {
		for _, el := range e.Elts {
			kv := el.(*ast.KeyValueExpr)
			if (g.info.Types[kv.Key].Value != nil && g.info.Types[kv.Value].Value != nil) == pass {
				elts = append(elts, el)
			}
		}
	}
	for _, el := range elts {
		kv := el.(*ast.KeyValueExpr)
		m := g.mark()
		kt, et := mt.Key(), mt.Elem()
		ktmp := g.temp(kt)
		g.elemTo(kv.Key, ktmp, kt)
		vtmp := g.temp(et)
		g.elemTo(kv.Value, vtmp, et)
		g.mapArgs(t, mp, ktmp)
		g.callRT("mapassign")
		g.move(loc{kind: lRegInd, reg: AX}, vtmp, et)
		g.release(m)
	}
	g.move(dst, mp, t)
}

func (g *gen) rangeMap(s *ast.RangeStmt, name string, set func(ast.Expr, *types.Var, loc, types.Type)) {
	mt := g.typeOf(s.X)
	u := mt.Underlying().(*types.Map)
	ml := g.keep(g.expr(s.X), mt)
	// the iterator lives on the heap (its type is internal/runtime/maps.Iter; key and elem pointers are its
	// first two words, and a nil key means the iteration is over)
	iterPkg := g.c.pr.byPath["internal/runtime/maps"]
	if iterPkg != nil && iterPkg != g.c.pkg {
		g.c.pr.check(iterPkg)
	}
	if iterPkg == nil || iterPkg.tpkg == nil {
		giveUp("internal/runtime/maps is not loaded")
	}
	iterT := iterPkg.tpkg.Scope().Lookup("Iter").Type()
	g.a.lea(AX, msym(g.c.typeSym(iterT), 0))
	g.callRT("newobject")
	it := g.ptrTemp()
	g.a.store(mb(BP, it.off), AX, 8)
	g.a.movRR(CX, AX)
	g.loadTo(BX, ml, mt)
	g.a.lea(AX, msym(g.c.typeSym(mt), 0))
	g.callRT("mapIterStart")
	top, cont, done := g.newLabel(), g.newLabel(), g.newLabel()
	g.place(top)
	g.a.load(AX, mb(BP, it.off), 8, false)
	g.a.load(CX, mb(AX, 0), 8, false)
	g.a.testRR(8, CX, CX)
	g.jcc(ccE, done)
	kp, ep := g.ptrTemp(), g.ptrTemp()
	g.a.store(mb(BP, kp.off), CX, 8)
	g.a.load(CX, mb(AX, 8), 8, false)
	g.a.store(mb(BP, ep.off), CX, 8)
	set(s.Key, g.rangeVar(s.Key), loc{kind: lInd, slot: kp.off}, u.Key())
	set(s.Value, g.rangeVar(s.Value), loc{kind: lInd, slot: ep.off}, u.Elem())
	g.push(done, cont, name)
	g.block(s.Body.List)
	g.popScope()
	g.place(cont)
	g.a.load(AX, mb(BP, it.off), 8, false)
	g.callRT("mapIterNext")
	g.jmp(top)
	g.place(done)
}

// rangeVar: the variable a range clause declares for e, if it declares one.
func (g *gen) rangeVar(e ast.Expr) *types.Var {
	if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
		v, _ := g.info.Defs[id].(*types.Var)
		return v
	}
	return nil
}

// zeroSym: read-only zeros, at least size bytes (the value of a missing map entry that is larger than the
// runtime's own zero value).
func (c *compiler) zeroSym(size int64) *obj.LSym {
	size = align(size, 4096)
	c.mu.Lock()
	if c.zeroSyms == nil {
		c.zeroSyms = map[int64]*obj.LSym{}
	}
	s := c.zeroSyms[size]
	c.mu.Unlock()
	if s == nil {
		s = c.staticSym("zero")
		c.defData(s, objabi.SNOPTRBSS, size)
		c.mu.Lock()
		c.zeroSyms[size] = s
		c.mu.Unlock()
	}
	return s
}
