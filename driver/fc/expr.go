// expr.go: expressions.
package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

// constant: where a constant of type t can be read from.
func (g *gen) constLoc(v constant.Value, t types.Type) loc {
	u, _ := t.Underlying().(*types.Basic)
	if u == nil {
		// a constant of pointer or other non-basic type: only nil or an integer converted by unsafe
		if v.Kind() == constant.Int {
			i, _ := constant.Int64Val(v)
			return loc{kind: lImm, imm: i}
		}
		return loc{kind: lZero}
	}
	if u.Info()&types.IsUntyped != 0 {
		t = types.Default(t)
		u, _ = t.Underlying().(*types.Basic)
		if u == nil {
			return loc{kind: lZero}
		}
	}
	switch {
	case u.Info()&types.IsBoolean != 0:
		if constant.BoolVal(v) {
			return loc{kind: lImm, imm: 1}
		}
		return loc{kind: lImm, imm: 0}
	case u.Info()&types.IsInteger != 0 || u.Kind() == types.UnsafePointer:
		v = constant.ToInt(v)
		if i, ok := constant.Int64Val(v); ok {
			return loc{kind: lImm, imm: i}
		}
		x, _ := constant.Uint64Val(v)
		return loc{kind: lImm, imm: int64(x)}
	case u.Info()&types.IsFloat != 0:
		f, _ := constant.Float64Val(constant.ToFloat(v))
		if u.Kind() == types.Float32 {
			f32 := float32(f)
			if f32 == 0 && !math.Signbit(float64(f32)) {
				return loc{kind: lZero}
			}
			return loc{kind: lSym, sym: g.c.ctxt.Float32Sym(f32)}
		}
		if f == 0 && !math.Signbit(f) {
			return loc{kind: lZero}
		}
		return loc{kind: lSym, sym: g.c.ctxt.Float64Sym(f)}
	case u.Info()&types.IsString != 0:
		s := constant.StringVal(v)
		if s == "" {
			return loc{kind: lZero}
		}
		return loc{kind: lSym, sym: g.c.stringHeader(s)}
	case u.Info()&types.IsComplex != 0:
		re, _ := constant.Float64Val(constant.Real(v))
		im, _ := constant.Float64Val(constant.Imag(v))
		return loc{kind: lSym, sym: g.c.complexSym(re, im, u.Kind() == types.Complex64)}
	}
	giveUp("constant of type %v", t)
	return loc{}
}

// varLoc: the home of a variable.
func (g *gen) varLoc(v *types.Var) loc {
	if l, ok := g.vars[v]; ok {
		return l
	}
	if v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
		return loc{kind: lSym, sym: g.c.globalSym(v)}
	}
	giveUp("variable %s has no home", v.Name())
	return loc{}
}

// deref: the memory a pointer points at, given where the pointer value is.
func (g *gen) deref(l loc) loc {
	switch l.kind {
	case lFrame:
		return loc{kind: lInd, slot: l.off}
	case lReg:
		return loc{kind: lRegInd, reg: l.reg}
	case lImm, lZero:
		// a nil dereference: let it fault
		g.a.movImm(AX, 0)
		return loc{kind: lRegInd, reg: AX}
	}
	g.a.load(R13, g.m(l, R13), 8, false)
	tmp := g.ptrTemp()
	g.a.store(mb(BP, tmp.off), R13, 8)
	return loc{kind: lInd, slot: tmp.off}
}

// pure: evaluating e emits no code that disturbs AX, CX or X0 (it may use R12 and R13).
func (g *gen) pure(e ast.Expr) bool {
	if tv, ok := g.info.Types[e]; ok && tv.Value != nil {
		return true
	}
	switch e := e.(type) {
	case *ast.Ident:
		return true
	case *ast.ParenExpr:
		return g.pure(e.X)
	case *ast.SelectorExpr:
		if sel := g.info.Selections[e]; sel != nil {
			return sel.Kind() == types.FieldVal && g.pure(e.X)
		}
		return true // qualified identifier
	case *ast.StarExpr:
		return g.pure(e.X)
	}
	return false
}

