// unit.go: one of fc's packages, handed to the linker in memory (the smallest step of the design in main.go).
//
// NewUnit runs when a package's back end is done, on that package's thread, in place of obj.WriteObjFile.
// It does what the object file's reader would otherwise give the linker, and nothing else:
//
//   - numbering: every symbol the package defines or refers to gets a slot, in the order and with the kinds
//     of reference cmd/internal/obj's NumberSyms gives them (definitions by package index; content-addressed
//     definitions, short and long; definitions by name; references by name; builtins), written into the
//     LSym's PkgIdx and SymIdx, so that a relocation's target is (LSym.PkgIdx, LSym.SymIdx) read in place;
//   - a FuncInfo symbol per function (args, locals, func ID and flags, start line: what fclink reads of it);
//   - the aux list of each definition (Go type, FuncInfo, funcdata, pc-value tables), the one thing laid
//     out anew (9 bytes per entry);
//   - content keys: a short content-addressed symbol (8 bytes or less, no relocations) is keyed by its bytes,
//     as in an object file; a long one by a 128-bit hash of its size, section, bytes and relocations (two
//     maphash sums with fixed per-process seeds: equal content gives equal keys within the process, and fc's
//     units are the only ones that use them) instead of the object file's SHA-256;
//   - the relocations sorted by offset, as the object writer leaves them (only where they are not already).
//
// What it does not do, compared with writing an object: no string table, no serialisation, no file, no
// SHA-256, no reference-name block, no file symbols for the pc-file tables. Data and relocations are fc's
// own memory (fclink never writes them).
package fclinklib

