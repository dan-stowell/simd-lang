// stmt.go: statements.
package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"

	"cmd/internal/objabi"
)

func (g *gen) block(list []ast.Stmt) {
	n := len(g.declSlots)
	for _, s := range list {
		g.stmt(s)
	}
	// the variables declared in the block are out of scope: their slots are free for what follows
	if os.Getenv("FC_NOSCOPE") == "" {
		for _, i := range g.declSlots[n:] {
			s := g.slots[i]
			key := shapeKey(s.size, s.ptrs)
			g.free[key] = append(g.free[key], i)
			if tidyKnob != "temps" {
				g.slotDead(i)
			}
		}
	}
	g.declSlots = g.declSlots[:n]
}

func (g *gen) stmt(s ast.Stmt) {
	mark := g.mark()
	defer func() {
		g.release(mark)
		g.dirty = g.dirty[:0] // what the statement released is dead now
	}()
	g.pos(s.Pos())
	switch s := s.(type) {
	case *ast.BlockStmt:
		g.block(s.List)
	case *ast.EmptyStmt:
	case *ast.ExprStmt:
		switch e := ast.Unparen(s.X).(type) {
		case *ast.CallExpr:
			g.call(e)
		case *ast.UnaryExpr:
			if e.Op == token.ARROW {
				g.recv(e, false)
				break
			}
			g.expr(e)
		default:
			g.expr(s.X)
		}
	case *ast.AssignStmt:
		g.assign(s)
	case *ast.IncDecStmt:
		op := token.ADD
		if s.Tok == token.DEC {
			op = token.SUB
		}
		g.opAssign(s.X, nil, op)
	case *ast.DeclStmt:
		gd := s.Decl.(*ast.GenDecl)
		if gd.Tok != token.VAR {
			return
		}
		for _, sp := range gd.Specs {
			g.varSpec(sp.(*ast.ValueSpec))
		}
	case *ast.ReturnStmt:
		g.returnStmt(s)
	case *ast.IfStmt:
		if s.Init != nil {
			g.stmt(s.Init)
		}
		// A branch a constant condition rules out is not compiled (the runtime's nosplit functions
		// count on that: their debug checks call functions that would not fit the stack bound).
		if v, known := g.staticCond(s.Cond); known {
			if v && (s.Else == nil || !hasLabel(s.Else)) {
				g.block(s.Body.List)
				break
			}
			if !v && !hasLabel(s.Body) {
				if s.Else != nil {
					g.stmt(s.Else)
				}
				break
			}
		}
		els, done := g.newLabel(), g.newLabel()
		m := g.mark()
		g.condJump(s.Cond, els)
		g.release(m)
		g.block(s.Body.List)
		if s.Else != nil {
			g.jmp(done)
			g.place(els)
			g.stmt(s.Else)
			g.place(done)
		} else {
			g.place(els)
		}
	case *ast.ForStmt:
		g.forStmt(s, "")
	case *ast.RangeStmt:
		g.rangeStmt(s, "")
	case *ast.SwitchStmt:
		g.switchStmt(s, "")
	case *ast.TypeSwitchStmt:
		g.typeSwitch(s, "")
	case *ast.SelectStmt:
		g.selectStmt(s, "")
	case *ast.LabeledStmt:
		lb := g.labelFor(g.info.Defs[s.Label].(*types.Label))
		g.place(lb)
		switch st := s.Stmt.(type) {
		case *ast.ForStmt:
			g.forStmt(st, s.Label.Name)
		case *ast.RangeStmt:
			g.rangeStmt(st, s.Label.Name)
		case *ast.SwitchStmt:
			g.switchStmt(st, s.Label.Name)
		case *ast.TypeSwitchStmt:
			g.typeSwitch(st, s.Label.Name)
		case *ast.SelectStmt:
			g.selectStmt(st, s.Label.Name)
		default:
			g.stmt(s.Stmt)
		}
	case *ast.BranchStmt:
		g.branch(s)
	case *ast.DeferStmt:
		g.deferStmt(s)
	case *ast.GoStmt:
		g.goStmt(s)
	case *ast.SendStmt:
		g.send(s)
	default:
		giveUp("statement %T", s)
	}
}

func (g *gen) labelFor(l *types.Label) *label {
	lb := g.labels[l]
	if lb == nil {
		lb = g.newLabel()
		g.labels[l] = lb
	}
	return lb
}

