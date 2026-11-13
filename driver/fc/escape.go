// escape.go: escape analysis over the typed syntax tree, after the stock compiler's
// (cmd/compile/internal/escape): every variable and every allocating expression is a location; every
// assignment is an edge between locations weighted by its dereference count (-1 for an address-of); the heap
// is a location that outlives everything. A location whose address reaches a location that outlives it must
// be allocated on the heap. Functions are analyzed bottom-up over the package's call graph, one strongly
// connected component at a time; what a function does with each parameter is its summary, used at static
// call sites.
//
// Where this differs from the stock analysis, on purpose:
//   - closures capture by reference, always (fc's closure objects hold the addresses of the variables), so a
//     variable captured by a closure that escapes is a heap variable even if it is never reassigned;
//   - the results of a function literal are treated like those of any function: what flows into them
//     outlives everything (stock tracks literals whose every call site it sees);
//   - an append's new backing store and a make of non-constant size are heap sites when they outlive their
//     loop iteration / always (stock guards its reuse with a flag / uses a small fixed stack buffer);
//   - the variables of a `for` clause and of a range clause are per-iteration variables (as the language says
//     from Go 1.22): they are declared one loop deeper than the statement.
//
// The files: escape.go (the driver, the graph and its solution), escape_expr.go (expressions),
// escape_call.go (calls), escape_stmt.go (statements), escape_dump.go (`fc esc`, the decisions as text for
// tools/fc/escdiff.sh).
package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"go/version"
	"os"
	"strings"
	"sync"
)

// escInfo is the result for one package.
type escInfo struct {
	// A local variable (parameter, result or local) that must live on the heap. A variable that is not
	// here may have its address taken and may be captured by closures: it stays in the frame.
	heapVar map[*types.Var]bool
	// An allocation site that must use the heap, keyed by its syntax node: the *ast.CompositeLit of &T{...}
	// (and of slice and map literals), the *ast.CallExpr of new(...) and make(...) and append(...),
	// the *ast.FuncLit (its closure object), the *ast.SelectorExpr of a method value, and for a value
	// converted to an interface the ast.Expr being converted (explicit or implicit conversion sites:
	// assignments, call arguments, returns, composite literal elements, sends, comparisons ...).
	// A site not in the map may be allocated in the frame of the enclosing function.
	//
	// More keys: the *ast.CallExpr of a call of a variadic function is the slice made of its variadic
	// arguments; the *ast.GoStmt and *ast.DeferStmt are the closure object that binds the callee and the
	// arguments of `go f(x)` and `defer f(x)` (for `defer func() { ... }()` it is the *ast.FuncLit); the
	// *ast.RangeStmt of a range over a function is the closure object of its body; the *ast.CallExpr of a
	// conversion between strings and byte or rune slices and the *ast.BinaryExpr of a string concatenation
	// are their results' storage. When two sites share a node (any([]int{1}): the literal's array and the
	// interface's copy of the slice) the entry is true if either must use the heap.
	heapSite map[ast.Node]bool
	// Summaries: for each function and method declared in the package, what happens to each parameter
	// (receiver first).
	summary map[*types.Func]*escSummary

	// for `fc esc`
	sites  []escSite
	params []escParam
	vars   []*types.Var // every heap variable, in the order found
	varWhy map[*types.Var]string
	varPos map[*types.Var]token.Pos // where the stock compiler reports a variable, if not at its declaration

	// With escByValue: the captured variables that closures hold copies of.
	byValue map[*types.Var]bool
}

// escSummary says what a function does with its parameters.
type escSummary struct {
	// Params has an entry for the receiver, if there is one, and then for each parameter (a variadic
	// parameter is one parameter: the slice).
	Params []escLeaks
	// UintptrEscapes: the function is marked //go:uintptrescapes: a pointer converted to uintptr in its
	// argument list escapes.
	UintptrEscapes bool
}

// escLeaks is where the value of one parameter flows. A count is the number of dereferences on the shortest
// path: 0 is the parameter's value itself, 1 what it points to, and so on; -1 is "does not flow there".
type escLeaks struct {
	Heap    int   // to the heap (or anywhere else that outlives the call)
	Results []int // to each result of the function, nil if to none
}

// leaks reports whether the parameter's value, or anything reachable from it, flows anywhere.
func (l escLeaks) leaks() bool {
	if l.Heap >= 0 {
		return true
	}
	for _, r := range l.Results {
		if r >= 0 {
			return true
		}
	}
	return false
}

