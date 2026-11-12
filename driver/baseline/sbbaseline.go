// sbbaseline.go: a baseline code generator for the stock compiler's lowered trees. (Part of the sb patch
// set: driver/mkcompiler.sh copies this file into cmd/compile/internal/ssagen and hooks it into Compile.)
//
// With SB_BASELINE set, every function reaches baselineCompile before the SSA back end. If the function
// stays inside what this file understands, its machine code is produced here in one walk over the tree
// that `walk` left: no SSA construction, no optimisation passes, no register allocation. Anything else
// makes baselineCompile give up (a panic carrying the reason, recovered at the top) and the function goes
// to SSA as if nothing had happened.
//
// The design is the simplest one that is safe under Go's runtime:
//
//   - Every variable lives in memory: parameters passed in registers are stored to their ABI spill slots
//     on entry, locals and temporaries have typed slots in the frame. An expression's value is in AX (and
//     only there) between two tree nodes; nothing is in a register across a call.
//   - The frame is zeroed on entry and the function has one stack map: every pointer word of every slot,
//     live for the whole function. A slot therefore always holds nil or a valid pointer.
//   - The whole body is marked as an unsafe point, so the function is preempted only at calls, where the
//     stack map is exact.
//   - Pointer stores to memory that is not this frame go through the write barrier.
package ssagen

import (
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"sync"

	"cmd/compile/internal/abi"
	"cmd/compile/internal/base"
	"cmd/compile/internal/bitvec"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/objw"
	"cmd/compile/internal/reflectdata"
	"cmd/compile/internal/rttype"
	"cmd/compile/internal/staticdata"
	"cmd/compile/internal/typebits"
	"cmd/compile/internal/types"
	"cmd/internal/obj"
	"cmd/internal/obj/x86"
	rtabi "internal/abi"
)

var (
	sbBaseline     = os.Getenv("SB_BASELINE") != ""
	sbBaselineOnly = os.Getenv("SB_BASELINE_ONLY") // substring of the symbol name
	sbBaselineHash = os.Getenv("SB_BASELINE_HASH") // "k/n": only functions whose name hashes to k mod n (bisecting)
	sbBaselineLog  = os.Getenv("SB_BASELINE_LOG")  // directory: one file per compiler process, a line per function
	sbLogMu        sync.Mutex
	sbLogFile      *os.File
)

type sbBail string

func sbGiveUp(format string, a ...any) { panic(sbBail(fmt.Sprintf(format, a...))) }

func sbLog(fn *ir.Func, verdict string) {
	if sbBaselineLog == "" {
		return
	}
	sbLogMu.Lock()
	if sbLogFile == nil {
		sbLogFile, _ = os.OpenFile(fmt.Sprintf("%s/%d.txt", sbBaselineLog, os.Getpid()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	fmt.Fprintf(sbLogFile, "%s\t%s\n", fn.LSym.Name, verdict)
	sbLogMu.Unlock()
}

// sbPackageOK: packages whose functions the baseline may take. The runtime and what it stands on have
// rules of their own (no write barriers here, no stack growth there) that the tree does not show.
func sbPackageOK() bool {
	if base.Flag.CompilingRuntime {
		return false
	}
	p := base.Ctxt.Pkgpath
	if base.Flag.Std && (strings.HasPrefix(p, "internal/") || p == "reflect" || p == "sync" || strings.HasPrefix(p, "sync/") ||
		p == "syscall" || p == "os" || p == "time" || strings.HasPrefix(p, "runtime") || p == "unsafe" || p == "math" || strings.HasPrefix(p, "math/")) {
		return false
	}
	return true
}

// baselineCompile compiles fn and reports true, or reports false having changed nothing.
func baselineCompile(fn *ir.Func, worker int) (ok bool) {
	if !sbPackageOK() {
		return false
	}
	name := fn.LSym.Name
	if sbBaselineOnly != "" && !strings.Contains(name, sbBaselineOnly) {
		return false
	}
	if sbBaselineHash != "" {
		ks, ns, _ := strings.Cut(sbBaselineHash, "/")
		k, _ := strconv.Atoi(ks)
		n, _ := strconv.Atoi(ns)
		h := fnv.New32a()
		h.Write([]byte(name))
		if n > 0 && int(h.Sum32()%uint32(n)) != k {
			return false
		}
	}
	pp := objw.NewProgs(fn, worker)
	g := &sbGen{fn: fn, pp: pp, homes: map[*ir.Name]sbLoc{}, labels: map[*types.Sym]*sbLabel{}}
	defer func() {
		if r := recover(); r != nil {
			why, isBail := r.(sbBail)
			if !isBail {
				panic(r)
			}
			pp.Free()
			sbLog(fn, "ssa\t"+string(why))
			ok = false
		}
	}()
	g.function()
	// Nothing global has been touched until here; from here on nothing can fail.
	fi := fn.LSym.Func()
	for _, s := range g.spills {
		fi.AddSpill(s)
	}
	fi.GCArgs, fi.GCLocals = g.argsSym, g.localsSym
	pp.Flush()
	fieldtrack(pp.Text.From.Sym, fn.FieldTrack)
	pp.Free()
	sbLog(fn, "baseline")
	return true
}

// ---- locations

type sbLocKind uint8

const (
	sbAuto   sbLocKind = iota // a slot in this frame: off is negative, from the top of the locals
	sbParam                   // in the argument area: off from its start
	sbGlobal                  // a symbol + off
	sbInd                     // off from the pointer in reg
	sbOut                     // in the outgoing argument area: off from the stack pointer
	sbHeapVar                 // a variable moved to the heap: the frame slot at off holds its address (see resolve)
)

type sbLoc struct {
	kind sbLocKind
	off  int64
	sym  *obj.LSym
	reg  int16
}

func (l sbLoc) addr() obj.Addr {
	switch l.kind {
	case sbAuto:
		return obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_AUTO, Reg: x86.REG_SP, Offset: l.off}
	case sbParam:
		return obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_PARAM, Reg: x86.REG_SP, Offset: l.off}
	case sbGlobal:
		return obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: l.sym, Offset: l.off}
	case sbOut:
		return obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_SP, Offset: l.off}
	case sbHeapVar:
		sbGiveUp("heap variable used without loading its address")
	}
	return obj.Addr{Type: obj.TYPE_MEM, Reg: l.reg, Offset: l.off}
}

func (l sbLoc) plus(off int64) sbLoc {
	if l.kind == sbHeapVar {
		sbGiveUp("offset into an unresolved heap variable")
	}
	l.off += off
	return l
}

// resolve turns the home of a heap-moved variable into a real location: its address is loaded into AX.
func (g *sbGen) resolve(l sbLoc) sbLoc {
	if l.kind != sbHeapVar {
		return l
	}
	g.ins(x86.AMOVQ, sbLoc{kind: sbAuto, off: l.off}.addr(), sbR(x86.REG_AX))
	return sbLoc{kind: sbInd, reg: x86.REG_AX}
}

// newHeap allocates the heap home of variable n and stores its address in n's pointer slot.
func (g *sbGen) newHeap(n *ir.Name) {
	l := g.homes[n]
	if l.kind != sbHeapVar {
		sbGiveUp("heap allocation for a stack variable")
	}
	t := n.Type()
	if t.Size() == 0 {
		g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: ir.Syms.Zerobase}, sbR(x86.REG_AX))
	} else {
		g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: reflectdata.TypeLinksym(t)}, sbR(x86.REG_AX))
		g.call(ir.Syms.Newobject)
	}
	g.ins(x86.AMOVQ, sbR(x86.REG_AX), sbLoc{kind: sbAuto, off: l.off}.addr())
}

func (g *sbGen) heapHome() sbLoc {
	return sbLoc{kind: sbHeapVar, off: g.newSlot(types.Types[types.TUNSAFEPTR]).off}
}

// inFrame: a store here needs no write barrier
func (l sbLoc) inFrame() bool { return l.kind == sbAuto || l.kind == sbParam || l.kind == sbOut }

type sbLabel struct {
	target *obj.Prog   // set once the label is placed
	jumps  []*obj.Prog // jumps waiting for it
}

type sbSlot struct {
	off int64 // top-relative, negative
	t   *types.Type
}

type sbGen struct {
	fn      *ir.Func
	pp      *objw.Progs
	abi     *abi.ABIParamResultInfo
	homes   map[*ir.Name]sbLoc
	slots   []sbSlot // every slot of the frame, for the stack map
	locals  int64    // bytes of locals so far
	maxarg  int64    // bytes of outgoing argument area
	freePtr []int64  // released one-word temporaries that hold pointers
	freeInt []int64  // released one-word temporaries that hold no pointers
	labels  map[*types.Sym]*sbLabel
	brk     []*sbLabel // innermost last
	cont    []*sbLabel
	lastRes []sbLoc // where the results of the most recent call were put
	resHome []sbLoc // this function's results, by index
	resHeap []sbLoc // for a result moved to the heap: its pointer slot (kind sbHeapVar); zero value otherwise
	spills  []obj.RegSpill

	argsSym, localsSym *obj.LSym
}