// expr evaluates e and says where its value is. A value in memory may be the variable itself, not a copy.
func (g *gen) expr(e ast.Expr) loc {
	tv := g.info.Types[e]
	if tv.Value != nil {
		return g.constLoc(tv.Value, g.subst(tv.Type))
	}
	if tv.IsNil() {
		return loc{kind: lZero}
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return g.expr(e.X)
	case *ast.Ident:
		switch o := g.info.Uses[e].(type) {
		case *types.Var:
			return g.varLoc(o)
		case *types.Func:
			return g.funcValue(o, e)
		case *types.Nil:
			return loc{kind: lZero}
		}
		if o, ok := g.info.Defs[e].(*types.Var); ok {
			return g.varLoc(o)
		}
		giveUp("identifier %s", e.Name)
	case *ast.SelectorExpr:
		return g.selector(e)
	case *ast.StarExpr:
		l := g.deref(g.expr(e.X))
		if g.c.sizeof(g.typeOf(e)) == 0 {
			g.nilcheckLoc(l)
		}
		return l
	case *ast.IndexExpr:
		return g.index(e)
	case *ast.IndexListExpr:
		if id := identOf(e.X); id != nil {
			if fn, ok := g.info.Uses[id].(*types.Func); ok {
				return g.funcValue(fn, id)
			}
		}
		giveUp("generic instantiation")
	case *ast.SliceExpr:
		return g.sliceExpr(e)
	case *ast.UnaryExpr:
		return g.unary(e)
	case *ast.BinaryExpr:
		return g.binary(e)
	case *ast.CallExpr:
		rs := g.call(e)
		if len(rs) != 1 {
			giveUp("call with %d results used as a value", len(rs))
		}
		return rs[0]
	case *ast.CompositeLit:
		t := g.typeOf(e)
		tmp := g.temp(t)
		g.compositeTo(e, tmp, t)
		return tmp
	case *ast.FuncLit:
		return g.funcLit(e)
	case *ast.TypeAssertExpr:
		return g.typeAssert(e, false)[0]
	}
	giveUp("expression %T", e)
	return loc{}
}

// exprAs evaluates e as a value of type t: the place where a value is assigned to a variable, passed, or
// returned, where Go converts implicitly (to an interface type).
func (g *gen) exprAs(e ast.Expr, t types.Type) loc {
	et := g.typeOf(e)
	l := g.expr(e)
	g.convSite = e
	l = g.convertImplicit(l, et, t)
	g.convSite = nil
	return l
}

func (g *gen) convertImplicit(l loc, from, to types.Type) loc {
	if isInterface(to) && !isInterface(from) {
		if b, ok := from.Underlying().(*types.Basic); ok && b.Kind() == types.UntypedNil {
			return loc{kind: lZero}
		}
		return g.toInterface(l, from, to)
	}
	if isInterface(to) && isInterface(from) && !types.Identical(from.Underlying(), to.Underlying()) {
		return g.ifaceToIface(l, from, to)
	}
	return l
}

// exprTo evaluates e into dst (memory), a variable of type t.
func (g *gen) exprTo(e ast.Expr, dst loc, t types.Type) {
	if cl, ok := ast.Unparen(e).(*ast.CompositeLit); ok && dst.inFrame() && types.Identical(g.typeOf(e), t) && !g.mentionsFrame(cl, dst) && !g.addrFrame(dst) {
		g.compositeTo(cl, dst, t)
		return
	}
	g.move(dst, g.exprAs(e, t), t)
}

// mentionsFrame: could evaluating the literal read the destination? (x = T{x.b, x.a} must not be built in place.)
func (g *gen) mentionsFrame(e ast.Expr, dst loc) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := g.info.Uses[id].(*types.Var); ok {
				if l, ok := g.vars[v]; ok && l.kind == lFrame {
					size := align(g.c.sizeof(g.vt(v)), 8)
					if dst.kind == lFrame && dst.off >= l.off && dst.off < l.off+size {
						found = true
					}
				}
			}
		}
		return !found
	})
	return found
}

func (g *gen) nilcheckLoc(l loc) {
	switch l.kind {
	case lInd:
		g.a.load(R13, mb(BP, l.slot), 8, false)
		g.a.nilcheck(R13)
	case lRegInd:
		g.a.nilcheck(l.reg)
	}
}

