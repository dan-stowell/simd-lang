// call.go: calls, conversions and the built-in functions.
package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"cmd/internal/obj"
)

// ifaceMethods lists an interface's methods in the order of its method table (the stock compiler's order:
// exported names first, then by name, then by package path).
func ifaceMethods(it *types.Interface) []*types.Func {
	ms := make([]*types.Func, it.NumMethods())
	for i := range ms {
		ms[i] = it.Method(i)
	}
	sort.Slice(ms, func(i, j int) bool {
		a, b := ms[i], ms[j]
		if a.Exported() != b.Exported() {
			return a.Exported()
		}
		if a.Name() != b.Name() {
			return a.Name() < b.Name()
		}
		if !a.Exported() {
			return a.Pkg().Path() < b.Pkg().Path()
		}
		return false
	})
	return ms
}

type callee struct {
	sym   *obj.LSym // a direct call
	clo   loc       // a closure value (a pointer to a funcval), stashed
	iface loc       // an interface value, stashed: call method number imethod
	imeth int
	kind  int  // 0 direct, 1 closure, 2 interface
	abi0  bool // a direct call of an assembly function with ABI0
}

// emitCall moves the arguments to where the ABI wants them, calls, and collects the results.
// Arguments must be safe from each other: not in registers.
func (g *gen) emitCall(ai *abiInfo, args []loc, c callee) []loc {
	if ai.argWidth > g.maxarg {
		g.maxarg = ai.argWidth
	}
	for i, p := range ai.in {
		if args[i].kind == lReg || args[i].kind == lRegInd {
			panic("emitCall: argument in a register")
		}
		if p.parts == nil {
			g.move(loc{kind: lOut, off: p.off}, args[i], p.typ)
		}
	}
	for i, p := range ai.in {
		a := args[i]
		for _, q := range p.parts {
			switch {
			case a.kind == lZero:
				if q.float {
					g.a.xorps(q.reg, q.reg)
				} else {
					g.a.movImm(q.reg, 0)
				}
			case a.kind == lImm:
				g.a.movImm(q.reg, a.imm)
			case q.float:
				g.a.fload(q.reg, g.m(a.plus(q.off), R13), q.size)
			default:
				g.a.load(q.reg, g.m(a.plus(q.off), q.reg), q.size, false)
			}
		}
	}
	switch c.kind {
	case 0:
		g.a.call(c.sym)
		if c.abi0 {
			// assembly may have used the registers ABIInternal fixes
			g.a.xorps(X15, X15)
			g.a.loadG()
		}
	case 1:
		g.loadTo(DX, c.clo, types.Typ[types.UnsafePointer])
		g.a.load(R12, mb(DX, 0), 8, false)
		g.a.callReg(R12)
	case 2:
		g.a.load(R12, g.m(c.iface, R12), 8, false)
		g.a.load(R12, mb(R12, int64(24+8*c.imeth)), 8, false)
		g.a.callReg(R12)
	}
	// results
	var rs []loc
	for _, p := range ai.out {
		size := g.c.sizeof(p.typ)
		switch {
		case size == 0:
			rs = append(rs, loc{kind: lZero})
		case p.parts == nil:
			tmp := g.temp(p.typ)
			g.move(tmp, loc{kind: lOut, off: p.off}, p.typ)
			rs = append(rs, tmp)
		case len(ai.out) == 1 && len(p.parts) == 1 && (isWord(p.typ) || isFloat(p.typ)):
			rs = append(rs, loc{kind: lReg, reg: p.parts[0].reg})
		default:
			tmp := g.temp(p.typ)
			for _, q := range p.parts {
				m := mb(BP, tmp.off+q.off)
				if q.float {
					g.a.fstore(m, q.reg, q.size)
				} else {
					g.a.store(m, q.reg, q.size)
				}
			}
			rs = append(rs, tmp)
		}
	}
	return rs
}

// rtCall calls a runtime function with the given argument values; it returns the results.
func (g *gen) rtCall(name string, args ...loc) []loc {
	fn := g.c.rtFunc(name)
	ai := g.c.abiOf(fn.Type().(*types.Signature))
	return g.emitCall(ai, args, callee{sym: g.c.rt(name)})
}

func (g *gen) call(e *ast.CallExpr) []loc {
	ai, args, c, rs, done := g.prepCall(e, true)
	if done {
		return rs
	}
	return g.emitCall(ai, args, c)
}