var sbIntRegs = []int16{x86.REG_AX, x86.REG_BX, x86.REG_CX, x86.REG_DI, x86.REG_SI, x86.REG_R8, x86.REG_R9, x86.REG_R10, x86.REG_R11}

func sbReg(r abi.RegIndex) int16 {
	if int(r) >= len(sbIntRegs) {
		sbGiveUp("floating-point register")
	}
	return sbIntRegs[r]
}

// ---- emission helpers

func sbR(r int16) obj.Addr   { return obj.Addr{Type: obj.TYPE_REG, Reg: r} }
func sbImm(v int64) obj.Addr { return obj.Addr{Type: obj.TYPE_CONST, Offset: v} }

func (g *sbGen) ins(as obj.As, from, to obj.Addr) *obj.Prog {
	p := g.pp.Prog(as)
	p.From, p.To = from, to
	return p
}

func (g *sbGen) ins1(as obj.As, to obj.Addr) *obj.Prog {
	p := g.pp.Prog(as)
	p.To = to
	return p
}

func (g *sbGen) jump(as obj.As) *obj.Prog {
	p := g.pp.Prog(as)
	p.To.Type = obj.TYPE_BRANCH
	return p
}

// here returns a Prog that the next instruction will follow; jumps may target it.
func (g *sbGen) here() *obj.Prog { return g.pp.Prog(obj.ANOP) }

func (g *sbGen) land(ps ...*obj.Prog) {
	t := g.here()
	for _, p := range ps {
		p.To.SetTarget(t)
	}
}

func (g *sbGen) jumpTo(as obj.As, l *sbLabel) {
	p := g.jump(as)
	if l.target != nil {
		p.To.SetTarget(l.target)
	} else {
		l.jumps = append(l.jumps, p)
	}
}

func (g *sbGen) place(l *sbLabel) {
	l.target = g.here()
	for _, p := range l.jumps {
		p.To.SetTarget(l.target)
	}
	l.jumps = nil
}

func (g *sbGen) call(sym *obj.LSym) {
	p := g.pp.Prog(obj.ACALL)
	p.To.Type = obj.TYPE_MEM
	p.To.Name = obj.NAME_EXTERN
	p.To.Sym = sym
}

// ---- types

// sbScalar: the type is one machine word or less, held in an integer register: integers, booleans,
// pointers and everything shaped like a pointer.
func sbScalar(t *types.Type) bool {
	if t == nil {
		return false
	}
	return t.IsInteger() || t.IsBoolean() || t.IsPtrShaped() || t.IsUintptr() || t.IsUnsafePtr()
}

func sbLoadOp(t *types.Type) obj.As {
	switch t.Size() {
	case 8:
		return x86.AMOVQ
	case 4:
		if t.IsSigned() {
			return x86.AMOVLQSX
		}
		return x86.AMOVL
	case 2:
		if t.IsSigned() {
			return x86.AMOVWQSX
		}
		return x86.AMOVWQZX
	case 1:
		if t.IsSigned() {
			return x86.AMOVBQSX
		}
		return x86.AMOVBQZX
	}
	sbGiveUp("scalar of size %d", t.Size())
	return 0
}

func sbStoreOp(t *types.Type) obj.As {
	switch t.Size() {
	case 8:
		return x86.AMOVQ
	case 4:
		return x86.AMOVL
	case 2:
		return x86.AMOVW
	case 1:
		return x86.AMOVB
	}
	sbGiveUp("scalar of size %d", t.Size())
	return 0
}

// extend makes reg the full 64-bit value of type t again after arithmetic on it
func (g *sbGen) extend(reg int16, t *types.Type) {
	if t.Size() < 8 {
		g.ins(sbLoadOp(t), sbR(reg), sbR(reg))
	}
}

// ---- the frame

func (g *sbGen) newSlot(t *types.Type) sbLoc {
	sz := t.Size()
	al := int64(t.Alignment())
	if al < 1 {
		al = 1
	}
	if sz == 0 {
		sz = 1
	}
	g.locals = types.RoundUp(g.locals+sz, al)
	if g.locals > 1<<16 {
		sbGiveUp("frame over 64 kB")
	}
	off := -g.locals
	g.slots = append(g.slots, sbSlot{off, t})
	return sbLoc{kind: sbAuto, off: off}
}

// temp: a one-word temporary for a value of type t. Pointer words and other words never share a slot.
func (g *sbGen) temp(t *types.Type) sbLoc {
	list := &g.freeInt
	wt := types.Types[types.TUINTPTR]
	if t.HasPointers() {
		list = &g.freePtr
		wt = types.Types[types.TUNSAFEPTR]
	}
	if n := len(*list); n > 0 {
		off := (*list)[n-1]
		*list = (*list)[:n-1]
		return sbLoc{kind: sbAuto, off: off}
	}
	return g.newSlot(wt)
}

func (g *sbGen) release(l sbLoc, t *types.Type) {
	if t.HasPointers() {
		g.freePtr = append(g.freePtr, l.off)
	} else {
		g.freeInt = append(g.freeInt, l.off)
	}
}

// ---- the function

func (g *sbGen) function() {
	fn := g.fn
	if fn.ABI != obj.ABIInternal {
		sbGiveUp("ABI0 function")
	}
	if fn.HasDefer() {
		sbGiveUp("defer")
	}
	if len(fn.ClosureVars) > 0 {
		sbGiveUp("closure variables")
	}
	if fn.Pragma&^(ir.Noinline) != 0 {
		sbGiveUp("pragma")
	}
	if fn.ABIWrapper() || fn.Needctxt() {
		sbGiveUp("ABI wrapper or needs context")
	}
	if base.Flag.Race || base.Flag.MSan || base.Flag.ASan || base.Flag.Cfg.Instrumenting {
		sbGiveUp("instrumented build")
	}
	if base.Flag.Dwarf {
		sbGiveUp("debug information wanted") // the DWARF generator reads the SSA back end's frame layout
	}
	g.abi = abiForFunc(fn, ssaConfig.ABI0, ssaConfig.ABI1).ABIAnalyze(fn.Type(), true)
	pp := g.pp
	pp.Pos = fn.Pos()

	// homes: register parameters in their ABI spill slots, results in the frame, autos in the frame
	type regParam struct {
		reg int16
		loc sbLoc
		t   *types.Type
	}
	var regParams []regParam
	argBits := bitvec.New(int32(types.RoundUp(g.abi.ArgWidth(), 8) / 8))     // at entry: the parameters
	argBitsBody := bitvec.New(int32(types.RoundUp(g.abi.ArgWidth(), 8) / 8)) // after the prologue: and the results that live on the stack
	for i := range g.abi.InParams() {
		pa := g.abi.InParam(i)
		off := pa.FrameOffset(g.abi)
		if pa.Type.Size() == 0 {
			continue
		}
		if pa.Type.HasPointers() {
			typebits.Set(pa.Type, off, argBits)
			typebits.Set(pa.Type, off, argBitsBody)
		}
		if pa.Name != nil {
			g.homes[pa.Name] = sbLoc{kind: sbParam, off: off}
		}
		if len(pa.Registers) == 0 {
			continue
		}
		ts, offs := pa.RegisterTypesAndOffsets()
		for j, r := range pa.Registers {
			if !sbScalar(ts[j]) {
				sbGiveUp("register parameter part of type %v", ts[j])
			}
			regParams = append(regParams, regParam{sbReg(r), sbLoc{kind: sbParam, off: off + offs[j]}, ts[j]})
		}
	}
	var stackResults []sbSlot
	for i := range g.abi.OutParams() {
		pa := g.abi.OutParam(i)
		var home sbLoc
		switch {
		case pa.Type.Size() == 0:
			home = g.newSlot(pa.Type)
		case len(pa.Registers) == 0:
			// a result the caller left room for: zeroed below, and live in the body's map from then on
			off := pa.FrameOffset(g.abi)
			home = sbLoc{kind: sbParam, off: off}
			stackResults = append(stackResults, sbSlot{off, pa.Type})
			if pa.Type.HasPointers() {
				typebits.Set(pa.Type, off, argBitsBody)
			}
		default:
			ts, _ := pa.RegisterTypesAndOffsets()
			for _, t := range ts {
				if !sbScalar(t) {
					sbGiveUp("result part of type %v", t)
				}
			}
			home = g.newSlot(pa.Type)
		}
		g.resHome = append(g.resHome, home)
		if pa.Name != nil {
			g.homes[pa.Name] = home
		}
	}
	// Variables whose address escapes live on the heap; the frame holds a pointer. For a local the object
	// is allocated where the variable is declared; for a parameter or a result, on entry (below).
	type heapParam struct {
		n   *ir.Name
		abi sbLoc
	}
	var heapParams []heapParam
	g.resHeap = make([]sbLoc, len(g.resHome))
	for _, n := range fn.Dcl {
		switch n.Class {
		case ir.PAUTO:
			if n.OnStack() {
				g.homes[n] = g.newSlot(n.Type())
			} else {
				g.homes[n] = g.heapHome()
			}
		case ir.PPARAM, ir.PPARAMOUT:
			home, ok := g.homes[n]
			if !ok {
				if n.Type().Size() != 0 && !ir.IsBlank(n) {
					sbGiveUp("parameter %v without an ABI home", n)
				}
				continue
			}
			if n.OnStack() || ir.IsBlank(n) {
				continue
			}
			g.homes[n] = g.heapHome()
			heapParams = append(heapParams, heapParam{n, home})
			if n.Class == ir.PPARAMOUT {
				for i := range g.resHome {
					if g.abi.OutParam(i).Name == n {
						g.resHeap[i] = g.homes[n]
					}
				}
			}
		default:
			sbGiveUp("variable of class %v", n.Class)
		}
	}

	// prologue (after the stack check the assembler adds): register parameters to their slots, then zero the frame
	for _, rp := range regParams {
		g.ins(sbStoreOp(rp.t), sbR(rp.reg), rp.loc.addr())
		// what the assembler does around the call to morestack: the same stores, and the loads back
		a := obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_SP, Offset: rp.loc.off + 8}
		g.spills = append(g.spills, obj.RegSpill{Reg: rp.reg, Addr: a, Spill: sbStoreOp(rp.t), Unspill: sbPlainLoad(rp.t)})
	}
	lea := g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_AUTO, Reg: x86.REG_SP}, sbR(x86.REG_DI))
	cnt := g.ins(x86.AMOVQ, sbImm(0), sbR(x86.REG_CX))
	g.ins(x86.AXORL, sbR(x86.REG_AX), sbR(x86.REG_AX))
	g.pp.Prog(x86.AREP)
	g.pp.Prog(x86.ASTOSQ)
	for _, r := range stackResults {
		g.zero(sbLoc{kind: sbParam, off: r.off}, r.t)
	}
	pp.NextLive = objw.StackMapIndex(1)
	pp.NextUnsafe = true
	for _, hp := range heapParams {
		g.newHeap(hp.n)
		if hp.n.Class == ir.PPARAM { // the value that arrived moves to the heap copy
			g.ins(x86.AMOVQ, sbR(x86.REG_AX), sbR(x86.REG_DI))
			g.copy(sbLoc{kind: sbInd, reg: x86.REG_DI}, hp.abi, hp.n.Type())
		}
	}

	g.stmts(fn.Body)
	// falling off the end of the body: return (a function with results always ends in a return statement)
	g.epilogue()

	// the frame is known now
	g.locals = types.RoundUp(g.locals, 8)
	lea.From.Offset = -g.locals
	cnt.From.Offset = g.locals / 8
	g.maxarg = types.RoundUp(g.maxarg, 8)
	frame := g.maxarg + g.locals
	if frame >= maxStackSize {
		sbGiveUp("frame too large")
	}
	pp.Text.To.Type = obj.TYPE_TEXTSIZE
	pp.Text.To.Val = int32(types.RoundUp(g.abi.ArgWidth(), int64(types.RegSize)))
	pp.Text.To.Offset = frame

	// stack maps: two identical entries each (index 0 is what the runtime uses at function entry, index 1
	// is in force everywhere after the prologue)
	locBits := bitvec.New(int32(g.locals / 8))
	for _, s := range g.slots {
		if s.t.HasPointers() {
			typebits.Set(s.t, g.locals+s.off, locBits)
		}
	}
	g.argsSym = sbStackMap(argBits, argBitsBody)
	g.localsSym = sbStackMap(locBits, locBits)
	// FUNCDATA must follow the TEXT directly
	fd := func(kind int64, sym *obj.LSym) *obj.Prog {
		p := pp.NewProg()
		p.As = obj.AFUNCDATA
		p.Pos = pp.Text.Pos
		p.From.SetConst(kind)
		p.To.Type = obj.TYPE_MEM
		p.To.Name = obj.NAME_EXTERN
		p.To.Sym = sym
		return p
	}
	a := fd(rtabi.FUNCDATA_ArgsPointerMaps, g.argsSym)
	l := fd(rtabi.FUNCDATA_LocalsPointerMaps, g.localsSym)
	a.Link, l.Link = l, pp.Text.Link
	pp.Text.Link = a
}