// selector: a field, a qualified identifier, or a method value.
func (g *gen) selector(e *ast.SelectorExpr) loc {
	sel := g.info.Selections[e]
	if sel == nil {
		switch o := g.info.Uses[e.Sel].(type) {
		case *types.Var:
			return g.varLoc(o)
		case *types.Func:
			return g.funcValue(o, e.Sel)
		}
		giveUp("selector %s", e.Sel.Name)
	}
	switch sel.Kind() {
	case types.FieldVal:
		l := g.expr(e.X)
		l, _ = g.fieldPath(l, g.typeOf(e.X), sel.Index())
		return l
	case types.MethodVal:
		return g.methodValue(e, sel)
	case types.MethodExpr:
		return g.methodExpr(e, sel)
	}
	giveUp("selection kind")
	return loc{}
}

// fieldPath follows a path of field indices from a value of type t at l, dereferencing embedded pointers.
func (g *gen) fieldPath(l loc, t types.Type, path []int) (loc, types.Type) {
	for _, i := range path {
		if p, ok := t.Underlying().(*types.Pointer); ok {
			l = g.deref(l)
			t = p.Elem()
		}
		st := t.Underlying().(*types.Struct)
		off := g.c.fieldOffsets(st)[i]
		if l.kind == lZero || !l.isMem() {
			giveUp("field of a value that is not in memory")
		}
		if (l.kind == lInd || l.kind == lRegInd) && l.off+off >= 4096 {
			g.nilcheckLoc(l)
		}
		l = l.plus(off)
		t = st.Field(i).Type()
	}
	return l, t
}

// ---- indexing

// intIndex loads an index of any integer type into reg as a 64-bit value.
func (g *gen) loadIndex(reg int, l loc, t types.Type) {
	if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		t = types.Typ[types.Int]
	}
	g.loadTo(reg, l, t)
}

func (g *gen) boundsFail(fn string, unsigned bool) {
	// x in BX, y in CX
	if unsigned {
		fn += "U"
	}
	g.a.movRR(AX, BX)
	g.a.movRR(BX, CX)
	g.callRT(fn)
	g.a.ud2()
}

func isUnsignedIndex(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsUnsigned != 0
}

// scaleAdd: AX = DX + BX*size
func (g *gen) scaleAdd(size int64) {
	switch size {
	case 0:
		g.a.movRR(AX, DX)
	case 1, 2, 4, 8:
		g.a.lea(AX, mem{base: DX, index: BX, scale: uint8(size)})
	default:
		if int64(int32(size)) != size {
			giveUp("element too large")
		}
		g.a.imulImm(BX, BX, int32(size))
		g.a.lea(AX, mem{base: DX, index: BX, scale: 1})
	}
}

func (g *gen) index(e *ast.IndexExpr) loc {
	if id := identOf(e.X); id != nil {
		if _, ok := g.info.Instances[id]; ok {
			// F[int] as a value
			if fn, ok := g.info.Uses[id].(*types.Func); ok {
				return g.funcValue(fn, id)
			}
			giveUp("generic instantiation")
		}
	}
	xt := g.typeOf(e.X)
	it := g.typeOf(e.Index)
	if _, ok := xt.Underlying().(*types.Map); ok {
		return g.mapIndex(e, false)[0]
	}
	xl := g.indexBase(e.X, xt)
	if u, ok := xt.Underlying().(*types.Array); ok {
		if tv := g.info.Types[e.Index]; tv.Value != nil {
			i, _ := constant.Int64Val(constant.ToInt(tv.Value))
			return xl.plus(i * g.c.sizeof(u.Elem()))
		}
	}
	xl = g.stash(xl, xt)
	return g.indexAddr(xt, xl, g.expr(e.Index), it)
}

// indexBase evaluates the operand of an index expression: a slice or string value, a pointer to an array,
// or an array in memory.
func (g *gen) indexBase(x ast.Expr, xt types.Type) loc {
	xl := g.expr(x)
	switch u := xt.Underlying().(type) {
	case *types.Array:
		if !xl.isMem() {
			giveUp("index of an array that is not in memory")
		}
		if (xl.kind == lInd || xl.kind == lRegInd) && xl.off+g.c.sizeof(u) >= 4096 {
			g.nilcheckLoc(xl)
		}
	case *types.Pointer:
	default:
		if xl.kind == lZero {
			xl = g.zeroTemp(xt)
		}
	}
	return xl
}

