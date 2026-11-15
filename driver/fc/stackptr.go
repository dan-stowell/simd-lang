package main

// stackptr.go: pointer variables that provably point into the function's own frame, so that stores through
// them need no write barrier (loc.stk, loc.noBarrier).
//
// The case is  p := (*T)(unsafe.Pointer(&v))  or  p := &v  with v a variable in the frame, p declared there
// and never assigned again, and p's own address never taken. Then p.f = x writes v, in the frame.

import (
	"go/ast"
	"go/token"
	"go/types"
)

// noteStackPtr is called when v has just been declared with the initializer e.
func (g *gen) noteStackPtr(v *types.Var, e ast.Expr) {
	if _, ok := v.Type().Underlying().(*types.Pointer); !ok || g.heapVars[v] || g.fnBody == nil {
		return
	}
	// strip parentheses and conversions down to &x
	for {
		e = ast.Unparen(e)
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !tvOf(call.Fun).IsType() {
			break
		}
		e = call.Args[0]
	}
	u, ok := e.(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return
	}
	// &x, &x.f, &x[const] with x a variable of this frame (fields and elements of the value itself, no pointers followed)
	x := ast.Unparen(u.X)
	for {
		switch y := x.(type) {
		case *ast.SelectorExpr:
			sel := selnOf(y)
			if sel == nil || sel.Kind() != types.FieldVal || sel.Indirect() {
				return
			}
			x = ast.Unparen(y.X)
			continue
		case *ast.IndexExpr:
			if _, ok := g.typeOf(y.X).Underlying().(*types.Array); !ok {
				return
			}
			x = ast.Unparen(y.X)
			continue
		}
		break
	}
	id, ok := x.(*ast.Ident)
	if !ok {
		return
	}
	w, ok := useOf(id).(*types.Var)
	if !ok || g.heapVars[w] {
		return
	}
	if l, ok := g.vars[w]; !ok || !l.inFrame() {
		return
	}
	if g.reassigned == nil {
		g.reassigned = map[*types.Var]bool{}
		mark := func(e ast.Expr) {
			if id, ok := ast.Unparen(e).(*ast.Ident); ok {
				if v, ok := useOf(id).(*types.Var); ok {
					g.reassigned[v] = true
				}
			}
		}
		ast.Inspect(g.fnBody, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					mark(l) // (a new variable of := is a definition, not a use: not marked)
				}
			case *ast.IncDecStmt:
				mark(n.X)
			case *ast.RangeStmt:
				if n.Key != nil {
					mark(n.Key)
				}
				if n.Value != nil {
					mark(n.Value)
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					mark(n.X)
				}
			}
			return true
		})
	}
	if g.reassigned[v] {
		return
	}
	if g.stkPtr == nil {
		g.stkPtr = map[*types.Var]bool{}
	}
	g.stkPtr[v] = true
}

// isStackPtr: e is a variable noteStackPtr accepted.
func (g *gen) isStackPtr(e ast.Expr) bool {
	if g.stkPtr == nil {
		return false
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := useOf(id).(*types.Var)
	return ok && g.stkPtr[v]
}