// sbPlainLoad: the load that undoes sbStoreOp (no extension needed: the register is defined by its width)
func sbPlainLoad(t *types.Type) obj.As {
	switch t.Size() {
	case 8:
		return x86.AMOVQ
	case 4:
		return x86.AMOVL
	case 2:
		return x86.AMOVWLZX
	}
	return x86.AMOVBLZX
}

// sbStackMap: a two-entry stack map: what is live at function entry, and what is live everywhere else
func sbStackMap(entry, body bitvec.BitVec) *obj.LSym {
	var tmp obj.LSym
	off := objw.Uint32(&tmp, 0, 2)
	off = objw.Uint32(&tmp, off, uint32(entry.N))
	off = objw.BitVec(&tmp, off, entry)
	objw.BitVec(&tmp, off, body)
	return base.Ctxt.GCLocalsSym(tmp.P)
}

// epilogue: results from their homes into the ABI's registers, and return
func (g *sbGen) epilogue() {
	for i := range g.abi.OutParams() { // a result that was moved to the heap comes back to where the caller looks
		if h := g.resHeap[i]; h.kind == sbHeapVar && g.abi.OutParam(i).Type.Size() != 0 {
			g.ins(x86.AMOVQ, sbLoc{kind: sbAuto, off: h.off}.addr(), sbR(x86.REG_SI))
			g.copy(g.resHome[i], sbLoc{kind: sbInd, reg: x86.REG_SI}, g.abi.OutParam(i).Type)
		}
	}
	for i := range g.abi.OutParams() {
		pa := g.abi.OutParam(i)
		if pa.Type.Size() == 0 || len(pa.Registers) == 0 {
			continue // nothing to move: a result on the stack is already where the caller looks
		}
		home := g.resHome[i]
		ts, offs := pa.RegisterTypesAndOffsets()
		for j, r := range pa.Registers {
			g.ins(sbLoadOp(ts[j]), home.plus(offs[j]).addr(), sbR(sbReg(r)))
		}
	}
	g.pp.Prog(obj.ARET)
}

// ---- statements

func (g *sbGen) stmts(l ir.Nodes) {
	for _, n := range l {
		g.stmt(n)
	}
}

func (g *sbGen) label(s *types.Sym) *sbLabel {
	l := g.labels[s]
	if l == nil {
		l = new(sbLabel)
		g.labels[s] = l
	}
	return l
}

func (g *sbGen) stmt(n ir.Node) {
	if n.Pos().IsKnown() {
		g.pp.Pos = n.Pos()
	}
	g.stmts(n.Init())
	switch n.Op() {
	case ir.OBLOCK:
		g.stmts(n.(*ir.BlockStmt).List)
	case ir.ODCL:
		v := n.(*ir.Decl).X
		if v.Class != ir.PAUTO {
			sbGiveUp("declaration of class %v", v.Class)
		}
		if !v.OnStack() {
			g.newHeap(v) // a fresh object each time the declaration is executed
		}
	case ir.OAS:
		n := n.(*ir.AssignStmt)
		g.assign(n.X, n.Y)
	case ir.OIF:
		n := n.(*ir.IfStmt)
		g.scalar(n.Cond)
		g.ins(x86.ATESTB, sbR(x86.REG_AX), sbR(x86.REG_AX))
		jElse := g.jump(x86.AJEQ)
		g.stmts(n.Body)
		if len(n.Else) == 0 {
			g.land(jElse)
			break
		}
		jEnd := g.jump(obj.AJMP)
		g.land(jElse)
		g.stmts(n.Else)
		g.land(jEnd)
	case ir.OFOR:
		n := n.(*ir.ForStmt)
		if n.DistinctVars {
			sbGiveUp("loop with per-iteration variables")
		}
		brk, cont := new(sbLabel), new(sbLabel)
		if n.Label != nil {
			// a labelled loop: `break L` and `continue L` arrive as branches to these
			g.labels[sbLoopSym(n.Label, "break")] = brk
			g.labels[sbLoopSym(n.Label, "continue")] = cont
		}
		top := g.here()
		var jOut *obj.Prog
		if n.Cond != nil {
			g.scalar(n.Cond)
			g.ins(x86.ATESTB, sbR(x86.REG_AX), sbR(x86.REG_AX))
			jOut = g.jump(x86.AJEQ)
		}
		g.brk, g.cont = append(g.brk, brk), append(g.cont, cont)
		g.stmts(n.Body)
		g.brk, g.cont = g.brk[:len(g.brk)-1], g.cont[:len(g.cont)-1]
		g.place(cont)
		if n.Post != nil {
			g.stmt(n.Post)
		}
		g.jump(obj.AJMP).To.SetTarget(top)
		if jOut != nil {
			g.land(jOut)
		}
		g.place(brk)
	case ir.OBREAK, ir.OCONTINUE:
		n := n.(*ir.BranchStmt)
		var l *sbLabel
		switch {
		case n.Label != nil && n.Op() == ir.OBREAK:
			l = g.labels[sbLoopSym(n.Label, "break")]
		case n.Label != nil:
			l = g.labels[sbLoopSym(n.Label, "continue")]
		case n.Op() == ir.OBREAK && len(g.brk) > 0:
			l = g.brk[len(g.brk)-1]
		case n.Op() == ir.OCONTINUE:
			for i := len(g.cont) - 1; i >= 0 && l == nil; i-- {
				l = g.cont[i]
			}
		}
		if l == nil {
			sbGiveUp("break or continue out of something that is not a for loop")
		}
		g.jumpTo(obj.AJMP, l)
	case ir.OGOTO:
		g.jumpTo(obj.AJMP, g.label(n.(*ir.BranchStmt).Label))
	case ir.OLABEL:
		g.place(g.label(n.(*ir.LabelStmt).Label))
	case ir.ORETURN:
		g.stmts(n.(*ir.ReturnStmt).Results)
		g.epilogue()
	case ir.OCALLFUNC, ir.OCALLINTER:
		g.callFunc(n.(*ir.CallExpr))
	case ir.OTAILCALL:
		// A wrapper's tail call, done as a call: the results are passed on. (The frame stays; the runtime
		// already knows how to skip wrapper frames.)
		call := n.(*ir.TailCallStmt).Call
		g.callFunc(call)
		for i := range g.abi.OutParams() {
			if t := g.abi.OutParam(i).Type; t.Size() != 0 {
				g.copy(g.resHome[i], g.lastRes[i], t)
			}
		}
		g.epilogue()
	case ir.OSWITCH:
		// walk has compiled the switch into tests and jumps; what is left to do is `break`
		n := n.(*ir.SwitchStmt)
		brk := new(sbLabel)
		if n.Label != nil {
			g.labels[sbLoopSym(n.Label, "break")] = brk
		}
		g.brk = append(g.brk, brk)
		g.cont = append(g.cont, nil) // a continue inside belongs to an enclosing loop
		g.stmts(n.Compiled)
		g.brk, g.cont = g.brk[:len(g.brk)-1], g.cont[:len(g.cont)-1]
		g.place(brk)
	case ir.OINLMARK:
		// with the function inlined here gone from tracebacks, nothing else changes
	case ir.OCHECKNIL:
		g.scalar(n.(*ir.UnaryExpr).X)
		g.ins(x86.ATESTB, sbR(x86.REG_AX), obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_AX})
	default:
		sbGiveUp("statement %v", n.Op())
	}
}

