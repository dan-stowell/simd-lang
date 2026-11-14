// layout.go: where everything goes. Each live definition belongs to a bucket (a part of a section) and an
// alignment class. One parallel pass sums, per unit, the bytes each (bucket, class) needs; prefix sums over
// the units in address order give every unit's block of every bucket its place (inside a block the classes
// go from the largest alignment down, so nothing is wasted on padding); a second parallel pass hands out
// the addresses; a third gives every other slot (duplicates, references) the address of its definition, so
// that applying a relocation is one read of a table indexed by the slot the relocation names.
//
// The image, in address order (file offset = address - imageBase everywhere):
//
//	ELF header, program headers, build id note
//	text            functions; then the FIPS functions
//	rodata          read-only data; FIPS read-only data; strings the linker makes; the GC programs of data and bss
//	pclntab         header, function names, cutab, file names, pc-value tables, functab, funcdata, findfunctab
//	types           the types reflection looks up by string, sorted by it; other type data; itabs
//	(page boundary)
//	noptrdata       build info, moduledata, init task lists; pointer-free data; FIPS
//	data            data with pointers; FIPS
//	bss, noptrbss   (not in the file)
package fclinklib

import (
	"cmd/internal/goobj"
	"cmd/internal/objabi"
	"encoding/binary"
	"internal/abi"
	"math/bits"
	"slices"
	"strings"
	"sync"
	"unsafe"
)

const (
	bNone = iota
	bText
	bTextFIPS
	bRodata
	bRodataFIPS
	bGofunc
	bTypelink // laid out one by one in sorted order, not by unit
	bType
	bItab
	bNoptrdata
	bNoptrdataFIPS
	bData
	bDataFIPS
	bBSS
	bNoptrBSS
	bCov
	nBuckets

	nClasses    = 13 // alignment 1<<0 .. 1<<12
	stClassShft = 13
	imageBase   = 0x400000
	headerSize  = 0x1000
	funcAlign   = 32
	maxAlign    = 32
	funcSize    = 11 * 4 // runtime._func
	minFunc     = abi.MINFUNC
)

// the layout, as addresses
var lay struct {
	text, etext                  uint32
	textFIPS, etextFIPS          uint32
	rodata, erodata              uint32
	rodataFIPS, erodataFIPS      uint32
	gcdata, gcbss                uint32 // room for the GC programs
	gcdataMax, gcbssMax          uint32
	pclntab, epclntab            uint32
	pcheader, funcnametab, cutab uint32
	filetab, pctab, functab      uint32
	ftabStructs                  uint32 // offset of the first _func in functab
	functabSize                  uint32
	gofunc, findfunctab          uint32
	findfuncSize                 uint32
	types, etypes                uint32
	typedesclen                  uint32
	itabs, eitabs                uint32
	noptrdata, enoptrdata        uint32
	noptrdataFIPS, enoptrdataFIP uint32
	data, edata                  uint32
	dataFIPS, edataFIPS          uint32
	bss, ebss                    uint32
	noptrbss, enoptrbss          uint32
	covctrs, ecovctrs            uint32
	end                          uint32
	nfunc, nfiles, cutabLen      uint32
	nameSize, pctabSize          uint32
	fileNames                    []string // expanded, in filetab order
	fileOffs                     []uint32
	cutabData                    []uint32
}

func rnd(v, a uint32) uint32 { return (v + a - 1) &^ (a - 1) }

func symAlign(s symv) uint32 {
	if a := s.Align(); a != 0 {
		return a
	}
	a, siz := uint32(maxAlign), s.Siz()
	for a > siz && a > 1 {
		a >>= 1
	}
	return a
}

