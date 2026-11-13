// build.go: whole-program mode. One process parses and type-checks every package of the plan once, then
// compiles the requested packages in parallel, each to its own object.
//
//	fc build -plan plan.json -o outdir [-j N] [-stats file] <import paths... | all>
//
// (Packages with assembly need the assembler's symabis and are compiled without ABI wrappers for now; the
// mode exists first to measure what the compiler costs: see tools/fc/cost.sh.)
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/constant"
	"go/types"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"cmd/internal/goobj"
	"cmd/internal/objabi"
)

// cpuSeconds: user and system time of this process and of the children it has waited for (the assembler,
// the linker).
func cpuSeconds() float64 {
	t := 0.0
	for _, who := range []int{syscall.RUSAGE_SELF, syscall.RUSAGE_CHILDREN} {
		var ru syscall.Rusage
		syscall.Getrusage(who, &ru)
		t += float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6 + float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
	}
	return t
}

func buildMode(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	plan := fs.String("plan", "", "`go list -json` stream of the packages")
	out := fs.String("o", "", "output directory for the objects")
	j := fs.Int("j", runtime.GOMAXPROCS(0), "packages compiled at once")
	stats := fs.String("stats", "", "write a line per package: path, source bytes, parse ms, check ms, compile ms, object bytes, result")
	link := fs.String("link", "", "link the program whose main package is the (single) argument into this file; packages of the\n"+
		"runtime's closure and packages with assembly (and what they import) come from the plan's stock archives")
	prof := fs.String("cpuprofile", "", "write a CPU profile")
	fs.Parse(args)
	if *prof != "" {
		f, err := os.Create(*prof)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	if *plan == "" || *out == "" || fs.NArg() == 0 {
		log.Fatal("usage: fc build -plan plan.json -o outdir [-j N] <import paths... | all>")
	}
	if os.Getenv("GOGC") == "" {
		// One build, one heap: collecting it is work the build does not need (the stock baseline mode it is
		// measured against also runs with the collector off). The limit is a guard for the 31 GiB machine.
		debug.SetGCPercent(-1)
		debug.SetMemoryLimit(20 << 30)
	}
	t0 := time.Now()
	c0 := cpuSeconds()
	pr := newProgram()
	if err := pr.readPlan(*plan); err != nil {
		log.Fatal(err)
	}
	var targets []*Pkg
	if fs.NArg() == 1 && fs.Arg(0) == "all" {
		for _, p := range pr.byPath {
			if !strings.Contains(p.ImportPath, " [") && len(p.GoFiles) > 0 {
				targets = append(targets, p)
			}
		}
	} else {
		for _, a := range fs.Args() {
			p := pr.byPath[a]
			if p == nil {
				log.Fatalf("fc: %s is not in the plan", a)
			}
			targets = append(targets, p)
		}
	}
	// link mode: the main package's whole closure, minus what stays with the stock compiler
	for _, k := range strings.FieldsFunc(os.Getenv("FC_KEEP_STOCK"), func(r rune) bool { return r == ',' || r == ' ' }) {
		keepStock[k] = true // (bisecting: these packages, and what they import, come from the stock archives)
	}
	var mainPkg *Pkg
	var stockDone chan error
	stock := map[*Pkg]bool{}
	var closure []*Pkg
	if *link != "" {
		if len(targets) != 1 {
			log.Fatal("fc build -link: one main package")
		}
		mainPkg = targets[0]
		seen := map[*Pkg]bool{}
		var walk func(p *Pkg)
		walk = func(p *Pkg) {
			if seen[p] {
				return
			}
			seen[p] = true
			for _, ip := range p.Imports {
				if m, ok := p.ImportMap[ip]; ok {
					ip = m
				}
				if d := pr.byPath[ip]; d != nil {
					walk(d)
				}
			}
			closure = append(closure, p)
		}
		walk(mainPkg)
		if rt := pr.byPath["runtime"]; rt != nil {
			walk(rt)
		}
		var markStock func(p *Pkg)
		markStock = func(p *Pkg) {
			if stock[p] {
				return
			}
			stock[p] = true
			for _, ip := range p.Imports {
				if m, ok := p.ImportMap[ip]; ok {
					ip = m
				}
				if d := pr.byPath[ip]; d != nil {
					markStock(d)
				}
			}
		}
		for _, p := range closure {
			if p.ImportPath == "runtime" || len(p.GoFiles) == 0 || keepStock[p.ImportPath] {
				markStock(p)
			}
		}
		targets = nil
		var need []string
		for _, p := range closure {
			if !stock[p] {
				targets = append(targets, p)
			} else if p.Export == "" && p.ImportPath != "unsafe" {
				need = append(need, p.ImportPath)
			}
		}
		if len(need) > 0 {
			// The plan has no archives for the packages that stay with the stock compiler (a cold build):
			// have the go command build them now, beside our own work.
			stockDone = make(chan error, 1)
			go func() {
				cmd := exec.Command("go", append([]string{"list", "-export", "-json", "-gcflags=all=-l"}, need...)...)
				cmd.Dir = mainPkg.Dir
				cmd.Stderr = os.Stderr
				outb, err := cmd.Output()
				if err == nil {
					dec := json.NewDecoder(bytes.NewReader(outb))
					for {
						var e struct{ ImportPath, Export string }
						if dec.Decode(&e) != nil {
							break
						}
						if p := pr.byPath[e.ImportPath]; p != nil {
							p.Export = e.Export
						}
					}
				}
				stockDone <- err
			}()
		}
	}
	sort.Slice(targets, func(i, k int) bool { return targets[i].ImportPath < targets[k].ImportPath })
	// front end: everything the targets depend on
	var wg sync.WaitGroup
	for _, need := range append([]*Pkg{pr.byPath["runtime"], pr.byPath["internal/runtime/maps"]}, targets...) {
		if need == nil {
			continue
		}
		wg.Add(1)
		go func() { defer wg.Done(); pr.check(need) }()
	}
	wg.Wait()
	tCheck := time.Since(t0)
	cCheck := cpuSeconds() - c0
	os.MkdirAll(*out, 0o777)
	// back end
	type result struct {
		p         *Pkg
		compileNS int64
		objBytes  int64
		errs      []string
	}
	results := make([]result, len(targets))
	sem := make(chan struct{}, *j)
	initMemo := sync.Map{}
	var hasInit func(p *Pkg) bool
	hasInit = func(p *Pkg) bool {
		if v, ok := initMemo.Load(p); ok {
			return v.(bool)
		}
		v := false
		if p.tpkg != nil {
			c := newCompiler(pr, p, p.ImportPath, "")
			v = c.needsInit()
			for _, d := range p.deps {
				if v {
					break
				}
				v = hasInit(d)
			}
		}
		initMemo.Store(p, v)
		return v
	}
	var stockOnce sync.Once
	waitStock := func() {
		stockOnce.Do(func() {
			if stockDone != nil {
				if err := <-stockDone; err != nil {
					log.Fatalf("fc build: stock packages: %v", err)
				}
			}
		})
	}
	for i, p := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i].p = p
			if p.err != nil {
				results[i].errs = []string{"type check: " + p.err.Error()}
				return
			}
			t := time.Now()
			path := p.ImportPath
			if p.Name == "main" {
				path = "main"
			}
			c := newCompiler(pr, p, path, "")
			var asmDir string
			if len(p.SFiles) > 0 {
				// assembly: the assembler says which symbols it defines and uses, before the compile
				asmDir = filepath.Join(*out, "asm", strings.ReplaceAll(objabi.PathToPrefix(p.ImportPath), "/", "__"))
				os.MkdirAll(asmDir, 0o777)
				os.WriteFile(filepath.Join(asmDir, "go_asm.h"), nil, 0o666)
				if err := runAsm(p, asmDir, "-gensymabis", "-o", filepath.Join(asmDir, "symabis")); err != nil {
					results[i].errs = []string{"assembler (symabis): " + err.Error()}
					return
				}
				c.readSymABIs(filepath.Join(asmDir, "symabis"))
			} else {
				c.readSymABIs("")
			}
			c.depInit = func(d *Pkg) bool {
				if stock[d] {
					waitStock()
					stockInitMu.Lock()
					defer stockInitMu.Unlock()
					v, ok := stockInit[d]
					if !ok {
						v = d.Export != "" && definedSyms(d.Export)[objabi.PathToPrefix(d.ImportPath)+"..inittask"]
						stockInit[d] = v
					}
					return v
				}
				return hasInit(d)
			}
			// the linker loads a package only if some loaded object says it imports it
			for _, d := range p.deps {
				ip := goobj.ImportedPkg{Pkg: d.ImportPath}
				if stock[d] {
					waitStock()
				}
				if stock[d] && d.Export != "" {
					ip.Fingerprint = stockFingerprint(d.Export) // the linker compares it with the archive's
				}
				c.ctxt.Imports = append(c.ctxt.Imports, ip)
			}
			errs := c.compilePackage()
			if len(errs) == 0 {
				name := objPath(*out, p)
				if asmDir != "" {
					// the object and the assembler's objects go into one archive
					goObj := filepath.Join(asmDir, "_go_.o")
					c.writeObject(goObj)
					c.writeAsmHdr(filepath.Join(asmDir, "go_asm.h"))
					members := []string{goObj}
					for _, sf := range p.SFiles {
						o := filepath.Join(asmDir, strings.TrimSuffix(sf, ".s")+".o")
						if err := runAsmFile(p, asmDir, sf, o); err != nil {
							errs = append(errs, "assembler: "+err.Error())
							break
						}
						members = append(members, o)
					}
					if len(errs) == 0 {
						writePkgArchive(name, members)
					}
				} else {
					c.writeObject(name)
				}
				if st, err := os.Stat(name); err == nil {
					results[i].objBytes = st.Size()
				}
			}
			results[i].errs = errs
			results[i].compileNS = int64(time.Since(t))
		}()
	}
	wg.Wait()
	var sf *os.File
	if *stats != "" {
		sf, _ = os.Create(*stats)
		defer sf.Close()
	}
	var okN, src, okSrc int64
	var parse, check, compile int64
	for _, r := range results {
		res := "ok"
		if len(r.errs) > 0 {
			res = "no: " + r.errs[0]
		} else {
			okN++
			okSrc += r.p.srcBytes
			parse += r.p.parseNS
			check += r.p.checkNS
			compile += r.compileNS
		}
		src += r.p.srcBytes
		if sf != nil {
			fmt.Fprintf(sf, "%s\t%d\t%.2f\t%.2f\t%.2f\t%d\t%s\n", r.p.ImportPath, r.p.srcBytes, float64(r.p.parseNS)/1e6, float64(r.p.checkNS)/1e6, float64(r.compileNS)/1e6, r.objBytes, res)
		}
	}
	if *link != "" {
		failed := 0
		for _, r := range results {
			if len(r.errs) > 0 {
				failed++
				fmt.Fprintf(os.Stderr, "fc: %s: %s\n", r.p.ImportPath, r.errs[0])
			}
		}
		if failed > 0 {
			log.Fatalf("fc build -link: %d packages not compiled", failed)
		}
		waitStock()
		tl := time.Now()
		var cfg strings.Builder
		for _, p := range closure {
			path := p.ImportPath
			switch {
			case path == "unsafe":
			case stock[p]:
				if p.Export == "" {
					log.Fatalf("fc build -link: no stock archive for %s in the plan (go list -export)", path)
				}
				fmt.Fprintf(&cfg, "packagefile %s=%s\n", path, p.Export)
			case p == mainPkg:
			default:
				fmt.Fprintf(&cfg, "packagefile %s=%s\n", path, objPath(*out, p))
			}
		}
		cfgFile := filepath.Join(*out, "importcfg.link")
		os.WriteFile(cfgFile, []byte(cfg.String()), 0o666)
		cmd := exec.Command("go", "tool", "link", "-o", *link, "-importcfg", cfgFile, "-buildmode=exe", "-s", "-w", objPath(*out, mainPkg))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("fc build -link: link: %v", err)
		}
		fmt.Printf("link: %d fc objects, %d stock archives, %.0f ms\n", len(targets), len(stock), float64(time.Since(tl).Milliseconds()))
	}
	total := cpuSeconds() - c0
	var all int64
	n := 0
	for _, p := range pr.byPath {
		if p.tpkg != nil {
			all += p.srcBytes
			n++
		}
	}
	fmt.Printf("front end: %d packages, %.1f MB of source, %.0f ms wall, %.2f core-s\n", n, float64(all)/1e6, float64(tCheck.Milliseconds()), cCheck)
	fmt.Printf("back end:  %d of %d packages compiled (%.2f of %.2f MB), %.0f ms wall, %.2f core-s\n", okN, len(targets), float64(okSrc)/1e6, float64(src)/1e6, float64((time.Since(t0) - tCheck).Milliseconds()), total-cCheck)
	fmt.Printf("compiled packages only: parse %.2f s + check %.2f s + compile %.2f s = %.2f core-s for %.2f MB: %.2f core-s per MB\n",
		float64(parse)/1e9, float64(check)/1e9, float64(compile)/1e9, float64(parse+check+compile)/1e9, float64(okSrc)/1e6, float64(parse+check+compile)/1e9/(float64(okSrc)/1e6+1e-9))
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	fmt.Printf("total: %.0f ms wall, %.2f core-s (with the assembler and the linker), peak memory %.1f GB\n", float64(time.Since(t0).Milliseconds()), total, float64(ru.Maxrss)/1e6)
}