// prepCall evaluates the callee and the arguments of a call. A conversion or a built-in is carried out on
// the spot (done is true and rs its results) when now is set, and given up on otherwise.
func (g *gen) prepCall(e *ast.CallExpr, now bool) (ai *abiInfo, args []loc, c callee, rs []loc, done bool) {
	fun := ast.Unparen(e.Fun)
	if tv := g.info.Types[fun]; tv.IsType() {
		if !now {
			giveUp("deferred conversion")
		}
		return nil, nil, c, []loc{g.convert(e.Args[0], g.subst(tv.Type))}, true
	}
	// F[int](x): the callee is under the index expression
	switch ix := fun.(type) {
	case *ast.IndexExpr:
		if id := identOf(ix.X); id != nil {
			if _, ok := g.info.Instances[id]; ok {
				fun = ast.Unparen(ix.X)
			}
		}
	case *ast.IndexListExpr:
		fun = ast.Unparen(ix.X)
	}
	if id := identOf(fun); id != nil {
		if b, ok := g.info.Uses[id].(*types.Builtin); ok {
			if !now {
				giveUp("deferred built-in %s", b.Name())
			}
			return nil, nil, c, g.builtin(e, b.Name()), true
		}
	}
	sig, ok := g.typeOf(e.Fun).Underlying().(*types.Signature)
	if !ok {
		giveUp("call of %T", fun)
	}
	var recvArg bool
	direct := func(fn *types.Func) {
		if g.c.isIntrinsic(fn) {
			giveUp("compiler intrinsic %s", fn.FullName())
		}
		sym, fsig := g.funcRef(fn, identOf(fun))
		c = callee{sym: sym}
		in := []types.Type{}
		if r := fsig.Recv(); r != nil {
			in = append(in, r.Type())
		}
		for i := 0; i < sig.Params().Len(); i++ {
			in = append(in, sig.Params().At(i).Type())
		}
		var out []types.Type
		for i := 0; i < sig.Results().Len(); i++ {
			out = append(out, sig.Results().At(i).Type())
		}
		ai = g.c.abiOfTypes(in, out)
	}
	if id := identOf(fun); id != nil {
		if fn, ok := g.info.Uses[id].(*types.Func); ok && g.info.Selections[selOf(fun)] == nil && g.c.isIntrinsic(fn) {
			if !now {
				giveUp("deferred intrinsic %s", fn.Name())
			}
			return nil, nil, c, g.intrinsic(fn, e), true
		}
	}
	switch f := fun.(type) {
	case *ast.Ident:
		if fn, ok := g.info.Uses[f].(*types.Func); ok {
			direct(fn)
		}
	case *ast.SelectorExpr:
		sel := g.info.Selections[f]
		if sel == nil {
			if fn, ok := g.info.Uses[f.Sel].(*types.Func); ok {
				direct(fn)
			}
			break
		}
		if sel.Kind() != types.MethodVal {
			break // a field of function type, or a method expression: a function value
		}
		var rl loc
		c, ai, rl = g.method(f, sel, sig)
		args = append(args, rl)
		recvArg = true
	}
	if ai == nil {
		// a function value
		fl := g.stashValue(g.expr(fun), g.typeOf(fun))
		c = callee{kind: 1, clo: fl}
		ai = g.c.abiOf(sig)
	}
	_ = recvArg
	args = append(args, g.args(e, sig)...)
	return ai, args, c, nil, false
}

func hasTypeParams(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok {
		return n.TypeArgs().Len() > 0 || n.TypeParams().Len() > 0
	}
	return false
}

// addrOfLoc: the address of a memory location, as a pointer value.
func (g *gen) addrOfLoc(l loc) loc {
	switch l.kind {
	case lInd:
		if l.off == 0 {
			return loc{kind: lFrame, off: l.slot}
		}
		g.a.load(AX, mb(BP, l.slot), 8, false)
		if l.off >= 4096 {
			g.a.nilcheck(AX)
		}
		g.a.lea(AX, mb(AX, l.off))
		return loc{kind: lReg, reg: AX}
	case lRegInd:
		if l.off != 0 || l.reg != AX {
			g.a.lea(AX, mb(l.reg, l.off))
		}
		return loc{kind: lReg, reg: AX}
	case lSym:
		g.a.lea(AX, msym(l.sym, l.off))
		return loc{kind: lReg, reg: AX}
	}
	giveUp("address of a value in the frame")
	return loc{}
}

