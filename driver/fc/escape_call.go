// escape_call.go: calls. The arguments of a call whose callee is known flow where the callee's summary says
// (or, for a callee in the same batch or a function literal, to its parameters themselves); the arguments
// of any other call flow to the heap.
package main

import (
	"go/ast"
	"go/types"
)

// escCallee is what a call calls.
type escCallee struct {
	fn        *types.Func      // a declared function or method (its origin, if generic); or
	lit       *escFn           // a function literal that captures variables (it is in this batch); or
	sum       *escSummary      // a function literal that captures nothing; or none of them: unknown
	sig       *types.Signature // as the call sees it
	recv      ast.Expr         // x of x.M(...)
	recvShift int
	fun       ast.Expr // the callee expression, when it has a value that must be evaluated
}

// staticCallee: what a variable that is assigned once, where it is declared, always holds: a function
// literal the walk has seen already, or a declared function.
func (e *escWalk) staticCallee(id *ast.Ident, c *escCallee) bool {
	v := e.local(id)
	if v == nil || e.pk.reassigned[v] {
		return false
	}
	if lit := e.pk.staticLit[v]; lit != nil {
		c.lit = e.litFn[lit]
		c.sum = e.pk.litSum[lit]
		return c.lit != nil || c.sum != nil
	}
	if fn := e.pk.staticFunc[v]; fn != nil {
		c.fn = fn.Origin()
		return true
	}
	return false
}

func (e *escWalk) callee(fun ast.Expr) escCallee {
	c := escCallee{}
	c.sig, _ = escUnder(e.typeOf(fun)).(*types.Signature)
	f := ast.Unparen(fun)
	// an explicit instantiation
	switch x := f.(type) {
	case *ast.IndexExpr:
		if _, ok := escUnder(e.typeOf(x.X)).(*types.Signature); ok {
			f = ast.Unparen(x.X)
		}
	case *ast.IndexListExpr:
		f = ast.Unparen(x.X)
	}
	switch f := f.(type) {
	case *ast.Ident:
		switch o := e.info.Uses[f].(type) {
		case *types.Func:
			c.fn = o.Origin()
			return c
		case *types.Var:
			if e.staticCallee(f, &c) {
				return c
			}
		}
	case *ast.FuncLit:
		c.fun = fun
		return c // the walk of the literal names its function: see call
	case *ast.SelectorExpr:
		sel := e.info.Selections[f]
		if sel == nil {
			if o, ok := e.info.Uses[f.Sel].(*types.Func); ok {
				c.fn = o.Origin()
				return c
			}
			break
		}
		switch sel.Kind() {
		case types.MethodVal:
			m := sel.Obj().(*types.Func)
			c.recv = f.X
			c.recvShift = e.recvShift(f, sel)
			if !escIsIface(m.Type().(*types.Signature).Recv().Type()) && !escIsTypeParam(sel.Recv()) {
				c.fn = m.Origin()
			}
			return c
		case types.MethodExpr:
			m := sel.Obj().(*types.Func)
			if !escIsIface(m.Type().(*types.Signature).Recv().Type()) && !escIsTypeParam(sel.Recv()) {
				// the receiver is the first argument: the summary's first parameter too
				c.fn = m.Origin()
				// only when the receiver types agree: T.M where M has receiver T, (*T).M where *T
				mrecv := m.Type().(*types.Signature).Recv().Type()
				if c.sig == nil || c.sig.Params().Len() == 0 || !types.Identical(c.sig.Params().At(0).Type(), mrecv) {
					c.fn = nil
				}
			}
			return c
		}
	}
	c.fun = fun
	return c
}

