// iface.go: type assertions, type switches, conversions between interface types.
package main

import (
	"go/ast"
	"go/types"
)

func isEmptyIface(t types.Type) bool {
	it, ok := t.Underlying().(*types.Interface)
	return ok && it.NumMethods() == 0
}

// dynType: AX = the dynamic type of the interface value at l (static type t), or 0 for a nil interface.
func (g *gen) dynType(l loc, t types.Type) {
	g.a.load(AX, g.m(l, R13), 8, false)
	if !isEmptyIface(t) {
		g.a.testRR(8, AX, AX)
		z := g.a.jcc(ccE)
		g.a.load(AX, mb(AX, 8), 8, false)
		g.a.patch(z, g.a.pc())
	}
}

// ifaceValue: the value of concrete type t held by the interface value at l (already known to hold a t).
func (g *gen) ifaceValue(l loc, t types.Type) loc {
	if g.c.sizeof(t) == 0 {
		return loc{kind: lZero}
	}
	if isDirectIface(t) {
		return l.plus(8)
	}
	g.a.load(R13, g.m(l, R13).plus(8), 8, false)
	p := g.ptrTemp()
	g.a.store(mb(BP, p.off), R13, 8)
	return loc{kind: lInd, slot: p.off}
}

// typeAssert: x.(T), or v, ok := x.(T).
func (g *gen) typeAssert(e *ast.TypeAssertExpr, commaOK bool) []loc {
	xt := g.typeOf(e.X)
	t := g.typeOf(e.Type)
	x := g.keep(g.inMem(g.expr(e.X), xt), xt)
	if x.kind != lFrame {
		x = g.zeroTemp(xt)
	}
	boolT := types.Typ[types.Bool]
	if isInterface(t) {
		res := g.temp(t)
		g.dynType(x, xt)
		if isEmptyIface(t) {
			g.a.store(mb(BP, res.off), AX, 8)
			if !commaOK {
				g.a.testRR(8, AX, AX)
				ok := g.a.jcc(ccNE)
				g.a.lea(AX, msym(g.c.typeSym(t), 0))
				g.callRT("panicnildottype")
				g.a.ud2()
				g.a.patch(ok, g.a.pc())
			}
		} else {
			g.a.movRR(BX, AX)
			g.a.lea(AX, msym(g.c.typeSym(t), 0))
			if commaOK {
				g.callRT("assertE2I2")
			} else {
				g.callRT("assertE2I")
			}
			g.a.store(mb(BP, res.off), AX, 8)
		}
		// the data word goes along only if the assertion held
		g.a.load(CX, mb(BP, res.off), 8, false)
		g.a.testRR(8, CX, CX)
		g.a.movImm(R8, 0)
		z := g.a.jcc(ccE)
		g.a.load(R8, mb(BP, x.off+8), 8, false)
		g.a.patch(z, g.a.pc())
		g.a.store(mb(BP, res.off+8), R8, 8)
		if !commaOK {
			return []loc{res}
		}
		okl := g.wordTemp()
		g.a.testRR(8, CX, CX)
		g.a.setcc(ccNE, AX)
		g.a.extend(AX, 1, false)
		g.a.store(mb(BP, okl.off), AX, 8)
		_ = boolT
		return []loc{res, okl}
	}
	// a concrete type: compare the dynamic type with T's descriptor
	tsym := g.c.typeSym(t)
	g.dynType(x, xt)
	g.a.lea(CX, msym(tsym, 0))
	g.a.aluRR(aCMP, 8, AX, CX)
	if !commaOK {
		ok := g.a.jcc(ccE)
		// panicdottypeE(have, want, iface) / panicdottypeI(have itab, want, iface)
		g.a.movRR(BX, CX)
		g.a.lea(CX, msym(g.c.typeSym(xt), 0))
		if isEmptyIface(xt) {
			g.callRT("panicdottypeE")
		} else {
			g.a.load(AX, mb(BP, x.off), 8, false)
			g.callRT("panicdottypeI")
		}
		g.a.ud2()
		g.a.patch(ok, g.a.pc())
		return []loc{g.ifaceValue(x, t)}
	}
	res := g.temp(t)
	okl := g.wordTemp()
	no := g.a.jcc(ccNE)
	m := g.mark()
	g.move(res, g.ifaceValue(x, t), t)
	g.release(m)
	g.a.storeImm(mb(BP, okl.off), 1, 8)
	done := g.a.jmp()
	g.a.patch(no, g.a.pc())
	g.zero(res, t)
	g.a.storeImm(mb(BP, okl.off), 0, 8)
	g.a.patch(done, g.a.pc())
	if g.c.sizeof(t) == 0 {
		return []loc{{kind: lZero}, okl}
	}
	return []loc{res, okl}
}

