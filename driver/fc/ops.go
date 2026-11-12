// ops.go: strings, equality and interface values.
package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

func constantFloat(f float64) constant.Value { return constant.MakeFloat64(f) }

func (c *compiler) anyType() types.Type { return types.Universe.Lookup("any").Type() }

// inMem: the value as something with an address in this frame's reach (a zero value or a register is
// put in a temporary).
func (g *gen) inMem(l loc, t types.Type) loc {
	switch l.kind {
	case lZero:
		return g.zeroTemp(t)
	case lImm:
		tmp := g.wordTemp()
		g.a.movImm(R8, l.imm)
		g.a.store(mb(BP, tmp.off), R8, 8)
		return tmp
	case lReg:
		return g.stash(l, t)
	case lRegInd:
		return g.stash(l, t)
	}
	return l
}

// operandsL: like operands, with the left operand already evaluated.
func (g *gen) operandsL(l loc, y ast.Expr, xt, yt types.Type, r1, r2 int) {
	if g.pure(y) {
		if l.kind == lRegInd {
			g.loadTo(r1, l, xt)
			l = loc{kind: lReg, reg: r1}
		}
		r := g.expr(y)
		g.loadTo(r2, r, yt)
		g.loadTo(r1, l, xt)
		return
	}
	if l.kind == lRegInd {
		g.loadTo(regFor(xt), l, xt)
		l = loc{kind: lReg, reg: regFor(xt)}
	}
	l = g.stashValue(l, xt)
	r := g.expr(y)
	g.loadTo(r2, r, yt)
	g.loadTo(r1, l, xt)
}

// ---- strings

func (g *gen) concat(x, y ast.Expr, t types.Type) loc {
	// flatten a + b + c into one call
	var parts []ast.Expr
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD && g.c.info.Types[e].Value == nil {
			walk(b.X)
			walk(b.Y)
			return
		}
		parts = append(parts, e)
	}
	walk(x)
	walk(y)
	var ls []loc
	for _, p := range parts {
		ls = append(ls, g.keep(g.expr(p), t))
	}
	return g.concatLocs(ls, t)
}

func (g *gen) concatLoc(l loc, y ast.Expr, t types.Type) loc {
	l = g.keep(l, t)
	r := g.keep(g.expr(y), t)
	return g.concatLocs([]loc{l, r}, t)
}

// concatLocs: runtime.concatstringN(buf, a0, a1, ...) for 2..5 strings, concatstrings(buf, []string) beyond.
func (g *gen) concatLocs(ls []loc, t types.Type) loc {
	strT := types.Typ[types.String]
	nilp := loc{kind: lImm, imm: 0}
	if len(ls) <= 5 {
		args := []loc{nilp}
		args = append(args, ls...)
		name := "concatstring" + string(rune('0'+len(ls)))
		return g.inMem(g.rtCall(name, args...)[0], strT)
	}
	sliceT := types.NewSlice(strT)
	sl := g.newBacking(strT, int64(len(ls)), sliceT)
	for i, l := range ls {
		g.move(loc{kind: lInd, slot: sl.off, off: int64(i) * 16}, l, strT)
	}
	return g.inMem(g.rtCall("concatstrings", nilp, sl)[0], strT)
}

func (g *gen) stringCompare(e *ast.BinaryExpr, t types.Type) loc {
	xl := g.keep(g.expr(e.X), t)
	yl := g.expr(e.Y)
	return g.compareLocs(e.Op, xl, t, yl, t)
}

// ---- comparison of values wherever they are