// args evaluates the arguments of a call to a function of signature sig, in order, each safe from the rest.
func (g *gen) args(e *ast.CallExpr, sig *types.Signature) []loc {
	np := sig.Params().Len()
	var out []loc
	if len(e.Args) == 1 && isTuple(g.info.Types[e.Args[0]].Type) {
		if tup, ok := g.typeOf(e.Args[0]).(*types.Tuple); ok {
			// f(g()) with several results
			rs := g.multi(e.Args[0])
			if sig.Variadic() {
				giveUp("multi-value call passed to a variadic function")
			}
			for i := 0; i < tup.Len(); i++ {
				pt := sig.Params().At(i).Type()
				out = append(out, g.keep(g.convertImplicit(rs[i].l, rs[i].t, pt), pt))
			}
			return out
		}
	}
	nfixed := np
	if sig.Variadic() {
		nfixed--
	}
	laterPure := func(i int) bool {
		for _, a := range e.Args[i+1:] {
			if !g.pure(a) {
				return false
			}
		}
		return true
	}
	hold := func(l loc, t types.Type, i int) loc {
		if laterPure(i) {
			return g.stash(l, t)
		}
		return g.stashValue(l, t)
	}
	for i := 0; i < nfixed; i++ {
		pt := sig.Params().At(i).Type()
		out = append(out, hold(g.exprAs(e.Args[i], pt), pt, i))
	}
	if !sig.Variadic() {
		return out
	}
	st := sig.Params().At(np - 1).Type()
	if e.Ellipsis.IsValid() {
		out = append(out, hold(g.exprAs(e.Args[nfixed], st), st, nfixed))
		return out
	}
	extra := e.Args[nfixed:]
	if len(extra) == 0 {
		out = append(out, loc{kind: lZero})
		return out
	}
	et := st.Underlying().(*types.Slice).Elem()
	sl := g.newBacking(et, int64(len(extra)), st)
	es := g.c.sizeof(et)
	for i, a := range extra {
		g.exprTo(a, loc{kind: lInd, slot: sl.off, off: int64(i) * es}, et)
	}
	out = append(out, sl)
	return out
}

func isTuple(t types.Type) bool {
	_, ok := t.(*types.Tuple)
	return ok
}

// newBacking allocates an array of n elements and returns a slice temporary (type st) over it.
func (g *gen) newBacking(et types.Type, n int64, st types.Type) loc {
	g.a.lea(AX, msym(g.c.typeSym(et), 0))
	g.a.movImm(BX, n)
	g.callRT("newarray")
	sl := g.temp(st)
	g.a.store(mb(BP, sl.off), AX, 8)
	g.a.storeImm(mb(BP, sl.off+8), n, 8)
	g.a.storeImm(mb(BP, sl.off+16), n, 8)
	return sl
}

// ---- conversions

