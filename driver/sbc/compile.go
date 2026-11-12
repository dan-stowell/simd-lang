// compile.go: the functions sbc takes, and the code it writes for them.
//
// Version 0 takes frameless leaf functions: a body that is a sequence of `if cond { return e }` ending in
// `return e`, one result, where parameters and result each fit one integer register and every expression
// is made of parameters, constants, field loads through pointers, integer arithmetic, comparisons and
// short-circuit boolean operators. No calls, no stack, no allocation, no write barrier: the generated
// function is NOSPLIT|NOFRAME with no frame and needs no stack map. A nil pointer faults in the first
// page and the runtime raises the usual panic, exactly as for the stock compiler's code.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cmd/internal/obj"
	"cmd/internal/obj/x86"
	"cmd/internal/objabi"
	"cmd/internal/src"
)

type Pkg struct {
	ImportPath, Dir, Name string
	GoFiles               []string
	Imports               []string
	ImportMap             map[string]string
	Standard              bool
	Module                *struct{ Path, Version, GoVersion string }

	idx     int
	files   []*ast.File
	cands   []*ast.FuncDecl
	tpkg    *types.Package
	info    *types.Info
	deps    []*Pkg
	users   []*Pkg
	waiting int32
}

var (
	fset   = token.NewFileSet()
	byPath = map[string]*Pkg{}
	sizes  = types.SizesFor("gc", "amd64")
)

type importer struct{ p *Pkg }

func (i importer) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if m, ok := i.p.ImportMap[path]; ok {
		path = m
	}
	d := byPath[path]
	if d == nil || d.tpkg == nil {
		return nil, fmt.Errorf("%s: import %s not ready", i.p.ImportPath, path)
	}
	return d.tpkg, nil
}

// Packages sbc leaves alone: the runtime and what it is built on have rules of their own (nosplit chains,
// write-barrier-free code, functions the compiler or the linker know by name).
func excluded(p *Pkg) bool {
	if !p.Standard {
		return false
	}
	ip := p.ImportPath
	return ip == "runtime" || strings.HasPrefix(ip, "runtime/") || strings.HasPrefix(ip, "internal/") ||
		ip == "reflect" || ip == "sync" || strings.HasPrefix(ip, "sync/") || ip == "syscall" || ip == "unsafe" ||
		strings.HasPrefix(ip, "vendor/")
}

// ---- which functions could qualify, judged on syntax alone (before type checking)

func exprOK(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return exprOK(x.X)
	case *ast.SelectorExpr:
		return exprOK(x.X)
	case *ast.StarExpr:
		return exprOK(x.X)
	case *ast.UnaryExpr:
		return (x.Op == token.NOT || x.Op == token.SUB || x.Op == token.XOR) && exprOK(x.X)
	case *ast.BinaryExpr:
		switch x.Op {
		case token.ADD, token.SUB, token.MUL, token.AND, token.OR, token.XOR, token.AND_NOT,
			token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ, token.LAND, token.LOR:
			return exprOK(x.X) && exprOK(x.Y)
		}
	case *ast.CallExpr: // only a conversion will pass the typed check
		return len(x.Args) == 1 && !x.Ellipsis.IsValid() && exprOK(x.Fun) && exprOK(x.Args[0])
	}
	return false
}

