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
	}
	return obj.Addr{Type: obj.TYPE_MEM, Reg: l.reg, Offset: l.off}
}

func (l sbLoc) plus(off int64) sbLoc { l.off += off; return l }

// inFrame: a store here needs no write barrier
func (l sbLoc) inFrame() bool { return l.kind == sbAuto || l.kind == sbParam }

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
	if fn.Wrapper() || fn.ABIWrapper() || fn.Needctxt() {
		sbGiveUp("wrapper or needs context")
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
	argBits := bitvec.New(int32(types.RoundUp(g.abi.ArgWidth(), 8) / 8))
	for i := range g.abi.InParams() {
		pa := g.abi.InParam(i)
		off := pa.FrameOffset(g.abi)
		if pa.Type.Size() == 0 {
			continue
		}
		if pa.Type.HasPointers() {
			typebits.Set(pa.Type, off, argBits)
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
	for i := range g.abi.OutParams() {
		pa := g.abi.OutParam(i)
		if len(pa.Registers) == 0 && pa.Type.Size() != 0 {
			sbGiveUp("result on the stack")
		}
		if pa.Name != nil {
			g.homes[pa.Name] = g.newSlot(pa.Type)
		}
	}
	for _, n := range fn.Dcl {
		switch n.Class {
		case ir.PAUTO:
			g.homes[n] = g.newSlot(n.Type())
		case ir.PPARAM, ir.PPARAMOUT:
			if _, ok := g.homes[n]; !ok && n.Type().Size() != 0 {
				sbGiveUp("parameter %v without an ABI home", n)
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
	pp.NextLive = objw.StackMapIndex(1)
	pp.NextUnsafe = true

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
	g.argsSym = sbStackMap(argBits)
	g.localsSym = sbStackMap(locBits)
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

func sbStackMap(bv bitvec.BitVec) *obj.LSym {
	var tmp obj.LSym
	off := objw.Uint32(&tmp, 0, 2)
	off = objw.Uint32(&tmp, off, uint32(bv.N))
	off = objw.BitVec(&tmp, off, bv)
	objw.BitVec(&tmp, off, bv)
	return base.Ctxt.GCLocalsSym(tmp.P)
}

// epilogue: results from their homes into the ABI's registers, and return
func (g *sbGen) epilogue() {
	for i := range g.abi.OutParams() {
		pa := g.abi.OutParam(i)
		if pa.Type.Size() == 0 {
			continue
		}
		var home sbLoc
		if pa.Name != nil {
			home = g.homes[pa.Name]
		} else {
			sbGiveUp("unnamed result")
		}
		ts, offs := pa.RegisterTypesAndOffsets()
		for j, r := range pa.Registers {
			if !sbScalar(ts[j]) {
				sbGiveUp("result part of type %v", ts[j])
			}
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
		case n.Op() == ir.OCONTINUE && len(g.cont) > 0:
			l = g.cont[len(g.cont)-1]
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
	case ir.OCALLFUNC:
		g.callFunc(n.(*ir.CallExpr))
	default:
		sbGiveUp("statement %v", n.Op())
	}
}

// sbLoopSym: a private key for the break/continue targets of a labelled loop
func sbLoopSym(label *types.Sym, what string) *types.Sym {
	return label.Pkg.Lookup(".sb." + what + "." + label.Name)
}

// assign: x = y for a scalar x; y == nil means the zero value
func (g *sbGen) assign(x, y ir.Node) {
	if ir.IsBlank(x) {
		if y != nil {
			g.value(y)
		}
		return
	}
	t := x.Type()
	if !sbScalar(t) {
		sbGiveUp("assignment of type %v", t)
	}
	if y == nil {
		g.ins(x86.AXORL, sbR(x86.REG_AX), sbR(x86.REG_AX))
	} else {
		g.scalar(y)
	}
	g.storeAX(x)
}

// storeAX stores the scalar in AX to the place x names. It may use every other register.
func (g *sbGen) storeAX(x ir.Node) {
	t := x.Type()
	if x.Op() == ir.ONAME {
		l := g.nameLoc(x.(*ir.Name))
		g.storeScalar(l, t, x86.REG_AX)
		return
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
		return g.nameLoc(n.(*ir.Name))
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
		return g.loc(n.X).plus(n.Offset())
	}
	sbGiveUp("address of %v", n.Op())
	return sbLoc{}
}

// ---- expressions

// value evaluates n for its effects and leaves a scalar result, if any, in AX
func (g *sbGen) value(n ir.Node) {
	if n.Op() == ir.OCALLFUNC {
		g.callFunc(n.(*ir.CallExpr))
		if n.Type() != nil && sbScalar(n.Type()) {
			g.ins(sbLoadOp(n.Type()), g.lastRes[0].addr(), sbR(x86.REG_AX))
		}
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
		if nn.Class == ir.PFUNC {
			sbGiveUp("function value")
		}
		g.ins(sbLoadOp(t), g.nameLoc(nn).addr(), sbR(x86.REG_AX))
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
	case ir.ODEREF, ir.ODOTPTR, ir.ODOT:
		l := g.loc(n)
		g.ins(sbLoadOp(t), l.addr(), sbR(x86.REG_AX))
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
	case ir.OCALLFUNC:
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

// callFunc: a static call to a Go function. Results go to fresh slots, recorded in g.lastRes.
func (g *sbGen) callFunc(n *ir.CallExpr) {
	g.stmts(n.Init())
	if len(n.KeepAlive) > 0 {
		sbGiveUp("call with keepalive arguments")
	}
	if ir.IsIntrinsicCall != nil && ir.IsIntrinsicCall(n) {
		sbGiveUp("intrinsic call")
	}
	callee, ok := n.Fun.(*ir.Name)
	if !ok || callee.Op() != ir.ONAME || callee.Class != ir.PFUNC {
		sbGiveUp("call through %v", n.Fun.Op())
	}
	if callee.Func != nil && callee.Func.ABI != obj.ABIInternal {
		sbGiveUp("call of an ABI0 function")
	}
	ft := n.Fun.Type()
	if ft.NumRecvs() != 0 {
		sbGiveUp("method call not lowered")
	}
	info := ssaConfig.ABI1.ABIAnalyze(ft, false)
	if len(n.Args) != len(info.InParams()) {
		sbGiveUp("call with %d arguments for %d parameters", len(n.Args), len(info.InParams()))
	}
	// evaluate every argument into a temporary first: evaluating one may use any register
	type argTmp struct {
		tmp sbLoc
		t   *types.Type
	}
	tmps := make([]argTmp, len(n.Args))
	for i, a := range n.Args {
		pa := info.InParam(i)
		if !sbScalar(pa.Type) || !sbScalar(a.Type()) {
			sbGiveUp("argument of type %v", pa.Type)
		}
		g.scalar(a)
		tmps[i] = argTmp{g.temp(pa.Type), pa.Type}
		g.ins(x86.AMOVQ, sbR(x86.REG_AX), tmps[i].tmp.addr())
	}
	for i := range n.Args {
		pa := info.InParam(i)
		if len(pa.Registers) == 0 {
			off := pa.FrameOffset(info)
			g.ins(x86.AMOVQ, tmps[i].tmp.addr(), sbR(x86.REG_R12))
			g.ins(sbStoreOp(pa.Type), sbR(x86.REG_R12), obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_SP, Offset: off})
		}
	}
	for i := range n.Args {
		pa := info.InParam(i)
		if len(pa.Registers) == 1 {
			g.ins(x86.AMOVQ, tmps[i].tmp.addr(), sbR(sbReg(pa.Registers[0])))
		}
	}
	for i := range tmps {
		g.release(tmps[i].tmp, tmps[i].t)
	}
	if w := info.ArgWidth(); w > g.maxarg {
		g.maxarg = w
	}
	g.call(callee.LinksymABI(obj.ABIInternal))
	// results
	g.lastRes = g.lastRes[:0]
	for i := range info.OutParams() {
		pa := info.OutParam(i)
		if pa.Type.Size() == 0 {
			g.lastRes = append(g.lastRes, sbLoc{})
			continue
		}
		if len(pa.Registers) != 1 || !sbScalar(pa.Type) {
			sbGiveUp("call result of type %v", pa.Type)
		}
		l := g.newSlot(pa.Type)
		g.ins(sbStoreOp(pa.Type), sbR(sbReg(pa.Registers[0])), l.addr())
		g.lastRes = append(g.lastRes, l)
	}
}