func (g *gen) convert(arg ast.Expr, to types.Type) loc {
	from := g.typeOf(arg)
	if isInterface(to) {
		return g.exprAs(arg, to)
	}
	l := g.expr(arg)
	if b, ok := from.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		from = types.Default(from)
	}
	fu, tu := from.Underlying(), to.Underlying()
	fb, _ := fu.(*types.Basic)
	tb, _ := tu.(*types.Basic)
	isInt := func(b *types.Basic) bool { return b != nil && b.Info()&types.IsInteger != 0 }
	isFlt := func(b *types.Basic) bool { return b != nil && b.Info()&types.IsFloat != 0 }
	isStr := func(b *types.Basic) bool { return b != nil && b.Info()&types.IsString != 0 }
	switch {
	case isInt(fb) && isInt(tb):
		if l.kind == lImm {
			return l
		}
		fs, ts := g.c.sizeof(from), g.c.sizeof(to)
		if ts > fs || (ts == fs && isSigned(from) == isSigned(to)) || (ts == 8 && fs == 8) {
			if ts == fs || l.isMem() && isSigned(from) == isSigned(to) && false {
				return l // same representation
			}
		}
		g.loadTo(AX, l, from)
		g.a.extend(AX, int(ts), isSigned(to))
		return loc{kind: lReg, reg: AX}
	case isInt(fb) && isFlt(tb):
		g.loadTo(AX, l, from)
		ts := int(g.c.sizeof(to))
		if !isSigned(from) && g.c.sizeof(from) == 8 {
			// unsigned 64-bit: halve (keeping the low bit sticky), convert, double
			g.a.testRR(8, AX, AX)
			neg := g.a.jcc(ccS)
			g.a.cvtsi2f(ts, X0, AX, 8)
			done := g.a.jmp()
			g.a.patch(neg, g.a.pc())
			g.a.movRR(CX, AX)
			g.a.shiftImm(5, 8, AX, 1)
			g.a.aluImm(aAND, 4, CX, 1)
			g.a.aluRR(aOR, 8, AX, CX)
			g.a.cvtsi2f(ts, X0, AX, 8)
			g.a.farith(0x58, ts, X0, X0)
			g.a.patch(done, g.a.pc())
		} else {
			g.a.cvtsi2f(ts, X0, AX, 8)
		}
		return loc{kind: lReg, reg: X0}
	case isFlt(fb) && isInt(tb):
		fs := int(g.c.sizeof(from))
		g.loadTo(X0, l, from)
		if !isSigned(to) && g.c.sizeof(to) == 8 {
			// unsigned 64-bit: values of 2^63 and up are converted after subtracting 2^63
			two63 := g.constLoc(constantFloat(9223372036854775808.0), from)
			g.loadTo(X1, two63, from)
			g.a.ucomis(fs, X0, X1)
			big := g.a.jcc(ccAE)
			g.a.cvttf2si(fs, AX, X0, 8)
			done := g.a.jmp()
			g.a.patch(big, g.a.pc())
			g.a.farith(0x5C, fs, X0, X1)
			g.a.cvttf2si(fs, AX, X0, 8)
			g.a.movImm(CX, -1<<63)
			g.a.aluRR(aXOR, 8, AX, CX)
			g.a.patch(done, g.a.pc())
		} else {
			g.a.cvttf2si(fs, AX, X0, 8)
			g.a.extend(AX, int(g.c.sizeof(to)), isSigned(to))
		}
		return loc{kind: lReg, reg: AX}
	case isFlt(fb) && isFlt(tb):
		if g.c.sizeof(from) == g.c.sizeof(to) {
			return l
		}
		g.loadTo(X0, l, from)
		g.a.cvtf2f(int(g.c.sizeof(from)), X0, X0)
		return loc{kind: lReg, reg: X0}
	case isStr(tb) && isInt(fb):
		g.loadTo(BX, l, from)
		g.a.movImm(AX, 0)
		g.callRT("intstring")
		return g.pairResult(to)
	case isStr(tb) && !isStr(fb):
		sl, ok := fu.(*types.Slice)
		if !ok {
			giveUp("conversion %v to string", from)
		}
		if l.kind == lZero {
			return l
		}
		l = g.stash(l, from)
		m := g.m(l, R13)
		eb := sl.Elem().Underlying().(*types.Basic)
		if eb.Kind() == types.Byte {
			g.a.load(BX, m, 8, false)
			g.a.load(CX, m.plus(8), 8, false)
			g.a.movImm(AX, 0)
			g.callRT("slicebytetostring")
		} else {
			g.a.load(BX, m, 8, false)
			g.a.load(CX, m.plus(8), 8, false)
			g.a.load(DI, m.plus(16), 8, false)
			g.a.movImm(AX, 0)
			g.callRT("slicerunetostring")
		}
		return g.pairResult(to)
	case isStr(fb) && !isStr(tb):
		sl, ok := tu.(*types.Slice)
		if !ok {
			giveUp("conversion of string to %v", to)
		}
		if l.kind == lZero {
			l = g.zeroTemp(from)
		}
		l = g.stash(l, from)
		m := g.m(l, R13)
		g.a.load(BX, m, 8, false)
		g.a.load(CX, m.plus(8), 8, false)
		g.a.movImm(AX, 0)
		if sl.Elem().Underlying().(*types.Basic).Kind() == types.Byte {
			g.callRT("stringtoslicebyte")
		} else {
			g.callRT("stringtoslicerune")
		}
		res := g.temp(to)
		g.a.store(mb(BP, res.off), AX, 8)
		g.a.store(mb(BP, res.off+8), BX, 8)
		g.a.store(mb(BP, res.off+16), CX, 8)
		return res
	}
	if isComplex(from) || isComplex(to) {
		if g.c.sizeof(from) == g.c.sizeof(to) {
			return l
		}
		return g.complexConv(l, from, to)
	}
	if _, ok := fu.(*types.Slice); ok {
		switch tt := tu.(type) {
		case *types.Pointer:
			if arr, ok := tt.Elem().Underlying().(*types.Array); ok {
				return g.sliceToArrayPtr(l, from, arr)
			}
		case *types.Array:
			p := g.sliceToArrayPtr(l, from, tt)
			if tt.Len() == 0 {
				return loc{kind: lZero}
			}
			return g.deref(p)
		}
	}
	// the same representation under another type
	return l
}

func (g *gen) sliceToArrayPtr(l loc, from types.Type, arr *types.Array) loc {
	if l.kind == lZero {
		l = g.zeroTemp(from)
	}
	l = g.stash(l, from)
	m := g.m(l, R13)
	g.a.load(BX, m.plus(8), 8, false)
	g.a.movImm(CX, arr.Len())
	g.a.aluRR(aCMP, 8, BX, CX)
	ok := g.a.jcc(ccAE)
	g.boundsFail("goPanicSliceConvert", false)
	g.a.patch(ok, g.a.pc())
	g.a.load(AX, g.m(l, R13), 8, false)
	return loc{kind: lReg, reg: AX}
}

