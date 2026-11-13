// load.go: the front end. Every package the target depends on is parsed and type-checked from source by
// go/types, in one address space, dependencies first and in parallel. No export data is read or written.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Pkg is one package of the build: the fields `go list -json` prints, and what the front end adds.
type Pkg struct {
	ImportPath string
	Dir        string
	Name       string
	GoFiles    []string
	SFiles     []string
	Imports    []string
	ImportMap  map[string]string
	EmbedFiles []string
	Export     string // the stock-compiled archive (go list -export)
	Standard   bool
	Module     *struct{ Path, Version, GoVersion string }

	files []*ast.File
	tpkg  *types.Package
	info  *types.Info
	deps  []*Pkg
	once  sync.Once
	err   error

	// two-pass front end: the package as its importers see it (declarations only)
	dpkg     *types.Package
	dOnce    sync.Once
	dinfo    *types.Info   // what the declarations pass recorded; becomes info when the bodies are checked (checkBodies)
	bodies   *types.Bodies // the function bodies the declarations pass left
	perFn    bool          // each function body has an Info of its own (bodyInfo); info holds the package level only
	bodyInfo map[*ast.BlockStmt]*types.Info
	derr     error
	depOnce  sync.Once
	depErr   error

	parseOnce sync.Once
	discOnce  sync.Once
	imps      []string          // import paths as written in the files
	src       map[string][]byte // file bytes read by the self plan, until parsed
	listed    bool              // GoFiles came from a plan file

	srcBytes         int64
	parseNS, checkNS int64

	lnOnce       sync.Once
	checkEnd     time.Time // when the full check finished
	linkname     map[string]string
	linknamed    map[string]bool // local names with any //go:linkname directive
	linknamedStd map[string]bool
	declOnce     sync.Once
	decls        map[*types.Func]*ast.FuncDecl
}

type Program struct {
	fset   *token.FileSet
	byPath map[string]*Pkg
	sizes  types.Sizes
	sem    *prioPool
	plan   *selfPlan  // nil when the packages come from a plan file
	mu     sync.Mutex // guards byPath while packages are being discovered

	tctx         *types.Context // shared by every package: one object per instantiated type
	byTpkg       sync.Map       // *types.Package -> *Pkg
	declPkgs     sync.Map       // *types.Package -> true: the packages made by the declarations-only pass
	linkNames    sync.Map       // types.Type -> its link name, for every compiler of a whole-program build
	typeOwner    sync.Map       // descriptor symbol name -> the *compiler that writes it (whole-program builds)
	instOwner    sync.Map       // instance symbol name -> the *compiler that compiles it
	rtWidth      sync.Map       // runtime function name -> the argument area of its real signature
	rtcOnce      sync.Once
	rtClosure    map[string]bool // the runtime and what it imports, by plain import path
	fullFn       sync.Map        // a declarations-pass *types.Func -> the full pass's
	declNS       int64           // time in declarations-only checks
	byInfo       sync.Map        // *types.Info -> *Pkg
	rtOnce       sync.Once
	localGeneric sync.Map // *types.Named declared in a function -> does its definition use type parameters
	localMu      sync.Mutex
	localInst    map[localKey]*types.Named
	rtDecls      *types.Package // the runtime functions the stock compiler knows, for their signatures
}

func newProgram() *Program {
	return &Program{fset: token.NewFileSet(), byPath: map[string]*Pkg{}, sizes: types.SizesFor("gc", "amd64"), tctx: types.NewContext(),
		sem: newPrioPool(runtime.GOMAXPROCS(0))}
}

// readPlan reads a stream of `go list -json` objects.
func (pr *Program) readPlan(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	// either a stream of objects (go list -json) or one array (the driver's own planner: sb -dumpplan)
	br := bufio.NewReader(f)
	if b, _ := br.Peek(1); len(b) == 1 && b[0] == '[' {
		var ps []*Pkg
		if err := json.NewDecoder(br).Decode(&ps); err != nil {
			return err
		}
		for _, p := range ps {
			p.listed = true
			pr.byPath[p.ImportPath] = p
		}
		return nil
	}
	dec = json.NewDecoder(br)
	for {
		p := new(Pkg)
		if err := dec.Decode(p); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		p.listed = true
		pr.byPath[p.ImportPath] = p
	}
}

