// complex.go: complex numbers (two floats in memory) and the print built-ins.
package main

import (
	"go/ast"
	"go/token"
	"go/types"
)

func (g *gen) floatOf(t types.Type) types.Type {
	if g.c.sizeof(t) == 8 {
		return types.Typ[types.Float32]
	}
	return types.Typ[types.Float64]
}

// complexArith: x op y for complex operands of type t, both in memory or zero.
func (g *gen) complexArith(xl, yl loc, op token.Token, t types.Type) loc {
	h := int(g.c.sizeof(t) / 2)
	xl, yl = g.inMem(xl, t), g.inMem(yl, t)
	res := g.temp(t)
	part := func(x int, l loc, im bool) {
		off := int64(0)
		if im {
			off = int64(h)
		}
		g.a.fload(x, g.m(l, R13).plus(off), h)
	}
	switch op {
	case token.ADD, token.SUB:
		o := byte(0x58)
		if op == token.SUB {
			o = 0x5C
		}
		for _, im := range []bool{false, true} {
			part(X0, xl, im)
			part(X1, yl, im)
			g.a.farith(o, h, X0, X1)
			off := int64(0)
			if im {
				off = int64(h)
			}
			g.a.fstore(mb(BP, res.off+off), X0, h)
		}
	case token.MUL:
		if h == 4 {
			// as the stock compiler: in float64, rounded once
			wide := types.Typ[types.Complex128]
			r := g.complexArith(g.complexConv(xl, t, wide), g.complexConv(yl, t, wide), op, wide)
			return g.complexConv(r, wide, t)
		}
		// (a+bi)(c+di) = (ac-bd) + (ad+bc)i
		part(X0, xl, false) // a
		part(X1, yl, false) // c
		g.a.farith(0x59, h, X0, X1)
		part(X1, xl, true) // b
		part(X14, yl, true)
		g.a.farith(0x59, h, X1, X14)
		g.a.farith(0x5C, h, X0, X1)
		g.a.fstore(mb(BP, res.off), X0, h)
		part(X0, xl, false) // a
		part(X1, yl, true)  // d
		g.a.farith(0x59, h, X0, X1)
		part(X1, xl, true) // b
		part(X14, yl, false)
		g.a.farith(0x59, h, X1, X14)
		g.a.farith(0x58, h, X0, X1)
		g.a.fstore(mb(BP, res.off+int64(h)), X0, h)
	case token.QUO:
		// runtime.complex128div(n, d): the four parts in X0..X3 as float64
		for i, pr := range []struct {
			l  loc
			im bool
		}{{xl, false}, {xl, true}, {yl, false}, {yl, true}} {
			part(X0+i, pr.l, pr.im)
			if h == 4 {
				g.a.cvtf2f(4, X0+i, X0+i)
			}
		}
		g.callRT("complex128div")
		if h == 4 {
			g.a.cvtf2f(8, X0, X0)
			g.a.cvtf2f(8, X1, X1)
		}
		g.a.fstore(mb(BP, res.off), X0, h)
		g.a.fstore(mb(BP, res.off+int64(h)), X1, h)
	default:
		giveUp("complex operator %v", op)
	}
	return res
}

func (g *gen) complexNeg(l loc, t types.Type) loc {
	h := int(g.c.sizeof(t) / 2)
	l = g.inMem(l, t)
	res := g.temp(t)
	for _, off := range []int64{0, int64(h)} {
		// flip the sign bit of each part
		g.a.load(AX, g.m(l, R13).plus(off), h, false)
		if h == 8 {
			g.a.movImm(R8, -1<<63)
			g.a.aluRR(aXOR, 8, AX, R8)
		} else {
			g.a.aluImm(aXOR, 4, AX, -1<<31)
		}
		g.a.store(mb(BP, res.off+off), AX, h)
	}
	return res
}

func (g *gen) makeComplex(e *ast.CallExpr) loc {
	t := g.typeOf(e)
	h := int(g.c.sizeof(t) / 2)
	ft := g.floatOf(t)
	res := g.temp(t)
	re := g.keep(g.exprAs(e.Args[0], ft), ft)
	im := g.expr(e.Args[1])
	g.loadTo(X1, im, ft)
	g.loadTo(X0, re, ft)
	g.a.fstore(mb(BP, res.off), X0, h)
	g.a.fstore(mb(BP, res.off+int64(h)), X1, h)
	return res
}

