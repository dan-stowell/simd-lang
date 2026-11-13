// escape_expr.go: the flow of values through expressions.
package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// escWalk is the walk of one function's body.
type escWalk struct {
	*escBatch
	fn        *escFn
	loopDepth int
	declBump  int // added to the loop depth of declarations: the init statement of a for loop
	stmtPos   token.Pos
}

func (e *escWalk) typeOf(n ast.Expr) types.Type {
	if tv, ok := e.info.Types[n]; ok {
		return tv.Type
	}
	if id, ok := n.(*ast.Ident); ok {
		if o := e.info.ObjectOf(id); o != nil {
			return o.Type()
		}
	}
	return nil
}

// local: the local variable an identifier names, if it names one.
func (e *escWalk) local(id *ast.Ident) *types.Var {
	v, ok := e.info.Uses[id].(*types.Var)
	if !ok {
		v, ok = e.info.Defs[id].(*types.Var)
		if !ok {
			return nil
		}
	}
	if v.IsField() || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
		return nil
	}
	return v
}

// loc: the location of a local variable. Declarations come first in a walk in source order; a variable
// that has none yet is declared where it is first seen.
func (e *escWalk) loc(v *types.Var) *escLoc {
	if l := e.varLoc[v]; l != nil {
		return l
	}
	return e.dcl(v)
}

// dcl declares a variable here: at this loop depth.
func (e *escWalk) dcl(v *types.Var) *escLoc {
	l := e.varLoc[v]
	if l == nil {
		l = e.newLoc(e.fn, e.loopDepth+e.declBump)
		l.v = v
		e.varLoc[v] = l
		e.sizeCheck(l, v.Type(), true)
		return l
	}
	l.loopDepth = e.loopDepth + e.declBump
	return l
}

func (e *escWalk) temp() *escLoc { return e.newLoc(e.fn, e.loopDepth) }

// spill makes the location of an allocation site, flows its address to k and returns the hole for what
// is stored in it.
func (e *escWalk) spill(k escHole, n ast.Node, kind escSiteKind, pos token.Pos) escHole {
	l := e.newLoc(e.fn, e.loopDepth)
	e.sites = append(e.sites, escSite{node: n, kind: kind, pos: pos, gen: e.fn.generic})
	l.site = len(e.sites)
	e.flow(k.addr(), l)
	return l.hole()
}

// heapSiteWhy sends the site behind hole k (as spill returned it) to the heap for a reason of its own.
func (e *escWalk) heapSiteWhy(k escHole, why string) {
	k.dst.escapes = true
	e.sites[k.dst.site-1].why = why
}

// tee is a hole that flows to every one of ks.
func (e *escWalk) tee(ks ...escHole) escHole {
	var live []escHole
	for _, k := range ks {
		if k.dst != &e.blankLoc {
			live = append(live, k)
		}
	}
	switch len(live) {
	case 0:
		return e.discardHole()
	case 1:
		return live[0]
	}
	l := e.temp()
	for _, k := range live {
		if k.derefs < 0 {
			panic("fc: escape: tee of an address")
		}
		e.flow(k, l)
	}
	return l.hole()
}

func (e *escWalk) discard(n ast.Expr) {
	if n != nil {
		e.expr(e.discardHole(), n)
	}
}

func (k escHole) dotType(t types.Type) escHole {
	if !escIsIface(t) && !escIsTypeParam(t) && !escDirectIface(t) {
		k = k.shift(1)
	}
	return k
}

// needsIface: storing n where a value of type dst is wanted converts it to an interface.
func (e *escWalk) needsIface(dst types.Type, src types.Type) bool {
	if !escIsIface(dst) || src == nil {
		return false
	}
	if escIsIface(src) {
		return false
	}
	if b, ok := src.Underlying().(*types.Basic); ok && b.Kind() == types.UntypedNil {
		return false
	}
	if _, ok := src.(*types.Tuple); ok {
		return false
	}
	return true
}

// exprTo flows n to k as a value of type dst (nil: of its own type).
func (e *escWalk) exprTo(k escHole, n ast.Expr, dst types.Type) {
	if n == nil {
		return
	}
	if dst != nil && e.needsIface(dst, e.typeOf(n)) {
		e.convIface(k, n, escPos(n))
		return
	}
	e.expr(k, n)
}

