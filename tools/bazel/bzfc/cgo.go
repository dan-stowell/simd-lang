// cgo.go: cgo packages in a configuration with cgo on (rules_go's pure=off with a C toolchain, BuildBuddy's
// default). The bb CLI needs it: go-tree-sitter is C behind cgo, with no pure-Go files to fall back to.
//
// fc compiles Go only and fclink links Go objects only. So a closure with cgo packages is built in fc's mixed
// mode: the cgo packages and everything they import (the runtime included) are the stock compiler's, built
// by the go command of -go's GOROOT (GOPATH mode, over a farm of symlinks to the packages' directories); fc
// compiles the rest, type-checking the cgo packages from the files cgo generated (go list -compiled), and the
// stock linker links in external mode (C objects, libc). Different from Bazel's build of the same target: the
// C compiler is the system's (gcc, -O0 like rules_go's fastbuild) instead of the workspace's hermetic LLVM
// toolchain, whose C runtimes (crt, glibc stubs, libunwind, compiler-rt: a few hundred CppCompile actions)
// are not built; and the standard library is built with netgo,osusergo (net and os/user without their cgo
// halves), as in the rest of fc's builds.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type stockSet struct {
	once    sync.Once
	entries map[string]json.RawMessage // plan entries (fc's Pkg fields) by import path
	keep    []string                   // FC_KEEP_STOCK
	env     []string                   // for fc: the go command and linker it runs
	err     error
}

var (
	stockMu sync.Mutex
	stockBy = map[[32]byte]*stockSet{}
)

// stockCgo: the plan entries of the cgo packages of pkgs and of everything they import, compiled by the go
// command, and the environment fc then needs. Once per set of cgo packages.
func stockCgo(pkgs []*Pkg) (*stockSet, error) {
	var roots []*Pkg
	for _, p := range pkgs {
		if p.cgo {
			roots = append(roots, p)
		}
	}
	if len(roots) == 0 {
		return nil, nil
	}
	// the Bazel packages of the cgo packages' closure
	seen := map[*Pkg]bool{}
	var order []*Pkg
	var walk func(p *Pkg)
	walk = func(p *Pkg) {
		if seen[p] {
			return
		}
		seen[p] = true
		for _, d := range p.deps {
			walk(d)
		}
		order = append(order, p)
	}
	var names []string
	for _, r := range roots {
		walk(r)
		names = append(names, r.ImportPath)
	}
	sort.Strings(names)
	key := sha256.Sum256([]byte(strings.Join(names, "\n")))
	stockMu.Lock()
	s := stockBy[key]
	if s == nil {
		s = &stockSet{}
		stockBy[key] = s
	}
	stockMu.Unlock()
	s.once.Do(func() { s.err = s.build(order, names, fmt.Sprintf("%x", key[:6])) })
	return s, s.err
}

