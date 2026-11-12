// rtype_write.go: writing type descriptors and interface tables (a port of reflectdata.writeType and its
// helpers; the layouts are internal/abi's Type, UncommonType, ArrayType ... MapType, ITab on amd64).
package main

import (
	"encoding/binary"
	"go/token"
	"go/types"
	"internal/abi"
	"internal/buildcfg"
	"strconv"
	"strings"

	"cmd/internal/hash"
	"cmd/internal/obj"
	"cmd/internal/objabi"
)

const (
	rtTypeSize     = 48 // abi.Type
	rtUncommonSize = 16 // abi.UncommonType
	rtMethodSize   = 16 // abi.Method
	rtIMethodSize  = 8  // abi.IMethod
	rtFieldSize    = 24 // abi.StructField
)

// typeHash is the hash in a descriptor and in an itab, which type switches compare (stock: types.TypeHash).
func (c *compiler) typeHash(t types.Type) uint32 {
	h := hash.Sum32([]byte(c.typeLinkName(t)))
	return binary.LittleEndian.Uint32(h[:4])
}

// writtenByBasic: is t one of the descriptors package runtime's object carries for everybody: a basic
// type, any, error, a slice of one of those, func(error) string (stock: writtenByWriteBasicTypes)?
func writtenByBasic(t types.Type) bool {
	t = types.Unalias(t)
	if s, ok := t.(*types.Signature); ok {
		if s.Params().Len() == 1 && s.Results().Len() == 1 && isErrorType(s.Params().At(0).Type()) {
			if b, ok := types.Unalias(s.Results().At(0).Type()).(*types.Basic); ok && b.Kind() == types.String {
				return true
			}
		}
	}
	if s, ok := t.(*types.Slice); ok {
		t = types.Unalias(s.Elem())
	}
	switch u := t.(type) {
	case *types.Basic:
		return true
	case *types.Interface:
		return u.NumMethods() == 0 && u.IsMethodSet()
	}
	return isErrorType(t)
}

func isErrorType(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() == nil && n.Obj().Name() == "error"
}

// needEmit: does this object define the descriptor (and the wrappers) of the type whose base is t (stock:
// reflectdata.NeedEmit)?
func (c *compiler) needEmit(t types.Type) bool {
	if t == nil {
		return false
	}
	if writtenByBasic(t) {
		return c.path == "runtime"
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return true // an unnamed type: every object that mentions it carries it
	}
	if n.Obj().Pkg() == c.tpkg {
		return true
	}
	return n.TypeArgs().Len() > 0 // an instance of a generic type, perhaps never made before
}

// typeBase is the type whose package decides where t's descriptor lives: T for *T.
func typeBase(t types.Type) types.Type {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok && hasSym(p.Elem()) {
		return types.Unalias(p.Elem())
	}
	return t
}

// typePkg is the package of a descriptor's uncommon type (stock: typePkg).
func typePkg(t types.Type) (*types.Package, bool) {
	t = types.Unalias(t)
	if !hasSym(t) {
		switch u := t.(type) {
		case *types.Array:
			t = types.Unalias(u.Elem())
		case *types.Slice:
			t = types.Unalias(u.Elem())
		case *types.Pointer:
			t = types.Unalias(u.Elem())
		case *types.Chan:
			t = types.Unalias(u.Elem())
		}
	}
	switch u := t.(type) {
	case *types.Named:
		if u.Obj().Pkg() != nil {
			return u.Obj().Pkg(), true
		}
	case *types.Basic:
		if u.Kind() == types.UnsafePointer {
			return types.Unsafe, true
		}
	}
	return nil, false
}

// pkgPathSym is the name data holding a package's path (stock: dimportpath). p == nil is the package of
// the fields of compiler-made structs: no path.
func (c *compiler) pkgPathSym(p *types.Package) *obj.LSym {
	prefix, path := "", ""
	if p != nil {
		prefix, path = c.pkgPrefix(p), p.Path()
		if p == c.tpkg {
			path = c.path
		}
	}
	s := c.lookup("type:.importpath."+prefix+".", obj.ABI0, "")
	if !s.OnList() {
		n := c.nameData(s, path, "", nil, false, false, false)
		s.Set(obj.AttrDuplicateOK, true)
		s.Set(obj.AttrContentAddressable, true)
		s.Align = 1
		c.defData(s, objabi.SRODATA, n)
	}
	return s
}

