// closures.go: function literals, method values, defer, go, recover.
//
// A closure object is an array of pointer words: the code pointer, then one word per captured thing. A
// captured variable lives on the heap and the word is its address. A bound value (the receiver of a method
// value, the arguments of a deferred call) is the word itself when it is one pointer, else the address of a
// heap copy. So the object needs no type descriptor of its own: it is a [n]unsafe.Pointer.
package main

import (
	"go/ast"
	"go/types"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

var unsafePtr = types.Typ[types.UnsafePointer]

// freeVars: the variables of enclosing functions that the literal uses, in order of first use.
func (g *gen) freeVars(lit ast.Node) []*types.Var {
	var out []*types.Var
	seen := map[*types.Var]bool{}
	ast.Inspect(lit, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := g.info.Uses[id].(*types.Var)
		if !ok || v.IsField() || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() || seen[v] {
			return true
		}
		if v.Pos() >= lit.Pos() && v.Pos() < lit.End() {
			return true
		}
		if _, ok := g.vars[v]; !ok {
			return true // not a variable of this function (a package-level variable of a generic's package, say)
		}
		seen[v] = true
		out = append(out, v)
		return true
	})
	return out
}

// staticClosure: the funcval of a function that captures nothing.
func (c *compiler) staticClosure(target *obj.LSym) *obj.LSym {
	s := c.lookup(target.Name+"·f", obj.ABI0, "_")
	c.mu.Lock()
	if !s.OnList() {
		s.WriteAddr(c.ctxt, 0, 8, target, 0)
		s.Type = objabi.SRODATA
		s.Size = 8
		s.Set(obj.AttrDuplicateOK, true)
		s.Set(obj.AttrOnList, true)
		c.ctxt.Data = append(c.ctxt.Data, s)
	}
	c.mu.Unlock()
	return s
}

// newClosure allocates a closure object of n words after the code pointer; the result is a pointer temporary.
func (g *gen) newClosure(code *obj.LSym, n int) loc {
	g.a.lea(AX, msym(g.c.typeSym(unsafePtr), 0))
	g.a.movImm(BX, int64(n+1))
	g.callRT("newarray")
	p := g.ptrTemp()
	g.a.store(mb(BP, p.off), AX, 8)
	g.a.lea(R8, msym(code, 0))
	g.a.store(mb(AX, 0), R8, 8) // not a heap pointer: no write barrier
	return p
}

// childName names a function nested in this one as the stock compiler's ir.closureName does, so that
// names in tracebacks and profiles agree (runtime/pprof's tests look for F.func4): F.func1, F.func1.1,
// F-range1, F.gowrap1 / F.deferwrap1, each kind counted separately. (Method values and expressions are
// fc's own: F.mv1, F.mexpr1.)
func (g *gen) childName(kind string) string {
	i, suffix := 3, "."+kind
	switch kind {
	case "func":
		i = 0
		if g.isLit {
			suffix = "."
		}
	case "-range":
		i, suffix = 1, kind
	case "gowrap", "deferwrap":
		i = 2
	}
	g.ngen[i]++
	return g.name + suffix + itoa(g.ngen[i])
}

// child starts the generator of a function nested in this one.
func (g *gen) child(name string, sig *types.Signature) *gen {
	cg := g.c.newGen(name, sig, g.info, g.env)
	cg.esc = g.esc
	cg.out.sym = g.c.lookup(name, obj.ABIInternal, "_")
	cg.out.flags |= g.out.flags & obj.DUPOK
	return cg
}

func (g *gen) funcLit(e *ast.FuncLit) loc {
	sig := g.info.Types[e].Type.(*types.Signature)
	return g.closure("func", sig, e, e.Body, nil, func(cg *gen) { cg.block(e.Body.List) })
}