type pkgImporter struct {
	pr *Program
	p  *Pkg
}

func (i pkgImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if m, ok := i.p.ImportMap[path]; ok {
		path = m
	}
	d := i.pr.byPath[path]
	if d == nil {
		return nil, fmt.Errorf("%s: import %q is not in the plan", i.p.ImportPath, path)
	}
	tp, err := i.pr.view(d)
	if tp == nil {
		return nil, fmt.Errorf("%s: import %q was not checked (%v)", i.p.ImportPath, path, err)
	}
	return tp, nil
}

// twoPass (off with FC_TWOPASS=0): a package is checked twice. First its declarations only, which is all its
// importers need, so the chain of checks through the import graph is a chain of short ones; then in full,
// for its own code generation, importing the declaration-only packages. The two passes make different
// type objects for the same package: nothing may compare objects across them (symbols are linked by name;
// full() maps a function to the full pass's object).
var twoPass = os.Getenv("FC_TWOPASS") != "0"

// bodiesLater (off with FC_BODIES=0, which gives the two passes above): the declarations pass is the first
// half of the one check of a package, not a second check: it records into the package's Info and keeps
// the function bodies (types.CheckDecls, driver/gotypes), and check() checks those against the same
// *types.Package, the bodies of a big package in several shards at once. So importers and the package's
// own code see the same objects, declarations are checked once, and the longest serial step of a build
// (the full check of its biggest package) is cut into its declarations and an n-th of its bodies.
var bodiesLater = twoPass && os.Getenv("FC_BODIES") != "0"

// newInfo: an Info with the maps fc reads, sized for n bytes of source: growing these maps is a tenth of
// the type checker's time otherwise.
func newInfo(n int) *types.Info {
	return &types.Info{
		// (measured over MinIO's closure: one entry per 14.1, 110, 24.3 and 158 source bytes)
		Types:      make(map[ast.Expr]types.TypeAndValue, n/13),
		Defs:       make(map[*ast.Ident]types.Object, n/95),
		Uses:       make(map[*ast.Ident]types.Object, n/22),
		Implicits:  map[ast.Node]types.Object{},
		Selections: make(map[*ast.SelectorExpr]*types.Selection, n/130),
		Instances:  map[*ast.Ident]types.Instance{},
	}
}

// fnInfos (off with FC_FNINFO=0): what the checker records about a function body goes into an Info of that
// function's own, not into one set of maps for the package. The maps of a big package (five million
// expressions in MinIO's closure, 0.9 GB) do not fit any cache, and both the checker writing them and the
// code generator reading them were a quarter of the build's CPU in hash-map probes; a function's maps are
// small and still in the cache when its code is generated. Not for packages compiled with the escape
// analysis or the runtime's rules (the runtime's closure, FC_ESC=1), which look at a package as a whole.
var fnInfos = os.Getenv("FC_FNINFO") != "0"

// infoFor: the Info that knows about fd's body and signature.
func (p *Pkg) infoFor(fd *ast.FuncDecl) *types.Info {
	if fd != nil && fd.Body != nil {
		if in := p.bodyInfo[fd.Body]; in != nil {
			return in
		}
	}
	return p.info
}

// infos: every Info of the package (the package level first).
func (p *Pkg) infos() []*types.Info {
	out := make([]*types.Info, 0, 1+len(p.bodyInfo))
	out = append(out, p.info)
	for _, f := range p.files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if in := p.bodyInfo[fd.Body]; in != nil {
					out = append(out, in)
				}
			}
		}
	}
	return out
}

