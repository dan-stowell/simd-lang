// elf.go: the ELF header, program headers, the Go build id note and a minimal section table.
package fclinklib

import (
	"bytes"
	"debug/elf"
)

func writeELF() {
	type seg struct {
		typ, flags          uint32
		off, vaddr          uint64
		filesz, memsz, algn uint64
	}
	var note []byte
	if *flagBuildid != "" {
		note = le.AppendUint32(note, 4)
		note = le.AppendUint32(note, uint32(len(*flagBuildid)))
		note = le.AppendUint32(note, 4) // ELF_NOTE_GOBUILDID_TAG
		note = append(note, "Go\x00\x00"...)
		note = append(note, *flagBuildid...)
		for len(note)%4 != 0 {
			note = append(note, 0)
		}
	}
	segs := []seg{
		{uint32(elf.PT_LOAD), uint32(elf.PF_R | elf.PF_X), 0, imageBase, uint64(lay.etext - imageBase), uint64(lay.etext - imageBase), 4096},
		{uint32(elf.PT_LOAD), uint32(elf.PF_R), uint64(lay.rodata - imageBase), uint64(lay.rodata), uint64(lay.etypes - lay.rodata), uint64(lay.etypes - lay.rodata), 4096},
		{uint32(elf.PT_LOAD), uint32(elf.PF_R | elf.PF_W), uint64(lay.noptrdata - imageBase), uint64(lay.noptrdata), uint64(lay.edata - lay.noptrdata), uint64(lay.end - lay.noptrdata), 4096},
		{uint32(elf.PT_GNU_STACK), uint32(elf.PF_R | elf.PF_W), 0, 0, 0, 0, 8},
	}
	noteOff := uint64(64 + 56*(len(segs)+1))
	if note != nil {
		segs = append(segs, seg{uint32(elf.PT_NOTE), uint32(elf.PF_R), noteOff, imageBase + noteOff, uint64(len(note)), uint64(len(note)), 4})
		if int(noteOff)+len(note) > headerSize {
			fatalf("build id too long")
		}
		copy(out[noteOff:], note)
	}
	entry := lookup(*flagEntry, 0)
	if entry == 0 || addr[entry] == 0 {
		fatalf("missing entry symbol %s", *flagEntry)
	}

	// sections, for tools (the runtime needs none of this)
	type sect struct {
		name       string
		typ        elf.SectionType
		flags      elf.SectionFlag
		addr, size uint32
		align      uint64
	}
	type fileOnly struct { // a section that is only in the file (the symbol table and its names)
		data       []byte
		link, info uint32
		entsize    uint64
	}
	extra := map[int]fileOnly{}
	bi := buildinfoSym
	sects := []sect{
		{},
		{".text", elf.SHT_PROGBITS, elf.SHF_ALLOC | elf.SHF_EXECINSTR, lay.text, lay.etext - lay.text, 32},
		{".rodata", elf.SHT_PROGBITS, elf.SHF_ALLOC, lay.rodata, lay.erodata - lay.rodata, 32},
		{".gopclntab", elf.SHT_PROGBITS, elf.SHF_ALLOC, lay.pclntab, lay.epclntab - lay.pclntab, 32},
		{".go.type", elf.SHT_PROGBITS, elf.SHF_ALLOC, lay.types, lay.etypes - lay.types, 32},
		{".go.buildinfo", elf.SHT_PROGBITS, elf.SHF_ALLOC | elf.SHF_WRITE, addr[bi], rnd(lay.noptrdata+1, 16) - lay.noptrdata, 16},
		{".noptrdata", elf.SHT_PROGBITS, elf.SHF_ALLOC | elf.SHF_WRITE, lay.noptrdata, lay.enoptrdata - lay.noptrdata, 32},
		{".data", elf.SHT_PROGBITS, elf.SHF_ALLOC | elf.SHF_WRITE, lay.data, lay.edata - lay.data, 64},
		{".bss", elf.SHT_NOBITS, elf.SHF_ALLOC | elf.SHF_WRITE, lay.bss, lay.ebss - lay.bss, 64},
		{".noptrbss", elf.SHT_NOBITS, elf.SHF_ALLOC | elf.SHF_WRITE, lay.noptrbss, lay.enoptrbss - lay.noptrbss, 32},
	}
	for _, sp := range dataSpecials {
		if sp.gid == bi {
			sects[5].size = sp.size
			// .noptrdata starts after it, so the two do not overlap
			sects[6].addr = addr[bi] + sp.size
			sects[6].size = lay.enoptrdata - sects[6].addr
		}
	}
	if note != nil {
		sects = append(sects, sect{".note.go.buildid", elf.SHT_NOTE, elf.SHF_ALLOC, imageBase + uint32(noteOff), uint32(len(note)), 4})
	}
	if !*flagStrip {
		syms, names := symtab()
		extra[len(sects)] = fileOnly{syms, uint32(len(sects) + 1), 1, 24}
		extra[len(sects)+1] = fileOnly{data: names}
		sects = append(sects, sect{name: ".symtab", typ: elf.SHT_SYMTAB, align: 8}, sect{name: ".strtab", typ: elf.SHT_STRTAB, align: 1})
	}
	sects = append(sects, sect{name: ".shstrtab", typ: elf.SHT_STRTAB, align: 1})
	strtab := []byte{0}
	shoff := uint64(rnd(lay.edata-imageBase, 8))
	stroff := shoff + uint64(64*len(sects))
	for _, s := range sects {
		strtab = append(strtab, s.name...)
		strtab = append(strtab, 0)
	}
	// the file-only sections go after the section names, written to the file (the mapping ends there)
	dataOff := (stroff + uint64(len(strtab)) + 7) &^ 7
	sh := out[shoff:]
	strtab = strtab[:1]
	for i, s := range sects {
		if i == 0 {
			continue
		}
		e := sh[64*i:]
		le.PutUint32(e[0:], uint32(len(strtab)))
		strtab = append(strtab, s.name...)
		strtab = append(strtab, 0)
		le.PutUint32(e[4:], uint32(s.typ))
		le.PutUint64(e[8:], uint64(s.flags))
		le.PutUint64(e[16:], uint64(s.addr))
		if s.addr != 0 {
			le.PutUint64(e[24:], uint64(s.addr-imageBase))
		}
		le.PutUint64(e[32:], uint64(s.size))
		le.PutUint64(e[48:], s.align)
		if x, ok := extra[i]; ok {
			le.PutUint32(e[40:], x.link)
			le.PutUint32(e[44:], x.info)
			le.PutUint64(e[56:], x.entsize)
			dataOff = (dataOff + s.align - 1) &^ (s.align - 1)
			le.PutUint64(e[24:], dataOff)
			le.PutUint64(e[32:], uint64(len(x.data)))
			if _, err := outFile.WriteAt(x.data, int64(dataOff)); err != nil {
				fatalf("%v", err)
			}
			dataOff += uint64(len(x.data))
		} else if s.name == ".shstrtab" {
			le.PutUint64(e[24:], stroff)
			le.PutUint64(e[32:], uint64(len(strtab)))
		}
	}
	copy(out[stroff:], strtab)
	outSize = max(int64(stroff)+int64(len(strtab)), int64(dataOff))
	if int64(stroff)+int64(len(strtab)) > int64(len(out)) {
		fatalf("section table does not fit")
	}

	h := out[:64]
	copy(h, "\x7fELF\x02\x01\x01") // 64-bit, little-endian, version 1; OS ABI 0
	le.PutUint16(h[16:], uint16(elf.ET_EXEC))
	le.PutUint16(h[18:], uint16(elf.EM_X86_64))
	le.PutUint32(h[20:], 1)
	le.PutUint64(h[24:], uint64(addr[entry]))
	le.PutUint64(h[32:], 64) // program headers
	le.PutUint64(h[40:], shoff)
	le.PutUint16(h[52:], 64) // header size
	le.PutUint16(h[54:], 56)
	le.PutUint16(h[56:], uint16(len(segs)))
	le.PutUint16(h[58:], 64)
	le.PutUint16(h[60:], uint16(len(sects)))
	le.PutUint16(h[62:], uint16(len(sects)-1))
	for i, s := range segs {
		p := out[64+56*i:]
		le.PutUint32(p[0:], s.typ)
		le.PutUint32(p[4:], s.flags)
		le.PutUint64(p[8:], s.off)
		le.PutUint64(p[16:], s.vaddr)
		le.PutUint64(p[24:], s.vaddr)
		le.PutUint64(p[32:], s.filesz)
		le.PutUint64(p[40:], s.memsz)
		le.PutUint64(p[48:], s.algn)
	}
	if err := outFile.Truncate(outSize); err != nil {
		fatalf("%v", err)
	}
	outFile.Close()
}

