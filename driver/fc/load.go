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
	if d.tpkg == nil {
		return nil, fmt.Errorf("%s: import %q was not checked (%v)", i.p.ImportPath, path, d.err)
	}
	return d.tpkg, nil
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
		pr.parse(p)
		if p.err != nil {
			return
		}
		imps := p.imps
		var wg sync.WaitGroup
		for _, ip := range imps {
			if m, ok := p.ImportMap[ip]; ok {
				ip = m
			}
			d := pr.byPath[ip]
			if d == nil {
				p.err = fmt.Errorf("%s: import %q is not in the plan", p.ImportPath, ip)
				return
			}
			p.deps = append(p.deps, d)
			wg.Add(1)
			go func() { defer wg.Done(); pr.check(d) }()
		}
		wg.Wait()
		for _, d := range p.deps {
			if d.err != nil {
				p.err = d.err
				return
			}
		}
		pr.sem <- struct{}{}
		defer func() { <-pr.sem }()
		// sized from the source: growing these maps is a tenth of the type checker's time otherwise
		n := int(p.srcBytes)
		p.info = &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue, n/9),
			Defs:       make(map[*ast.Ident]types.Object, n/40),
			Uses:       make(map[*ast.Ident]types.Object, n/14),
			Implicits:  map[ast.Node]types.Object{},
			Selections: make(map[*ast.SelectorExpr]*types.Selection, n/40),
			Instances:  map[*ast.Ident]types.Instance{},
		}
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
