// write.go: the output. One parallel pass over the units copies every live symbol into the mapped output
// file, applies its relocations, writes its function-table entry (pclntab) and records the pointer words
// of data and bss symbols for the GC bitmaps.
package main

import (
	"cmd/internal/goobj"
	"cmd/internal/objabi"
	"encoding/binary"
	"internal/abi"
	"os"
	"syscall"
)

var (
	out       []byte // the output file, mapped: out[address-imageBase]
	outFile   *os.File
	outSize   int64
	shdrRoom  = int64(4096) // section headers and their names, after the image
	gcDataBit []byte        // one bit per word of [data, edata): is it a pointer?
	gcBssBit  []byte

	deferreturnAddr, unreachableMethodAddr, mapinitnoopAddr uint32
)

var le = binary.LittleEndian

func write() {
	fileEnd := int64(lay.edata - imageBase)
	outSize = fileEnd + shdrRoom
	os.Remove(*flagOut)
	f, err := os.OpenFile(*flagOut, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		fatalf("%v", err)
	}
	if err := f.Truncate(outSize); err != nil {
		fatalf("%v", err)
	}
	out, err = syscall.Mmap(int(f.Fd()), 0, int(outSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		fatalf("mmap %s: %v", *flagOut, err)
	}
	outFile = f
	gcDataBit = make([]byte, (lay.edata-lay.data)/64+8)
	gcBssBit = make([]byte, (lay.ebss-lay.bss)/64+8)
	if g := lookupName("runtime.deferreturn", 1); g != 0 {
		deferreturnAddr = addr[g]
	}
	if g := lookupName("runtime.unreachableMethod", 1); g != 0 {
		unreachableMethodAddr = addr[g]
	}
	if g := lookupName("runtime.mapinitnoop", 1); g != 0 {
		mapinitnoopAddr = addr[g]
	}
	lap("create", "")
	var nrel [64]struct {
		n uint64
		_ [56]byte
	}
	par(len(units), func(w, ui int) { nrel[w].n += writeUnit(units[ui]) })
	var n uint64
	for i := range nrel {
		n += nrel[i].n
	}
	lap("write", "%d relocations applied", n)
}

func sectBase(a uint32) uint32 {
	switch {
	case a < lay.rodata:
		return lay.text
	case a < lay.pclntab:
		return lay.rodata
	case a < lay.types:
		return lay.pclntab
	case a < lay.noptrdata:
		return lay.types
	case a < lay.data:
		return lay.noptrdata
	case a < lay.bss:
		return lay.data
	case a < lay.noptrbss:
		return lay.bss
	}
	return lay.noptrbss
}

func writeUnit(u *unit) (nrel uint64) {
	o, r := u.o, u.o.r
	fidx := u.funcBase
	foff := lay.ftabStructs + u.ftabBase
	inl := u.inlBase
	names := out[lay.funcnametab-imageBase:]
	pctab := out[lay.pctab-imageBase:]
	for j := u.lo; j < min(u.hi, o.nalldef); j++ {
		gid := o.base + j
		st := state[gid]
		if st&(stLive|stPC|stNameNeeded) == 0 || canon[gid] != gid {
			continue
		}
		s := r.Sym(j)
		if st&stPC != 0 {
			if a := addr[gid]; a != 0 {
				copy(pctab[a:], r.Data(j))
			}
		}
		b := int(st >> stBucketShift & 31)
		text := isTextBucket(b)
		if text || st&stNameNeeded != 0 {
			copy(names[aux[gid]:], s.Name(r))
		}
		if b == bNone {
			continue
		}
		a := addr[gid]
		siz := s.Siz()
		switch b {
		case bBSS:
			gcSym(gcBssBit, o, j, a-lay.bss)
			continue
		case bNoptrBSS, bCov:
			continue
		case bData, bDataFIPS:
			gcSym(gcDataBit, o, j, a-lay.data)
		}
		d := r.Data(j)
		if uint32(len(d)) > siz {
			errorf("%s: initialize bounds (%d < %d)", s.Name(r), siz, len(d))
			continue
		}
		P := out[a-imageBase : a-imageBase+siz : a-imageBase+siz]
		copy(P, d)
		if text {
			pad := out[a-imageBase+siz : a-imageBase+occupied(siz, b, st>>stClassShft&15)]
			for i := range pad {
				pad[i] = 0xCC // INT3
			}
		}
		relocs := r.Relocs(j)
		nrel += uint64(len(relocs))
		deferreturn := uint32(0)
		for i := range relocs {
			rl := &relocs[i]
			rsiz := uint32(rl.Siz())
			if rsiz == 0 {
				continue // a marker for the dead-code pass
			}
			t := objabi.RelocType(rl.Type())
			weak := t&objabi.R_WEAK != 0
			t &^= objabi.R_WEAK
			off := uint32(rl.Off())
			if off+rsiz > siz {
				errorf("%s: invalid relocation at %d+%d, size %d", s.Name(r), off, rsiz, siz)
				continue
			}
			tg := o.ref(rl.Sym())
			ta := addr[tg]
			var v int64
			switch t {
			case objabi.R_ADDR:
				if ta == 0 && tg != 0 {
					if !weak {
						undefined(o, s, tg)
						continue
					}
					ta = unreachableMethodAddr // a method nothing can call: throws if called
				}
				v = int64(ta) + rl.Add()
				if rsiz == 4 && int32(v) < 0 {
					errorf("%s: non-pc-relative relocation address for %s is too big: %#x", s.Name(r), symName(canon[tg]), v)
				}
			case objabi.R_METHODOFF, objabi.R_ADDROFF:
				if ta == 0 {
					if t == objabi.R_METHODOFF {
						le.PutUint32(P[off:], ^uint32(0)) // the runtime knows -1 is no method
						continue
					}
					if !weak {
						undefined(o, s, tg)
					}
					continue
				}
				v = int64(ta-sectBase(ta)) + rl.Add()
			case objabi.R_CALL, objabi.R_PCREL:
				if ta == 0 && tg != 0 {
					if !(weak && t == objabi.R_CALL) {
						undefined(o, s, tg)
						continue
					}
					ta = mapinitnoopAddr // the initializer of a map variable that is not live
				}
				if t == objabi.R_CALL && ta == deferreturnAddr && deferreturn == 0 && text {
					deferreturn = off - 1 // the start of the call instruction
				}
				v = int64(ta) + rl.Add() - int64(a+off+rsiz)
				if tg == 0 {
					v = rl.Add() - int64(a+off+rsiz)
				}
				if v != int64(int32(v)) {
					errorf("%s: pc-relative relocation to %s is too far", s.Name(r), symName(canon[tg]))
				}
			case objabi.R_TLS_LE:
				v = -8 + rl.Add() // ELF: g is at -8(FS)
			case objabi.R_CONST:
				v = rl.Add()
			case objabi.R_SIZE:
				if c := canon[tg]; c != 0 && c <= nslots {
					o2, j2 := objOf(c)
					v = int64(o2.r.Sym(j2).Siz())
				}
				v += rl.Add()
			default:
				errorf("%s: unsupported relocation type %v to %s", s.Name(r), t, symName(canon[tg]))
				continue
			}
			switch rsiz {
			case 8:
				le.PutUint64(P[off:], uint64(v))
			case 4:
				le.PutUint32(P[off:], uint32(v))
			case 2:
				le.PutUint16(P[off:], uint16(v))
			case 1:
				P[off] = byte(v)
			default:
				errorf("%s: bad relocation size %d", s.Name(r), rsiz)
			}
		}
		if !text {
			continue
		}
		// ---- the function's entry in the pc -> function table, and its _func
		fi, npc, nfd, ninl := funcShape(o, j)
		ft := out[lay.functab-imageBase:]
		le.PutUint32(ft[fidx*8:], a-lay.text)
		le.PutUint32(ft[fidx*8+4:], foff)
		f := ft[foff:]
		le.PutUint32(f[0:], a-lay.text)
		le.PutUint32(f[4:], aux[gid])
		le.PutUint32(f[12:], deferreturn)
		le.PutUint32(f[28:], npc)
		le.PutUint32(f[32:], o.cuOff)
		f[43] = byte(nfd)
		if fi != nil {
			copy(f[8:12], fi[0:4])      // args
			copy(f[36:40], fi[12:16])   // startLine
			f[40], f[41] = fi[8], fi[9] // funcID, flag
			pcd, fdd := f[funcSize:], f[funcSize+4*npc:]
			for k := uint32(0); k < nfd; k++ {
				le.PutUint32(fdd[4*k:], ^uint32(0))
			}
			auxs := r.Auxs(j)
			kp, kf, pcinline := 0, 0, uint32(0)
			for i := range auxs {
				x := &auxs[i]
				switch x.Type() {
				case goobj.AuxPcsp:
					le.PutUint32(f[16:], addr[o.ref(x.Sym())])
				case goobj.AuxPcfile:
					le.PutUint32(f[20:], addr[o.ref(x.Sym())])
				case goobj.AuxPcline:
					le.PutUint32(f[24:], addr[o.ref(x.Sym())])
				case goobj.AuxPcinline:
					pcinline = addr[o.ref(x.Sym())]
				case goobj.AuxPcdata:
					le.PutUint32(pcd[4*kp:], addr[o.ref(x.Sym())])
					kp++
				case goobj.AuxFuncdata:
					// (a symbol nothing defines is an assembly function's missing args_stackmap: no value)
					if ta := addr[o.ref(x.Sym())]; ta != 0 {
						le.PutUint32(fdd[4*kf:], ta-lay.gofunc)
					}
					kf++
				}
			}
			if ninl > 0 {
				le.PutUint32(pcd[4*abi.PCDATA_InlTreeIndex:], pcinline)
				l := (*goobj.FuncInfo)(nil).ReadFuncInfoLengths(fi)
				t := out[inl-imageBase:]
				for k := uint32(0); k < ninl; k++ {
					node := (*goobj.FuncInfo)(nil).ReadInlTree(fi, l.InlTreeOff, k)
					callee := canon[o.ref(node.Func)]
					e := t[16*k:]
					if cfi := funcInfoOf(callee); cfi != nil {
						e[0] = cfi[8]              // funcID
						copy(e[12:16], cfi[12:16]) // startLine
					}
					le.PutUint32(e[4:], aux[callee])
					le.PutUint32(e[8:], uint32(node.ParentPC))
				}
				le.PutUint32(fdd[4*abi.FUNCDATA_InlTree:], inl-lay.gofunc)
				inl += 16 * ninl
			}
		}
		fidx++
		foff += rnd(funcSize+4*npc+4*nfd, 8)
	}
	return nrel
}

func funcInfoOf(c uint32) []byte {
	if c == 0 || c > nslots {
		return nil
	}
	o, j := objOf(c)
	auxs := o.r.Auxs(j)
	for i := range auxs {
		if auxs[i].Type() == goobj.AuxFuncInfo {
			return dataOf(canon[o.ref(auxs[i].Sym())])
		}
	}
	return nil
}

func undefined(o *object, s *goobj.Sym, tg uint32) {
	c := canon[tg]
	what := "undefined"
	if c <= nslots {
		what = "not reachable (fclink's dead-code pass and the code disagree)"
	}
	errorf("%s: relocation target %s %s", s.Name(o.r), symName(c), what)
}

// gcSym sets, in bitmap bm, the bits of the pointer words of data symbol j of o, which is at byte offset
// off of the bitmap's section. The symbol's Go type says where its pointers are (ld.GCProg.AddSym).
func gcSym(bm []byte, o *object, j uint32, off uint32) {
	auxs := o.r.Auxs(j)
	for i := range auxs {
		if auxs[i].Type() == goobj.AuxGotype {
			if t := canon[o.ref(auxs[i].Sym())]; t != 0 && t <= nslots {
				typePtrs(t, func(w int64) {
					w += int64(off / 8)
					bm[w>>3] |= 1 << (w & 7)
				})
			}
			return
		}
	}
}

// typePtrs calls emit with the index of every pointer word of a value of type t.
func typePtrs(t uint32, emit func(w int64)) {
	o, j := objOf(t)
	p := o.r.Data(j)
	if len(p) < 48 {
		return
	}
	ptrdata := int64(le.Uint64(p[8:]))
	if ptrdata == 0 {
		return
	}
	relocs := o.r.Relocs(j)
	target := func(off int32) uint32 {
		if rl := relocAt(relocs, off); rl != nil {
			if c := canon[o.ref(rl.Sym())]; c <= nslots {
				return c
			}
		}
		return 0
	}
	switch typeKind(p) {
	default:
		if p[abi.TFlagOff(8)]&byte(abi.TFlagGCMaskOnDemand) != 0 {
			errorf("%s: GC mask not available", o.r.Sym(j).Name(o.r))
			return
		}
		mask := dataOf(target(32))
		for i := int64(0); i < ptrdata/8 && i/8 < int64(len(mask)); i++ {
			if mask[i/8]>>(i%8)&1 != 0 {
				emit(i)
			}
		}
	case abi.Array:
		elem := target(48)
		n := int64(le.Uint64(p[48+16:]))
		ed := dataOf(elem)
		if elem == 0 || len(ed) < 48 {
			return
		}
		esize := int64(le.Uint64(ed)) / 8
		var ws []int64
		typePtrs(elem, func(w int64) { ws = append(ws, w) })
		for k := int64(0); k < n; k++ {
			for _, w := range ws {
				emit(k*esize + w)
			}
		}
	case abi.Struct:
		n := int(le.Uint64(p[48+16:]))
		fo := abi.RTypeSize(abi.Struct, 8)
		if p[abi.TFlagOff(8)]&byte(abi.TFlagUncommon) != 0 {
			fo += int(abi.UncommonSize())
		}
		for i := 0; i < n; i++ {
			f := fo + i*abi.StructFieldSize(8)
			ft := target(int32(f + 8))
			if ft == 0 {
				continue
			}
			foff := int64(le.Uint64(p[f+16:])) / 8
			typePtrs(ft, func(w int64) { emit(foff + w) })
		}
	}
}