// convIface: n converted to an interface. Unless the value is a pointer already, the interface holds the
// address of a copy.
func (e *escWalk) convIface(k escHole, n ast.Expr, pos token.Pos) {
	t := e.typeOf(n)
	if !escDirectIface(t) {
		k = e.spill(k, n, escSiteIface, pos)
		if !mentionsTypeParam(t) && e.pr.sizes.Sizeof(t) > escMaxImplicitStackVarSize {
			e.heapSiteWhy(k, "too large for stack")
		}
	}
	e.expr(k, n)
}

// escPos: where the stock compiler reports an expression (its syntax tree puts a binary expression at its
// operator, a call at its parenthesis, a selector at its dot, a composite literal at its brace).
func escPos(n ast.Expr) token.Pos {
	switch n := n.(type) {
	case *ast.ParenExpr:
		return n.Lparen
	case *ast.SelectorExpr:
		return n.X.End()
	case *ast.IndexExpr:
		return n.Lbrack
	case *ast.IndexListExpr:
		return n.Lbrack
	case *ast.SliceExpr:
		return n.Lbrack
	case *ast.CallExpr:
		return n.Lparen
	case *ast.BinaryExpr:
		return n.OpPos
	case *ast.CompositeLit:
		return n.Lbrace
	case *ast.TypeAssertExpr:
		return n.X.End()
	}
	return n.Pos()
}

// expr flows the value of n to k.
func (e *escWalk) expr(k escHole, n ast.Expr) {
	if n == nil {
		return
	}
	tv, ok := e.info.Types[n]
	if ok {
		if tv.Value != nil || tv.IsType() {
			return
		}
		if k.derefs >= 0 && tv.Type != nil && !e.pk.hasPointers(tv.Type) {
			k = e.discardHole()
		}
	}
	switch n := n.(type) {
	case *ast.Ident:
		if v := e.local(n); v != nil {
			e.flow(k, e.loc(v))
		}
	case *ast.BasicLit:
	case *ast.ParenExpr:
		e.expr(k, n.X)
	case *ast.FuncLit:
		e.funcLit(k, n)
	case *ast.CompositeLit:
		e.compositeLit(k, n)
	case *ast.UnaryExpr:
		switch n.Op {
		case token.AND:
			if cl, ok := ast.Unparen(n.X).(*ast.CompositeLit); ok {
				k = e.spill(k, cl, escSitePtrLit, n.OpPos)
				e.implicitSize(k, e.typeOf(cl))
				e.compositeBody(k, cl, e.typeOf(cl))
			} else {
				e.expr(k.addr(), n.X)
			}
		default:
			e.discard(n.X)
		}
	case *ast.BinaryExpr:
		if n.Op == token.ADD {
			if b, ok := escUnder(tv.Type).(*types.Basic); ok && b.Info()&types.IsString != 0 {
				e.spill(k, n, escSiteString, n.OpPos)
				e.discard(n.X)
				e.discard(n.Y)
				return
			}
		}
		switch n.Op {
		case token.EQL, token.NEQ:
			// comparing an interface with a value of another type converts the value
			e.exprTo(e.discardHole(), n.X, e.typeOf(n.Y))
			e.exprTo(e.discardHole(), n.Y, e.typeOf(n.X))
		default:
			e.discard(n.X)
			e.discard(n.Y)
		}
	case *ast.StarExpr:
		e.expr(k.deref(), n.X)
	case *ast.SelectorExpr:
		e.selector(k, n)
	case *ast.IndexExpr:
		switch u := escUnder(e.typeOf(n.X)).(type) {
		case *types.Signature:
			return // an instantiated function
		case *types.Array:
			e.expr(k, n.X)
		case *types.Map:
			// a key that is looked up does not escape
			e.discard(n.X)
			e.exprTo(e.discardHole(), n.Index, u.Key())
			return
		case *types.Basic:
			e.discard(n.X)
		default: // a slice, a pointer to an array
			e.expr(k.deref(), n.X)
		}
		e.discard(n.Index)
	case *ast.IndexListExpr:
	case *ast.SliceExpr:
		if _, ok := escUnder(e.typeOf(n.X)).(*types.Array); ok {
			e.expr(k.addr(), n.X) // the array's address
		} else {
			e.expr(k, n.X)
		}
		e.discard(n.Low)
		e.discard(n.High)
		e.discard(n.Max)
	case *ast.TypeAssertExpr:
		if n.Type == nil {
			e.expr(k, n.X)
		} else {
			e.expr(k.dotType(tv.Type), n.X)
		}
	case *ast.CallExpr:
		if k.dst == &e.blankLoc {
			e.call(nil, n, nil)
		} else {
			e.call([]escHole{k}, n, nil)
		}
	case *ast.KeyValueExpr:
		e.expr(k, n.Value)
	}
}

