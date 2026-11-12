// rtype_alg.go: what a descriptor says about the memory of a value: where its pointers are (GC masks), how
// two values are compared and how one is hashed (ports of cmd/compile/internal/types size.go/alg.go,
// typebits, and reflectdata alg.go), and the group type of a map.
package main

import (
	"fmt"
	"go/types"
	"internal/buildcfg"
	"strings"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

// ptrData is the size of the prefix of a t that holds its pointers (stock: types.PtrDataSize).
func (c *compiler) ptrData(t types.Type) int64 {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Kind() == types.UnsafePointer || u.Info()&types.IsString != 0 {
			return 8
		}
		return 0
	case *types.Pointer:
		if notInHeap(u.Elem()) {
			return 0
		}
		return 8
	case *types.Slice:
		if notInHeap(u.Elem()) {
			return 0
		}
		return 8
	case *types.Chan, *types.Map, *types.Signature:
		return 8
	case *types.Interface:
		return 16
	case *types.Struct:
		var offs []int64
		for i := u.NumFields() - 1; i >= 0; i-- {
			if n := c.ptrData(u.Field(i).Type()); n > 0 {
				if offs == nil {
					offs = c.fieldOffsets(u)
				}
				return offs[i] + n
			}
		}
		return 0
	case *types.Array:
		if u.Len() > 0 {
			if n := c.ptrData(u.Elem()); n > 0 {
				return c.sizeof(u.Elem())*(u.Len()-1) + n
			}
		}
		return 0
	}
	giveUp("pointer data of %v", t)
	return 0
}

// setPtrMask sets the bit of every word of a t at off that the collector must follow (stock: typebits.Set).
// Of an interface that is the data word only: the first word points to an itab or a type, never to an
// object the collector owns.
func (c *compiler) setPtrMask(t types.Type, off int64, mask []byte) {
	if c.ptrData(t) == 0 {
		return
	}
	set := func(off int64) { mask[off/64] |= 1 << (uint(off/8) % 8) }
	switch u := t.Underlying().(type) {
	case *types.Basic, *types.Pointer, *types.Chan, *types.Map, *types.Signature, *types.Slice:
		set(off)
	case *types.Interface:
		set(off + 8)
	case *types.Array:
		es := c.sizeof(u.Elem())
		if es == 0 {
			return
		}
		for i := int64(0); i < u.Len(); i++ {
			c.setPtrMask(u.Elem(), off+i*es, mask)
		}
	case *types.Struct:
		offs := c.fieldOffsets(u)
		for i := 0; i < u.NumFields(); i++ {
			c.setPtrMask(u.Field(i).Type(), off+offs[i], mask)
		}
	}
}

const maxPtrmaskBytes = 16 // internal/abi.MaxPtrmaskBytes

// gcSym is the symbol a descriptor's GCData points to: the pointer mask, or for a type with more than
// 128 words of pointer data a word in which the runtime builds the mask on demand (stock: dgcsym).
func (c *compiler) gcSym(t types.Type) (s *obj.LSym, onDemand bool, ptrdata int64) {
	ptrdata = c.ptrData(t)
	if ptrdata/8 <= maxPtrmaskBytes*8 {
		n := (ptrdata/8 + 7) / 8
		n = (n + 7) &^ 7
		mask := make([]byte, n)
		c.setPtrMask(t, 0, mask)
		s = c.lookup(fmt.Sprintf("runtime.gcbits.%x", mask), obj.ABI0, "")
		if !s.OnList() {
			for i, x := range mask {
				s.WriteInt(c.ctxt, int64(i), 1, int64(x))
			}
			s.Set(obj.AttrLocal, true)
			s.Set(obj.AttrDuplicateOK, true)
			s.Set(obj.AttrContentAddressable, true)
			s.Align = 8
			c.defData(s, objabi.SRODATA, n)
		}
		return s, false, ptrdata
	}
	s = c.lookup("type:.gcmask."+c.typeLinkName(t), obj.ABI0, "")
	if !s.OnList() {
		s.Set(obj.AttrLocal, true)
		s.Set(obj.AttrDuplicateOK, true)
		c.defData(s, objabi.SNOPTRBSS, 8)
	}
	return s, true, ptrdata
}

