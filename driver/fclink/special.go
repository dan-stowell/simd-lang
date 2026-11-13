// special.go: what the linker itself contributes: runtime.firstmoduledata, the init task lists, the string
// variables it sets, go:buildinfo, go:fipsinfo, the section boundary symbols, the fixed tables of pclntab,
// findfunctab, the GC programs of the data and bss sections.
package main

import (
	"cmd/internal/gcprog"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"internal/abi"
	"internal/buildcfg"
	"path/filepath"
	"sort"
	"strings"
)

// A special is a symbol whose content the linker writes: one it makes, or one an object defines and the
// linker overrides (runtime.firstmoduledata, ...).
type special struct {
	gid         uint32
	size, align uint32
	fill        func(p []byte)
}

var roSpecials, dataSpecials []*special

func addSpecial(list *[]*special, gid, size, align uint32, fill func(p []byte)) {
	*list = append(*list, &special{gid, size, align, fill})
	state[gid] |= stLive | stOverride
}

func isLive(g uint32) bool { return g != 0 && state[g]&stLive != 0 }

func expandFile(name string) string {
	name = strings.TrimPrefix(name, "gofile..")
	const n = len("$GOROOT")
	if len(name) >= n+1 && name[:n] == "$GOROOT" && (name[n] == '/' || name[n] == '\\') {
		if buildcfg.GOROOT != "" {
			return filepath.ToSlash(filepath.Join(buildcfg.GOROOT, name[n:]))
		}
	}
	return name
}

func makeSpecials() {
	// string variables: -X, runtime.buildVersion, runtime.modinfo, runtime.defaultGOROOT
	var names []string
	for name := range strdata {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		v := lookupName(name, 0)
		if !isLive(v) || !isDef(v) {
			continue
		}
		val := strdata[name]
		str := synth(name + ".str")
		addSpecial(&roSpecials, str, uint32(len(val)), 1, func(p []byte) { copy(p, val) })
		o, j := objOf(v)
		addSpecial(&dataSpecials, v, max(16, o.r.Sym(j).Siz()), 8, func(p []byte) {
			le.PutUint64(p, uint64(addr[str]))
			le.PutUint64(p[8:], uint64(len(val)))
		})
	}

	// go:buildinfo, which `go version` reads
	bi := []byte("\xff Go buildinf:\x08\x02")
	bi = append(bi, make([]byte, 32-len(bi))...)
	bi = binary.AppendUvarint(bi, uint64(len(strdata["runtime.buildVersion"])))
	bi = append(bi, strdata["runtime.buildVersion"]...)
	bi = binary.AppendUvarint(bi, uint64(len(strdata["runtime.modinfo"])))
	bi = append(bi, strdata["runtime.modinfo"]...)
	for len(bi)%16 != 0 {
		bi = append(bi, 0)
	}
	// (first in the data segment: the specials are placed in order)
	buildinfoSym = synth("go:buildinfo")
	dataSpecials = append([]*special{{buildinfoSym, uint32(len(bi)), 16, func(p []byte) { copy(p, bi) }}}, dataSpecials...)

	md := lookupName("runtime.firstmoduledata", 0)
	if !isDef(md) {
		fatalf("runtime.firstmoduledata is not defined: is the runtime in the importcfg?")
	}
	o, j := objOf(md)
	addSpecial(&dataSpecials, md, o.r.Sym(j).Siz(), 8, fillModuledata)
	if g := lookupName("runtime.lastmoduledatap", 0); isLive(g) {
		addSpecial(&dataSpecials, g, 8, 8, func(p []byte) { le.PutUint64(p, uint64(addr[md])) })
	}
	taskList := func(name string, tasks []uint32) uint32 {
		g := synth(name)
		addSpecial(&dataSpecials, g, uint32(8*len(tasks)), 8, func(p []byte) {
			for i, t := range tasks {
				le.PutUint64(p[8*i:], uint64(addr[t]))
			}
		})
		return g
	}
	mainTasks := taskList("go:main.inittasks", mainInittasks)
	if g := lookupName("runtime.runtime_inittasks", 0); isLive(g) {
		rt := taskList("go:runtime.inittasks", runtimeInittasks)
		addSpecial(&dataSpecials, g, 24, 8, func(p []byte) {
			le.PutUint64(p, uint64(addr[rt]))
			le.PutUint64(p[8:], uint64(len(runtimeInittasks)))
			le.PutUint64(p[16:], uint64(len(runtimeInittasks)))
		})
	}
	modInittasks = mainTasks
	textsect = synth("runtime.textsectionmap")
	addSpecial(&roSpecials, textsect, 24, 8, func(p []byte) {
		le.PutUint64(p[8:], uint64(lay.etext-lay.text))
		le.PutUint64(p[16:], uint64(lay.text))
	})
	// runtime.memProfileInternal not linked in: the runtime need not collect memory profiles
	if mp := lookupName("runtime.memProfileInternal", 1); mp != 0 && !isLive(mp) {
		if g := lookupName("runtime.disableMemoryProfiling", 0); isLive(g) {
			addSpecial(&dataSpecials, g, 1, 1, func(p []byte) { p[0] = 1 })
		}
	}
	// go:fipsinfo (crypto/internal/fips140/check.Linkinfo): magic, the hash of the FIPS sections, their bounds
	if g := lookupName("go:fipsinfo", 0); isLive(g) && !isDef(g) {
		fipsinfo = g
		addSpecial(&dataSpecials, g, 16+32+8+8*8, 8, func(p []byte) {
			copy(p, "\xff Go fipsinfo \xff\x00")
			le.PutUint64(p[48:], uint64(addr[g]))
			for i, a := range fipsBounds() {
				le.PutUint64(p[56+8*i:], uint64(a))
			}
		})
	}
}

