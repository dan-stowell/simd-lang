// gen.go: the code generator's frame and value model.
//
// One walk over the typed syntax tree of a function writes its machine code. The model is the simplest one
// that is safe under Go's runtime (the same one driver/baseline proved on MinIO):
//
//   - Every variable lives in memory. Parameters that arrive in registers are stored to their ABI spill
//     slots on entry; locals and temporaries have slots below BP. A scalar value is in AX (a float in X0)
//     only between two steps of an expression; nothing is in a register across a call.
//   - Locals are addressed from BP and outgoing arguments from SP, so no offset depends on the final frame
//     size; the prologue, which does, is assembled after the body and put in front of it.
//   - The locals are zeroed on entry and the function has one stack map: every pointer word of every slot,
//     live for the whole function. A slot is only reused for a value with the same pointer layout, so it
//     always holds nil or a valid pointer.
//   - A variable whose address is taken, or that a closure captures, lives on the heap; its slot holds the
//     pointer. (No escape analysis yet.)
//   - Pointer stores to memory outside this frame go through the write barrier.
package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"cmd/internal/obj"
)

type locKind uint8

const (
	lNone   locKind = iota
	lFrame          // off(BP): a local (negative) or a parameter (16 and up)
	lOut            // off(SP): outgoing arguments
	lSym            // sym+off
	lInd            // off(p) where the pointer p is in the frame slot at slot(BP)
	lRegInd         // off(reg): valid until reg is clobbered
	lReg            // in a register: AX for words, X0 for floats
	lImm            // an integer constant (also booleans and nil pointers)
	lZero           // the zero value of any type
)

type loc struct {
	kind locKind
	off  int64
	slot int64
	sym  *obj.LSym
	reg  int
	imm  int64
}

func (l loc) plus(off int64) loc {
	switch l.kind {
	case lFrame, lOut, lSym, lInd, lRegInd:
		l.off += off
	case lZero:
	default:
		panic("plus on a value that is not in memory")
	}
	return l
}

func (l loc) isMem() bool {
	return l.kind == lFrame || l.kind == lOut || l.kind == lSym || l.kind == lInd || l.kind == lRegInd
}

// inFrame: a store here needs no write barrier
func (l loc) inFrame() bool { return l.kind == lFrame || l.kind == lOut }

type slotInfo struct {
	off  int64 // the slot is [BP-off, BP-off+size)
	size int64
	ptrs []bool
}

type label struct {
	pc    int   // -1 until placed
	jumps []int // rel32 offsets waiting for it
}

type scope struct {
	brk, cont *label
	name      string // label of the statement, if any
}

type gen struct {
	c    *compiler
	a    asm
	out  *funcOut
	name string // symbol name, for naming closures
	sig  *types.Signature
	abi  *abiInfo

	vars     map[*types.Var]loc
	heapVars map[*types.Var]bool
	slots    []slotInfo
	free     map[string][]int // free slots by shape
	temps    []int            // slots handed out since the function began, released by mark
	locals   int64
	maxarg   int64

	resHome  []loc
	resVars  []*types.Var
	ret      *label
	scopes   []scope
	labels   map[*types.Label]*label
	lblScope map[*types.Label]*scope
	hasDefer bool
	nclosure int
	lastPos  token.Pos
	ctxSlot  loc // the closure object, in a closure
	nosplit  bool
	fallthru *label
}

const fullFrame = -1 << 30 // placeholder SP delta in the body's pcsp events: the whole frame

// ---- types

func (g *gen) typeOf(e ast.Expr) types.Type {
	tv, ok := g.c.info.Types[e]
	if !ok {
		if id, ok := e.(*ast.Ident); ok {
			if o := g.c.info.Uses[id]; o != nil {
				return o.Type()
			}
			if o := g.c.info.Defs[id]; o != nil {
				return o.Type()
			}
		}
		giveUp("no type for expression %T", e)
	}
	return tv.Type
}

func isFloat(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsFloat != 0
}

func isComplex(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsComplex != 0
}

func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func isSigned(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && b.Info()&types.IsUnsigned == 0
}

func isInterface(t types.Type) bool {
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

// isWord: the value is one machine word or less and travels in an integer register.
func isWord(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsInteger|types.IsBoolean) != 0 || u.Kind() == types.UnsafePointer || u.Kind() == types.UntypedNil
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return true
	}
	return false
}