func (g *gen) branch(s *ast.BranchStmt) {
	switch s.Tok {
	case token.GOTO:
		lbl := g.info.Uses[s.Label].(*types.Label)
		if g.rf != nil && (lbl.Pos() < g.rf.body.Pos() || lbl.Pos() >= g.rf.body.End()) {
			// out of this loop body: the loop's function jumps
			g.rf.targets = append(g.rf.targets, rfTarget{s.Tok, s.Label.Name, lbl})
			g.leaveBody(int64(2 + len(g.rf.targets)))
			return
		}
		g.jmp(g.labelFor(lbl))
	case token.FALLTHROUGH:
		if g.fallthru == nil {
			giveUp("fallthrough outside a switch")
		}
		g.jmp(g.fallthru)
	case token.BREAK, token.CONTINUE:
		for i := len(g.scopes) - 1; i >= 0; i-- {
			sc := g.scopes[i]
			if s.Label != nil && sc.name != s.Label.Name {
				continue
			}
			if s.Tok == token.BREAK {
				g.jmp(sc.brk)
				return
			}
			if sc.cont != nil {
				g.jmp(sc.cont)
				return
			}
		}
		if g.rf != nil {
			// the target is outside this loop body: tell the loop's function which
			name := ""
			if s.Label != nil {
				name = s.Label.Name
			}
			g.rf.targets = append(g.rf.targets, rfTarget{s.Tok, name, nil})
			g.leaveBody(int64(2 + len(g.rf.targets)))
			return
		}
		giveUp("break or continue without a target")
	}
}

// declareVar gives a new local its home and, for a heap variable, its object.
func (g *gen) declareVar(v *types.Var) loc {
	l := g.declare(v)
	if g.heapVars[v] {
		g.newHeapVar(v)
	}
	return l
}

func (g *gen) varSpec(sp *ast.ValueSpec) {
	var vars []*types.Var
	for _, n := range sp.Names {
		v, _ := g.info.Defs[n].(*types.Var)
		vars = append(vars, v)
	}
	switch {
	case len(sp.Values) == 0:
		for _, v := range vars {
			if v == nil || v.Name() == "_" {
				continue
			}
			l := g.declareVar(v)
			if !g.heapVars[v] {
				g.zero(l, g.vt(v))
			}
		}
	case len(sp.Values) == len(sp.Names):
		// evaluate everything before any of the new variables exists
		var vals []loc
		for i, e := range sp.Values {
			var t types.Type
			if vars[i] != nil {
				t = g.vt(vars[i])
			} else {
				t = g.typeOf(e)
			}
			if len(sp.Values) == 1 && vars[i] != nil && vars[i].Name() != "_" {
				l := g.declareVarLate(vars[i], e, t)
				_ = l
				return
			}
			vals = append(vals, g.keep(g.exprAs(e, t), t))
		}
		for i, v := range vars {
			if v == nil || v.Name() == "_" {
				continue
			}
			g.move(g.declareVar(v), vals[i], g.vt(v))
		}
	default:
		rs := g.multi(sp.Values[0])
		for i, v := range vars {
			if v == nil || v.Name() == "_" {
				continue
			}
			g.move(g.declareVar(v), g.convertImplicit(rs[i].l, rs[i].t, g.vt(v)), g.vt(v))
		}
	}
}

// declareVarLate: v := e for one variable: e is evaluated before v exists, straight into its home when it
// is a frame variable that e cannot mention.
func (g *gen) declareVarLate(v *types.Var, e ast.Expr, t types.Type) loc {
	if g.heapVars[v] {
		val := g.keep(g.exprAs(e, t), t)
		l := g.declareVar(v)
		g.move(l, val, t)
		return l
	}
	l := g.declare(v)
	g.exprTo(e, l, t)
	return l
}

// keep makes a value safe from everything evaluated or assigned after it (a private copy unless it is a
// constant).
func (g *gen) keep(l loc, t types.Type) loc {
	switch l.kind {
	case lImm, lZero:
		return l
	case lSym:
		if l.sym.Type == objabi.SRODATA {
			return l
		}
	case lReg:
		return g.stash(l, t)
	}
	tmp := g.temp(t)
	g.move(tmp, l, t)
	return tmp
}