// sbLoopSym: a private key for the break/continue targets of a labelled loop
func sbLoopSym(label *types.Sym, what string) *types.Sym {
	return label.Pkg.Lookup(".sb." + what + "." + label.Name)
}

// assign: x = y; y == nil means the zero value
func (g *sbGen) assign(x, y ir.Node) {
	if ir.IsBlank(x) {
		if y != nil {
			g.value(y)
		}
		return
	}
	t := x.Type()
	if t.Size() == 0 {
		if y != nil {
			g.value(y)
		}
		return
	}
	if sbScalar(t) {
		if y == nil {
			g.ins(x86.AXORL, sbR(x86.REG_AX), sbR(x86.REG_AX))
		} else {
			g.scalar(y)
		}
		g.storeAX(x)
		return
	}
	if t.IsFloat() || t.IsComplex() {
		sbGiveUp("floating-point assignment")
	}
	if y == nil || sbIsZeroLit(y) {
		if y != nil {
			g.stmts(y.Init())
		}
		l := g.loc(x)
		if l.kind == sbInd {
			g.ins(x86.AMOVQ, sbR(x86.REG_AX), sbR(x86.REG_DI))
			l.reg = x86.REG_DI
		}
		g.zero(l, t)
		return
	}
	src := g.valueLoc(y)
	var keep sbLoc
	if src.kind == sbInd { // its base is in AX, which finding x's address may need
		keep = g.temp(types.Types[types.TUNSAFEPTR])
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), keep.addr())
	}
	dst := g.loc(x)
	if dst.kind == sbInd {
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), sbR(x86.REG_DI))
		dst.reg = x86.REG_DI
	}
	if src.kind == sbInd {
		g.ins(x86.AMOVQ, keep.addr(), sbR(x86.REG_SI))
		g.release(keep, types.Types[types.TUNSAFEPTR])
		src.reg = x86.REG_SI
	}
	g.copy(dst, src, t)
}

// sbIsZeroLit: after walk, a composite literal that is still in the tree is the zero value of its type
func sbIsZeroLit(n ir.Node) bool {
	switch n.Op() {
	case ir.ONIL, ir.OSTRUCTLIT, ir.OARRAYLIT, ir.OSLICELIT:
		return true
	}
	return false
}

// sbWords: for each 8-byte word of t, whether it is a pointer
func sbWords(t *types.Type) []bool {
	n := types.RoundUp(t.Size(), 8) / 8
	w := make([]bool, n)
	if t.HasPointers() {
		bv := bitvec.New(int32(n))
		typebits.Set(t, 0, bv)
		for i := range w {
			w[i] = bv.Get(int32(i))
		}
	}
	return w
}

// copy: *dst = *src for a value of type t. Bases, if any, must be in DI (dst) and SI (src). Uses R8, DX, R11.
func (g *sbGen) copy(dst, src sbLoc, t *types.Type) {
	if t.Size() > 2048 {
		sbGiveUp("copy of %d bytes", t.Size())
	}
	words := sbWords(t)
	barrier := !dst.inFrame()
	off := int64(0)
	for i := range words {
		if off+8 > t.Size() {
			break
		}
		g.ins(x86.AMOVQ, src.plus(off).addr(), sbR(x86.REG_R8))
		if words[i] && barrier {
			g.storeScalar(dst.plus(off), types.Types[types.TUNSAFEPTR], x86.REG_R8)
		} else {
			g.ins(x86.AMOVQ, sbR(x86.REG_R8), dst.plus(off).addr())
		}
		off += 8
	}
	for _, c := range []struct {
		n      int64
		ld, st obj.As
	}{{4, x86.AMOVL, x86.AMOVL}, {2, x86.AMOVWLZX, x86.AMOVW}, {1, x86.AMOVBLZX, x86.AMOVB}} {
		for off+c.n <= t.Size() {
			g.ins(c.ld, src.plus(off).addr(), sbR(x86.REG_R8))
			g.ins(c.st, sbR(x86.REG_R8), dst.plus(off).addr())
			off += c.n
		}
	}
}

// zero: *dst = the zero value of t. A base, if any, must be in DI. Uses R8, DX, R11.
func (g *sbGen) zero(dst sbLoc, t *types.Type) {
	if t.Size() > 2048 {
		sbGiveUp("zeroing %d bytes", t.Size())
	}
	words := sbWords(t)
	barrier := !dst.inFrame()
	g.ins(x86.AXORL, sbR(x86.REG_R8), sbR(x86.REG_R8))
	off := int64(0)
	for i := range words {
		if off+8 > t.Size() {
			break
		}
		if words[i] && barrier {
			g.storeScalar(dst.plus(off), types.Types[types.TUNSAFEPTR], x86.REG_R8)
		} else {
			g.ins(x86.AMOVQ, sbR(x86.REG_R8), dst.plus(off).addr())
		}
		off += 8
	}
	for _, c := range []struct {
		n  int64
		st obj.As
	}{{4, x86.AMOVL}, {2, x86.AMOVW}, {1, x86.AMOVB}} {
		for off+c.n <= t.Size() {
			g.ins(c.st, sbR(x86.REG_R8), dst.plus(off).addr())
			off += c.n
		}
	}
}

