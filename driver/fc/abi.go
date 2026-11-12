// abi.go: ABIInternal on amd64, as cmd/compile/internal/abi assigns it: which registers or stack slots carry
// each parameter and result of a signature.
package main

import (
	"go/types"
)

var intArgRegs = []int{AX, BX, CX, DI, SI, R8, R9, R10, R11}

const numFloatArgRegs = 15

// part is one register-sized piece of a value: its offset in the value, its size, and what it is.
type part struct {
	off   int64
	size  int
	float bool
	ptr   bool
	reg   int // assigned register (register-assigned values only)
}

type abiParam struct {
	typ   types.Type
	parts []part // nil: on the stack
	off   int64  // frame offset from the first argument byte: the stack slot, or the spill slot of a register value
}

type abiInfo struct {
	in, out  []abiParam
	argWidth int64 // stack arguments, stack results and the spill area
}

func align(n, a int64) int64 { return (n + a - 1) &^ (a - 1) }

// flatten appends the register-sized parts of t; ok is false if t cannot travel in registers.
func (c *compiler) flatten(t types.Type, off int64, ps []part) ([]part, bool) {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsString != 0:
			return append(ps, part{off: off, size: 8, ptr: true}, part{off: off + 8, size: 8}), true
		case u.Info()&types.IsComplex != 0:
			h := int(c.sizeof(t) / 2)
			return append(ps, part{off: off, size: h, float: true}, part{off: off + int64(h), size: h, float: true}), true
		case u.Info()&types.IsFloat != 0:
			return append(ps, part{off: off, size: int(c.sizeof(t)), float: true}), true
		case u.Kind() == types.UnsafePointer:
			return append(ps, part{off: off, size: 8, ptr: true}), true
		}
		return append(ps, part{off: off, size: int(c.sizeof(t))}), true
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return append(ps, part{off: off, size: 8, ptr: true}), true
	case *types.Slice:
		return append(ps, part{off: off, size: 8, ptr: true}, part{off: off + 8, size: 8}, part{off: off + 16, size: 8}), true
	case *types.Interface:
		return append(ps, part{off: off, size: 8, ptr: true}, part{off: off + 8, size: 8, ptr: true}), true
	case *types.Struct:
		offs := c.fieldOffsets(u)
		ok := true
		for i := 0; i < u.NumFields() && ok; i++ {
			if c.sizeof(u.Field(i).Type()) == 0 {
				continue
			}
			ps, ok = c.flatten(u.Field(i).Type(), off+offs[i], ps)
		}
		return ps, ok
	case *types.Array:
		switch u.Len() {
		case 0:
			return ps, true
		case 1:
			return c.flatten(u.Elem(), off, ps)
		}
		return ps, false
	}
	panic("flatten: " + t.String())
}

type abiState struct {
	c            *compiler
	ints, floats int
	stack, spill int64
}

func (s *abiState) assign(t types.Type, isResult bool) abiParam {
	p := abiParam{typ: t, off: -1}
	size := s.c.sizeof(t)
	if size != 0 {
		if ps, ok := s.c.flatten(t, 0, nil); ok {
			ni, nf := 0, 0
			for _, q := range ps {
				if q.float {
					nf++
				} else {
					ni++
				}
			}
			if ni <= len(intArgRegs)-s.ints && nf <= numFloatArgRegs-s.floats {
				for i := range ps {
					if ps[i].float {
						ps[i].reg = X0 + s.floats
						s.floats++
					} else {
						ps[i].reg = intArgRegs[s.ints]
						s.ints++
					}
				}
				p.parts = ps
			}
		}
	}
	a := s.c.alignof(t)
	if p.parts == nil {
		s.stack = align(s.stack, a)
		p.off = s.stack
		s.stack += size
	} else if !isResult {
		s.spill = align(s.spill, a)
		p.off = s.spill
		s.spill += size
	}
	return p
}

// abiOf analyzes a signature; the receiver, if any, is the first parameter.
func (c *compiler) abiOf(sig *types.Signature) *abiInfo {
	var in, out []types.Type
	if r := sig.Recv(); r != nil {
		in = append(in, r.Type())
	}
	for i := 0; i < sig.Params().Len(); i++ {
		in = append(in, sig.Params().At(i).Type())
	}
	for i := 0; i < sig.Results().Len(); i++ {
		out = append(out, sig.Results().At(i).Type())
	}
	return c.abiOfTypes(in, out)
}

func (c *compiler) abiOfTypes(in, out []types.Type) *abiInfo {
	s := abiState{c: c}
	info := &abiInfo{}
	for _, t := range in {
		info.in = append(info.in, s.assign(t, false))
	}
	s.stack = align(s.stack, 8)
	s.ints, s.floats = 0, 0
	for _, t := range out {
		info.out = append(info.out, s.assign(t, true))
	}
	spillBase := align(s.stack, 8)
	for i := range info.in {
		if info.in[i].parts != nil {
			info.in[i].off += spillBase
		}
	}
	info.argWidth = spillBase + align(s.spill, 8)
	return info
}