func isPtrWord(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return true
	}
	return false
}

// ptrBits appends, for each word of t, whether it holds a pointer.
func (c *compiler) ptrBits(t types.Type, bits []bool) []bool {
	size := c.sizeof(t)
	n := int((size + 7) / 8)
	base := len(bits)
	for i := 0; i < n; i++ {
		bits = append(bits, false)
	}
	c.setPtrBits(t, 0, bits[base:])
	return bits
}

func (c *compiler) setPtrBits(t types.Type, off int64, bits []bool) {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Kind() == types.UnsafePointer || u.Info()&types.IsString != 0 {
			bits[off/8] = true
		}
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature, *types.Slice:
		bits[off/8] = true
	case *types.Interface:
		bits[off/8] = true
		bits[off/8+1] = true
	case *types.Struct:
		offs := c.fieldOffsets(u)
		for i := 0; i < u.NumFields(); i++ {
			if c.sizeof(u.Field(i).Type()) != 0 {
				c.setPtrBits(u.Field(i).Type(), off+offs[i], bits)
			}
		}
	case *types.Array:
		es := c.sizeof(u.Elem())
		if es == 0 || !c.hasPointers(u.Elem()) {
			return
		}
		for i := int64(0); i < u.Len(); i++ {
			c.setPtrBits(u.Elem(), off+i*es, bits)
		}
	default:
		panic("ptrBits: " + t.String())
	}
}

func (c *compiler) hasPointers(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() == types.UnsafePointer || u.Info()&types.IsString != 0
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature, *types.Slice, *types.Interface:
		return true
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if c.hasPointers(u.Field(i).Type()) {
				return true
			}
		}
		return false
	case *types.Array:
		return u.Len() > 0 && c.hasPointers(u.Elem())
	}
	panic("hasPointers: " + t.String())
}

// ---- frame slots

func shapeKey(size int64, ptrs []bool) string {
	b := make([]byte, 0, len(ptrs)+8)
	b = fmt.Appendf(b, "%d:", size)
	any := false
	for _, p := range ptrs {
		if p {
			b = append(b, '1')
			any = true
		} else {
			b = append(b, '0')
		}
	}
	if !any {
		return fmt.Sprintf("%d", size)
	}
	return string(b)
}

func (g *gen) newSlotBits(size int64, ptrs []bool) int {
	size = align(size, 8)
	if size == 0 {
		size = 8
	}
	key := shapeKey(size, ptrs)
	if fl := g.free[key]; len(fl) > 0 {
		i := fl[len(fl)-1]
		g.free[key] = fl[:len(fl)-1]
		return i
	}
	g.locals += size
	g.slots = append(g.slots, slotInfo{off: g.locals, size: size, ptrs: ptrs})
	return len(g.slots) - 1
}

// temp returns a frame slot for a value of type t, released by the enclosing mark.
func (g *gen) temp(t types.Type) loc {
	i := g.newSlotBits(g.c.sizeof(t), g.c.ptrBits(t, nil))
	g.temps = append(g.temps, i)
	return loc{kind: lFrame, off: -g.slots[i].off}
}

var ptrShape = []bool{true}

func (g *gen) ptrTemp() loc {
	i := g.newSlotBits(8, ptrShape)
	g.temps = append(g.temps, i)
	return loc{kind: lFrame, off: -g.slots[i].off}
}

func (g *gen) wordTemp() loc {
	i := g.newSlotBits(8, []bool{false})
	g.temps = append(g.temps, i)
	return loc{kind: lFrame, off: -g.slots[i].off}
}

func (g *gen) mark() int { return len(g.temps) }

func (g *gen) release(mark int) {
	for _, i := range g.temps[mark:] {
		s := g.slots[i]
		key := shapeKey(s.size, s.ptrs)
		g.free[key] = append(g.free[key], i)
	}
	g.temps = g.temps[:mark]
}

// ---- labels and positions

func (g *gen) newLabel() *label { return &label{pc: -1} }

func (g *gen) place(l *label) {
	l.pc = g.a.pc()
	for _, j := range l.jumps {
		g.a.patch(j, l.pc)
	}
	l.jumps = nil
}

func (g *gen) jmp(l *label) {
	j := g.a.jmp()
	if l.pc >= 0 {
		g.a.patch(j, l.pc)
	} else {
		l.jumps = append(l.jumps, j)
	}
}