// indexAddr: the element of x (type xt, at xl, safe from the index's evaluation) at index il, after the
// bounds check.
func (g *gen) indexAddr(xt types.Type, xl loc, il loc, it types.Type) loc {
	g.loadIndex(BX, il, it)
	var es int64 = 1
	switch u := xt.Underlying().(type) {
	case *types.Array:
		es = g.c.sizeof(u.Elem())
		g.a.movImm(CX, u.Len())
	case *types.Pointer:
		arr := u.Elem().Underlying().(*types.Array)
		es = g.c.sizeof(arr.Elem())
		g.a.movImm(CX, arr.Len())
	case *types.Slice:
		es = g.c.sizeof(u.Elem())
		g.a.load(CX, g.m(xl, R13).plus(8), 8, false)
	case *types.Basic:
		g.a.load(CX, g.m(xl, R13).plus(8), 8, false)
	default:
		giveUp("index of %v", xt)
	}
	g.a.aluRR(aCMP, 8, BX, CX)
	ok := g.a.jcc(ccB)
	g.boundsFail("goPanicIndex", isUnsignedIndex(it))
	g.a.patch(ok, g.a.pc())
	switch xt.Underlying().(type) {
	case *types.Array:
		g.addrTo(DX, xl)
	case *types.Pointer:
		g.loadTo(DX, xl, xt)
		g.a.nilcheck(DX)
	default:
		g.a.load(DX, g.m(xl, R13), 8, false)
	}
	g.scaleAdd(es)
	return loc{kind: lRegInd, reg: AX}
}

func identOf(e ast.Expr) *ast.Ident {
	switch e := e.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}

// zeroTemp: a zeroed temporary of type t.
func (g *gen) zeroTemp(t types.Type) loc {
	tmp := g.temp(t)
	g.zero(tmp, t)
	return tmp
}