// copySig copies what src knows about fd's name, receiver and signature into dst, so that a function's
// Info answers for every node of its declaration.
func copySig(dst, src *types.Info, fd *ast.FuncDecl) {
	if o, ok := src.Defs[fd.Name]; ok {
		dst.Defs[fd.Name] = o
	}
	visit := func(x ast.Node) bool {
		if x == nil {
			return true
		}
		switch x := x.(type) {
		case *ast.Ident:
			if o, ok := src.Defs[x]; ok {
				dst.Defs[x] = o
			}
			if o, ok := src.Uses[x]; ok {
				dst.Uses[x] = o
			}
			if in, ok := src.Instances[x]; ok {
				dst.Instances[x] = in
			}
		case *ast.SelectorExpr:
			if sel, ok := src.Selections[x]; ok {
				dst.Selections[x] = sel
			}
		}
		if e, ok := x.(ast.Expr); ok {
			if tv, ok := src.Types[e]; ok {
				dst.Types[e] = tv
			}
		}
		if o, ok := src.Implicits[x]; ok {
			dst.Implicits[x] = o
		}
		return true
	}
	if fd.Recv != nil {
		ast.Inspect(fd.Recv, visit)
	}
	ast.Inspect(fd.Type, visit)
}

// checkBodies is the second half of p's check (bodiesLater); the caller holds a slot of pr.sem.
func (pr *Program) checkBodies(p *Pkg) {
	var mu sync.Mutex
	onErr := func(err error) {
		mu.Lock()
		if p.err == nil {
			p.err = err
		}
		mu.Unlock()
	}
	b := p.bodies
	n := b.Len()
	k := 1
	if p.srcBytes >= bodyShardMin {
		k = min(runtime.GOMAXPROCS(0), int(p.srcBytes/(bodyShardMin/2)))
	}
	if declOnly {
		n = 0
	}
	// function literals outside functions to shard 0 (they share their variable's declaration, and are
	// recorded with the package level); the functions biggest first, each to the lightest shard; a shard
	// checks its bodies in source order
	shards := make([][]int, k)
	load := make([]int, k)
	var fns []int
	for i := 0; i < n; i++ {
		if b.IsLit(i) {
			shards[0] = append(shards[0], i)
			load[0] += b.Size(i)
		} else {
			fns = append(fns, i)
		}
	}
	if k > 1 {
		sort.SliceStable(fns, func(x, y int) bool { return b.Size(fns[x]) > b.Size(fns[y]) })
	}
	for _, i := range fns {
		m := 0
		for s := 1; s < k; s++ {
			if load[s] < load[m] {
				m = s
			}
		}
		shards[m] = append(shards[m], i)
		load[m] += b.Size(i)
	}
	var fnInfo []*types.Info // by body, when each has its own
	if p.perFn {
		decls := make(map[*ast.BlockStmt]*ast.FuncDecl, len(fns))
		for _, f := range p.files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
					decls[fd.Body] = fd
				}
			}
		}
		fnInfo = make([]*types.Info, n)
		p.bodyInfo = make(map[*ast.BlockStmt]*types.Info, len(fns))
		for _, i := range fns {
			body := b.Body(i)
			fd := decls[body]
			if fd == nil {
				continue // (not a declared function: recorded with the package level)
			}
			in := newInfo(b.Size(i) + 64)
			copySig(in, p.dinfo, fd)
			fnInfo[i] = in
			p.bodyInfo[body] = in
		}
	}
	infos := make([]*types.Info, k) // per shard, when the bodies of a shard share one
	infos[0] = p.dinfo
	par := &parallel{pool: pr.sem, prio: p.srcBytes + 1, helpers: k - 1}
	for s := 0; s < k; s++ {
		sort.Ints(shards[s])
		if s > 0 && !p.perFn {
			infos[s] = newInfo(load[s] * 5 / 4)
		}
		par.run(func() {
			b.Check(s, shards[s], func(i int) *types.Info {
				if fnInfo != nil && fnInfo[i] != nil {
					return fnInfo[i]
				}
				if p.perFn {
					return p.dinfo // (only shard 0 has such bodies)
				}
				return infos[s]
			}, onErr)
		})
	}
	par.wait()
	if k > 1 && !p.perFn {
		// one Info for the package: the shards' maps into the first (a map each at a time)
		d := p.dinfo
		par.run(func() {
			for _, in := range infos[1:] {
				for e, v := range in.Types {
					d.Types[e] = v
				}
			}
		})
		par.run(func() {
			for _, in := range infos[1:] {
				for e, v := range in.Uses {
					d.Uses[e] = v
				}
			}
		})
		par.run(func() {
			for _, in := range infos[1:] {
				for e, v := range in.Defs {
					d.Defs[e] = v
				}
				for e, v := range in.Implicits {
					d.Implicits[e] = v
				}
			}
		})
		par.run(func() {
			for _, in := range infos[1:] {
				for e, v := range in.Selections {
					d.Selections[e] = v
				}
				for e, v := range in.Instances {
					d.Instances[e] = v
				}
			}
		})
		par.wait()
	}
	b.InitOrder(p.dinfo, onErr)
	p.bodies = nil
}