// pairResult stores AX, BX (a string result) into a temporary of type t.
func (g *gen) pairResult(t types.Type) loc {
	res := g.temp(t)
	g.a.store(mb(BP, res.off), AX, 8)
	g.a.store(mb(BP, res.off+8), BX, 8)
	return res
}

// ---- built-in functions

func (g *gen) builtin(e *ast.CallExpr, name string) []loc {
	one := func(l loc) []loc { return []loc{l} }
	intT := types.Typ[types.Int]
	switch name {
	case "len", "cap":
		x := e.Args[0]
		xt := g.typeOf(x)
		l := g.expr(x)
		switch u := xt.Underlying().(type) {
		case *types.Basic, *types.Slice:
			if l.kind == lZero {
				return one(loc{kind: lImm, imm: 0})
			}
			off := int64(8)
			if name == "cap" {
				off = 16
			}
			g.a.load(AX, g.m(l, R13).plus(off), 8, false)
			return one(loc{kind: lReg, reg: AX})
		case *types.Array:
			return one(loc{kind: lImm, imm: u.Len()})
		case *types.Pointer:
			return one(loc{kind: lImm, imm: u.Elem().Underlying().(*types.Array).Len()})
		case *types.Map:
			// the count is the first word of the map header
			g.loadTo(AX, l, xt)
			g.a.testRR(8, AX, AX)
			z := g.a.jcc(ccE)
			g.a.load(AX, mb(AX, 0), 8, false)
			g.a.patch(z, g.a.pc())
			return one(loc{kind: lReg, reg: AX})
		case *types.Chan:
			g.loadTo(AX, l, xt)
			if name == "len" {
				g.callRT("chanlen")
			} else {
				g.callRT("chancap")
			}
			return one(loc{kind: lReg, reg: AX})
		}
	case "new":
		t := g.typeOf(e.Args[0])
		if !g.info.Types[e.Args[0]].IsType() {
			giveUp("new(expr)")
		}
		g.a.lea(AX, msym(g.c.typeSym(t), 0))
		g.callRT("newobject")
		return one(loc{kind: lReg, reg: AX})
	case "panic":
		l := g.exprAs(e.Args[0], g.c.anyType())
		l = g.stash(l, g.c.anyType())
		m := g.m(l, R13)
		g.a.load(AX, m, 8, false)
		g.a.load(BX, m.plus(8), 8, false)
		g.callRT("gopanic")
		g.a.ud2()
		return nil
	case "append":
		return one(g.appendCall(e))
	case "copy":
		return one(g.copyCall(e))
	case "make":
		return one(g.makeCall(e))
	case "delete":
		g.mapDelete(e)
		return nil
	case "min", "max":
		return one(g.minmax(e, name))
	case "close":
		g.loadTo(AX, g.expr(e.Args[0]), g.typeOf(e.Args[0]))
		g.callRT("closechan")
		return nil
	case "clear":
		g.clearCall(e)
		return nil
	case "print", "println":
		g.printCall(e, name == "println")
		return nil
	case "recover":
		return one(g.recoverCall(e))
	case "complex":
		return one(g.makeComplex(e))
	case "real", "imag":
		return one(g.complexPart(e, name == "imag"))
	case "Sizeof":
		return one(loc{kind: lImm, imm: g.c.sizeof(g.typeOf(e.Args[0]))})
	case "Alignof":
		return one(loc{kind: lImm, imm: g.c.alignof(g.typeOf(e.Args[0]))})
	case "Offsetof":
		se := ast.Unparen(e.Args[0]).(*ast.SelectorExpr)
		sel := g.info.Selections[se]
		t := g.typeOf(se.X)
		off := int64(0)
		for _, i := range sel.Index() {
			if p, ok := t.Underlying().(*types.Pointer); ok {
				t = p.Elem()
				off = 0
			}
			st := t.Underlying().(*types.Struct)
			off += g.c.fieldOffsets(st)[i]
			t = st.Field(i).Type()
		}
		return one(loc{kind: lImm, imm: off})
	case "Add": // unsafe.Add(p, n)
		pt := g.typeOf(e.Args[0])
		nt := g.typeOf(e.Args[1])
		if b, ok := nt.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			nt = intT
		}
		g.operands(e.Args[0], e.Args[1], pt, nt, AX, CX)
		g.a.aluRR(aADD, 8, AX, CX)
		return one(loc{kind: lReg, reg: AX})
	case "StringData", "SliceData":
		l := g.expr(e.Args[0])
		if l.kind == lZero {
			return one(l)
		}
		g.a.load(AX, g.m(l, R13), 8, false)
		return one(loc{kind: lReg, reg: AX})
	case "String", "Slice":
		// unsafe.String(p, n), unsafe.Slice(p, n): checked by the runtime, then the header is built
		pt := g.typeOf(e.Args[0])
		nt := g.typeOf(e.Args[1])
		if b, ok := nt.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			nt = intT
		}
		p := g.stashValue(g.expr(e.Args[0]), pt)
		n := g.wordTemp()
		g.loadTo(AX, g.expr(e.Args[1]), nt)
		g.a.store(mb(BP, n.off), AX, 8)
		rt := g.typeOf(e)
		if name == "String" {
			g.loadTo(AX, p, pt)
			g.a.load(BX, mb(BP, n.off), 8, false)
			g.callRT("unsafestringcheckptr")
		} else {
			et := pt.Underlying().(*types.Pointer).Elem()
			g.a.lea(AX, msym(g.c.typeSym(et), 0))
			g.loadTo(BX, p, pt)
			g.a.load(CX, mb(BP, n.off), 8, false)
			g.callRT("unsafeslicecheckptr")
		}
		res := g.temp(rt)
		g.loadTo(AX, p, pt)
		g.a.store(mb(BP, res.off), AX, 8)
		g.a.load(AX, mb(BP, n.off), 8, false)
		g.a.store(mb(BP, res.off+8), AX, 8)
		if name == "Slice" {
			g.a.store(mb(BP, res.off+16), AX, 8)
		}
		return one(res)
	}
	giveUp("built-in %s", name)
	return nil
}