// symtab: an ELF symbol table of the functions, read back from the function table and the function names
// already in the output (pclntab): what go tool objdump, nm and addr2line look functions up by. A
// function's size reaches to the next one's entry.
func symtab() (syms, names []byte) {
	at := func(a uint32) []byte { return out[a-imageBase:] }
	ftab, fnames := at(lay.functab), at(lay.funcnametab)
	syms = make([]byte, 24, 24*(lay.nfunc+1))
	names = []byte{0}
	for i := uint32(0); i < lay.nfunc; i++ {
		entry, next := le.Uint32(ftab[8*i:]), le.Uint32(ftab[8*i+8:])
		f := ftab[le.Uint32(ftab[8*i+4:]):]
		name := fnames[le.Uint32(f[4:]):]
		if n := bytes.IndexByte(name, 0); n >= 0 {
			name = name[:n]
		}
		var e [24]byte
		le.PutUint32(e[0:], uint32(len(names)))
		e[4] = byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_FUNC)
		le.PutUint16(e[6:], 1) // .text
		le.PutUint64(e[8:], uint64(lay.text+entry))
		le.PutUint64(e[16:], uint64(next-entry))
		syms = append(syms, e[:]...)
		names = append(names, name...)
		names = append(names, 0)
	}
	return syms, names
}