import (
	"bytes"
	"cmd/internal/goobj"
	"cmd/internal/obj"
	"cmd/internal/objabi"
	"encoding/binary"
	"hash/maphash"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// A Unit is one package's symbols as fclink reads them (source.go): slots in object-file order.
type Unit struct {
	syms                          []*obj.LSym // per slot
	info                          []symv      // per slot
	ndef, nh64, nh, nnonpkg, nref uint32
	h64                           []uint64    // per short content-addressed definition: its key
	h                             [][2]uint64 // per long content-addressed definition: its key
	auxOff                        []uint32    // per definition, and one more: its aux entries in auxs
	auxs                          []goobj.Aux
	files                         []string
	// Per definition, the content and the relocations, laid out flat with each target as a SymRef: read in
	// place through the LSyms, every relocation would be a cache miss on its target's LSym in fc's heap
	// (measured: the link's write phase took 1.4x the CPU of the same link over object files).
	names  []string // per slot
	data   [][]byte
	relOff []uint32 // per definition, and one more: its relocations in rel
	rel    []reloc
}

var unitSeeds = [2]maphash.Seed{maphash.MakeSeed(), maphash.MakeSeed()}

// NewUnit numbers ctxt's symbols and returns the package's unit. ctxt must not be written to afterwards.
func NewUnit(ctxt *obj.Link) *Unit {
	u := &Unit{}
	var defs, h64, hd, nonpkg []*obj.LSym
	def := func(s *obj.LSym) {
		if s.PkgIdx != goobj.PkgIdxInvalid {
			return
		}
		switch {
		case s.ContentAddressable():
			if s.Size <= 8 && len(s.R) == 0 && contentHashSection(s) == 0 {
				s.PkgIdx, s.SymIdx = goobj.PkgIdxHashed64, int32(len(h64))
				h64 = append(h64, s)
			} else {
				s.PkgIdx, s.SymIdx = goobj.PkgIdxHashed, int32(len(hd))
				hd = append(hd, s)
			}
		case s.Pkg == "_" || s.DuplicateOK():
			s.PkgIdx, s.SymIdx = goobj.PkgIdxNone, int32(len(nonpkg))
			nonpkg = append(nonpkg, s)
		default:
			s.PkgIdx, s.SymIdx = goobj.PkgIdxSelf, int32(len(defs))
			defs = append(defs, s)
		}
	}
	nfunc := 0
	for _, list := range [][]*obj.LSym{ctxt.Text, ctxt.Data} {
		for _, s := range list {
			def(s)
			if !s.Type.IsText() {
				continue
			}
			fn := s.Func()
			if fn == nil {
				continue
			}
			nfunc++
			pc := &fn.Pcln
			for _, p := range []*obj.LSym{pc.Pcsp, pc.Pcfile, pc.Pcline, pc.Pcinline} {
				if p != nil {
					def(p)
				}
			}
			for _, p := range pc.Pcdata {
				if p != nil {
					def(p)
				}
			}
		}
	}
	// FuncInfo symbols, after the other definitions by index (genFuncInfoSyms)
	finfo := make([]obj.LSym, nfunc)
	var fb bytes.Buffer
	k := 0
	for _, s := range ctxt.Text {
		fn := s.Func()
		if fn == nil {
			continue
		}
		start := fb.Len()
		fi := goobj.FuncInfo{Args: uint32(fn.Args), Locals: uint32(fn.Locals), FuncID: fn.FuncID, FuncFlag: fn.FuncFlag, StartLine: fn.StartLine}
		fi.Write(&fb)
		is := &finfo[k]
		is.Type, is.Size = objabi.SDATA, int64(fb.Len()-start)
		is.PkgIdx, is.SymIdx = goobj.PkgIdxSelf, int32(len(defs))
		fn.FuncInfoSym = is
		defs = append(defs, is)
		k++
	}
	all := fb.Bytes()
	off := 0
	for i := range finfo {
		n := int(finfo[i].Size)
		finfo[i].P = all[off : off+n : off+n]
		off += n
	}

	// references: by name, or builtins by index; content-addressed symbols nothing lists (obj's constant
	// symbols, $f64.* and the like) are definitions, sorted by name after the others, as NumberSyms has them
	var refs, consts []*obj.LSym
	ref := func(rs *obj.LSym) {
		if rs == nil || rs.PkgIdx != goobj.PkgIdxInvalid {
			return
		}
		if i := goobj.BuiltinIdx(rs.Name, int(rs.ABI())); i != -1 && !rs.IsLinkname() {
			rs.PkgIdx, rs.SymIdx = goobj.PkgIdxBuiltin, int32(i)
			return
		}
		if rs.ContentAddressable() {
			rs.PkgIdx, rs.SymIdx = goobj.PkgIdxHashed64, -1 // (numbered below)
			consts = append(consts, rs)
			return
		}
		rs.PkgIdx, rs.SymIdx = goobj.PkgIdxNone, int32(len(nonpkg)+len(refs))
		refs = append(refs, rs)
	}
	refsOf := func(s *obj.LSym) {
		for i := range s.R {
			ref(s.R[i].Sym)
		}
		ref(s.Gotype)
		if fn := s.Func(); fn != nil && s.Type.IsText() {
			for _, d := range fn.Pcln.Funcdata {
				ref(d)
			}
		}
	}
	for _, list := range [][]*obj.LSym{ctxt.Text, ctxt.Data} {
		for _, s := range list {
			refsOf(s)
		}
	}
	for done := 0; done < len(consts); {
		batch := consts[done:]
		sort.Slice(batch, func(i, j int) bool { return batch[i].Name < batch[j].Name })
		done = len(consts)
		for _, s := range batch {
			s.PkgIdx = goobj.PkgIdxInvalid
			def(s)
		}
		for _, s := range batch {
			refsOf(s)
		}
	}

	u.ndef, u.nh64, u.nh, u.nnonpkg, u.nref = uint32(len(defs)), uint32(len(h64)), uint32(len(hd)), uint32(len(nonpkg)), uint32(len(refs))
	n := len(defs) + len(h64) + len(hd) + len(nonpkg) + len(refs)
	u.syms = make([]*obj.LSym, 0, n)
	for _, l := range [][]*obj.LSym{defs, h64, hd, nonpkg, refs} {
		u.syms = append(u.syms, l...)
	}
	pkgPrefix := ctxt.Pkgpath
	u.info = make([]symv, n)
	u.names = make([]string, n)
	nalldef := n - len(refs)
	u.auxOff = make([]uint32, nalldef+1)
	u.relOff = make([]uint32, nalldef+1)
	u.data = make([][]byte, nalldef)
	nrel := 0
	for _, s := range u.syms[:nalldef] {
		nrel += len(s.R)
	}
	u.rel = make([]reloc, 0, nrel)
	for j, s := range u.syms {
		u.info[j] = symInfo(s, pkgPrefix)
		u.names[j] = s.Name
		if j >= nalldef {
			continue
		}
		if len(s.R) > 1 && !slices.IsSortedFunc(s.R, relocByOff) {
			slices.SortStableFunc(s.R, relocByOff)
		}
		u.data[j] = s.P
		u.auxs = appendAux(u.auxs, s)
		u.auxOff[j+1] = uint32(len(u.auxs))
		for k := range s.R {
			r := &s.R[k]
			u.rel = append(u.rel, reloc{r.Add, symRef(r.Sym), r.Off, uint16(r.Type), r.Siz})
		}
		u.relOff[j+1] = uint32(len(u.rel))
	}
	u.h64 = make([]uint64, len(h64))
	for i, s := range h64 {
		var b [8]byte
		copy(b[:], s.P)
		u.h64[i] = binary.LittleEndian.Uint64(b[:])
	}
	u.h = make([][2]uint64, len(hd))
	hs := hasher{u: u, state: make([]uint8, len(hd))}
	for _, s := range hd {
		hs.hash(s)
	}
	for _, f := range ctxt.PosTable.FileTable() {
		u.files = append(u.files, filepath.ToSlash(f))
	}
	return u
}

func relocByOff(a, b obj.Reloc) int { return int(a.Off) - int(b.Off) }

// symInfo: what an object's symbol record says of s (obj's writer.Sym), as far as fclink reads it.
func symInfo(s *obj.LSym, pkgPrefix string) symv {
	abi := uint16(s.ABI())
	if s.Static() {
		abi = goobj.SymABIstatic
	}
	var flag, flag2 uint8
	if s.DuplicateOK() {
		flag |= goobj.SymFlagDupok
	}
	if s.Local() {
		flag |= goobj.SymFlagLocal
	}
	if s.MakeTypelink() {
		flag |= goobj.SymFlagTypelink
	}
	if s.Leaf() {
		flag |= goobj.SymFlagLeaf
	}
	if s.NoSplit() {
		flag |= goobj.SymFlagNoSplit
	}
	if s.ReflectMethod() {
		flag |= goobj.SymFlagReflectMethod
	}
	name := s.Name
	if strings.HasPrefix(name, "type:") && len(name) > 5 && name[5] != '.' && s.Type == objabi.SRODATA {
		flag |= goobj.SymFlagGoType
	}
	if s.UsedInIface() {
		flag2 |= goobj.SymFlagUsedInIface
	}
	if strings.HasPrefix(name, "go:itab.") && s.Type == objabi.SRODATA {
		flag2 |= goobj.SymFlagItab
	}
	if strings.HasPrefix(name, pkgPrefix) && strings.HasPrefix(name[len(pkgPrefix):], ".") && strings.HasPrefix(name[len(pkgPrefix)+1:], objabi.GlobalDictPrefix) {
		flag2 |= goobj.SymFlagDict
	}
	if s.IsPkgInit() {
		flag2 |= goobj.SymFlagPkgInit
	}
	if s.IsLinkname() || name == "main.main" {
		flag2 |= goobj.SymFlagLinkname
	}
	if s.ABIWrapper() {
		flag2 |= goobj.SymFlagABIWrapper
	}
	return symv{siz: uint32(s.Size), align: uint32(s.Align), abi: abi, typ: uint8(s.Type), flag: flag, flag2: flag2}
}

func symRef(s *obj.LSym) goobj.SymRef {
	if s == nil {
		return goobj.SymRef{}
	}
	return goobj.SymRef{PkgIdx: uint32(s.PkgIdx), SymIdx: uint32(s.SymIdx)}
}

// appendAux: s's aux entries, in the object writer's order (writer.Aux; fc makes no DWARF symbols).
func appendAux(auxs []goobj.Aux, s *obj.LSym) []goobj.Aux {
	add := func(t uint8, rs *obj.LSym) {
		var a goobj.Aux
		a.SetType(t)
		a.SetSym(symRef(rs))
		auxs = append(auxs, a)
	}
	if s.Gotype != nil {
		add(goobj.AuxGotype, s.Gotype)
	}
	fn := s.Func()
	if fn == nil || !s.Type.IsText() {
		return auxs
	}
	add(goobj.AuxFuncInfo, fn.FuncInfoSym)
	pc := &fn.Pcln
	for _, d := range pc.Funcdata {
		add(goobj.AuxFuncdata, d)
	}
	for _, x := range []struct {
		t uint8
		s *obj.LSym
	}{{goobj.AuxPcsp, pc.Pcsp}, {goobj.AuxPcfile, pc.Pcfile}, {goobj.AuxPcline, pc.Pcline}, {goobj.AuxPcinline, pc.Pcinline}} {
		if x.s != nil && x.s.Size != 0 {
			add(x.t, x.s)
		}
	}
	for _, d := range pc.Pcdata {
		add(goobj.AuxPcdata, d)
	}
	return auxs
}

// contentHashSection is obj's: content-addressed symbols of different sections are never merged.
func contentHashSection(s *obj.LSym) byte {
	if s.Type == objabi.STEXT {
		return 't'
	}
	if s.Type == objabi.STEXTFIPS {
		return 'f'
	}
	if s.IsPcdata() {
		return 'P'
	}
	name := s.Name
	if strings.HasPrefix(name, "gcargs.") ||
		strings.HasPrefix(name, "gclocals.") ||
		strings.HasPrefix(name, "gclocals·") ||
		strings.HasSuffix(name, ".opendefer") ||
		strings.HasSuffix(name, ".arginfo0") ||
		strings.HasSuffix(name, ".arginfo1") ||
		strings.HasSuffix(name, ".argliveinfo") ||
		strings.HasSuffix(name, ".wrapinfo") ||
		strings.HasSuffix(name, ".args_stackmap") ||
		strings.HasSuffix(name, ".stkobj") {
		return 'F'
	}
	if strings.HasPrefix(name, "type:") {
		return 'T'
	}
	return 0
}

// hasher computes the keys of a unit's long content-addressed symbols: obj's contentHash with maphash for
// SHA-256 and with names for package-indexed targets (as SB_EARLY_EXPORT's sbHashByName has it).
type hasher struct {
	u     *Unit
	state []uint8 // per long content-addressed definition: 0 not yet, 1 in progress, 2 done
	buf   []byte
}

func (hs *hasher) hash(s *obj.LSym) [2]uint64 {
	i := s.SymIdx
	switch hs.state[i] {
	case 2:
		return hs.u.h[i]
	case 1:
		return [2]uint64{} // (a cycle: obj assumes there is none)
	}
	hs.state[i] = 1
	for k := range s.R {
		if rs := s.R[k].Sym; rs != nil && rs != s && rs.PkgIdx == goobj.PkgIdxHashed {
			hs.hash(rs)
		}
	}
	b := hs.buf[:0]
	b = binary.LittleEndian.AppendUint64(b, uint64(s.Size))
	b = append(b, contentHashSection(s))
	if s.Type == objabi.STEXT || s.Type == objabi.STEXTFIPS {
		b = binary.AppendUvarint(b, uint64(len(s.Name)))
		b = append(b, s.Name...)
	}
	p := bytes.TrimRight(s.P, "\x00")
	b = binary.AppendUvarint(b, uint64(len(p)))
	b = append(b, p...)
	for k := range s.R {
		r := &s.R[k]
		b = binary.LittleEndian.AppendUint32(b, uint32(r.Off))
		b = append(b, r.Siz)
		b = binary.LittleEndian.AppendUint16(b, uint16(r.Type))
		b = binary.LittleEndian.AppendUint64(b, uint64(r.Add))
		rs := r.Sym
		switch {
		case rs == nil:
			b = append(b, 4)
		case rs == s:
			b = append(b, 5)
		case rs.PkgIdx == goobj.PkgIdxHashed64:
			var t [8]byte
			copy(t[:], rs.P)
			b = append(b, 0)
			b = append(b, t[:]...)
		case rs.PkgIdx == goobj.PkgIdxHashed:
			h := hs.u.h[rs.SymIdx]
			b = append(b, 1)
			b = binary.LittleEndian.AppendUint64(b, h[0])
			b = binary.LittleEndian.AppendUint64(b, h[1])
		case rs.PkgIdx == goobj.PkgIdxBuiltin:
			b = append(b, 3)
			b = binary.LittleEndian.AppendUint32(b, uint32(rs.SymIdx))
		default:
			b = append(b, 2)
			b = binary.AppendUvarint(b, uint64(len(rs.Name)))
			b = append(b, rs.Name...)
			b = binary.LittleEndian.AppendUint16(b, uint16(rs.ABI()))
		}
	}
	h := [2]uint64{maphash.Bytes(unitSeeds[0], b), maphash.Bytes(unitSeeds[1], b)}
	hs.buf = b
	hs.u.h[i] = h
	hs.state[i] = 2
	return h
}