func (l escLeaks) result(i int) int {
	if i < len(l.Results) {
		return l.Results[i]
	}
	return -1
}

func (l *escLeaks) addHeap(derefs int) {
	if l.Heap < 0 || derefs < l.Heap {
		l.Heap = derefs
	}
}

func (l *escLeaks) addResult(i, n, derefs int) {
	if l.Results == nil {
		l.Results = make([]int, n)
		for j := range l.Results {
			l.Results[j] = -1
		}
	}
	if l.Results[i] < 0 || derefs < l.Results[i] {
		l.Results[i] = derefs
	}
}

// optimize drops the result flows that the heap flow already covers.
func (l *escLeaks) optimize() {
	if l.Heap < 0 {
		return
	}
	any := false
	for i, r := range l.Results {
		if r >= l.Heap {
			l.Results[i] = -1
		} else if r >= 0 {
			any = true
		}
	}
	if !any {
		l.Results = nil
	}
}

// the stock compiler's limits (ir.MaxStackVarSize, ir.MaxImplicitStackVarSize)
const (
	escMaxStackVarSize         = 128 * 1024
	escMaxImplicitStackVarSize = 64 * 1024
)

type escSiteKind uint8

const (
	escSiteNew escSiteKind = iota
	escSitePtrLit
	escSiteSliceLit
	escSiteMapLit
	escSiteMakeSlice
	escSiteMakeMap
	escSiteAppend
	escSiteClosure
	escSiteMethodValue
	escSiteIface
	escSiteDDD
	escSiteGoDefer
	escSiteRangeFunc
	escSiteString
)

var escSiteNames = [...]string{"new", "&literal", "slice literal", "map literal", "make slice", "make map", "append",
	"func literal", "method value", "interface", "... argument", "go/defer", "range func", "string"}

// escSite is one allocation site and its verdict.
type escSite struct {
	node ast.Node
	kind escSiteKind
	pos  token.Pos // where the stock compiler reports it
	heap bool
	why  string // a reason that is not the flow of its address: "non-constant size", "too large for stack" ...
	gen  bool   // inside a generic function
}

// escParam is one parameter of a declared function and its summary.
type escParam struct {
	v       *types.Var
	fn      *types.Func
	leaks   escLeaks
	results *types.Tuple
	gen     bool
	moved   bool // the parameter itself is a heap variable
}

var escCache sync.Map // *Pkg -> *escEntry

type escEntry struct {
	once sync.Once
	info *escInfo
}

// escape analyzes p (type-checked; its dependencies' results are available through pr) and caches the result.
func (pr *Program) escape(p *Pkg) *escInfo {
	v, _ := escCache.LoadOrStore(p, new(escEntry))
	ent := v.(*escEntry)
	ent.once.Do(func() { ent.info = pr.escapePkg(p) })
	return ent.info
}

// ---- functions

// escFn is a function being analyzed: a declaration, a function literal, the body of a range over a
// function (a function literal in all but syntax), or the package's variable initializers taken together.
type escFn struct {
	decl    *ast.FuncDecl
	lit     *ast.FuncLit
	rng     *ast.RangeStmt
	obj     *types.Func
	sig     *types.Signature
	parent  *escFn
	results []*escLoc // of the function a return statement here returns from
	params  []*escLoc // receiver first; nil entries for parameters that cannot be named
	looping map[*types.Label]bool
	generic bool
	perIter bool // Go 1.22 loop variables
}

// escContains: is fn nested (at any depth) in outer?
func escContains(outer, fn *escFn) bool {
	for fn = fn.parent; fn != nil; fn = fn.parent {
		if fn == outer {
			return true
		}
	}
	return false
}

// ---- the graph

type escLoc struct {
	v         *types.Var // a variable, or
	site      int        // 1 + the index of a site in the batch's list, or neither: a temporary
	fn        *escFn
	loopDepth int
	edges     []escEdge
	escapes   bool
	param     bool
	paramOut  bool
	resultIdx int
	leaks     escLeaks

	// for the choice between capturing a variable by reference and by value
	addrTaken  bool
	reassigned bool
	captured   bool

	// for the walk
	walkgen uint32
	derefs  int
	queued  bool
}

