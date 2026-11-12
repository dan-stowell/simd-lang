// typecode.go: the functions type descriptors point at: equality, hash, and method wrappers. rtype.go asks
// for them (needEq, needHash, needWrapper); they are compiled here like any other function.
package main

import (
	"go/token"
	"go/types"

	"cmd/internal/obj"
)

// unwrittenTypeCode compiles everything the descriptors written so far need, until nothing is left.
func (c *compiler) unwrittenTypeCode() (did bool) {
	for {
		c.rtMu.Lock()
		eqs, hashes, wraps := c.needEq, c.needHash, c.needWrapper
		c.needEq, c.needHash, c.needWrapper = nil, nil, nil
		c.rtMu.Unlock()
		if len(eqs)+len(hashes)+len(wraps) == 0 {
			return
		}
		did = true
		for _, r := range eqs {
			c.genEq(r)
		}
		for _, r := range hashes {
			c.genHash(r)
		}
		for _, r := range wraps {
			c.genWrapper(r)
		}
	}
}

func param(t types.Type) *types.Var { return types.NewParam(token.NoPos, nil, "", t) }

func (c *compiler) pkgPos() token.Pos { return c.pkg.files[0].Package }

// genEq: func(p, q unsafe.Pointer) bool comparing two values of r.typ.
func (c *compiler) genEq(r algReq) {
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(param(unsafePtr), param(unsafePtr)),
		types.NewTuple(param(types.Typ[types.Bool])), false)
	g := c.newGen(r.sym.Name, sig, c.info, nil)
	g.out.sym = r.sym
	g.out.flags |= obj.DUPOK
	g.compileFunc(nil, c.pkgPos(), c.pkgPos(), nil, func() {
		x := loc{kind: lInd, slot: g.paramLoc(g.abi.in[0]).off}
		y := loc{kind: lInd, slot: g.paramLoc(g.abi.in[1]).off}
		ne := g.newLabel()
		g.eqJump(x, y, r.typ, ne)
		g.a.storeImm(g.m(g.resHome[0], R13), 1, 1)
		g.jmp(g.ret)
		g.place(ne)
		g.a.storeImm(g.m(g.resHome[0], R13), 0, 1)
	})
}

// genHash: func(p unsafe.Pointer, h uintptr) uintptr hashing a value of r.typ.
func (c *compiler) genHash(r algReq) {
	up := types.Typ[types.Uintptr]
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(param(unsafePtr), param(up)), types.NewTuple(param(up)), false)
	g := c.newGen(r.sym.Name, sig, c.info, nil)
	g.out.sym = r.sym
	g.out.flags |= obj.DUPOK
	g.compileFunc(nil, c.pkgPos(), c.pkgPos(), nil, func() {
		p := g.paramLoc(g.abi.in[0])
		h := g.paramLoc(g.abi.in[1])
		g.hashValue(p, 0, h, r.typ)
		g.a.load(AX, mb(BP, h.off), 8, false)
		g.a.store(g.m(g.resHome[0], R13), AX, 8)
	})
}

// hashValue: h = hash(value of type t at *p + off, h). p and h are frame slots.
func (g *gen) hashValue(p loc, off int64, h loc, t types.Type) {
	size := g.c.sizeof(t)
	if size == 0 {
		return
	}
	call := func(fn string, withSize bool) {
		g.a.load(AX, mb(BP, p.off), 8, false)
		if off != 0 {
			g.a.lea(AX, mb(AX, off))
		}
		g.a.load(BX, mb(BP, h.off), 8, false)
		if withSize {
			g.a.movImm(CX, size)
		}
		g.callRT(fn)
		g.a.store(mb(BP, h.off), AX, 8)
	}
	if g.c.plainMemory(t) {
		call("memhash", true)
		return
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsString != 0:
			call("strhash", false)
		case u.Info()&types.IsFloat != 0 && size == 4:
			call("f32hash", false)
		case u.Info()&types.IsFloat != 0:
			call("f64hash", false)
		case u.Info()&types.IsComplex != 0 && size == 8:
			call("c64hash", false)
		case u.Info()&types.IsComplex != 0:
			call("c128hash", false)
		default:
			call("memhash", true)
		}
	case *types.Interface:
		if u.NumMethods() == 0 {
			call("nilinterhash", false)
		} else {
			call("interhash", false)
		}
	case *types.Struct:
		offs := g.c.fieldOffsets(u)
		for i := 0; i < u.NumFields(); i++ {
			if u.Field(i).Name() == "_" {
				continue
			}
			g.hashValue(p, off+offs[i], h, u.Field(i).Type())
		}
	case *types.Array:
		es := g.c.sizeof(u.Elem())
		if u.Len() <= 4 {
			for i := int64(0); i < u.Len(); i++ {
				g.hashValue(p, off+i*es, h, u.Elem())
			}
			return
		}
		// a loop: q walks the elements
		q, n := g.ptrTemp(), g.wordTemp()
		g.a.load(AX, mb(BP, p.off), 8, false)
		g.a.lea(AX, mb(AX, off))
		g.a.store(mb(BP, q.off), AX, 8)
		g.a.storeImm(mb(BP, n.off), u.Len(), 8)
		top := g.newLabel()
		g.place(top)
		g.hashValue(q, 0, h, u.Elem())
		g.a.aluMI(aADD, 8, mb(BP, q.off), es)
		g.a.aluMI(aSUB, 8, mb(BP, n.off), 1)
		g.jcc(ccNE, top)
	default:
		giveUp("hash of %v", t)
	}
}

// genWrapper: a method reached through another receiver type: the pointer form of a value method, a method
// promoted from an embedded field, or the method of an instantiated generic type.
func (c *compiler) genWrapper(r wrapperReq) {
	msig := r.method.Type().(*types.Signature)
	if r.instance {
		// the method itself, compiled for these type arguments
		origin := r.method.Origin()
		rt := msig.Recv().Type()
		if p, ok := rt.(*types.Pointer); ok {
			rt = p.Elem()
		}
		named := types.Unalias(rt).(*types.Named)
		targs := make([]types.Type, named.TypeArgs().Len())
		for i := range targs {
			targs[i] = named.TypeArgs().At(i)
		}
		sym := c.instantiate(origin, origin.Type().(*types.Signature).RecvTypeParams(), targs, named)
		if sym != r.sym {
			giveUp("method of an instantiated type: %s is named %s by the code generator", r.sym.Name, sym.Name)
		}
		return
	}
	sig := types.NewSignatureType(param(r.recv), nil, nil, msig.Params(), msig.Results(), msig.Variadic())
	plain := types.NewSignatureType(nil, nil, nil, msig.Params(), msig.Results(), msig.Variadic())
	g := c.newGen(r.sym.Name, sig, c.info, nil)
	g.out.sym = r.sym
	g.out.flags |= obj.WRAPPER
	if r.dupok {
		g.out.flags |= obj.DUPOK
	}
	g.compileFunc(sig.Recv(), c.pkgPos(), c.pkgPos(), nil, func() {
		path := r.sel.Index()
		cl, ai, recv := g.methodOn(g.paramLoc(g.abi.in[0]), r.recv, r.method, path, plain, nil)
		args := []loc{recv}
		for i := 1; i < len(g.abi.in); i++ {
			args = append(args, g.paramLoc(g.abi.in[i]))
		}
		rs := g.emitCall(ai, args, cl)
		for i, res := range rs {
			g.move(g.resHome[i], res, g.abi.out[i].typ)
		}
	})
}
