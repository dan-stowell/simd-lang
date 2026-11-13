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
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"strings"
	"sync"

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
	c          *compiler
	info       *types.Info                     // of the package the body is in
	env        map[*types.TypeParam]types.Type // type arguments of the instance being compiled
	substCache map[types.Type]types.Type
	a          asm
	out        *funcOut
	name       string // symbol name, for naming closures
	sig        *types.Signature
	abi        *abiInfo

	vars     map[*types.Var]loc
	heapVars map[*types.Var]bool
	slots    []slotInfo
	free     map[shape][]int // free slots by shape
	temps    []int           // slots handed out since the function began, released by mark
	locals   int64
	maxarg   int64

	resHome   []loc
	resVars   []*types.Var
	ret       *label
	scopes    []scope
	labels    map[*types.Label]*label
	lblScope  map[*types.Label]*scope
	hasDefer  bool
	nclosure  int
	lastPos   token.Pos
	ctxSlot   loc // the closure object, in a closure
	nosplit   bool
	fallthru  *label
	entry     func()
	bpFix     []int // offsets in the body of frame sizes to fill in (LEAQ n(SP), BP)
	lastPlace int   // the address of the label placed last (another label may already name the current address)
	declSlots []int // the slots of the variables declared in the blocks being compiled
	nDecl     int
	esc       escResult           // escape analysis of the package the body is in; nil: everything escapes
	addrVars  map[*types.Var]bool // variables whose address is taken or that are captured, heap or not
	convSite  ast.Node            // the expression being converted to an interface (for the analysis's verdict)
	rf        *rfCtx              // set in the body of a range-over-func loop
	deferTok  *types.Var          // the runtime's token for defers made in range-over-func loop bodies
	root      *gen                // the function whose results a return there sets
	abi0      bool                // the function itself is called with ABI0 (a wrapper for assembly callers)
}

const fullFrame = -1 << 30 // placeholder SP delta in the body's pcsp events: the whole frame

// ---- types

func (g *gen) typeOf(e ast.Expr) types.Type {
	tv, ok := g.info.Types[e]
	if !ok {
		if id, ok := e.(*ast.Ident); ok {
			if o := g.info.Uses[id]; o != nil {
				return g.subst(o.Type())
			}
			if o := g.info.Defs[id]; o != nil {
				return g.subst(o.Type())
			}
		}
		giveUp("no type for expression %T", e)
	}
	return g.subst(tv.Type)
}

// vt: the type of a variable of the function being compiled (its type arguments applied).
func (g *gen) vt(v *types.Var) types.Type { return g.subst(v.Type()) }

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
	case *types.Pointer:
		return !nihElem(u.Elem())
	case *types.Chan, *types.Map, *types.Signature:
		return true
	}
	return false
}

var nihCache sync.Map // *types.Named -> bool

// nihElem: is elem a type the runtime keeps outside the collected heap (it embeds
// internal/runtime/sys.NotInHeap)? A pointer to one is not a pointer to the collector: no write barrier,
// no bit in a pointer map. Such types exist only in the runtime's own packages.
func nihElem(elem types.Type) bool {
	elem = types.Unalias(elem)
	if n, ok := elem.(*types.Named); ok {
		p := n.Obj().Pkg()
		if p == nil || (p.Path() != "runtime" && !strings.HasPrefix(p.Path(), "internal/runtime/")) {
			return false
		}
		if v, ok := nihCache.Load(n); ok {
			return v.(bool)
		}
		v := notInHeap(n)
		nihCache.Store(n, v)
		return v
	}
	switch elem.Underlying().(type) {
	case *types.Struct, *types.Array:
		return notInHeap(elem)
	}
	return false
}

// ptrBits appends, for each word of t, whether it holds a pointer.
func (c *compiler) ptrBits(t types.Type, bits []bool) []bool {
	if bits == nil {
		// the usual question: the whole map of one type (shared: do not modify)
		l := &c.layout
		l.mu.Lock()
		v, ok := l.ptrs[t]
		l.mu.Unlock()
		if ok {
			return v
		}
		v = c.ptrBitsUncached(t, make([]bool, 0, (c.sizeof(t)+7)/8))
		l.mu.Lock()
		if l.ptrs == nil {
			l.ptrs = map[types.Type][]bool{}
		}
		l.ptrs[t] = v
		l.mu.Unlock()
		return v
	}
	return c.ptrBitsUncached(t, bits)
}

