// chans.go: channel operations and select.
package main

import (
	"go/ast"
	"go/token"
	"go/types"
)

func (g *gen) send(s *ast.SendStmt) {
	ct := g.typeOf(s.Chan)
	et := ct.Underlying().(*types.Chan).Elem()
	cl := g.keep(g.expr(s.Chan), ct)
	v := g.keep(g.inMem(g.exprAs(s.Value, et), et), et)
	v = g.inMem(v, et)
	g.addrTo(BX, v)
	g.loadTo(AX, cl, ct)
	g.callRT("chansend1")
}

// recv: <-c, or v, ok := <-c.
func (g *gen) recv(e *ast.UnaryExpr, commaOK bool) []loc {
	ct := g.typeOf(e.X)
	et := ct.Underlying().(*types.Chan).Elem()
	cl := g.keep(g.expr(e.X), ct)
	return g.recvLoc(cl, ct, et, commaOK)
}

func (g *gen) recvLoc(cl loc, ct, et types.Type, commaOK bool) []loc {
	res := g.temp(et)
	g.zero(res, et)
	if g.c.sizeof(et) == 0 {
		g.a.movImm(BX, 0)
	} else {
		g.addrTo(BX, res)
	}
	g.loadTo(AX, cl, ct)
	if !commaOK {
		g.callRT("chanrecv1")
		return []loc{res}
	}
	g.callRT("chanrecv2")
	okl := g.wordTemp()
	g.a.extend(AX, 1, false)
	g.a.store(mb(BP, okl.off), AX, 8)
	return []loc{res, okl}
}

func (g *gen) rangeChan(s *ast.RangeStmt, name string, set func(ast.Expr, *types.Var, loc, types.Type)) {
	ct := g.typeOf(s.X)
	et := ct.Underlying().(*types.Chan).Elem()
	cl := g.keep(g.expr(s.X), ct)
	top, done := g.newLabel(), g.newLabel()
	g.place(top)
	m := g.mark()
	rs := g.recvLoc(cl, ct, et, true)
	g.a.aluMI(aCMP, 8, mb(BP, rs[1].off), 0)
	g.jcc(ccE, done)
	set(s.Key, g.rangeVar(s.Key), rs[0], et)
	g.push(done, top, name)
	g.block(s.Body.List)
	g.popScope()
	g.release(m)
	g.jmp(top)
	g.place(done)
}