// needsInit: does the package have initialization to run (an init function, or a variable whose value is
// not known at compile time)?
func (c *compiler) needsInit() bool {
	for _, fd := range c.funcDecls() {
		if fd.Recv == nil && fd.Name.Name == "init" {
			return true
		}
	}
	for _, in := range c.info.InitOrder {
		if len(in.Lhs) != 1 || !c.isStatic(in.Rhs, in.Lhs[0].Type()) {
			return true
		}
	}
	return false
}

var (
	stockInitMu sync.Mutex
	stockInit   = map[*Pkg]bool{}
	// keepStock: packages that stay with the stock compiler in link mode for a reason other than assembly
	keepStock = map[string]bool{}
)

func objPath(dir string, p *Pkg) string {
	return filepath.Join(dir, strings.ReplaceAll(objabi.PathToPrefix(p.ImportPath), "/", "__")+".o")
}

var fingerprints sync.Map

// stockFingerprint: the fingerprint in the header of a stock archive's Go object.
func stockFingerprint(path string) goobj.FingerprintType {
	if v, ok := fingerprints.Load(path); ok {
		return v.(goobj.FingerprintType)
	}
	var fp goobj.FingerprintType
	for _, m := range readArchive(path) {
		if m.name != "_go_.o" {
			continue
		}
		if i := bytes.Index(m.data, []byte("\n!\n")); i >= 0 {
			if r := goobj.NewReaderFromBytes(m.data[i+3:], false); r != nil {
				fp = r.Fingerprint()
			}
		}
	}
	fingerprints.Store(path, fp)
	return fp
}