// compareLocs: x op y as 0 or 1 in AX. x must be safe from the evaluation of y (already done).
func (g *gen) compareLocs(op token.Token, xl loc, xt types.Type, yl loc, yt types.Type) loc {
	untyped := func(t types.Type) bool {
		b, ok := t.Underlying().(*types.Basic)
		return ok && b.Info()&types.IsUntyped != 0
	}
	t := xt
	if untyped(xt) {
		t = yt
	}
	if untyped(t) {
		t = types.Default(t)
	}
	// an interface compared with a concrete value: convert the concrete side
	if isInterface(xt) != isInterface(yt) && !untyped(xt) && !untyped(yt) {
		if isInterface(xt) {
			yl = g.inMem(yl, yt)
			yl = g.toInterface(yl, yt, xt)
			yt = xt
		} else {
			xl = g.toInterface(g.inMem(xl, xt), xt, yt)
			xt = yt
			yl = g.inMem(yl, yt)
		}
		t = xt
	}
	switch {
	case isFloat(t):
		if yl.kind == lReg && yl.reg == X0 {
			g.a.fmov(X1, X0)
			yl = loc{kind: lReg, reg: X1}
		}
		g.loadTo(X1, yl, t)
		g.loadTo(X0, xl, t)
		size := int(g.c.sizeof(t))
		switch op {
		case token.EQL:
			g.a.ucomis(size, X0, X1)
			g.a.setcc(ccE, AX)
			g.a.setcc(ccNP, CX)
			g.a.aluRR(aAND, 4, AX, CX)
		case token.NEQ:
			g.a.ucomis(size, X0, X1)
			g.a.setcc(ccNE, AX)
			g.a.setcc(ccP, CX)
			g.a.aluRR(aOR, 4, AX, CX)
		case token.GTR:
			g.a.ucomis(size, X0, X1)
			g.a.setcc(ccA, AX)
		case token.GEQ:
			g.a.ucomis(size, X0, X1)
			g.a.setcc(ccAE, AX)
		case token.LSS:
			g.a.ucomis(size, X1, X0)
			g.a.setcc(ccA, AX)
		case token.LEQ:
			g.a.ucomis(size, X1, X0)
			g.a.setcc(ccAE, AX)
		}
		g.a.extend(AX, 1, false)
		return loc{kind: lReg, reg: AX}
	case isWord(t) && !isInterface(t):
		g.loadTo(CX, yl, t)
		g.loadTo(AX, xl, t)
		g.a.aluRR(aCMP, 8, AX, CX)
		cc := cmpCC[op][0]
		if !isSigned(t) {
			cc = cmpCC[op][1]
		}
		g.a.setcc(cc, AX)
		g.a.extend(AX, 1, false)
		return loc{kind: lReg, reg: AX}
	case isString(t) && op != token.EQL && op != token.NEQ:
		xl, yl = g.inMem(xl, t), g.inMem(yl, t)
		r := g.rtCall("cmpstring", xl, yl)[0]
		g.loadTo(AX, r, types.Typ[types.Int])
		g.a.aluImm(aCMP, 8, AX, 0)
		g.a.setcc(cmpCC[op][0], AX)
		g.a.extend(AX, 1, false)
		return loc{kind: lReg, reg: AX}
	}
	if op != token.EQL && op != token.NEQ {
		giveUp("ordering of %v", t)
	}
	// equality of anything else: jump to ne on the first difference
	if yl.kind == lReg || yl.kind == lRegInd {
		yl = g.stash(yl, t)
	}
	ne, done := g.newLabel(), g.newLabel()
	g.eqJump(xl, yl, t, ne)
	if op == token.EQL {
		g.a.movImm(AX, 1)
	} else {
		g.a.movImm(AX, 0)
	}
	g.jmp(done)
	g.place(ne)
	if op == token.EQL {
		g.a.movImm(AX, 0)
	} else {
		g.a.movImm(AX, 1)
	}
	g.place(done)
	return loc{kind: lReg, reg: AX}
}

func (g *gen) equality(e *ast.BinaryExpr, xt, yt types.Type) loc {
	xl := g.keep(g.expr(e.X), xt)
	yl := g.expr(e.Y)
	return g.compareLocs(e.Op, xl, xt, yl, yt)
}

