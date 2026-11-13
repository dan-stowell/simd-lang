// escape_stmt.go: statements.
package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

func constantInt64(tv types.TypeAndValue) (int64, bool) {
	if tv.Value == nil {
		return 0, false
	}
	v := constant.ToInt(tv.Value)
	if v.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(v)
}

// block walks statements and restores the loop depth (a label inside may have raised it). Statements that
// cannot be reached are skipped, the ones the stock compiler's front end drops: what follows a return, a
// goto or a panic up to the next label, and the branch of an if that a constant condition excludes.
func (e *escWalk) block(list []ast.Stmt) {
	old := e.loopDepth
	lastLabel := -1
	for i, s := range list {
		if _, ok := s.(*ast.LabeledStmt); ok {
			lastLabel = i
		}
	}
	dead := false
	for i, s := range list {
		if dead && i > lastLabel {
			continue
		}
		e.stmt(s)
		dead = e.terminates(s)
	}
	e.loopDepth = old
}

// staticBool: is the condition always true (+1), always false (-1), or neither (0)?
func (e *escWalk) staticBool(x ast.Expr) int { return e.pk.staticBool(x) }

func (e *escPkg) staticBool(x ast.Expr) int {
	if x == nil {
		return 0
	}
	if tv, ok := e.info.Types[x]; ok && tv.Value != nil && tv.Value.Kind() == constant.Bool {
		if constant.BoolVal(tv.Value) {
			return 1
		}
		return -1
	}
	if b, ok := ast.Unparen(x).(*ast.BinaryExpr); ok {
		switch b.Op {
		case token.LAND:
			l := e.staticBool(b.X)
			if l < 0 {
				return -1
			}
			if r := e.staticBool(b.Y); l > 0 || r < 0 {
				return r
			}
		case token.LOR:
			l := e.staticBool(b.X)
			if l > 0 {
				return 1
			}
			if r := e.staticBool(b.Y); l < 0 || r > 0 {
				return r
			}
		}
	}
	return 0
}

// terminates: control does not reach the statement after s.
func (e *escWalk) terminates(s ast.Stmt) bool { return e.pk.terminates(s) }

func (e *escPkg) terminates(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.BranchStmt:
		return s.Tok == token.GOTO
	case *ast.ReturnStmt:
		return true
	case *ast.ExprStmt:
		if call, ok := ast.Unparen(s.X).(*ast.CallExpr); ok {
			if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && id.Name == "panic" {
				_, ok := e.info.Uses[id].(*types.Builtin)
				return ok
			}
		}
	case *ast.IfStmt:
		c := e.staticBool(s.Cond)
		return (c < 0 || e.terminates(s.Body)) && (c > 0 || s.Else != nil && e.terminates(s.Else))
	case *ast.BlockStmt:
		for i := len(s.List) - 1; i >= 0; i-- {
			if _, ok := s.List[i].(*ast.EmptyStmt); !ok {
				return e.terminates(s.List[i])
			}
		}
	}
	return false
}