// closure compiles a nested function whose code is lit (searched for the variables it captures; extra
// lists more to capture) and returns its value.
func (g *gen) closure(kind string, sig *types.Signature, lit ast.Node, litBody *ast.BlockStmt, extra []*types.Var, body func(cg *gen)) loc {
	free := g.freeVars(lit)
	for _, v := range extra {
		dup := false
		for _, f := range free {
			dup = dup || f == v
		}
		if !dup {
			free = append(free, v)
		}
	}
	for _, v := range free {
		l := g.vars[v]
		if l.kind == lFrame && g.addrVars[v] {
			continue // captured in the frame: the closure does not outlive this activation
		}
		if l.kind != lInd || l.off != 0 {
			giveUp("captured variable %s is not on the heap", v.Name())
		}
	}
	e := litBody
	cg := g.child(g.childName(kind), sig)
	cg.isLit = true
	if len(free) > 0 {
		cg.entry = func() {
			cg.saveCtx()
			for i, v := range free {
				j := cg.newSlotBits(8, ptrShape)
				slot := -cg.slots[j].off
				cg.a.load(R8, mb(DX, int64(8*(i+1))), 8, false)
				cg.a.store(mb(BP, slot), R8, 8)
				cg.vars[v] = loc{kind: lInd, slot: slot}
			}
		}
	}
	cg.compileFunc(nil, e.Pos(), e.Rbrace, e, func() { body(cg) })
	if len(free) == 0 {
		g.a.lea(AX, msym(g.c.staticClosure(cg.out.sym), 0))
		return loc{kind: lReg, reg: AX}
	}
	if !g.siteHeap(lit) {
		// the closure object in the frame: an array of pointers
		ptrs := make([]bool, len(free)+1)
		for i := range ptrs {
			ptrs[i] = true
		}
		j := g.newSlotBits(int64(8*(len(free)+1)), ptrs)
		obj := loc{kind: lFrame, off: -g.slots[j].off}
		g.a.lea(R8, msym(cg.out.sym, 0))
		g.a.store(mb(BP, obj.off), R8, 8)
		for i, v := range free {
			if l := g.vars[v]; l.kind == lFrame {
				g.a.lea(R8, mb(BP, l.off))
			} else {
				g.a.load(R8, mb(BP, l.slot), 8, false)
			}
			g.a.store(mb(BP, obj.off+int64(8*(i+1))), R8, 8)
		}
		return g.frameAddr(obj)
	}
	p := g.newClosure(cg.out.sym, len(free))
	for i, v := range free {
		src := loc{kind: lFrame, off: g.vars[v].slot}
		if l := g.vars[v]; l.kind == lFrame {
			src = g.frameAddr(l) // (the analysis kept the variable in the frame but not the closure)
		}
		g.move(loc{kind: lInd, slot: p.off, off: int64(8 * (i + 1))}, src, unsafePtr)
	}
	return p
}

// saveCtx stores the closure object (in DX on entry) in a slot of its own.
func (g *gen) saveCtx() {
	j := g.newSlotBits(8, ptrShape)
	g.ctxSlot = loc{kind: lFrame, off: -g.slots[j].off}
	g.a.store(mb(BP, g.ctxSlot.off), DX, 8)
}