// sliceExpr: x[i:j] and x[i:j:k] of a string, a slice, or (a pointer to) an array.
func (g *gen) sliceExpr(e *ast.SliceExpr) loc {
	xt := g.typeOf(e.X)
	rt := g.typeOf(e)
	isStr := isString(xt)
	var es int64 = 1
	var arrLen int64 = -1
	xl := g.expr(e.X)
	switch u := xt.Underlying().(type) {
	case *types.Slice:
		es = g.c.sizeof(u.Elem())
	case *types.Pointer:
		arr := u.Elem().Underlying().(*types.Array)
		es = g.c.sizeof(arr.Elem())
		arrLen = arr.Len()
	case *types.Array:
		// an addressable array: slice its address
		es = g.c.sizeof(u.Elem())
		arrLen = u.Len()
		if !xl.isMem() {
			giveUp("slice of an array that is not in memory")
		}
		g.addrTo(AX, xl)
		xl = loc{kind: lReg, reg: AX}
		xt = types.NewPointer(xt)
	}
	if xl.kind == lZero {
		xl = g.zeroTemp(xt)
	}
	xl = g.stash(xl, xt)
	res := g.temp(rt)
	// the bounds, as words in temporaries (a missing one is nil)
	var lo, hi, max loc
	idx := func(x ast.Expr) loc {
		if x == nil {
			return loc{}
		}
		l := g.expr(x)
		if l.kind != lImm {
			g.loadIndex(AX, l, g.typeOf(x))
			l = g.wordTemp()
			g.a.store(mb(BP, l.off), AX, 8)
		}
		return l
	}
	lo, hi, max = idx(e.Low), idx(e.High), idx(e.Max)
	// base pointer in DX, length in SI, capacity in DI
	if arrLen >= 0 {
		g.loadTo(DX, xl, xt)
		g.a.nilcheck(DX)
		g.a.movImm(SI, arrLen)
		g.a.movImm(DI, arrLen)
	} else {
		xm := g.m(xl, R13)
		g.a.load(DX, xm, 8, false)
		g.a.load(SI, xm.plus(8), 8, false)
		if isStr {
			g.a.movRR(DI, SI)
		} else {
			g.a.load(DI, xm.plus(16), 8, false)
		}
	}
	// R8 = low, R9 = high, R10 = max (the new capacity's end)
	word := types.Typ[types.Int]
	if lo.kind == lNone {
		g.a.movImm(R8, 0)
	} else {
		g.loadTo(R8, lo, word)
	}
	if hi.kind == lNone {
		g.a.movRR(R9, SI)
	} else {
		g.loadTo(R9, hi, word)
	}
	if max.kind == lNone {
		g.a.movRR(R10, DI)
	} else {
		g.loadTo(R10, max, word)
	}
	uns := func(x ast.Expr) bool { return x != nil && isUnsignedIndex(g.typeOf(x)) }
	unsOf := map[int]bool{R8: uns(e.Low), R9: uns(e.High), R10: uns(e.Max)}
	fail := func(fn string, x, y int) {
		// if x > y (unsigned): panic
		g.a.aluRR(aCMP, 8, x, y)
		ok := g.a.jcc(ccBE)
		g.a.movRR(BX, x)
		g.a.movRR(CX, y)
		g.boundsFail(fn, unsOf[x])
		g.a.patch(ok, g.a.pc())
	}
	capName := "Acap"
	if arrLen >= 0 || isStr {
		capName = "Alen"
	}
	if max.kind != lNone {
		fail("goPanicSlice3"+capName, R10, DI)
		fail("goPanicSlice3B", R9, R10)
		fail("goPanicSlice3C", R8, R9)
	} else {
		if hi.kind != lNone {
			fail("goPanicSlice"+capName, R9, DI)
		}
		fail("goPanicSliceB", R8, R9)
	}
	// length = high-low, capacity = max-low, pointer = base + low*size unless the capacity is zero
	rm := g.m(res, R13)
	g.a.aluRR(aSUB, 8, R9, R8)
	g.a.store(rm.plus(8), R9, 8)
	g.a.aluRR(aSUB, 8, R10, R8)
	if !isStr {
		g.a.store(rm.plus(16), R10, 8)
	}
	if isStr {
		g.a.testRR(8, R9, R9)
	} else {
		g.a.testRR(8, R10, R10)
	}
	skip := g.a.jcc(ccE)
	switch es {
	case 1:
		g.a.aluRR(aADD, 8, DX, R8)
	default:
		if int64(int32(es)) != es {
			giveUp("element too large")
		}
		g.a.imulImm(R8, R8, int32(es))
		g.a.aluRR(aADD, 8, DX, R8)
	}
	g.a.patch(skip, g.a.pc())
	g.a.store(rm, DX, 8)
	return res
}

// ---- operators

func (g *gen) unary(e *ast.UnaryExpr) loc {
	t := g.typeOf(e)
	switch e.Op {
	case token.AND:
		return g.addressOf(e.X)
	case token.ADD:
		return g.expr(e.X)
	case token.ARROW:
		return g.recv(e, false)[0]
	}
	if isComplex(t) {
		if e.Op != token.SUB {
			giveUp("complex unary %v", e.Op)
		}
		return g.complexNeg(g.keep(g.expr(e.X), t), t)
	}
	l := g.expr(e.X)
	size := int(g.c.sizeof(t))
	switch e.Op {
	case token.NOT:
		g.loadTo(AX, l, t)
		g.a.aluImm(aXOR, 4, AX, 1)
		return loc{kind: lReg, reg: AX}
	case token.SUB:
		if isFloat(t) {
			g.loadTo(X0, l, t)
			if size == 8 {
				g.a.movqRX(AX, X0)
				g.a.movImm(R8, math.MinInt64)
				g.a.aluRR(aXOR, 8, AX, R8)
				g.a.movqXR(X0, AX)
			} else {
				g.a.movdRX(AX, X0)
				g.a.aluImm(aXOR, 4, AX, math.MinInt32)
				g.a.movdXR(X0, AX)
			}
			return loc{kind: lReg, reg: X0}
		}
		g.loadTo(AX, l, t)
		g.a.unary(3, 8, AX)
		g.a.extend(AX, size, isSigned(t))
		return loc{kind: lReg, reg: AX}
	case token.XOR:
		g.loadTo(AX, l, t)
		g.a.unary(2, 8, AX)
		g.a.extend(AX, size, isSigned(t))
		return loc{kind: lReg, reg: AX}
	}
	giveUp("unary %v", e.Op)
	return loc{}
}