// paramHole: where argument i of the call flows (-1: the receiver). ks is where the call's results flow.
func (e *escWalk) paramHole(c *escCallee, ks []escHole, i int) escHole {
	if c.lit != nil {
		if i < 0 || i >= len(c.lit.params) {
			return e.heapHole()
		}
		if l := c.lit.params[i]; l.v.Name() != "_" && l.v.Name() != "" {
			return l.hole()
		}
		return e.discardHole()
	}
	if c.sum != nil {
		return e.tagHole(c.sum, ks, i)
	}
	if c.fn == nil {
		return e.heapHole()
	}
	hasRecv := c.fn.Type().(*types.Signature).Recv() != nil
	// a method expression's receiver is argument 0; a method call's is argument -1
	j := i
	if hasRecv && c.recv != nil {
		j = i + 1
	}
	if j < 0 {
		return e.heapHole()
	}
	if fn := e.fns[c.fn]; fn != nil {
		if j >= len(fn.params) {
			return e.heapHole()
		}
		if l := fn.params[j]; l.v.Name() != "_" && l.v.Name() != "" {
			return l.hole()
		}
		return e.discardHole()
	}
	s := e.pk.summaryOf(c.fn)
	// The summary of a generic function is made on its body, where a value of a type parameter may hold
	// pointers. This call knows the type arguments: what is some dereferences away from an argument
	// whose type has no pointers that far away does not flow anywhere.
	if s != nil && i >= 0 && i < c.sig.Params().Len() && j < len(s.Params) {
		if fsig := c.fn.Type().(*types.Signature); fsig.TypeParams().Len() > 0 || fsig.RecvTypeParams().Len() > 0 {
			lk := s.Params[j]
			pt := c.sig.Params().At(i).Type()
			pruned := false
			if lk.Heap > 0 && !e.pk.ptrsAfter(pt, lk.Heap) {
				lk.Heap = -1
				pruned = true
			}
			for r, d := range lk.Results {
				if d > 0 && !e.pk.ptrsAfter(pt, d) {
					if !pruned {
						lk.Results = append([]int(nil), lk.Results...)
						pruned = true
					}
					lk.Results[r] = -1
				}
			}
			if pruned {
				return e.leaksHole(lk, ks)
			}
		}
	}
	return e.tagHole(s, ks, j)
}

// tagHole: where a summary says its parameter j flows.
func (e *escWalk) tagHole(s *escSummary, ks []escHole, j int) escHole {
	if s == nil || j < 0 || j >= len(s.Params) {
		return e.heapHole()
	}
	return e.leaksHole(s.Params[j], ks)
}

func (e *escWalk) leaksHole(lk escLeaks, ks []escHole) escHole {
	var tags []escHole
	if lk.Heap >= 0 {
		tags = append(tags, e.heapHole().shift(lk.Heap))
	}
	for r, d := range lk.Results {
		if d >= 0 && r < len(ks) {
			tags = append(tags, ks[r].shift(d))
		}
	}
	return e.tee(tags...)
}

// ptrsAfter: can a value of type t lead to a pointer after d dereferences?
func (pk *escPkg) ptrsAfter(t types.Type, d int) bool {
	if d <= 0 {
		return pk.hasPointers(t)
	}
	if t == nil || escIsTypeParam(t) {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString == 0 && pk.hasPointers(t) // a string's bytes hold none
	case *types.Pointer:
		return pk.hasPointers(t) && pk.ptrsAfter(u.Elem(), d-1)
	case *types.Slice:
		return pk.hasPointers(t) && pk.ptrsAfter(u.Elem(), d-1)
	case *types.Array:
		return u.Len() != 0 && pk.ptrsAfter(u.Elem(), d)
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if pk.ptrsAfter(u.Field(i).Type(), d) {
				return true
			}
		}
		return false
	case *types.Map:
		// what is in a map is in the heap already; a range over one flows its values (the stock
		// compiler's model, kept so that the two agree)
		return pk.ptrsAfter(u.Elem(), d-1)
	case *types.Chan:
		return false // what was sent is in the heap already
	}
	return true // an interface, a function, an unsafe.Pointer: anything
}

// arg flows an argument to k, and to extra if there is one (the closure object of a go or defer statement,
// which holds the arguments until the call).
func (e *escWalk) arg(k escHole, a ast.Expr, t types.Type, extra *escHole) {
	if extra != nil {
		k = e.tee(k, *extra)
	}
	e.exprTo(k, a, t)
}