// valueLoc: a place from which n's value can be read. For something that lives in memory it is that
// memory (perhaps with its base in AX); anything else is evaluated into a fresh slot of the frame.
func (g *sbGen) valueLoc(n ir.Node) sbLoc {
	t := n.Type()
	switch n.Op() {
	case ir.ONAME:
		if n.(*ir.Name).Class != ir.PFUNC {
			g.stmts(n.Init())
			return g.resolve(g.nameLoc(n.(*ir.Name)))
		}
	case ir.ODEREF, ir.ODOTPTR, ir.ODOT, ir.OINDEX, ir.OLINKSYMOFFSET:
		if n.Op() != ir.OINDEX || !n.(*ir.IndexExpr).X.Type().IsString() {
			return g.loc(n)
		}
	case ir.OCONVNOP:
		if !sbScalar(t) {
			g.stmts(n.Init())
			return g.valueLoc(n.(*ir.ConvExpr).X) // same memory layout, by definition of this conversion
		}
	case ir.OCALLFUNC, ir.OCALLINTER:
		g.callFunc(n.(*ir.CallExpr))
		if len(g.lastRes) == 0 {
			sbGiveUp("value of a call without results")
		}
		return g.lastRes[0]
	case ir.ORESULT:
		g.stmts(n.Init())
		i := n.(*ir.ResultExpr).Index
		if int(i) >= len(g.lastRes) {
			sbGiveUp("result %d of a call that has %d", i, len(g.lastRes))
		}
		return g.lastRes[i]
	}
	if t.IsFloat() || t.IsComplex() {
		sbGiveUp("floating-point value")
	}
	slot := g.newSlot(t)
	if sbScalar(t) {
		g.scalar(n)
		g.ins(sbStoreOp(t), sbR(x86.REG_AX), slot.addr())
		return slot
	}
	g.stmts(n.Init())
	word := func(i int64, part ir.Node) { // evaluate a one-word part into the slot's word i
		g.scalar(part)
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), slot.plus(8*i).addr())
	}
	switch n.Op() {
	case ir.ONIL, ir.OSTRUCTLIT, ir.OARRAYLIT, ir.OSLICELIT:
		g.zero(slot, t) // the slot was zero at entry, but this may be the second time round a loop
	case ir.OLITERAL:
		if !t.IsString() {
			sbGiveUp("literal of type %v", t)
		}
		str := ir.StringVal(n)
		if str == "" {
			g.zero(slot, t)
			break
		}
		g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: staticdata.StringSym(n.Pos(), str)}, sbR(x86.REG_AX))
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), slot.addr())
		g.ins(x86.AMOVQ, sbImm(int64(len(str))), slot.plus(8).addr())
	case ir.OMAKEFACE:
		n := n.(*ir.BinaryExpr)
		// each part is parked in the slot as soon as it is known: the slot is typed, so a pointer waits safely
		word(0, n.X)
		word(1, n.Y)
	case ir.OSLICEHEADER:
		n := n.(*ir.SliceHeaderExpr)
		word(0, n.Ptr)
		word(1, n.Len)
		word(2, n.Cap)
	case ir.OSTRINGHEADER:
		n := n.(*ir.StringHeaderExpr)
		word(0, n.Ptr)
		word(1, n.Len)
	case ir.OSLICE, ir.OSLICEARR, ir.OSLICESTR, ir.OSLICE3, ir.OSLICE3ARR:
		g.slice(n.(*ir.SliceExpr), slot)
	default:
		sbGiveUp("value %v of type %v", n.Op(), t)
	}
	return slot
}

// storeAX stores the scalar in AX to the place x names. It may use every other register.
func (g *sbGen) storeAX(x ir.Node) {
	t := x.Type()
	if x.Op() == ir.ONAME {
		if l := g.nameLoc(x.(*ir.Name)); l.kind != sbHeapVar {
			g.storeScalar(l, t, x86.REG_AX)
			return
		}
	}
	// The address needs computing, which takes AX: park the value.
	tmp := g.temp(t)
	g.ins(x86.AMOVQ, sbR(x86.REG_AX), tmp.addr())
	l := g.loc(x) // base, if any, in AX
	g.ins(x86.AMOVQ, tmp.addr(), sbR(x86.REG_CX))
	g.release(tmp, t)
	g.storeScalar(l, t, x86.REG_CX)
}

// storeScalar: *l = reg, through the write barrier when the word is a pointer and l is not in this frame.
// Uses DX and R11; l's base register and reg are left alone until the store itself.
func (g *sbGen) storeScalar(l sbLoc, t *types.Type, reg int16) {
	if !t.HasPointers() || l.inFrame() {
		g.ins(sbStoreOp(t), sbR(reg), l.addr())
		return
	}
	wb := obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: ir.Syms.WriteBarrier}
	g.ins(x86.ACMPL, wb, sbImm(0))
	skip := g.jump(x86.AJEQ)
	g.call(ir.Syms.GCWriteBarrier[1]) // room for two pointers; R11 points at it; every other register is preserved
	g.ins(x86.AMOVQ, sbR(reg), obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_R11})
	g.ins(x86.AMOVQ, l.addr(), sbR(x86.REG_DX))
	g.ins(x86.AMOVQ, sbR(x86.REG_DX), obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_R11, Offset: 8})
	g.land(skip)
	g.ins(x86.AMOVQ, sbR(reg), l.addr())
}

func (g *sbGen) nameLoc(n *ir.Name) sbLoc {
	switch n.Class {
	case ir.PAUTO, ir.PPARAM, ir.PPARAMOUT:
		l, ok := g.homes[n]
		if !ok {
			sbGiveUp("variable %v has no home", n)
		}
		return l
	case ir.PEXTERN:
		return sbLoc{kind: sbGlobal, sym: n.Linksym()}
	}
	sbGiveUp("name of class %v", n.Class)
	return sbLoc{}
}

// loc computes where the addressable expression n lives. If the result is indirect its base is in AX.
func (g *sbGen) loc(n ir.Node) sbLoc {
	g.stmts(n.Init())
	switch n.Op() {
	case ir.ONAME:
		return g.resolve(g.nameLoc(n.(*ir.Name)))
	case ir.ODEREF:
		n := n.(*ir.StarExpr)
		g.scalar(n.X)
		return sbLoc{kind: sbInd, reg: x86.REG_AX}
	case ir.ODOTPTR:
		n := n.(*ir.SelectorExpr)
		g.scalar(n.X)
		if n.Offset() >= 4096 {
			g.ins(x86.ATESTB, sbR(x86.REG_AX), obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_AX}) // fault here, not somewhere far away
		}
		return sbLoc{kind: sbInd, reg: x86.REG_AX, off: n.Offset()}
	case ir.ODOT:
		n := n.(*ir.SelectorExpr)
		return g.valueLoc(n.X).plus(n.Offset()) // a field of something in memory, or of a value just computed into the frame
	case ir.OLINKSYMOFFSET:
		n := n.(*ir.LinksymOffsetExpr)
		return sbLoc{kind: sbGlobal, sym: n.Linksym, off: n.Offset_}
	case ir.OCONVNOP:
		n := n.(*ir.ConvExpr)
		if !sbScalar(n.Type()) {
			return g.loc(n.X)
		}
	case ir.OINDEX:
		n := n.(*ir.IndexExpr)
		g.index(n)
		return sbLoc{kind: sbInd, reg: x86.REG_AX}
	}
	sbGiveUp("address of %v", n.Op())
	return sbLoc{}
}

func sbRuntime(name string) *obj.LSym { return base.PkgLinksym("runtime", name, obj.ABIInternal) }

// boundsFail: call one of the runtime's bounds panics with x in BX and y in CX. Does not return.
func (g *sbGen) boundsFail(fn string, unsigned bool) {
	if unsigned {
		fn += "U"
	}
	g.ins(x86.AMOVQ, sbR(x86.REG_BX), sbR(x86.REG_AX))
	g.ins(x86.AMOVQ, sbR(x86.REG_CX), sbR(x86.REG_BX))
	g.call(sbRuntime(fn))
	g.pp.Prog(x86.AUD2)
}

// index leaves the address of n.X[n.Index] in AX, after the bounds check. n.X is a slice, an array or a string.
func (g *sbGen) index(n *ir.IndexExpr) {
	xt := n.X.Type()
	if !sbScalar(n.Index.Type()) {
		sbGiveUp("index of type %v", n.Index.Type())
	}
	g.scalar(n.Index)
	it := g.temp(types.Types[types.TINT])
	g.ins(x86.AMOVQ, sbR(x86.REG_AX), it.addr())
	var size int64
	switch {
	case xt.IsSlice() || xt.IsString():
		l := g.valueLoc(n.X)
		g.ins(x86.AMOVQ, l.plus(8).addr(), sbR(x86.REG_CX)) // length
		g.ins(x86.AMOVQ, l.addr(), sbR(x86.REG_DX))         // data
		size = 1
		if xt.IsSlice() {
			size = xt.Elem().Size()
		}
	case xt.IsArray():
		l := g.loc(n.X)
		if !(l.kind == sbInd && l.off == 0) {
			g.ins(x86.ALEAQ, l.addr(), sbR(x86.REG_AX))
		}
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), sbR(x86.REG_DX))
		g.ins(x86.AMOVQ, sbImm(xt.NumElem()), sbR(x86.REG_CX))
		size = xt.Elem().Size()
	default:
		sbGiveUp("index into %v", xt)
	}
	g.ins(x86.AMOVQ, it.addr(), sbR(x86.REG_BX))
	g.release(it, types.Types[types.TINT])
	if !n.Bounded() { // walk marks an index it has already proved (or that may be one past the end, for a copy of nothing)
		g.ins(x86.ACMPQ, sbR(x86.REG_BX), sbR(x86.REG_CX))
		ok := g.jump(x86.AJCS) // index < length, as unsigned numbers: a negative index is a huge one
		g.boundsFail("goPanicIndex", !n.Index.Type().IsSigned())
		g.land(ok)
	}
	switch size {
	case 0:
		g.ins(x86.AMOVQ, sbR(x86.REG_DX), sbR(x86.REG_AX))
	case 1, 2, 4, 8:
		g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_DX, Index: x86.REG_BX, Scale: int16(size)}, sbR(x86.REG_AX))
	default:
		p := g.ins(x86.AIMUL3Q, sbImm(size), sbR(x86.REG_BX))
		p.AddRestSourceReg(x86.REG_BX)
		g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_DX, Index: x86.REG_BX, Scale: 1}, sbR(x86.REG_AX))
	}
}