func (g *gen) jcc(cc int, l *label) {
	j := g.a.jcc(cc)
	if l.pc >= 0 {
		g.a.patch(j, l.pc)
	} else {
		l.jumps = append(l.jumps, j)
	}
}

func (g *gen) pos(p token.Pos) {
	if !p.IsValid() || p == g.lastPos {
		return
	}
	g.lastPos = p
	g.out.poss = append(g.out.poss, p)
	g.out.lines = append(g.out.lines, pcval{g.a.pc(), int32(len(g.out.poss) - 1)})
}

func (g *gen) unsafeBegin() { g.out.unsafe = append(g.out.unsafe, pcval{g.a.pc(), -2}) }
func (g *gen) unsafeEnd()   { g.out.unsafe = append(g.out.unsafe, pcval{g.a.pc(), -1}) }

// ---- memory operands, loads and stores

// m is the operand for a value in memory; scratch receives the base pointer of an indirect location.
func (g *gen) m(l loc, scratch int) mem {
	switch l.kind {
	case lFrame:
		return mb(BP, l.off)
	case lOut:
		return mb(SP, l.off)
	case lSym:
		return msym(l.sym, l.off)
	case lInd:
		g.a.load(scratch, mb(BP, l.slot), 8, false)
		return mb(scratch, l.off)
	case lRegInd:
		return mb(l.reg, l.off)
	}
	panic(fmt.Sprintf("m: value of kind %d is not in memory", l.kind))
}

// loadTo: reg = the word or float at l, of type t. A general register is extended to 64 bits as t says.
func (g *gen) loadTo(reg int, l loc, t types.Type) {
	size := int(g.c.sizeof(t))
	if isX(reg) {
		switch l.kind {
		case lReg:
			if l.reg != reg {
				g.a.fmov(reg, l.reg)
			}
		case lZero:
			g.a.xorps(reg, reg)
		default:
			g.a.fload(reg, g.m(l, R13), size)
		}
		return
	}
	switch l.kind {
	case lImm:
		g.a.movImm(reg, l.imm)
	case lZero:
		g.a.movImm(reg, 0)
	case lReg:
		g.a.movRR(reg, l.reg)
	default:
		g.a.load(reg, g.m(l, reg), size, isSigned(t))
	}
}

// storeReg: *l = reg, a word or float of type t; through the write barrier if it is a pointer leaving the frame.
// reg is not R10, R11 or R13.
func (g *gen) storeReg(l loc, reg int, t types.Type) {
	size := int(g.c.sizeof(t))
	if size == 0 {
		return
	}
	if isX(reg) {
		g.a.fstore(g.m(l, R13), reg, size)
		return
	}
	m := g.m(l, R13)
	if isPtrWord(t) && !l.inFrame() {
		g.storePtr(m, reg)
		return
	}
	g.a.store(m, reg, size)
}

// storePtr: *m = reg, a pointer, with the write barrier. Uses R10 and R11.
func (g *gen) storePtr(m mem, reg int) {
	if g.c.noWB {
		g.a.store(m, reg, 8)
		return
	}
	g.unsafeBegin()
	g.a.aluMI(aCMP, 4, msym(g.c.rtVar("writeBarrier"), 0), 0)
	skip := g.a.jcc(ccE)
	g.a.call(g.c.rt("gcWriteBarrier2")) // room for two pointers at R11; every other register is preserved
	g.a.store(mb(R11, 0), reg, 8)
	g.a.load(R10, m, 8, false)
	g.a.store(mb(R11, 8), R10, 8)
	g.a.patch(skip, g.a.pc())
	g.a.store(m, reg, 8)
	g.unsafeEnd()
}

// stash makes a value survive the evaluation of something else: a register goes to a temporary.
func (g *gen) stash(l loc, t types.Type) loc {
	switch l.kind {
	case lReg:
		var tmp loc
		if isPtrWord(t) {
			tmp = g.ptrTemp()
		} else {
			tmp = g.wordTemp()
		}
		if isX(l.reg) {
			g.a.fstore(g.m(tmp, R13), l.reg, int(g.c.sizeof(t)))
		} else {
			g.a.store(g.m(tmp, R13), l.reg, 8)
		}
		return tmp
	case lRegInd:
		// the address survives in a pointer temporary
		tmp := g.ptrTemp()
		g.a.store(g.m(tmp, R13), l.reg, 8)
		return loc{kind: lInd, slot: tmp.off, off: l.off}
	}
	return l
}