// nameData writes an abi.Name into s: flags, the name, the tag, the package path.
func (c *compiler) nameData(s *obj.LSym, name, tag string, pkg *types.Package, hasPkg, exported, embedded bool) int64 {
	var bits byte
	if exported {
		bits |= 1 << 0
	}
	if tag != "" {
		bits |= 1 << 1
	}
	if hasPkg {
		bits |= 1 << 2
	}
	if embedded {
		bits |= 1 << 3
	}
	b := []byte{bits}
	b = binary.AppendUvarint(b, uint64(len(name)))
	b = append(b, name...)
	if tag != "" {
		b = binary.AppendUvarint(b, uint64(len(tag)))
		b = append(b, tag...)
	}
	ot := s.WriteBytes(c.ctxt, 0, b)
	if hasPkg {
		s.WriteOff(c.ctxt, ot, c.pkgPathSym(pkg), 0)
		ot += 4
	}
	return ot
}

// nameSym is the name data of a type, a field or a method (stock: dname). Names without a package are
// shared by every object; names of unexported methods of another package are numbered.
func (c *compiler) nameSym(name, tag string, pkg *types.Package, hasPkg, exported, embedded bool) *obj.LSym {
	sname := "type:.namedata."
	if !hasPkg {
		switch {
		case name == "" && exported:
			sname += "-noname-exported." + tag
		case name == "":
			sname += "-noname-unexported." + tag
		case exported:
			sname += name + "." + tag
		default:
			sname += name + "-" + tag
		}
	} else {
		sname += objabi.PathToPrefix(c.path) + "." + strconv.Itoa(c.rtNames)
		c.rtNames++
	}
	if embedded {
		sname += ".embedded"
	}
	s := c.lookup(sname, obj.ABI0, "")
	if s.OnList() {
		return s
	}
	n := c.nameData(s, name, tag, pkg, hasPkg, exported, embedded)
	s.Set(obj.AttrDuplicateOK, true)
	s.Set(obj.AttrContentAddressable, true)
	s.Align = 1
	c.defData(s, objabi.SRODATA, n)
	return s
}

func (c *compiler) abiKind(t types.Type) abi.Kind {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Bool:
			return abi.Bool
		case types.Int:
			return abi.Int
		case types.Int8:
			return abi.Int8
		case types.Int16:
			return abi.Int16
		case types.Int32:
			return abi.Int32
		case types.Int64:
			return abi.Int64
		case types.Uint:
			return abi.Uint
		case types.Uint8:
			return abi.Uint8
		case types.Uint16:
			return abi.Uint16
		case types.Uint32:
			return abi.Uint32
		case types.Uint64:
			return abi.Uint64
		case types.Uintptr:
			return abi.Uintptr
		case types.Float32:
			return abi.Float32
		case types.Float64:
			return abi.Float64
		case types.Complex64:
			return abi.Complex64
		case types.Complex128:
			return abi.Complex128
		case types.String:
			return abi.String
		case types.UnsafePointer:
			return abi.UnsafePointer
		}
	case *types.Pointer:
		return abi.Pointer
	case *types.Struct:
		return abi.Struct
	case *types.Interface:
		return abi.Interface
	case *types.Chan:
		return abi.Chan
	case *types.Map:
		return abi.Map
	case *types.Array:
		return abi.Array
	case *types.Slice:
		return abi.Slice
	case *types.Signature:
		return abi.Func
	}
	giveUp("type descriptor for %v", t)
	return 0
}

// isPtrElem: did the stock compiler ever form the type *t for the pointer type t? Only then does t's
// descriptor point to *t's. That is a fact about the stock compiler's run, not about the program: in mixed
// mode its object answers; otherwise the answer is whether the package's own types mention *t.
func (c *compiler) isPtrElem(t types.Type) bool {
	if c.stockDefs != nil {
		return c.stockDefs["type:*"+c.typeSymName(t)]
	}
	if !c.rtPtrSet {
		c.rtPtrSet = true
		c.rtPtrTo = map[string]bool{}
		c.forEachType(func(x types.Type) {
			if p, ok := x.(*types.Pointer); ok {
				if _, ok := types.Unalias(p.Elem()).(*types.Pointer); ok && !mentionsTypeParamRT(p, nil) {
					c.rtPtrTo[c.typeLinkName(p.Elem())] = true
				}
			}
		})
	}
	return c.rtPtrTo[c.typeLinkName(t)]
}