// appendCall: append(s, x...) or append(s, a, b, c).
func (g *gen) appendCall(e *ast.CallExpr) loc {
	st := g.typeOf(e)
	et := st.Underlying().(*types.Slice).Elem()
	es := g.c.sizeof(et)
	s := g.keep(g.exprAs(e.Args[0], st), st)
	if s.kind == lZero {
		s = g.zeroTemp(st)
	}
	res := g.temp(st)
	if len(e.Args) == 1 {
		g.move(res, s, st)
		return res
	}
	// the number of new elements, and for the spread form where they come from
	var src loc
	n := g.wordTemp()
	if e.Ellipsis.IsValid() {
		at := g.typeOf(e.Args[1])
		src = g.keep(g.expr(e.Args[1]), at) // a slice or a string
		if src.kind == lZero {
			g.move(res, s, st)
			return res
		}
		g.a.load(AX, g.m(src, R13).plus(8), 8, false)
		g.a.store(mb(BP, n.off), AX, 8)
	} else {
		g.a.storeImm(mb(BP, n.off), int64(len(e.Args)-1), 8)
	}
	// grow if len+n > cap
	sm := mb(BP, s.off)
	if s.kind != lFrame {
		panic("append: slice not in the frame")
	}
	g.a.load(BX, sm.plus(8), 8, false)
	g.a.aluRM(aADD, 8, BX, mb(BP, n.off)) // new length
	g.a.load(CX, sm.plus(16), 8, false)
	g.a.aluRR(aCMP, 8, BX, CX)
	grow := g.a.jcc(ccA)
	g.a.load(AX, sm, 8, false)
	g.a.store(mb(BP, res.off), AX, 8)
	g.a.store(mb(BP, res.off+8), BX, 8)
	g.a.store(mb(BP, res.off+16), CX, 8)
	done := g.a.jmp()
	g.a.patch(grow, g.a.pc())
	// growslice(oldPtr, newLen, oldCap, num, et) (ptr, len, cap)
	g.a.load(AX, sm, 8, false)
	g.a.load(DI, mb(BP, n.off), 8, false)
	g.a.lea(SI, msym(g.c.typeSym(et), 0))
	g.callRT("growslice")
	g.a.store(mb(BP, res.off), AX, 8)
	g.a.store(mb(BP, res.off+8), BX, 8)
	g.a.store(mb(BP, res.off+16), CX, 8)
	g.a.patch(done, g.a.pc())
	// store the new elements at res.ptr[len(s):]
	if e.Ellipsis.IsValid() {
		if es == 0 {
			return res
		}
		// memmove / typedslicecopy from src
		g.a.load(DX, mb(BP, res.off), 8, false)
		g.a.load(BX, sm.plus(8), 8, false)
		g.scaleAdd(es) // AX = destination
		if g.c.hasPointers(et) {
			// typedslicecopy(et, dstPtr, dstLen, srcPtr, srcLen)
			g.a.movRR(BX, AX)
			g.a.lea(AX, msym(g.c.typeSym(et), 0))
			g.a.load(CX, mb(BP, n.off), 8, false)
			srcm := g.m(src, R13)
			g.a.load(DI, srcm, 8, false)
			g.a.load(SI, srcm.plus(8), 8, false)
			g.callRT("typedslicecopy")
		} else {
			srcm := g.m(src, R13)
			g.a.load(BX, srcm, 8, false)
			g.a.load(CX, mb(BP, n.off), 8, false)
			if es != 1 {
				g.a.imulImm(CX, CX, int32(es))
			}
			g.callRT("memmove")
		}
		return res
	}
	// element i goes to res.ptr[len(s)+i]; the address is recomputed for each (evaluation may call)
	base := g.ptrTemp()
	g.a.load(DX, mb(BP, res.off), 8, false)
	g.a.load(BX, sm.plus(8), 8, false)
	g.scaleAdd(es)
	g.a.store(mb(BP, base.off), AX, 8)
	for i, a := range e.Args[1:] {
		g.exprTo(a, loc{kind: lInd, slot: base.off, off: int64(i) * es}, et)
	}
	return res
}

