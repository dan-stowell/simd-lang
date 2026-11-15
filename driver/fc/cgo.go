// cgo.go: packages that use cgo, in `fc build -link` (the go command's (*Builder).cgo and build, in
// cmd/go/internal/work/exec.go, are the specification). For each such package of the plan (go list -json
// with CGO_ENABLED=1: CgoFiles, CFiles, CgoCFLAGS, ...):
//
//  1. `go tool cgo -objdir D -importpath P -- CPPFLAGS CFLAGS cgofiles` writes _cgo_gotypes.go, *.cgo1.go
//     (the Go side), *.cgo2.c, _cgo_export.c/.h and _cgo_main.c; runtime/cgo gets -import_runtime_cgo=false
//     -import_syscall=false, as from the go command.
//  2. The package's Go files are its plain Go files and cgo's Go output; fc compiles them as any package (the
//     front end waits for step 1: parse). What that code needs from fc: //go:cgo_unsafe_args (the function
//     is ABI0: static.go), the //go:cgo_* directives in the object for the linker (cgoPragmas, obj.go).
//  3. gcc compiles cgo's C output and the package's own C, C++ and gcc assembly (.S) files, with the flags
//     of exec.go's compilerCmd and the package's #cgo flags, beside the rest of the build.
//  4. The package's archive holds fc's Go object, the Go assembler's objects and the C objects (as `go tool
//     pack r` leaves it): the stock linker, in external mode, hands the C objects to the host linker.
//
// Not done: the dynimport step (_cgo_main.c linked into _cgo_.o, `cgo -dynimport` -> _cgo_import.go). Its
// output is //go:cgo_import_dynamic and //go:cgo_dynamic_linker directives, which only an internal link
// uses: with -linkmode=external the stock linker returns from adddynlib and Adddynsym at once (ld/go.go),
// and the dynamic symbols the C objects use are left to the host linker, with the libraries the
// //go:cgo_ldflag directives name. So a cgo program from fc must be linked externally (buildMode checks).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"cmd/internal/objabi"
)

// cgoRun: the cgo step of one package.
type cgoRun struct {
	gen     chan struct{} // closed when cgo's Go output is written (and p.GoFiles names it)
	genErr  error
	objsWG  sync.WaitGroup // the C compiles
	objs    []string       // in the order of the go command's archive
	objErr  atomic.Pointer[error]
	objdir  string
	gccS    []string // assembly files for gcc
	cflags  []string // CPPFLAGS + CFLAGS (with -I objdir), for gcc
	cxxflag []string
}

// cgoImports: a cgo package's imports as the go command has them for the compile: not "C", and what cgo's
// Go output imports beyond the package's own (runtime/cgo, syscall, unsafe).
func (p *Pkg) cgoImports() {
	if !p.usesCgo() {
		return
	}
	imps := []string{}
	for _, ip := range p.Imports {
		if ip != "C" {
			imps = append(imps, ip)
		}
	}
	for _, ip := range []string{"runtime/cgo", "syscall", "unsafe"} {
		if plainPath(p.ImportPath) == "runtime/cgo" && ip != "unsafe" {
			continue
		}
		if !contains(imps, ip) {
			imps = append(imps, ip)
		}
	}
	p.Imports = imps
}

// usesCgo: does the plan say p has cgo files?
func (p *Pkg) usesCgo() bool { return len(p.CgoFiles) > 0 }

var (
	cgoRuns  atomic.Int64 // packages through cgo
	cgoCCs   atomic.Int64 // C compiler runs
	ccSem    chan struct{}
	ccSemOne sync.Once
)

func envList(key, def string) []string {
	v := os.Getenv(key)
	if v == "" {
		v = def
	}
	return strings.Fields(v)
}