type typedLoc struct {
	l loc
	t types.Type
}

// multi evaluates an expression with several results: a call, or one of the comma-ok forms.
func (g *gen) multi(e ast.Expr) []typedLoc {
	e = ast.Unparen(e)
	tup, _ := g.typeOf(e).(*types.Tuple)
	var ls []loc
	switch e := e.(type) {
	case *ast.CallExpr:
		ls = g.call(e)
	case *ast.IndexExpr:
		ls = g.mapIndex(e, true)
	case *ast.TypeAssertExpr:
		ls = g.typeAssert(e, true)
	case *ast.UnaryExpr:
		ls = g.recv(e, true)
	default:
		giveUp("multi-value expression %T", e)
	}
	out := make([]typedLoc, len(ls))
	for i := range ls {
		out[i] = typedLoc{ls[i], tup.At(i).Type()}
	}
	return out
}

// lvalue: where an assignment to e stores (lNone for the blank identifier), safe across the evaluation of
// the right-hand side.
func (g *gen) lvalue(e ast.Expr) loc {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok && id.Name == "_" {
		return loc{}
	}
	if ix, ok := e.(*ast.IndexExpr); ok {
		if _, ok := g.typeOf(ix.X).Underlying().(*types.Map); ok {
			return g.mapAssign(ix)
		}
	}
	return g.stash(g.expr(e), g.typeOf(e))
}

func (g *gen) isMapIndex(e ast.Expr) bool {
	if ix, ok := ast.Unparen(e).(*ast.IndexExpr); ok {
		_, ok := g.typeOf(ix.X).Underlying().(*types.Map)
		return ok
	}
	return false
}

