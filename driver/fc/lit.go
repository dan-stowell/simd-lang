// lit.go: composite literals.
package main

import (
	"go/ast"
	"go/constant"
	"go/types"
)

// compositeTo builds the literal e, of type t, at dst (memory that stays put: a frame slot or the target
// of a pointer held in a frame slot).
func (g *gen) compositeTo(e *ast.CompositeLit, dst loc, t types.Type) {
	if dst.kind == lRegInd {
		dst = g.stash(dst, t)
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		g.zero(dst, t)
		offs := g.c.fieldOffsets(u)
		for i, el := range e.Elts {
			fi := i
			val := el
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				f := g.info.Uses[kv.Key.(*ast.Ident)].(*types.Var)
				fi = -1
				for j := 0; j < u.NumFields(); j++ {
					if u.Field(j) == f || u.Field(j).Name() == f.Name() && samePkg(u.Field(j).Pkg(), f.Pkg()) {
						fi = j
					}
				}
				if fi < 0 {
					// a field promoted from an embedded struct
					o, path, _ := types.LookupFieldOrMethod(t, false, f.Pkg(), f.Name())
					if _, ok := o.(*types.Var); !ok {
						giveUp("field %s of a literal", f.Name())
					}
					off := int64(0)
					ft := t
					for _, pi := range path {
						st, ok := ft.Underlying().(*types.Struct)
						if !ok {
							giveUp("field %s of a literal, promoted through a pointer", f.Name())
						}
						off += g.c.fieldOffsets(st)[pi]
						ft = st.Field(pi).Type()
					}
					g.elemTo(kv.Value, dst.plus(off), ft)
					continue
				}
				val = kv.Value
			}
			g.elemTo(val, dst.plus(offs[fi]), u.Field(fi).Type())
		}
	case *types.Array:
		g.zero(dst, t)
		g.arrayElems(e, dst, u.Elem())
	case *types.Slice:
		n := g.litLen(e)
		if n == 0 {
			// an empty, non-nil slice
			g.a.lea(AX, msym(g.c.rtVar("zerobase"), 0))
			m := g.m(dst, R13)
			g.a.store(m, AX, 8) // TODO write barrier is not needed: zerobase is not in the heap
			g.a.storeImm(m.plus(8), 0, 8)
			g.a.storeImm(m.plus(16), 0, 8)
			return
		}
		sl := g.newBacking(u.Elem(), n, t, e)
		g.arrayElems(e, loc{kind: lInd, slot: sl.off}, u.Elem())
		g.move(dst, sl, t)
	case *types.Map:
		g.mapLit(e, dst, t)
	case *types.Pointer:
		// &T{...} with the & elided inside a literal
		p := g.addressOf(e)
		g.move(dst, p, t)
	default:
		giveUp("composite literal of %v", t)
	}
}

// elemTo stores one element of a literal.
func (g *gen) elemTo(val ast.Expr, dst loc, t types.Type) {
	m := g.mark()
	if cl, ok := val.(*ast.CompositeLit); ok {
		lt := g.typeOf(cl)
		if _, isPtr := lt.Underlying().(*types.Pointer); isPtr || cl.Type == nil && isPtrTo(t) {
			// elided &T{...}
			et := t.Underlying().(*types.Pointer).Elem()
			if !g.siteHeap(cl) {
				obj := g.frameObject(et)
				g.compositeTo(cl, obj, et)
				g.move(dst, g.frameAddr(obj), t)
				g.release(m)
				return
			}
			g.a.lea(AX, msym(g.c.typeSym(et), 0))
			g.callRT("newobject")
			p := g.ptrTemp()
			g.a.store(mb(BP, p.off), AX, 8)
			g.compositeTo(cl, loc{kind: lInd, slot: p.off}, et)
			g.move(dst, p, t)
			g.release(m)
			return
		}
		if types.Identical(lt, t) {
			g.compositeTo(cl, dst, t)
			g.release(m)
			return
		}
	}
	g.move(dst, g.exprAs(val, t), t)
	g.release(m)
}

func isPtrTo(t types.Type) bool {
	_, ok := t.Underlying().(*types.Pointer)
	return ok
}

// litLen: the length of an array or slice literal (one past its largest index).
func (g *gen) litLen(e *ast.CompositeLit) int64 {
	var n, i int64
	for _, el := range e.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			v, _ := constant.Int64Val(constant.ToInt(g.info.Types[kv.Key].Value))
			i = v
		}
		i++
		if i > n {
			n = i
		}
	}
	return n
}

func (g *gen) arrayElems(e *ast.CompositeLit, dst loc, et types.Type) {
	es := g.c.sizeof(et)
	var i int64
	for _, el := range e.Elts {
		val := el
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			v, _ := constant.Int64Val(constant.ToInt(g.info.Types[kv.Key].Value))
			i = v
			val = kv.Value
		}
		g.elemTo(val, dst.plus(i*es), et)
		i++
	}
}