type escEdge struct {
	src    *escLoc
	derefs int // >= -1
}

// escHole is somewhere a value can be stored: a location, behind derefs dereferences (-1: its address is
// what is stored).
type escHole struct {
	dst    *escLoc
	derefs int
	addrOf bool // the last step to here was an address-of: what flows in has its address taken
}

func (l *escLoc) hole() escHole { return escHole{dst: l} }

func (k escHole) shift(delta int) escHole {
	k.derefs += delta
	if k.derefs < -1 {
		panic("fc: escape: derefs underflow")
	}
	if delta != 0 {
		k.addrOf = delta < 0
	}
	return k
}
func (k escHole) deref() escHole { return k.shift(1) }
func (k escHole) addr() escHole  { return k.shift(-1) }

// escBatch is one strongly connected component of the package's call graph.
type escBatch struct {
	pr   *Program
	p    *Pkg
	info *types.Info
	out  *escInfo
	pk   *escPkg

	locs     []*escLoc
	sites    []escSite
	heapLoc  escLoc
	blankLoc escLoc
	varLoc   map[*types.Var]*escLoc
	fns      map[*types.Func]*escFn // the declarations of this batch
	litFn    map[*ast.FuncLit]*escFn
	force    []ast.Node // sites that use the heap whatever the graph says
	lits     []*escFn   // the function literals (and bodies of ranges over functions) walked
	closures []escClosure
}

// escClosure is a function literal's closure object and the variables it captures.
type escClosure struct {
	k    escHole
	vars []*types.Var
}

// escByValue: closures hold copies of the captured variables that are never assigned after the first
// capture, whose address is never taken, and that are at most 128 bytes (the stock compiler's rule); they
// are listed in escInfo.byValue. Off, every capture is by reference, which is what fc's code generator
// does. (`fc esc -byvalue`, or FC_ESC_BYVALUE=1, turns it on.)
var escByValue = os.Getenv("FC_ESC_BYVALUE") != ""

// flowClosures: now that the batch's functions have been walked, every assignment to the captured
// variables has been seen.
func (b *escBatch) flowClosures() {
	for _, c := range b.closures {
		for _, v := range c.vars {
			l := b.varLoc[v]
			byval := escByValue && !l.addrTaken && !l.reassigned && !l.paramOut && !mentionsTypeParam(v.Type()) &&
				b.pr.sizes.Sizeof(v.Type()) <= 128
			if byval {
				b.out.byValue[v] = true
				b.flow(c.k, l)
			} else {
				b.flow(c.k.addr(), l)
			}
		}
	}
	b.closures = nil
}

func (b *escBatch) heapHole() escHole    { return b.heapLoc.hole() }
func (b *escBatch) discardHole() escHole { return b.blankLoc.hole() }

func (b *escBatch) newLoc(fn *escFn, depth int) *escLoc {
	l := &escLoc{fn: fn, loopDepth: depth, leaks: escLeaks{Heap: -1}}
	b.locs = append(b.locs, l)
	return l
}

func (b *escBatch) flow(k escHole, src *escLoc) {
	dst := k.dst
	if k.addrOf {
		src.addrTaken = true
	}
	if dst == &b.blankLoc {
		return
	}
	if dst == src && k.derefs >= 0 {
		return
	}
	if dst.escapes && k.derefs < 0 {
		src.escapes = true
		return
	}
	dst.edges = append(dst.edges, escEdge{src, k.derefs})
}

// outlives: may a value stored in l survive other, were other in its function's frame?
func (b *escBatch) outlives(l, other *escLoc) bool {
	if l.escapes {
		return true
	}
	// what a function returns: the caller may do anything with it
	if l.paramOut {
		return true
	}
	// declared outside other's loop: the next iteration reuses other's storage
	if l.fn == other.fn && l.loopDepth < other.loopDepth {
		return true
	}
	// other is a variable of a function literal inside l's function: the literal returns first
	if escContains(l.fn, other.fn) {
		return true
	}
	return false
}

func (l *escLoc) leakTo(sink *escLoc, derefs int) {
	if !sink.escapes && sink.paramOut && sink.fn == l.fn {
		l.leaks.addResult(sink.resultIdx, len(l.fn.results), derefs)
		return
	}
	l.leaks.addHeap(derefs)
}