// inspectLive is ast.Inspect without the statements that cannot be reached (as block and stmt skip them).
func (pk *escPkg) inspectLive(n ast.Node, f func(ast.Node) bool) {
	if n == nil {
		return
	}
	list := func(stmts []ast.Stmt) {
		lastLabel := -1
		for i, s := range stmts {
			if _, ok := s.(*ast.LabeledStmt); ok {
				lastLabel = i
			}
		}
		dead := false
		for i, s := range stmts {
			if dead && i > lastLabel {
				continue
			}
			pk.inspectLive(s, f)
			dead = pk.terminates(s)
		}
	}
	ast.Inspect(n, func(m ast.Node) bool {
		switch m := m.(type) {
		case *ast.BlockStmt:
			if f(m) {
				list(m.List)
			}
			return false
		case *ast.CaseClause:
			if f(m) {
				for _, x := range m.List {
					pk.inspectLive(x, f)
				}
				list(m.Body)
			}
			return false
		case *ast.CommClause:
			if f(m) {
				if m.Comm != nil {
					pk.inspectLive(m.Comm, f)
				}
				list(m.Body)
			}
			return false
		case *ast.IfStmt:
			if !f(m) {
				return false
			}
			if m.Init != nil {
				pk.inspectLive(m.Init, f)
			}
			pk.inspectLive(m.Cond, f)
			c := pk.staticBool(m.Cond)
			if c >= 0 {
				pk.inspectLive(m.Body, f)
			}
			if c <= 0 && m.Else != nil {
				pk.inspectLive(m.Else, f)
			}
			return false
		case *ast.ForStmt:
			if m.Cond != nil && pk.staticBool(m.Cond) < 0 {
				if f(m) {
					if m.Init != nil {
						pk.inspectLive(m.Init, f)
					}
					pk.inspectLive(m.Cond, f)
				}
				return false
			}
		}
		if m == nil {
			return false
		}
		return f(m)
	})
}

// cond evaluates a condition, without the operand that a constant operand before it excludes.
func (e *escWalk) cond(x ast.Expr) {
	if x == nil {
		return
	}
	if b, ok := ast.Unparen(x).(*ast.BinaryExpr); ok && (b.Op == token.LAND || b.Op == token.LOR) {
		l := e.staticBool(b.X)
		e.cond(b.X)
		if b.Op == token.LAND && l < 0 || b.Op == token.LOR && l > 0 {
			return
		}
		e.cond(b.Y)
		return
	}
	e.discard(x)
}

func (e *escWalk) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case nil, *ast.EmptyStmt, *ast.BranchStmt:
	case *ast.BlockStmt:
		e.block(s.List)
	case *ast.ExprStmt:
		if c, ok := ast.Unparen(s.X).(*ast.CallExpr); ok {
			e.call(nil, c, nil)
		} else {
			e.discard(s.X)
		}
	case *ast.DeclStmt:
		if d, ok := s.Decl.(*ast.GenDecl); ok && d.Tok == token.VAR {
			for _, sp := range d.Specs {
				e.valueSpec(sp.(*ast.ValueSpec))
			}
		}
	case *ast.AssignStmt:
		e.assign(s)
	case *ast.IncDecStmt:
		e.addr(s.X) // m[k]++ stores the key
	case *ast.SendStmt:
		e.discard(s.Chan)
		var et types.Type
		if c, ok := escUnder(e.typeOf(s.Chan)).(*types.Chan); ok {
			et = c.Elem()
		}
		e.exprTo(e.heapHole(), s.Value, et)
	case *ast.GoStmt:
		e.goDefer(s.Call, s, true)
	case *ast.DeferStmt:
		e.goDefer(s.Call, s, false)
	case *ast.ReturnStmt:
		e.ret(s)
	case *ast.LabeledStmt:
		if l, ok := e.info.Defs[s.Label].(*types.Label); ok && e.fn.looping[l] {
			e.loopDepth++
		}
		e.stmt(s.Stmt)
	case *ast.IfStmt:
		old := e.loopDepth
		e.stmt(s.Init)
		e.cond(s.Cond)
		c := e.staticBool(s.Cond)
		if c >= 0 {
			e.block(s.Body.List)
		}
		if s.Else != nil && c <= 0 {
			e.block([]ast.Stmt{s.Else})
		}
		e.loopDepth = old
	case *ast.ForStmt:
		old := e.loopDepth
		if s.Init != nil {
			if e.fn.perIter {
				e.declBump = 1
			}
			e.stmt(s.Init)
			e.declBump = 0
		}
		e.loopDepth++
		e.cond(s.Cond)
		if s.Cond == nil || e.staticBool(s.Cond) >= 0 {
			e.stmt(s.Post)
			e.block(s.Body.List)
		}
		e.loopDepth = old
	case *ast.RangeStmt:
		e.rangeStmt(s)
	case *ast.SwitchStmt:
		old := e.loopDepth
		e.stmt(s.Init)
		var tt types.Type
		if s.Tag != nil {
			tt = e.typeOf(s.Tag)
		}
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			for _, x := range cc.List {
				e.exprTo(e.discardHole(), x, tt)
			}
		}
		e.discard(s.Tag)
		for _, c := range s.Body.List {
			e.block(c.(*ast.CaseClause).Body)
		}
		e.loopDepth = old
	case *ast.TypeSwitchStmt:
		old := e.loopDepth
		e.stmt(s.Init)
		var x ast.Expr
		bound := false
		switch a := s.Assign.(type) {
		case *ast.ExprStmt:
			x = a.X
		case *ast.AssignStmt:
			x = a.Rhs[0]
			bound = true
		}
		if ta, ok := ast.Unparen(x).(*ast.TypeAssertExpr); ok {
			x = ta.X
		}
		var ks []escHole
		if bound {
			for _, c := range s.Body.List {
				if v, ok := e.info.Implicits[c].(*types.Var); ok {
					l := e.dcl(v)
					// the stock compiler has one variable per clause, at the clause's first type
					if cc := c.(*ast.CaseClause); len(cc.List) > 0 {
						e.out.varPos[v] = escPos(cc.List[0])
					} else {
						e.out.varPos[v] = cc.Pos()
					}
					if e.pk.hasPointers(v.Type()) {
						ks = append(ks, l.hole().dotType(v.Type()))
					}
				}
			}
		}
		e.expr(e.tee(ks...), x)
		for _, c := range s.Body.List {
			e.block(c.(*ast.CaseClause).Body)
		}
		e.loopDepth = old
	case *ast.SelectStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CommClause)
			old := e.loopDepth
			e.stmt(cc.Comm)
			e.block(cc.Body)
			e.loopDepth = old
		}
	}
}