// bindValues makes a closure of signature sig (nil: func()) that carries the given values; body generates
// its code, given where the values are inside it.
func (g *gen) bindValues(kind string, vals []typedLoc, sig *types.Signature, body func(cg *gen, vals []loc)) loc {
	if sig == nil {
		sig = types.NewSignatureType(nil, nil, nil, nil, nil, false)
	}
	// what goes into the object, word by word: a pointer itself, anything else as the address of a copy
	at := make([]int, len(vals))
	var words []int // index of the value of each word
	for i, v := range vals {
		if g.c.sizeof(v.t) == 0 {
			at[i] = -1
			continue
		}
		at[i] = len(words)
		words = append(words, i)
	}
	cg := g.child(g.childName(kind), sig)
	cg.out.flags |= obj.WRAPPER
	cg.entry = cg.saveCtx
	cg.compileFunc(nil, g.lastPos, g.lastPos, nil, func() {
		ctx := cg.ctxSlot.off
		locs := make([]loc, len(vals))
		for i, v := range vals {
			switch {
			case at[i] < 0:
				locs[i] = loc{kind: lZero}
			case isPtrWord(v.t):
				locs[i] = loc{kind: lInd, slot: ctx, off: int64(8 * (at[i] + 1))}
			default:
				box := cg.ptrTemp()
				cg.a.load(R8, mb(BP, ctx), 8, false)
				cg.a.load(R8, mb(R8, int64(8*(at[i]+1))), 8, false)
				cg.a.store(mb(BP, box.off), R8, 8)
				locs[i] = loc{kind: lInd, slot: box.off}
			}
		}
		body(cg, locs)
	})
	if len(words) == 0 {
		g.a.lea(AX, msym(g.c.staticClosure(cg.out.sym), 0))
		return loc{kind: lReg, reg: AX}
	}
	// boxes first (each allocation is a call), then the object
	srcs := make([]loc, len(words))
	for w, i := range words {
		v := vals[i]
		if isPtrWord(v.t) {
			srcs[w] = v.l
			continue
		}
		g.a.lea(AX, msym(g.c.typeSym(v.t), 0))
		g.callRT("newobject")
		box := g.ptrTemp()
		g.a.store(mb(BP, box.off), AX, 8)
		g.move(loc{kind: lInd, slot: box.off}, v.l, v.t)
		srcs[w] = box
	}
	p := g.newClosure(cg.out.sym, len(words))
	for w := range words {
		g.move(loc{kind: lInd, slot: p.off, off: int64(8 * (w + 1))}, srcs[w], unsafePtr)
	}
	return p
}

// bind makes a closure that performs the prepared call (callee c, arguments args of the ABI ai) when it is
// called. With sig set the closure has that signature: its own parameters follow the bound arguments and
// the results are returned (a method value); otherwise it is a func() (defer, go).
func (g *gen) bind(kind string, ai *abiInfo, args []loc, c callee, sig *types.Signature) loc {
	var vals []typedLoc
	switch c.kind {
	case 1:
		vals = append(vals, typedLoc{c.clo, unsafePtr})
	case 2:
		vals = append(vals, typedLoc{c.iface, unsafePtr}, typedLoc{c.iface.plus(8), unsafePtr})
	}
	ncallee := len(vals)
	for i, a := range args {
		if c.kind == 2 && i == 0 {
			continue // the receiver is the interface's data word
		}
		vals = append(vals, typedLoc{a, ai.in[i].typ})
	}
	return g.bindValues(kind, vals, sig, func(cg *gen, locs []loc) {
		cc := c
		var cargs []loc
		switch c.kind {
		case 1:
			cc.clo = locs[0]
		case 2:
			cc.iface = locs[0] // the two words are adjacent in the closure object
			cargs = append(cargs, locs[1])
		}
		cargs = append(cargs, locs[ncallee:]...)
		nbound := len(args)
		for i := nbound; i < len(ai.in); i++ {
			cargs = append(cargs, cg.paramLoc(cg.abi.in[i-nbound]))
		}
		rs := cg.emitCall(ai, cargs, cc)
		for i, r := range rs {
			if i < len(cg.resHome) {
				cg.move(cg.resHome[i], r, cg.abi.out[i].typ)
			}
		}
	})
}

// methodValue: x.M as a function value with x bound.
func (g *gen) methodValue(e *ast.SelectorExpr, sel *types.Selection) loc {
	sig := g.typeOf(e).(*types.Signature)
	c, ai, recv := g.method(e, sel, sig)
	if c.kind == 2 {
		// a method value of a nil interface panics here, not when it is called
		g.a.load(R13, g.m(c.iface, R13), 8, false)
		g.a.nilcheck(R13)
	}
	return g.bind("mv", ai, []loc{recv}, c, sig)
}