// stashValue is stash for a value that later code may overwrite: anything not in this frame is copied.
func (g *gen) stashValue(l loc, t types.Type) loc {
	if l.kind == lSym || l.kind == lInd || l.kind == lRegInd {
		tmp := g.temp(t)
		g.move(tmp, l, t)
		return tmp
	}
	return g.stash(l, t)
}

// regFor: the register a value of type t is computed in.
func regFor(t types.Type) int {
	if isFloat(t) {
		return X0
	}
	return AX
}

// move: *dst = src, a value of type t.
func (g *gen) move(dst, src loc, t types.Type) {
	size := g.c.sizeof(t)
	if size == 0 {
		return
	}
	if src.kind == lZero {
		g.zero(dst, t)
		return
	}
	if isWord(t) || isFloat(t) {
		r := R8
		if isFloat(t) {
			r = X14
		}
		if src.kind == lReg {
			r = src.reg
		} else {
			g.loadTo(r, src, t)
		}
		g.storeReg(dst, r, t)
		return
	}
	if !src.isMem() {
		panic(fmt.Sprintf("move: %v from a value of kind %d", t, src.kind))
	}
	barrier := !dst.inFrame() && g.c.hasPointers(t) && !g.c.noWB
	if size > 128 {
		g.a.lea(SI, g.m(src, R12))
		g.a.lea(DI, g.m(dst, R13))
		if barrier {
			g.a.movRR(BX, DI)
			g.a.movRR(CX, SI)
			g.a.lea(AX, msym(g.c.typeSym(t), 0))
			g.callRT("typedmemmove")
			return
		}
		g.a.movImm(CX, size/8)
		g.a.repMovsq()
		if r := size % 8; r != 0 {
			g.a.movImm(CX, r)
			g.a.repMovsb()
		}
		return
	}
	sm := g.m(src, R12)
	dm := g.m(dst, R13)
	var words []bool
	if barrier {
		words = g.c.ptrBits(t, nil)
	}
	off := int64(0)
	for off+8 <= size {
		w := int(off / 8)
		if off+16 <= size && !(barrier && (words[w] || words[w+1])) {
			g.a.movupsLoad(X14, sm.plus(off))
			g.a.movupsStore(dm.plus(off), X14)
			off += 16
			continue
		}
		g.a.load(R8, sm.plus(off), 8, false)
		if barrier && words[w] {
			g.storePtr(dm.plus(off), R8)
		} else {
			g.a.store(dm.plus(off), R8, 8)
		}
		off += 8
	}
	for _, n := range []int64{4, 2, 1} {
		for off+n <= size {
			g.a.load(R8, sm.plus(off), int(n), false)
			g.a.store(dm.plus(off), R8, int(n))
			off += n
		}
	}
}

// zero: *dst = the zero value of t.
func (g *gen) zero(dst loc, t types.Type) {
	size := g.c.sizeof(t)
	if size == 0 {
		return
	}
	barrier := !dst.inFrame() && g.c.hasPointers(t) && !g.c.noWB
	if isWord(t) || isFloat(t) {
		if barrier {
			g.a.movImm(R8, 0)
			g.storePtr(g.m(dst, R13), R8)
			return
		}
		if isFloat(t) {
			g.a.fstore(g.m(dst, R13), X15, int(size))
			return
		}
		g.a.storeImm(g.m(dst, R13), 0, int(size))
		return
	}
	if size > 128 {
		g.a.lea(DI, g.m(dst, R13))
		if barrier {
			g.a.movRR(BX, DI)
			g.a.lea(AX, msym(g.c.typeSym(t), 0))
			g.callRT("typedmemclr")
			return
		}
		g.a.movImm(CX, size/8)
		g.a.movImm(AX, 0)
		g.a.repStosq()
		if r := size % 8; r != 0 {
			g.a.movImm(CX, r)
			g.a.repStosb()
		}
		return
	}
	dm := g.m(dst, R13)
	var words []bool
	if barrier {
		words = g.c.ptrBits(t, nil)
	}
	off := int64(0)
	for off+8 <= size {
		w := int(off / 8)
		if off+16 <= size && !(barrier && (words[w] || words[w+1])) {
			g.a.movupsStore(dm.plus(off), X15)
			off += 16
			continue
		}
		if barrier && words[w] {
			g.a.movImm(R8, 0)
			g.storePtr(dm.plus(off), R8)
		} else {
			g.a.storeImm(dm.plus(off), 0, 8)
		}
		off += 8
	}
	for _, n := range []int64{4, 2, 1} {
		for off+n <= size {
			g.a.storeImm(dm.plus(off), 0, int(n))
			off += n
		}
	}
}