// writeCommon writes the abi.Type at the start of t's descriptor (stock: dcommontype).
func (c *compiler) writeCommon(s *obj.LSym, t types.Type) {
	eq := c.eqClosure(t)
	pt, isPtr := t.Underlying().(*types.Pointer)
	var sptr *obj.LSym
	weak := true
	if !isPtr || c.isPtrElem(t) {
		tptr := types.NewPointer(t)
		if hasSym(t) || len(c.methods(tptr)) > 0 {
			weak = false
		}
		sptr = c.writeType(tptr)
	}
	gc, onDemand, ptrdata := c.gcSym(t)

	var tflag abi.TFlag
	if hasSym(t) || len(c.methods(t)) > 0 {
		tflag |= abi.TFlagUncommon
	}
	if hasSym(t) {
		tflag |= abi.TFlagNamed
	}
	if c.algKind(t) == algMem {
		tflag |= abi.TFlagRegularMemory
	}
	if onDemand {
		tflag |= abi.TFlagGCMaskOnDemand
	}
	// the name of T is stored as "*T" without its first byte, so that T and *T share it
	exported := false
	p := c.typeNameString(t)
	if !strings.HasPrefix(p, "*") {
		p = "*" + p
		tflag |= abi.TFlagExtraStar
		exported = symExported(t)
	} else {
		exported = symExported(pt.Elem())
	}
	if isDirectIface(t) {
		tflag |= abi.TFlagDirectIface
	}
	align := c.alignof(t)
	s.WriteInt(c.ctxt, 0, 8, c.sizeof(t))
	s.WriteInt(c.ctxt, 8, 8, ptrdata)
	s.WriteInt(c.ctxt, 16, 4, int64(c.typeHash(t)))
	s.WriteInt(c.ctxt, 20, 1, int64(tflag))
	s.WriteInt(c.ctxt, 21, 1, align)
	s.WriteInt(c.ctxt, 22, 1, align)
	s.WriteInt(c.ctxt, 23, 1, int64(c.abiKind(t)))
	c.writePtr(s, 24, eq)
	c.writePtr(s, 32, gc)
	s.WriteOff(c.ctxt, 40, c.nameSym(p, "", nil, false, exported, false), 0)
	switch {
	case sptr == nil:
		s.WriteInt(c.ctxt, 44, 4, 0)
	case weak:
		s.WriteWeakOff(c.ctxt, 44, sptr, 0)
	default:
		s.WriteOff(c.ctxt, 44, sptr, 0)
	}
}

// symExported: does t have a name, and is it exported?
func symExported(t types.Type) bool {
	switch u := types.Unalias(t).(type) {
	case *types.Named:
		return token.IsExported(u.Obj().Name())
	case *types.Basic:
		return u.Kind() == types.UnsafePointer // "Pointer", of package unsafe
	}
	return false
}

func (c *compiler) writePtr(s *obj.LSym, off int64, target *obj.LSym) {
	if target == nil {
		s.WriteInt(c.ctxt, off, 8, 0)
	} else {
		s.WriteAddr(c.ctxt, off, 8, target, 0)
	}
}

func (c *compiler) writeMethodOff(s *obj.LSym, off int64, target *obj.LSym) {
	s.WriteInt(c.ctxt, off, 4, 0)
	s.AddRel(c.ctxt, obj.Reloc{Type: objabi.R_METHODOFF, Off: int32(off), Siz: 4, Sym: target})
}

