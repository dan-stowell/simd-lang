// load.go: the front end. Every package the target depends on is parsed and type-checked from source by
// go/types, in one address space, dependencies first and in parallel. No export data is read or written.
package main

import (
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
	Standard   bool
	Module     *struct{ Path, Version, GoVersion string }

	files []*ast.File
	tpkg  *types.Package
	info  *types.Info
	deps  []*Pkg
	once  sync.Once
	err   error

	lnOnce   sync.Once
	linkname map[string]string
}

type Program struct {
	fset   *token.FileSet
	byPath map[string]*Pkg
	sizes  types.Sizes
	sem    chan struct{}

	rtOnce  sync.Once
	rtDecls *types.Package // the runtime functions the stock compiler knows, for their signatures
}

func newProgram() *Program {
	return &Program{fset: token.NewFileSet(), byPath: map[string]*Pkg{}, sizes: types.SizesFor("gc", "amd64"),
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
	for {
		p := new(Pkg)
		if err := dec.Decode(p); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
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

// check parses and type-checks p after its dependencies. Safe to call from many goroutines.
func (pr *Program) check(p *Pkg) error {
	p.once.Do(func() {
		// parse first: the imports that matter are the ones in the files
		pr.sem <- struct{}{}
		seen := map[string]bool{}
		var imps []string
		for _, name := range p.GoFiles {
			path := name
			if !filepath.IsAbs(path) {
				path = filepath.Join(p.Dir, name)
			}
			f, err := parser.ParseFile(pr.fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				p.err = err
				<-pr.sem
				return
			}
			p.files = append(p.files, f)
			for _, im := range f.Imports {
				ip := strings.Trim(im.Path.Value, "\"`")
				if ip != "unsafe" && ip != "C" && !seen[ip] {
					seen[ip] = true
					imps = append(imps, ip)
				}
			}
		}
		<-pr.sem
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
		p.info = &types.Info{
			Types:        map[ast.Expr]types.TypeAndValue{},
			Defs:         map[*ast.Ident]types.Object{},
			Uses:         map[*ast.Ident]types.Object{},
			Implicits:    map[ast.Node]types.Object{},
			Selections:   map[*ast.SelectorExpr]*types.Selection{},
			Instances:    map[*ast.Ident]types.Instance{},
			Scopes:       map[ast.Node]*types.Scope{},
			FileVersions: map[*ast.File]string{},
		}
		gover := ""
		if p.Module != nil && p.Module.GoVersion != "" {
			gover = "go" + p.Module.GoVersion
		}
		conf := types.Config{Importer: pkgImporter{pr, p}, Sizes: pr.sizes, GoVersion: gover,
			Error: func(err error) {
				if p.err == nil {
					p.err = err
				}
			}}
		path := p.ImportPath
		if i := strings.Index(path, " ["); i >= 0 {
			path = path[:i]
		}
		p.tpkg, _ = conf.Check(path, pr.fset, p.files, p.info)
		if p.err != nil {
			p.tpkg = nil
		}
	})
	return p.err
}