func candidate(fd *ast.FuncDecl) bool {
	if fd.Body == nil || fd.Type.TypeParams != nil || fd.Name.Name == "_" || fd.Name.Name == "init" || fd.Name.Name == "main" {
		return false
	}
	if fd.Doc != nil {
		for _, c := range fd.Doc.List {
			if strings.HasPrefix(c.Text, "//go:") || strings.HasPrefix(c.Text, "//line") {
				return false
			}
		}
	}
	if fd.Type.Results == nil || len(fd.Type.Results.List) != 1 || len(fd.Type.Results.List[0].Names) != 0 {
		return false
	}
	n := len(fd.Body.List)
	if n == 0 {
		return false
	}
	for i, s := range fd.Body.List {
		switch x := s.(type) {
		case *ast.ReturnStmt:
			if i != n-1 || len(x.Results) != 1 || !exprOK(x.Results[0]) {
				return false
			}
		case *ast.IfStmt:
			if i == n-1 || x.Init != nil || x.Else != nil || !exprOK(x.Cond) || len(x.Body.List) != 1 {
				return false
			}
			r, ok := x.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(r.Results) != 1 || !exprOK(r.Results[0]) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ---- code generation

type unsupported string

func bail(format string, a ...any) { panic(unsupported(fmt.Sprintf(format, a...))) }

// intClass: the width in bytes and signedness of a type that lives in one integer register; ok is false
// for anything else. Pointers and bools are unsigned.
func intClass(t types.Type) (width int, signed, ok bool) {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Bool, types.UntypedBool: // a comparison's result is an untyped bool until something gives it a type
			return 1, false, true
		case types.Int8:
			return 1, true, true
		case types.Uint8:
			return 1, false, true
		case types.Int16:
			return 2, true, true
		case types.Uint16:
			return 2, false, true
		case types.Int32:
			return 4, true, true
		case types.Uint32:
			return 4, false, true
		case types.Int, types.Int64:
			return 8, true, true
		case types.Uint, types.Uint64, types.Uintptr, types.UnsafePointer:
			return 8, false, true
		}
	case *types.Pointer:
		return 8, false, true
	}
	return 0, false, false
}

var argRegs = []int16{x86.REG_AX, x86.REG_BX, x86.REG_CX, x86.REG_DI, x86.REG_SI, x86.REG_R8, x86.REG_R9, x86.REG_R10, x86.REG_R11}

type fgen struct {
	e      *emitter
	info   *types.Info
	params map[types.Object]int16
	free   []int16 // registers that hold nothing
	fixups []*obj.Prog
}

func (g *fgen) alloc() int16 {
	if len(g.free) == 0 {
		bail("out of registers")
	}
	r := g.free[len(g.free)-1]
	g.free = g.free[:len(g.free)-1]
	return r
}

func (g *fgen) release(r int16) {
	for _, pr := range g.params {
		if pr == r {
			return // a parameter's register is never ours to give away
		}
	}
	g.free = append(g.free, r)
}

// owned makes sure r may be overwritten: a parameter's register is copied to a fresh one first.
func (g *fgen) owned(r int16) int16 {
	for _, pr := range g.params {
		if pr == r {
			t := g.alloc()
			g.e.ins(x86.AMOVQ, reg(r), reg(t))
			return t
		}
	}
	return r
}

// extend rewrites r as the 64-bit value of a width-byte integer (after arithmetic that may have carried
// into the upper bits, or for a narrow parameter, whose upper bits the ABI leaves undefined).
func (g *fgen) extend(r int16, width int, signed bool) {
	var as obj.As
	switch {
	case width == 8:
		return
	case width == 4 && signed:
		as = x86.AMOVLQSX
	case width == 4:
		as = x86.AMOVL // a 32-bit move clears the upper half
	case width == 2 && signed:
		as = x86.AMOVWQSX
	case width == 2:
		as = x86.AMOVWQZX
	case width == 1 && signed:
		as = x86.AMOVBQSX
	default:
		as = x86.AMOVBQZX
	}
	g.e.ins(as, reg(r), reg(r))
}

func loadOp(width int, signed bool) obj.As {
	switch {
	case width == 8:
		return x86.AMOVQ
	case width == 4 && signed:
		return x86.AMOVLQSX
	case width == 4:
		return x86.AMOVL
	case width == 2 && signed:
		return x86.AMOVWQSX
	case width == 2:
		return x86.AMOVWQZX
	case width == 1 && signed:
		return x86.AMOVBQSX
	}
	return x86.AMOVBQZX
}

func (g *fgen) jump(as obj.As) *obj.Prog {
	return g.e.add(&obj.Prog{As: as, To: obj.Addr{Type: obj.TYPE_BRANCH}})
}

func (g *fgen) land(ps ...*obj.Prog) {
	nop := g.e.add(&obj.Prog{As: obj.ANOP})
	for _, p := range ps {
		p.To.SetTarget(nop)
	}
}

// expr evaluates e into a register it returns. The value is the full 64-bit extension of e's type. The
// register is either a parameter's (read only) or one the caller must release.
func (g *fgen) expr(e ast.Expr) int16 {
	tv, ok := g.info.Types[e]
	if !ok {
		bail("untyped expression")
	}
	if tv.IsType() {
		bail("type as value")
	}
	if tv.Value != nil {
		r := g.alloc()
		switch tv.Value.Kind() {
		case constant.Bool:
			v := int64(0)
			if constant.BoolVal(tv.Value) {
				v = 1
			}
			g.e.ins(x86.AMOVQ, imm(v), reg(r))
		case constant.Int:
			w, signed, ok := intClass(tv.Type)
			if !ok {
				bail("constant of type %s", tv.Type)
			}
			var v int64
			if signed {
				x, exact := constant.Int64Val(tv.Value)
				if !exact {
					bail("constant does not fit")
				}
				v = x
			} else {
				x, exact := constant.Uint64Val(tv.Value)
				if !exact {
					bail("constant does not fit")
				}
				v = int64(x)
			}
			_ = w
			g.e.ins(x86.AMOVQ, imm(v), reg(r))
		default:
			bail("constant kind %v", tv.Value.Kind())
		}
		return r
	}
	if tv.IsNil() {
		// The other operand decides whether this nil is one register (a pointer) or more (a slice, an
		// interface...): the comparison checks its operands' classes, and a nil result is checked by ret.
		r := g.alloc()
		g.e.ins(x86.AXORL, reg(r), reg(r))
		return r
	}
	width, signed, ok := intClass(tv.Type)
	if !ok {
		bail("value of type %s", tv.Type)
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return g.expr(x.X)
	case *ast.Ident:
		pr, ok := g.params[g.info.Uses[x]]
		if !ok {
			bail("identifier that is not a parameter")
		}
		if width == 8 {
			return pr
		}
		t := g.alloc()
		g.e.ins(loadOp(width, signed), reg(pr), reg(t))
		return t
	case *ast.StarExpr:
		p := g.expr(x.X)
		if _, isPtr := g.info.Types[x.X].Type.Underlying().(*types.Pointer); !isPtr {
			bail("dereference of a non-pointer")
		}
		t := g.alloc()
		g.e.ins(loadOp(width, signed), mem(p, 0), reg(t))
		g.release(p)
		return t
	case *ast.SelectorExpr:
		p, off := g.fieldAddr(x)
		t := g.alloc()
		g.e.ins(loadOp(width, signed), mem(p, off), reg(t))
		g.release(p)
		return t
	case *ast.UnaryExpr:
		r := g.owned(g.expr(x.X))
		switch x.Op {
		case token.NOT:
			g.e.ins(x86.AXORQ, imm(1), reg(r))
		case token.SUB:
			g.e.add(&obj.Prog{As: x86.ANEGQ, To: reg(r)})
			g.extend(r, width, signed)
		case token.XOR:
			g.e.add(&obj.Prog{As: x86.ANOTQ, To: reg(r)})
			g.extend(r, width, signed)
		default:
			bail("unary %s", x.Op)
		}
		return r
	case *ast.CallExpr:
		if ftv := g.info.Types[x.Fun]; !ftv.IsType() || len(x.Args) != 1 {
			bail("call")
		}
		_, _, ok := intClass(g.info.Types[x.Args[0]].Type)
		if !ok {
			bail("conversion from %s", g.info.Types[x.Args[0]].Type)
		}
		// The operand is the 64-bit extension of its own type; converting keeps the low bits of the new width.
		r := g.expr(x.Args[0])
		if width < 8 {
			r = g.owned(r)
			g.extend(r, width, signed)
		}
		return r
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND, token.LOR:
			r := g.owned(g.expr(x.X))
			g.e.ins(x86.ATESTQ, reg(r), reg(r))
			var j *obj.Prog
			if x.Op == token.LAND {
				j = g.jump(x86.AJEQ) // false: the answer is already in r
			} else {
				j = g.jump(x86.AJNE)
			}
			y := g.expr(x.Y)
			g.e.ins(x86.AMOVQ, reg(y), reg(r))
			g.release(y)
			g.land(j)
			return r
		}
		lt, rt := g.info.Types[x.X].Type, g.info.Types[x.Y].Type
		if g.info.Types[x.X].IsNil() { // nil == p: the typed side says what is compared
			lt = rt
		}
		if g.info.Types[x.Y].IsNil() {
			rt = lt
		}
		lw, lsigned, lok := intClass(lt)
		if _, _, rok := intClass(rt); !lok || !rok {
			bail("operands of type %s", lt)
		}
		a := g.expr(x.X)
		b := g.expr(x.Y)
		switch x.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			_ = lw
			g.e.ins(x86.ACMPQ, reg(a), reg(b)) // operands are full 64-bit extensions, so one comparison serves every width
			g.release(b)
			g.release(a)
			t := g.alloc()
			var set obj.As
			switch x.Op {
			case token.EQL:
				set = x86.ASETEQ
			case token.NEQ:
				set = x86.ASETNE
			case token.LSS:
				set = map[bool]obj.As{true: x86.ASETLT, false: x86.ASETCS}[lsigned]
			case token.LEQ:
				set = map[bool]obj.As{true: x86.ASETLE, false: x86.ASETLS}[lsigned]
			case token.GTR:
				set = map[bool]obj.As{true: x86.ASETGT, false: x86.ASETHI}[lsigned]
			case token.GEQ:
				set = map[bool]obj.As{true: x86.ASETGE, false: x86.ASETCC}[lsigned]
			}
			g.e.add(&obj.Prog{As: set, To: reg(t)})
			g.e.ins(x86.AMOVBQZX, reg(t), reg(t))
			return t
		}
		if _, isPtr := tv.Type.Underlying().(*types.Pointer); isPtr || tv.Type.Underlying() == types.Typ[types.UnsafePointer] {
			bail("pointer arithmetic")
		}
		if b0, isB := tv.Type.Underlying().(*types.Basic); isB && b0.Kind() == types.Bool {
			bail("arithmetic on bool")
		}
		a = g.owned(a)
		var as obj.As
		switch x.Op {
		case token.ADD:
			as = x86.AADDQ
		case token.SUB:
			as = x86.ASUBQ
		case token.MUL:
			as = x86.AIMULQ
		case token.AND:
			as = x86.AANDQ
		case token.OR:
			as = x86.AORQ
		case token.XOR:
			as = x86.AXORQ
		case token.AND_NOT:
			b = g.owned(b)
			g.e.add(&obj.Prog{As: x86.ANOTQ, To: reg(b)})
			as = x86.AANDQ
		default:
			bail("binary %s", x.Op)
		}
		g.e.ins(as, reg(b), reg(a))
		g.release(b)
		g.extend(a, width, signed)
		return a
	}
	bail("expression %T", e)
	return 0
}