// bodyShardMin: a package with this much source has its bodies checked in shards (one per half of it,
// up to the number of CPUs).
const bodyShardMin = 512 << 10

// view checks d as far as its importers need and returns the package they import.
func (pr *Program) view(d *Pkg) (*types.Package, error) {
	if !twoPass {
		pr.check(d)
		return d.tpkg, d.err
	}
	d.dOnce.Do(func() {
		if d.derr = pr.resolveDeps(d); d.derr != nil {
			return
		}
		// (asked before the slot is taken: inRuntime may parse, which waits for a slot)
		d.perFn = bodiesLater && fnInfos && escWhere != "1" && tidyKnob != "all" && !pr.inRuntime(d)
		pr.sem.acquire(prioDecl + d.srcBytes)
		defer pr.sem.release()
		conf := types.Config{Importer: pkgImporter{pr, d}, Sizes: pr.sizes, GoVersion: goVersionOf(d), Context: pr.tctx,
			IgnoreFuncBodies: true,
			Error: func(err error) {
				if d.derr == nil {
					d.derr = err
				}
			}}
		t0 := time.Now()
		if bodiesLater {
			conf.IgnoreFuncBodies = false
			if d.perFn {
				d.dinfo = newInfo(int(d.srcBytes) / 4) // (the package level only)
			} else {
				d.dinfo = newInfo(int(d.srcBytes))
			}
			d.dpkg, d.bodies, _ = conf.CheckDecls(plainPath(d.ImportPath), pr.fset, d.files, d.dinfo)
		} else {
			d.dpkg, _ = conf.Check(plainPath(d.ImportPath), pr.fset, d.files, nil)
		}
		atomic.AddInt64(&pr.declNS, int64(time.Since(t0)))
		if declStat && time.Since(t0) > 30*time.Millisecond {
			fmt.Fprintf(os.Stderr, "decl %s %d ms\n", d.ImportPath, time.Since(t0).Milliseconds())
		}
		if d.derr != nil {
			d.dpkg = nil
		} else {
			pr.byTpkg.Store(d.dpkg, d)
			if !bodiesLater {
				pr.declPkgs.Store(d.dpkg, true)
			}
		}
	})
	return d.dpkg, d.derr
}