func (e *escWalk) valueSpec(s *ast.ValueSpec) {
	var ks []escHole
	var ts []types.Type
	for _, id := range s.Names {
		v, _ := e.info.Defs[id].(*types.Var)
		if v == nil || id.Name == "_" {
			ks = append(ks, e.discardHole())
			ts = append(ts, nil)
			continue
		}
		ks = append(ks, e.dcl(v).hole())
		ts = append(ts, v.Type())
	}
	e.stmtPos = s.Pos()
	e.assignTo(ks, ts, s.Values)
	e.stmtPos = token.NoPos
}

// initSpec: a package-level variable declaration: the values flow to the heap.
func (e *escWalk) initSpec(s *ast.ValueSpec) {
	var ks []escHole
	var ts []types.Type
	for _, id := range s.Names {
		v, _ := e.info.Defs[id].(*types.Var)
		if v == nil || id.Name == "_" {
			ks = append(ks, e.discardHole())
			ts = append(ts, nil)
			continue
		}
		ks = append(ks, e.heapHole())
		ts = append(ts, v.Type())
	}
	e.assignTo(ks, ts, s.Values)
}

// assignTo flows the values to the holes: one value each, or one call for all of them.
func (e *escWalk) assignTo(ks []escHole, ts []types.Type, values []ast.Expr) {
	if len(values) == 0 {
		return
	}
	if len(values) == 1 && len(ks) > 1 {
		e.multi(ks, ts, values[0])
		return
	}
	for i, v := range values {
		if i < len(ks) {
			e.exprTo(ks[i], v, ts[i])
		} else {
			e.discard(v)
		}
	}
}

// forceSite: a value converted to an interface where there is no expression to name the conversion (one
// result of a call with several): it uses the heap, and the site's key is the whole expression.
func (e *escWalk) forceSite(n ast.Expr, pos token.Pos) {
	if !pos.IsValid() {
		pos = escPos(n)
	}
	e.force = append(e.force, n)
	e.sites = append(e.sites, escSite{node: n, kind: escSiteIface, pos: pos, heap: true, why: "one of a call's results", gen: e.fn.generic})
}