func (g *gen) copyCall(e *ast.CallExpr) loc {
	dt := g.typeOf(e.Args[0])
	srct := g.typeOf(e.Args[1])
	et := dt.Underlying().(*types.Slice).Elem()
	es := g.c.sizeof(et)
	d := g.keep(g.expr(e.Args[0]), dt)
	s := g.keep(g.expr(e.Args[1]), srct)
	if d.kind == lZero || s.kind == lZero {
		return loc{kind: lImm, imm: 0}
	}
	n := g.wordTemp()
	if g.c.hasPointers(et) {
		dm, sm := g.m(d, R12), g.m(s, R13)
		g.a.lea(AX, msym(g.c.typeSym(et), 0))
		g.a.load(BX, dm, 8, false)
		g.a.load(CX, dm.plus(8), 8, false)
		g.a.load(DI, sm, 8, false)
		g.a.load(SI, sm.plus(8), 8, false)
		g.callRT("typedslicecopy")
		return loc{kind: lReg, reg: AX}
	}
	// n = min(len(dst), len(src)); memmove(dst, src, n*size)
	dm, sm := g.m(d, R12), g.m(s, R13)
	g.a.load(CX, dm.plus(8), 8, false)
	g.a.load(DX, sm.plus(8), 8, false)
	g.a.aluRR(aCMP, 8, CX, DX)
	le := g.a.jcc(ccLE)
	g.a.movRR(CX, DX)
	g.a.patch(le, g.a.pc())
	g.a.store(mb(BP, n.off), CX, 8)
	g.a.load(AX, dm, 8, false)
	g.a.load(BX, sm, 8, false)
	if es != 1 {
		g.a.imulImm(CX, CX, int32(es))
	}
	g.callRT("memmove")
	return n
}

func (g *gen) makeCall(e *ast.CallExpr) loc {
	t := g.typeOf(e)
	intT := types.Typ[types.Int]
	argInt := func(a ast.Expr) loc {
		at := g.typeOf(a)
		if b, ok := at.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			at = intT
		}
		l := g.expr(a)
		if l.kind == lImm {
			return l
		}
		g.loadTo(AX, l, at)
		w := g.wordTemp()
		g.a.store(mb(BP, w.off), AX, 8)
		return w
	}
	switch u := t.Underlying().(type) {
	case *types.Slice:
		ln := argInt(e.Args[1])
		cp := ln
		if len(e.Args) > 2 {
			cp = argInt(e.Args[2])
		}
		g.a.lea(AX, msym(g.c.typeSym(u.Elem()), 0))
		g.loadTo(BX, ln, intT)
		g.loadTo(CX, cp, intT)
		g.callRT("makeslice")
		res := g.temp(t)
		g.a.store(mb(BP, res.off), AX, 8)
		g.loadTo(AX, ln, intT)
		g.a.store(mb(BP, res.off+8), AX, 8)
		g.loadTo(AX, cp, intT)
		g.a.store(mb(BP, res.off+16), AX, 8)
		return res
	case *types.Map:
		if len(e.Args) == 1 {
			g.callRT("makemap_small")
			return loc{kind: lReg, reg: AX}
		}
		hint := argInt(e.Args[1])
		g.a.lea(AX, msym(g.c.typeSym(t), 0))
		g.loadTo(BX, hint, intT)
		g.a.movImm(CX, 0)
		g.callRT("makemap")
		return loc{kind: lReg, reg: AX}
	case *types.Chan:
		size := loc{kind: lImm, imm: 0}
		if len(e.Args) > 1 {
			size = argInt(e.Args[1])
		}
		g.a.lea(AX, msym(g.c.typeSym(t), 0))
		g.loadTo(BX, size, intT)
		g.callRT("makechan")
		return loc{kind: lReg, reg: AX}
	}
	giveUp("make(%v)", t)
	return loc{}
}