// resolveDeps parses p, finds the packages it imports and has each checked as far as an importer needs.
func (pr *Program) resolveDeps(p *Pkg) error {
	p.depOnce.Do(func() {
		pr.parse(p)
		if p.err != nil {
			p.depErr = p.err
			return
		}
		var wg sync.WaitGroup
		for _, ip := range p.imps {
			if m, ok := p.ImportMap[ip]; ok {
				ip = m
			}
			d := pr.byPath[ip]
			if d == nil {
				p.depErr = fmt.Errorf("%s: import %q is not in the plan", p.ImportPath, ip)
				return
			}
			if dbg := os.Getenv("FC_DBGDEPS"); dbg != "" && plainPath(d.ImportPath) == dbg {
				fmt.Fprintf(os.Stderr, "dep: %q imports %q -> %q (%p)\n", p.ImportPath, ip, d.ImportPath, d)
			}
			p.deps = append(p.deps, d)
			wg.Add(1)
			go func() { defer wg.Done(); pr.view(d) }()
		}
		wg.Wait()
		for _, d := range p.deps {
			if _, err := pr.view(d); err != nil {
				p.depErr = err
				return
			}
		}
	})
	return p.depErr
}

func goVersionOf(p *Pkg) string {
	if p.Module != nil && p.Module.GoVersion != "" {
		return "go" + p.Module.GoVersion
	}
	return ""
}

func plainPath(path string) string {
	if i := strings.Index(path, " ["); i >= 0 {
		return path[:i]
	}
	return path
}

// full: fn as the full pass of its package declares it (fn may come from the declarations-only pass).
func (pr *Program) full(fn *types.Func) *types.Func {
	if !twoPass || fn.Pkg() == nil {
		return fn
	}
	// (not p.dpkg: another goroutine may be in the middle of p's declarations pass while fn is of its full pass)
	if _, decl := pr.declPkgs.Load(fn.Pkg()); !decl {
		return fn
	}
	p := pr.byTypes(fn.Pkg())
	if p == nil {
		return fn
	}
	if v, ok := pr.fullFn.Load(fn); ok {
		return v.(*types.Func)
	}
	pr.check(p)
	if p.tpkg == nil {
		return fn
	}
	var out *types.Func
	if recv := fn.Type().(*types.Signature).Recv(); recv == nil {
		out, _ = p.tpkg.Scope().Lookup(fn.Name()).(*types.Func)
	} else {
		rt := types.Unalias(recv.Type())
		if pt, ok := rt.(*types.Pointer); ok {
			rt = types.Unalias(pt.Elem())
		}
		if n, ok := rt.(*types.Named); ok {
			if tn, ok := p.tpkg.Scope().Lookup(n.Obj().Name()).(*types.TypeName); ok {
				if fnamed, ok := types.Unalias(tn.Type()).(*types.Named); ok {
					for i := 0; i < fnamed.NumMethods(); i++ {
						if m := fnamed.Method(i); m.Name() == fn.Name() {
							out = m
						}
					}
				}
			}
		}
	}
	if out == nil {
		out = fn
	}
	pr.fullFn.Store(fn, out)
	return out
}

