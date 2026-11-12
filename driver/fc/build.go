// build.go: whole-program mode. One process parses and type-checks every package of the plan once, then
// compiles the requested packages in parallel, each to its own object.
//
//	fc build -plan plan.json -o outdir [-j N] [-stats file] <import paths... | all>
//
// (Packages with assembly need the assembler's symabis and are compiled without ABI wrappers for now; the
// mode exists first to measure what the compiler costs: see tools/fc/cost.sh.)
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"cmd/internal/objabi"
)

func cpuSeconds() float64 {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6 + float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
}

func buildMode(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	plan := fs.String("plan", "", "`go list -json` stream of the packages")
	out := fs.String("o", "", "output directory for the objects")
	j := fs.Int("j", runtime.GOMAXPROCS(0), "packages compiled at once")
	stats := fs.String("stats", "", "write a line per package: path, source bytes, parse ms, check ms, compile ms, object bytes, result")
	fs.Parse(args)
	if *plan == "" || *out == "" || fs.NArg() == 0 {
		log.Fatal("usage: fc build -plan plan.json -o outdir [-j N] <import paths... | all>")
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
			c.readSymABIs("")
			c.depInit = hasInit
			errs := c.compilePackage()
			if len(errs) == 0 {
				name := filepath.Join(*out, strings.ReplaceAll(objabi.PathToPrefix(p.ImportPath), "/", "__")+".o")
				c.writeObject(name)
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
	fmt.Printf("total: %.0f ms wall, %.2f core-s\n", float64(time.Since(t0).Milliseconds()), total)
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