func (g *gen) assign(s *ast.AssignStmt) {
	switch s.Tok {
	case token.ASSIGN, token.DEFINE:
	default:
		var op token.Token
		switch s.Tok {
		case token.ADD_ASSIGN:
			op = token.ADD
		case token.SUB_ASSIGN:
			op = token.SUB
		case token.MUL_ASSIGN:
			op = token.MUL
		case token.QUO_ASSIGN:
			op = token.QUO
		case token.REM_ASSIGN:
			op = token.REM
		case token.AND_ASSIGN:
			op = token.AND
		case token.OR_ASSIGN:
			op = token.OR
		case token.XOR_ASSIGN:
			op = token.XOR
		case token.SHL_ASSIGN:
			op = token.SHL
		case token.SHR_ASSIGN:
			op = token.SHR
		case token.AND_NOT_ASSIGN:
			op = token.AND_NOT
		}
		g.opAssign(s.Lhs[0], s.Rhs[0], op)
		return
	}
	// new variables of a := are declared after the right-hand side is evaluated
	isNew := func(e ast.Expr) *types.Var {
		if s.Tok != token.DEFINE {
			return nil
		}
		if id, ok := e.(*ast.Ident); ok {
			if v, ok := g.info.Defs[id].(*types.Var); ok {
				return v
			}
		}
		return nil
	}
	lhsType := func(e ast.Expr) types.Type {
		if id, ok := e.(*ast.Ident); ok {
			if id.Name == "_" {
				return nil
			}
			if v, ok := g.info.Defs[id].(*types.Var); ok {
				return g.vt(v)
			}
		}
		return g.typeOf(e)
	}
	if len(s.Lhs) == 1 && len(s.Rhs) == 1 {
		lhs, rhs := s.Lhs[0], s.Rhs[0]
		t := lhsType(lhs)
		if t == nil {
			g.nilcheckLoc(g.expr(rhs)) // _ = e (a dereference still faults)
			return
		}
		if v := isNew(lhs); v != nil {
			g.declareVarLate(v, rhs, t)
			return
		}
		if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
			if v, ok := g.info.Uses[id].(*types.Var); ok {
				g.exprTo(rhs, g.varLoc(v), t)
				return
			}
		}
		if g.pure(rhs) {
			dst := g.lvalue(lhs)
			g.move(dst, g.exprAs(rhs, t), t)
			return
		}
		if g.isMapIndex(lhs) {
			// m[k] = v: the value is computed before the slot is found (the slot may move)
			ix := ast.Unparen(lhs).(*ast.IndexExpr)
			ml, kl := g.mapOperands(ix)
			val := g.keep(g.exprAs(rhs, t), t)
			dst := g.mapAssignLocs(ix, ml, kl)
			g.move(dst, val, t)
			return
		}
		dst := g.lvalue(lhs)
		g.move(dst, g.exprAs(rhs, t), t)
		return
	}
	// several targets: destinations, then values, then the stores
	late := make([]func() loc, len(s.Lhs))
	types_ := make([]types.Type, len(s.Lhs))
	news := make([]*types.Var, len(s.Lhs))
	mapIx := make([]*ast.IndexExpr, len(s.Lhs))
	var mapOps [][2]loc
	for i, lhs := range s.Lhs {
		types_[i] = lhsType(lhs)
		news[i] = isNew(lhs)
		if news[i] == nil && types_[i] != nil {
			if g.isMapIndex(lhs) {
				mapIx[i] = ast.Unparen(lhs).(*ast.IndexExpr)
				ml, kl := g.mapOperands(mapIx[i])
				mapOps = append(mapOps, [2]loc{ml, kl})
				continue
			}
			late[i] = g.lvalueLate(lhs)
		}
	}
	var vals []typedLoc
	if len(s.Rhs) == 1 {
		vals = g.multi(s.Rhs[0])
	} else {
		// Without calls among them the stock compiler evaluates the right-hand sides last to first
		// (observable in which of two bad indexes panics); with calls, in order.
		vals = make([]typedLoc, len(s.Rhs))
		order := make([]int, len(s.Rhs))
		calls := false
		for i, rhs := range s.Rhs {
			order[i] = len(s.Rhs) - 1 - i
			ast.Inspect(rhs, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					calls = true
				case *ast.UnaryExpr:
					calls = calls || n.Op == token.ARROW
				}
				return !calls
			})
		}
		if calls {
			for i := range order {
				order[i] = i
			}
		}
		for _, i := range order {
			rhs := s.Rhs[i]
			t := types_[i]
			if t == nil {
				t = g.typeOf(rhs)
			}
			vals[i] = typedLoc{g.keep(g.exprAs(rhs, t), t), t}
		}
	}
	nm := 0
	for i := range s.Lhs {
		t := types_[i]
		if t == nil {
			continue
		}
		v := g.convertImplicit(vals[i].l, vals[i].t, t)
		if news[i] != nil {
			if v.kind == lReg || v.kind == lRegInd {
				v = g.keep(v, t)
			}
			g.move(g.declareVar(news[i]), v, t)
			continue
		}
		if mapIx[i] != nil {
			if v.kind == lReg || v.kind == lRegInd {
				v = g.keep(v, t)
			}
			dst := g.mapAssignLocs(mapIx[i], mapOps[nm][0], mapOps[nm][1])
			nm++
			g.move(dst, v, t)
			continue
		}
		if v.kind == lReg || v.kind == lRegInd {
			v = g.keep(v, t)
		}
		g.move(late[i](), v, t)
	}
}

// lvalueLate is lvalue for an assignment with several targets: the operands are evaluated now, but an
// index is checked (and may panic) only when its store is carried out, after the earlier stores; and a
// pointer that is dereferenced is the one in force now.
func (g *gen) lvalueLate(e ast.Expr) func() loc {
	e = ast.Unparen(e)
	if ix, ok := e.(*ast.IndexExpr); ok {
		xt := g.typeOf(ix.X)
		it := g.typeOf(ix.Index)
		xl := g.indexBase(ix.X, xt)
		switch xt.Underlying().(type) {
		case *types.Array:
			xl = g.pin(xl)
		default:
			xl = g.keep(xl, xt)
		}
		il := g.expr(ix.Index)
		if il.kind != lImm {
			g.loadIndex(AX, il, it)
			il = g.wordTemp()
			g.a.store(mb(BP, il.off), AX, 8)
			if !isUnsignedIndex(it) {
				it = types.Typ[types.Int]
			} else {
				it = types.Typ[types.Uint]
			}
		}
		return func() loc { return g.indexAddr(xt, xl, il, it) }
	}
	l := g.pin(g.expr(e))
	return func() loc { return l }
}

