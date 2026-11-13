// elf.go: the ELF header, program headers, the Go build id note and a minimal section table.
package main

import "debug/elf"

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
	bi := synthByName[synthKey{"go:buildinfo", 0}]
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
	sects = append(sects, sect{name: ".shstrtab", typ: elf.SHT_STRTAB, align: 1})
	strtab := []byte{0}
	shoff := uint64(rnd(lay.edata-imageBase, 8))
	stroff := shoff + uint64(64*len(sects))
	sh := out[shoff:]
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
		if s.typ == elf.SHT_STRTAB {
			le.PutUint64(e[24:], stroff)
			le.PutUint64(e[32:], uint64(len(strtab)))
		}
	}
	copy(out[stroff:], strtab)
	outSize = int64(stroff) + int64(len(strtab))
	if outSize > int64(len(out)) {
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