func (b *escBatch) walkAll() {
	// a queue with pushes at both ends, as in the stock compiler: escaping locations first
	n := len(b.locs) + 1
	front := make([]*escLoc, 0, n) // popped from the end
	var back []*escLoc
	bi := 0
	enqueue := func(l *escLoc) {
		if !l.queued {
			l.queued = true
			if l.escapes {
				front = append(front, l)
			} else {
				back = append(back, l)
			}
		}
	}
	for _, l := range b.locs {
		l.queued = true
		front = append(front, l)
	}
	b.heapLoc.queued = true
	front = append(front, &b.heapLoc)
	var walkgen uint32
	for {
		var root *escLoc
		if len(front) > 0 {
			root = front[len(front)-1]
			front = front[:len(front)-1]
		} else if bi < len(back) {
			root = back[bi]
			bi++
		} else {
			break
		}
		root.queued = false
		walkgen++
		b.walkOne(root, walkgen, enqueue)
	}
}

// walkOne finds the least number of dereferences from root to every location that flows to it.
func (b *escBatch) walkOne(root *escLoc, walkgen uint32, enqueue func(*escLoc)) {
	root.walkgen = walkgen
	root.derefs = 0
	todo := []*escLoc{root}
	for len(todo) > 0 {
		l := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		derefs := l.derefs
		newEsc := false
		if derefs < 0 {
			// l's address flows to root; what is stored in l only flows there as a value
			derefs = 0
			if b.outlives(root, l) {
				newEsc = true
			}
		}
		if l.param && b.outlives(root, l) {
			l.leakTo(root, derefs)
		}
		if newEsc && !l.escapes {
			l.escapes = true
			enqueue(l)
			continue
		}
		for _, e := range l.edges {
			if e.src.escapes {
				continue
			}
			d := derefs + e.derefs
			if e.src.walkgen != walkgen || e.src.derefs > d {
				e.src.walkgen = walkgen
				e.src.derefs = d
				todo = append(todo, e.src)
			}
		}
	}
}

// ---- the package

type escPkg struct {
	pr       *Program
	p        *Pkg
	info     *types.Info
	out      *escInfo
	decls    map[*types.Func]*ast.FuncDecl
	fileOf   map[*ast.FuncDecl]*ast.File
	ptrCache map[types.Type]bool
	// variables assigned after their declaration, or whose address is taken: they do not name one function
	reassigned map[*types.Var]bool
	staticLit  map[*types.Var]*ast.FuncLit
	staticFunc map[*types.Var]*types.Func
	sumCache   map[*types.Func]*escSummary
	// a function literal that captures nothing is a function of its own, analyzed before the function
	// it is in (as in the stock compiler): it has a summary
	litSum map[*ast.FuncLit]*escSummary
}

type escUnit struct {
	decl  *ast.FuncDecl
	obj   *types.Func
	specs []*ast.ValueSpec // the package's initializers
	file  *ast.File

	// Tarjan
	index, low int
	onStack    bool
	refs       []*escUnit
}

