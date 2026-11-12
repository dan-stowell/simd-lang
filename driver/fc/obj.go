// obj.go: the compiler's context for one package, and how a finished function or data symbol becomes part of
// the object file. Symbols are cmd/internal/obj's LSyms, filled in directly (bytes, relocations, pc tables);
// obj.WriteObjFile serializes them. The assembler half of cmd/internal/obj (Progs) is not used.
package main

import (
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"internal/abi"
	"log"
	"os"
	"sort"
	"strings"
	"sync"

	"cmd/internal/bio"
	"cmd/internal/obj"
	"cmd/internal/obj/x86"
	"cmd/internal/objabi"
	"cmd/internal/src"
)

type compiler struct {
	pr       *Program
	pkg      *Pkg
	info     *types.Info
	tpkg     *types.Package
	ctxt     *obj.Link
	path     string // the package's path as the linker knows it (-p)
	trimpath string

	fileIdx    map[*token.File]int32
	mu         sync.Mutex
	funcs      []*funcOut // finished functions, in the order they will be written
	closures   map[string]int
	strSyms    map[string]*obj.LSym
	tmpN       int
	runtime    map[string]*obj.LSym
	initCode   []func(g *gen) // dynamic initialization of package-level variables, in order
	strHdrs    map[string]*obj.LSym
	noWB       bool              // no write barriers (parts of the runtime)
	depInit    func(p *Pkg) bool // does a dependency have an init task?
	forceInit  bool              // emit an init task even with nothing to run (the stock object has one)
	errs       []string
	hasInit    bool
	abis       *symABIs
	embed      *embedCfg // from -embedcfg (mixed mode)
	zeroSyms   map[int64]*obj.LSym
	globGen    *gen // names the function literals of package-level initializers
	ninit      int
	dynInits   []string // variables initialized by code
	parity     func()   // mixed mode: define what stock importers expect of this object
	instances  map[string]bool
	instQueue  []*instance
	rtypeState // rtype.go: descriptors written; needEq, needHash, needWrapper for the code generator
}

func newCompiler(pr *Program, p *Pkg, path, trimpath string) *compiler {
	ctxt := obj.Linknew(&x86.Linkamd64)
	ctxt.Pkgpath = path
	ctxt.Flag_optimize = true
	ctxt.DiagFunc = func(format string, args ...any) { log.Fatalf("fc: "+format, args...) }
	ctxt.Std = p.Standard
	c := &compiler{pr: pr, pkg: p, info: p.info, tpkg: p.tpkg, ctxt: ctxt, path: path, trimpath: trimpath,
		fileIdx: map[*token.File]int32{}, closures: map[string]int{}, strSyms: map[string]*obj.LSym{},
		runtime: map[string]*obj.LSym{}, strHdrs: map[string]*obj.LSym{}, instances: map[string]bool{}}
	return c
}

func (c *compiler) sizeof(t types.Type) int64  { return c.pr.sizes.Sizeof(t) }
func (c *compiler) alignof(t types.Type) int64 { return c.pr.sizes.Alignof(t) }
func (c *compiler) fieldOffsets(s *types.Struct) []int64 {
	fs := make([]*types.Var, s.NumFields())
	for i := range fs {
		fs[i] = s.Field(i)
	}
	return c.pr.sizes.Offsetsof(fs)
}

type bail string

// giveUp abandons the package: something the compiler does not handle yet.
func giveUp(format string, a ...any) { panic(bail(fmt.Sprintf(format, a...))) }

// ---- symbols

// pkgPrefix is a package's path as it appears in symbol names.
func (c *compiler) pkgPrefix(p *types.Package) string {
	if p == c.tpkg {
		return objabi.PathToPrefix(c.path)
	}
	if p.Name() == "main" && p.Path() != "main" {
		// a main package is "main" to the linker; only the package itself references its symbols
		return "main"
	}
	return objabi.PathToPrefix(p.Path())
}