// startCgo starts the cgo step of every cgo package of pkgs; dir is the build's object directory.
func startCgo(pkgs []*Pkg, dir string) {
	ccSemOne.Do(func() { ccSem = make(chan struct{}, runtime.GOMAXPROCS(0)) })
	for _, p := range pkgs {
		if !p.usesCgo() || p.cgo != nil {
			continue
		}
		r := &cgoRun{gen: make(chan struct{})}
		p.cgo = r
		// assembly: gcc's (.S, and .s in a cgo package), except runtime/cgo's Go assembly (not gcc_*)
		var goAsm []string
		for _, f := range p.SFiles {
			if p.Standard && p.ImportPath == "runtime/cgo" && !strings.HasPrefix(f, "gcc_") {
				goAsm = append(goAsm, f)
			} else {
				r.gccS = append(r.gccS, f)
			}
		}
		p.SFiles = goAsm
		r.objdir = filepath.Join(dir, "cgo", strings.ReplaceAll(objabi.PathToPrefix(p.ImportPath), "/", "__")) + "/"
		go func() {
			defer close(r.gen)
			r.genErr = r.generate(p)
			if r.genErr == nil {
				r.compileC(p)
			}
		}()
	}
}

func contains(s []string, x string) bool {
	for _, y := range s {
		if y == x {
			return true
		}
	}
	return false
}

// generate: step 1.
func (r *cgoRun) generate(p *Pkg) error {
	cgoRuns.Add(1)
	if len(p.CgoPkgConfig) > 0 || len(p.SwigFiles) > 0 || len(p.SwigCXXFiles) > 0 || len(p.FFiles) > 0 {
		return fmt.Errorf("cgo: pkg-config, SWIG and Fortran are not handled (%s)", p.ImportPath)
	}
	if err := os.MkdirAll(r.objdir, 0o777); err != nil {
		return err
	}
	cppflags := append(envList("CGO_CPPFLAGS", ""), p.CgoCPPFLAGS...)
	cflags := append(envList("CGO_CFLAGS", "-O2 -g"), p.CgoCFLAGS...)
	cxxflags := append(envList("CGO_CXXFLAGS", "-O2 -g"), p.CgoCXXFLAGS...)
	ldflags := append(envList("CGO_LDFLAGS", "-O2 -g"), p.CgoLDFLAGS...)
	if len(p.MFiles) > 0 {
		ldflags = append(ldflags, "-lobjc")
	}
	cppflags = append(cppflags, "-I", r.objdir)
	r.cflags = append(append([]string{}, cppflags...), cflags...)
	r.cxxflag = append(append([]string{}, cppflags...), cxxflags...)
	var cgoflags []string
	if p.Standard && p.ImportPath == "runtime/cgo" {
		cgoflags = append(cgoflags, "-import_runtime_cgo=false")
	}
	if p.Standard && (p.ImportPath == "runtime/race" || p.ImportPath == "runtime/msan" || p.ImportPath == "runtime/cgo" || p.ImportPath == "runtime/asan") {
		cgoflags = append(cgoflags, "-import_syscall=false")
	}
	env := append(os.Environ(), "GOTOOLCHAIN=local", "TERM=dumb")
	if len(ldflags) > 0 {
		q := make([]string, len(ldflags))
		for i, f := range ldflags {
			q[i] = strconv.Quote(f)
		}
		cgoflags = append(cgoflags, "-ldflags="+strings.Join(q, " "))
		env = append(env, "CGO_LDFLAGS=")
	}
	var files []string
	for _, f := range p.CgoFiles {
		files = append(files, absIn(p.Dir, f))
	}
	args := append([]string{"-objdir", r.objdir, "-importpath", p.ImportPath}, cgoflags...)
	args = append(append(append(args, "--"), cppflags...), cflags...)
	args = append(args, files...)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "pkg/tool/linux_amd64/cgo"), args...)
	cmd.Dir = p.Dir
	cmd.Env = env
	atomic.AddInt64(&toolStarts, 1)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cgo: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	// the Go files: the plain ones, then cgo's (the go command's order)
	gofiles := append([]string{}, p.GoFiles...)
	gofiles = append(gofiles, r.objdir+"_cgo_gotypes.go")
	for _, f := range p.CgoFiles {
		gofiles = append(gofiles, r.objdir+strings.TrimSuffix(filepath.Base(f), ".go")+".cgo1.go")
	}
	p.GoFiles = gofiles
	return nil
}

func absIn(dir, f string) string {
	if filepath.IsAbs(f) {
		return f
	}
	return filepath.Join(dir, f)
}