func (g *gen) minmax(e *ast.CallExpr, name string) loc {
	t := g.typeOf(e)
	if !isWord(t) {
		giveUp("%s of %v", name, t)
	}
	acc := g.wordTemp()
	g.loadTo(AX, g.exprAs(e.Args[0], t), t)
	g.a.store(mb(BP, acc.off), AX, 8)
	for _, a := range e.Args[1:] {
		g.loadTo(AX, g.exprAs(a, t), t)
		g.a.load(CX, mb(BP, acc.off), 8, false)
		g.a.aluRR(aCMP, 8, AX, CX)
		var cc int
		switch {
		case name == "min" && isSigned(t):
			cc = ccGE
		case name == "min":
			cc = ccAE
		case isSigned(t):
			cc = ccLE
		default:
			cc = ccBE
		}
		skip := g.a.jcc(cc)
		g.a.store(mb(BP, acc.off), AX, 8)
		g.a.patch(skip, g.a.pc())
	}
	return acc
}

var _ = token.ADD

// method prepares a call of the method selected by f (x.M): the callee, its ABI, and the receiver argument
// (evaluated, safe from what is evaluated next). sig is the method's signature without the receiver.
func (g *gen) method(f *ast.SelectorExpr, sel *types.Selection, sig *types.Signature) (c callee, ai *abiInfo, recv loc) {
	fn := sel.Obj().(*types.Func)
	path := sel.Index()
	if g.env != nil {
		// inside an instance: the method is the one the type argument has
		obj, p2, _ := types.LookupFieldOrMethod(g.typeOf(f.X), true, fn.Pkg(), fn.Name())
		m, ok := obj.(*types.Func)
		if !ok {
			giveUp("method %s of %v", fn.Name(), g.typeOf(f.X))
		}
		fn, path = m, p2
	}
	return g.methodOn(g.expr(f.X), g.typeOf(f.X), fn, path, sig, f.Sel)
}

// methodOn is method for a receiver operand already evaluated: the value of type xt at l.
func (g *gen) methodOn(l loc, xt types.Type, fn *types.Func, path []int, sig *types.Signature, id *ast.Ident) (c callee, ai *abiInfo, recv loc) {
	direct := func(fn *types.Func) {
		if g.c.isIntrinsic(fn) {
			giveUp("compiler intrinsic %s", fn.FullName())
		}
		sym, fsig := g.funcRef(fn, id)
		c = callee{sym: sym}
		in := []types.Type{}
		if r := fsig.Recv(); r != nil {
			in = append(in, r.Type())
		}
		for i := 0; i < sig.Params().Len(); i++ {
			in = append(in, sig.Params().At(i).Type())
		}
		var out []types.Type
		for i := 0; i < sig.Results().Len(); i++ {
			out = append(out, sig.Results().At(i).Type())
		}
		ai = g.c.abiOfTypes(in, out)
	}
	for once := true; once; once = false {
		msig := fn.Type().(*types.Signature)
		l, t := g.fieldPath(l, xt, path[:len(path)-1])
		if isInterface(t) {
			if l.kind == lZero {
				l = g.zeroTemp(t)
			}
			l = g.stashValue(l, t)
			it := t.Underlying().(*types.Interface)
			idx := -1
			for i, m := range ifaceMethods(it) {
				if m.Name() == fn.Name() && (m.Exported() || m.Pkg() == fn.Pkg()) {
					idx = i
				}
			}
			if idx < 0 {
				giveUp("interface method %s not found", fn.Name())
			}
			c = callee{kind: 2, iface: l, imeth: idx}
			in := []types.Type{types.Typ[types.UnsafePointer]}
			for i := 0; i < sig.Params().Len(); i++ {
				in = append(in, sig.Params().At(i).Type())
			}
			var out []types.Type
			for i := 0; i < sig.Results().Len(); i++ {
				out = append(out, sig.Results().At(i).Type())
			}
			ai = g.c.abiOfTypes(in, out)
			recv = l.plus(8)
			g.usedIfaceMethod(t, fn)
			break
		}
		_, wantPtr := msig.Recv().Type().(*types.Pointer)
		_, havePtr := t.Underlying().(*types.Pointer)
		var rl loc
		switch {
		case wantPtr && havePtr, !wantPtr && !havePtr:
			rl = l
			if rl.kind == lZero {
				rl = g.zeroTemp(t)
			}
		case wantPtr:
			rl = g.addrOfLoc(l)
		default:
			rl = g.deref(l)
		}
		rl = g.stashValue(rl, msig.Recv().Type())
		recv = rl
		direct(fn)
	}
	return
}

func selOf(e ast.Expr) *ast.SelectorExpr {
	s, _ := e.(*ast.SelectorExpr)
	return s
}