func (pr *Program) escapePkg(p *Pkg) *escInfo {
	out := &escInfo{heapVar: map[*types.Var]bool{}, heapSite: map[ast.Node]bool{}, summary: map[*types.Func]*escSummary{},
		varWhy: map[*types.Var]string{}, varPos: map[*types.Var]token.Pos{},
		byValue: map[*types.Var]bool{}}
	if p.tpkg == nil || p.info == nil {
		return out
	}
	pk := &escPkg{pr: pr, p: p, info: p.info, out: out, decls: map[*types.Func]*ast.FuncDecl{}, fileOf: map[*ast.FuncDecl]*ast.File{},
		ptrCache: map[types.Type]bool{}, reassigned: map[*types.Var]bool{}, staticLit: map[*types.Var]*ast.FuncLit{},
		sumCache: map[*types.Func]*escSummary{}, staticFunc: map[*types.Var]*types.Func{}, litSum: map[*ast.FuncLit]*escSummary{}}
	var units []*escUnit
	byObj := map[*types.Func]*escUnit{}
	initUnit := &escUnit{}
	for _, f := range p.files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				obj, _ := p.info.Defs[d.Name].(*types.Func)
				if obj == nil {
					continue
				}
				pk.decls[obj] = d
				pk.fileOf[d] = f
				if d.Body == nil {
					pk.bodyless(obj, d)
					continue
				}
				u := &escUnit{decl: d, obj: obj, file: f}
				units = append(units, u)
				byObj[obj] = u
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, s := range d.Specs {
					if vs := s.(*ast.ValueSpec); len(vs.Values) > 0 {
						initUnit.specs = append(initUnit.specs, vs)
					}
				}
			}
		}
	}
	if len(initUnit.specs) > 0 {
		units = append(units, initUnit)
	}
	// the call graph: every reference to a function of the package
	for _, u := range units {
		seen := map[*escUnit]bool{}
		visit := func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if fn, ok := p.info.Uses[id].(*types.Func); ok {
					if t := byObj[fn.Origin()]; t != nil && !seen[t] {
						seen[t] = true
						u.refs = append(u.refs, t)
					}
				}
			}
			return true
		}
		if u.decl != nil {
			pk.inspectLive(u.decl.Body, visit)
			pk.scanAssigned(u.decl.Body)
		}
		for _, s := range u.specs {
			for _, v := range s.Values {
				pk.inspectLive(v, visit)
				pk.scanAssigned(v)
			}
		}
	}
	// strongly connected components, callees first (Tarjan)
	index := 0
	var stack []*escUnit
	var visit func(u *escUnit)
	visit = func(u *escUnit) {
		index++
		u.index, u.low = index, index
		stack = append(stack, u)
		u.onStack = true
		for _, t := range u.refs {
			if t.index == 0 {
				visit(t)
				u.low = min(u.low, t.low)
			} else if t.onStack {
				u.low = min(u.low, t.index)
			}
		}
		if u.low == u.index {
			var scc []*escUnit
			for {
				t := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				t.onStack = false
				scc = append(scc, t)
				if t == u {
					break
				}
			}
			pk.batch(scc)
		}
	}
	for _, u := range units {
		if u.index == 0 {
			visit(u)
		}
	}
	return out
}

// scanAssigned records the local variables that are assigned after their declaration or have their address
// taken, and the function literals that variables are declared with.
func (pk *escPkg) scanAssigned(root ast.Node) {
	info := pk.info
	var mark func(e ast.Expr)
	mark = func(e ast.Expr) {
		switch e := ast.Unparen(e).(type) {
		case *ast.Ident:
			if v, ok := info.Uses[e].(*types.Var); ok {
				pk.reassigned[v] = true
			}
		case *ast.SelectorExpr:
			mark(e.X)
		case *ast.IndexExpr:
			mark(e.X)
		}
	}
	ast.Inspect(root, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, l := range n.Lhs {
				id, ok := l.(*ast.Ident)
				if n.Tok == token.DEFINE && ok && info.Defs[id] != nil {
					if v, ok := info.Defs[id].(*types.Var); ok && len(n.Lhs) == len(n.Rhs) {
						pk.staticValue(v, n.Rhs[i])
					}
					continue
				}
				mark(l)
			}
		case *ast.ValueSpec:
			if len(n.Names) == len(n.Values) {
				for i, id := range n.Names {
					if v, ok := info.Defs[id].(*types.Var); ok {
						pk.staticValue(v, n.Values[i])
					}
				}
			}
		case *ast.IncDecStmt:
			mark(n.X)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				if n.Key != nil {
					mark(n.Key)
				}
				if n.Value != nil {
					mark(n.Value)
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				mark(n.X)
			}
		}
		return true
	})
}

// staticValue: v is declared with the value x. If x is a function literal or names a function, calls of v
// (while nothing else is assigned to it) are calls of that.
func (pk *escPkg) staticValue(v *types.Var, x ast.Expr) {
	switch x := ast.Unparen(x).(type) {
	case *ast.FuncLit:
		pk.staticLit[v] = x
	case *ast.Ident:
		if fn, ok := pk.info.Uses[x].(*types.Func); ok {
			pk.staticFunc[v] = fn
		}
	case *ast.SelectorExpr:
		if pk.info.Selections[x] == nil {
			if fn, ok := pk.info.Uses[x.Sel].(*types.Func); ok {
				pk.staticFunc[v] = fn
			}
		}
	}
}