// compileC: step 3, started; wait with objects.
func (r *cgoRun) compileC(p *Pkg) {
	type job struct {
		src   string
		cxx   bool
		gas   bool
		flags []string
	}
	var jobs []job
	jobs = append(jobs, job{src: r.objdir + "_cgo_export.c", flags: r.cflags})
	for _, f := range p.CgoFiles {
		jobs = append(jobs, job{src: r.objdir + strings.TrimSuffix(filepath.Base(f), ".go") + ".cgo2.c", flags: r.cflags})
	}
	for _, f := range r.gccS {
		jobs = append(jobs, job{src: absIn(p.Dir, f), gas: true, flags: r.cflags})
	}
	for _, f := range append(append([]string{}, p.CFiles...), p.MFiles...) {
		jobs = append(jobs, job{src: absIn(p.Dir, f), flags: r.cflags})
	}
	for _, f := range p.CXXFiles {
		jobs = append(jobs, job{src: absIn(p.Dir, f), cxx: true, flags: r.cxxflag})
	}
	for i, j := range jobs {
		out := r.objdir + fmt.Sprintf("_x%03d.o", i+1)
		r.objs = append(r.objs, out)
		r.objsWG.Add(1)
		go func() {
			defer r.objsWG.Done()
			ccSem <- struct{}{}
			defer func() { <-ccSem }()
			if err := r.cc(p, j.src, out, j.flags, j.cxx, j.gas); err != nil {
				r.objErr.CompareAndSwap(nil, &err)
			}
		}()
	}
}

// cc: exec.go's ccompile with compilerCmd's flags (gcc -I <package dir> -fPIC -m64 -pthread ...).
func (r *cgoRun) cc(p *Pkg, src, out string, flags []string, cxx, gas bool) error {
	cgoCCs.Add(1)
	if gas {
		data, err := os.ReadFile(src)
		if err == nil {
			for _, kw := range []string{"TEXT", "DATA", "GLOBL"} {
				if bytes.HasPrefix(data, []byte(kw)) || bytes.Contains(data, []byte("\n"+kw)) {
					return fmt.Errorf("package using cgo has Go assembly file %s", filepath.Base(src))
				}
			}
		}
	}
	compiler := envList("CC", "gcc")
	if cxx {
		compiler = envList("CXX", "g++")
	}
	args := append(append([]string{}, compiler[1:]...), "-I", p.Dir, "-fPIC", "-m64", "-pthread", "-Wl,--no-gc-sections",
		"-fmessage-length=0", "-ffile-prefix-map="+strings.TrimSuffix(r.objdir, "/")+"=/tmp/go-build", "-gno-record-gcc-switches")
	args = append(append(args, flags...), "-o", out, "-c", filepath.Base(src))
	cmd := exec.Command(compiler[0], args...)
	cmd.Dir = filepath.Dir(src)
	cmd.Env = append(os.Environ(), "TERM=dumb")
	atomic.AddInt64(&toolStarts, 1)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %v\n%s", compiler[0], filepath.Base(src), err, strings.TrimSpace(string(b)))
	}
	return nil
}

// waitGen: the cgo step's Go output is there (the front end calls it before parsing p).
func (p *Pkg) waitGen() error {
	if p.cgo == nil {
		return nil
	}
	<-p.cgo.gen
	return p.cgo.genErr
}

// cgoObjects: the C objects of p, once compiled.
func (p *Pkg) cgoObjects() ([]string, error) {
	if p.cgo == nil {
		return nil, nil
	}
	if err := p.waitGen(); err != nil {
		return nil, err
	}
	p.cgo.objsWG.Wait()
	if e := p.cgo.objErr.Load(); e != nil {
		return nil, *e
	}
	return p.cgo.objs, nil
}

// ---- the //go:cgo_* directives (cmd/compile/internal/noder: pragcgo; gc/obj.go writes them)