// slice evaluates the slice expression n into slot (a slice or string header in this frame).
func (g *sbGen) slice(n *ir.SliceExpr, slot sbLoc) {
	xt := n.X.Type()
	str := xt.IsString()
	// operand header -> three temporaries (pointer, length, capacity)
	tp := g.temp(types.Types[types.TUNSAFEPTR])
	tl := g.temp(types.Types[types.TINT])
	tc := g.temp(types.Types[types.TINT])
	var esize int64
	switch {
	case xt.IsSlice() || str:
		l := g.valueLoc(n.X)
		g.ins(x86.AMOVQ, l.addr(), sbR(x86.REG_DX))
		g.ins(x86.AMOVQ, l.plus(8).addr(), sbR(x86.REG_CX))
		if str {
			g.ins(x86.AMOVQ, sbR(x86.REG_CX), sbR(x86.REG_BX))
			esize = 1
		} else {
			g.ins(x86.AMOVQ, l.plus(16).addr(), sbR(x86.REG_BX))
			esize = xt.Elem().Size()
		}
	case xt.IsPtr() && xt.Elem().IsArray():
		g.scalar(n.X)
		g.ins(x86.ATESTB, sbR(x86.REG_AX), obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_AX}) // slicing a nil array pointer panics
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), sbR(x86.REG_DX))
		g.ins(x86.AMOVQ, sbImm(xt.Elem().NumElem()), sbR(x86.REG_CX))
		g.ins(x86.AMOVQ, sbR(x86.REG_CX), sbR(x86.REG_BX))
		esize = xt.Elem().Elem().Size()
	default:
		sbGiveUp("slice of %v", xt)
	}
	g.ins(x86.AMOVQ, sbR(x86.REG_DX), tp.addr())
	g.ins(x86.AMOVQ, sbR(x86.REG_CX), tl.addr())
	g.ins(x86.AMOVQ, sbR(x86.REG_BX), tc.addr())
	// bounds: i (default 0), j (default the length), k (default the capacity), each parked as soon as known
	bound := func(e ir.Node, deflt sbLoc) (sbLoc, bool) {
		if e == nil {
			return deflt, true
		}
		if !sbScalar(e.Type()) {
			sbGiveUp("slice bound of type %v", e.Type())
		}
		g.scalar(e)
		t := g.temp(types.Types[types.TINT])
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), t.addr())
		return t, e.Type().IsSigned()
	}
	zeroT := g.temp(types.Types[types.TINT])
	g.ins(x86.AMOVQ, sbImm(0), zeroT.addr())
	ti, isigned := bound(n.Low, zeroT)
	tj, jsigned := bound(n.High, tl)
	tk, ksigned := bound(n.Max, tc)
	three := n.Max != nil
	// checks: k <= cap, j <= k (or j <= cap when there is no k; j <= len for a string), i <= j; all unsigned
	check := func(x, y sbLoc, fn string, unsigned bool) {
		if n.Bounded() {
			return
		}
		g.ins(x86.AMOVQ, x.addr(), sbR(x86.REG_BX))
		g.ins(x86.AMOVQ, y.addr(), sbR(x86.REG_CX))
		g.ins(x86.ACMPQ, sbR(x86.REG_BX), sbR(x86.REG_CX))
		ok := g.jump(x86.AJLS) // x <= y unsigned
		g.boundsFail(fn, unsigned)
		g.land(ok)
	}
	if three {
		check(tk, tc, "goPanicSlice3Acap", !ksigned)
		check(tj, tk, "goPanicSlice3B", !jsigned)
		check(ti, tj, "goPanicSlice3C", !isigned)
	} else {
		if n.High != nil {
			if str {
				check(tj, tl, "goPanicSliceAlen", !jsigned)
			} else {
				check(tj, tc, "goPanicSliceAcap", !jsigned)
			}
		}
		check(ti, tj, "goPanicSliceB", !isigned)
	}
	// result: length j-i, capacity k-i, pointer p + i*size unless the new capacity (length, for a string) is
	// zero, when the pointer stays where it is so that it never points past the end of the object
	g.ins(x86.AMOVQ, ti.addr(), sbR(x86.REG_BX))
	g.ins(x86.AMOVQ, tj.addr(), sbR(x86.REG_CX))
	g.ins(x86.ASUBQ, sbR(x86.REG_BX), sbR(x86.REG_CX)) // new length
	g.ins(x86.AMOVQ, tk.addr(), sbR(x86.REG_SI))
	g.ins(x86.ASUBQ, sbR(x86.REG_BX), sbR(x86.REG_SI)) // new capacity
	g.ins(x86.AMOVQ, tp.addr(), sbR(x86.REG_DX))
	if str {
		g.ins(x86.ATESTQ, sbR(x86.REG_CX), sbR(x86.REG_CX))
	} else {
		g.ins(x86.ATESTQ, sbR(x86.REG_SI), sbR(x86.REG_SI))
	}
	stay := g.jump(x86.AJEQ)
	if esize != 1 {
		p := g.ins(x86.AIMUL3Q, sbImm(esize), sbR(x86.REG_BX))
		p.AddRestSourceReg(x86.REG_BX)
	}
	g.ins(x86.AADDQ, sbR(x86.REG_BX), sbR(x86.REG_DX))
	g.land(stay)
	g.ins(x86.AMOVQ, sbR(x86.REG_DX), slot.addr())
	g.ins(x86.AMOVQ, sbR(x86.REG_CX), slot.plus(8).addr())
	if !str {
		g.ins(x86.AMOVQ, sbR(x86.REG_SI), slot.plus(16).addr())
	}
	for _, t := range []sbLoc{ti, tj, tk} {
		if t != tl && t != tc && t != zeroT {
			g.release(t, types.Types[types.TINT])
		}
	}
	g.release(zeroT, types.Types[types.TINT])
	g.release(tc, types.Types[types.TINT])
	g.release(tl, types.Types[types.TINT])
	g.release(tp, types.Types[types.TUNSAFEPTR])
}

// ---- expressions

// value evaluates n for its effects and leaves a scalar result, if any, in AX
func (g *sbGen) value(n ir.Node) {
	if n.Op() == ir.OCALLFUNC || n.Op() == ir.OCALLINTER {
		g.callFunc(n.(*ir.CallExpr))
		if n.Type() != nil && sbScalar(n.Type()) && len(g.lastRes) > 0 {
			g.ins(sbLoadOp(n.Type()), g.lastRes[0].addr(), sbR(x86.REG_AX))
		}
		return
	}
	if !sbScalar(n.Type()) {
		g.valueLoc(n)
		return
	}
	g.scalar(n)
}

var sbSetcc = map[ir.Op][2]obj.As{ // [unsigned, signed]
	ir.OEQ: {x86.ASETEQ, x86.ASETEQ}, ir.ONE: {x86.ASETNE, x86.ASETNE},
	ir.OLT: {x86.ASETCS, x86.ASETLT}, ir.OLE: {x86.ASETLS, x86.ASETLE},
	ir.OGT: {x86.ASETHI, x86.ASETGT}, ir.OGE: {x86.ASETCC, x86.ASETGE},
}

var sbArith = map[ir.Op]obj.As{ir.OADD: x86.AADDQ, ir.OSUB: x86.ASUBQ, ir.OMUL: x86.AIMULQ, ir.OAND: x86.AANDQ, ir.OOR: x86.AORQ, ir.OXOR: x86.AXORQ}