// writeType returns t's descriptor symbol, defining it when this object is the one to (stock: writeType).
// Called with rtMu held.
func (c *compiler) writeType(t types.Type) *obj.LSym {
	t = types.Unalias(t)
	if b, ok := t.(*types.Basic); ok {
		t = types.Typ[b.Kind()] // byte is uint8, rune is int32
		if b.Info()&types.IsUntyped != 0 {
			giveUp("type descriptor for %v", t)
		}
	}
	s := c.lookup("type:"+c.typeSymName(t), obj.ABI0, "")
	if c.rtDone[s] {
		return s
	}
	c.rtDone[s] = true
	tbase := typeBase(t)
	if !c.needEmit(tbase) {
		return s
	}
	ms := c.methods(t)
	extra := hasSym(t) || len(ms) != 0

	var B int64
	dataAdd := int64(0)
	switch u := t.Underlying().(type) {
	default:
		B = rtTypeSize
	case *types.Array:
		B = rtTypeSize + 24
	case *types.Slice, *types.Pointer:
		B = rtTypeSize + 8
	case *types.Chan:
		B = rtTypeSize + 16
	case *types.Signature:
		B = rtTypeSize + 8
		dataAdd = int64(u.Params().Len()+u.Results().Len()) * 8
	case *types.Interface:
		B = rtTypeSize + 32
		dataAdd = int64(u.NumMethods()) * rtIMethodSize
	case *types.Map:
		B = rtTypeSize + 88
	case *types.Struct:
		B = rtTypeSize + 32
		dataAdd = int64(u.NumFields()) * rtFieldSize
	}
	C := B
	if extra {
		C = B + rtUncommonSize
	}
	D := C + dataAdd
	E := D + int64(len(ms))*rtMethodSize

	c.writeCommon(s, t)

	switch u := t.Underlying().(type) {
	case *types.Array:
		s1 := c.writeType(u.Elem())
		s2 := c.writeType(types.NewSlice(u.Elem()))
		c.writePtr(s, 48, s1)
		c.writePtr(s, 56, s2)
		s.WriteInt(c.ctxt, 64, 8, u.Len())
	case *types.Slice:
		c.writePtr(s, 48, c.writeType(u.Elem()))
	case *types.Chan:
		c.writePtr(s, 48, c.writeType(u.Elem()))
		dir := int64(abi.BothDir)
		switch u.Dir() {
		case types.RecvOnly:
			dir = int64(abi.RecvDir)
		case types.SendOnly:
			dir = int64(abi.SendDir)
		}
		s.WriteInt(c.ctxt, 56, 8, dir)
	case *types.Signature:
		var ts []types.Type
		for i := 0; i < u.Params().Len(); i++ {
			ts = append(ts, u.Params().At(i).Type())
		}
		for i := 0; i < u.Results().Len(); i++ {
			ts = append(ts, u.Results().At(i).Type())
		}
		for _, t1 := range ts {
			c.writeType(t1)
		}
		out := u.Results().Len()
		if u.Variadic() {
			out |= 1 << 15
		}
		s.WriteInt(c.ctxt, 48, 2, int64(u.Params().Len()))
		s.WriteInt(c.ctxt, 50, 2, int64(out))
		for i, t1 := range ts {
			c.writePtr(s, C+int64(i)*8, c.writeType(t1))
		}
	case *types.Interface:
		ims := ifaceMethods(u)
		mts := make([]*types.Signature, len(ims))
		for i, m := range ims {
			sig := m.Type().(*types.Signature)
			mts[i] = types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
			c.writeType(mts[i])
		}
		var tpkg *types.Package
		if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil {
			tpkg = n.Obj().Pkg()
			c.writePtr(s, 48, c.pkgPathSym(tpkg))
		} else {
			c.writePtr(s, 48, nil)
		}
		n := int64(len(ims))
		s.WriteAddr(c.ctxt, 56, 8, s, C)
		s.WriteInt(c.ctxt, 64, 8, n)
		s.WriteInt(c.ctxt, 72, 8, n)
		for i, m := range ims {
			hasPkg := !m.Exported() && m.Pkg() != tpkg
			off := C + int64(i)*rtIMethodSize
			s.WriteOff(c.ctxt, off, c.nameSym(m.Name(), "", m.Pkg(), hasPkg, m.Exported(), false), 0)
			s.WriteOff(c.ctxt, off+4, c.writeType(mts[i]), 0)
		}
	case *types.Map:
		c.writeMapType(s, t, u)
	case *types.Pointer:
		c.writePtr(s, 48, c.writeType(u.Elem()))
	case *types.Struct:
		n := u.NumFields()
		for i := 0; i < n; i++ {
			c.writeType(u.Field(i).Type())
		}
		// the package of the unexported field names, which all come from one package
		var spkg *types.Package
		hasPkg := false
		for i := 0; i < n; i++ {
			if f := u.Field(i); !token.IsExported(f.Name()) {
				spkg, hasPkg = f.Pkg(), true
				break
			}
		}
		if hasPkg {
			c.writePtr(s, 48, c.pkgPathSym(spkg))
		} else {
			c.writePtr(s, 48, nil)
		}
		s.WriteAddr(c.ctxt, 56, 8, s, C)
		s.WriteInt(c.ctxt, 64, 8, int64(n))
		s.WriteInt(c.ctxt, 72, 8, int64(n))
		offs := c.fieldOffsets(u)
		for i := 0; i < n; i++ {
			f := u.Field(i)
			off := C + int64(i)*rtFieldSize
			c.writePtr(s, off, c.nameSym(f.Name(), u.Tag(i), nil, false, token.IsExported(f.Name()), f.Embedded()))
			c.writePtr(s, off+8, c.writeType(f.Type()))
			s.WriteInt(c.ctxt, off+16, 8, offs[i])
		}
	}

	if extra {
		c.writeUncommon(s, B, t, ms, dataAdd)
	}

	dupok := true
	if n, ok := tbase.(*types.Named); ok {
		dupok = n.TypeArgs().Len() > 0
	} else if _, ok := tbase.(*types.Basic); ok {
		dupok = false
	}
	if c.path == "runtime" && s.Name == "type:*interface {}" {
		dupok = false // stock WriteBasicTypes writes it as *any, and any has a name
	}
	if dupok {
		s.Set(obj.AttrDuplicateOK, true)
	}
	// unnamed types that reflect can construct are listed, so that it finds these instead of making its own
	keep := false
	if !hasSym(t) {
		switch t.(type) {
		case *types.Pointer, *types.Array, *types.Chan, *types.Signature, *types.Map, *types.Slice, *types.Struct:
			keep = true
		}
	}
	if c.algKind(t) == algNoalg {
		keep = false
	}
	s.Set(obj.AttrMakeTypelink, keep)
	s.Align = 8
	c.defData(s, objabi.SRODATA, E)
	return s
}