func (g *gen) complexPart(e *ast.CallExpr, imag bool) loc {
	t := g.typeOf(e.Args[0])
	if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		t = types.Typ[types.Complex128]
	}
	l := g.expr(e.Args[0])
	if l.kind == lZero {
		return l
	}
	if imag {
		return l.plus(g.c.sizeof(t) / 2)
	}
	return l
}

// complexConv: complex64 <-> complex128.
func (g *gen) complexConv(l loc, from, to types.Type) loc {
	if l.kind == lZero {
		return l
	}
	fh, th := int(g.c.sizeof(from)/2), int(g.c.sizeof(to)/2)
	l = g.inMem(l, from)
	res := g.temp(to)
	for _, i := range []int64{0, 1} {
		g.a.fload(X0, g.m(l, R13).plus(i*int64(fh)), fh)
		g.a.cvtf2f(fh, X0, X0)
		g.a.fstore(mb(BP, res.off+i*int64(th)), X0, th)
	}
	return res
}

// printCall: print and println, as calls of the runtime's printers.
func printType(t types.Type) types.Type {
	if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		t = types.Default(t)
		if b.Kind() == types.UntypedNil {
			t = unsafePtr
		}
	}
	return t
}

func (g *gen) printCall(e *ast.CallExpr, nl bool) {
	var vals []typedLoc
	for _, a := range e.Args {
		t := printType(g.typeOf(a))
		vals = append(vals, typedLoc{g.inMem(g.keep(g.inMem(g.exprAs(a, t), t), t), t), t})
	}
	g.printLocs(vals, nl)
}

// printLocs prints values that are in memory.
func (g *gen) printLocs(vals []typedLoc, nl bool) {
	g.callRT("printlock")
	for i, v := range vals {
		if i > 0 && nl {
			g.callRT("printsp")
		}
		t := v.t
		m := g.m(v.l, R13)
		switch u := t.Underlying().(type) {
		case *types.Basic:
			switch {
			case u.Info()&types.IsBoolean != 0:
				g.loadTo(AX, v.l, t)
				g.callRT("printbool")
			case u.Info()&types.IsString != 0:
				g.a.load(AX, m, 8, false)
				g.a.load(BX, m.plus(8), 8, false)
				g.callRT("printstring")
			case u.Info()&types.IsFloat != 0:
				g.loadTo(X0, v.l, t)
				if g.c.sizeof(t) == 4 {
					g.callRT("printfloat32")
				} else {
					g.callRT("printfloat64")
				}
			case u.Info()&types.IsComplex != 0:
				h := int(g.c.sizeof(t) / 2)
				g.a.fload(X0, m, h)
				g.a.fload(X1, m.plus(int64(h)), h)
				if h == 4 {
					g.callRT("printcomplex64")
				} else {
					g.callRT("printcomplex128")
				}
			case u.Kind() == types.UnsafePointer:
				g.loadTo(AX, v.l, t)
				g.callRT("printpointer")
			case u.Kind() == types.Uintptr:
				g.loadTo(AX, v.l, t)
				g.callRT("printuintptr")
			case u.Info()&types.IsUnsigned != 0:
				g.loadTo(AX, v.l, t)
				g.callRT("printuint")
			default:
				g.loadTo(AX, v.l, t)
				g.callRT("printint")
			}
		case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
			g.loadTo(AX, v.l, t)
			g.callRT("printpointer")
		case *types.Slice:
			g.a.load(AX, m, 8, false)
			g.a.load(BX, m.plus(8), 8, false)
			g.a.load(CX, m.plus(16), 8, false)
			g.callRT("printslice")
		case *types.Interface:
			g.a.load(AX, m, 8, false)
			g.a.load(BX, m.plus(8), 8, false)
			if u.NumMethods() == 0 {
				g.callRT("printeface")
			} else {
				g.callRT("printiface")
			}
		default:
			giveUp("print of %v", t)
		}
	}
	if nl {
		g.callRT("printnl")
	}
	g.callRT("printunlock")
}