func (s *stockSet) build(order []*Pkg, roots []string, tag string) error {
	t0 := time.Now()
	gopath := filepath.Join(*flagWork, "gopath."+tag)
	src := filepath.Join(gopath, "src")
	// the farm: GOPATH/src/<import path>/ holds symlinks to the files of the package's directory (nested
	// import paths are often separate Bazel repositories: cloud.google.com/go/auth and .../auth/oauth2adapt)
	// A subdirectory no package of the set is in or under is linked whole (headers: tree_sitter/parser.h).
	under := map[string]bool{}
	for _, p := range order {
		for d := p.ImportPath; d != "." && d != "/"; d = filepath.Dir(d) {
			under[d] = true
		}
	}
	for _, p := range order {
		if ip := first(p.a.args["-importpath"]); ip != "" && ip != p.ImportPath {
			return fmt.Errorf("cgo closure: %s has importmap %s (not handled)", ip, p.ImportPath)
		}
		dst := filepath.Join(src, p.ImportPath)
		if err := os.MkdirAll(dst, 0o777); err != nil {
			return err
		}
		// Bazel's sources of the package (generated ones included), at their place under the package's source
		// directory, else by name
		srcDir := ""
		for _, s := range p.a.args["-src"] {
			if !strings.HasPrefix(s, "bazel-out/") {
				srcDir = filepath.Dir(abs(s))
				break
			}
		}
		have := map[string]bool{}
		place := func(rel, target string, cp bool) error {
			if have[rel] {
				return nil
			}
			have[rel] = true
			have[strings.SplitN(rel, "/", 2)[0]] = true
			d := filepath.Join(dst, rel)
			os.MkdirAll(filepath.Dir(d), 0o777)
			if !cp {
				return os.Symlink(target, d)
			}
			b, err := os.ReadFile(target) // (the go command embeds no symlinks)
			if err != nil {
				return err
			}
			return os.WriteFile(d, b, 0o666)
		}
		for _, s := range p.a.args["-src"] {
			x := abs(s)
			rel := filepath.Base(x)
			if srcDir != "" && strings.HasPrefix(x, srcDir+"/") {
				rel = strings.TrimPrefix(x, srcDir+"/")
			}
			if err := place(rel, x, false); err != nil {
				return err
			}
		}
		for _, rel := range p.EmbedFiles {
			if err := place(rel, filepath.Join(p.Dir, rel), true); err != nil {
				return err
			}
		}
		// the other files of the source directory but Go files (C a go_library has from a cc_library in cdeps:
		// go-tree-sitter's alloc.c), and its subdirectories no package of the set is in or under, whole
		// (headers: go-tree-sitter/golang's tree_sitter/parser.h)
		if srcDir != "" {
			ents, _ := os.ReadDir(srcDir)
			for _, e := range ents {
				n := e.Name()
				st, err := os.Stat(filepath.Join(srcDir, n))
				if err != nil || have[n] || st.IsDir() && under[p.ImportPath+"/"+n] || !st.IsDir() && strings.HasSuffix(n, ".go") {
					continue
				}
				if err := place(n, filepath.Join(srcDir, n), false); err != nil {
					return err
				}
			}
		}
	}
	gobin := filepath.Dir(*flagGo)
	s.env = []string{"PATH=" + gobin + ":" + os.Getenv("PATH"), "GOPATH=" + gopath, "GO111MODULE=off", "CGO_ENABLED=1",
		"GOFLAGS=-tags=netgo,osusergo", "GOCACHE=" + filepath.Join(*flagWork, "gocache"), "GOTOOLCHAIN=local", "CGO_CFLAGS=-O0",
		"GOOS=linux", "GOARCH=amd64"}
	// (-gcflags=all=-l: as fc's own stock packages, go list -export in driver/fc/build.go)
	cmd := exec.Command(*flagGo, append([]string{"list", "-deps", "-export", "-compiled", "-json", "-gcflags=all=-l"}, roots...)...)
	cmd.Dir = src
	cmd.Env = append(os.Environ(), s.env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outb, err := cmd.Output()
	if err != nil {
		// (C compilers warn a lot: the lines that are not warnings, and all of it in a file)
		log := filepath.Join(gopath, "go-list.err")
		os.WriteFile(log, stderr.Bytes(), 0o666)
		var errs []string
		for _, l := range strings.Split(stderr.String(), "\n") {
			if strings.Contains(l, "error") || strings.HasPrefix(l, "#") || strings.Contains(l, "cannot") || strings.Contains(l, "undefined") {
				errs = append(errs, l)
			}
		}
		if len(errs) > 30 {
			errs = errs[:30]
		}
		return fmt.Errorf("go list -export (cgo closure: %s): %v (%s)\n%s", strings.Join(roots, " "), err, log, strings.Join(errs, "\n"))
	}
	s.entries = map[string]json.RawMessage{}
	dec := json.NewDecoder(bytes.NewReader(outb))
	n := 0
	for dec.More() {
		var e struct {
			ImportPath, Dir, Name, Export string
			Standard                      bool
			GoFiles, CgoFiles             []string
			CompiledGoFiles, Imports      []string
			ImportMap                     map[string]string `json:",omitempty"`
			Error                         *struct{ Err string }
		}
		if err := dec.Decode(&e); err != nil {
			return err
		}
		if e.Error != nil {
			return fmt.Errorf("go list %s: %s", e.ImportPath, e.Error.Err)
		}
		if e.ImportPath == "unsafe" {
			continue
		}
		if e.Export == "" {
			return fmt.Errorf("go list %s: no export file", e.ImportPath)
		}
		files := e.CompiledGoFiles
		if len(files) == 0 {
			files = e.GoFiles
		}
		gofiles := files // (cgo's output is in the cache, under names without .go)
		imps := map[string]bool{}
		for _, ip := range e.Imports {
			if ip != "C" {
				imps[ip] = true
			}
		}
		if len(e.CgoFiles) > 0 {
			// what cgo's output imports beyond the package's own imports
			for _, ip := range []string{"runtime/cgo", "syscall", "unsafe"} {
				if ip != e.ImportPath && !(e.ImportPath == "runtime/cgo" && ip == "syscall") {
					imps[ip] = true
				}
			}
		}
		var imports []string
		for ip := range imps {
			imports = append(imports, ip)
		}
		sort.Strings(imports)
		b, _ := json.Marshal(struct {
			ImportPath, Dir, Name string
			GoFiles, Imports      []string
			ImportMap             map[string]string `json:",omitempty"`
			Export                string
			Standard              bool
		}{e.ImportPath, e.Dir, e.Name, gofiles, imports, e.ImportMap, e.Export, e.Standard})
		s.entries[e.ImportPath] = b
		n++
	}
	s.keep = append(append([]string{}, roots...), "runtime/cgo")
	s.env = append(s.env, "FC_LINKER=", "FC_KEEP_STOCK="+strings.Join(s.keep, ","), "FC_LINKFLAGS=-linkmode=external")
	fmt.Fprintf(os.Stderr, "bzfc: cgo: %d packages (%s) and their closure, %d packages, compiled by the go command in %.0f ms\n",
		len(roots), strings.Join(roots, " "), n, ms(time.Since(t0)))
	return nil
}