// eqJump compares two values of type t (in memory, or zero) and jumps to ne if they differ.
func (g *gen) eqJump(xl, yl loc, t types.Type, ne *label) {
	size := g.c.sizeof(t)
	if size == 0 {
		return
	}
	if xl.kind == lZero && yl.kind == lZero {
		return
	}
	if xl.kind == lZero {
		xl, yl = yl, xl
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsString != 0:
			if yl.kind == lZero {
				g.a.aluMI(aCMP, 8, g.m(xl, R13).plus(8), 0)
				g.jcc(ccNE, ne)
				return
			}
			g.a.load(CX, g.m(xl, R13).plus(8), 8, false)
			g.a.aluRM(aCMP, 8, CX, g.m(yl, R13).plus(8))
			g.jcc(ccNE, ne)
			g.a.load(AX, g.m(xl, R13), 8, false)
			g.a.load(BX, g.m(yl, R13), 8, false)
			g.callRT("memequal")
			g.a.testRR(1, AX, AX)
			g.jcc(ccE, ne)
			return
		case u.Info()&types.IsFloat != 0:
			g.loadTo(X0, xl, t)
			g.loadTo(X1, yl, t)
			g.a.ucomis(int(size), X0, X1)
			g.jcc(ccNE, ne)
			g.jcc(ccP, ne)
			return
		case u.Info()&types.IsComplex != 0:
			h := int(size / 2)
			for _, off := range []int64{0, int64(h)} {
				if yl.kind == lZero {
					g.a.xorps(X1, X1)
				} else {
					g.a.fload(X1, g.m(yl, R13).plus(off), h)
				}
				g.a.fload(X0, g.m(xl, R13).plus(off), h)
				g.a.ucomis(h, X0, X1)
				g.jcc(ccNE, ne)
				g.jcc(ccP, ne)
			}
			return
		}
		g.loadTo(CX, yl, t)
		g.loadTo(AX, xl, t)
		g.a.aluRR(aCMP, 8, AX, CX)
		g.jcc(ccNE, ne)
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		g.loadTo(CX, yl, t)
		g.loadTo(AX, xl, t)
		g.a.aluRR(aCMP, 8, AX, CX)
		g.jcc(ccNE, ne)
	case *types.Slice:
		// only comparable with nil
		if yl.kind != lZero {
			giveUp("comparison of slices")
		}
		g.a.aluMI(aCMP, 8, g.m(xl, R13), 0)
		g.jcc(ccNE, ne)
	case *types.Interface:
		if yl.kind == lZero {
			g.a.aluMI(aCMP, 8, g.m(xl, R13), 0)
			g.jcc(ccNE, ne)
			return
		}
		// different type words: unequal. Same: nil, or the runtime compares the data.
		g.a.load(AX, g.m(xl, R13), 8, false)
		g.a.aluRM(aCMP, 8, AX, g.m(yl, R13))
		g.jcc(ccNE, ne)
		g.a.load(BX, g.m(xl, R13).plus(8), 8, false)
		g.a.load(CX, g.m(yl, R13).plus(8), 8, false)
		if u.NumMethods() == 0 {
			g.callRT("efaceeq")
		} else {
			g.callRT("ifaceeq")
		}
		g.a.testRR(1, AX, AX)
		g.jcc(ccE, ne)
	case *types.Struct:
		offs := g.c.fieldOffsets(u)
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if f.Name() == "_" {
				continue
			}
			yf := yl
			if yl.kind != lZero {
				yf = yl.plus(offs[i])
			}
			g.eqJump(xl.plus(offs[i]), yf, f.Type(), ne)
		}
	case *types.Array:
		es := g.c.sizeof(u.Elem())
		if u.Len() <= 4 {
			for i := int64(0); i < u.Len(); i++ {
				ye := yl
				if yl.kind != lZero {
					ye = yl.plus(i * es)
				}
				g.eqJump(xl.plus(i*es), ye, u.Elem(), ne)
			}
			return
		}
		if yl.kind == lZero {
			yl = g.zeroTemp(t)
		}
		if g.c.plainMemory(u.Elem()) {
			g.addrTo(AX, xl)
			g.addrTo(BX, yl)
			g.a.movImm(CX, size)
			g.callRT("memequal")
			g.a.testRR(1, AX, AX)
			g.jcc(ccE, ne)
			return
		}
		// a loop over the elements
		px, py, i := g.ptrTemp(), g.ptrTemp(), g.wordTemp()
		g.addrTo(AX, xl)
		g.a.store(mb(BP, px.off), AX, 8)
		g.addrTo(AX, yl)
		g.a.store(mb(BP, py.off), AX, 8)
		g.a.storeImm(mb(BP, i.off), u.Len(), 8)
		top := g.newLabel()
		g.place(top)
		g.eqJump(loc{kind: lInd, slot: px.off}, loc{kind: lInd, slot: py.off}, u.Elem(), ne)
		g.a.aluMI(aADD, 8, mb(BP, px.off), es)
		g.a.aluMI(aADD, 8, mb(BP, py.off), es)
		g.a.aluMI(aSUB, 8, mb(BP, i.off), 1)
		g.jcc(ccNE, top)
	default:
		giveUp("equality of %v", t)
	}
}