func (c *compiler) lookup(name string, abi obj.ABI, pkg string) *obj.LSym {
	return c.ctxt.LookupABIInit(name, abi, func(s *obj.LSym) { s.Pkg = pkg })
}

// rt is a function or variable of package runtime, by name.
func (c *compiler) rt(name string) *obj.LSym {
	c.mu.Lock()
	s := c.runtime[name]
	if s == nil {
		s = c.lookup("runtime."+name, obj.ABIInternal, "runtime")
		c.runtime[name] = s
	}
	c.mu.Unlock()
	return s
}

func (c *compiler) rtVar(name string) *obj.LSym {
	return c.lookup("runtime."+name, obj.ABI0, "runtime")
}

// dataSym defines a data symbol of this package.
func (c *compiler) defData(s *obj.LSym, kind objabi.SymKind, size int64) {
	s.Type = kind
	s.Size = size
	if s.OnList() {
		log.Fatalf("fc: symbol %s defined twice", s.Name)
	}
	s.Set(obj.AttrOnList, true)
	c.mu.Lock()
	c.ctxt.Data = append(c.ctxt.Data, s)
	c.mu.Unlock()
}

// stringSym is the read-only symbol holding the bytes of a string constant (shared across the program).
func (c *compiler) stringSym(v string) *obj.LSym {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := c.strSyms[v]; s != nil {
		return s
	}
	// content-addressable: the linker merges equal ones
	s := &obj.LSym{Name: fmt.Sprintf("go:string.fc%d", len(c.strSyms)), Type: objabi.SRODATA, P: []byte(v), Size: int64(len(v)), Align: 1}
	s.Set(obj.AttrContentAddressable, true)
	s.Set(obj.AttrDuplicateOK, true)
	s.Set(obj.AttrLocal, true)
	s.Set(obj.AttrOnList, true)
	c.ctxt.Data = append(c.ctxt.Data, s)
	c.strSyms[v] = s
	return s
}

// ---- positions

func (c *compiler) registerFiles() {
	wd, _ := os.Getwd()
	for _, f := range c.pkg.files {
		tf := c.pr.fset.File(f.Pos())
		name := tf.Name()
		abs := objabi.AbsFile(wd, name, c.trimpath)
		base := src.NewFileBase(name, abs)
		xp := c.ctxt.PosTable.XPos(src.MakePos(base, 1, 1))
		c.fileIdx[tf] = int32(c.ctxt.InnermostPos(xp).FileIndex())
	}
}

func (c *compiler) fileLine(p token.Pos) (int32, int32) {
	if !p.IsValid() {
		return 0, 1
	}
	tf := c.pr.fset.File(p)
	c.mu.Lock()
	idx, ok := c.fileIdx[tf]
	if !ok {
		// a file of another package (the body of a generic function instantiated here)
		name := tf.Name()
		base := src.NewFileBase(name, objabi.AbsFile("", name, c.trimpath))
		xp := c.ctxt.PosTable.XPos(src.MakePos(base, 1, 1))
		idx = int32(c.ctxt.InnermostPos(xp).FileIndex())
		c.fileIdx[tf] = idx
	}
	c.mu.Unlock()
	return idx, int32(tf.Line(p)) // TODO //line directives
}

// ---- functions

type pcval struct {
	pc  int
	val int32
}

// funcOut is a compiled function, ready to become a text symbol.
type funcOut struct {
	sym       *obj.LSym
	code      []byte
	relocs    []obj.Reloc
	flags     int // obj.NOSPLIT, obj.DUPOK, obj.WRAPPER, ...
	pos       token.Pos
	args      int32
	locals    int32
	sp        []pcval
	lines     []pcval // value: index into poss
	poss      []token.Pos
	unsafe    []pcval
	stackmap  []pcval
	argsMap   *obj.LSym
	localsMap *obj.LSym
	funcFlag  abi.FuncFlag
}