var modInittasks, textsect, fipsinfo, buildinfoSym uint32

func fipsBounds() [8]uint32 {
	return [8]uint32{lay.textFIPS, lay.etextFIPS, lay.rodataFIPS, lay.erodataFIPS, lay.noptrdataFIPS, lay.enoptrdataFIP, lay.dataFIPS, lay.edataFIPS}
}

// defineSyms gives the symbols that name places in the image their addresses.
func defineSyms() {
	fb := fipsBounds()
	def := map[string]uint32{
		"runtime.text": lay.text, "runtime.etext": lay.etext,
		"runtime.rodata": lay.rodata, "runtime.erodata": lay.erodata,
		"runtime.types": lay.types, "runtime.etypes": lay.etypes,
		"runtime.noptrdata": lay.noptrdata, "runtime.enoptrdata": lay.enoptrdata,
		"runtime.data": lay.data, "runtime.edata": lay.edata,
		"runtime.bss": lay.bss, "runtime.ebss": lay.ebss,
		"runtime.noptrbss": lay.noptrbss, "runtime.enoptrbss": lay.enoptrbss,
		"runtime.covctrs": lay.covctrs, "runtime.ecovctrs": lay.ecovctrs,
		"runtime.end": lay.end, "runtime.gcdata": lay.gcdata, "runtime.gcbss": lay.gcbss,
		"runtime.egcdata": lay.gcdata + lay.gcdataMax, "runtime.egcbss": lay.gcbss + lay.gcbssMax,
		"runtime.pclntab": lay.pclntab, "runtime.epclntab": lay.epclntab, "runtime.pcheader": lay.pcheader,
		"runtime.funcnametab": lay.funcnametab, "runtime.cutab": lay.cutab, "runtime.filetab": lay.filetab,
		"runtime.pctab": lay.pctab, "runtime.functab": lay.functab, "runtime.findfunctab": lay.findfunctab,
		"go:func.*": lay.gofunc, "runtime.gcmask.*": lay.noptrbss,
		"go:textfipsstart": fb[0], "go:textfipsend": fb[1], "go:rodatafipsstart": fb[2], "go:rodatafipsend": fb[3],
		"go:noptrdatafipsstart": fb[4], "go:noptrdatafipsend": fb[5], "go:datafipsstart": fb[6], "go:datafipsend": fb[7],
	}
	for name, a := range def {
		if g := lookupName(name, 0); g != 0 && !isDef(g) {
			addr[g] = a
		}
	}
}

func fillModuledata(p []byte) {
	off := 0
	word := func(v uint64) {
		le.PutUint64(p[off:], v)
		off += 8
	}
	slice := func(a, n uint32) {
		word(uint64(a))
		word(uint64(n))
		word(uint64(n))
	}
	word(uint64(lay.pcheader))
	slice(lay.funcnametab, lay.nameSize)
	slice(lay.cutab, lay.cutabLen)
	slice(lay.filetab, lay.pctab-lay.filetab)
	slice(lay.pctab, lay.pctabSize)
	slice(lay.functab, lay.functabSize)
	slice(lay.functab, lay.nfunc+1)
	word(uint64(lay.findfunctab))
	ft := out[lay.functab-imageBase:]
	word(uint64(lay.text + le.Uint32(ft)))               // minpc
	word(uint64(lay.text + le.Uint32(ft[8*lay.nfunc:]))) // maxpc
	for _, a := range []uint32{lay.text, lay.etext, lay.noptrdata, lay.enoptrdata, lay.data, lay.edata, lay.bss, lay.ebss,
		lay.noptrbss, lay.enoptrbss, lay.covctrs, lay.ecovctrs, lay.end, lay.gcdata, lay.gcbss, lay.types} {
		word(uint64(a))
	}
	word(uint64(lay.typedesclen))
	word(uint64(lay.etypes))
	word(uint64(lay.itabs - lay.types))
	word(uint64(lay.eitabs - lay.itabs))
	word(uint64(lay.rodata))
	word(uint64(lay.gofunc))
	word(uint64(lay.epclntab))
	slice(addr[textsect], 1)
	slice(0, 0) // ptab
	word(0)     // pluginpath
	word(0)
	slice(0, 0) // pkghashes
	if len(mainInittasks) != 0 {
		slice(addr[modInittasks], uint32(len(mainInittasks)))
	} else {
		slice(0, 0)
	}
	word(0) // modulename
	word(0)
	slice(0, 0) // modulehashes
	p[off] = 1  // hasmain
}