// fieldAddr gives the address of the field x selects as a register and an offset under 4096 (so that a nil
// base faults in the first page). The base is a pointer the selection goes through: the operand itself,
// or an embedded pointer on the way, or the pointer under a struct-valued operand such as p.inner in
// p.inner.f.
func (g *fgen) fieldAddr(x *ast.SelectorExpr) (int16, int64) {
	sel := g.info.Selections[x]
	if sel == nil || sel.Kind() != types.FieldVal {
		bail("selector that is not a field")
	}
	var p int16
	var off int64
	var cur *types.Struct
	bt := g.info.Types[x.X].Type
	switch u := bt.Underlying().(type) {
	case *types.Pointer:
		st, ok := u.Elem().Underlying().(*types.Struct)
		if !ok {
			bail("pointer to non-struct")
		}
		p, cur = g.expr(x.X), st
	case *types.Struct:
		inner := x.X
		for {
			if pe, ok := inner.(*ast.ParenExpr); ok {
				inner = pe.X
				continue
			}
			break
		}
		switch y := inner.(type) {
		case *ast.SelectorExpr:
			p, off = g.fieldAddr(y)
		case *ast.StarExpr:
			if _, isPtr := g.info.Types[y.X].Type.Underlying().(*types.Pointer); !isPtr {
				bail("dereference of a non-pointer")
			}
			p = g.expr(y.X)
		default:
			bail("field of a struct that is not in memory")
		}
		cur = u
	default:
		bail("field of %s", bt)
	}
	path := sel.Index()
	for i, fi := range path {
		fields := make([]*types.Var, cur.NumFields())
		for j := range fields {
			fields[j] = cur.Field(j)
		}
		off += sizes.Offsetsof(fields)[fi]
		if off >= 4096 {
			bail("field offset %d needs an explicit nil check", off)
		}
		if i == len(path)-1 {
			break
		}
		ft := cur.Field(fi).Type()
		switch u := ft.Underlying().(type) {
		case *types.Struct:
			cur = u // an embedded struct: same memory, further in
		case *types.Pointer:
			st, ok := u.Elem().Underlying().(*types.Struct)
			if !ok {
				bail("embedded pointer to non-struct")
			}
			t := g.alloc()
			g.e.ins(x86.AMOVQ, mem(p, off), reg(t)) // an embedded pointer: load it and go on from there
			g.release(p)
			p, off, cur = t, 0, st
		default:
			bail("selection through %s", ft)
		}
	}
	return p, off
}