// ---- algorithms

type algKind uint8

const (
	algMem     algKind = iota // compared and hashed as memory
	algOther                  // one special comparison (float, string, interface): stock AFLOAT32 ... AINTER
	algSpecial                // needs a comparison function of its own
	algNoeq                   // not comparable
	algNoalg                  // not comparable, and marked so: a compiler-made type
)

var algPriority = [...]int8{algMem: -1, algOther: 0, algSpecial: 1, algNoeq: 2, algNoalg: 3}

// algKind is how values of t are compared (stock: types.AlgType, computed in CalcSize).
func (c *compiler) algKind(t types.Type) algKind {
	t = types.Unalias(t)
	if c.rtNoalg[t] {
		return algNoalg
	}
	a := algMem
	set := func(b algKind) {
		if algPriority[b] > algPriority[a] {
			a = b
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Info()&(types.IsFloat|types.IsComplex|types.IsString) != 0 {
			return algOther
		}
		return algMem
	case *types.Pointer, *types.Chan:
		return algMem
	case *types.Signature, *types.Map, *types.Slice:
		return algNoeq
	case *types.Interface:
		return algOther
	case *types.Array:
		switch e := c.algKind(u.Elem()); e {
		case algMem, algNoeq, algNoalg:
			set(e)
		default:
			switch u.Len() {
			case 0:
			case 1:
				set(e)
			default:
				set(algSpecial)
			}
		}
	case *types.Struct:
		n := u.NumFields()
		if n == 1 && u.Field(0).Name() != "_" {
			set(c.algKind(u.Field(0).Type()))
			break
		}
		offs := c.fieldOffsets(u)
		size := c.sizeof(u)
		for i := 0; i < n; i++ {
			f := u.Field(i)
			e := c.algKind(f.Type())
			switch e {
			case algNoeq, algNoalg:
			case algMem:
				end := size
				if i+1 < n {
					end = offs[i+1]
				}
				if f.Name() == "_" || offs[i]+c.sizeof(f.Type()) != end {
					e = algSpecial
				}
			default:
				e = algSpecial
			}
			set(e)
		}
	default:
		giveUp("algorithm of %v", t)
	}
	return a
}

// eqSignature describes the comparisons an equality function for t makes, in order; types with the same
// signature share the function (stock: reflectdata.eqSignature; amd64 merges loads, so no alignment part).
//
//	M%d  %d bytes compared as memory      K%d  %d bytes skipped
//	F    float32    G  float64    S  string    I  non-empty interface    E  empty interface
//	[%d%s]  the signature %s repeated %d times
func (c *compiler) eqSignature(t types.Type) string {
	e := eqSigBuilder{c: c}
	e.build(t)
	e.flush(true)
	return e.r.String()
}

type eqSigBuilder struct {
	c       *compiler
	r       strings.Builder
	regMem  int64
	skipMem int64
}

func (e *eqSigBuilder) flush(atEnd bool) {
	if e.regMem > 0 {
		fmt.Fprintf(&e.r, "M%d", e.regMem)
		e.regMem = 0
	}
	if e.skipMem > 0 {
		if !atEnd {
			fmt.Fprintf(&e.r, "K%d", e.skipMem)
		}
		e.skipMem = 0
	}
}

func (e *eqSigBuilder) regular(n int64) {
	if e.regMem == 0 {
		e.flush(false)
	}
	e.regMem += n
}

func (e *eqSigBuilder) skip(n int64) {
	if e.skipMem == 0 {
		e.flush(false)
	}
	e.skipMem += n
}

func (e *eqSigBuilder) one(b byte) {
	e.flush(false)
	e.r.WriteByte(b)
}

func (e *eqSigBuilder) build(t types.Type) {
	c := e.c
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Kind() == types.Float32:
			e.one('F')
		case u.Kind() == types.Float64:
			e.one('G')
		case u.Kind() == types.Complex64:
			e.one('F')
			e.one('F')
		case u.Kind() == types.Complex128:
			e.one('G')
			e.one('G')
		case u.Info()&types.IsString != 0:
			e.one('S')
		default:
			e.regular(c.sizeof(t))
		}
	case *types.Pointer, *types.Chan:
		e.regular(8)
	case *types.Interface:
		if u.NumMethods() == 0 {
			e.one('E')
		} else {
			e.one('I')
		}
	case *types.Struct:
		offs := c.fieldOffsets(u)
		var off int64
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if f.Name() == "_" {
				continue
			}
			if off < offs[i] {
				e.skip(offs[i] - off)
			}
			e.build(f.Type())
			off = offs[i] + c.sizeof(f.Type())
		}
		if size := c.sizeof(u); off < size {
			e.skip(size - off)
		}
	case *types.Array:
		if c.algKind(t) == algMem {
			e.regular(c.sizeof(t))
			break
		}
		et := u.Elem()
		n := u.Len()
		switch n {
		case 0:
		case 1:
			e.build(et)
		default:
			es := c.sizeof(et)
			if es == 0 {
				break
			}
			unroll := max(1, 32/es)
			for n%unroll != 0 {
				e.build(et)
				n--
			}
			if n == 0 {
				break
			}
			if n == unroll {
				for range n {
					e.build(et)
				}
				break
			}
			e.flush(false)
			fmt.Fprintf(&e.r, "[%d", n/unroll)
			for range unroll {
				e.build(et)
			}
			e.flush(false)
			e.r.WriteByte(']')
		}
	default:
		giveUp("equality signature of %v", t)
	}
}

