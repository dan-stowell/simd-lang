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
func (g *gen) freeVars(lit *ast.FuncLit) []*types.Var {
	var out []*types.Var
	seen := map[*types.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
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

func (g *gen) childName(kind string) string {
	g.nclosure++
	return g.name + "." + kind + itoa(g.nclosure)
}

// child starts the generator of a function nested in this one.
func (g *gen) child(name string, sig *types.Signature) *gen {
	cg := g.c.newGen(name, sig, g.info, g.env)
	cg.out.sym = g.c.lookup(name, obj.ABIInternal, "_")
	cg.out.flags |= g.out.flags & obj.DUPOK
	return cg
}

func (g *gen) funcLit(e *ast.FuncLit) loc {
	sig := g.info.Types[e].Type.(*types.Signature)
	free := g.freeVars(e)
	for _, v := range free {
		if l := g.vars[v]; l.kind != lInd || l.off != 0 {
			giveUp("captured variable %s is not on the heap", v.Name())
		}
	}
	cg := g.child(g.childName("func"), sig)
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
	cg.compileFunc(nil, e.Pos(), e.Body.Rbrace, e.Body, func() { cg.block(e.Body.List) })
	if len(free) == 0 {
		g.a.lea(AX, msym(g.c.staticClosure(cg.out.sym), 0))
		return loc{kind: lReg, reg: AX}
	}
	p := g.newClosure(cg.out.sym, len(free))
	for i, v := range free {
		g.move(loc{kind: lInd, slot: p.off, off: int64(8 * (i + 1))}, loc{kind: lFrame, off: g.vars[v].slot}, unsafePtr)
	}
	return p
}

// saveCtx stores the closure object (in DX on entry) in a slot of its own.
func (g *gen) saveCtx() {
	j := g.newSlotBits(8, ptrShape)
	g.ctxSlot = loc{kind: lFrame, off: -g.slots[j].off}
	g.a.store(mb(BP, g.ctxSlot.off), DX, 8)
}

// bind makes a closure that performs the prepared call (callee c, arguments args of the ABI ai) when it is
// called. With forward set the closure has the signature sig: its own parameters follow the bound arguments
// and the results are returned (a method value); otherwise it is a func() (defer, go).
func (g *gen) bind(kind string, ai *abiInfo, args []loc, c callee, sig *types.Signature) loc {
	if sig == nil {
		sig = types.NewSignatureType(nil, nil, nil, nil, nil, false)
	}
	// what goes into the object, word by word
	type word struct {
		src   loc
		boxed types.Type // nil: the word is the value
	}
	var words []word
	calleeAt := -1
	switch c.kind {
	case 1:
		calleeAt = len(words)
		words = append(words, word{src: c.clo})
	case 2:
		calleeAt = len(words)
		words = append(words, word{src: c.iface}, word{src: c.iface.plus(8)})
	}
	argAt := make([]int, len(args))
	for i, a := range args {
		if c.kind == 2 && i == 0 {
			argAt[i] = calleeAt + 1 // the receiver is the interface's data word
			continue
		}
		t := ai.in[i].typ
		argAt[i] = len(words)
		switch {
		case g.c.sizeof(t) == 0:
			argAt[i] = -1
		case isPtrWord(t):
			words = append(words, word{src: a})
		default:
			words = append(words, word{src: a, boxed: t})
		}
	}
	cg := g.child(g.childName(kind), sig)
	cg.out.flags |= obj.WRAPPER
	cg.entry = cg.saveCtx
	cg.compileFunc(nil, g.lastPos, g.lastPos, nil, func() {
		ctx := cg.ctxSlot.off
		var cargs []loc
		for i := range args {
			at := argAt[i]
			switch {
			case at < 0:
				cargs = append(cargs, loc{kind: lZero})
			case words[at].boxed == nil:
				cargs = append(cargs, loc{kind: lInd, slot: ctx, off: int64(8 * (at + 1))})
			default:
				box := cg.ptrTemp()
				cg.a.load(R8, mb(BP, ctx), 8, false)
				cg.a.load(R8, mb(R8, int64(8*(at+1))), 8, false)
				cg.a.store(mb(BP, box.off), R8, 8)
				cargs = append(cargs, loc{kind: lInd, slot: box.off})
			}
		}
		nbound := len(args)
		for i := nbound; i < len(ai.in); i++ {
			cargs = append(cargs, cg.paramLoc(cg.abi.in[i-nbound]))
		}
		cc := c
		switch c.kind {
		case 1:
			cc.clo = loc{kind: lInd, slot: ctx, off: int64(8 * (calleeAt + 1))}
		case 2:
			cc.iface = loc{kind: lInd, slot: ctx, off: int64(8 * (calleeAt + 1))}
		}
		rs := cg.emitCall(ai, cargs, cc)
		if len(cg.resHome) > 0 {
			if len(rs) == 1 && (rs[0].kind == lReg) {
				cg.move(cg.resHome[0], rs[0], cg.abi.out[0].typ)
			} else {
				for i, r := range rs {
					cg.move(cg.resHome[i], r, cg.abi.out[i].typ)
				}
			}
		}
	})
	if len(words) == 0 {
		g.a.lea(AX, msym(g.c.staticClosure(cg.out.sym), 0))
		return loc{kind: lReg, reg: AX}
	}
	// boxes first (each allocation is a call), then the object
	srcs := make([]loc, len(words))
	for i, w := range words {
		if w.boxed == nil {
			srcs[i] = w.src
			continue
		}
		g.a.lea(AX, msym(g.c.typeSym(w.boxed), 0))
		g.callRT("newobject")
		box := g.ptrTemp()
		g.a.store(mb(BP, box.off), AX, 8)
		g.move(loc{kind: lInd, slot: box.off}, w.src, w.boxed)
		srcs[i] = box
	}
	p := g.newClosure(cg.out.sym, len(words))
	for i := range words {
		g.move(loc{kind: lInd, slot: p.off, off: int64(8 * (i + 1))}, srcs[i], unsafePtr)
	}
	return p
}

// methodValue: x.M as a function value with x bound.
func (g *gen) methodValue(e *ast.SelectorExpr, sel *types.Selection) loc {
	sig := g.typeOf(e).(*types.Signature)
	c, ai, recv := g.method(e, sel, sig)
	return g.bind("mv", ai, []loc{recv}, c, sig)
}

func (g *gen) deferStmt(s *ast.DeferStmt) {
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
	ai, args, c, _, _ := g.prepCall(call, false)
	if c.kind == 1 && len(args) == 0 && len(ai.out) == 0 {
		return c.clo // already a func()
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
