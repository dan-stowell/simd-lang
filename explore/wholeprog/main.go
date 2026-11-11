// wholeprog: two measurements for a whole-program Go compiler, on a real build's plan
// (the JSON written by `sb -n -plan=self -dumpplan`).
//
//	wholeprog decls  plan.json   time a declarations-only type check of every package (go/types,
//	                             IgnoreFuncBodies), packages in dependency order on all cores
//	wholeprog shapes plan.json   type-check everything with bodies and count the distinct typed shapes
//	                             of expression nodes and of statements (is a stencil back end plausible?)
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Pkg struct {
	ImportPath, Dir, Name string
	GoFiles               []string
	Imports               []string
	ImportMap             map[string]string
	Module                *struct{ GoVersion string }

	files   []*ast.File
	tpkg    *types.Package
	deps    []*Pkg
	users   []*Pkg
	waiting int32
	bytes   int
}

var fset = token.NewFileSet()
var byPath = map[string]*Pkg{}

type imp struct{ p *Pkg }

func (i imp) Import(path string) (*types.Package, error) {
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

func cpu() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func main() {
	mode := os.Args[1]
	b, err := os.ReadFile(os.Args[2])
	if err != nil {
		panic(err)
	}
	var pkgs []*Pkg
	if err := json.Unmarshal(b, &pkgs); err != nil {
		panic(err)
	}
	for _, p := range pkgs {
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

	// parse everything, in parallel
	t0, c0 := time.Now(), cpu()
	var wg sync.WaitGroup
	ch := make(chan *Pkg, len(pkgs))
	for _, p := range pkgs {
		ch <- p
	}
	close(ch)
	var mu sync.Mutex
	total := 0
	for w := 0; w < P; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				for _, f := range p.GoFiles {
					src, err := os.ReadFile(filepath.Join(p.Dir, f))
					if err != nil {
						panic(err)
					}
					af, err := parser.ParseFile(fset, filepath.Join(p.Dir, f), src, parser.SkipObjectResolution)
					if err != nil {
						panic(err)
					}
					p.files = append(p.files, af)
					p.bytes += len(src)
				}
				mu.Lock()
				total += p.bytes
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Printf("parse (go/parser): %d packages, %.1f MB: %.0f ms wall on %d cores, %.1f core-s\n",
		len(pkgs), float64(total)/1e6, float64(time.Since(t0).Milliseconds()), P, (cpu() - c0).Seconds())

	check := func(ignoreBodies bool, visit func(p *Pkg, info *types.Info)) {
		for _, p := range pkgs {
			p.tpkg = nil
			p.waiting = int32(len(p.deps))
		}
		t0, c0 := time.Now(), cpu()
		ready := make(chan *Pkg, len(pkgs))
		for _, p := range pkgs {
			if p.waiting == 0 {
				ready <- p
			}
		}
		var done, nerr int32
		var chain sync.Map // package -> time its check finished (for the critical chain)
		var wg sync.WaitGroup
		for w := 0; w < P; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for p := range ready {
					conf := types.Config{Importer: imp{p}, IgnoreFuncBodies: ignoreBodies, Sizes: types.SizesFor("gc", "amd64"),
						Error: func(err error) { atomic.AddInt32(&nerr, 1) }}
					if p.Module != nil && p.Module.GoVersion != "" {
						v := strings.Split(p.Module.GoVersion, ".")
						if len(v) >= 2 {
							conf.GoVersion = "go" + v[0] + "." + v[1]
						}
					} else if p.Module != nil {
						conf.GoVersion = "go1.16"
					}
					var info *types.Info
					if visit != nil {
						info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
					}
					st := time.Now()
					tp, _ := conf.Check(p.ImportPath, fset, p.files, info)
					p.tpkg = tp
					chain.Store(p, time.Since(st))
					if visit != nil {
						visit(p, info)
					}
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
		wall := time.Since(t0)
		// the longest chain of checks through the import graph: what unlimited cores could not shorten
		memo := map[*Pkg]time.Duration{}
		var longest func(p *Pkg) time.Duration
		longest = func(p *Pkg) time.Duration {
			if v, ok := memo[p]; ok {
				return v
			}
			var best time.Duration
			for _, d := range p.deps {
				best = max(best, longest(d))
			}
			d, _ := chain.Load(p)
			memo[p] = best + d.(time.Duration)
			return memo[p]
		}
		var cp time.Duration
		for _, p := range pkgs {
			cp = max(cp, longest(p))
		}
		what := "with function bodies"
		if ignoreBodies {
			what = "declarations only (IgnoreFuncBodies)"
		}
		fmt.Printf("type check, %s: %.0f ms wall on %d cores, %.1f core-s, longest chain %.0f ms, %d errors\n",
			what, float64(wall.Milliseconds()), P, (cpu() - c0).Seconds(), float64(cp.Milliseconds()), nerr)
	}

	switch mode {
	case "decls":
		check(true, nil)
		check(true, nil)
		check(false, nil)
	case "shapes":
		sh := newShapes()
		check(false, sh.visit)
		sh.report()
	}
}

// ---- shapes

type shapes struct {
	mu    sync.Mutex
	node  map[uint64]int // typed expression nodes: (kind, operator, class of each operand, class of result)
	stmt  map[uint64]int // whole statements: the tree of node shapes, without nested statement lists
	nodeS map[uint64]string
	nNode int
	nStmt int
}

func newShapes() *shapes {
	return &shapes{node: map[uint64]int{}, stmt: map[uint64]int{}, nodeS: map[uint64]string{}}
}

// class: what a code generator needs to know about a value's type to pick a template
func class(t types.Type) string {
	if t == nil {
		return "-"
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Kind() == types.UntypedNil:
			return "nil"
		case u.Info()&types.IsBoolean != 0:
			return "bool"
		case u.Info()&types.IsString != 0:
			return "str"
		case u.Info()&types.IsFloat != 0:
			if u.Kind() == types.Float32 {
				return "f32"
			}
			return "f64"
		case u.Info()&types.IsComplex != 0:
			return "cplx"
		case u.Info()&types.IsInteger != 0:
			s := "i"
			if u.Info()&types.IsUnsigned != 0 {
				s = "u"
			}
			switch u.Kind() {
			case types.Int8, types.Uint8:
				return s + "8"
			case types.Int16, types.Uint16:
				return s + "16"
			case types.Int32, types.Uint32:
				return s + "32"
			default:
				return s + "64"
			}
		case u.Kind() == types.UnsafePointer:
			return "ptr"
		}
		return "basic"
	case *types.Pointer:
		return "ptr"
	case *types.Slice:
		return "slice"
	case *types.Map:
		return "map"
	case *types.Chan:
		return "chan"
	case *types.Signature:
		return "func"
	case *types.Interface:
		if _, ok := t.(*types.TypeParam); ok {
			return "tparam"
		}
		if u.NumMethods() == 0 {
			return "eface"
		}
		return "iface"
	case *types.Struct:
		// a struct's template depends on its size class: registers, small copy, or memmove
		sz := sizeof(t)
		switch {
		case sz < 0:
			return "stgen"
		case sz <= 8:
			return "st8"
		case sz <= 16:
			return "st16"
		case sz <= 64:
			return "st64"
		}
		return "stbig"
	case *types.Array:
		sz := sizeof(t)
		if sz < 0 {
			return "argen"
		}
		if sz <= 16 {
			return "ar16"
		}
		return "arbig"
	case *types.Tuple:
		return fmt.Sprintf("tuple%d", u.Len())
	}
	return "other"
}

// sizeof is -1 for a type whose size is not known here (it mentions a type parameter)
func sizeof(t types.Type) (sz int64) {
	defer func() {
		if recover() != nil {
			sz = -1
		}
	}()
	return types.SizesFor("gc", "amd64").Sizeof(t)
}

type walker struct {
	info  *types.Info
	pkg   *types.Package
	nodes map[uint64]int
	names map[uint64]string
	stmts map[uint64]int
	nn    int
	ns    int
}

func (w *walker) leaf(id *ast.Ident) string {
	obj := w.info.Uses[id]
	if obj == nil {
		obj = w.info.Defs[id]
	}
	switch o := obj.(type) {
	case *types.Var:
		if o.IsField() {
			return "field"
		}
		if o.Parent() == w.pkg.Scope() || (o.Pkg() != nil && o.Pkg() != w.pkg) {
			return "global"
		}
		return "local"
	case *types.Const:
		return "const"
	case *types.Func:
		return "funcname"
	case *types.Nil:
		return "nil"
	case *types.TypeName:
		return "type"
	case *types.Builtin:
		return "builtin:" + o.Name()
	case *types.PkgName:
		return "pkg"
	}
	return "ident"
}

// expr records an expression node and everything below it. It returns the full shape of the tree (used for
// statement shapes) and the short description a parent node sees of this operand: a leaf keeps its kind
// (local, global, const, field...), anything computed is "t" (a temporary), both with the type class.
func (w *walker) expr(e ast.Expr) (full, short string) {
	if e == nil {
		return "_", "_"
	}
	tv := w.info.Types[e]
	c := class(tv.Type)
	if tv.Value != nil {
		return "K:" + c, "K:" + c // a constant, whatever the syntax that produced it
	}
	if tv.IsType() {
		return "T", "T"
	}
	var node string // operator, operand descriptions, result class
	var sub []string
	op := func(name string, operands ...ast.Expr) {
		node = name
		for _, o := range operands {
			f, s := w.expr(o)
			sub = append(sub, f)
			node += " " + s
		}
	}
	switch x := e.(type) {
	case *ast.Ident:
		l := w.leaf(x) + ":" + c
		return l, l
	case *ast.BasicLit:
		return "K:" + c, "K:" + c
	case *ast.ParenExpr:
		return w.expr(x.X)
	case *ast.BinaryExpr:
		op("bin"+x.Op.String(), x.X, x.Y)
	case *ast.UnaryExpr:
		op("un"+x.Op.String(), x.X)
	case *ast.StarExpr:
		op("deref", x.X)
	case *ast.SelectorExpr:
		if _, ok := w.info.Uses[idOf(x.X)].(*types.PkgName); ok {
			l := w.leaf(x.Sel) + ":" + c // pkg.Name is a leaf
			return l, l
		}
		op("sel."+w.leaf(x.Sel), x.X)
	case *ast.IndexExpr:
		op("index", x.X, x.Index)
	case *ast.IndexListExpr:
		node = "inst"
	case *ast.SliceExpr:
		op("slice", x.X, x.Low, x.High, x.Max)
	case *ast.TypeAssertExpr:
		op("assert", x.X)
	case *ast.CallExpr:
		if ftv := w.info.Types[x.Fun]; ftv.IsType() {
			op("conv", x.Args...)
			break
		}
		// a call's template depends on what is called and on the classes of the arguments
		_, callee := w.expr(x.Fun)
		if i := strings.IndexByte(callee, ':'); i > 0 && !strings.HasPrefix(callee, "builtin") {
			callee = callee[:i]
		}
		op("call "+callee, x.Args...)
	case *ast.CompositeLit:
		for _, el := range x.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				w.expr(kv.Value)
			} else {
				w.expr(el)
			}
		}
		node = fmt.Sprintf("lit n=%d", min(len(x.Elts), 8))
	case *ast.FuncLit:
		node = "closure"
	default:
		node = fmt.Sprintf("%T", e)
	}
	node += " ->" + c
	h := fnv.New64a()
	h.Write([]byte(node))
	k := h.Sum64()
	w.nodes[k]++
	if _, ok := w.names[k]; !ok {
		w.names[k] = node
	}
	w.nn++
	return "(" + node + " [" + strings.Join(sub, " ") + "])", "t:" + c
}