// writeUncommon writes the abi.UncommonType at off and the method table after the type's own data.
func (c *compiler) writeUncommon(s *obj.LSym, off int64, t types.Type, ms []methodSig, dataAdd int64) {
	for _, m := range ms {
		c.writeType(m.typ)
	}
	tpkg, hasPkg := typePkg(t)
	if hasPkg {
		s.WriteOff(c.ctxt, off, c.pkgPathSym(tpkg), 0)
	} else {
		s.WriteInt(c.ctxt, off, 4, 0)
	}
	dataAdd += rtUncommonSize
	xcount := 0
	for _, m := range ms {
		if m.fn.Exported() {
			xcount++
		}
	}
	if len(ms) > 0xffff {
		giveUp("too many methods on %v", t)
	}
	s.WriteInt(c.ctxt, off+4, 2, int64(len(ms)))
	s.WriteInt(c.ctxt, off+6, 2, int64(xcount))
	s.WriteInt(c.ctxt, off+8, 4, dataAdd)
	for i, m := range ms {
		e := off + dataAdd + int64(i)*rtMethodSize
		mpkg := !m.fn.Exported() && (!hasPkg || m.fn.Pkg() != tpkg)
		s.WriteOff(c.ctxt, e, c.nameSym(m.fn.Name(), "", m.fn.Pkg(), mpkg, m.fn.Exported(), false), 0)
		c.writeMethodOff(s, e+4, c.writeType(m.mtype))
		c.writeMethodOff(s, e+8, m.isym)
		c.writeMethodOff(s, e+12, m.tsym)
	}
}