// operands evaluates x and y (both of type-compatible word or float kinds) and leaves x in r1 and y in r2.
func (g *gen) operands(x, y ast.Expr, xt, yt types.Type, r1, r2 int) {
	l := g.expr(x)
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

var cmpCC = map[token.Token][2]int{ // signed, unsigned
	token.EQL: {ccE, ccE}, token.NEQ: {ccNE, ccNE},
	token.LSS: {ccL, ccB}, token.LEQ: {ccLE, ccBE},
	token.GTR: {ccG, ccA}, token.GEQ: {ccGE, ccAE},
}

func isComparison(op token.Token) bool {
	_, ok := cmpCC[op]
	return ok
}

func (g *gen) binary(e *ast.BinaryExpr) loc {
	switch e.Op {
	case token.LAND, token.LOR:
		// short circuit: materialize through branches
		f, done := g.newLabel(), g.newLabel()
		g.condJump(e, f)
		g.a.movImm(AX, 1)
		g.jmp(done)
		g.place(f)
		g.a.movImm(AX, 0)
		g.place(done)
		return loc{kind: lReg, reg: AX}
	}
	if isComparison(e.Op) {
		return g.comparison(e)
	}
	t := g.typeOf(e)
	if e.Op == token.SHL || e.Op == token.SHR {
		return g.shift(e.X, e.Y, e.Op, t)
	}
	return g.arith(e.X, e.Y, e.Op, t)
}

// arith: x op y for the arithmetic and bitwise operators; both operands have type t.
func (g *gen) arith(x, y ast.Expr, op token.Token, t types.Type) loc {
	if isString(t) {
		if op != token.ADD {
			giveUp("string operator")
		}
		return g.concat(x, y, t)
	}
	if isComplex(t) {
		xl := g.keep(g.exprAs(x, t), t)
		return g.complexArith(xl, g.keep(g.exprAs(y, t), t), op, t)
	}
	size := int(g.c.sizeof(t))
	if isFloat(t) {
		g.operands(x, y, t, t, X0, X1)
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
		g.a.farith(o, size, X0, X1)
		return loc{kind: lReg, reg: X0}
	}
	g.operands(x, y, t, t, AX, CX)
	return g.arithRegs(op, t)
}

// arithRegs: AX = AX op CX for integers of type t.
func (g *gen) arithRegs(op token.Token, t types.Type) loc {
	size := int(g.c.sizeof(t))
	signed := isSigned(t)
	switch op {
	case token.ADD:
		g.a.aluRR(aADD, 8, AX, CX)
	case token.SUB:
		g.a.aluRR(aSUB, 8, AX, CX)
	case token.MUL:
		g.a.imulRR(8, AX, CX)
	case token.AND:
		g.a.aluRR(aAND, 8, AX, CX)
		return loc{kind: lReg, reg: AX}
	case token.OR:
		g.a.aluRR(aOR, 8, AX, CX)
		return loc{kind: lReg, reg: AX}
	case token.XOR:
		g.a.aluRR(aXOR, 8, AX, CX)
		return loc{kind: lReg, reg: AX}
	case token.AND_NOT:
		g.a.unary(2, 8, CX)
		g.a.aluRR(aAND, 8, AX, CX)
		return loc{kind: lReg, reg: AX}
	case token.QUO, token.REM:
		g.a.testRR(8, CX, CX)
		ok := g.a.jcc(ccNE)
		g.callRT("panicdivide")
		g.a.ud2()
		g.a.patch(ok, g.a.pc())
		if signed {
			// the most negative value divided by -1 traps in the processor: x / -1 = -x, x % -1 = 0
			g.a.aluImm(aCMP, 8, CX, -1)
			normal := g.a.jcc(ccNE)
			if op == token.QUO {
				g.a.unary(3, 8, AX)
			} else {
				g.a.movImm(AX, 0)
			}
			done := g.a.jmp()
			g.a.patch(normal, g.a.pc())
			g.a.cxd(8)
			g.a.unary(7, 8, CX)
			if op == token.REM {
				g.a.movRR(AX, DX)
			}
			g.a.patch(done, g.a.pc())
		} else {
			g.a.movImm(DX, 0)
			g.a.unary(6, 8, CX)
			if op == token.REM {
				g.a.movRR(AX, DX)
			}
		}
	default:
		giveUp("operator %v", op)
	}
	g.a.extend(AX, size, signed)
	return loc{kind: lReg, reg: AX}
}

// shift: x << y or x >> y; x has type t, y any integer type.
func (g *gen) shift(x, y ast.Expr, op token.Token, t types.Type) loc {
	yt := g.typeOf(y)
	if b, ok := yt.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		yt = types.Typ[types.Uint]
	}
	g.operands(x, y, t, yt, AX, CX)
	return g.shiftRegs(op, t, yt)
}