// call: ks is where the results flow, nil if nowhere.
func (e *escWalk) call(ks []escHole, n *ast.CallExpr, extra *escHole) {
	fun := ast.Unparen(n.Fun)
	if tv, ok := e.info.Types[fun]; ok && tv.IsType() {
		k := e.discardHole()
		if len(ks) > 0 {
			k = ks[0]
		}
		if extra != nil {
			k = e.tee(k, *extra)
		}
		e.conversion(k, n)
		return
	}
	if name := e.builtinName(fun); name != "" {
		e.builtin(ks, n, name, extra)
		return
	}
	c := e.callee(n.Fun)
	if c.sig == nil {
		// not a call the type checker understood: everything escapes
		for _, a := range n.Args {
			e.expr(e.heapHole(), a)
		}
		return
	}
	if c.fun != nil {
		k := e.discardHole()
		if extra != nil {
			k = *extra
		}
		e.expr(k, c.fun)
		if lit, ok := ast.Unparen(c.fun).(*ast.FuncLit); ok {
			c.lit = e.litFn[lit]
			c.sum = e.pk.litSum[lit]
		}
	} else if extra != nil && (c.lit != nil || c.sum != nil) {
		e.expr(*extra, n.Fun)
	}
	if c.fn != nil && e.intrinsicCall(c.fn, n) {
		return
	}
	if c.recv != nil {
		k := e.paramHole(&c, ks, -1)
		if extra != nil {
			k = e.tee(k, *extra)
		}
		d := c.recvShift
		e.expr(k.shift(d), c.recv)
	}
	uintptrEsc := false
	if c.fn != nil {
		if d := e.pk.decls[c.fn]; d != nil {
			_, uintptrEsc = escPragmas(d)
		} else if s := e.pk.summaryOf(c.fn); s != nil {
			uintptrEsc = s.UintptrEscapes
		}
	}
	params := c.sig.Params()
	np := params.Len()
	// f(g()) where g has several results
	if len(n.Args) == 1 && np+btoi(c.sig.Variadic()) > 1 {
		if tup, ok := e.typeOf(n.Args[0]).(*types.Tuple); ok {
			gks := make([]escHole, tup.Len())
			var dddK escHole
			for j := 0; j < tup.Len(); j++ {
				var k escHole
				var pt types.Type
				if c.sig.Variadic() && j >= np-1 {
					if j == np-1 {
						dddK = e.spill(e.argHole(&c, ks, np-1, extra), n, escSiteDDD, n.Lparen)
					}
					k = dddK
					pt = params.At(np - 1).Type().(*types.Slice).Elem()
				} else {
					k = e.argHole(&c, ks, j, extra)
					pt = params.At(j).Type()
				}
				if e.needsIface(pt, tup.At(j).Type()) && !escDirectIface(tup.At(j).Type()) {
					// the interface's copy has no syntax of its own: it uses the heap
					e.forceSite(n.Args[0], n.Lparen)
					k = e.heapHole()
				}
				gks[j] = k
			}
			if g, ok := ast.Unparen(n.Args[0]).(*ast.CallExpr); ok {
				e.call(gks, g, nil)
			}
			e.results(&c, ks)
			return
		}
	}
	for i := 0; i < np; i++ {
		pt := params.At(i).Type()
		if c.sig.Variadic() && i == np-1 {
			if n.Ellipsis.IsValid() {
				if i < len(n.Args) {
					e.arg(e.paramHole(&c, ks, i), n.Args[i], pt, extra)
				}
				break
			}
			if len(n.Args) <= i {
				break // no variadic arguments: a nil slice
			}
			k := e.spill(e.argHole(&c, ks, i, extra), n, escSiteDDD, n.Lparen)
			var et types.Type
			if st, ok := escUnder(pt).(*types.Slice); ok {
				et = st.Elem()
				if !mentionsTypeParam(et) && int64(len(n.Args)-i)*e.pr.sizes.Sizeof(et) > escMaxImplicitStackVarSize {
					e.heapSiteWhy(k, "too large for stack")
				}
			}
			for _, a := range n.Args[i:] {
				if uintptrEsc && e.uintptrArg(a) {
					continue
				}
				e.exprTo(k, a, et)
			}
			break
		}
		if i >= len(n.Args) {
			break
		}
		if uintptrEsc && e.uintptrArg(n.Args[i]) {
			continue
		}
		e.arg(e.paramHole(&c, ks, i), n.Args[i], pt, extra)
	}
	e.results(&c, ks)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (e *escWalk) argHole(c *escCallee, ks []escHole, i int, extra *escHole) escHole {
	k := e.paramHole(c, ks, i)
	if extra != nil {
		k = e.tee(k, *extra)
	}
	return k
}

// results: the results of a callee whose body is in the graph flow to the call's.
func (e *escWalk) results(c *escCallee, ks []escHole) {
	if ks == nil {
		return
	}
	var fn *escFn
	if c.lit != nil {
		fn = c.lit
	} else if c.fn != nil {
		fn = e.fns[c.fn]
	}
	if fn == nil {
		return
	}
	for i, r := range fn.results {
		if i < len(ks) {
			e.flow(ks[i], r)
		}
	}
}

// uintptrArg: an argument uintptr(p) of a //go:uintptrescapes function, p an unsafe.Pointer: p escapes.
func (e *escWalk) uintptrArg(a ast.Expr) bool {
	call, ok := ast.Unparen(a).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if tv := e.info.Types[call.Fun]; !tv.IsType() {
		return false
	}
	tb, _ := escUnder(e.typeOf(call)).(*types.Basic)
	fb, _ := escUnder(e.typeOf(call.Args[0])).(*types.Basic)
	if tb == nil || fb == nil || tb.Kind() != types.Uintptr || fb.Kind() != types.UnsafePointer {
		return false
	}
	e.expr(e.heapHole(), call.Args[0])
	return true
}

// intrinsicCall: the functions the compiler expands itself. Their arguments are not values.
func (e *escWalk) intrinsicCall(fn *types.Func, n *ast.CallExpr) bool {
	if fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() {
	case "internal/abi":
		switch fn.Name() {
		case "FuncPCABI0", "FuncPCABIInternal":
			// of a function's name: a constant. (Of a function value the stock compiler makes an ordinary
			// call of a function without a body: the value escapes. So it does here.)
			if len(n.Args) == 1 {
				if id := escFuncName(n.Args[0]); id != nil {
					if _, ok := e.info.Uses[id].(*types.Func); ok {
						return true
					}
				}
			}
			return false
		case "EscapeNonString":
			// its argument escapes if it has a pointer that is not a string's
			for _, a := range n.Args {
				k := e.heapHole()
				if !escNonStringPointers(e.pk, e.typeOf(a)) {
					k = e.discardHole()
				}
				e.expr(k, a)
			}
			return true
		}
	}
	return false
}

func escFuncName(x ast.Expr) *ast.Ident {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	}
	return nil
}