func (g *fgen) ret(e ast.Expr) {
	r := g.expr(e)
	if r != x86.REG_AX {
		g.e.ins(x86.AMOVQ, reg(r), reg(x86.REG_AX))
	}
	g.release(r)
	g.e.ret()
}

// compileFunc writes fd into e, or reports why it cannot. A failure leaves half a function in e, so the
// caller tries each function on a scratch emitter first and only then on the real one.
func compileFunc(e *emitter, p *Pkg, fd *ast.FuncDecl, symName string) (err error) {
	fn, _ := p.info.Defs[fd.Name].(*types.Func)
	if fn == nil {
		return fmt.Errorf("no definition")
	}
	sig := fn.Type().(*types.Signature)
	if sig.TypeParams() != nil || sig.RecvTypeParams() != nil || sig.Variadic() {
		return fmt.Errorf("generic or variadic")
	}
	g := &fgen{e: e, info: p.info, params: map[types.Object]int16{}}
	var vars []*types.Var
	if sig.Recv() != nil {
		vars = append(vars, sig.Recv())
	}
	for i := 0; i < sig.Params().Len(); i++ {
		vars = append(vars, sig.Params().At(i))
	}
	if len(vars) > len(argRegs) {
		return fmt.Errorf("more than %d parameters", len(argRegs))
	}
	used := map[int16]bool{}
	for i, v := range vars {
		if _, _, ok := intClass(v.Type()); !ok {
			return fmt.Errorf("parameter of type %s", v.Type())
		}
		g.params[v] = argRegs[i]
		used[argRegs[i]] = true
	}
	if _, _, ok := intClass(sig.Results().At(0).Type()); !ok {
		return fmt.Errorf("result of type %s", sig.Results().At(0).Type())
	}
	for _, r := range []int16{x86.REG_R13, x86.REG_R12, x86.REG_DX} {
		g.free = append(g.free, r)
	}
	for i := len(argRegs) - 1; i >= 0; i-- {
		if !used[argRegs[i]] {
			g.free = append(g.free, argRegs[i])
		}
	}
	defer func() {
		if r := recover(); r != nil {
			u, ok := r.(unsupported)
			if !ok {
				panic(r)
			}
			err = fmt.Errorf("%s", string(u))
		}
	}()
	pos := fset.Position(fd.Pos())
	e.setPos(pos.Filename, uint(pos.Line))
	e.text(symName, obj.NOSPLIT|obj.NOFRAME, 0, 0)
	for _, s := range fd.Body.List {
		e.setLine(uint(fset.Position(s.Pos()).Line))
		switch x := s.(type) {
		case *ast.ReturnStmt:
			g.ret(x.Results[0])
		case *ast.IfStmt:
			c := g.expr(x.Cond)
			e.ins(x86.ATESTQ, reg(c), reg(c))
			g.release(c)
			j := g.jump(x86.AJEQ)
			saved := append([]int16(nil), g.free...)
			g.ret(x.Body.List[0].(*ast.ReturnStmt).Results[0])
			g.free = saved
			g.land(j)
		}
	}
	return nil
}