// pctab encodes a pc-value table (see cmd/internal/obj.funcpctab) from events sorted by pc.
func pctab(evs []pcval, size int) []byte {
	var dst []byte
	val := int32(-1)
	pc := 0
	started := false
	for i, e := range evs {
		if i+1 < len(evs) && evs[i+1].pc == e.pc {
			continue // the last value set at a pc wins
		}
		if e.pc >= size {
			break
		}
		if started && e.val == val {
			continue
		}
		if started {
			dst = binary.AppendUvarint(dst, uint64(e.pc-pc))
			pc = e.pc
		} else if e.pc != 0 {
			// the table starts at the function's first byte: the default until the first event
			dst = binary.AppendVarint(dst, 0)
			dst = binary.AppendUvarint(dst, uint64(e.pc))
			pc = e.pc
		}
		dst = binary.AppendVarint(dst, int64(e.val-val))
		val = e.val
		started = true
	}
	if started {
		dst = binary.AppendUvarint(dst, uint64(size-pc))
		dst = append(dst, 0)
	}
	return dst
}

func pcSym(data []byte) *obj.LSym {
	s := &obj.LSym{Type: objabi.SRODATA, Align: 1, P: data, Size: int64(len(data))}
	s.Set(obj.AttrContentAddressable, true)
	s.Set(obj.AttrPcdata, true)
	return s
}

// stackMapSym builds a stack map symbol: n bitmaps of nbit bits each.
func (c *compiler) stackMapSym(nbit int, maps ...[]bool) *obj.LSym {
	data := binary.LittleEndian.AppendUint32(nil, uint32(len(maps)))
	data = binary.LittleEndian.AppendUint32(data, uint32(nbit))
	for _, m := range maps {
		bs := make([]byte, (nbit+7)/8)
		for i, b := range m {
			if b {
				bs[i/8] |= 1 << (i % 8)
			}
		}
		data = append(data, bs...)
	}
	s := c.ctxt.GCLocalsSym(data)
	c.mu.Lock()
	if !s.OnList() {
		s.Type = objabi.SRODATA
		s.Size = int64(len(data))
		s.Set(obj.AttrDuplicateOK, true)
		s.Set(obj.AttrOnList, true)
		c.ctxt.Data = append(c.ctxt.Data, s)
	}
	c.mu.Unlock()
	return s
}

// placeholderFunc defines s as a function that traps: a symbol the stock object has and fc does not.
func (c *compiler) placeholderFunc(s *obj.LSym) {
	f := &funcOut{sym: s, code: []byte{0x0F, 0x0B}, flags: obj.NOSPLIT, sp: []pcval{{0, 0}}, pos: c.pkg.files[0].Package}
	c.installFunc(f)
}

// installFuncs installs every finished function as a text symbol. Called once, single-threaded.
func (c *compiler) installFuncs() {
	sort.SliceStable(c.funcs, func(i, j int) bool { return c.funcs[i].pos < c.funcs[j].pos })
	for _, f := range c.funcs {
		c.installFunc(f)
	}
}

func (c *compiler) installFunc(f *funcOut) {
	{
		s := f.sym
		file, line := c.fileLine(f.pos)
		_ = file
		c.ctxt.InitTextSym(s, f.flags, src.NoXPos)
		fn := s.Func()
		fn.StartLine = line
		fn.FuncFlag |= f.funcFlag
		fn.Args = f.args
		fn.Locals = f.locals
		s.P = f.code
		s.Size = int64(len(f.code))
		s.R = f.relocs
		size := len(f.code)
		pc := &fn.Pcln
		pc.Pcsp = pcSym(pctab(f.sp, size))
		pc.UsedFiles = map[goobjCUFileIndex]struct{}{}
		var files, lines []pcval
		for _, l := range f.lines {
			fi, ln := c.fileLine(f.poss[l.val])
			files = append(files, pcval{l.pc, fi})
			lines = append(lines, pcval{l.pc, ln})
			pc.UsedFiles[goobjCUFileIndex(fi)] = struct{}{}
		}
		pc.Pcfile = pcSym(pctab(files, size))
		pc.Pcline = pcSym(pctab(lines, size))
		pc.Pcinline = pcSym(nil)
		pc.Pcdata = []*obj.LSym{pcSym(pctab(f.unsafe, size)), pcSym(pctab(f.stackmap, size))}
		if f.argsMap != nil {
			pc.Funcdata = []*obj.LSym{f.argsMap, f.localsMap}
		}
	}
}