func sigNum(s string) (int64, string) {
	n := 0
	var x int64
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		x = x*10 + int64(s[n]-'0')
		n++
	}
	return x, s[n:]
}

// sysClosure is the closure of a runtime function, runtime.<name>·f, defined (DUPOK) in every object that
// uses it.
func (c *compiler) sysClosure(name string) *obj.LSym {
	s := c.lookup("runtime."+name+"·f", obj.ABI0, "runtime")
	c.mu.Lock() // the code generator defines closures of functions under c.mu too (funcValue)
	if !s.OnList() {
		s.WriteAddr(c.ctxt, 0, 8, c.lookup("runtime."+name, obj.ABIInternal, "runtime"), 0)
		s.Type = objabi.SRODATA
		s.Size = 8
		s.Set(obj.AttrDuplicateOK, true)
		s.Set(obj.AttrOnList, true)
		c.ctxt.Data = append(c.ctxt.Data, s)
	}
	c.mu.Unlock()
	return s
}

// algClosure is the closure a descriptor refers to for the signature sig: of a runtime function when one
// fits, else type:.eqfunc.<sig> / type:.hashfunc.<sig> around runtime.mem{equal,hash}_varlen or around a
// function the code generator must write (type:.eq.<sig> / type:.hash.<sig>).
func (c *compiler) algClosure(t types.Type, sig string, hash bool) *obj.LSym {
	rt := map[string]string{"": "mem%s0", "M1": "mem%s8", "M2": "mem%s16", "M4": "mem%s32", "M8": "mem%s64",
		"M16": "mem%s128", "S": "str%s", "I": "inter%s", "E": "nilinter%s", "F": "f32%s", "G": "f64%s",
		"FF": "c64%s", "GG": "c128%s"}
	word, closure, fn := "equal", ".eqfunc.", ".eq."
	if hash {
		word, closure, fn = "hash", ".hashfunc.", ".hash."
	}
	if f, ok := rt[sig]; ok {
		return c.sysClosure(fmt.Sprintf(f, word))
	}
	s := c.lookup("type:"+closure+sig, obj.ABI0, "")
	if s.OnList() {
		return s
	}
	if sig[0] == 'M' {
		if n, rest := sigNum(sig[1:]); rest == "" {
			s.WriteAddr(c.ctxt, 0, 8, c.lookup("runtime.mem"+word+"_varlen", obj.ABIInternal, "runtime"), 0)
			s.WriteInt(c.ctxt, 8, 8, n)
			s.Set(obj.AttrDuplicateOK, true)
			c.defData(s, objabi.SRODATA, 16)
			return s
		}
	}
	f := c.lookup("type:"+fn+sig, obj.ABIInternal, "")
	if !c.rtAlg[f.Name] {
		c.rtAlg[f.Name] = true
		if hash {
			c.needHash = append(c.needHash, algReq{sig, f, t})
		} else {
			c.needEq = append(c.needEq, algReq{sig, f, t})
		}
	}
	s.WriteAddr(c.ctxt, 0, 8, f, 0)
	s.Set(obj.AttrDuplicateOK, true)
	c.defData(s, objabi.SRODATA, 8)
	return s
}