func escNonStringPointers(pk *escPkg, t types.Type) bool {
	if t == nil || escIsTypeParam(t) {
		return true
	}
	if !pk.hasPointers(t) {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString == 0
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if escNonStringPointers(pk, u.Field(i).Type()) {
				return true
			}
		}
		return false
	case *types.Array:
		return escNonStringPointers(pk, u.Elem())
	}
	return true
}

func (e *escWalk) builtinName(fun ast.Expr) string {
	var id *ast.Ident
	switch f := fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return ""
	}
	if b, ok := e.info.Uses[id].(*types.Builtin); ok {
		return b.Name()
	}
	return ""
}

func (e *escWalk) builtin(ks []escHole, n *ast.CallExpr, name string, extra *escHole) {
	k0 := e.discardHole()
	if len(ks) > 0 {
		k0 = ks[0]
	}
	args := n.Args
	elemPtrs := func(a ast.Expr) bool {
		switch u := escUnder(e.typeOf(a)).(type) {
		case *types.Slice:
			return e.pk.hasPointers(u.Elem())
		case *types.Basic:
			return false // a string's bytes
		}
		return true
	}
	switch name {
	case "append":
		if len(args) == 0 {
			return
		}
		// the slice itself may be the result; or its elements are copied to a new array in the heap
		k := k0
		if elemPtrs(args[0]) {
			k = e.tee(k0, e.heapHole().deref())
		}
		e.arg(k, args[0], nil, extra)
		var et types.Type
		if st, ok := escUnder(e.typeOf(n)).(*types.Slice); ok {
			et = st.Elem()
		}
		if n.Ellipsis.IsValid() {
			k := e.discardHole()
			if len(args) > 1 && elemPtrs(args[1]) {
				k = e.heapHole().deref()
			}
			if len(args) > 1 {
				e.arg(k, args[1], nil, extra)
			}
		} else {
			for _, a := range args[1:] {
				e.arg(e.heapHole(), a, et, extra)
			}
		}
		// the array append may allocate
		e.spill(k0, n, escSiteAppend, n.Lparen)
	case "copy":
		if len(args) != 2 {
			return
		}
		e.arg(e.discardHole(), args[0], nil, extra)
		k := e.discardHole()
		if elemPtrs(args[1]) {
			k = e.heapHole().deref()
		}
		e.arg(k, args[1], nil, extra)
	case "panic":
		if len(args) == 1 {
			e.arg(e.heapHole(), args[0], types.NewInterfaceType(nil, nil), extra)
		}
	case "min", "max":
		for _, a := range args {
			e.arg(k0, a, nil, extra)
		}
	case "new":
		if len(args) != 1 {
			return
		}
		k := e.spill(k0, n, escSiteNew, n.Lparen)
		if p, ok := escUnder(e.typeOf(n)).(*types.Pointer); ok {
			e.implicitSize(k, p.Elem())
		}
		if tv := e.info.Types[args[0]]; !tv.IsType() {
			e.expr(k, args[0]) // new(expr)
		}
	case "make":
		if len(args) == 0 {
			return
		}
		for _, a := range args[1:] {
			e.arg(e.discardHole(), a, nil, extra)
		}
		t := e.typeOf(n)
		switch u := escUnder(t).(type) {
		case *types.Slice:
			k := e.spill(k0, n, escSiteMakeSlice, n.Lparen)
			e.makeSize(k, n, u)
		case *types.Map:
			e.spill(k0, n, escSiteMakeMap, n.Lparen)
		case *types.Chan:
		default:
			// a type parameter: any of them
			k := e.spill(k0, n, escSiteMakeSlice, n.Lparen)
			e.heapSiteWhy(k, "make of a type parameter")
		}
	case "Add", "Slice", "String":
		if len(args) == 2 {
			e.arg(k0, args[0], nil, extra)
			e.arg(e.discardHole(), args[1], nil, extra)
		}
	case "StringData", "SliceData":
		if len(args) == 1 {
			e.arg(k0, args[0], nil, extra)
		}
	case "Sizeof", "Alignof", "Offsetof":
	default: // len cap real imag complex close clear delete print println recover
		for _, a := range args {
			e.arg(e.discardHole(), a, nil, extra)
		}
	}
}

// makeSize: a slice made with a size that is not a constant, or too large, uses the heap.
func (e *escWalk) makeSize(k escHole, n *ast.CallExpr, t *types.Slice) {
	if mentionsTypeParam(t.Elem()) {
		e.heapSiteWhy(k, "element size depends on a type parameter")
		return
	}
	size := e.pr.sizes.Sizeof(t.Elem())
	if size == 0 {
		e.heapSiteWhy(k, "zero-sized element")
		return
	}
	r := n.Args[len(n.Args)-1]
	if len(n.Args) < 2 {
		e.heapSiteWhy(k, "non-constant size")
		return
	}
	tv := e.info.Types[r]
	if tv.Value == nil {
		e.heapSiteWhy(k, "non-constant size")
		return
	}
	v, ok := constantInt64(tv)
	if !ok || v < 0 || v > escMaxImplicitStackVarSize/size {
		e.heapSiteWhy(k, "too large for stack")
	}
}
