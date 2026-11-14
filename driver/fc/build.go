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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"cmd/internal/fclinklib"
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
	stats := fs.String("stats", "", "write a line per package: path, source bytes, parse ms, check ms, compile ms, object bytes, result,\nthen ms after the start: check done, back end start, back end end; and ms writing the object")
	link := fs.String("link", "", "link the program whose main package is the (single) argument into this file; packages of the\n"+
		"runtime's closure and packages with assembly (and what they import) come from the plan's stock archives")
	chdir := fs.String("C", "", "no plan file: find the packages from the main package in this directory (the argument is then a\n"+
		"directory relative to it, like . or ./cmd/x); the go command is not asked")
	prof := fs.String("cpuprofile", "", "write a CPU profile")
	memprof := fs.String("memprofile", "", "write an allocation profile at the end")
	fs.Parse(args)
	if *memprof != "" {
		defer func() {
			if f, err := os.Create(*memprof); err == nil {
				pprof.Lookup("allocs").WriteTo(f, 0)
				f.Close()
			}
		}()
	}
	if *prof != "" {
		f, err := os.Create(*prof)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	if (*plan == "") == (*chdir == "") || *out == "" || fs.NArg() == 0 {
		log.Fatal("usage: fc build -plan plan.json -o outdir [-j N] <import paths... | all>")
	}
	if os.Getenv("GOGC") == "" {
		// One build, one heap: collecting it is work the build does not need (the stock baseline mode it is
		// measured against also runs with the collector off). The limit is a guard for the 31 GiB machine.
		debug.SetGCPercent(-1)
		debug.SetMemoryLimit(20 << 30)
		if v, err := strconv.Atoi(os.Getenv("FC_GOGC")); err == nil {
			debug.SetGCPercent(v) // (an experiment: the collector on)
		}
	}
	// fclink: in this process, the packages handed over in memory (fclinklib.NewUnit) instead of objects;
	// with FC_OBJFILES=1 the fclink command on written objects, if there is one
	withFclink := useFclink() && (!objFiles || linkerPath() != "")
	inProc := *link != "" && withFclink && !objFiles
	if *link != "" && os.Getenv("FC_RUNTIME") == "" {
		// a whole program for our own linker: the runtime is ours too, and the go command is not needed
		rtByFc = withFclink
	}
	t0 := time.Now()
	c0 := cpuSeconds()
	pr := newProgram()
	var targets []*Pkg
	if *chdir != "" {
		m, err := pr.selfMain(*chdir, fs.Arg(0))
		if err != nil {
			log.Fatalf("fc build: %v", err)
		}
		targets = []*Pkg{m}
	} else if err := pr.readPlan(*plan); err != nil {
		log.Fatal(err)
	}
	if *chdir != "" {
	} else if fs.NArg() == 1 && fs.Arg(0) == "all" {
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
			if (p.ImportPath == "runtime" && !rtByFc) || len(p.GoFiles) == 0 || keepStock[p.ImportPath] {
				markStock(p)
			}
		}
		// FC_RT_FC="pkg pkg": bisecting the runtime's packages: these are compiled by fc although the runtime
		// that imports them is the stock compiler's.
		for _, name := range strings.Fields(os.Getenv("FC_RT_FC")) {
			if p := pr.byPath[name]; p != nil && len(p.GoFiles) > 0 {
				delete(stock, p)
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
				ru := cmd.ProcessState.SysUsage().(*syscall.Rusage)
				fmt.Printf("stock packages: %d built by the go command, ready %d ms after the start, %.2f core-s\n", len(need), time.Since(t0).Milliseconds(),
					float64(ru.Utime.Nano()+ru.Stime.Nano())/1e9)
				stockDone <- err
			}()
		}
	}
	sort.Slice(targets, func(i, k int) bool { return targets[i].ImportPath < targets[k].ImportPath })
	// front end: everything the targets depend on
	var wg, wgF sync.WaitGroup
	for _, need := range append([]*Pkg{pr.byPath["runtime"], pr.byPath["internal/runtime/maps"]}, targets...) {
		if need == nil {
			continue
		}
		wgF.Add(1)
		go func() { defer wgF.Done(); pr.check(need) }()
	}
	// The back end does not wait for the whole front end: a package is compiled as soon as it (and so
	// everything it imports) is checked.
	var tCheck time.Duration
	var cCheck float64
	frontDone := make(chan struct{})
	go func() {
		wgF.Wait()
		tCheck = time.Since(t0)
		cCheck = cpuSeconds() - c0
		if declOnly {
			fmt.Printf("declarations only: all checked %d ms after the start, %.2f core-s\n", tCheck.Milliseconds(), cCheck)
			os.Exit(0)
		}
		close(frontDone)
	}()
	os.MkdirAll(*out, 0o777)
	// back end
	type result struct {
		p                       *Pkg
		compileNS               int64
		startMS, endMS, writeMS float64 // back end: start and end, ms after the program's start; of that, writing the object (or making the unit)
		objBytes                int64
		errs                    []string
		unit                    *fclinklib.Unit // in-process link: the package's symbols
		asmObjs                 []string        // in-process link: the assembler's objects
	}
	results := make([]result, len(targets))
	sem := newPrioPool(*j)
	initMemo := sync.Map{}
	var hasInit func(p *Pkg) bool
	hasInit = func(p *Pkg) bool {
		if v, ok := initMemo.Load(p); ok {
			return v.(bool)
		}
		v := false
		pr.check(p)
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
			pr.check(p)
			if declOnly {
				select {} // (the experiment ends when the front end does)
			}
			sem.acquire(p.srcBytes)
			defer sem.release()
			results[i].p = p
			if p.err != nil {
				results[i].errs = []string{"type check: " + p.err.Error()}
				return
			}
			t := time.Now()
			results[i].startMS = float64(t.Sub(t0).Microseconds()) / 1e3
			path := p.ImportPath
			if p.Name == "main" {
				path = "main"
			}
			c := newCompiler(pr, p, path, "")
			c.fnSem = sem
			if p.srcBytes >= 64<<10 {
				c.fnHelpers = *j - 1 // (a small package is one job; the cores are filled across packages)
			}
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
			tw := time.Now()
			if len(errs) == 0 && inProc {
				// no object: the linker reads the package's symbols where they are
				if asmDir != "" {
					c.writeAsmHdr(filepath.Join(asmDir, "go_asm.h"))
					for _, sf := range p.SFiles {
						o := filepath.Join(asmDir, strings.TrimSuffix(sf, ".s")+".o")
						if err := runAsmFile(p, asmDir, sf, o); err != nil {
							errs = append(errs, "assembler: "+err.Error())
							break
						}
						results[i].asmObjs = append(results[i].asmObjs, o)
					}
				}
				results[i].unit = fclinklib.NewUnit(c.ctxt)
			} else if len(errs) == 0 {
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
			results[i].writeMS = float64(time.Since(tw).Microseconds()) / 1e3
			results[i].endMS = float64(time.Since(t0).Microseconds()) / 1e3
		}()
	}
	wg.Wait()
	<-frontDone
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
			fmt.Fprintf(sf, "%s\t%d\t%.2f\t%.2f\t%.2f\t%d\t%s\t%.0f\t%.0f\t%.0f\t%.0f\n", r.p.ImportPath, r.p.srcBytes, float64(r.p.parseNS)/1e6, float64(r.p.checkNS)/1e6, float64(r.compileNS)/1e6, r.objBytes, res,
				float64(r.p.checkEnd.Sub(t0).Microseconds())/1e3, r.startMS, r.endMS, r.writeMS)
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
		if inProc {
			// the inputs in the order the importcfg would have them, the main package last
			byPkg := map[*Pkg]*result{}
			for i := range results {
				byPkg[results[i].p] = &results[i]
			}
			var inputs, mainIn []fclinklib.Input
			for _, p := range closure {
				path := p.ImportPath
				switch {
				case path == "unsafe":
				case stock[p]:
					if p.Export == "" {
						log.Fatalf("fc build -link: no stock archive for %s in the plan (go list -export)", path)
					}
					inputs = append(inputs, fclinklib.Input{Pkg: path, File: p.Export})
				default:
					r := byPkg[p]
					if p == mainPkg {
						path = "main"
					}
					in := []fclinklib.Input{{Pkg: path, Unit: r.unit}}
					for _, o := range r.asmObjs {
						in = append(in, fclinklib.Input{Pkg: path, File: o})
					}
					if p == mainPkg {
						mainIn = in
					} else {
						inputs = append(inputs, in...)
					}
				}
			}
			args := append([]string{"-s", "-o", *link}, strings.Fields(os.Getenv("FC_LINKFLAGS"))...)
			fclinklib.Link(args, append(inputs, mainIn...))
			fmt.Printf("link (fclink in process): %d fc units, %d stock archives, %.0f ms\n", len(targets), len(stock), float64(time.Since(tl).Milliseconds()))
		} else {
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
			linker := "stock linker"
			if withFclink {
				// the campaign's linker (driver/fclink) instead of the stock one
				args := append([]string{"-s", "-o", *link, "-importcfg", cfgFile}, strings.Fields(os.Getenv("FC_LINKFLAGS"))...)
				cmd = exec.Command(linkerPath(), append(args, objPath(*out, mainPkg))...)
				linker = "fclink"
			}
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				log.Fatalf("fc build -link: link: %v", err)
			}
			fmt.Printf("link (%s): %d fc objects, %d stock archives, %.0f ms\n", linker, len(targets), len(stock), float64(time.Since(tl).Milliseconds()))
		}
	}
	if os.Getenv("FC_INFOSTATS") != "" {
		var nt, nd, nu, ns, nb int64
		for _, p := range pr.byPath {
			if p.info != nil {
				nt += int64(len(p.info.Types))
				nd += int64(len(p.info.Defs))
				nu += int64(len(p.info.Uses))
				ns += int64(len(p.info.Selections))
				nb += p.srcBytes
			}
		}
		fmt.Printf("info maps: %d source bytes; bytes per entry: Types %.1f, Defs %.1f, Uses %.1f, Selections %.1f\n", nb,
			float64(nb)/float64(nt), float64(nb)/float64(nd), float64(nb)/float64(nu), float64(nb)/float64(ns))
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
	fmt.Printf("front end: %d packages, %.1f MB of source, all checked %.0f ms after the start (%.2f core-s spent by then, back end included)\n", n, float64(all)/1e6, float64(tCheck.Milliseconds()), cCheck)
	fmt.Printf("back end:  %d of %d packages compiled (%.2f of %.2f MB; %d functions, %.1f MB of code), overlapping the front end; everything done at %.0f ms\n", okN, len(targets), float64(okSrc)/1e6, float64(src)/1e6, statFuncs, float64(statCode)/1e6, float64(time.Since(t0).Milliseconds()))
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