// multi: one expression for several destinations: a call, or the comma-ok forms.
func (e *escWalk) multi(ks []escHole, ts []types.Type, v ast.Expr) {
	x := ast.Unparen(v)
	call, ok := x.(*ast.CallExpr)
	tup, isTuple := e.typeOf(v).(*types.Tuple)
	if !ok || !isTuple {
		// v, ok = x.(T); v, ok = m[k]; v, ok = <-c
		e.exprToComma(ks[0], v, ts[0])
		return
	}
	ks = append([]escHole(nil), ks...)
	for i := range ks {
		if i < tup.Len() && ts[i] != nil && e.needsIface(ts[i], tup.At(i).Type()) && !escDirectIface(tup.At(i).Type()) {
			// the interface's copy has no syntax of its own: it uses the heap
			e.forceSite(v, e.stmtPos)
			ks[i] = e.heapHole()
		}
	}
	e.call(ks, call, nil)
}

// exprToComma: the first value of a comma-ok expression, converted to type t.
func (e *escWalk) exprToComma(k escHole, v ast.Expr, t types.Type) {
	if tup, ok := e.typeOf(v).(*types.Tuple); ok && tup.Len() > 0 && t != nil && e.needsIface(t, tup.At(0).Type()) &&
		!escDirectIface(tup.At(0).Type()) {
		e.forceSite(v, e.stmtPos)
		k = e.heapHole()
	}
	// the type recorded for the expression is the pair: do not prune on it
	switch x := ast.Unparen(v).(type) {
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			e.expr(k.dotType(e.typeOf(x.Type)), x.X)
		} else {
			e.expr(k, x.X)
		}
	case *ast.IndexExpr:
		e.discard(x.X)
		if m, ok := escUnder(e.typeOf(x.X)).(*types.Map); ok {
			e.exprTo(e.discardHole(), x.Index, m.Key())
		} else {
			e.discard(x.Index)
		}
	case *ast.UnaryExpr:
		e.discard(x.X)
	default:
		e.expr(k, v)
	}
}

// addr: the hole that an assignment to n stores into.
func (e *escWalk) addr(n ast.Expr) escHole {
	switch n := ast.Unparen(n).(type) {
	case nil:
		return e.discardHole()
	case *ast.Ident:
		if n.Name == "_" {
			return e.discardHole()
		}
		if v := e.local(n); v != nil {
			return e.loc(v).hole()
		}
	case *ast.SelectorExpr:
		sel := e.info.Selections[n]
		if sel == nil || sel.Kind() != types.FieldVal {
			break
		}
		d, t := escPath(e.typeOf(n.X), sel.Index())
		if _, ok := t.Underlying().(*types.Pointer); ok {
			d++
		}
		if d == 0 {
			return e.addr(n.X)
		}
		e.discard(n.X)
	case *ast.IndexExpr:
		switch u := escUnder(e.typeOf(n.X)).(type) {
		case *types.Array:
			e.discard(n.Index)
			return e.addr(n.X)
		case *types.Map:
			e.discard(n.X)
			// a key that is stored escapes
			e.exprTo(e.heapHole(), n.Index, u.Key())
		default:
			e.discard(n.X)
			e.discard(n.Index)
		}
	case *ast.StarExpr:
		e.discard(n.X)
	default:
		e.discard(n)
	}
	return e.heapHole()
}

func (e *escWalk) lhsType(n ast.Expr) types.Type {
	if id, ok := ast.Unparen(n).(*ast.Ident); ok && id.Name == "_" {
		return nil
	}
	return e.typeOf(n)
}