func (c *compiler) ptrBitsUncached(t types.Type, bits []bool) []bool {
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
	case *types.Pointer:
		bits[off/8] = !nihElem(u.Elem())
	case *types.Slice:
		bits[off/8] = !nihElem(u.Elem())
	case *types.Chan, *types.Map, *types.Signature:
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
	case *types.Pointer:
		return !nihElem(u.Elem())
	case *types.Slice:
		return !nihElem(u.Elem())
	case *types.Chan, *types.Map, *types.Signature, *types.Interface:
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

// shapeKey identifies slots that may stand in for each other: the same size and pointer words.
type shape struct {
	size int64
	mask uint64 // pointer words, for slots of up to 64 words
	big  string // beyond that
}

func shapeKey(size int64, ptrs []bool) shape {
	k := shape{size: size}
	if len(ptrs) <= 64 {
		for i, p := range ptrs {
			if p {
				k.mask |= 1 << i
			}
		}
		return k
	}
	b := make([]byte, len(ptrs))
	any := false
	for i, p := range ptrs {
		b[i] = '0'
		if p {
			b[i] = '1'
			any = true
		}
	}
	if any {
		k.big = string(b)
	}
	return k
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
	// a jump to the very next instruction is not emitted: take it back, if nothing refers to the address
	// after it yet
	for n := len(l.jumps); n > 0; n = len(l.jumps) {
		j := l.jumps[n-1]
		at := j - 1
		if j+4 != g.a.pc() || at < 0 || g.a.b[at] != 0xE9 || g.a.maxTarget > at || g.lastPlace > at || !g.nothingAfter(at) {
			break
		}
		g.a.b = g.a.b[:at]
		l.jumps = l.jumps[:n-1]
		for i := len(g.out.lines) - 1; i >= 0 && g.out.lines[i].pc > at; i-- {
			g.out.lines[i].pc = at // a line that began after the jump begins here now
		}
	}
	l.pc = g.a.pc()
	g.lastPlace = l.pc
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
		g.addrTo(SI, src)
		g.addrTo(DI, dst)
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
	// (the address may go to the runtime, where a fault is fatal: a nil base must fault here)
	if l.kind == lInd {
		g.a.load(reg, mb(BP, l.slot), 8, false)
		g.a.nilcheck(reg)
		if l.off != 0 {
			g.a.lea(reg, mb(reg, l.off))
		}
		return
	}
	if l.kind == lRegInd {
		g.a.nilcheck(l.reg)
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

// newGen starts a function. sig is its declared signature; info is the type information of the package its
// body is in; env gives the type arguments when the function is an instance of a generic one.
func (c *compiler) newGen(name string, sig *types.Signature, info *types.Info, env map[*types.TypeParam]types.Type) *gen {
	g := &gen{c: c, name: name, sig: sig, info: info, env: env, vars: map[*types.Var]loc{}, heapVars: map[*types.Var]bool{},
		free: map[shape][]int{}, labels: map[*types.Label]*label{}, lblScope: map[*types.Label]*scope{}}
	g.out = &funcOut{}
	if env != nil {
		g.substCache = map[types.Type]types.Type{}
	}
	var in, out []types.Type
	if r := sig.Recv(); r != nil {
		in = append(in, g.subst(r.Type()))
	}
	for i := 0; i < sig.Params().Len(); i++ {
		in = append(in, g.subst(sig.Params().At(i).Type()))
	}
	for i := 0; i < sig.Results().Len(); i++ {
		out = append(out, g.subst(sig.Results().At(i).Type()))
	}
	g.abi = c.abiOfTypes(in, out)
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
		g.declSlots = append(g.declSlots, i)
		g.nDecl++
		l := loc{kind: lInd, slot: -g.slots[i].off}
		g.vars[v] = l
		return l
	}
	i := g.newSlotBits(g.c.sizeof(g.vt(v)), g.c.ptrBits(g.vt(v), nil))
	g.declSlots = append(g.declSlots, i)
	g.nDecl++
	l := loc{kind: lFrame, off: -g.slots[i].off}
	g.vars[v] = l
	return l
}

// newHeapVar allocates the object of a heap variable (zeroed) and stores its pointer in the variable's slot.
func (g *gen) newHeapVar(v *types.Var) {
	l := g.vars[v]
	g.a.lea(AX, msym(g.c.typeSym(g.vt(v)), 0))
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
	if g.entry != nil {
		g.entry() // a closure saves its context register first
	}
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
			g.move(g.vars[v], home, g.vt(v))
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
		if g.heapVars[v] {
			g.declare(v)
			g.newHeapVar(v)
		} else if v.Name() != "" && v.Name() != "_" {
			g.vars[v] = home
		}
	}
	if g.deferTok != nil && g.rf == nil {
		g.declareVar(g.deferTok) // filled in where the loop starts (rangeFunc)
		g.hasDefer = true
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
	if f := os.Getenv("FC_FRAME"); f != "" && strings.Contains(g.out.sym.Name, f) {
		hist := map[int64]int{}
		for _, sl := range g.slots {
			hist[sl.size]++
		}
		fmt.Fprintf(os.Stderr, "frame %s: locals %d, outgoing %d, %d slots (%d declared), by size %v\n", g.out.sym.Name, L, g.maxarg, len(g.slots), g.nDecl, hist)
	}
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
	if g.abi0 {
		// called by assembly: the fixed registers of ABIInternal are not set up
		p.xorps(X15, X15)
		p.loadG()
	}
	var sp []pcval
	sp = append(sp, pcval{0, 0})
	spAt := p.pc()
	p.push(BP)
	_ = spAt
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
	for _, at := range g.bpFix {
		binary.LittleEndian.PutUint32(body.b[at:], uint32(n))
	}
	code := make([]byte, 0, len(p.b)+len(body.b)+96)
	code = append(append(code, p.b...), body.b...)
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
	relocs = append(append([]obj.Reloc(nil), p.relocs...), relocs...)
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

// escResult is what the code generator asks the escape analysis.
type escResult interface {
	varOnHeap(v *types.Var) bool // must this address-taken or captured variable live on the heap?
	siteOnHeap(n ast.Node) bool  // must this allocation (literal, new, make, closure, boxed value) use the heap?
}

// siteHeap: must the allocation at n use the heap?
func (g *gen) siteHeap(n ast.Node) bool {
	return g.esc == nil || n == nil || g.esc.siteOnHeap(n)
}

// frameObject reserves a slot for an object of type t that lives as long as the function (an allocation
// that does not escape) and returns it zeroed.
func (g *gen) frameObject(t types.Type) loc {
	i := g.newSlotBits(g.c.sizeof(t), g.c.ptrBits(t, nil))
	l := loc{kind: lFrame, off: -g.slots[i].off}
	g.zero(l, t)
	return l
}

// frameAddr: a pointer temporary holding the address of a frame location.
func (g *gen) frameAddr(l loc) loc {
	p := g.ptrTemp()
	g.a.lea(R8, mb(BP, l.off))
	g.a.store(mb(BP, p.off), R8, 8)
	return p
}

// nothingAfter: no relocation and no table entry is at an address beyond pc.
func (g *gen) nothingAfter(pc int) bool {
	last := func(evs []pcval) bool { return len(evs) == 0 || evs[len(evs)-1].pc <= pc }
	if n := len(g.a.relocs); n > 0 && int(g.a.relocs[n-1].Off) >= pc {
		return false
	}
	for _, at := range g.bpFix {
		if at >= pc {
			return false
		}
	}
	return last(g.out.sp) && last(g.out.unsafe) && last(g.out.stackmap)
}

// smallConst: e is an integer constant that fits a sign-extended 32-bit immediate, for an operation on
// values of type t.
func (g *gen) smallConst(e ast.Expr, t types.Type) (int64, bool) {
	if e == nil {
		return 0, false
	}
	tv := g.info.Types[e]
	if tv.Value == nil || !isWord(t) {
		return 0, false
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok || b.Info()&types.IsInteger == 0 {
		return 0, false
	}
	l := g.constLoc(tv.Value, t)
	if l.kind != lImm || int64(int32(l.imm)) != l.imm {
		return 0, false
	}
	return l.imm, true
}
