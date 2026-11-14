// source.go: the one way every phase reads an object's symbols, whichever source the object has: a Go object
// file (goobj.Reader over the mapped file) or one of fc's units in memory (unit.go). Both number their slots
// the same way and make references the same way (goobj.SymRef: package-indexed, content-addressed, by name,
// builtin), so the phases see no difference; the accessors are the only place that knows which it is.
package fclinklib

import (
	"cmd/internal/goobj"
)

// symv: a symbol's record (kind, flags, size, alignment), what goobj.Sym holds besides the name.
type symv struct {
	siz, align  uint32
	abi         uint16
	typ         uint8
	flag, flag2 uint8
}

func (s symv) Siz() uint32         { return s.siz }
func (s symv) Align() uint32       { return s.align }
func (s symv) ABI() uint16         { return s.abi }
func (s symv) Type() uint8         { return s.typ }
func (s symv) Dupok() bool         { return s.flag&goobj.SymFlagDupok != 0 }
func (s symv) Typelink() bool      { return s.flag&goobj.SymFlagTypelink != 0 }
func (s symv) ReflectMethod() bool { return s.flag&goobj.SymFlagReflectMethod != 0 }
func (s symv) IsGoType() bool      { return s.flag&goobj.SymFlagGoType != 0 }
func (s symv) UsedInIface() bool   { return s.flag2&goobj.SymFlagUsedInIface != 0 }
func (s symv) IsItab() bool        { return s.flag2&goobj.SymFlagItab != 0 }

// sym: the record of slot j.
func (o *object) sym(j uint32) symv {
	if m := o.m; m != nil {
		return m.info[j]
	}
	s := o.r.Sym(j)
	return symv{s.Siz(), s.Align(), s.ABI(), s.Type(), s.Flag(), s.Flag2()}
}

func (o *object) name(j uint32) string {
	if m := o.m; m != nil {
		return m.names[j]
	}
	return o.r.Sym(j).Name(o.r)
}

func (o *object) nameLen(j uint32) int {
	if m := o.m; m != nil {
		return len(m.names[j])
	}
	return o.r.Sym(j).NameLen(o.r)
}

// data: the content of definition j (it may be shorter than the symbol: the rest is zeros).
func (o *object) data(j uint32) []byte {
	if m := o.m; m != nil {
		return m.data[j]
	}
	return o.r.Data(j)
}

func (o *object) dataSize(j uint32) int {
	if m := o.m; m != nil {
		return len(m.data[j])
	}
	return o.r.DataSize(j)
}

// relocs: the relocations of definition j, in offset order.
func (o *object) relocs(j uint32) relocs {
	if m := o.m; m != nil {
		return relocs{m: m.rel[m.relOff[j]:m.relOff[j+1]]}
	}
	return relocs{g: o.r.Relocs(j)}
}

// auxs: the aux entries of definition j (Go type, FuncInfo, funcdata, pc-value tables).
func (o *object) auxs(j uint32) []goobj.Aux {
	if m := o.m; m != nil {
		return m.auxs[m.auxOff[j]:m.auxOff[j+1]]
	}
	return o.r.Auxs(j)
}

// the file table the pc-file tables index
func (o *object) nfile() int {
	if m := o.m; m != nil {
		return len(m.files)
	}
	return o.r.NFile()
}

func (o *object) fileName(i int) string {
	if m := o.m; m != nil {
		return m.files[i]
	}
	return o.r.File(i)
}

// the packages references by package index name (index 0 is invalid; fc's units refer to other packages
// by name only)
func (o *object) npkg() int {
	if o.m != nil {
		return 1
	}
	return o.r.NPkg()
}

func (o *object) pkgName(i int) string {
	if o.m != nil {
		return ""
	}
	return o.r.Pkg(i)
}

// hash64, hash: the key of the i-th short and long content-addressed definition
func (o *object) hash64(i uint32) uint64 {
	if m := o.m; m != nil {
		return m.h64[i]
	}
	return o.r.Hash64(i)
}

func (o *object) hash(i uint32) (k1, k2 uint64) {
	if m := o.m; m != nil {
		return m.h[i][0], m.h[i][1]
	}
	h := o.r.Hash(i)
	return le.Uint64(h[:8]), le.Uint64(h[8:])
}

// flags of references by package index (none in fc's units)
func (o *object) nrefFlags() int {
	if o.m != nil {
		return 0
	}
	return o.r.NRefFlags()
}

// A reloc is one relocation, read from either source.
type reloc struct {
	add int64
	sym goobj.SymRef
	off int32
	typ uint16
	siz uint8
}

func (r reloc) Off() int32        { return r.off }
func (r reloc) Siz() uint8        { return r.siz }
func (r reloc) Type() uint16      { return r.typ }
func (r reloc) Add() int64        { return r.add }
func (r reloc) Sym() goobj.SymRef { return r.sym }

// relocs: a symbol's relocations, in an object file (g) or in a unit (m, laid out by NewUnit).
type relocs struct {
	g []goobj.Reloc
	m []reloc
}

func (rs relocs) Len() int { return len(rs.g) + len(rs.m) }

func (rs relocs) At(i int) reloc {
	if rs.m != nil {
		return rs.m[i]
	}
	r := &rs.g[i]
	return reloc{r.Add(), r.Sym(), r.Off(), r.Type(), r.Siz()}
}
