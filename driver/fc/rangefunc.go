// rangefunc.go: for ... := range f, where f is an iterator function. The loop body becomes a function
// that f calls for each value; it returns false to stop. Control that leaves the body (break, continue to
// an outer loop, return) is recorded in a state word and carried out after f returns.
package main

import (
	"go/ast"
	"go/token"
	"go/types"
)

type rfTarget struct {
	tok   token.Token
	label string
}

type rfCtx struct {
	state   *types.Var // 0 running or finished, 1 break, 2 return, 3+k: targets[k]
	targets []rfTarget
}

// leaveBody ends the current call of a loop body with the given state: the iterator is told to stop.
func (g *gen) leaveBody(state int64) {
	g.a.storeImm(g.m(g.vars[g.rf.state], R13), state, 8)
	g.a.storeImm(g.m(g.resultDst(0), R13), 0, 1)
	g.jmp(g.ret)
}

// returnJump: leave the function (the results are set).
func (g *gen) returnJump() {
	if g.rf != nil {
		g.leaveBody(2)
		return
	}
	g.jmp(g.ret)
}

func (g *gen) rangeFunc(s *ast.RangeStmt, name string) {
	ft := g.typeOf(s.X)
	xsig := ft.Underlying().(*types.Signature)
	ysig := xsig.Params().At(0).Type().Underlying().(*types.Signature)
	fl := g.keep(g.expr(s.X), ft)
	root := g
	if g.rf != nil {
		root = g.root
	}
	rf := &rfCtx{state: types.NewVar(s.Pos(), nil, "", types.Typ[types.Int])}
	g.heapVars[rf.state] = true
	g.declareVar(rf.state)
	extra := []*types.Var{rf.state}
	for _, v := range root.resVars {
		if _, ok := g.vars[v]; ok {
			extra = append(extra, v)
		}
	}
	if g.rf != nil {
		extra = append(extra, g.rf.state)
	}
	define := s.Tok == token.DEFINE
	clo := g.closure(ysig, s.Body, s.Body, extra, func(cg *gen) {
		cg.rf = rf
		cg.root = root
		for i, e := range []ast.Expr{s.Key, s.Value} {
			if e == nil || i >= len(cg.abi.in) {
				continue
			}
			if id, ok := e.(*ast.Ident); ok && id.Name == "_" {
				continue
			}
			src := cg.paramLoc(cg.abi.in[i])
			st := cg.abi.in[i].typ
			if define {
				v := cg.info.Defs[e.(*ast.Ident)].(*types.Var)
				cg.move(cg.declareVar(v), cg.convertImplicit(src, st, cg.vt(v)), cg.vt(v))
				continue
			}
			m := cg.mark()
			dt := cg.typeOf(e)
			if cg.isMapIndex(e) {
				ix := ast.Unparen(e).(*ast.IndexExpr)
				ml, kl := cg.mapOperands(ix)
				cg.move(cg.mapAssignLocs(ix, ml, kl), cg.convertImplicit(src, st, dt), dt)
			} else {
				cg.move(cg.lvalue(e), cg.convertImplicit(src, st, dt), dt)
			}
			cg.release(m)
		}
		brk, cont := cg.newLabel(), cg.newLabel()
		cg.push(brk, cont, name)
		cg.block(s.Body.List)
		cg.popScope()
		cg.place(cont)
		cg.a.storeImm(cg.m(cg.resultDst(0), R13), 1, 1)
		cg.jmp(cg.ret)
		cg.place(brk)
		cg.leaveBody(1)
	})
	clo = g.stash(clo, unsafePtr)
	g.emitCall(g.c.abiOf(xsig), []loc{clo}, callee{kind: 1, clo: fl})
	// what did the body ask for?
	done := g.newLabel()
	sm := g.m(g.vars[rf.state], R13)
	g.a.load(AX, sm, 8, false)
	st := g.wordTemp()
	g.a.store(mb(BP, st.off), AX, 8)
	g.a.aluImm(aCMP, 8, AX, 1)
	g.jcc(ccBE, done)
	g.a.aluImm(aCMP, 8, AX, 2)
	notRet := g.newLabel()
	g.jcc(ccNE, notRet)
	g.returnJump()
	g.place(notRet)
	for k, t := range rf.targets {
		next := g.newLabel()
		g.a.aluMI(aCMP, 8, mb(BP, st.off), int64(3+k))
		g.jcc(ccNE, next)
		bs := &ast.BranchStmt{Tok: t.tok}
		if t.label != "" {
			bs.Label = ast.NewIdent(t.label)
		}
		g.branch(bs)
		g.place(next)
	}
	g.place(done)
}