// scalar evaluates n, a one-word value, into AX: the full 64-bit extension of its type. Every other
// register may be overwritten.
func (g *sbGen) scalar(n ir.Node) {
	t := n.Type()
	if !sbScalar(t) {
		sbGiveUp("%v of type %v as a scalar", n.Op(), t)
	}
	g.stmts(n.Init())
	switch n.Op() {
	case ir.ONAME:
		nn := n.(*ir.Name)
		if nn.Class == ir.PFUNC { // a function used as a value: the address of its closure object
			g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: staticdata.FuncLinksym(nn)}, sbR(x86.REG_AX))
			break
		}
		g.ins(sbLoadOp(t), g.resolve(g.nameLoc(nn)).addr(), sbR(x86.REG_AX))
	case ir.OLITERAL:
		v := n.Val()
		switch {
		case t.IsBoolean():
			b := int64(0)
			if ir.BoolVal(n) {
				b = 1
			}
			g.ins(x86.AMOVQ, sbImm(b), sbR(x86.REG_AX))
		case t.IsInteger() || t.IsUintptr():
			_ = v
			g.ins(x86.AMOVQ, sbImm(ir.IntVal(t, v)), sbR(x86.REG_AX))
			g.extend(x86.REG_AX, t)
		default:
			sbGiveUp("literal of type %v", t)
		}
	case ir.ONIL:
		g.ins(x86.AXORL, sbR(x86.REG_AX), sbR(x86.REG_AX))
	case ir.ODEREF, ir.ODOTPTR, ir.ODOT, ir.OLINKSYMOFFSET:
		l := g.loc(n)
		g.ins(sbLoadOp(t), l.addr(), sbR(x86.REG_AX))
	case ir.OINDEX:
		g.index(n.(*ir.IndexExpr))
		g.ins(sbLoadOp(t), obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_AX}, sbR(x86.REG_AX))
	case ir.OLEN, ir.OCAP:
		x := n.(*ir.UnaryExpr).X
		xt := x.Type()
		switch {
		case xt.IsString() || xt.IsSlice():
			off := int64(8)
			if n.Op() == ir.OCAP {
				off = 16
			}
			l := g.valueLoc(x)
			g.ins(x86.AMOVQ, l.plus(off).addr(), sbR(x86.REG_AX))
		case xt.IsMap() || xt.IsChan():
			// nil has length 0; otherwise the count is the first word (a channel's capacity is the second)
			g.scalar(x)
			g.ins(x86.ATESTQ, sbR(x86.REG_AX), sbR(x86.REG_AX))
			j := g.jump(x86.AJEQ)
			off := int64(0)
			if n.Op() == ir.OCAP {
				off = 8
			}
			g.ins(x86.AMOVQ, obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_AX, Offset: off}, sbR(x86.REG_AX))
			g.land(j)
		case xt.IsArray():
			g.ins(x86.AMOVQ, sbImm(xt.NumElem()), sbR(x86.REG_AX))
		default:
			sbGiveUp("len of %v", xt)
		}
	case ir.OSPTR, ir.OITAB:
		l := g.valueLoc(n.(*ir.UnaryExpr).X)
		g.ins(x86.AMOVQ, l.addr(), sbR(x86.REG_AX))
	case ir.OIDATA:
		l := g.valueLoc(n.(*ir.UnaryExpr).X)
		g.ins(x86.AMOVQ, l.plus(8).addr(), sbR(x86.REG_AX))
	case ir.OCFUNC:
		sym := n.(*ir.UnaryExpr).X.(*ir.Name).Linksym()
		if sym.ABI() != obj.ABIInternal {
			sbGiveUp("code pointer of an ABI0 function")
		}
		g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: sym}, sbR(x86.REG_AX))
	case ir.ONEW:
		n := n.(*ir.UnaryExpr)
		if n.X != nil && n.X.Op() == ir.ODYNAMICTYPE {
			sbGiveUp("new of a type known only at run time")
		}
		et := t.Elem()
		if et.Size() == 0 {
			g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: ir.Syms.Zerobase}, sbR(x86.REG_AX))
			break
		}
		g.ins(x86.ALEAQ, obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: reflectdata.TypeLinksym(et)}, sbR(x86.REG_AX))
		g.call(ir.Syms.Newobject)
	case ir.ODIV, ir.OMOD:
		n := n.(*ir.BinaryExpr)
		if !(t.IsInteger() || t.IsUintptr()) {
			sbGiveUp("division of %v", t)
		}
		g.binary(n.X, n.Y)
		g.ins(x86.ATESTQ, sbR(x86.REG_CX), sbR(x86.REG_CX))
		nz := g.jump(x86.AJNE)
		g.call(ir.Syms.Panicdivide)
		g.pp.Prog(x86.AUD2)
		g.land(nz)
		if t.IsSigned() {
			// x / -1 is -x and x % -1 is 0, also for the most negative x, where the machine instruction traps
			g.ins(x86.ACMPQ, sbR(x86.REG_CX), sbImm(-1))
			ne := g.jump(x86.AJNE)
			if n.Op() == ir.ODIV {
				g.ins1(x86.ANEGQ, sbR(x86.REG_AX))
			} else {
				g.ins(x86.AXORL, sbR(x86.REG_AX), sbR(x86.REG_AX))
			}
			done := g.jump(obj.AJMP)
			g.land(ne)
			g.pp.Prog(x86.ACQO)
			g.ins(x86.AIDIVQ, sbR(x86.REG_CX), obj.Addr{})
			if n.Op() == ir.OMOD {
				g.ins(x86.AMOVQ, sbR(x86.REG_DX), sbR(x86.REG_AX))
			}
			g.land(done)
		} else {
			g.ins(x86.AXORL, sbR(x86.REG_DX), sbR(x86.REG_DX))
			g.ins(x86.ADIVQ, sbR(x86.REG_CX), obj.Addr{})
			if n.Op() == ir.OMOD {
				g.ins(x86.AMOVQ, sbR(x86.REG_DX), sbR(x86.REG_AX))
			}
		}
		g.extend(x86.REG_AX, t)
	case ir.OLSH, ir.ORSH:
		n := n.(*ir.BinaryExpr)
		ct := n.Y.Type()
		if !(t.IsInteger() || t.IsUintptr()) || !(ct.IsInteger() || ct.IsUintptr()) {
			sbGiveUp("shift of %v by %v", t, ct)
		}
		g.binary(n.X, n.Y)
		if ct.IsSigned() {
			g.ins(x86.ATESTQ, sbR(x86.REG_CX), sbR(x86.REG_CX))
			nn := g.jump(x86.AJGE)
			g.call(ir.Syms.Panicshift)
			g.pp.Prog(x86.AUD2)
			g.land(nn)
		}
		// the machine uses the count modulo 64; Go wants zero (or all sign bits) from 64 up
		g.ins(x86.ACMPQ, sbR(x86.REG_CX), sbImm(64))
		small := g.jump(x86.AJCS)
		if n.Op() == ir.ORSH && t.IsSigned() {
			g.ins(x86.ASARQ, sbImm(63), sbR(x86.REG_AX))
		} else {
			g.ins(x86.AXORL, sbR(x86.REG_AX), sbR(x86.REG_AX))
		}
		done := g.jump(obj.AJMP)
		g.land(small)
		switch {
		case n.Op() == ir.OLSH:
			g.ins(x86.ASHLQ, sbR(x86.REG_CX), sbR(x86.REG_AX))
		case t.IsSigned():
			g.ins(x86.ASARQ, sbR(x86.REG_CX), sbR(x86.REG_AX))
		default:
			g.ins(x86.ASHRQ, sbR(x86.REG_CX), sbR(x86.REG_AX))
		}
		g.land(done)
		g.extend(x86.REG_AX, t)
	case ir.OADDR:
		n := n.(*ir.AddrExpr)
		l := g.loc(n.X)
		if l.kind == sbInd && l.off == 0 {
			break // the base is the address
		}
		g.ins(x86.ALEAQ, l.addr(), sbR(x86.REG_AX))
	case ir.OCONVNOP:
		n := n.(*ir.ConvExpr)
		if !sbScalar(n.X.Type()) || n.X.Type().Size() != t.Size() {
			sbGiveUp("conversion from %v to %v", n.X.Type(), t)
		}
		g.scalar(n.X)
		g.extend(x86.REG_AX, t) // same size, perhaps other signedness
	case ir.OCONV:
		n := n.(*ir.ConvExpr)
		ft := n.X.Type()
		if !(ft.IsInteger() || ft.IsUintptr() || ft.IsBoolean()) || !(t.IsInteger() || t.IsUintptr()) {
			if !(ft.IsUnsafePtr() || ft.IsUintptr() || ft.IsPtr()) || !(t.IsUnsafePtr() || t.IsUintptr() || t.IsPtr()) {
				sbGiveUp("conversion from %v to %v", ft, t)
			}
		}
		g.scalar(n.X) // the extension of the source type: narrowing keeps the low bits, widening is done
		g.extend(x86.REG_AX, t)
	case ir.ONOT:
		g.scalar(n.(*ir.UnaryExpr).X)
		g.ins(x86.AXORL, sbImm(1), sbR(x86.REG_AX))
	case ir.ONEG:
		g.scalar(n.(*ir.UnaryExpr).X)
		g.ins1(x86.ANEGQ, sbR(x86.REG_AX))
		g.extend(x86.REG_AX, t)
	case ir.OBITNOT:
		g.scalar(n.(*ir.UnaryExpr).X)
		g.ins1(x86.ANOTQ, sbR(x86.REG_AX))
		g.extend(x86.REG_AX, t)
	case ir.OANDAND, ir.OOROR:
		n := n.(*ir.LogicalExpr)
		g.scalar(n.X)
		g.ins(x86.ATESTB, sbR(x86.REG_AX), sbR(x86.REG_AX))
		var j *obj.Prog
		if n.Op() == ir.OANDAND {
			j = g.jump(x86.AJEQ)
		} else {
			j = g.jump(x86.AJNE)
		}
		g.scalar(n.Y)
		g.land(j)
	case ir.OEQ, ir.ONE, ir.OLT, ir.OLE, ir.OGT, ir.OGE:
		n := n.(*ir.BinaryExpr)
		ot := n.X.Type()
		if (n.Op() == ir.OEQ || n.Op() == ir.ONE) && (ir.IsNil(n.X) || ir.IsNil(n.Y)) && (ot.IsSlice() || ot.IsInterface()) {
			// against nil a slice is its data pointer and an interface its type word
			other := n.X
			if ir.IsNil(other) {
				other = n.Y
			}
			l := g.valueLoc(other)
			g.ins(x86.AMOVQ, l.addr(), sbR(x86.REG_AX))
			g.ins(x86.ATESTQ, sbR(x86.REG_AX), sbR(x86.REG_AX))
			g.ins1(sbSetcc[n.Op()][0], sbR(x86.REG_AX))
			g.ins(x86.AMOVBQZX, sbR(x86.REG_AX), sbR(x86.REG_AX))
			break
		}
		if !sbScalar(ot) || !sbScalar(n.Y.Type()) {
			sbGiveUp("comparison of %v", ot)
		}
		g.binary(n.X, n.Y) // X in AX, Y in CX
		g.ins(x86.ACMPQ, sbR(x86.REG_AX), sbR(x86.REG_CX))
		signed := 0
		if ot.IsInteger() && ot.IsSigned() {
			signed = 1
		}
		g.ins1(sbSetcc[n.Op()][signed], sbR(x86.REG_AX))
		g.ins(x86.AMOVBQZX, sbR(x86.REG_AX), sbR(x86.REG_AX))
	case ir.OADD, ir.OSUB, ir.OMUL, ir.OAND, ir.OOR, ir.OXOR:
		n := n.(*ir.BinaryExpr)
		if !(t.IsInteger() || t.IsUintptr()) {
			sbGiveUp("arithmetic on %v", t)
		}
		g.binary(n.X, n.Y)
		g.ins(sbArith[n.Op()], sbR(x86.REG_CX), sbR(x86.REG_AX))
		g.extend(x86.REG_AX, t)
	case ir.OANDNOT:
		n := n.(*ir.BinaryExpr)
		g.binary(n.X, n.Y)
		g.ins1(x86.ANOTQ, sbR(x86.REG_CX))
		g.ins(x86.AANDQ, sbR(x86.REG_CX), sbR(x86.REG_AX))
		g.extend(x86.REG_AX, t)
	case ir.OCALLFUNC, ir.OCALLINTER:
		g.value(n)
	case ir.ORESULT:
		n := n.(*ir.ResultExpr)
		if int(n.Index) >= len(g.lastRes) {
			sbGiveUp("result %d of a call that has %d", n.Index, len(g.lastRes))
		}
		g.ins(sbLoadOp(t), g.lastRes[n.Index].addr(), sbR(x86.REG_AX))
	default:
		sbGiveUp("expression %v", n.Op())
	}
}