func (g *gen) deferStmt(s *ast.DeferStmt) {
	if g.rf != nil {
		// in the body of a range-over-func loop: the call is deferred to the enclosing function, through
		// the token it got from the runtime on entry
		tok := g.root.deferTok
		if tok == nil {
			giveUp("defer in a loop body without a token")
		}
		clo := g.stash(g.deferred(s.Call, "deferwrap"), unsafePtr)
		tm := g.m(g.vars[tok], R13)
		g.a.load(BX, tm, 8, false)
		g.a.load(CX, tm.plus(8), 8, false)
		g.loadTo(AX, clo, unsafePtr)
		g.callRT("deferprocat")
		return
	}
	g.hasDefer = true
	clo := g.deferred(s.Call, "deferwrap")
	clo = g.stash(clo, unsafePtr)
	g.loadTo(AX, clo, unsafePtr)
	g.callRT("deferproc")
}

func (g *gen) goStmt(s *ast.GoStmt) {
	clo := g.deferred(s.Call, "gowrap")
	clo = g.stash(clo, unsafePtr)
	g.loadTo(AX, clo, unsafePtr)
	g.callRT("newproc")
}

// deferred: a func() that performs the call later, its callee and arguments evaluated now.
func (g *gen) deferred(call *ast.CallExpr, kind string) loc {
	if id := identOf(ast.Unparen(call.Fun)); id != nil {
		if b, ok := g.info.Uses[id].(*types.Builtin); ok {
			return g.deferredBuiltin(call, b.Name(), kind)
		}
	}
	ai, args, c, _, _ := g.prepCall(call, false)
	if c.kind == 2 {
		// a method of a nil interface: the panic is here, not when the call runs
		g.a.load(R13, g.m(c.iface, R13), 8, false)
		g.a.nilcheck(R13)
	}
	if c.kind == 1 && len(args) == 0 && len(ai.out) == 0 {
		return c.clo // already a func()
	}
	if c.kind == 0 && !c.abi0 && !c.asm && len(args) == 0 && len(ai.out) == 0 {
		// go f() / defer f() of a function without parameters or results: no wrapper, as the stock
		// compiler (typecheck.normalizeGoDeferCall), which keeps the wrappers' numbering the same
		g.a.lea(AX, msym(g.c.staticClosure(c.sym), 0))
		return loc{kind: lReg, reg: AX}
	}
	return g.bind(kind, ai, args, c, nil)
}

func (g *gen) recoverCall(e *ast.CallExpr) loc {
	g.callRT("gorecover")
	res := g.temp(g.c.anyType())
	g.a.store(mb(BP, res.off), AX, 8)
	g.a.store(mb(BP, res.off+8), BX, 8)
	return res
}