// funcShape reads what a function's pclntab entry needs to know: its FuncInfo, and how many pcdata and
// funcdata slots it has (an inlined-into function has at least the inline-tree ones).
func funcShape(o *object, j uint32) (fi []byte, npc, nfd, ninl uint32) {
	auxs := o.auxs(j)
	for i := range auxs {
		switch auxs[i].Type() {
		case goobj.AuxFuncInfo:
			fi = dataOf(canon[o.ref(auxs[i].Sym())])
		case goobj.AuxPcdata:
			npc++
		case goobj.AuxFuncdata:
			nfd++
		}
	}
	if fi == nil {
		return nil, 0, 0, 0
	}
	ninl = (*goobj.FuncInfo)(nil).ReadFuncInfoLengths(fi).NumInlTree
	if ninl > 0 {
		npc = max(npc, abi.PCDATA_InlTreeIndex+1)
		nfd = max(nfd, abi.FUNCDATA_InlTree+1)
	}
	return
}

func bucketOf(o *object, j uint32, s symv, st uint32) int {
	switch objabi.SymKind(s.Type()) {
	case objabi.STEXT:
		return bText
	case objabi.STEXTFIPS:
		return bTextFIPS
	case objabi.SRODATA:
		switch {
		case st&stFuncdata != 0:
			return bGofunc
		case s.IsItab():
			return bItab
		case strings.HasPrefix(o.name(j), "type:"):
			if s.Typelink() {
				return bTypelink
			}
			return bType
		}
		return bRodata
	case objabi.SRODATAFIPS:
		return bRodataFIPS
	case objabi.SNOPTRDATA, objabi.SCOVERAGE_AUXVAR:
		return bNoptrdata
	case objabi.SNOPTRDATAFIPS:
		return bNoptrdataFIPS
	case objabi.SDATA:
		return bData
	case objabi.SDATAFIPS:
		return bDataFIPS
	case objabi.SBSS:
		return bBSS
	case objabi.SNOPTRBSS:
		return bNoptrBSS
	case objabi.SCOVERAGE_COUNTER:
		return bCov
	}
	return bNone
}

func isTextBucket(b int) bool { return b == bText || b == bTextFIPS }

// occupied: the bytes a symbol takes in its class's run (its size rounded up to the class's alignment).
func occupied(siz uint32, b int, cl uint32) uint32 {
	if isTextBucket(b) && siz < minFunc {
		siz = minFunc
	}
	return rnd(siz, 1<<cl)
}

type typelink struct {
	gid uint32
	str string
}

// typeString returns a type descriptor's string (what reflect's lookup by string compares).
func typeString(gid uint32) string {
	o, j := objOf(gid)
	p := o.data(j)
	rl, ok := relocAt(o.relocs(j), 4*8+8)
	if !ok {
		return ""
	}
	d := dataOf(canon[o.ref(rl.Sym())])
	if len(d) < 2 {
		return ""
	}
	n, k := binary.Uvarint(d[1:])
	str := unsafe.String(&d[1+k], int(n))
	if p[abi.TFlagOff(8)]&byte(abi.TFlagExtraStar) != 0 {
		return str[1:]
	}
	return str
}

func cmpTypelink(a, b typelink) int {
	if c := strings.Compare(a.str, b.str); c != 0 {
		return c
	}
	return int(a.gid) - int(b.gid)
}

// sortTypelinks: a merge sort whose halves are sorted on separate threads.
func sortTypelinks(x, tmp []typelink, depth int) {
	if depth == 0 || len(x) < 2048 {
		slices.SortFunc(x, cmpTypelink)
		return
	}
	m := len(x) / 2
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { sortTypelinks(x[:m], tmp[:m], depth-1); wg.Done() }()
	sortTypelinks(x[m:], tmp[m:], depth-1)
	wg.Wait()
	copy(tmp, x)
	a, b, k := tmp[:m], tmp[m:], 0
	for len(a) > 0 && len(b) > 0 {
		if cmpTypelink(a[0], b[0]) <= 0 {
			x[k], a = a[0], a[1:]
		} else {
			x[k], b = b[0], b[1:]
		}
		k++
	}
	k += copy(x[k:], a)
	copy(x[k:], b)
}