// bodyless: an assembly function, or one another package provides through a linkname. Its pointer
// arguments escape unless the declaration says //go:noescape.
func (pk *escPkg) bodyless(obj *types.Func, d *ast.FuncDecl) {
	sig := obj.Type().(*types.Signature)
	noescape, uintptrEsc := escPragmas(d)
	s := &escSummary{UintptrEscapes: uintptrEsc}
	add := func(v *types.Var) {
		l := escLeaks{Heap: -1}
		if !noescape && pk.hasPointers(v.Type()) {
			l.Heap = 0
		}
		s.Params = append(s.Params, l)
		if v.Name() != "" && v.Name() != "_" && pk.hasPointers(v.Type()) {
			pk.out.params = append(pk.out.params, escParam{v: v, fn: obj, leaks: l, results: sig.Results()})
		}
	}
	if r := sig.Recv(); r != nil {
		add(r)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		add(sig.Params().At(i))
	}
	pk.out.summary[obj] = s
}

func escPragmas(d *ast.FuncDecl) (noescape, uintptrEscapes bool) {
	if d.Doc == nil {
		return
	}
	for _, c := range d.Doc.List {
		switch strings.TrimSpace(c.Text) {
		case "//go:noescape":
			noescape = true
		case "//go:uintptrescapes":
			uintptrEscapes = true
		}
	}
	return
}

// summaryOf: the summary of a function declared in any package, nil if it has none (then its arguments
// escape).
func (pk *escPkg) summaryOf(fn *types.Func) *escSummary {
	fn = fn.Origin()
	if fn.Pkg() == nil {
		return nil
	}
	if fn.Pkg() == pk.p.tpkg {
		return pk.out.summary[fn]
	}
	if s, ok := pk.sumCache[fn]; ok {
		return s
	}
	var s *escSummary
	if dep := pk.pr.byTypes(fn.Pkg()); dep != nil && dep != pk.p {
		s = pk.pr.escape(dep).summary[fn]
	}
	pk.sumCache[fn] = s
	return s
}

func (pk *escPkg) newFn(parent *escFn) *escFn {
	fn := &escFn{parent: parent}
	if parent != nil {
		fn.generic = parent.generic
		fn.perIter = parent.perIter
		fn.looping = parent.looping
	}
	return fn
}

// batch analyzes one component of the call graph.
func (pk *escPkg) batch(units []*escUnit) {
	b := &escBatch{pr: pk.pr, p: pk.p, info: pk.info, out: pk.out, pk: pk, varLoc: map[*types.Var]*escLoc{},
		fns: map[*types.Func]*escFn{}, litFn: map[*ast.FuncLit]*escFn{}}
	b.heapLoc.escapes = true
	b.heapLoc.leaks.Heap = -1
	type work struct {
		u  *escUnit
		fn *escFn
	}
	var todo []work
	for _, u := range units {
		fn := pk.newFn(nil)
		fn.perIter = true
		if u.decl != nil {
			fn.decl = u.decl
			fn.obj = u.obj
			fn.sig = u.obj.Type().(*types.Signature)
			fn.generic = fn.sig.TypeParams().Len() > 0 || fn.sig.RecvTypeParams().Len() > 0
			if v := pk.info.FileVersions[u.file]; v != "" && version.Compare(v, "go1.22") < 0 {
				fn.perIter = false
			}
			b.initFn(fn)
			b.fns[u.obj] = fn
		}
		todo = append(todo, work{u, fn})
	}
	for _, w := range todo {
		e := &escWalk{escBatch: b, fn: w.fn, loopDepth: 1}
		if w.u.decl != nil {
			w.fn.looping = escLoopingLabels(pk.info, w.u.decl.Body)
			e.block(w.u.decl.Body.List)
			continue
		}
		for _, s := range w.u.specs {
			e.initSpec(s)
		}
	}
	b.flowClosures()
	b.walkAll()
	b.finish(units)
}

// loneLit analyzes a function literal that captures nothing, in a batch of its own.
func (pk *escPkg) loneLit(n *ast.FuncLit, parent *escFn) {
	sig, _ := pk.info.Types[n].Type.(*types.Signature)
	if sig == nil || pk.litSum[n] != nil {
		return
	}
	b := &escBatch{pr: pk.pr, p: pk.p, info: pk.info, out: pk.out, pk: pk, varLoc: map[*types.Var]*escLoc{},
		fns: map[*types.Func]*escFn{}, litFn: map[*ast.FuncLit]*escFn{}}
	b.heapLoc.escapes = true
	b.heapLoc.leaks.Heap = -1
	fn := pk.newFn(nil)
	fn.lit = n
	fn.sig = sig
	fn.generic = parent.generic
	fn.perIter = parent.perIter
	fn.looping = escLoopingLabels(pk.info, n.Body)
	b.initFn(fn)
	b.lits = append(b.lits, fn)
	e := &escWalk{escBatch: b, fn: fn, loopDepth: 1}
	e.block(n.Body.List)
	b.flowClosures()
	b.walkAll()
	b.finish(nil)
	s := &escSummary{}
	for _, l := range fn.params {
		lk := escLeaks{Heap: -1}
		if l.v.Name() != "" && l.v.Name() != "_" && pk.hasPointers(l.v.Type()) {
			lk = l.leaks
			lk.optimize()
		}
		s.Params = append(s.Params, lk)
	}
	pk.litSum[n] = s
}