// plainMemory: two values of type t are equal exactly when their bytes are (no padding, floats, strings
// or interfaces inside).
func (c *compiler) plainMemory(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsInteger|types.IsBoolean) != 0 || u.Kind() == types.UnsafePointer
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return true
	case *types.Array:
		return c.plainMemory(u.Elem())
	case *types.Struct:
		offs := c.fieldOffsets(u)
		end := int64(0)
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if f.Name() == "_" || !c.plainMemory(f.Type()) || offs[i] != end {
				return false
			}
			end = offs[i] + c.sizeof(f.Type())
		}
		return end == c.sizeof(t)
	}
	return false
}

// ---- interface values

// markUsedInIface tells the linker that t is converted to an interface in the current function (its
// methods may be called through one).
func (g *gen) markUsedInIface(tsym *obj.LSym) {
	g.a.relocs = append(g.a.relocs, obj.Reloc{Type: objabi.R_USEIFACE, Sym: tsym})
}

// usedIfaceMethod tells the linker that method m of the interface type it is called in the current function.
func (g *gen) usedIfaceMethod(it types.Type, m *types.Func) {
	iface := it.Underlying().(*types.Interface)
	idx := 0
	for i, im := range ifaceMethods(iface) {
		if im.Name() == m.Name() {
			idx = i
		}
	}
	off := int64(48 + 4*8 + idx*8)
	if _, ok := types.Unalias(it).(*types.Named); ok {
		off += 16 // the uncommon type
	}
	g.a.relocs = append(g.a.relocs, obj.Reloc{Type: objabi.R_USEIFACEMETHOD, Sym: g.c.typeSym(it), Add: off})
}

// toInterface converts a value of the concrete type from to the interface type to.
func (g *gen) toInterface(l loc, from, to types.Type) loc {
	if b, ok := from.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		from = types.Default(from)
	}
	tsym := g.c.typeSym(from)
	var word *obj.LSym
	if to.Underlying().(*types.Interface).NumMethods() == 0 {
		word = tsym
	} else {
		word = g.c.itabSym(from, to)
	}
	g.markUsedInIface(tsym)
	res := g.temp(to)
	switch {
	case isDirectIface(from):
		l = g.inMem(l, from)
		g.a.load(AX, g.m(l, R13), 8, false)
	case g.c.sizeof(from) == 0:
		g.a.lea(AX, msym(g.c.rtVar("zerobase"), 0))
	default:
		l = g.inMem(l, from)
		g.addrTo(BX, l)
		g.a.lea(AX, msym(tsym, 0))
		g.callRT("convT")
	}
	g.a.store(mb(BP, res.off+8), AX, 8)
	g.a.lea(AX, msym(word, 0))
	g.a.store(mb(BP, res.off), AX, 8)
	return res
}