// pin fixes the memory an lvalue names: the pointer it goes through is copied, so a later assignment to
// the pointer variable does not move it.
func (g *gen) pin(l loc) loc {
	switch l.kind {
	case lInd:
		tmp := g.ptrTemp()
		g.a.load(R13, mb(BP, l.slot), 8, false)
		g.a.store(mb(BP, tmp.off), R13, 8)
		return loc{kind: lInd, slot: tmp.off, off: l.off}
	case lRegInd:
		return g.stash(l, unsafePtr)
	}
	return l
}

// opAssign: x op= y (y nil: the constant 1).
func (g *gen) opAssign(x, y ast.Expr, op token.Token) {
	t := g.typeOf(x)
	if g.isMapIndex(x) {
		// m[k] op= v: read, compute, then assign
		ix := ast.Unparen(x).(*ast.IndexExpr)
		ml, kl := g.mapOperands(ix)
		cur := g.keep(g.mapIndexLocs(ix, ml, kl, false)[0], t)
		val := g.keep(g.opLoc(cur, y, op, t), t)
		dst := g.mapAssignLocs(ix, ml, kl)
		g.move(dst, val, t)
		return
	}
	dst := g.lvalue(x)
	v := g.opLoc(dst, y, op, t)
	g.move(dst, v, t)
}

// opLoc: the value of l op y, where l is a value of type t.
func (g *gen) opLoc(l loc, y ast.Expr, op token.Token, t types.Type) loc {
	if y == nil {
		if isFloat(t) {
			g.loadTo(X0, l, t)
			one := g.constLoc(constant.MakeInt64(1), t)
			g.loadTo(X1, one, t)
			o := byte(0x58)
			if op == token.SUB {
				o = 0x5C
			}
			g.a.farith(o, int(g.c.sizeof(t)), X0, X1)
			return loc{kind: lReg, reg: X0}
		}
		if isComplex(t) {
			return g.complexArith(g.keep(l, t), g.constLoc(constant.MakeInt64(1), t), op, t)
		}
		g.loadTo(AX, l, t)
		if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 {
			g.a.aluImm(immOp[op], 8, AX, 1)
			g.a.extend(AX, int(g.c.sizeof(t)), isSigned(t))
			return loc{kind: lReg, reg: AX}
		}
		g.a.movImm(CX, 1)
		return g.arithRegs(op, t)
	}
	switch {
	case isString(t):
		return g.concatLoc(l, y, t)
	case isComplex(t):
		xl := g.keep(l, t)
		return g.complexArith(xl, g.keep(g.exprAs(y, t), t), op, t)
	case op == token.SHL || op == token.SHR:
		yt := g.typeOf(y)
		if b, ok := yt.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			yt = types.Typ[types.Uint]
		}
		g.operandsL(l, y, t, yt, AX, CX)
		return g.shiftRegs(op, t, yt)
	case isFloat(t):
		g.operandsL(l, y, t, t, X0, X1)
		var o byte
		switch op {
		case token.ADD:
			o = 0x58
		case token.SUB:
			o = 0x5C
		case token.MUL:
			o = 0x59
		case token.QUO:
			o = 0x5E
		default:
			giveUp("float operator %v", op)
		}
		g.a.farith(o, int(g.c.sizeof(t)), X0, X1)
		return loc{kind: lReg, reg: X0}
	}
	g.operandsL(l, y, t, t, AX, CX)
	return g.arithRegs(op, t)
}

func (g *gen) resultDst(i int) loc {
	v := g.resVars[i]
	if l, ok := g.vars[v]; ok {
		return l
	}
	return g.resHome[i]
}

func (g *gen) returnStmt(s *ast.ReturnStmt) {
	// in the body of a range-over-func loop the results are the enclosing function's
	fn := g
	if g.rf != nil {
		fn = g.root
	}
	n := len(fn.resVars)
	out := fn.abi.out
	dst := func(i int) loc {
		if g.rf != nil {
			return g.vars[fn.resVars[i]]
		}
		return g.resultDst(i)
	}
	switch {
	case len(s.Results) == 0:
	case len(s.Results) == 1 && n == 1:
		g.exprTo(s.Results[0], dst(0), out[0].typ)
	case len(s.Results) == 1 && n > 1:
		rs := g.multi(s.Results[0])
		for i, r := range rs {
			t := out[i].typ
			g.move(dst(i), g.convertImplicit(r.l, r.t, t), t)
		}
	default:
		vals := make([]loc, n)
		for _, i := range callsFirst(s.Results) {
			t := out[i].typ
			vals[i] = g.keep(g.exprAs(s.Results[i], t), t)
		}
		for i := range vals {
			g.move(dst(i), vals[i], out[i].typ)
		}
	}
	g.returnJump()
}