func layout() {
	makeSpecials()

	// ---- sizes
	par(len(units), func(_, ui int) {
		u := units[ui]
		o := u.o
		for j := u.lo; j < min(u.hi, o.nalldef); j++ {
			gid := o.base + j
			st := state[gid]
			if st&(stLive|stPC|stNameNeeded) == 0 || canon[gid] != gid {
				continue
			}
			s := o.sym(j)
			if st&stPC != 0 {
				u.pcSize += uint32(o.dataSize(j))
			}
			b := bNone
			if st&stLive != 0 && st&stOverride == 0 {
				b = bucketOf(o, j, s, st)
			}
			if isTextBucket(b) {
				_, npc, nfd, ninl := funcShape(o, j)
				u.nfunc[b-bText]++
				u.ftabSize += rnd(funcSize+4*npc+4*nfd, 8)
				u.inlSize += 16 * ninl
				u.nameSize += uint32(o.nameLen(j)) + 1
			} else if st&stNameNeeded != 0 {
				u.nameSize += uint32(o.nameLen(j)) + 1
			}
			if b == bNone {
				continue
			}
			if b == bTypelink {
				u.typelinks = append(u.typelinks, gid)
				state[gid] = st | bTypelink<<stBucketShift
				continue
			}
			a := symAlign(s)
			if isTextBucket(b) {
				a = max(a, funcAlign)
			}
			if b == bItab {
				a = 8 // the runtime walks the itabs: no gaps
			}
			cl := uint32(bits.TrailingZeros32(a))
			if a&(a-1) != 0 || cl >= nClasses {
				errorf("%s: alignment %d", o.name(j), a)
				continue
			}
			state[gid] = st | uint32(b)<<stBucketShift | cl<<stClassShft
			u.csize[b][cl] += occupied(s.Siz(), b, cl)
		}
		// The function table is written in symbol order and must be in address order: a unit's functions
		// are one run. When some want more than the usual alignment (assembly with a 64-byte alignment
		// beside functions without), all of the unit's functions take the largest.
		for b := bText; b <= bTextFIPS; b++ {
			top, n := uint32(0), 0
			for cl := uint32(0); cl < nClasses; cl++ {
				if u.csize[b][cl] != 0 {
					top, n = cl, n+1
				}
			}
			if n < 2 {
				continue
			}
			u.csize[b] = [nClasses]uint32{}
			for j := u.lo; j < min(u.hi, o.nalldef); j++ {
				gid := o.base + j
				st := state[gid]
				if int(st>>stBucketShift&31) != b || st&stLive == 0 || canon[gid] != gid {
					continue
				}
				state[gid] = st&^(15<<stClassShft) | top<<stClassShft
				u.csize[b][top] += occupied(o.sym(j).Siz(), b, top)
			}
		}
	})

	// ---- the typelink types, sorted by type string (reflect binary-searches them)
	var tls []typelink
	for _, u := range units {
		for _, g := range u.typelinks {
			tls = append(tls, typelink{gid: g})
		}
	}
	const tgrain = 512
	par((len(tls)+tgrain-1)/tgrain, func(_, i int) {
		for k := i * tgrain; k < min((i+1)*tgrain, len(tls)); k++ {
			tls[k].str = typeString(tls[k].gid)
		}
	})
	depth := 0
	for 1<<depth < nthreads {
		depth++
	}
	sortTypelinks(tls, make([]typelink, len(tls)), depth)

	// ---- pclntab tables that are per object: the file table
	for _, u := range units {
		u.o.nfunc += u.nfunc[0] + u.nfunc[1]
	}
	fileOff := map[string]uint32{}
	var fileSize uint32
	for _, o := range objs {
		if o.nfunc == 0 {
			continue
		}
		o.cuOff = uint32(len(lay.cutabData))
		for f, n := 0, o.nfile(); f < n; f++ {
			name := o.fileName(f)
			off, ok := fileOff[name]
			if !ok {
				x := expandFile(name)
				off = fileSize
				fileOff[name] = off
				lay.fileNames = append(lay.fileNames, x)
				lay.fileOffs = append(lay.fileOffs, off)
				fileSize += uint32(len(x)) + 1
			}
			lay.cutabData = append(lay.cutabData, off)
		}
	}
	lay.nfiles = uint32(len(lay.fileNames))

	// ---- addresses of the units' blocks, in address order
	va := uint32(imageBase + headerSize)
	bucket := func(b int, minAlign uint32) (start, end uint32) {
		va = rnd(va, minAlign)
		start = va
		for _, u := range units {
			cs := &u.csize[b]
			top := -1
			for cl := nClasses - 1; cl >= 0; cl-- {
				if cs[cl] != 0 {
					top = cl
					break
				}
			}
			if top >= 0 {
				va = rnd(va, max(minAlign, 1<<top))
			}
			for cl := nClasses - 1; cl >= 0; cl-- { // (empty classes too: zero-sized symbols need an address)
				u.cbase[b][cl] = va
				va += cs[cl]
			}
		}
		return start, va
	}
	place := func(list []*special) {
		for _, sp := range list {
			va = rnd(va, sp.align)
			addr[sp.gid] = va
			va += sp.size
		}
	}
	lay.text, _ = bucket(bText, funcAlign)
	lay.textFIPS, lay.etextFIPS = bucket(bTextFIPS, funcAlign)
	lay.etext = va

	va = rnd(va+1, 4096)
	lay.rodata, _ = bucket(bRodata, 8)
	lay.rodataFIPS, lay.erodataFIPS = bucket(bRodataFIPS, 32)
	place(roSpecials)
	var dataMax, bssMax uint32
	for _, u := range units {
		for cl := 0; cl < nClasses; cl++ {
			dataMax += u.csize[bData][cl] + u.csize[bDataFIPS][cl]
			bssMax += u.csize[bBSS][cl]
		}
		dataMax += 128
		bssMax += 64
	}
	lay.gcdataMax, lay.gcbssMax = dataMax/8/7+64, bssMax/8/7+64 // a literal run spends 8 bits on 7
	lay.gcdata = va
	va += lay.gcdataMax
	lay.gcbss = va
	va += lay.gcbssMax
	lay.erodata = va

	lay.nameSize = 1 // name offset 0 means "no name"
	for _, u := range units {
		u.funcBase[0] = lay.nfunc // (the function table is in address order: the FIPS functions come last)
		lay.nfunc += u.nfunc[0]
		u.nameBase = lay.nameSize
		lay.nameSize += u.nameSize
		u.pcBase = 1 + lay.pctabSize // offset 0 means "no table"
		lay.pctabSize += u.pcSize
		u.ftabBase = lay.functabSize
		lay.functabSize += u.ftabSize
	}
	for _, u := range units {
		u.funcBase[1] = lay.nfunc
		lay.nfunc += u.nfunc[1]
	}
	lay.pctabSize++
	va = rnd(va, 32)
	lay.pclntab, lay.pcheader = va, va
	va += 8 + 8*8
	lay.funcnametab = va
	va = rnd(va+lay.nameSize, 4)
	lay.cutab = va
	lay.cutabLen = uint32(len(lay.cutabData))
	va += 4 * lay.cutabLen
	lay.filetab = va
	va += fileSize
	lay.pctab = va
	va = rnd(va+lay.pctabSize, 8)
	lay.functab = va
	lay.ftabStructs = rnd(lay.nfunc*8+4, 8)
	lay.functabSize += lay.ftabStructs
	va += lay.functabSize
	va = rnd(va, 32)
	lay.gofunc = va
	bucket(bGofunc, 8)
	va = rnd(va, 4)
	for _, u := range units {
		u.inlBase = va
		va += u.inlSize
	}
	va = rnd(va, 4)
	lay.findfunctab = va
	textLen := lay.etext - lay.text
	lay.findfuncSize = 4*((textLen+abi.FuncTabBucketSize-1)/abi.FuncTabBucketSize) + (textLen+abi.FuncTabBucketSize/16-1)/(abi.FuncTabBucketSize/16)
	va += lay.findfuncSize
	lay.epclntab = va

	va = rnd(va, 32)
	lay.types = va
	va += 8 // so that no type has offset 0
	for i := range tls {
		o, j := objOf(tls[i].gid)
		s := o.sym(j)
		va = rnd(va, max(8, symAlign(s)))
		addr[tls[i].gid] = va
		va += s.Siz()
	}
	lay.typedesclen = va - lay.types
	bucket(bType, 8)
	lay.itabs, lay.eitabs = bucket(bItab, 8)
	lay.etypes = va

	va = rnd(va, 4096)
	lay.noptrdata = va
	place(dataSpecials)
	bucket(bNoptrdata, 8)
	lay.noptrdataFIPS, lay.enoptrdataFIP = bucket(bNoptrdataFIPS, 32)
	lay.enoptrdata = va
	lay.data, _ = bucket(bData, 64) // 64 bytes are one byte of GC bitmap: units write disjoint bytes
	lay.dataFIPS, lay.edataFIPS = bucket(bDataFIPS, 64)
	va = rnd(va, 8)
	lay.edata = va
	lay.bss, _ = bucket(bBSS, 64)
	va = rnd(va, 8)
	lay.ebss = va
	lay.noptrbss, _ = bucket(bNoptrBSS, 32)
	lay.covctrs, lay.ecovctrs = bucket(bCov, 8)
	lay.enoptrbss = va
	lay.end = va
	if va >= 1<<31 {
		fatalf("image too large: %#x", va)
	}
	if lay.nfunc == 0 {
		fatalf("no functions are reachable: is the entry symbol %s defined?", *flagEntry)
	}

	// ---- every live definition's address, name offset, pc-value table offset
	par(len(units), func(_, ui int) {
		u := units[ui]
		o := u.o
		cur := u.cbase
		pc, name := u.pcBase, u.nameBase
		for j := u.lo; j < min(u.hi, o.nalldef); j++ {
			gid := o.base + j
			st := state[gid]
			if st&(stLive|stPC|stNameNeeded) == 0 || canon[gid] != gid {
				continue
			}
			s := o.sym(j)
			if st&stPC != 0 {
				if n := uint32(o.dataSize(j)); n != 0 {
					addr[gid] = pc
					pc += n
				}
			}
			b := int(st >> stBucketShift & 31)
			if isTextBucket(b) || st&stNameNeeded != 0 {
				aux[gid] = name
				name += uint32(o.nameLen(j)) + 1
			}
			if b == bNone || b == bTypelink {
				continue
			}
			cl := st >> stClassShft & 15
			addr[gid] = cur[b][cl]
			cur[b][cl] += occupied(s.Siz(), b, cl)
		}
	})
	defineSyms()
	// ---- every other slot: the address of what it resolves to
	par(len(units), func(_, ui int) {
		u := units[ui]
		base := u.o.base
		for g := base + u.lo; g < base+u.hi; g++ {
			if c := canon[g]; c != g {
				addr[g] = addr[c]
			}
		}
	})
	lap("layout", "%d functions, text %.1f MB, rodata %.1f, pclntab %.1f, types %.1f (%d sorted), data %.1f, bss %.1f",
		lay.nfunc, mb(lay.etext-lay.text), mb(lay.erodata-lay.rodata), mb(lay.epclntab-lay.pclntab), mb(lay.etypes-lay.types), len(tls),
		mb(lay.edata-lay.noptrdata), mb(lay.end-lay.edata))
}

func mb(n uint32) float64 { return float64(n) / (1 << 20) }
