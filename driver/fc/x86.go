// x86.go: the machine-code emitter. Instructions are encoded straight into the function's byte buffer;
// there is no instruction list and no second pass (jumps are rel32 and patched when their label is placed).
package main

import (
	"encoding/binary"
	"fmt"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

// Registers: 0-15 are the general registers by their x86 number, 16-31 are X0-X15.
const (
	AX = iota
	CX
	DX
	BX
	SP
	BP
	SI
	DI
	R8
	R9
	R10
	R11
	R12
	R13
	R14 // g
	R15
	X0  = 16
	X1  = 17
	X14 = 30
	X15 = 31 // zero in ABIInternal
)

const noReg = -1

func isX(r int) bool { return r >= X0 }

// mem is a memory operand: disp(base)(index*scale), or sym+disp(SB) when sym is set (RIP-relative).
type mem struct {
	base  int
	index int // noReg unless indexed
	scale uint8
	disp  int64
	sym   *obj.LSym
}

func mb(base int, disp int64) mem { return mem{base: base, index: noReg, disp: disp} }
func msym(s *obj.LSym, off int64) mem {
	return mem{base: noReg, index: noReg, disp: off, sym: s}
}
func (m mem) plus(off int64) mem { m.disp += off; return m }

type asm struct {
	b      []byte
	relocs []obj.Reloc
}

func (a *asm) pc() int         { return len(a.b) }
func (a *asm) byte(bs ...byte) { a.b = append(a.b, bs...) }
func (a *asm) u32(v uint32)    { a.b = binary.LittleEndian.AppendUint32(a.b, v) }
func (a *asm) u64(v uint64)    { a.b = binary.LittleEndian.AppendUint64(a.b, v) }

// op emits [prefix] [REX] opcode... modrm for a register-direct operand pair (reg field, r/m register).
// w is the operand size in bytes (8 sets REX.W, 2 the 0x66 prefix; 1 forces a REX for SPL..DIL).
func (a *asm) rex(w int, reg, index, rm int, byteRegs bool) {
	r := byte(0)
	if w == 8 {
		r |= 8
	}
	if reg >= 0 && reg&8 != 0 {
		r |= 4
	}
	if index >= 0 && index&8 != 0 {
		r |= 2
	}
	if rm >= 0 && rm&8 != 0 {
		r |= 1
	}
	if r != 0 || byteRegs {
		a.byte(0x40 | r)
	}
}

func needByteRex(w int, rs ...int) bool {
	if w != 1 {
		return false
	}
	for _, r := range rs {
		if r >= SP && r <= DI {
			return true
		}
	}
	return false
}

// rr: opcode bytes with a register-direct modrm.  pfx is 0 or a mandatory prefix (0x66, 0xF2, 0xF3).
func (a *asm) rr(pfx byte, w int, opc []byte, reg, rm int) {
	if w == 2 && pfx == 0 {
		a.byte(0x66)
	}
	if pfx != 0 {
		a.byte(pfx)
	}
	a.rex(w, reg&15, noReg, rm&15, needByteRex(w, reg, rm))
	a.byte(opc...)
	a.byte(0xC0 | byte(reg&7)<<3 | byte(rm&7))
}

// rm: opcode bytes with a memory modrm. immAfter is the number of immediate bytes that will follow the
// operand (a RIP-relative displacement is relative to the end of the instruction).
func (a *asm) rm(pfx byte, w int, opc []byte, reg int, m mem, immAfter int) {
	if w == 2 && pfx == 0 {
		a.byte(0x66)
	}
	if pfx != 0 {
		a.byte(pfx)
	}
	a.rex(w, reg&15, m.index, m.base, needByteRex(w, reg))
	a.byte(opc...)
	regf := byte(reg&7) << 3
	if m.sym != nil {
		a.byte(0x05 | regf)
		a.relocs = append(a.relocs, obj.Reloc{Off: int32(len(a.b)), Siz: 4, Type: objabi.R_PCREL, Sym: m.sym, Add: m.disp - int64(immAfter)})
		a.u32(0)
		return
	}
	if m.base == noReg {
		panic("mem without base")
	}
	if int64(int32(m.disp)) != m.disp {
		panic(fmt.Sprintf("displacement %d out of range", m.disp))
	}
	b := byte(m.base & 7)
	sib := m.index != noReg || b == 4
	var mod byte
	switch {
	case m.disp == 0 && b != 5:
		mod = 0
	case m.disp >= -128 && m.disp <= 127:
		mod = 0x40
	default:
		mod = 0x80
	}
	if sib {
		a.byte(mod | regf | 4)
		idx := byte(4) // none
		sc := byte(0)
		if m.index != noReg {
			if m.index == SP {
				panic("SP as index")
			}
			idx = byte(m.index & 7)
			switch m.scale {
			case 1:
			case 2:
				sc = 1
			case 4:
				sc = 2
			case 8:
				sc = 3
			default:
				panic("bad scale")
			}
		}
		a.byte(sc<<6 | idx<<3 | b)
	} else {
		a.byte(mod | regf | b)
	}
	switch mod {
	case 0x40:
		a.byte(byte(m.disp))
	case 0x80:
		a.u32(uint32(m.disp))
	}
}

// ---- moves

// load: reg = *m, zero- or sign-extended from size bytes to 64 bits.
func (a *asm) load(reg int, m mem, size int, signed bool) {
	switch {
	case size == 8:
		a.rm(0, 8, []byte{0x8B}, reg, m, 0)
	case size == 4 && signed:
		a.rm(0, 8, []byte{0x63}, reg, m, 0)
	case size == 4:
		a.rm(0, 4, []byte{0x8B}, reg, m, 0)
	case size == 2 && signed:
		a.rm(0, 8, []byte{0x0F, 0xBF}, reg, m, 0)
	case size == 2:
		a.rm(0, 4, []byte{0x0F, 0xB7}, reg, m, 0)
	case size == 1 && signed:
		a.rm(0, 8, []byte{0x0F, 0xBE}, reg, m, 0)
	case size == 1:
		a.rm(0, 4, []byte{0x0F, 0xB6}, reg, m, 0)
	default:
		panic("load size")
	}
}

// store: *m = the low size bytes of reg.
func (a *asm) store(m mem, reg int, size int) {
	switch size {
	case 8, 4, 2:
		a.rm(0, size, []byte{0x89}, reg, m, 0)
	case 1:
		a.rm(0, 1, []byte{0x88}, reg, m, 0)
	default:
		panic("store size")
	}
}

// storeImm: *m = v (size bytes; an 8-byte store sign-extends the 32-bit immediate).
func (a *asm) storeImm(m mem, v int64, size int) {
	switch size {
	case 8, 4:
		a.rm(0, size, []byte{0xC7}, 0, m, 4)
		a.u32(uint32(v))
	case 2:
		a.rm(0, 2, []byte{0xC7}, 0, m, 2)
		a.byte(byte(v), byte(v>>8))
	case 1:
		a.rm(0, 1, []byte{0xC6}, 0, m, 1)
		a.byte(byte(v))
	default:
		panic("storeImm size")
	}
}

func (a *asm) movRR(dst, src int) {
	if dst != src {
		a.rr(0, 8, []byte{0x89}, src, dst)
	}
}

func (a *asm) movImm(reg int, v int64) {
	switch {
	case v == 0:
		a.rr(0, 4, []byte{0x31}, reg, reg)
	case v > 0 && v <= 0xFFFFFFFF:
		a.rex(4, noReg, noReg, reg, false)
		a.byte(0xB8 | byte(reg&7))
		a.u32(uint32(v))
	case int64(int32(v)) == v:
		a.rr(0, 8, []byte{0xC7}, 0, reg)
		a.u32(uint32(v))
	default:
		a.rex(8, noReg, noReg, reg, false)
		a.byte(0xB8 | byte(reg&7))
		a.u64(uint64(v))
	}
}

func (a *asm) lea(reg int, m mem) { a.rm(0, 8, []byte{0x8D}, reg, m, 0) }

// extend: reg = its low size bytes, zero- or sign-extended to 64 bits.
func (a *asm) extend(reg int, size int, signed bool) {
	switch {
	case size == 8:
	case size == 4 && signed:
		a.rr(0, 8, []byte{0x63}, reg, reg)
	case size == 4:
		a.rr(0, 4, []byte{0x89}, reg, reg)
	case size == 2 && signed:
		a.rr(0, 8, []byte{0x0F, 0xBF}, reg, reg)
	case size == 2:
		a.rr(0, 4, []byte{0x0F, 0xB7}, reg, reg)
	case size == 1 && signed:
		a.rr(0, 8, []byte{0x0F, 0xBE}, reg, reg)
	case size == 1:
		// a size-1 operand forces the REX that selects SIL/DIL rather than DH/BH; the destination is 32 bits
		a.rex(4, reg, noReg, reg, reg >= SP && reg <= DI)
		a.byte(0x0F, 0xB6, 0xC0|byte(reg&7)<<3|byte(reg&7))
	}
}

// ---- arithmetic. The alu operations by their /n number: the opcode of "op r/m, r" is n*8+1.
const (
	aADD = 0
	aOR  = 1
	aADC = 2
	aSBB = 3
	aAND = 4
	aSUB = 5
	aXOR = 6
	aCMP = 7
)

func (a *asm) aluRR(op int, w int, dst, src int) {
	o := byte(op*8 + 1)
	if w == 1 {
		o--
	}
	a.rr(0, w, []byte{o}, src, dst)
}

// aluRM: reg op= *m
func (a *asm) aluRM(op int, w int, reg int, m mem) {
	o := byte(op*8 + 3)
	if w == 1 {
		o--
	}
	a.rm(0, w, []byte{o}, reg, m, 0)
}

func (a *asm) aluImm(op int, w int, reg int, v int64) {
	switch {
	case w == 1:
		a.rr(0, 1, []byte{0x80}, op, reg)
		a.byte(byte(v))
	case v >= -128 && v <= 127:
		a.rr(0, w, []byte{0x83}, op, reg)
		a.byte(byte(v))
	case int64(int32(v)) == v || w <= 4:
		a.rr(0, w, []byte{0x81}, op, reg)
		if w == 2 {
			a.byte(byte(v), byte(v>>8))
		} else {
			a.u32(uint32(v))
		}
	default:
		panic("aluImm: immediate does not fit")
	}
}

// aluMI: *m op= v (or a compare), w bytes wide
func (a *asm) aluMI(op int, w int, m mem, v int64) {
	switch {
	case w == 1:
		a.rm(0, 1, []byte{0x80}, op, m, 1)
		a.byte(byte(v))
	case v >= -128 && v <= 127:
		a.rm(0, w, []byte{0x83}, op, m, 1)
		a.byte(byte(v))
	case w == 2:
		a.rm(0, w, []byte{0x81}, op, m, 2)
		a.byte(byte(v), byte(v>>8))
	default:
		a.rm(0, w, []byte{0x81}, op, m, 4)
		a.u32(uint32(v))
	}
}

func (a *asm) testRR(w int, r1, r2 int) {
	o := byte(0x85)
	if w == 1 {
		o = 0x84
	}
	a.rr(0, w, []byte{o}, r2, r1)
}

// nilcheck: fault if reg is nil (TESTB AL, (reg))
func (a *asm) nilcheck(reg int) { a.rm(0, 1, []byte{0x84}, AX, mb(reg, 0), 0) }

func (a *asm) imulRR(w int, dst, src int) {
	if w == 1 {
		w = 4
	}
	a.rr(0, w, []byte{0x0F, 0xAF}, dst, src)
}
func (a *asm) imulImm(dst, src int, v int32) {
	a.rr(0, 8, []byte{0x69}, dst, src)
	a.u32(uint32(v))
}

// unary group F7 (F6 for bytes): 2 NOT, 3 NEG, 4 MUL, 5 IMUL, 6 DIV, 7 IDIV
func (a *asm) unary(n int, w int, reg int) {
	o := byte(0xF7)
	if w == 1 {
		o = 0xF6
	}
	a.rr(0, w, []byte{o}, n, reg)
}

// shift by CL: 4 SHL, 5 SHR, 7 SAR
func (a *asm) shiftCL(n int, w int, reg int) {
	o := byte(0xD3)
	if w == 1 {
		o = 0xD2
	}
	a.rr(0, w, []byte{o}, n, reg)
}
func (a *asm) shiftImm(n int, w int, reg int, c uint8) {
	o := byte(0xC1)
	if w == 1 {
		o = 0xC0
	}
	a.rr(0, w, []byte{o}, n, reg)
	a.byte(c)
}

// sign-extend AX into DX (CQO, CDQ, CWD)
func (a *asm) cxd(w int) {
	switch w {
	case 8:
		a.byte(0x48, 0x99)
	case 4:
		a.byte(0x99)
	case 2:
		a.byte(0x66, 0x99)
	}
}

// condition codes (the low nibble of Jcc/SETcc)
const (
	ccO  = 0x0
	ccB  = 0x2 // unsigned <   (carry)
	ccAE = 0x3
	ccE  = 0x4
	ccNE = 0x5
	ccBE = 0x6
	ccA  = 0x7
	ccS  = 0x8
	ccP  = 0xA
	ccNP = 0xB
	ccL  = 0xC
	ccGE = 0xD
	ccLE = 0xE
	ccG  = 0xF
)

func (a *asm) setcc(cc int, reg int) {
	a.rex(4, noReg, noReg, reg, reg >= SP && reg <= DI)
	a.byte(0x0F, 0x90|byte(cc), 0xC0|byte(reg&7))
}

// jcc and jmp return the offset of their rel32 for patching.
func (a *asm) jcc(cc int) int {
	a.byte(0x0F, 0x80|byte(cc))
	a.u32(0)
	return len(a.b) - 4
}
func (a *asm) jmp() int {
	a.byte(0xE9)
	a.u32(0)
	return len(a.b) - 4
}
func (a *asm) patch(at int, target int) {
	binary.LittleEndian.PutUint32(a.b[at:], uint32(int32(target-(at+4))))
}
func (a *asm) jmpTo(target int)         { a.patch(a.jmp(), target) }
func (a *asm) jccTo(cc int, target int) { a.patch(a.jcc(cc), target) }

func (a *asm) call(s *obj.LSym) {
	a.byte(0xE8)
	a.relocs = append(a.relocs, obj.Reloc{Off: int32(len(a.b)), Siz: 4, Type: objabi.R_CALL, Sym: s})
	a.u32(0)
}
func (a *asm) callReg(reg int) { a.rr(0, 4, []byte{0xFF}, 2, reg) }
func (a *asm) jmpReg(reg int)  { a.rr(0, 4, []byte{0xFF}, 4, reg) }
func (a *asm) ret()            { a.byte(0xC3) }
func (a *asm) ud2()            { a.byte(0x0F, 0x0B) }
func (a *asm) push(reg int) {
	a.rex(4, noReg, noReg, reg, false)
	a.byte(0x50 | byte(reg&7))
}
func (a *asm) pop(reg int) {
	a.rex(4, noReg, noReg, reg, false)
	a.byte(0x58 | byte(reg&7))
}
func (a *asm) repStosq() { a.byte(0xF3, 0x48, 0xAB) }
func (a *asm) repMovsq() { a.byte(0xF3, 0x48, 0xA5) }
func (a *asm) repMovsb() { a.byte(0xF3, 0xA4) }
func (a *asm) repStosb() { a.byte(0xF3, 0xAA) }

// ---- floating point (scalar SSE2). size is 4 or 8.
func fpfx(size int) byte {
	if size == 4 {
		return 0xF3
	}
	return 0xF2
}
func (a *asm) fload(x int, m mem, size int)  { a.rm(fpfx(size), 4, []byte{0x0F, 0x10}, x, m, 0) }
func (a *asm) fstore(m mem, x int, size int) { a.rm(fpfx(size), 4, []byte{0x0F, 0x11}, x, m, 0) }
func (a *asm) fmov(dst, src int)             { a.rr(0, 4, []byte{0x0F, 0x28}, dst, src) } // MOVAPS

// farith: dst op= src; op is the opcode byte: 0x58 add, 0x59 mul, 0x5C sub, 0x5E div, 0x51 sqrt
func (a *asm) farith(op byte, size int, dst, src int) {
	a.rr(fpfx(size), 4, []byte{0x0F, op}, dst, src)
}
func (a *asm) farithM(op byte, size int, dst int, m mem) {
	a.rm(fpfx(size), 4, []byte{0x0F, op}, dst, m, 0)
}
func (a *asm) ucomis(size int, x, y int) {
	if size == 8 {
		a.rr(0x66, 4, []byte{0x0F, 0x2E}, x, y)
	} else {
		a.rr(0, 4, []byte{0x0F, 0x2E}, x, y)
	}
}

// cvtsi2f: x = float(reg), from a 64-bit (w=8) or 32-bit integer
func (a *asm) cvtsi2f(size int, x, reg int, w int) { a.rr(fpfx(size), w, []byte{0x0F, 0x2A}, x, reg) }

// cvttf2si: reg = int(x), truncating, to a 64-bit or 32-bit integer
func (a *asm) cvttf2si(size int, reg, x int, w int) { a.rr(fpfx(size), w, []byte{0x0F, 0x2C}, reg, x) }

// cvtf2f: dst (size to) = src (size from)
func (a *asm) cvtf2f(from int, dst, src int) { a.rr(fpfx(from), 4, []byte{0x0F, 0x5A}, dst, src) }
func (a *asm) xorps(dst, src int)            { a.rr(0, 4, []byte{0x0F, 0x57}, dst, src) }

// movq between a general register and an X register (the 64 bits, as they are)
func (a *asm) movqXR(x, reg int) { a.rr(0x66, 8, []byte{0x0F, 0x6E}, x, reg) }
func (a *asm) movqRX(reg, x int) { a.rr(0x66, 8, []byte{0x0F, 0x7E}, x, reg) }
func (a *asm) movdXR(x, reg int) { a.rr(0x66, 4, []byte{0x0F, 0x6E}, x, reg) }
func (a *asm) movdRX(reg, x int) { a.rr(0x66, 4, []byte{0x0F, 0x7E}, x, reg) }

// 16 bytes at a time, unaligned
func (a *asm) movupsLoad(x int, m mem)  { a.rm(0, 4, []byte{0x0F, 0x10}, x, m, 0) }
func (a *asm) movupsStore(m mem, x int) { a.rm(0, 4, []byte{0x0F, 0x11}, x, m, 0) }

// loadG: R14 = g, from thread-local storage (MOVQ FS:-8, R14 once the linker fills in the offset).
func (a *asm) loadG() {
	a.byte(0x64, 0x4C, 0x8B, 0x34, 0x25)
	a.relocs = append(a.relocs, obj.Reloc{Off: int32(len(a.b)), Siz: 4, Type: objabi.R_TLS_LE})
	a.u32(0)
}