func asmArgs(p *Pkg, dir string) []string {
	args := []string{"-p", p.ImportPath, "-I", dir, "-I", filepath.Join(runtime.GOROOT(), "pkg/include"),
		"-D", "GOOS_linux", "-D", "GOARCH_amd64", "-D", "GOAMD64_v1"}
	if p.Standard {
		args = append(args, "-std")
	}
	return args
}

// runAsm runs the assembler over all of p's assembly files with extra flags (symabis generation).
func runAsm(p *Pkg, dir string, extra ...string) error {
	args := append(asmArgs(p, dir), extra...)
	for _, sf := range p.SFiles {
		args = append(args, filepath.Join(p.Dir, sf))
	}
	return runTool(p.Dir, args)
}

func runAsmFile(p *Pkg, dir, sf, out string) error {
	args := append(asmArgs(p, dir), "-o", out, filepath.Join(p.Dir, sf))
	return runTool(p.Dir, args)
}

func runTool(dir string, args []string) error {
	// the assembler itself, not through the go command (whose start costs as much as a small file)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "pkg/tool/linux_amd64/asm"), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if outb, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(outb)))
	}
	return nil
}

// writePkgArchive packs object files into an archive the linker accepts as a package.
func writePkgArchive(path string, members []string) {
	hdr := func(name string, size int) []byte {
		return []byte(fmt.Sprintf("%-16s%-12d%-6d%-6d%-8o%-10d`\n", name, 0, 0, 0, 0o644, size))
	}
	var ms []arMember
	def := []byte(objabi.HeaderString() + "\n$$\n\n$$\n")
	ms = append(ms, arMember{"__.PKGDEF", hdr("__.PKGDEF", len(def)), def})
	for _, m := range members {
		data, err := os.ReadFile(m)
		if err != nil {
			log.Fatal(err)
		}
		name := filepath.Base(m)
		if len(name) > 16 {
			name = name[:16]
		}
		ms = append(ms, arMember{name, hdr(name, len(data)), data})
	}
	writeArchive(path, ms)
}