// eqClosure is a descriptor's Equal: nil when t is not comparable (stock: geneq).
func (c *compiler) eqClosure(t types.Type) *obj.LSym {
	switch c.algKind(t) {
	case algNoeq, algNoalg:
		return nil
	}
	return c.algClosure(t, c.eqSignature(t), false)
}

// hashClosure is a map type's Hasher for the key type t (stock: genhash).
func (c *compiler) hashClosure(t types.Type) *obj.LSym {
	return c.algClosure(t, c.eqSignature(t), true)
}

// ---- maps

const (
	mapGroupSlots   = 8   // internal/abi.MapGroupSlots
	mapMaxKeyBytes  = 128 // internal/abi.MapMaxKeyBytes
	mapMaxElemBytes = 128
)

// mapGroup is the group type of a map, which the runtime allocates: the control word and eight slots
// (stock: reflectdata.MapGroupType). Its fields belong to no package, and it and its parts have no equality.
func (c *compiler) mapGroup(m *types.Map) *types.Struct {
	key := c.typeLinkName(m)
	if g := c.rtGroups[key]; g != nil {
		return g
	}
	kt, et := m.Key(), m.Elem()
	if c.sizeof(kt) > mapMaxKeyBytes {
		kt = types.NewPointer(kt)
	}
	if c.sizeof(et) > mapMaxElemBytes {
		et = types.NewPointer(et)
	}
	field := func(name string, t types.Type) *types.Var { return types.NewField(0, nil, name, t, false) }
	noalg := func(t types.Type) types.Type { c.rtNoalg[t] = true; return t }
	var fields []*types.Var
	if buildcfg.Experiment.MapSplitGroup {
		fields = []*types.Var{
			field("ctrl", types.Typ[types.Uint64]),
			field("keys", noalg(types.NewArray(kt, mapGroupSlots))),
			field("elems", noalg(types.NewArray(et, mapGroupSlots))),
		}
	} else {
		slot := noalg(types.NewStruct([]*types.Var{field("key", kt), field("elem", et)}, nil))
		fields = []*types.Var{
			field("ctrl", types.Typ[types.Uint64]),
			field("slots", noalg(types.NewArray(slot, mapGroupSlots))),
		}
	}
	g := types.NewStruct(fields, nil)
	c.rtNoalg[g] = true
	c.rtGroup[g] = m
	c.rtGroups[key] = g
	return g
}

// needKeyUpdate: must an assignment to an existing key store the key again (stock: needkeyupdate)?
func needKeyUpdate(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsFloat|types.IsComplex|types.IsString) != 0
	case *types.Interface:
		return true
	case *types.Array:
		return needKeyUpdate(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if needKeyUpdate(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

// hashMightPanic: can hashing a key of type t panic (it holds an interface)?
func hashMightPanic(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return true
	case *types.Array:
		return hashMightPanic(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if hashMightPanic(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}