// pathDerefs: how many pointers a selection follows on its way (its last step, the field or method itself,
// not included), and the type that step is applied to.
func escPath(t types.Type, index []int) (derefs int, last types.Type) {
	for _, i := range index[:len(index)-1] {
		if p, ok := t.Underlying().(*types.Pointer); ok {
			derefs++
			t = p.Elem()
		}
		st, ok := t.Underlying().(*types.Struct)
		if !ok || i >= st.NumFields() {
			break
		}
		t = st.Field(i).Type()
	}
	return derefs, t
}

func (e *escWalk) selector(k escHole, n *ast.SelectorExpr) {
	sel := e.info.Selections[n]
	if sel == nil {
		return // a qualified identifier: a package's variable or function
	}
	switch sel.Kind() {
	case types.FieldVal:
		d, t := escPath(e.typeOf(n.X), sel.Index())
		if _, ok := t.Underlying().(*types.Pointer); ok {
			d++
		}
		e.expr(k.shift(d), n.X)
	case types.MethodVal:
		e.methodValue(k, n, sel)
	}
}

// recvShift: the dereferences (or the address-of: -1) between the operand of a method selection and the
// receiver the method gets.
func (e *escWalk) recvShift(n *ast.SelectorExpr, sel *types.Selection) int {
	d, t := escPath(e.typeOf(n.X), sel.Index())
	fn := sel.Obj().(*types.Func)
	recv := fn.Type().(*types.Signature).Recv().Type()
	if escIsIface(recv) || escIsTypeParam(t) {
		return d
	}
	_, wantPtr := types.Unalias(recv).(*types.Pointer)
	_, havePtr := t.Underlying().(*types.Pointer)
	switch {
	case wantPtr && !havePtr:
		d--
	case !wantPtr && havePtr:
		d++
	}
	return d
}

// methodValue: x.M not called. The receiver is bound in a closure object, and will be the method's receiver
// when someone calls it.
func (e *escWalk) methodValue(k escHole, n *ast.SelectorExpr, sel *types.Selection) {
	closureK := e.spill(k, n, escSiteMethodValue, escPos(n))
	fn := sel.Obj().(*types.Func)
	c := escCallee{sig: fn.Type().(*types.Signature), recv: n.X}
	if !escIsIface(c.sig.Recv().Type()) && !escIsTypeParam(sel.Recv()) {
		c.fn = fn.Origin()
	}
	// nobody knows who calls it: the results flow to the heap
	ks := make([]escHole, c.sig.Results().Len())
	for i := range ks {
		ks[i] = e.heapHole()
	}
	paramK := e.paramHole(&c, ks, -1)
	e.expr(e.tee(paramK, closureK).shift(e.recvShift(n, sel)), n.X)
}

// implicitSize: the limit on what &T{} and new(T) put in the frame.
func (e *escWalk) implicitSize(k escHole, t types.Type) {
	if t == nil || mentionsTypeParam(t) {
		return
	}
	if e.pr.sizes.Sizeof(t) > escMaxImplicitStackVarSize {
		e.heapSiteWhy(k, "too large for stack")
	}
}

