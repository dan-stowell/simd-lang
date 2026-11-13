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
	dpkg    *types.Package
	dOnce   sync.Once
	derr    error
	depOnce sync.Once
	depErr  error

	parseOnce sync.Once
	discOnce  sync.Once
	imps      []string          // import paths as written in the files
	src       map[string][]byte // file bytes read by the self plan, until parsed
	listed    bool              // GoFiles came from a plan file

	srcBytes         int64
	parseNS, checkNS int64

	lnOnce       sync.Once
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
	sem    chan struct{}
	plan   *selfPlan  // nil when the packages come from a plan file
	mu     sync.Mutex // guards byPath while packages are being discovered

	tctx         *types.Context // shared by every package: one object per instantiated type
	byTpkg       sync.Map       // *types.Package -> *Pkg
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
		sem: make(chan struct{}, runtime.GOMAXPROCS(0))}
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
		pr.sem <- struct{}{}
		defer func() { <-pr.sem }()
		conf := types.Config{Importer: pkgImporter{pr, d}, Sizes: pr.sizes, GoVersion: goVersionOf(d), Context: pr.tctx,
			IgnoreFuncBodies: true,
			Error: func(err error) {
				if d.derr == nil {
					d.derr = err
				}
			}}
		t0 := time.Now()
		d.dpkg, _ = conf.Check(plainPath(d.ImportPath), pr.fset, d.files, nil)
		atomic.AddInt64(&pr.declNS, int64(time.Since(t0)))
		if d.derr != nil {
			d.dpkg = nil
		} else {
			pr.byTpkg.Store(d.dpkg, d)
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
	p := pr.byTypes(fn.Pkg())
	if p == nil || fn.Pkg() != p.dpkg {
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
		pr.sem <- struct{}{}
		defer func() { <-pr.sem }()
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
		pr.sem <- struct{}{}
		defer func() { <-pr.sem }()
		// sized from the source: growing these maps is a tenth of the type checker's time otherwise
		n := int(p.srcBytes)
		p.info = &types.Info{
			// (measured over MinIO's closure: one entry per 14.1, 110, 24.3 and 158 source bytes)
			Types:      make(map[ast.Expr]types.TypeAndValue, n/13),
			Defs:       make(map[*ast.Ident]types.Object, n/95),
			Uses:       make(map[*ast.Ident]types.Object, n/22),
			Implicits:  map[ast.Node]types.Object{},
			Selections: make(map[*ast.SelectorExpr]*types.Selection, n/130),
			Instances:  map[*ast.Ident]types.Instance{},
		}
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