// cgoPragmas: the package's //go:cgo_* directives in source order, as the stock compiler records them:
// the export ones with the definition ABI of the function they name (ssagen/abi.go, GenABIWrappers).
func (c *compiler) cgoPragmas() [][]string {
	var out [][]string
	for _, f := range c.pkg.files {
		for _, cg := range f.Comments {
			for _, cm := range cg.List {
				if !strings.HasPrefix(cm.Text, "//go:cgo_") {
					continue
				}
				if p := pragcgo(strings.TrimPrefix(cm.Text, "//")); p != nil {
					out = append(out, p)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	abis := map[string]string{}
	for _, fd := range c.funcDecls() {
		if fd.Recv != nil || fd.Name.Name == "_" {
			continue
		}
		fn, _ := defOf(fd.Name).(*types.Func)
		if fn == nil {
			continue
		}
		name := c.funcSymDecl(fn).Name
		abi := "ABIInternal"
		if fd.Body == nil && c.abis != nil && c.abis.defs[name] || hasDirective(fd.Doc, "//go:cgo_unsafe_args") {
			abi = "ABI0"
		}
		abis[name] = abi
	}
	for i, p := range out {
		if p[0] != "cgo_export_static" && p[0] != "cgo_export_dynamic" {
			continue
		}
		if abi, ok := abis[p[1]]; ok {
			if len(p) == 2 {
				p = append(p, p[1])
			}
			out[i] = append(p, abi)
		}
	}
	return out
}

// cgoExported: the linker names of the package's functions a //go:cgo_export_* directive names (no ABI
// wrappers for them: the definition is what C gets).
func (c *compiler) cgoExported() map[string]bool {
	m := map[string]bool{}
	for _, f := range c.pkg.files {
		for _, cg := range f.Comments {
			for _, cm := range cg.List {
				if strings.HasPrefix(cm.Text, "//go:cgo_export_") {
					if fs := pragmaFields(strings.TrimPrefix(cm.Text, "//")); len(fs) >= 2 {
						m[fs[1]] = true
					}
				}
			}
		}
	}
	return m
}

// pragcgo: noder's pragcgo (without its errors: a malformed directive is dropped).
func pragcgo(text string) []string {
	f := pragmaFields(text)
	if len(f) == 0 {
		return nil
	}
	verb := strings.TrimPrefix(f[0], "go:")
	f[0] = verb
	q := func(s string) bool { return len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' }
	switch verb {
	case "cgo_export_static", "cgo_export_dynamic":
		if !(len(f) == 2 && !q(f[1]) || len(f) == 3 && !q(f[1]) && !q(f[2])) {
			return nil
		}
	case "cgo_import_dynamic":
		switch {
		case len(f) == 2 && !q(f[1]):
		case len(f) == 3 && !q(f[1]) && !q(f[2]):
		case len(f) == 4 && !q(f[1]) && !q(f[2]) && q(f[3]):
			f[3] = strings.Trim(f[3], `"`)
		default:
			return nil
		}
	case "cgo_import_static":
		if len(f) != 2 || q(f[1]) {
			return nil
		}
	case "cgo_dynamic_linker", "cgo_ldflag":
		if len(f) != 2 || !q(f[1]) {
			return nil
		}
		f[1] = strings.Trim(f[1], `"`)
	default:
		return nil
	}
	return f
}

// pragmaFields: noder's (fields split at spaces, a quoted region one field).
func pragmaFields(s string) []string {
	var a []string
	inQuote := false
	fieldStart := -1
	for i, c := range s {
		switch {
		case c == '"':
			if inQuote {
				inQuote = false
				a = append(a, s[fieldStart:i+1])
				fieldStart = -1
			} else {
				inQuote = true
				if fieldStart >= 0 {
					a = append(a, s[fieldStart:i])
				}
				fieldStart = i
			}
		case !inQuote && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			if fieldStart >= 0 {
				a = append(a, s[fieldStart:i])
				fieldStart = -1
			}
		default:
			if fieldStart == -1 {
				fieldStart = i
			}
		}
	}
	if !inQuote && fieldStart >= 0 {
		a = append(a, s[fieldStart:])
	}
	return a
}

// cgoBlock: the "$$  // cgo" section of the object header (gc/obj.go, dumpLinkerObj), empty if there are
// no directives.
func cgoBlock(prags [][]string) []byte {
	if len(prags) == 0 {
		return nil
	}
	var b bytes.Buffer
	b.WriteString("\n$$\n\n$$\n\n")
	b.WriteString("\n$$  // cgo\n")
	json.NewEncoder(&b).Encode(prags)
	b.WriteString("\n$$\n\n")
	return b.Bytes()
}