func (e *escWalk) assign(s *ast.AssignStmt) {
	if s.Tok == token.DEFINE {
		for _, l := range s.Lhs {
			if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
				if v, ok := e.info.Defs[id].(*types.Var); ok {
					e.dcl(v)
				}
			}
		}
	}
	ks := make([]escHole, len(s.Lhs))
	ts := make([]types.Type, len(s.Lhs))
	for i, l := range s.Lhs {
		ks[i] = e.addr(l)
		ts[i] = e.lhsType(l)
	}
	if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
		// x op= y
		e.expr(ks[0], s.Rhs[0])
		return
	}
	if len(s.Lhs) == len(s.Rhs) {
		for i := range s.Lhs {
			if e.selfAssign(s.Lhs[i], s.Rhs[i]) {
				ks[i] = e.discardHole()
			}
			if e.headerData(s.Lhs[i]) {
				// a uintptr stored in a reflect.SliceHeader's Data is a pointer
				e.unsafeValue(e.heapHole(), s.Rhs[i])
				ks[i] = e.discardHole()
			}
		}
	}
	e.stmtPos = s.TokPos
	e.assignTo(ks, ts, s.Rhs)
	e.stmtPos = token.NoPos
}

func (e *escWalk) headerData(l ast.Expr) bool {
	sel, ok := ast.Unparen(l).(*ast.SelectorExpr)
	return ok && e.isHeaderData(sel)
}

func (e *escWalk) ret(s *ast.ReturnStmt) {
	if len(s.Results) == 0 {
		return
	}
	res := e.fn.results
	ks := make([]escHole, len(res))
	ts := make([]types.Type, len(res))
	for i, r := range res {
		ks[i] = r.hole()
		ts[i] = r.v.Type()
	}
	if len(ks) == 0 {
		for _, r := range s.Results {
			e.discard(r)
		}
		return
	}
	if len(s.Results) == 1 && len(ks) == 1 {
		e.exprTo(ks[0], s.Results[0], ts[0])
		return
	}
	e.stmtPos = s.Pos()
	e.assignTo(ks, ts, s.Results)
	e.stmtPos = token.NoPos
}

// goDefer: the call runs later, in another goroutine or when the function returns: its callee and its
// arguments are kept in a closure object until then. A goroutine's outlives everything; so does a deferred
// call's when the statement may run more than once (in a loop: each needs its own); otherwise the frame
// is still there when the call runs.
func (e *escWalk) goDefer(call *ast.CallExpr, s ast.Stmt, isGo bool) {
	k := e.heapHole()
	if !isGo && e.loopDepth == 1 && e.fn.rng == nil {
		k = e.discardHole()
	}
	if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok && len(call.Args) == 0 {
		if sig, ok := e.typeOf(lit).(*types.Signature); ok && sig.Results().Len() == 0 {
			e.expr(k, lit) // the literal's own closure object is what is kept
			return
		}
	}
	w := e.spill(k, s, escSiteGoDefer, s.Pos())
	e.call(nil, call, &w)
}

func (e *escWalk) rangeStmt(s *ast.RangeStmt) {
	xt := e.typeOf(s.X)
	u := escUnder(xt)
	if _, ok := u.(*types.Signature); ok {
		e.rangeFunc(s)
		return
	}
	old := e.loopDepth
	// the operand is evaluated once, before the loop
	tmp := e.temp()
	e.expr(tmp.hole(), s.X)
	e.loopDepth++
	if s.Tok == token.DEFINE {
		for _, x := range []ast.Expr{s.Key, s.Value} {
			if id, ok := x.(*ast.Ident); ok && id.Name != "_" {
				if v, ok := e.info.Defs[id].(*types.Var); ok {
					e.dcl(v)
				}
			}
		}
	}
	if s.Key != nil {
		e.addr(s.Key)
	}
	if s.Value != nil {
		vk := e.addr(s.Value)
		switch u.(type) {
		case *types.Array:
			e.flow(vk, tmp)
		case nil:
			e.flow(vk, tmp)
		default:
			e.flow(vk.deref(), tmp)
		}
	}
	e.block(s.Body.List)
	e.loopDepth = old
}