// initFn makes the locations of a function's parameters and results.
func (b *escBatch) initFn(fn *escFn) {
	sig := fn.sig
	add := func(v *types.Var) {
		l := b.newLoc(fn, 1)
		l.v = v
		l.param = true
		b.varLoc[v] = l
		fn.params = append(fn.params, l)
		b.sizeCheck(l, v.Type(), false)
	}
	if r := sig.Recv(); r != nil {
		add(r)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		add(sig.Params().At(i))
	}
	for i := 0; i < sig.Results().Len(); i++ {
		v := sig.Results().At(i)
		l := b.newLoc(fn, 1)
		l.v = v
		l.paramOut = true
		l.resultIdx = i
		b.varLoc[v] = l
		fn.results = append(fn.results, l)
	}
}

// sizeCheck sends a variable that is too large for the frame to the heap. (Parameters and results are
// where the caller put them.)
func (b *escBatch) sizeCheck(l *escLoc, t types.Type, local bool) {
	if !local || mentionsTypeParam(t) {
		return
	}
	if b.pr.sizes.Sizeof(t) > escMaxStackVarSize {
		l.escapes = true
		b.out.varWhy[l.v] = "too large for stack"
	}
}

func (b *escBatch) finish(units []*escUnit) {
	out := b.out
	for _, l := range b.locs {
		if l.v != nil && l.escapes && !out.heapVar[l.v] {
			out.heapVar[l.v] = true
			out.vars = append(out.vars, l.v)
			if l.fn.generic {
				out.varWhy[l.v] += " gen"
			}
		}
		if l.site != 0 {
			s := &b.sites[l.site-1]
			if l.escapes {
				s.heap = true
			}
		}
	}
	for _, n := range b.force {
		out.heapSite[n] = true
	}
	for _, s := range b.sites {
		if s.heap {
			out.heapSite[s.node] = true
		}
	}
	out.sites = append(out.sites, b.sites...)
	for _, u := range units {
		if u.decl == nil {
			continue
		}
		fn := b.fns[u.obj]
		_, uintptrEsc := escPragmas(u.decl)
		s := &escSummary{UintptrEscapes: uintptrEsc}
		for _, l := range fn.params {
			lk := escLeaks{Heap: -1}
			named := l.v.Name() != "" && l.v.Name() != "_"
			if named && b.pk.hasPointers(l.v.Type()) {
				lk = l.leaks
				lk.optimize()
			}
			s.Params = append(s.Params, lk)
			if named && b.pk.hasPointers(l.v.Type()) {
				out.params = append(out.params, escParam{v: l.v, fn: u.obj, leaks: lk, results: fn.sig.Results(), gen: fn.generic, moved: l.escapes})
			}
		}
		out.summary[u.obj] = s
	}
	// function literals have no summary, but `fc esc` prints what becomes of their parameters, as the
	// stock compiler does
	for _, fn := range b.lits {
		for _, l := range fn.params {
			if l.v.Name() == "" || l.v.Name() == "_" || !b.pk.hasPointers(l.v.Type()) {
				continue
			}
			lk := l.leaks
			lk.optimize()
			var res *types.Tuple
			if fn.sig != nil {
				res = fn.sig.Results()
			}
			out.params = append(out.params, escParam{v: l.v, leaks: lk, results: res, gen: fn.generic, moved: l.escapes})
		}
	}
}