func (e *escWalk) compositeLit(k escHole, n *ast.CompositeLit) {
	t := e.typeOf(n)
	if t == nil {
		return
	}
	if p, ok := t.Underlying().(*types.Pointer); ok && !escIsTypeParam(t) {
		// an element written {...} where the element type is *T: &T{...}
		k = e.spill(k, n, escSitePtrLit, n.Lbrace)
		e.implicitSize(k, p.Elem())
		t = p.Elem()
	}
	e.compositeBody(k, n, t)
}

func (e *escWalk) compositeBody(k escHole, n *ast.CompositeLit, t types.Type) {
	value := func(el ast.Expr) ast.Expr {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			return kv.Value
		}
		return el
	}
	switch u := escUnder(t).(type) {
	case *types.Struct:
		for i, el := range n.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				var ft types.Type
				if id, ok := kv.Key.(*ast.Ident); ok {
					if f, ok := e.info.Uses[id].(*types.Var); ok {
						ft = f.Type()
					}
				}
				e.exprTo(k, kv.Value, ft)
			} else if i < u.NumFields() {
				e.exprTo(k, el, u.Field(i).Type())
			}
		}
	case *types.Array:
		for _, el := range n.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				e.discard(kv.Key)
			}
			e.exprTo(k, value(el), u.Elem())
		}
	case *types.Slice:
		k = e.spill(k, n, escSiteSliceLit, n.Lbrace)
		if !mentionsTypeParam(u.Elem()) {
			var max int64 = -1
			var next int64
			for _, el := range n.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if tv := e.info.Types[kv.Key]; tv.Value != nil {
						if i, ok := constant.Int64Val(constant.ToInt(tv.Value)); ok {
							next = i
						}
					}
				}
				if next > max {
					max = next
				}
				next++
			}
			if (max+1)*e.pr.sizes.Sizeof(u.Elem()) > escMaxImplicitStackVarSize {
				e.heapSiteWhy(k, "too large for stack")
			}
		}
		for _, el := range n.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				e.discard(kv.Key)
			}
			e.exprTo(k, value(el), u.Elem())
		}
	case *types.Map:
		e.spill(k, n, escSiteMapLit, n.Lbrace)
		// keys and values are stored in the heap, whatever becomes of the map
		for _, el := range n.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				e.exprTo(e.heapHole(), kv.Key, u.Key())
				e.exprTo(e.heapHole(), kv.Value, u.Elem())
			}
		}
	default:
		for _, el := range n.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				e.expr(e.heapHole(), kv.Key)
			}
			e.expr(e.heapHole(), value(el))
		}
	}
}

// freeVars: the local variables used in the nodes that are declared outside [lo, hi).
func (e *escWalk) freeVars(lo, hi token.Pos, nodes ...ast.Node) []*types.Var {
	var out []*types.Var
	seen := map[*types.Var]bool{}
	for _, n := range nodes {
		if n == nil {
			continue
		}
		ast.Inspect(n, func(m ast.Node) bool {
			id, ok := m.(*ast.Ident)
			if !ok {
				return true
			}
			v, ok := e.info.Uses[id].(*types.Var)
			if !ok || v.IsField() || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() || seen[v] {
				return true
			}
			if v.Pos() >= lo && v.Pos() < hi {
				return true
			}
			if e.varLoc[v] == nil {
				return true // not a variable of a function being analyzed
			}
			seen[v] = true
			out = append(out, v)
			return true
		})
	}
	return out
}

// funcLit: the closure object holds the address of each variable the literal uses. Then the body.
func (e *escWalk) funcLit(k escHole, n *ast.FuncLit) {
	k = e.spill(k, n, escSiteClosure, n.Pos())
	for _, v := range e.freeVars(n.Pos(), n.End(), n) {
		e.flow(k.addr(), e.varLoc[v])
		if e.out.varWhy[v] == "" {
			e.out.varWhy[v] = "captured"
		}
	}
	fn := e.pk.newFn(e.fn)
	fn.lit = n
	fn.sig, _ = e.typeOf(n).(*types.Signature)
	if fn.sig == nil {
		return
	}
	fn.looping = escLoopingLabels(e.info, n.Body)
	e.initFn(fn)
	e.litFn[n] = fn
	e.lits = append(e.lits, fn)
	w := &escWalk{escBatch: e.escBatch, fn: fn, loopDepth: 1}
	w.block(n.Body.List)
}