// shiftRegs: AX = AX shifted by CX.
func (g *gen) shiftRegs(op token.Token, t, yt types.Type) loc {
	size := int(g.c.sizeof(t))
	signed := isSigned(t)
	if isSigned(yt) {
		g.a.testRR(8, CX, CX)
		ok := g.a.jcc(ccGE)
		g.callRT("panicshift")
		g.a.ud2()
		g.a.patch(ok, g.a.pc())
	}
	// a count of the width or more gives 0, or all sign bits
	g.a.aluImm(aCMP, 8, CX, int64(size*8))
	small := g.a.jcc(ccB)
	if op == token.SHR && signed {
		g.a.shiftImm(7, 8, AX, 63)
	} else {
		g.a.movImm(AX, 0)
	}
	done := g.a.jmp()
	g.a.patch(small, g.a.pc())
	switch {
	case op == token.SHL:
		g.a.shiftCL(4, 8, AX)
	case signed:
		g.a.shiftCL(7, 8, AX)
	default:
		g.a.shiftCL(5, 8, AX)
	}
	g.a.patch(done, g.a.pc())
	if op == token.SHL {
		g.a.extend(AX, size, signed)
	}
	return loc{kind: lReg, reg: AX}
}

// comparison materializes x op y as 0 or 1 in AX.
func (g *gen) comparison(e *ast.BinaryExpr) loc {
	xt, yt := g.typeOf(e.X), g.typeOf(e.Y)
	// the operand type: the typed side when the other is an untyped constant or nil
	t := xt
	if b, ok := xt.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		t = yt
	}
	if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		t = types.Default(t)
	}
	switch {
	case isFloat(t):
		g.operands(e.X, e.Y, t, t, X0, X1)
		size := int(g.c.sizeof(t))
		switch e.Op {
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
	case isWord(t) && !isInterface(xt) && !isInterface(yt):
		g.operands(e.X, e.Y, t, t, AX, CX)
		g.a.aluRR(aCMP, 8, AX, CX)
		cc := cmpCC[e.Op][0]
		if !isSigned(t) {
			cc = cmpCC[e.Op][1]
		}
		g.a.setcc(cc, AX)
		g.a.extend(AX, 1, false)
		return loc{kind: lReg, reg: AX}
	case isString(t):
		return g.stringCompare(e, t)
	}
	return g.equality(e, xt, yt)
}

// condJump evaluates a boolean expression and jumps to f if it is false (falls through if true).
func (g *gen) condJump(e ast.Expr, f *label) {
	e = ast.Unparen(e)
	if tv := g.info.Types[e]; tv.Value != nil {
		if !constant.BoolVal(tv.Value) {
			g.jmp(f)
		}
		return
	}
	switch e := e.(type) {
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			t := g.newLabel()
			g.condJump(e.X, t)
			g.jmp(f)
			g.place(t)
			return
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.LAND:
			g.condJump(e.X, f)
			g.condJump(e.Y, f)
			return
		case token.LOR:
			t, next := g.newLabel(), g.newLabel()
			g.condJump(e.X, next)
			g.jmp(t)
			g.place(next)
			g.condJump(e.Y, f)
			g.place(t)
			return
		}
		if isComparison(e.Op) {
			xt, yt := g.typeOf(e.X), g.typeOf(e.Y)
			t := xt
			if b, ok := xt.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
				t = yt
			}
			if isWord(t) && !isInterface(xt) && !isInterface(yt) {
				if b, ok := t.Underlying().(*types.Basic); !ok || b.Info()&types.IsUntyped == 0 {
					g.operands(e.X, e.Y, t, t, AX, CX)
					g.a.aluRR(aCMP, 8, AX, CX)
					cc := cmpCC[e.Op][0]
					if !isSigned(t) {
						cc = cmpCC[e.Op][1]
					}
					g.jcc(cc^1, f) // the opposite condition
					return
				}
			}
		}
	}
	l := g.expr(e)
	g.loadTo(AX, l, types.Typ[types.Bool])
	g.a.testRR(1, AX, AX)
	g.jcc(ccE, f)
}