func (g *gen) push(brk, cont *label, name string) {
	g.scopes = append(g.scopes, scope{brk: brk, cont: cont, name: name})
}
func (g *gen) popScope() { g.scopes = g.scopes[:len(g.scopes)-1] }

func (g *gen) forStmt(s *ast.ForStmt, name string) {
	if s.Init != nil {
		g.stmt(s.Init)
	}
	// loop variables declared by the init statement that live on the heap get a fresh object per iteration
	var perIter []*types.Var
	if as, ok := s.Init.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				if v, ok := g.info.Defs[id].(*types.Var); ok && g.heapVars[v] {
					perIter = append(perIter, v)
				}
			}
		}
	}
	top, cont, done := g.newLabel(), g.newLabel(), g.newLabel()
	g.place(top)
	if s.Cond != nil {
		m := g.mark()
		g.condJump(s.Cond, done)
		g.release(m)
	}
	g.push(done, cont, name)
	g.block(s.Body.List)
	g.popScope()
	g.place(cont)
	for _, v := range perIter {
		// copy the variable into a new object: closures made in this iteration keep the old one
		l := g.vars[v]
		g.a.lea(AX, msym(g.c.typeSym(g.vt(v)), 0))
		g.callRT("newobject")
		np := g.ptrTemp()
		g.a.store(mb(BP, np.off), AX, 8)
		g.move(loc{kind: lInd, slot: np.off}, l, g.vt(v))
		g.a.load(AX, mb(BP, np.off), 8, false)
		g.a.store(mb(BP, l.slot), AX, 8)
	}
	if s.Post != nil {
		g.stmt(s.Post)
	}
	g.jmp(top)
	g.place(done)
}

func (g *gen) switchStmt(s *ast.SwitchStmt, name string) {
	if s.Init != nil {
		g.stmt(s.Init)
	}
	var tag loc
	var tt types.Type
	if s.Tag != nil {
		tt = g.typeOf(s.Tag)
		if b, ok := tt.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			tt = types.Default(tt)
		}
		tag = g.keep(g.exprAs(s.Tag, tt), tt)
	}
	done := g.newLabel()
	clauses := s.Body.List
	bodies := make([]*label, len(clauses))
	for i := range clauses {
		bodies[i] = g.newLabel()
	}
	var deflt *label
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		if cc.List == nil {
			deflt = bodies[i]
			continue
		}
		for _, e := range cc.List {
			m := g.mark()
			g.pos(e.Pos())
			next := g.newLabel()
			if s.Tag == nil {
				g.condJump(e, next)
			} else {
				et := g.typeOf(e)
				el := g.expr(e)
				r := g.compareLocs(token.EQL, tag, tt, el, et)
				g.loadTo(AX, r, types.Typ[types.Bool])
				g.a.testRR(1, AX, AX)
				g.jcc(ccE, next)
			}
			g.release(m)
			g.jmp(bodies[i])
			g.place(next)
		}
	}
	if deflt != nil {
		g.jmp(deflt)
	} else {
		g.jmp(done)
	}
	g.push(done, nil, name)
	saved := g.fallthru
	for i, c := range clauses {
		cc := c.(*ast.CaseClause)
		g.place(bodies[i])
		g.fallthru = nil
		if i+1 < len(clauses) {
			g.fallthru = bodies[i+1]
		}
		g.block(cc.Body)
		g.jmp(done)
	}
	g.fallthru = saved
	g.popScope()
	g.place(done)
}