// binary evaluates x into AX and y into CX
func (g *sbGen) binary(x, y ir.Node) {
	g.scalar(x)
	tmp := g.temp(x.Type())
	g.ins(x86.AMOVQ, sbR(x86.REG_AX), tmp.addr())
	g.scalar(y)
	g.ins(x86.AMOVQ, sbR(x86.REG_AX), sbR(x86.REG_CX))
	g.ins(x86.AMOVQ, tmp.addr(), sbR(x86.REG_AX))
	g.release(tmp, x.Type())
}

// ---- calls

// callFunc: a call of a Go function (by name, through a function value, or through an interface). Every
// argument is evaluated into the frame first, then the registers and the outgoing stack area are loaded.
// Results go to fresh slots of the frame, recorded in g.lastRes.
func (g *sbGen) callFunc(n *ir.CallExpr) {
	g.stmts(n.Init())
	if len(n.KeepAlive) > 0 {
		sbGiveUp("call with keepalive arguments")
	}
	if n.Op() == ir.OCALLFUNC && ir.IsIntrinsicCall != nil && ir.IsIntrinsicCall(n) {
		sbGiveUp("intrinsic call")
	}
	ft := n.Fun.Type()
	info := ssaConfig.ABI1.ABIAnalyze(ft, false)
	var target *obj.LSym // a call by name
	var code sbLoc       // otherwise: the slot holding the code pointer
	var clo sbLoc        // and for a function value, the slot holding the closure pointer (for DX)
	hasClo := false
	args := []ir.Node(n.Args)
	first := 0 // parameters before args[0]: the receiver of an interface call
	var rcvr sbLoc
	switch {
	case n.Op() == ir.OCALLINTER:
		sel, ok := n.Fun.(*ir.SelectorExpr)
		if !ok || sel.Op() != ir.ODOTINTER {
			sbGiveUp("interface call through %v", n.Fun.Op())
		}
		l := g.valueLoc(sel.X)
		// the method's code is in the interface's table; a nil interface faults on the load
		g.ins(x86.AMOVQ, l.plus(8).addr(), sbR(x86.REG_CX))
		g.ins(x86.AMOVQ, l.addr(), sbR(x86.REG_AX))
		rcvr = g.temp(types.Types[types.TUNSAFEPTR])
		g.ins(x86.AMOVQ, sbR(x86.REG_CX), rcvr.addr())
		g.ins(x86.AMOVQ, obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_AX, Offset: sel.Offset() + rttype.ITab.OffsetOf("Fun")}, sbR(x86.REG_AX))
		code = g.temp(types.Types[types.TUINTPTR])
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), code.addr())
		first = 1
	case n.Fun.Op() == ir.ONAME && n.Fun.(*ir.Name).Class == ir.PFUNC:
		callee := n.Fun.(*ir.Name)
		if callee.Func != nil && callee.Func.ABI != obj.ABIInternal {
			sbGiveUp("call of an ABI0 function")
		}
		target = callee.LinksymABI(obj.ABIInternal)
	default:
		// a function value: a pointer to a closure object whose first word is the code
		if n.Fun.Type().Kind() != types.TFUNC {
			sbGiveUp("call through %v", n.Fun.Op())
		}
		g.scalar(n.Fun)
		clo = g.temp(types.Types[types.TUNSAFEPTR])
		hasClo = true
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), clo.addr())
	}
	if ft.NumRecvs() != 0 && first == 0 {
		sbGiveUp("method call not lowered")
	}
	if len(args)+first != len(info.InParams()) {
		sbGiveUp("call with %d arguments for %d parameters", len(args), len(info.InParams()))
	}
	// evaluate
	locs := make([]sbLoc, len(info.InParams()))
	for i := range info.InParams() {
		pa := info.InParam(i)
		if pa.Type.Size() == 0 {
			if i >= first {
				g.value(args[i-first])
			}
			continue
		}
		if i < first {
			locs[i] = rcvr
			continue
		}
		a := args[i-first]
		l := g.valueLoc(a)
		if l.kind == sbInd {
			// its base is in AX, which evaluating the next argument will use. (A variable's own home can be
			// read at call time: walk has moved everything with side effects out of the argument list.)
			slot := g.newSlot(pa.Type)
			g.ins(x86.AMOVQ, sbR(x86.REG_AX), sbR(x86.REG_SI))
			l.reg = x86.REG_SI
			g.copy(slot, l, pa.Type)
			l = slot
		}
		locs[i] = l
	}
	// stack arguments, then register arguments
	for i := range info.InParams() {
		pa := info.InParam(i)
		if pa.Type.Size() == 0 || len(pa.Registers) != 0 {
			continue
		}
		g.copy(sbLoc{kind: sbInd, reg: x86.REG_SP, off: pa.FrameOffset(info)}.frame(), locs[i], pa.Type)
	}
	for i := range info.InParams() {
		pa := info.InParam(i)
		if pa.Type.Size() == 0 || len(pa.Registers) == 0 {
			continue
		}
		ts, offs := pa.RegisterTypesAndOffsets()
		for j, r := range pa.Registers {
			if !sbScalar(ts[j]) {
				sbGiveUp("argument part of type %v", ts[j])
			}
			g.ins(sbLoadOp(ts[j]), locs[i].plus(offs[j]).addr(), sbR(sbReg(r)))
		}
	}
	if w := info.ArgWidth(); w > g.maxarg {
		g.maxarg = w
	}
	switch {
	case target != nil:
		g.call(target)
	case hasClo:
		g.ins(x86.AMOVQ, clo.addr(), sbR(x86.REG_DX))
		g.release(clo, types.Types[types.TUNSAFEPTR])
		p := g.pp.Prog(obj.ACALL)
		p.To = obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_DX} // a nil function value faults here
	default:
		g.ins(x86.AMOVQ, code.addr(), sbR(x86.REG_R12))
		g.release(code, types.Types[types.TUINTPTR])
		g.release(rcvr, types.Types[types.TUNSAFEPTR])
		p := g.pp.Prog(obj.ACALL)
		p.To = sbR(x86.REG_R12)
	}
	// results
	g.lastRes = g.lastRes[:0]
	for i := range info.OutParams() {
		pa := info.OutParam(i)
		if pa.Type.Size() == 0 {
			g.lastRes = append(g.lastRes, sbLoc{})
			continue
		}
		l := g.newSlot(pa.Type)
		g.lastRes = append(g.lastRes, l)
		if len(pa.Registers) == 0 {
			continue // copied below, once the registers are safe
		}
		ts, offs := pa.RegisterTypesAndOffsets()
		for j, r := range pa.Registers {
			if !sbScalar(ts[j]) {
				sbGiveUp("result part of type %v", ts[j])
			}
			g.ins(sbStoreOp(ts[j]), sbR(sbReg(r)), l.plus(offs[j]).addr())
		}
	}
	for i := range info.OutParams() {
		pa := info.OutParam(i)
		if pa.Type.Size() != 0 && len(pa.Registers) == 0 {
			g.copy(g.lastRes[i], sbLoc{kind: sbInd, reg: x86.REG_SP, off: pa.FrameOffset(info)}, pa.Type)
		}
	}
}

// frame marks an SP-relative location as part of this frame (no write barrier for stores into it)
func (l sbLoc) frame() sbLoc { l.kind = sbOut; return l }