// symbolName: the linker's name for a function, as the stock compiler spells it.
func symbolName(p *Pkg, fn *types.Func) (string, bool) {
	prefix := objabi.PathToPrefix(p.ImportPath)
	if p.Name == "main" {
		prefix = "main"
	}
	sig := fn.Type().(*types.Signature)
	if sig.Recv() == nil {
		return prefix + "." + fn.Name(), true
	}
	rt := sig.Recv().Type()
	star := ""
	if pt, ok := types.Unalias(rt).(*types.Pointer); ok {
		rt, star = pt.Elem(), "*"
	}
	named, ok := types.Unalias(rt).(*types.Named)
	if !ok || named.TypeParams() != nil || named.Obj().Parent() != p.tpkg.Scope() {
		return "", false
	}
	if star != "" {
		return prefix + ".(*" + named.Obj().Name() + ")." + fn.Name(), true
	}
	return prefix + "." + named.Obj().Name() + "." + fn.Name(), true
}

// trimmed: the file name the stock compiler records (its -trimpath rewrite of the package directory)
func trimmed(p *Pkg, file string) string {
	base := p.ImportPath
	if p.Module != nil && p.Module.Version != "" {
		base = p.Module.Path + "@" + p.Module.Version + strings.TrimPrefix(p.ImportPath, p.Module.Path)
	}
	return base + "/" + filepath.Base(file)
}