// rangeFunc: the body is a function literal passed to the iterator function. It captures the variables it
// uses, and the results of the function a return inside it returns from.
func (e *escWalk) rangeFunc(s *ast.RangeStmt) {
	c := e.callee(s.X)
	if c.fun != nil {
		e.discard(c.fun)
		if lit, ok := ast.Unparen(c.fun).(*ast.FuncLit); ok {
			c.lit = e.litFn[lit]
			c.sum = e.pk.litSum[lit]
		}
	}
	if c.recv != nil {
		e.expr(e.paramHole(&c, nil, -1).shift(c.recvShift), c.recv)
	}
	k := e.heapHole()
	if c.sig != nil && c.sig.Params().Len() == 1 {
		k = e.paramHole(&c, nil, 0)
	}
	k = e.spill(k, s, escSiteRangeFunc, s.For)
	nodes := []ast.Node{s.Body}
	if s.Tok == token.ASSIGN {
		if s.Key != nil {
			nodes = append(nodes, s.Key)
		}
		if s.Value != nil {
			nodes = append(nodes, s.Value)
		}
	}
	for _, v := range e.freeVars(s.Pos(), s.End(), nodes...) {
		e.flow(k.addr(), e.varLoc[v])
		if e.out.varWhy[v] == "" {
			e.out.varWhy[v] = "captured"
		}
	}
	returns := false
	ast.Inspect(s.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			returns = true
		}
		return true
	})
	if returns {
		for _, r := range e.fn.results {
			e.flow(k.addr(), r)
			if e.out.varWhy[r.v] == "" {
				e.out.varWhy[r.v] = "captured"
			}
		}
	}
	fn := e.pk.newFn(e.fn)
	fn.rng = s
	fn.results = e.fn.results
	e.lits = append(e.lits, fn)
	w := &escWalk{escBatch: e.escBatch, fn: fn, loopDepth: 1}
	if s.Tok == token.DEFINE {
		for _, x := range []ast.Expr{s.Key, s.Value} {
			if id, ok := x.(*ast.Ident); ok && id.Name != "_" {
				if v, ok := e.info.Defs[id].(*types.Var); ok {
					l := w.dcl(v)
					l.param = true
					fn.params = append(fn.params, l)
				}
			}
		}
	} else {
		if s.Key != nil {
			w.addr(s.Key)
		}
		if s.Value != nil {
			w.addr(s.Value)
		}
	}
	w.block(s.Body.List)
}

// selfAssign: assignments that store into an object only what it held already (as the stock compiler
// recognizes them): b.buf = b.buf[n:m]; val.x = val.y; val.x[i] = val.y[j].
func (e *escWalk) selfAssign(dst, src ast.Expr) bool {
	dst, src = ast.Unparen(dst), ast.Unparen(src)
	// through a pointer: *p or p.f where p is a pointer, p a variable
	base := func(x ast.Expr) types.Object {
		switch x := ast.Unparen(x).(type) {
		case *ast.StarExpr:
			if id, ok := ast.Unparen(x.X).(*ast.Ident); ok {
				return e.info.Uses[id]
			}
		case *ast.SelectorExpr:
			sel := e.info.Selections[x]
			if sel == nil || sel.Kind() != types.FieldVal || len(sel.Index()) != 1 {
				return nil
			}
			if _, ok := escUnder(e.typeOf(x.X)).(*types.Pointer); !ok {
				return nil
			}
			if id, ok := ast.Unparen(x.X).(*ast.Ident); ok {
				return e.info.Uses[id]
			}
		}
		return nil
	}
	if d := base(dst); d != nil {
		if sl, ok := src.(*ast.SliceExpr); ok {
			if _, isArray := escUnder(e.typeOf(sl.X)).(*types.Array); !isArray {
				if base(sl.X) == d {
					return true
				}
			}
		}
	}
	switch d := dst.(type) {
	case *ast.SelectorExpr:
		s, ok := src.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		ds, ss := e.info.Selections[d], e.info.Selections[s]
		if ds == nil || ss == nil || ds.Kind() != types.FieldVal || ss.Kind() != types.FieldVal ||
			len(ds.Index()) != 1 || len(ss.Index()) != 1 {
			return false
		}
		return e.sameSafe(d.X, s.X)
	case *ast.IndexExpr:
		s, ok := src.(*ast.IndexExpr)
		if !ok {
			return false
		}
		if _, ok := escUnder(e.typeOf(d.X)).(*types.Map); ok {
			return false
		}
		if _, ok := escUnder(e.typeOf(s.X)).(*types.Map); ok {
			return false
		}
		if e.affectsMemory(d.Index) || e.affectsMemory(s.Index) {
			return false
		}
		return e.sameSafe(d.X, s.X)
	}
	return false
}