// writeMapType writes the rest of an abi.MapType (stock: writeMapType).
func (c *compiler) writeMapType(s *obj.LSym, t types.Type, u *types.Map) {
	g := c.mapGroup(u)
	s1 := c.writeType(u.Key())
	s2 := c.writeType(u.Elem())
	s3 := c.writeType(g)
	hasher := c.hashClosure(u.Key())
	goffs := c.fieldOffsets(g)
	var keysOff, keyStride, elemsOff, elemStride, elemOff int64
	if buildcfg.Experiment.MapSplitGroup {
		keysOff = goffs[1]
		keyStride = c.sizeof(g.Field(1).Type().(*types.Array).Elem())
		elemsOff = goffs[2]
		elemStride = c.sizeof(g.Field(2).Type().(*types.Array).Elem())
	} else {
		slot := g.Field(1).Type().(*types.Array).Elem().(*types.Struct)
		keysOff = goffs[1]
		keyStride = c.sizeof(slot)
		elemOff = c.fieldOffsets(slot)[1]
		elemsOff = keysOff + elemOff
		elemStride = keyStride
	}
	c.writePtr(s, 48, s1)
	c.writePtr(s, 56, s2)
	c.writePtr(s, 64, s3)
	c.writePtr(s, 72, hasher)
	s.WriteInt(c.ctxt, 80, 8, c.sizeof(g))
	s.WriteInt(c.ctxt, 88, 8, keysOff)
	s.WriteInt(c.ctxt, 96, 8, keyStride)
	s.WriteInt(c.ctxt, 104, 8, elemsOff)
	s.WriteInt(c.ctxt, 112, 8, elemStride)
	s.WriteInt(c.ctxt, 120, 8, elemOff)
	var flags int64
	if needKeyUpdate(u.Key()) {
		flags |= abi.MapNeedKeyUpdate
	}
	if hashMightPanic(u.Key()) {
		flags |= abi.MapHashMightPanic
	}
	if c.sizeof(u.Key()) > mapMaxKeyBytes {
		flags |= abi.MapIndirectKey
	}
	if c.sizeof(u.Elem()) > mapMaxElemBytes {
		flags |= abi.MapIndirectElem
	}
	s.WriteInt(c.ctxt, 128, 4, flags)
	if types.Type(u) != t {
		// a named map type keeps the unnamed one alive: both must hash alike under reflection
		s.AddRel(c.ctxt, obj.Reloc{Type: objabi.R_KEEP, Sym: c.writeType(u)})
	}
}

// writeITab defines go:itab.<T>,<I>: the interface's and the type's descriptors, the type's hash, and the
// functions for the interface's methods in the interface's order (stock: writeITab). Marking the type as
// used in an interface (R_USEIFACE) belongs to the function that uses the itab, as in the stock compiler.
func (c *compiler) writeITab(t, iface types.Type) *obj.LSym {
	it, ok := iface.Underlying().(*types.Interface)
	if !ok || it.NumMethods() == 0 {
		giveUp("itab for %v as %v", t, iface)
	}
	s := c.lookup("go:itab."+c.typeLinkName(t)+","+c.typeLinkName(iface), obj.ABI0, "")
	if c.rtDone[s] {
		return s
	}
	c.rtDone[s] = true
	sigs := ifaceMethods(it)
	var entries []*obj.LSym
	for _, m := range c.methods(t) {
		if len(sigs) == 0 {
			break
		}
		if m.fn.Name() == sigs[0].Name() && (m.fn.Exported() || m.fn.Pkg() == sigs[0].Pkg()) {
			entries = append(entries, m.isym)
			sigs = sigs[1:]
		}
	}
	if len(sigs) != 0 {
		giveUp("itab: %v does not implement %v", t, iface)
	}
	c.writePtr(s, 0, c.writeType(iface))
	c.writePtr(s, 8, c.writeType(t))
	s.WriteInt(c.ctxt, 16, 4, int64(c.typeHash(t)))
	for i, fn := range entries {
		s.WriteWeakAddr(c.ctxt, 24+int64(i)*8, 8, fn, 0)
	}
	s.Set(obj.AttrDuplicateOK, true)
	s.Set(obj.AttrContentAddressable, true)
	s.Align = 8
	c.defData(s, objabi.SRODATA, 24+int64(len(entries))*8)
	return s
}