func idOf(e ast.Expr) *ast.Ident {
	id, _ := e.(*ast.Ident)
	return id
}

func (w *walker) stmtShape(s ast.Stmt) {
	var sh string
	ex := func(e ast.Expr) string { f, _ := w.expr(e); return f }
	switch x := s.(type) {
	case *ast.ExprStmt:
		sh = "expr " + ex(x.X)
	case *ast.AssignStmt:
		l := make([]string, len(x.Lhs))
		for i, e := range x.Lhs {
			l[i] = ex(e)
		}
		r := make([]string, len(x.Rhs))
		for i, e := range x.Rhs {
			r[i] = ex(e)
		}
		sh = "assign " + x.Tok.String() + " " + strings.Join(l, ",") + " = " + strings.Join(r, ",")
	case *ast.IncDecStmt:
		sh = "incdec " + x.Tok.String() + " " + ex(x.X)
	case *ast.ReturnStmt:
		r := make([]string, len(x.Results))
		for i, e := range x.Results {
			r[i] = ex(e)
		}
		sh = "return " + strings.Join(r, ",")
	case *ast.IfStmt:
		sh = "if " + ex(x.Cond)
	case *ast.ForStmt:
		sh = "for " + ex(x.Cond)
	case *ast.RangeStmt:
		sh = "range " + ex(x.X)
	case *ast.SwitchStmt:
		sh = "switch " + ex(x.Tag)
	case *ast.TypeSwitchStmt:
		sh = "typeswitch"
	case *ast.CaseClause:
		c := make([]string, len(x.List))
		for i, e := range x.List {
			c[i] = ex(e)
		}
		sh = "case " + strings.Join(c, ",")
	case *ast.GoStmt:
		sh = "go " + ex(x.Call)
	case *ast.DeferStmt:
		sh = "defer " + ex(x.Call)
	case *ast.SendStmt:
		sh = "send " + ex(x.Chan) + " " + ex(x.Value)
	case *ast.BranchStmt:
		sh = "branch " + x.Tok.String()
	case *ast.DeclStmt:
		sh = "decl"
		ast.Inspect(x, func(n ast.Node) bool {
			if vs, ok := n.(*ast.ValueSpec); ok {
				for _, v := range vs.Values {
					sh += " " + ex(v)
				}
			}
			return true
		})
	case *ast.SelectStmt:
		sh = "select"
	case *ast.CommClause:
		sh = "comm"
	case *ast.LabeledStmt, *ast.BlockStmt, *ast.EmptyStmt:
		return
	default:
		sh = fmt.Sprintf("%T", s)
	}
	h := fnv.New64a()
	h.Write([]byte(sh))
	w.stmts[h.Sum64()]++
	w.ns++
}