// parse reads and parses p's files (once). With a self plan (selfplan.go) it first finds which files of the
// directory belong to the build, from the bytes it then parses.
func (pr *Program) parse(p *Pkg) {
	p.parseOnce.Do(func() {
		pr.sem.acquire(prioParse)
		defer pr.sem.release()
		if pr.plan != nil && !p.listed {
			if err := pr.plan.loadDir(p); err != nil {
				p.err = err
				return
			}
		}
		seen := map[string]bool{}
		for _, name := range p.GoFiles {
			if rtByFc && filepath.Base(name) == "consts_norace.go" && plainPath(p.ImportPath) == "internal/runtime/sys" {
				// fc's frames are larger than the stock compiler's (every variable has a slot), and the
				// runtime's nosplit call chains must fit under the stack guard: an fc-built runtime takes
				// the guard the race detector's build uses (twice the bound; StackGuardMultiplier 2).
				name = filepath.Join(filepath.Dir(name), "consts_race.go")
			}
			path := name
			if !filepath.IsAbs(path) {
				path = filepath.Join(p.Dir, name)
			}
			src := p.src[name]
			if src == nil {
				var err error
				if src, err = os.ReadFile(path); err != nil {
					p.err = err
					return
				}
			}
			p.srcBytes += int64(len(src))
			t0 := time.Now()
			f, err := parser.ParseFile(pr.fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
			p.parseNS += int64(time.Since(t0))
			if err != nil {
				p.err = err
				return
			}
			p.files = append(p.files, f)
			if p.Name == "" {
				p.Name = f.Name.Name
			}
			for _, im := range f.Imports {
				ip := strings.Trim(im.Path.Value, "\"`")
				if ip != "unsafe" && ip != "C" && !seen[ip] {
					seen[ip] = true
					p.imps = append(p.imps, ip)
				}
			}
		}
		p.src = nil
	})
}

// check parses and type-checks p after its dependencies. Safe to call from many goroutines.
func (pr *Program) check(p *Pkg) error {
	p.once.Do(func() {
		if err := pr.resolveDeps(p); err != nil {
			if p.err == nil {
				p.err = err
			}
			return
		}
		if bodiesLater {
			if _, err := pr.view(p); err != nil {
				p.err = err
				return
			}
			pr.sem.acquire(p.srcBytes)
			defer pr.sem.release()
			t0 := time.Now()
			pr.checkBodies(p)
			p.checkNS = int64(time.Since(t0))
			p.checkEnd = time.Now()
			if p.err == nil {
				p.info = p.dinfo
				pr.byInfo.Store(p.info, p)
				p.tpkg = p.dpkg
			}
			return
		}
		pr.sem.acquire(p.srcBytes)
		defer pr.sem.release()
		p.info = newInfo(int(p.srcBytes))
		pr.byInfo.Store(p.info, p)
		gover := ""
		if p.Module != nil && p.Module.GoVersion != "" {
			gover = "go" + p.Module.GoVersion
		}
		conf := types.Config{Importer: pkgImporter{pr, p}, Sizes: pr.sizes, GoVersion: gover, Context: pr.tctx,
			Error: func(err error) {
				if p.err == nil {
					p.err = err
				}
			}}
		conf.IgnoreFuncBodies = declOnly
		path := p.ImportPath
		if i := strings.Index(path, " ["); i >= 0 {
			path = path[:i]
		}
		t0 := time.Now()
		p.tpkg, _ = conf.Check(path, pr.fset, p.files, p.info)
		p.checkNS = int64(time.Since(t0))
		p.checkEnd = time.Now()
		if p.err != nil {
			p.tpkg = nil
		} else {
			pr.byTpkg.Store(p.tpkg, p)
		}
	})
	return p.err
}

// byTypes: the package a go/types package was checked from.
func (pr *Program) byTypes(tp *types.Package) *Pkg {
	if p, ok := pr.byTpkg.Load(tp); ok {
		return p.(*Pkg)
	}
	return nil
}

// declOnly (FC_DECLONLY=1): an experiment: check declarations only and stop after the front end, to time
// the chain of declaration checks through the import graph.
var declStat = os.Getenv("FC_DECLSTAT") != ""

var declOnly = os.Getenv("FC_DECLONLY") != ""

// samePkg: the same package, whichever pass of the front end made the object.
func samePkg(a, b *types.Package) bool {
	return a == b || a != nil && b != nil && a.Path() == b.Path()
}

// rtByFc: the runtime and what it imports are compiled by fc too. FC_RUNTIME=1 turns it on, FC_RUNTIME=0
// off; unset, it is on in `fc build -link` when the linker is fclink (buildMode), off in the mixed mode of
// the tests, where the runtime is the stock compiler's.
var rtByFc = os.Getenv("FC_RUNTIME") != "" && os.Getenv("FC_RUNTIME") != "0"

// linkerPath: fclink's path, or "" for the stock linker. FC_LINKER names it (empty: the stock linker);
// unset, it is the fclink beside this binary, if there is one.
func linkerPath() string {
	if fl, ok := os.LookupEnv("FC_LINKER"); ok {
		return fl
	}
	if exe, err := os.Executable(); err == nil {
		fl := filepath.Join(filepath.Dir(exe), "fclink")
		if st, err := os.Stat(fl); err == nil && !st.IsDir() {
			return fl
		}
	}
	return ""
}