// addressOf: &x. The operand is a heap variable, a global, an element of something on the heap, or a
// composite literal (allocated here).
func (g *gen) addressOf(x ast.Expr) loc {
	x = ast.Unparen(x)
	if cl, ok := x.(*ast.CompositeLit); ok {
		t := g.typeOf(cl)
		if !g.siteHeap(cl) {
			obj := g.frameObject(t)
			g.compositeTo(cl, obj, t)
			return g.frameAddr(obj)
		}
		g.a.lea(AX, msym(g.c.typeSym(t), 0))
		g.callRT("newobject")
		p := g.ptrTemp()
		g.a.store(mb(BP, p.off), AX, 8)
		g.compositeTo(cl, loc{kind: lInd, slot: p.off}, t)
		return p
	}
	l := g.expr(x)
	switch l.kind {
	case lInd:
		if l.off == 0 {
			return loc{kind: lFrame, off: l.slot}
		}
		g.a.load(AX, mb(BP, l.slot), 8, false)
		g.a.nilcheck(AX)
		g.a.lea(AX, mb(AX, l.off))
		return loc{kind: lReg, reg: AX}
	case lRegInd:
		if l.off != 0 {
			g.a.nilcheck(l.reg)
		}
		if l.off != 0 || l.reg != AX {
			g.a.lea(AX, mb(l.reg, l.off))
		}
		return loc{kind: lReg, reg: AX}
	case lSym:
		g.a.lea(AX, msym(l.sym, l.off))
		return loc{kind: lReg, reg: AX}
	case lFrame:
		// a variable the escape analysis left in the frame
		g.a.lea(AX, mb(BP, l.off))
		return loc{kind: lReg, reg: AX}
	}
	giveUp("address of a value that is not in memory")
	return loc{}
}

// ---- static data helpers

func (c *compiler) stringHeader(s string) *obj.LSym {
	c.hdrMu.Lock()
	defer c.hdrMu.Unlock()
	if h := c.strHdrs[s]; h != nil {
		return h
	}
	data := c.stringSym(s)
	h := c.staticSym("strhdr")
	h.WriteAddr(c.ctxt, 0, 8, data, 0)
	h.WriteInt(c.ctxt, 8, 8, int64(len(s)))
	c.defData(h, objabi.SRODATA, 16)
	c.strHdrs[s] = h
	return h
}

func (c *compiler) complexSym(re, im float64, c64 bool) *obj.LSym {
	s := c.staticSym("complex")
	if c64 {
		s.WriteFloat32(c.ctxt, 0, float32(re))
		s.WriteFloat32(c.ctxt, 4, float32(im))
		c.defData(s, objabi.SRODATA, 8)
	} else {
		s.WriteFloat64(c.ctxt, 0, re)
		s.WriteFloat64(c.ctxt, 8, im)
		c.defData(s, objabi.SRODATA, 16)
	}
	return s
}

// addrFrame: is dst (part of) a frame variable whose address is taken? Such a variable can be read through
// a pointer while it is being assigned, so a literal is not built in place there.
func (g *gen) addrFrame(dst loc) bool {
	if dst.kind != lFrame {
		return false
	}
	for v := range g.addrVars {
		if l, ok := g.vars[v]; ok && l.kind == lFrame {
			if dst.off >= l.off && dst.off < l.off+align(g.c.sizeof(g.vt(v)), 8) {
				return true
			}
		}
	}
	return false
}