// ifaceToIface converts an interface value to another interface type (always possible, or checked by the
// type checker): a nil stays nil.
func (g *gen) ifaceToIface(l loc, from, to types.Type) loc {
	if l.kind == lZero {
		return l
	}
	x := g.keep(g.inMem(l, from), from)
	res := g.temp(to)
	g.dynType(x, from)
	if isEmptyIface(to) {
		g.a.store(mb(BP, res.off), AX, 8)
	} else {
		g.a.testRR(8, AX, AX)
		z := g.a.jcc(ccE)
		g.a.movRR(BX, AX)
		g.a.lea(AX, msym(g.c.typeSym(to), 0))
		g.callRT("assertE2I")
		g.a.patch(z, g.a.pc())
		g.a.store(mb(BP, res.off), AX, 8)
	}
	g.a.load(R8, g.m(x, R13).plus(8), 8, false)
	g.a.store(mb(BP, res.off+8), R8, 8)
	return res
}

func (g *gen) typeSwitch(s *ast.TypeSwitchStmt, name string) {
	if s.Init != nil {
		g.stmt(s.Init)
	}
	// the operand: x.(type), possibly in v := x.(type)
	var ta *ast.TypeAssertExpr
	switch a := s.Assign.(type) {
	case *ast.ExprStmt:
		ta = ast.Unparen(a.X).(*ast.TypeAssertExpr)
	case *ast.AssignStmt:
		ta = ast.Unparen(a.Rhs[0]).(*ast.TypeAssertExpr)
	}
	xt := g.typeOf(ta.X)
	x := g.keep(g.inMem(g.expr(ta.X), xt), xt)
	if x.kind != lFrame {
		x = g.zeroTemp(xt)
	}
	dyn := g.wordTemp() // the dynamic type
	g.dynType(x, xt)
	g.a.store(mb(BP, dyn.off), AX, 8)
	itab := g.ptrTemp() // the itab found by the last interface case tested
	clauses := s.Body.List
	bodies := make([]*label, len(clauses))
	done := g.newLabel()
	var deflt *label
	for i, cl := range clauses {
		cc := cl.(*ast.CaseClause)
		bodies[i] = g.newLabel()
		if cc.List == nil {
			deflt = bodies[i]
			continue
		}
		for _, te := range cc.List {
			if g.info.Types[te].IsNil() {
				g.a.aluMI(aCMP, 8, mb(BP, dyn.off), 0)
				g.jcc(ccE, bodies[i])
				continue
			}
			t := g.typeOf(te)
			switch {
			case isEmptyIface(t):
				g.a.aluMI(aCMP, 8, mb(BP, dyn.off), 0)
				g.jcc(ccNE, bodies[i])
			case isInterface(t):
				g.a.lea(AX, msym(g.c.typeSym(t), 0))
				g.a.load(BX, mb(BP, dyn.off), 8, false)
				g.callRT("assertE2I2")
				g.a.store(mb(BP, itab.off), AX, 8)
				g.a.testRR(8, AX, AX)
				g.jcc(ccNE, bodies[i])
			default:
				g.a.lea(AX, msym(g.c.typeSym(t), 0))
				g.a.aluRM(aCMP, 8, AX, mb(BP, dyn.off))
				g.jcc(ccE, bodies[i])
			}
		}
	}
	if deflt != nil {
		g.jmp(deflt)
	} else {
		g.jmp(done)
	}
	g.push(done, nil, name)
	saved := g.fallthru
	g.fallthru = nil
	for i, cl := range clauses {
		cc := cl.(*ast.CaseClause)
		g.place(bodies[i])
		if v, ok := g.info.Implicits[cc].(*types.Var); ok {
			m := g.mark()
			vt := g.vt(v)
			var val loc
			switch {
			case len(cc.List) == 1 && !g.info.Types[cc.List[0]].IsNil() && !isInterface(vt):
				val = g.ifaceValue(x, vt)
			case len(cc.List) == 1 && !g.info.Types[cc.List[0]].IsNil() && isEmptyIface(vt):
				tmp := g.temp(vt)
				g.a.load(R8, mb(BP, dyn.off), 8, false)
				g.a.store(mb(BP, tmp.off), R8, 8)
				g.a.load(R8, mb(BP, x.off+8), 8, false)
				g.a.store(mb(BP, tmp.off+8), R8, 8)
				val = tmp
			case len(cc.List) == 1 && !g.info.Types[cc.List[0]].IsNil():
				tmp := g.temp(vt)
				g.a.load(R8, mb(BP, itab.off), 8, false)
				g.a.store(mb(BP, tmp.off), R8, 8)
				g.a.load(R8, mb(BP, x.off+8), 8, false)
				g.a.store(mb(BP, tmp.off+8), R8, 8)
				val = tmp
			default:
				val = x // the operand itself, with its own type
			}
			l := g.declareVar(v)
			g.move(l, val, vt)
			g.release(m)
		}
		g.block(cc.Body)
		g.jmp(done)
	}
	g.fallthru = saved
	g.popScope()
	g.place(done)
}