func (e *escWalk) affectsMemory(n ast.Expr) bool {
	if tv, ok := e.info.Types[n]; ok && tv.Value != nil {
		return false
	}
	switch n := n.(type) {
	case *ast.Ident, *ast.BasicLit:
		return false
	case *ast.ParenExpr:
		return e.affectsMemory(n.X)
	case *ast.BinaryExpr:
		switch n.Op {
		case token.ADD, token.SUB, token.OR, token.XOR, token.MUL, token.SHL, token.SHR, token.AND, token.AND_NOT, token.QUO, token.REM:
			return e.affectsMemory(n.X) || e.affectsMemory(n.Y)
		}
	case *ast.IndexExpr:
		if _, ok := escUnder(e.typeOf(n.X)).(*types.Map); ok {
			return true
		}
		return e.affectsMemory(n.X) || e.affectsMemory(n.Index)
	case *ast.UnaryExpr:
		switch n.Op {
		case token.NOT, token.XOR, token.ADD, token.SUB:
			return e.affectsMemory(n.X)
		}
	case *ast.SelectorExpr:
		if sel := e.info.Selections[n]; sel != nil && sel.Kind() == types.FieldVal {
			return e.affectsMemory(n.X)
		}
	case *ast.StarExpr:
		return e.affectsMemory(n.X)
	case *ast.CallExpr:
		if len(n.Args) == 1 {
			if tv := e.info.Types[n.Fun]; tv.IsType() {
				return e.affectsMemory(n.Args[0])
			}
			if name := e.builtinName(n.Fun); name == "len" || name == "cap" {
				return e.affectsMemory(n.Args[0])
			}
		}
	}
	return true
}

// sameSafe: the two expressions are the same and have no side effects.
func (e *escWalk) sameSafe(l, r ast.Expr) bool {
	l, r = ast.Unparen(l), ast.Unparen(r)
	switch l := l.(type) {
	case *ast.Ident:
		r, ok := r.(*ast.Ident)
		if !ok {
			return false
		}
		lo, ro := e.info.Uses[l], e.info.Uses[r]
		_, isVar := lo.(*types.Var)
		return isVar && lo == ro
	case *ast.SelectorExpr:
		r, ok := r.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		ls, rs := e.info.Selections[l], e.info.Selections[r]
		if ls == nil || rs == nil {
			return ls == nil && rs == nil && e.info.Uses[l.Sel] == e.info.Uses[r.Sel] && e.info.Uses[l.Sel] != nil
		}
		return ls.Kind() == types.FieldVal && rs.Kind() == types.FieldVal && ls.Obj() == rs.Obj() && e.sameSafe(l.X, r.X)
	case *ast.StarExpr:
		r, ok := r.(*ast.StarExpr)
		return ok && e.sameSafe(l.X, r.X)
	case *ast.IndexExpr:
		r, ok := r.(*ast.IndexExpr)
		if !ok {
			return false
		}
		if _, ok := escUnder(e.typeOf(l.X)).(*types.Map); ok {
			return false
		}
		return e.sameSafe(l.X, r.X) && e.sameSafe(l.Index, r.Index)
	case *ast.BasicLit:
		lt, rt := e.info.Types[l], e.info.Types[r]
		return lt.Value != nil && rt.Value != nil && constant.Compare(lt.Value, token.EQL, rt.Value)
	}
	return false
}