// escLoopingLabels: the labels that a goto jumps back to. Each starts a loop.
func escLoopingLabels(info *types.Info, body *ast.BlockStmt) map[*types.Label]bool {
	var looping map[*types.Label]bool
	seen := map[*types.Label]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.LabeledStmt:
			if l, ok := info.Defs[n.Label].(*types.Label); ok {
				seen[l] = true
			}
		case *ast.BranchStmt:
			if n.Tok == token.GOTO && n.Label != nil {
				if l, ok := info.Uses[n.Label].(*types.Label); ok && seen[l] {
					if looping == nil {
						looping = map[*types.Label]bool{}
					}
					looping[l] = true
				}
			}
		}
		return true
	})
	return looping
}

// ---- types

// hasPointers: could a value of type t hold a pointer? (A type parameter could.)
func (pk *escPkg) hasPointers(t types.Type) bool {
	if t == nil {
		return true
	}
	if r, ok := pk.ptrCache[t]; ok {
		return r
	}
	r := true
	switch u := types.Unalias(t).(type) {
	case *types.TypeParam:
	case *types.Tuple:
		r = false
		for i := 0; i < u.Len(); i++ {
			if pk.hasPointers(u.At(i).Type()) {
				r = true
			}
		}
	default:
		switch u := t.Underlying().(type) {
		case *types.Basic:
			r = u.Kind() == types.String || u.Kind() == types.UnsafePointer || u.Kind() == types.UntypedNil ||
				u.Kind() == types.UntypedString || u.Kind() == types.Invalid
		case *types.Array:
			r = u.Len() != 0 && pk.hasPointers(u.Elem())
		case *types.Struct:
			r = false
			for i := 0; i < u.NumFields(); i++ {
				if pk.hasPointers(u.Field(i).Type()) {
					r = true
					break
				}
			}
		case *types.Pointer:
			// a pointer to memory the collector does not manage is not a pointer to it or to the escape
			// analysis (as in the stock compiler)
			r = !escNotInHeap(u.Elem())
		case *types.Slice:
			r = !escNotInHeap(u.Elem())
		}
	}
	pk.ptrCache[t] = r
	return r
}

// escNotInHeap: a type that includes internal/runtime/sys.NotInHeap: its values are never in the heap or
// on a stack.
func escNotInHeap(t types.Type) bool {
	switch u := types.Unalias(t).(type) {
	case *types.TypeParam:
		return false
	case *types.Named:
		if o := u.Obj(); o.Name() == "nih" && o.Pkg() != nil && o.Pkg().Path() == "internal/runtime/sys" {
			return true
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Array:
		return escNotInHeap(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if escNotInHeap(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

// escDirectIface: is a value of type t stored in an interface as the pointer itself (no allocation)?
func escDirectIface(t types.Type) bool {
	if _, ok := types.Unalias(t).(*types.TypeParam); ok {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	case *types.Array:
		return u.Len() == 1 && escDirectIface(u.Elem())
	case *types.Struct:
		return u.NumFields() == 1 && escDirectIface(u.Field(0).Type())
	}
	return false
}

// escIsIface: an interface type (not a type parameter, whose underlying type is its constraint).
func escIsIface(t types.Type) bool {
	if t == nil {
		return false
	}
	if _, ok := types.Unalias(t).(*types.TypeParam); ok {
		return false
	}
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

func escIsTypeParam(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := types.Unalias(t).(*types.TypeParam)
	return ok
}

// escUnder: the underlying type; of a type parameter, the one underlying type of its type set, or nil.
func escUnder(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	tp, ok := types.Unalias(t).(*types.TypeParam)
	if !ok {
		return t.Underlying()
	}
	it, ok := tp.Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	var core types.Type
	n := 0
	var add func(t types.Type) bool
	add = func(t types.Type) bool {
		switch t := types.Unalias(t).(type) {
		case *types.Union:
			for i := 0; i < t.Len(); i++ {
				if !add(t.Term(i).Type()) {
					return false
				}
			}
			return true
		case *types.TypeParam:
			return false
		}
		u := t.Underlying()
		if ui, ok := u.(*types.Interface); ok {
			for i := 0; i < ui.NumEmbeddeds(); i++ {
				if !add(ui.EmbeddedType(i)) {
					return false
				}
			}
			return true
		}
		n++
		if core == nil {
			core = u
			return true
		}
		return types.Identical(core, u)
	}
	for i := 0; i < it.NumEmbeddeds(); i++ {
		if !add(it.EmbeddedType(i)) {
			return nil
		}
	}
	if n == 0 {
		return nil
	}
	return core
}