// unsafeValue: a uintptr expression about to become an unsafe.Pointer. The pointers converted to uintptr
// inside its arithmetic are what it points to.
func (e *escWalk) unsafeValue(k escHole, n ast.Expr) {
	if tv, ok := e.info.Types[n]; ok && tv.Value != nil {
		return
	}
	switch n := n.(type) {
	case *ast.ParenExpr:
		e.unsafeValue(k, n.X)
		return
	case *ast.CallExpr:
		if tv := e.info.Types[n.Fun]; tv.IsType() && len(n.Args) == 1 {
			if b, ok := escUnder(e.typeOf(n.Args[0])).(*types.Basic); ok && b.Kind() == types.UnsafePointer {
				e.expr(k, n.Args[0])
			} else {
				e.discard(n.Args[0])
			}
			return
		}
	case *ast.SelectorExpr:
		if e.isHeaderData(n) {
			e.expr(k.deref(), n.X)
			return
		}
	case *ast.UnaryExpr:
		switch n.Op {
		case token.ADD, token.SUB, token.XOR:
			e.unsafeValue(k, n.X)
			return
		}
	case *ast.BinaryExpr:
		switch n.Op {
		case token.ADD, token.SUB, token.OR, token.XOR, token.MUL, token.QUO, token.REM, token.AND, token.AND_NOT:
			e.unsafeValue(k, n.X)
			e.unsafeValue(k, n.Y)
			return
		case token.SHL, token.SHR:
			e.unsafeValue(k, n.X)
			e.discard(n.Y)
			return
		}
	}
	e.discard(n)
}

// isHeaderData: p.Data where p is a *reflect.SliceHeader or *reflect.StringHeader.
func (e *escWalk) isHeaderData(n *ast.SelectorExpr) bool {
	if n.Sel.Name != "Data" {
		return false
	}
	p, ok := escUnder(e.typeOf(n.X)).(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := types.Unalias(p.Elem()).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "reflect" {
		return false
	}
	return named.Obj().Name() == "SliceHeader" || named.Obj().Name() == "StringHeader"
}

// conversion: T(x).
func (e *escWalk) conversion(k escHole, n *ast.CallExpr) {
	if len(n.Args) != 1 {
		return
	}
	x := n.Args[0]
	to, from := e.typeOf(n), e.typeOf(x)
	if to == nil || from == nil {
		e.expr(k, x)
		return
	}
	if e.needsIface(to, from) {
		e.convIface(k, x, escPos(x))
		return
	}
	if escIsTypeParam(to) || escIsTypeParam(from) {
		// whatever the instantiation makes of it: the value itself, or a new string or slice
		stringish := func(t types.Type) bool {
			if escIsTypeParam(t) {
				return true
			}
			switch u := t.Underlying().(type) {
			case *types.Basic:
				return u.Info()&types.IsString != 0
			case *types.Slice:
				return true
			}
			return false
		}
		if stringish(to) && stringish(from) {
			e.spill(k, n, escSiteString, escPos(x))
		}
		e.expr(k, x)
		return
	}
	tu, fu := to.Underlying(), from.Underlying()
	tb, _ := tu.(*types.Basic)
	fb, _ := fu.(*types.Basic)
	isString := func(b *types.Basic) bool { return b != nil && b.Info()&types.IsString != 0 }
	switch {
	case tb != nil && tb.Kind() == types.UnsafePointer && fb != nil && fb.Kind() == types.Uintptr:
		e.unsafeValue(k, x)
	case isString(tb) && !isString(fb), isString(fb) && !isString(tb):
		// string <-> []byte, []rune; string(rune): a new string or slice
		if tv := e.info.Types[n]; tv.Value == nil {
			e.spill(k, n, escSiteString, escPos(x))
		}
		e.discard(x)
	default:
		if _, ok := fu.(*types.Slice); ok {
			if _, ok := tu.(*types.Array); ok {
				e.expr(k.deref(), x) // the array is a copy of what the slice points to
				return
			}
		}
		e.expr(k, x)
	}
}