// addrTo: reg = the address of the memory location l.
func (g *gen) addrTo(reg int, l loc) {
	if l.kind == lInd {
		g.a.load(reg, mb(BP, l.slot), 8, false)
		if l.off != 0 {
			g.a.lea(reg, mb(reg, l.off))
		}
		return
	}
	g.a.lea(reg, g.m(l, R13))
}

// ---- calls to the runtime

// callRT calls a runtime function whose arguments are already in their registers.
func (g *gen) callRT(name string) {
	fn := g.c.rtFunc(name)
	ai := g.c.abiOf(fn.Type().(*types.Signature))
	if ai.argWidth > g.maxarg {
		g.maxarg = ai.argWidth
	}
	g.a.call(g.c.rt(name))
}

// ---- the function

func (c *compiler) newGen(name string, sig *types.Signature) *gen {
	g := &gen{c: c, name: name, sig: sig, vars: map[*types.Var]loc{}, heapVars: map[*types.Var]bool{},
		free: map[string][]int{}, labels: map[*types.Label]*label{}, lblScope: map[*types.Label]*scope{}}
	g.out = &funcOut{}
	g.abi = c.abiOf(sig)
	g.ret = g.newLabel()
	return g
}

// paramLoc: where parameter i (receiver first) lives after the prologue.
func (g *gen) paramLoc(p abiParam) loc { return loc{kind: lFrame, off: 16 + p.off} }

// declare gives a variable its home: a frame slot, or for a variable on the heap a slot for its pointer
// (the object itself is allocated by newHeapVar where the declaration executes).
func (g *gen) declare(v *types.Var) loc {
	if g.heapVars[v] {
		i := g.newSlotBits(8, ptrShape)
		l := loc{kind: lInd, slot: -g.slots[i].off}
		g.vars[v] = l
		return l
	}
	i := g.newSlotBits(g.c.sizeof(v.Type()), g.c.ptrBits(v.Type(), nil))
	l := loc{kind: lFrame, off: -g.slots[i].off}
	g.vars[v] = l
	return l
}

// newHeapVar allocates the object of a heap variable (zeroed) and stores its pointer in the variable's slot.
func (g *gen) newHeapVar(v *types.Var) {
	l := g.vars[v]
	g.a.lea(AX, msym(g.c.typeSym(v.Type()), 0))
	g.callRT("newobject")
	g.a.store(mb(BP, l.slot), AX, 8)
}