func (g *gen) rangeStmt(s *ast.RangeStmt, name string) {
	xt := g.typeOf(s.X)
	// the iteration variables: new per iteration (:=) or existing lvalues (=)
	var keyVar, valVar *types.Var
	define := s.Tok == token.DEFINE
	blank := func(e ast.Expr) bool {
		if e == nil {
			return true
		}
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "_"
	}
	if define {
		if !blank(s.Key) {
			keyVar = g.info.Defs[s.Key.(*ast.Ident)].(*types.Var)
		}
		if !blank(s.Value) {
			valVar = g.info.Defs[s.Value.(*ast.Ident)].(*types.Var)
		}
	}
	// setKey / setVal store an iteration value
	set := func(e ast.Expr, v *types.Var, src loc, st types.Type) {
		if blank(e) {
			return
		}
		if define {
			if src.kind == lReg || src.kind == lRegInd {
				src = g.keep(src, st)
			}
			l := g.declareVar(v)
			g.move(l, g.convertImplicit(src, st, g.vt(v)), g.vt(v))
			return
		}
		src = g.keep(src, st)
		dt := g.typeOf(e)
		if g.isMapIndex(e) {
			ix := ast.Unparen(e).(*ast.IndexExpr)
			ml, kl := g.mapOperands(ix)
			g.move(g.mapAssignLocs(ix, ml, kl), g.convertImplicit(src, st, dt), dt)
			return
		}
		dst := g.lvalue(e)
		g.move(dst, g.convertImplicit(src, st, dt), dt)
	}
	_ = keyVar
	_ = valVar
	top, cont, done := g.newLabel(), g.newLabel(), g.newLabel()
	intT := types.Typ[types.Int]
	switch u := xt.Underlying().(type) {
	case *types.Basic:
		if u.Info()&types.IsString != 0 {
			xs := g.keep(g.expr(s.X), xt)
			if xs.kind == lZero {
				xs = g.zeroTemp(xt)
			}
			i := g.wordTemp()    // byte offset of the current rune
			next := g.wordTemp() // offset of the next one
			g.a.storeImm(mb(BP, next.off), 0, 8)
			g.place(top)
			g.a.load(AX, mb(BP, next.off), 8, false)
			g.a.store(mb(BP, i.off), AX, 8)
			xm := g.m(xs, R13)
			g.a.aluRM(aCMP, 8, AX, xm.plus(8))
			g.jcc(ccGE, done)
			// the rune: one byte if below 0x80, else runtime.decoderune(s, i)
			r := g.wordTemp()
			g.a.load(DX, xm, 8, false)
			g.a.load(CX, mem{base: DX, index: AX, scale: 1}, 1, false)
			g.a.aluImm(aCMP, 4, CX, 0x80)
			multi := g.a.jcc(ccAE)
			g.a.store(mb(BP, r.off), CX, 8)
			g.a.aluImm(aADD, 8, AX, 1)
			g.a.store(mb(BP, next.off), AX, 8)
			one := g.a.jmp()
			g.a.patch(multi, g.a.pc())
			g.a.movRR(CX, AX)
			g.a.load(AX, xm, 8, false)
			g.a.load(BX, xm.plus(8), 8, false)
			g.callRT("decoderune")
			g.a.extend(AX, 4, true)
			g.a.store(mb(BP, r.off), AX, 8)
			g.a.store(mb(BP, next.off), BX, 8)
			g.a.patch(one, g.a.pc())
			set(s.Key, keyVar, i, intT)
			set(s.Value, valVar, r, types.Typ[types.Rune])
			g.push(done, cont, name)
			g.block(s.Body.List)
			g.popScope()
			g.place(cont)
			g.jmp(top)
			g.place(done)
			return
		}
		if u.Info()&types.IsInteger != 0 {
			// for i := range n
			var kt types.Type = xt
			if keyVar != nil {
				kt = g.vt(keyVar)
			} else if !blank(s.Key) {
				kt = g.typeOf(s.Key)
			}
			if b, ok := kt.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
				kt = intT
			}
			n := g.wordTemp()
			g.loadTo(AX, g.exprAs(s.X, kt), kt)
			g.a.store(mb(BP, n.off), AX, 8)
			i := g.wordTemp()
			g.a.storeImm(mb(BP, i.off), 0, 8)
			g.place(top)
			g.a.load(AX, mb(BP, i.off), 8, false)
			g.a.aluRM(aCMP, 8, AX, mb(BP, n.off))
			if isSigned(kt) {
				g.jcc(ccGE, done)
			} else {
				g.jcc(ccAE, done)
			}
			set(s.Key, keyVar, i, kt)
			g.push(done, cont, name)
			g.block(s.Body.List)
			g.popScope()
			g.place(cont)
			g.a.aluMI(aADD, 8, mb(BP, i.off), 1)
			g.jmp(top)
			g.place(done)
			return
		}
	case *types.Slice, *types.Array, *types.Pointer:
		var et types.Type
		var n int64 = -1
		var base loc // slice header, array in memory, or pointer to array
		isPtr := false
		switch u := u.(type) {
		case *types.Slice:
			et = u.Elem()
			base = g.keep(g.expr(s.X), xt)
			if base.kind == lZero {
				base = g.zeroTemp(xt)
			}
		case *types.Array:
			et = u.Elem()
			n = u.Len()
			if blank(s.Value) {
				// only the length matters; the operand is still evaluated unless it is a plain variable
				if !g.pure(s.X) {
					g.expr(s.X)
				}
			} else {
				base = g.keep(g.expr(s.X), xt)
				if base.kind == lZero {
					base = g.zeroTemp(xt)
				}
			}
		case *types.Pointer:
			arr, ok := u.Elem().Underlying().(*types.Array)
			if !ok {
				giveUp("range over %v", xt)
			}
			et = arr.Elem()
			n = arr.Len()
			base = g.keep(g.expr(s.X), xt)
			isPtr = true
		}
		es := g.c.sizeof(et)
		i := g.wordTemp()
		ln := g.wordTemp()
		if n >= 0 {
			g.a.movImm(AX, n)
		} else {
			g.a.load(AX, g.m(base, R13).plus(8), 8, false)
		}
		g.a.store(mb(BP, ln.off), AX, 8)
		g.a.storeImm(mb(BP, i.off), 0, 8)
		g.place(top)
		g.a.load(AX, mb(BP, i.off), 8, false)
		g.a.aluRM(aCMP, 8, AX, mb(BP, ln.off))
		g.jcc(ccGE, done)
		set(s.Key, keyVar, i, intT)
		if !blank(s.Value) {
			g.a.load(BX, mb(BP, i.off), 8, false)
			switch {
			case n < 0:
				g.a.load(DX, g.m(base, R13), 8, false)
			case isPtr:
				g.loadTo(DX, base, xt)
			default:
				g.addrTo(DX, base)
			}
			g.scaleAdd(es)
			set(s.Value, valVar, loc{kind: lRegInd, reg: AX}, et)
		}
		g.push(done, cont, name)
		g.block(s.Body.List)
		g.popScope()
		g.place(cont)
		g.a.aluMI(aADD, 8, mb(BP, i.off), 1)
		g.jmp(top)
		g.place(done)
		return
	case *types.Map:
		g.rangeMap(s, name, set)
		return
	case *types.Chan:
		g.rangeChan(s, name, set)
		return
	case *types.Signature:
		g.rangeFunc(s, name)
		return
	}
	giveUp("range over %v", xt)
}