func (e *emitter) setPos(file string, line uint) {
	if e.bases == nil {
		e.bases = map[string]*src.PosBase{}
	}
	b := e.bases[file]
	if b == nil {
		name := file
		if e.rename != nil {
			name = e.rename(file)
		}
		b = src.NewFileBase(name, name)
		e.bases[file] = b
	}
	e.base, e.line = b, line
}

func (e *emitter) setLine(line uint) { e.line = line }

type stats struct {
	Packages    int            `json:"packages"`
	Funcs       int            `json:"functions_in_program"`
	Candidates  int            `json:"candidates_by_syntax"`
	Taken       int            `json:"taken"`
	TakenPkgs   int            `json:"packages_with_taken_functions"`
	Reasons     map[string]int `json:"rejected_because"`
	ParseMs     float64        `json:"parse_ms"`
	CheckMs     float64        `json:"check_ms"`
	GenMs       float64        `json:"codegen_ms"`
	TextBytes   int64          `json:"text_bytes"`
	SourceBytes int64          `json:"source_bytes"`
}

func compileProgram(planPath, work string, only string) {
	b, err := os.ReadFile(planPath)
	if err != nil {
		fatal(err)
	}
	var pkgs []*Pkg
	if err := json.Unmarshal(b, &pkgs); err != nil {
		fatal(err)
	}
	st := stats{Packages: len(pkgs), Reasons: map[string]int{}}
	for i, p := range pkgs {
		p.idx = i
		byPath[p.ImportPath] = p
	}
	for _, p := range pkgs {
		for _, im := range p.Imports {
			if im == "unsafe" {
				continue
			}
			if m, ok := p.ImportMap[im]; ok {
				im = m
			}
			d := byPath[im]
			p.deps = append(p.deps, d)
			d.users = append(d.users, p)
		}
		p.waiting = int32(len(p.deps))
	}
	P := runtime.NumCPU()

	// parse, in parallel; note the candidates and empty every other function body, so that the type checker
	// spends its time on declarations and on the bodies we might take
	t0 := time.Now()
	var wg sync.WaitGroup
	ch := make(chan *Pkg, len(pkgs))
	for _, p := range pkgs {
		ch <- p
	}
	close(ch)
	var nfuncs, ncand, nbytes int64
	for w := 0; w < P; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				skip := excluded(p) || (only != "" && !strings.Contains(p.ImportPath, only))
				for _, f := range p.GoFiles {
					srcb, err := os.ReadFile(filepath.Join(p.Dir, f))
					if err != nil {
						fatal(err)
					}
					atomic.AddInt64(&nbytes, int64(len(srcb)))
					af, err := parser.ParseFile(fset, filepath.Join(p.Dir, f), srcb, parser.SkipObjectResolution|parser.ParseComments)
					if err != nil {
						fatal(err)
					}
					for _, d := range af.Decls {
						fd, ok := d.(*ast.FuncDecl)
						if !ok || fd.Body == nil {
							continue
						}
						atomic.AddInt64(&nfuncs, 1)
						if !skip && candidate(fd) {
							p.cands = append(p.cands, fd)
							atomic.AddInt64(&ncand, 1)
						} else if fd.Type.TypeParams == nil && fd.Name.Name != "init" && fd.Name.Name != "main" {
							fd.Body = nil // not ours: the checker need not look inside
						} else {
							fd.Body = &ast.BlockStmt{Lbrace: fd.Body.Lbrace, Rbrace: fd.Body.Rbrace}
						}
					}
					p.files = append(p.files, af)
				}
			}
		}()
	}
	wg.Wait()
	st.ParseMs = float64(time.Since(t0).Microseconds()) / 1000
	st.Funcs, st.Candidates, st.SourceBytes = int(nfuncs), int(ncand), nbytes

	// type check, packages in dependency order on all cores
	t0 = time.Now()
	ready := make(chan *Pkg, len(pkgs))
	for _, p := range pkgs {
		if p.waiting == 0 {
			ready <- p
		}
	}
	var done int32
	for w := 0; w < P; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ready {
				conf := types.Config{Importer: importer{p}, Sizes: sizes, Error: func(error) {}} // errors here are unused imports of emptied bodies
				if p.Module != nil && p.Module.GoVersion != "" {
					if v := strings.Split(p.Module.GoVersion, "."); len(v) >= 2 {
						conf.GoVersion = "go" + v[0] + "." + v[1]
					}
				} else if p.Module != nil {
					conf.GoVersion = "go1.16"
				}
				if len(p.cands) > 0 {
					p.info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{},
						Defs: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
				}
				p.tpkg, _ = conf.Check(p.ImportPath, fset, p.files, p.info)
				for _, u := range p.users {
					if atomic.AddInt32(&u.waiting, -1) == 0 {
						ready <- u
					}
				}
				if atomic.AddInt32(&done, 1) == int32(len(pkgs)) {
					close(ready)
				}
			}
		}()
	}
	wg.Wait()
	st.CheckMs = float64(time.Since(t0).Microseconds()) / 1000

	// generate: one object per package that has something
	t0 = time.Now()
	newEmitter("init") // the assembler's tables are built on first use, and not under a lock: do it before the workers start
	var mu sync.Mutex
	ch = make(chan *Pkg, len(pkgs))
	for _, p := range pkgs {
		if len(p.cands) > 0 {
			ch <- p
		}
	}
	close(ch)
	for w := 0; w < P; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				pkgpath := p.ImportPath
				if p.Name == "main" {
					pkgpath = "main"
				}
				e := newEmitter(pkgpath)
				e.rename = func(file string) string { return trimmed(p, file) }
				scratch := newEmitter(pkgpath)
				var taken []string
				reasons := map[string]int{}
				for _, fd := range p.cands {
					fn, _ := p.info.Defs[fd.Name].(*types.Func)
					if fn == nil {
						reasons["no definition"]++
						continue
					}
					name, ok := symbolName(p, fn)
					if !ok {
						reasons["receiver not a plain named type"]++
						continue
					}
					if err := compileFunc(scratch, p, fd, name); err != nil {
						msg := err.Error()
						if i := strings.IndexAny(msg, "0123456789"); i > 0 && strings.Contains(msg, "offset") {
							msg = "field offset needs an explicit nil check"
						}
						for _, pre := range []string{"parameter of type", "result of type", "value of type", "operands of type", "constant of type", "conversion from", "nil of type", "selection through"} {
							if strings.HasPrefix(msg, pre) {
								msg = pre + " (not one integer register)"
							}
						}
						reasons[msg]++
						continue
					}
					if err := compileFunc(e, p, fd, name); err != nil {
						fatal(fmt.Errorf("%s: compiled on the scratch emitter but not on the real one: %v", name, err))
					}
					taken = append(taken, name)
				}
				var size int64
				if len(taken) > 0 {
					dir := filepath.Join(work, fmt.Sprintf("b%04d", p.idx))
					os.MkdirAll(dir, 0o755)
					e.write(filepath.Join(dir, "sbc.o"))
					for _, s := range e.ctxt.Text {
						size += s.Size
					}
					var sb strings.Builder
					for _, n := range taken {
						fmt.Fprintf(&sb, "def %s ABIInternal\n", n)
					}
					if err := os.WriteFile(filepath.Join(dir, "sbc.symabis"), []byte(sb.String()), 0o644); err != nil {
						fatal(err)
					}
				}
				mu.Lock()
				st.Taken += len(taken)
				if len(taken) > 0 {
					st.TakenPkgs++
				}
				st.TextBytes += size
				for k, v := range reasons {
					st.Reasons[k] += v
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	st.GenMs = float64(time.Since(t0).Microseconds()) / 1000
	js, _ := json.MarshalIndent(st, "", " ")
	os.WriteFile(filepath.Join(work, "sbc.stats.json"), js, 0o644)
	type kv struct {
		k string
		v int
	}
	var rs []kv
	for k, v := range st.Reasons {
		rs = append(rs, kv{k, v})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].v > rs[j].v })
	fmt.Printf("sbc: %d functions in the program, %d candidates by syntax, %d taken in %d packages (%d bytes of code); parse %.0f ms, check %.0f ms, generate %.0f ms\n",
		st.Funcs, st.Candidates, st.Taken, st.TakenPkgs, st.TextBytes, st.ParseMs, st.CheckMs, st.GenMs)
	for _, r := range rs {
		fmt.Printf("sbc:   not taken, %6d: %s\n", r.v, r.k)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sbc:", err)
	os.Exit(1)
}