func gcProgram(bm []byte, nwords int64, room []byte) {
	n := 0
	var w gcprog.Writer
	w.Init(func(x byte) {
		if n < len(room) {
			room[n] = x
		}
		n++
	})
	for i := int64(0); i < nwords; i++ {
		if bm[i>>3] == 0 {
			i |= 7
			continue
		}
		if bm[i>>3]>>(i&7)&1 != 0 {
			w.Ptr(i)
		}
	}
	w.ZeroUntil(nwords)
	w.End()
	if n > len(room) {
		errorf("GC program of %d bytes does not fit the %d reserved", n, len(room))
	}
}

func finish() {
	at := func(a uint32) []byte { return out[a-imageBase:] }

	// ---- pclntab: header, cutab, file names; the end of the pc -> function table
	h := at(lay.pcheader)
	le.PutUint32(h, uint32(abi.CurrentPCLnTabMagic))
	h[6], h[7] = 1, 8 // minLC, ptrSize
	le.PutUint64(h[8:], uint64(lay.nfunc))
	le.PutUint64(h[16:], uint64(lay.nfiles))
	for i, a := range []uint32{lay.funcnametab, lay.cutab, lay.filetab, lay.pctab, lay.functab} {
		le.PutUint64(h[32+8*i:], uint64(a-lay.pcheader))
	}
	ct := at(lay.cutab)
	for i, v := range lay.cutabData {
		le.PutUint32(ct[4*i:], v)
	}
	ft := at(lay.filetab)
	for i, name := range lay.fileNames {
		copy(ft[lay.fileOffs[i]:], name)
	}
	// the last function's end: its entry offset plus its size, which is the address after the last
	// instruction of the text; every function's pad is INT3, so the end of the last block will do
	ftab := at(lay.functab)
	lastEntry := le.Uint32(ftab[8*(lay.nfunc-1):])
	end := lay.etext - lay.text
	if end <= lastEntry {
		end = lastEntry + 1
	}
	le.PutUint32(ftab[8*lay.nfunc:], end)

	// ---- findfunctab (ld.(*Link).findfunctab): per 4096-byte bucket of text the index of its first
	// function, and per 256-byte sub-bucket the distance from that
	const sub = abi.FuncTabBucketSize / 16
	textLen := lay.etext - lay.text
	nsub := (textLen + sub - 1) / sub
	nbuckets := (textLen + abi.FuncTabBucketSize - 1) / abi.FuncTabBucketSize
	idx := make([]int32, nsub)
	const grain = 4096
	par(int(lay.nfunc+grain-1)/grain, func(_, c int) {
		for i := uint32(c) * grain; i < min(uint32(c+1)*grain, lay.nfunc); i++ {
			// function i covers [p,q): it is the first function of every sub-bucket that starts in there
			p, q := le.Uint32(ftab[8*i:]), le.Uint32(ftab[8*i+8:])
			for k := (p + sub - 1) / sub; k*sub < q && k < nsub; k++ {
				idx[k] = int32(i)
			}
		}
	})
	fft := at(lay.findfunctab)
	for b := uint32(0); b < nbuckets; b++ {
		base := idx[b*16]
		le.PutUint32(fft[20*b:], uint32(base))
		for k := uint32(0); k < 16 && b*16+k < nsub; k++ {
			d := idx[b*16+k] - base
			if d < 0 || d >= 256 {
				errorf("findfunctab: bucket %d sub-bucket %d: function index %d from base %d", b, k, idx[b*16+k], base)
			}
			fft[20*b+4+k] = byte(d)
		}
	}

	// ---- the GC programs of data and bss
	gcProgram(gcDataBit, int64(lay.edata-lay.data)/8, at(lay.gcdata)[:lay.gcdataMax])
	gcProgram(gcBssBit, int64(lay.ebss-lay.bss)/8, at(lay.gcbss)[:lay.gcbssMax])

	for _, list := range [][]*special{roSpecials, dataSpecials} {
		for _, sp := range list {
			if sp.size != 0 {
				sp.fill(at(addr[sp.gid])[:sp.size])
			}
		}
	}
	if fipsinfo != 0 {
		// the hash the FIPS self-check verifies at start: HMAC-SHA256 with a zero key over the four sections
		hm := hmac.New(sha256.New, make([]byte, 32))
		hm.Write([]byte("go fips object v1\n"))
		fb := fipsBounds()
		for i := 0; i < 8; i += 2 {
			var n [8]byte
			binary.BigEndian.PutUint64(n[:], uint64(fb[i+1]-fb[i]))
			hm.Write(n[:])
			hm.Write(at(fb[i])[:fb[i+1]-fb[i]])
		}
		copy(at(addr[fipsinfo])[16:], hm.Sum(nil))
	}
	writeELF()
	lap("finish", "")
}