func (g *gen) selectStmt(s *ast.SelectStmt, name string) {
	clauses := s.Body.List
	if len(clauses) == 0 {
		g.callRT("block")
		g.a.ud2()
		return
	}
	type scase struct {
		cc     *ast.CommClause
		send   bool
		ch     loc
		ct, et types.Type
		elem   loc // the value to send, or where the received one goes
		idx    int // position in the runtime's array: sends first
	}
	var cases []*scase
	var deflt *ast.CommClause
	// evaluate channels and send values in source order
	for _, cl := range clauses {
		cc := cl.(*ast.CommClause)
		if cc.Comm == nil {
			deflt = cc
			continue
		}
		sc := &scase{cc: cc}
		switch st := cc.Comm.(type) {
		case *ast.SendStmt:
			sc.send = true
			sc.ct = g.typeOf(st.Chan)
			sc.et = sc.ct.Underlying().(*types.Chan).Elem()
			sc.ch = g.keep(g.expr(st.Chan), sc.ct)
			sc.elem = g.inMem(g.keep(g.inMem(g.exprAs(st.Value, sc.et), sc.et), sc.et), sc.et)
		default:
			var rx ast.Expr
			switch st := st.(type) {
			case *ast.ExprStmt:
				rx = st.X
			case *ast.AssignStmt:
				rx = st.Rhs[0]
			}
			ue := ast.Unparen(rx).(*ast.UnaryExpr)
			sc.ct = g.typeOf(ue.X)
			sc.et = sc.ct.Underlying().(*types.Chan).Elem()
			sc.ch = g.keep(g.expr(ue.X), sc.ct)
			sc.elem = g.temp(sc.et)
			g.zero(sc.elem, sc.et)
		}
		cases = append(cases, sc)
	}
	n := len(cases)
	nsend := 0
	for _, sc := range cases {
		if sc.send {
			sc.idx = nsend
			nsend++
		}
	}
	nrecv := 0
	for _, sc := range cases {
		if !sc.send {
			sc.idx = nsend + nrecv
			nrecv++
		}
	}
	chosen := g.wordTemp()
	recvOK := g.wordTemp()
	if n == 0 {
		g.a.storeImm(mb(BP, chosen.off), -1, 8)
	} else if n == 1 {
		// One communication: a plain send or receive, or with a default clause the runtime's non-blocking
		// send or receive, as the stock compiler does (walk/select.go). Not only cheaper than selectgo: the
		// runtime counts on it. time.sendTime's select runs inside the timer's own send (unlockAndRun), and
		// selectgo there looks at the channel's timer again, which for a ticker is due again: the timer
		// waited on the lock it held.
		sc := cases[0]
		elemTo := func(r int) {
			if g.c.sizeof(sc.et) == 0 {
				g.a.movImm(r, 0)
			} else {
				g.addrTo(r, sc.elem)
			}
		}
		g.a.storeImm(mb(BP, recvOK.off), 0, 8)
		switch {
		case sc.send && deflt == nil:
			elemTo(BX)
			g.loadTo(AX, sc.ch, sc.ct)
			g.callRT("chansend1")
			g.a.movImm(AX, 1)
		case sc.send:
			elemTo(BX)
			g.loadTo(AX, sc.ch, sc.ct)
			g.callRT("selectnbsend")
		case deflt == nil:
			elemTo(BX)
			g.loadTo(AX, sc.ch, sc.ct)
			g.callRT("chanrecv2")
			g.a.extend(AX, 1, false)
			g.a.store(mb(BP, recvOK.off), AX, 8)
			g.a.movImm(AX, 1)
		default:
			elemTo(AX)
			g.loadTo(BX, sc.ch, sc.ct)
			g.callRT("selectnbrecv")
			g.a.extend(BX, 1, false)
			g.a.store(mb(BP, recvOK.off), BX, 8)
		}
		// chosen: case 0 if selected, else -1 (the default clause)
		g.a.extend(AX, 1, false)
		g.a.aluImm(aSUB, 8, AX, 1)
		g.a.store(mb(BP, chosen.off), AX, 8)
	} else {
		// the scase array (channel, element pointer) and the order array (2n uint16)
		ptrs := make([]bool, 2*n)
		for i := range ptrs {
			ptrs[i] = true
		}
		ci := g.newSlotBits(int64(16*n), ptrs)
		g.temps = append(g.temps, ci)
		casesOff := -g.slots[ci].off
		oi := g.newSlotBits(int64(4*n), make([]bool, (4*n+7)/8))
		g.temps = append(g.temps, oi)
		orderOff := -g.slots[oi].off
		for _, sc := range cases {
			g.loadTo(R8, sc.ch, sc.ct)
			g.a.store(mb(BP, casesOff+int64(16*sc.idx)), R8, 8)
			if g.c.sizeof(sc.et) == 0 {
				g.a.movImm(R8, 0)
			} else {
				g.addrTo(R8, sc.elem)
			}
			g.a.store(mb(BP, casesOff+int64(16*sc.idx)+8), R8, 8)
		}
		g.a.lea(AX, mb(BP, casesOff))
		g.a.lea(BX, mb(BP, orderOff))
		g.a.movImm(CX, 0)
		g.a.movImm(DI, int64(nsend))
		g.a.movImm(SI, int64(nrecv))
		if deflt != nil {
			g.a.movImm(R8, 0)
		} else {
			g.a.movImm(R8, 1)
		}
		g.callRT("selectgo")
		g.a.store(mb(BP, chosen.off), AX, 8)
		g.a.extend(BX, 1, false)
		g.a.store(mb(BP, recvOK.off), BX, 8)
	}
	done := g.newLabel()
	g.push(done, nil, name)
	for _, sc := range cases {
		next := g.newLabel()
		g.a.aluMI(aCMP, 8, mb(BP, chosen.off), int64(sc.idx))
		g.jcc(ccNE, next)
		if as, ok := sc.cc.Comm.(*ast.AssignStmt); ok {
			// v [, ok] = <-c  or  v [, ok] := <-c
			vals := []typedLoc{{sc.elem, sc.et}, {recvOK, types.Typ[types.Bool]}}
			for i, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
					continue
				}
				if as.Tok == token.DEFINE {
					v := g.info.Defs[lhs.(*ast.Ident)].(*types.Var)
					g.move(g.declareVar(v), g.convertImplicit(vals[i].l, vals[i].t, g.vt(v)), g.vt(v))
					continue
				}
				dt := g.typeOf(lhs)
				m := g.mark()
				if g.isMapIndex(lhs) {
					ix := ast.Unparen(lhs).(*ast.IndexExpr)
					ml, kl := g.mapOperands(ix)
					g.move(g.mapAssignLocs(ix, ml, kl), g.convertImplicit(vals[i].l, vals[i].t, dt), dt)
				} else {
					dst := g.lvalue(lhs)
					g.move(dst, g.convertImplicit(vals[i].l, vals[i].t, dt), dt)
				}
				g.release(m)
			}
		}
		g.block(sc.cc.Body)
		g.jmp(done)
		g.place(next)
	}
	if deflt != nil {
		g.block(deflt.Body)
	}
	g.popScope()
	g.place(done)
}