// compileFunc generates the code of a function: the parameters' homes, the body (a callback), the return.
// scan is the syntax to search for variables that must live on the heap.
func (g *gen) compileFunc(recv *types.Var, pos, end token.Pos, scan ast.Node, body func()) {
	if scan != nil {
		g.findHeapVars(scan)
	}
	sig := g.sig
	var params []*types.Var
	if recv != nil {
		params = append(params, recv)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	g.pos(pos)
	// parameters: at home in the caller's frame, unless they moved to the heap
	for i, p := range g.abi.in {
		v := params[i]
		home := g.paramLoc(p)
		if v.Name() == "" || v.Name() == "_" {
			continue
		}
		if g.heapVars[v] {
			g.declare(v)
			g.newHeapVar(v)
			g.move(g.vars[v], home, v.Type())
		} else {
			g.vars[v] = home
		}
	}
	// results: stack results live where the caller looks; register results in a local slot
	for i, p := range g.abi.out {
		v := sig.Results().At(i)
		var home loc
		if p.parts == nil && g.c.sizeof(p.typ) != 0 {
			home = loc{kind: lFrame, off: 16 + p.off}
		} else {
			j := g.newSlotBits(g.c.sizeof(p.typ), g.c.ptrBits(p.typ, nil))
			home = loc{kind: lFrame, off: -g.slots[j].off}
		}
		g.resHome = append(g.resHome, home)
		g.resVars = append(g.resVars, v)
		if v.Name() != "" && v.Name() != "_" {
			if g.heapVars[v] {
				g.declare(v)
				g.newHeapVar(v)
			} else {
				g.vars[v] = home
			}
		}
	}
	body()
	g.pos(end)
	g.epilogue()
	g.finish(pos)
}

// epilogue: the deferred calls, the results into their registers, and the return. Every return statement
// jumps here.
func (g *gen) epilogue() {
	g.place(g.ret)
	if g.hasDefer {
		// After a recovered panic the runtime resumes at the instruction before this call.
		g.a.byte(0x90)
		g.callRT("deferreturn")
	}
	for i, p := range g.abi.out {
		v := g.resVars[i]
		if l, ok := g.vars[v]; ok && g.heapVars[v] {
			g.move(g.resHome[i], l, p.typ) // a result that moved to the heap comes back
		}
	}
	for i, p := range g.abi.out {
		for _, q := range p.parts {
			m := g.m(g.resHome[i].plus(q.off), R13)
			if q.float {
				g.a.fload(q.reg, m, q.size)
			} else {
				g.a.load(q.reg, m, q.size, false)
			}
		}
	}
	g.unsafeBegin()
	g.a.rr(0, 8, []byte{0x89}, BP, SP) // MOVQ BP, SP
	g.out.sp = append(g.out.sp, pcval{g.a.pc(), 8})
	g.a.pop(BP)
	g.out.sp = append(g.out.sp, pcval{g.a.pc(), 0})
	g.a.ret()
	g.out.sp = append(g.out.sp, pcval{g.a.pc(), fullFrame})
	g.unsafeEnd()
}

// finish assembles prologue + body + the call to morestack, and hands the function to the compiler.
func (g *gen) finish(pos token.Pos) {
	L := align(g.locals, 8)
	n := L + align(g.maxarg, 8)
	var p asm
	var moreJumps []int
	if !g.nosplit {
		const stackSmall = 128
		const stackBig = 4096
		switch {
		case n+8 <= stackSmall:
			p.rm(0, 8, []byte{0x3B}, SP, mb(R14, 16), 0) // CMPQ SP, 16(R14)
			moreJumps = append(moreJumps, p.jcc(ccBE))
		case n+8 <= stackBig:
			p.lea(R12, mb(SP, -(n+8-stackSmall)))
			p.rm(0, 8, []byte{0x3B}, R12, mb(R14, 16), 0)
			moreJumps = append(moreJumps, p.jcc(ccBE))
		default:
			p.movRR(R12, SP)
			p.aluImm(aSUB, 8, R12, n+8-stackSmall)
			moreJumps = append(moreJumps, p.jcc(ccB))
			p.rm(0, 8, []byte{0x3B}, R12, mb(R14, 16), 0)
			moreJumps = append(moreJumps, p.jcc(ccBE))
		}
	}
	var sp []pcval
	sp = append(sp, pcval{0, 0})
	p.push(BP)
	sp = append(sp, pcval{p.pc(), 8})
	p.movRR(BP, SP)
	if n != 0 {
		p.aluImm(aSUB, 8, SP, n)
	}
	sp = append(sp, pcval{p.pc(), int32(n + 8)})
	for _, in := range g.abi.in {
		for _, q := range in.parts {
			m := mb(BP, 16+in.off+q.off)
			if q.float {
				p.fstore(m, q.reg, q.size)
			} else {
				p.store(m, q.reg, q.size)
			}
		}
	}
	switch {
	case L == 0:
	case L <= 96:
		off := -L
		for ; off+16 <= 0; off += 16 {
			p.movupsStore(mb(BP, off), X15)
		}
		if off < 0 {
			p.storeImm(mb(BP, off), 0, 8)
		}
	default:
		p.lea(DI, mb(BP, -L))
		p.movImm(CX, L/8)
		p.movImm(AX, 0)
		p.repStosq()
	}
	for _, o := range g.abi.out {
		if o.parts == nil && g.c.sizeof(o.typ) != 0 {
			// a result on the stack: zero it (a named result starts at zero, and the collector reads it)
			size := align(g.c.sizeof(o.typ), 8)
			off := 16 + o.off
			if size <= 96 {
				for i := int64(0); i < size; i += 8 {
					p.storeImm(mb(BP, off+i), 0, 8)
				}
			} else {
				p.lea(DI, mb(BP, off))
				p.movImm(CX, size/8)
				p.movImm(AX, 0)
				p.repStosq()
			}
		}
	}
	plen := p.pc()
	body := g.a
	code := append(p.b, body.b...)
	out := g.out
	// tables: the prologue is one unsafe region at the function's first line
	var unsafe, stackmap, lines []pcval
	unsafe = append(unsafe, pcval{0, -2}, pcval{plen, -1})
	stackmap = append(stackmap, pcval{0, -1}, pcval{plen, 1})
	out.poss = append(out.poss, pos)
	lines = append(lines, pcval{0, int32(len(out.poss) - 1)})
	for _, e := range out.unsafe {
		unsafe = append(unsafe, pcval{e.pc + plen, e.val})
	}
	for _, e := range out.lines {
		lines = append(lines, pcval{e.pc + plen, e.val})
	}
	for _, e := range out.sp {
		v := e.val
		if v == fullFrame {
			v = int32(n + 8)
		}
		sp = append(sp, pcval{e.pc + plen, v})
	}
	relocs := body.relocs
	for i := range relocs {
		relocs[i].Off += int32(plen)
	}
	if !g.nosplit {
		// the stack is too small: store the register arguments where the prologue would, grow, start over
		var t asm
		for _, in := range g.abi.in {
			for _, q := range in.parts {
				m := mb(SP, 8+in.off+q.off)
				if q.float {
					t.fstore(m, q.reg, q.size)
				} else {
					t.store(m, q.reg, q.size)
				}
			}
		}
		t.byte(0xE8)
		more := "morestack_noctxt"
		if g.ctxSlot.kind != lNone {
			more = "morestack"
		}
		callOff := len(t.b)
		t.u32(0)
		for _, in := range g.abi.in {
			for _, q := range in.parts {
				m := mb(SP, 8+in.off+q.off)
				if q.float {
					t.fload(q.reg, m, q.size)
				} else {
					t.load(q.reg, m, q.size, false)
				}
			}
		}
		tpc := len(code)
		j := t.jmp()
		code = append(code, t.b...)
		binaryPatch(code, tpc+j, 0)
		for _, mj := range moreJumps {
			binaryPatch(code, mj, tpc)
		}
		relocs = append(relocs, obj.Reloc{Off: int32(tpc + callOff), Siz: 4, Type: objabiRCall, Sym: g.c.rtABI0(more)})
		unsafe = append(unsafe, pcval{tpc, -2})
		stackmap = append(stackmap, pcval{tpc, -1})
		sp = append(sp, pcval{tpc, 0})
		lines = append(lines, pcval{tpc, int32(len(out.poss) - 1)})
	}
	out.code = code
	out.relocs = relocs
	out.sp = sp
	out.unsafe = unsafe
	out.stackmap = stackmap
	out.lines = lines
	out.pos = pos
	out.args = int32(g.abi.argWidth)
	out.locals = int32(n)
	if g.nosplit {
		out.flags |= obj.NOSPLIT
	}
	// stack maps. Arguments: index 0 (entry) has the parameters, index 1 (the body) also the stack results.
	nargs := int(g.abi.argWidth / 8)
	entry := make([]bool, nargs)
	bodyMap := make([]bool, nargs)
	for _, in := range g.abi.in {
		if g.c.sizeof(in.typ) != 0 && g.c.hasPointers(in.typ) {
			g.c.setPtrBits(in.typ, in.off, entry)
			g.c.setPtrBits(in.typ, in.off, bodyMap)
		}
	}
	for _, o := range g.abi.out {
		if o.parts == nil && g.c.sizeof(o.typ) != 0 && g.c.hasPointers(o.typ) {
			g.c.setPtrBits(o.typ, o.off, bodyMap)
		}
	}
	out.argsMap = g.c.stackMapSym(nargs, entry, bodyMap)
	lm := make([]bool, n/8)
	for _, s := range g.slots {
		base := int((n - s.off) / 8)
		for i, b := range s.ptrs {
			if b {
				lm[base+i] = true
			}
		}
	}
	out.localsMap = g.c.stackMapSym(int(n/8), lm, lm)
	g.c.mu.Lock()
	g.c.funcs = append(g.c.funcs, out)
	g.c.mu.Unlock()
}

func binaryPatch(code []byte, at int, target int) {
	v := uint32(int32(target - (at + 4)))
	code[at] = byte(v)
	code[at+1] = byte(v >> 8)
	code[at+2] = byte(v >> 16)
	code[at+3] = byte(v >> 24)
}
