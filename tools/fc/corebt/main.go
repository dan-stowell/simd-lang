// corebt <binary> <core>: every thread's stack by frame pointers, symbolized from the binary's pclntab
package main

import (
	"debug/elf"
	"debug/gosym"
	"encoding/binary"
	"fmt"
	"os"
)

type seg struct {
	va, sz uint64
	off    int64
}

var segs []seg
var cf *os.File

func rd(va uint64) (uint64, bool) {
	for _, s := range segs {
		if va >= s.va && va+8 <= s.va+s.sz {
			var b [8]byte
			if _, err := cf.ReadAt(b[:], s.off+int64(va-s.va)); err != nil {
				return 0, false
			}
			return binary.LittleEndian.Uint64(b[:]), true
		}
	}
	return 0, false
}

func main() {
	bf, err := elf.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	pcln, _ := bf.Section(".gopclntab").Data()
	tab, err := gosym.NewTable(nil, gosym.NewLineTable(pcln, bf.Section(".text").Addr))
	if err != nil {
		panic(err)
	}
	sym := func(pc uint64) string {
		f, l, fn := tab.PCToLine(pc)
		if fn == nil {
			return ""
		}
		_ = f
		return fmt.Sprintf("%s:%d (+%#x)", fn.Name, l, pc-fn.Entry)
	}
	c, err := elf.Open(os.Args[2])
	if err != nil {
		panic(err)
	}
	cf, _ = os.Open(os.Args[2])
	for _, p := range c.Progs {
		if p.Type == elf.PT_LOAD {
			segs = append(segs, seg{p.Vaddr, p.Filesz, int64(p.Off)})
		}
	}
	for _, a := range os.Args[3:] { // extra: addresses to dump as words
		var va uint64
		fmt.Sscanf(a, "0x%x", &va)
		for i := uint64(0); i < 4; i++ {
			v, _ := rd(va + 8*i)
			fmt.Printf("mem %#x: %#x\n", va+8*i, v)
		}
	}
	for _, p := range c.Progs {
		if p.Type != elf.PT_NOTE {
			continue
		}
		b := make([]byte, p.Filesz)
		p.ReadAt(b, 0)
		for len(b) >= 12 {
			namesz, descsz, typ := binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint32(b[4:]), binary.LittleEndian.Uint32(b[8:])
			no := 12 + (namesz+3)&^3
			desc := b[no : no+descsz]
			b = b[no+(descsz+3)&^3:]
			if typ != 1 {
				continue
			}
			pid := binary.LittleEndian.Uint32(desc[32:])
			reg := func(i int) uint64 { return binary.LittleEndian.Uint64(desc[112+8*i:]) }
			rbp, rip, rsp, rdi := reg(4), reg(16), reg(19), reg(14)
			fmt.Printf("\nthread %d: rip %#x %s  futex/rdi %#x  r14(g) %#x r13 %#x\n", pid, rip, sym(rip), rdi, reg(1), reg(2))
			// frame pointers
			fp := rbp
			for n := 0; n < 60 && fp != 0; n++ {
				ra, ok := rd(fp + 8)
				if !ok {
					break
				}
				s := sym(ra)
				fmt.Printf("   %#x %s\n", ra, s)
				nfp, ok := rd(fp)
				if !ok || nfp <= fp {
					// systemstack switch: scan on for the next plausible frame
					break
				}
				fp = nfp
			}
			_ = rsp
		}
	}
}