// methodExpr: T.M or (*T).M as a function whose first parameter is the receiver.
func (g *gen) methodExpr(e *ast.SelectorExpr, sel *types.Selection) loc {
	sig := g.typeOf(e).(*types.Signature)
	recvT := sig.Params().At(0).Type()
	o, path, _ := types.LookupFieldOrMethod(recvT, false, sel.Obj().Pkg(), sel.Obj().Name())
	fn, ok := o.(*types.Func)
	if !ok {
		giveUp("method expression %s of %v", sel.Obj().Name(), recvT)
	}
	// the method's own signature: sig without its first parameter
	var ps []*types.Var
	for i := 1; i < sig.Params().Len(); i++ {
		ps = append(ps, sig.Params().At(i))
	}
	msig := types.NewSignatureType(nil, nil, nil, types.NewTuple(ps...), sig.Results(), sig.Variadic())
	if !isInterface(recvT) && g.env == nil {
		// the method under this receiver type exists as a symbol: the method itself or its wrapper
		if sel := types.NewMethodSet(recvT).Lookup(fn.Pkg(), fn.Name()); sel != nil {
			if _, generic := g.funcRefIsGeneric(fn); !generic {
				g.a.lea(AX, msym(g.c.staticClosure(g.c.methodSymFor(recvT, sel)), 0))
				return loc{kind: lReg, reg: AX}
			}
		}
	}
	cg := g.child(g.childName("mexpr"), sig)
	cg.out.flags |= obj.WRAPPER
	cg.compileFunc(nil, e.Pos(), e.End(), nil, func() {
		c, ai, recv := cg.methodOn(cg.paramLoc(cg.abi.in[0]), recvT, fn, path, msig, e.Sel)
		args := []loc{recv}
		for i := 1; i < len(cg.abi.in); i++ {
			args = append(args, cg.paramLoc(cg.abi.in[i]))
		}
		rs := cg.emitCall(ai, args, c)
		for i, r := range rs {
			cg.move(cg.resHome[i], r, cg.abi.out[i].typ)
		}
	})
	g.a.lea(AX, msym(g.c.staticClosure(cg.out.sym), 0))
	return loc{kind: lReg, reg: AX}
}

// deferredBuiltin: defer or go of a built-in function: the arguments are evaluated now, the operation is
// carried out by the closure.
func (g *gen) deferredBuiltin(call *ast.CallExpr, name, kind string) loc {
	var vals []typedLoc
	arg := func(e ast.Expr, t types.Type) {
		vals = append(vals, typedLoc{g.inMem(g.keep(g.inMem(g.exprAs(e, t), t), t), t), t})
	}
	switch name {
	case "close":
		arg(call.Args[0], g.typeOf(call.Args[0]))
		return g.bindValues(kind, vals, nil, func(cg *gen, ls []loc) {
			cg.loadTo(AX, ls[0], vals[0].t)
			cg.callRT("closechan")
		})
	case "panic":
		arg(call.Args[0], g.c.anyType())
		return g.bindValues(kind, vals, nil, func(cg *gen, ls []loc) {
			m := cg.m(ls[0], R13)
			cg.a.load(AX, m, 8, false)
			cg.a.load(BX, m.plus(8), 8, false)
			cg.callRT("gopanic")
			cg.a.ud2()
		})
	case "recover":
		return g.bindValues(kind, nil, nil, func(cg *gen, ls []loc) { cg.callRT("gorecover") })
	case "delete":
		mt := g.typeOf(call.Args[0])
		arg(call.Args[0], mt)
		arg(call.Args[1], mt.Underlying().(*types.Map).Key())
		return g.bindValues(kind, vals, nil, func(cg *gen, ls []loc) {
			cg.mapArgs(mt, ls[0], ls[1])
			cg.callRT("mapdelete")
		})
	case "copy":
		dt := g.typeOf(call.Args[0])
		arg(call.Args[0], dt)
		arg(call.Args[1], g.typeOf(call.Args[1]))
		return g.bindValues(kind, vals, nil, func(cg *gen, ls []loc) {
			if ls[0].kind != lZero && ls[1].kind != lZero {
				cg.copyLocs(cg.keep(ls[0], dt), cg.keep(ls[1], vals[1].t), dt)
			}
		})
	case "clear":
		arg(call.Args[0], g.typeOf(call.Args[0]))
		return g.bindValues(kind, vals, nil, func(cg *gen, ls []loc) { cg.clearLoc(ls[0], vals[0].t) })
	case "print", "println":
		for _, a := range call.Args {
			arg(a, printType(g.typeOf(a)))
		}
		return g.bindValues(kind, vals, nil, func(cg *gen, ls []loc) {
			var vs []typedLoc
			for i, l := range ls {
				vs = append(vs, typedLoc{cg.inMem(l, vals[i].t), vals[i].t})
			}
			cg.printLocs(vs, name == "println")
		})
	}
	giveUp("deferred built-in %s", name)
	return loc{}
}