// staticCond: the value of a condition that constants decide.
func (g *gen) staticCond(e ast.Expr) (val, known bool) {
	e = ast.Unparen(e)
	if tv := g.info.Types[e]; tv.Value != nil {
		return constant.BoolVal(tv.Value), true
	}
	switch e := e.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			v, k := g.staticCond(e.X)
			return !v, k
		}
	case *ast.BinaryExpr:
		if e.Op == token.LAND || e.Op == token.LOR {
			// decided by the left operand, or by the right one when the left has nothing to run
			stop := e.Op == token.LOR
			if v, k := g.staticCond(e.X); k && v == stop {
				return stop, true
			}
			if v, k := g.staticCond(e.Y); k && v == stop && !hasCall(e.X) && !mayPanic(e.X) {
				return stop, true
			}
		}
	}
	return false, false
}

// mayPanic: could evaluating e panic (conservatively: anything but names, selections of names and
// constants combined by comparisons and logic)?
func mayPanic(e ast.Expr) bool {
	bad := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident, *ast.BasicLit, *ast.ParenExpr, nil:
		case *ast.BinaryExpr:
			if n.Op == token.QUO || n.Op == token.REM || n.Op == token.SHL || n.Op == token.SHR {
				bad = true
			}
		case *ast.UnaryExpr:
			if n.Op != token.NOT && n.Op != token.SUB && n.Op != token.XOR {
				bad = true
			}
		default:
			bad = true
		}
		return !bad
	})
	return bad
}

func hasLabel(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if _, ok := n.(*ast.LabeledStmt); ok {
			found = true
		}
		return !found
	})
	return found
}