// ---- output

func (c *compiler) writeObject(path string) {
	c.ctxt.NumberSyms()
	out, err := bio.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	out.WriteString(objabi.HeaderString())
	if c.path == "main" {
		fmt.Fprintf(out, "main\n") // the linker looks for this line
	}
	fmt.Fprintf(out, "!\n")
	obj.WriteObjFile(c.ctxt, out)
	// The stock reader takes the address of the first byte after the relocation block even when a symbol
	// has no relocations; in an object with no data, no function and no referenced names that byte would
	// be past the end.
	out.Write(make([]byte, 8))
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}
}

// funcDecls lists the package's function declarations in source order.
func (c *compiler) funcDecls() []*ast.FuncDecl {
	var fds []*ast.FuncDecl
	for _, f := range c.pkg.files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				fds = append(fds, fd)
			}
		}
	}
	return fds
}

// funcSymName is the linker's name for a declared function or method.
func (c *compiler) funcSymName(fn *types.Func) string {
	sig := fn.Type().(*types.Signature)
	pp := c.pkgPrefix(fn.Pkg())
	if r := sig.Recv(); r != nil {
		t := r.Type()
		ptr := false
		if p, ok := t.(*types.Pointer); ok {
			t, ptr = p.Elem(), true
		}
		name := types.Unalias(t).(*types.Named).Obj().Name()
		if ptr {
			return pp + ".(*" + name + ")." + fn.Name()
		}
		return pp + "." + name + "." + fn.Name()
	}
	return pp + "." + fn.Name()
}

func (c *compiler) funcSym(fn *types.Func) *obj.LSym {
	if fn.Type().(*types.Signature).Recv() == nil {
		if ln := c.linknameOf(fn.Pkg(), fn.Name()); ln != "" {
			pkg := ""
			if i := strings.LastIndex(ln, "."); i >= 0 {
				pkg = ln[:i]
			}
			return c.lookup(ln, obj.ABIInternal, pkg)
		}
	}
	return c.lookup(c.funcSymName(fn), obj.ABIInternal, c.pkgPrefixOf(fn.Pkg()))
}

// linknameOf: the symbol name a //go:linkname directive in the object's package gives it, or "".
func (c *compiler) linknameOf(tp *types.Package, name string) string {
	if tp == nil {
		return ""
	}
	p := c.pr.byTypes(tp)
	if p == nil {
		return ""
	}
	return c.pr.linknames(p)[name]
}

func (c *compiler) pkgPrefixOf(p *types.Package) string {
	if p == nil {
		return ""
	}
	return c.pkgPrefix(p)
}

func (c *compiler) globalSym(v *types.Var) *obj.LSym {
	if ln := c.linknameOf(v.Pkg(), v.Name()); ln != "" {
		pkg := ""
		if i := strings.LastIndex(ln, "."); i >= 0 {
			pkg = ln[:i]
		}
		return c.lookup(ln, obj.ABI0, pkg)
	}
	return c.lookup(c.pkgPrefix(v.Pkg())+"."+v.Name(), obj.ABI0, c.pkgPrefix(v.Pkg()))
}

func hasDirective(doc *ast.CommentGroup, d string) bool {
	if doc == nil {
		return false
	}
	for _, cm := range doc.List {
		if cm.Text == d || strings.HasPrefix(cm.Text, d+" ") {
			return true
		}
	}
	return false
}