func (sh *shapes) visit(p *Pkg, info *types.Info) {
	w := &walker{info: info, pkg: p.tpkg, nodes: map[uint64]int{}, names: map[uint64]string{}, stmts: map[uint64]int{}}
	for _, f := range p.files {
		ast.Inspect(f, func(n ast.Node) bool {
			if s, ok := n.(ast.Stmt); ok {
				w.stmtShape(s)
			}
			return true
		})
	}
	sh.mu.Lock()
	for k, v := range w.nodes {
		sh.node[k] += v
		if _, ok := sh.nodeS[k]; !ok {
			sh.nodeS[k] = w.names[k]
		}
	}
	for k, v := range w.stmts {
		sh.stmt[k] += v
	}
	sh.nNode += w.nn
	sh.nStmt += w.ns
	sh.mu.Unlock()
}

func curve(m map[uint64]int, total int, what string) []uint64 {
	type kv struct {
		k uint64
		v int
	}
	var l []kv
	for k, v := range m {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].v > l[j].v })
	fmt.Printf("%s: %d occurrences, %d distinct shapes\n", what, total, len(l))
	acc := 0
	next := 16
	for i, e := range l {
		acc += e.v
		if i+1 == next || i+1 == len(l) {
			fmt.Printf("  top %7d shapes cover %6.2f%%\n", i+1, 100*float64(acc)/float64(total))
			next *= 4
		}
	}
	keys := make([]uint64, 0, 12)
	for i := 0; i < len(l) && i < 12; i++ {
		keys = append(keys, l[i].k)
	}
	return keys
}

func (sh *shapes) report() {
	top := curve(sh.node, sh.nNode, "expression nodes (operator, operand shapes, result class)")
	for _, k := range top {
		fmt.Printf("    %8d  %s\n", sh.node[k], sh.nodeS[k])
	}
	curve(sh.stmt, sh.nStmt, "statements (whole typed tree of the statement, nested statement lists excluded)")
}