// writeAsmHdr writes go_asm.h: the package's constants and struct layouts, for its assembly files.
func (c *compiler) writeAsmHdr(path string) {
	var b strings.Builder
	fmt.Fprintf(&b, "// generated by fc from package %s\n\n", c.tpkg.Name())
	scope := c.tpkg.Scope()
	for _, name := range scope.Names() {
		if name == "_" {
			continue
		}
		switch o := scope.Lookup(name).(type) {
		case *types.Const:
			if k := o.Val().Kind(); k == constant.Float || k == constant.Complex {
				continue
			}
			fmt.Fprintf(&b, "#define const_%s %s\n", name, o.Val().ExactString())
		case *types.TypeName:
			if o.IsAlias() {
				continue
			}
			nt, ok := o.Type().(*types.Named)
			if !ok || nt.TypeParams().Len() > 0 {
				continue
			}
			st, ok := nt.Underlying().(*types.Struct)
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "#define %s__size %d\n", name, c.sizeof(nt))
			offs := c.fieldOffsets(st)
			for i := 0; i < st.NumFields(); i++ {
				if f := st.Field(i); f.Name() != "_" {
					fmt.Fprintf(&b, "#define %s_%s %d\n", name, f.Name(), offs[i])
				}
			}
		}
	}
	os.WriteFile(path, []byte(b.String()), 0o666)
}
