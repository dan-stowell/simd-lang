// fclink: a linker for statically linked linux/amd64 Go executables, built for wall-clock speed on many
// cores. It reads the same inputs as `go tool link -s -w -buildmode=exe` (an importcfg.link and the main
// package's object; archives or bare Go objects from the stock compiler and assembler or from fc) and
// writes an ELF executable without DWARF and without a symbol table. CGO_ENABLED=0, internal linking only.
//
//	fclink -o out -importcfg importcfg.link [-buildid id] [-stats] main.o
//
// Every phase works on dense symbol ids (object base + index in the object) held in flat arrays:
//
//	load      map every input, find the Go objects in it                                 (parallel per file)
//	resolve   name / content-hash tables filled by all cores without locks, then every
//	          symbol slot of every object resolved to its canonical definition           (parallel per unit)
//	inittasks the init order from R_INITORDER                                            (serial, small)
//	deadcode  frontier-parallel flood with the stock linker's method pruning             (parallel per level)
//	size      per unit: bytes per (section bucket, alignment class), function counts     (parallel per unit)
//	layout    prefix sums over units; the typelink types sorted by type string           (serial, small)
//	address   every live definition gets its address; then every slot its target's       (parallel per unit)
//	write     copy + relocate + pclntab entries + GC bitmaps, straight into the mapped
//	          output file                                                                (parallel per unit)
//	finish    moduledata, findfunctab, GC programs, FIPS hash, ELF headers               (serial, small)
//
// A unit is an object or a slice of a big object's symbols, so one huge package does not bound the wall clock.
// Built inside the Go tree's cmd directory (it uses cmd/internal/goobj): see driver/mkfclink.sh.
package main

import (
	"flag"
	"fmt"
	"internal/buildcfg"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	flagOut       = flag.String("o", "a.out", "output `file`")
	flagImportcfg = flag.String("importcfg", "", "importcfg.link `file`")
	flagBuildid   = flag.String("buildid", "", "Go build `id` to record")
	flagStats     = flag.Bool("stats", false, "print a phase table")
	flagJ         = flag.Int("j", 0, "threads (default: all)")
	flagNoDCE     = flag.Bool("nodce", false, "keep every symbol (debugging)")
	flagCPUProf   = flag.String("cpuprofile", "", "write a CPU profile")
	flagDumpLive  = flag.String("dumplive", "", "write the names of the live symbols to `file` (debugging)")
	flagEntry     = flag.String("E", "_rt0_amd64_linux", "entry `symbol`")
	// accepted for compatibility with the go command's link line
	_         = flag.String("buildmode", "exe", "only exe")
	_         = flag.Bool("s", true, "no symbol table (always)")
	_         = flag.Bool("w", true, "no DWARF (always)")
	_         = flag.String("extld", "", "ignored")
	_         = flag.String("installsuffix", "", "ignored")
	_         = flag.String("linkmode", "internal", "only internal")
	_         = flag.String("B", "", "ignored")
	_         = flag.String("tmpdir", "", "ignored")
	flagXDefs multiFlag
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, " ") }
func (m *multiFlag) Set(s string) error { *m = append(*m, s); return nil }

var nthreads int

// par runs f(worker, i) for i in [0,n) on all threads; the order of the calls is not defined.
func par(n int, f func(w, i int)) {
	if n == 0 {
		return
	}
	nw := nthreads
	if nw > n {
		nw = n
	}
	if nw <= 1 {
		for i := 0; i < n; i++ {
			f(0, i)
		}
		return
	}
	var wg sync.WaitGroup
	var next atomic.Int64
	for w := 0; w < nw; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				f(w, i)
			}
		}(w)
	}
	wg.Wait()
}

// errors: collected from any thread, reported at the end of a phase.
var (
	errMu   sync.Mutex
	errList []string
	nerrs   atomic.Int64
)

func errorf(format string, args ...any) {
	if nerrs.Add(1) > 30 {
		return
	}
	errMu.Lock()
	errList = append(errList, fmt.Sprintf(format, args...))
	errMu.Unlock()
}

func exitIfErrors() {
	if nerrs.Load() == 0 {
		return
	}
	for _, e := range errList {
		fmt.Fprintln(os.Stderr, "fclink:", e)
	}
	if n := nerrs.Load(); n > int64(len(errList)) {
		fmt.Fprintf(os.Stderr, "fclink: ... %d errors in all\n", n)
	}
	os.Remove(*flagOut)
	os.Exit(2)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fclink: "+format+"\n", args...)
	os.Exit(2)
}

type phase struct {
	name      string
	wall, cpu float64
	note      string
}

var (
	phases   []phase
	phaseT0  time.Time
	phaseC0  float64
	linkT0   time.Time
	linkCPU0 float64
)

func cpuSeconds() float64 {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6 + float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
}

func lap(name string, format string, args ...any) {
	now, c := time.Now(), cpuSeconds()
	phases = append(phases, phase{name, now.Sub(phaseT0).Seconds() * 1000, (c - phaseC0) * 1000, fmt.Sprintf(format, args...)})
	phaseT0, phaseC0 = now, c
}

func main() {
	linkT0, linkCPU0 = time.Now(), cpuSeconds()
	phaseT0, phaseC0 = linkT0, linkCPU0
	flag.Var(&flagXDefs, "X", "`importpath.name=value`: set a string variable")
	flag.Parse()
	if flag.NArg() != 1 || *flagImportcfg == "" {
		fatalf("usage: fclink -o out -importcfg importcfg.link [-buildid id] [-stats] main.o")
	}
	nthreads = *flagJ
	if nthreads <= 0 {
		nthreads = runtime.GOMAXPROCS(0)
	}
	runtime.GOMAXPROCS(nthreads)
	debug.SetGCPercent(-1) // one link, one heap
	if *flagCPUProf != "" {
		f, err := os.Create(*flagCPUProf)
		if err != nil {
			fatalf("%v", err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	strdata["runtime.buildVersion"] = buildVersion()
	if buildcfg.GOROOT != "" {
		strdata["runtime.defaultGOROOT"] = buildcfg.GOROOT
	}
	for _, x := range flagXDefs {
		name, val, ok := strings.Cut(x, "=")
		if !ok {
			fatalf("-X flag requires argument of the form importpath.name=value")
		}
		strdata[name] = val
	}

	load(*flagImportcfg, flag.Arg(0))
	exitIfErrors()
	resolve()
	exitIfErrors()
	inittasks()
	deadcode()
	exitIfErrors()
	layout()
	exitIfErrors()
	write()
	exitIfErrors()
	finish()
	exitIfErrors()

	if *flagStats {
		total, totalCPU := time.Since(linkT0).Seconds()*1000, (cpuSeconds()-linkCPU0)*1000
		fmt.Printf("%-10s %8s %8s\n", "phase", "wall ms", "cpu ms")
		for _, p := range phases {
			fmt.Printf("%-10s %8.1f %8.1f  %s\n", p.name, p.wall, p.cpu, p.note)
		}
		fmt.Printf("%-10s %8.1f %8.1f  %d threads, output %.1f MB\n", "total", total, totalCPU, nthreads, float64(outSize)/1e6)
	}
}

func buildVersion() string {
	v := buildcfg.Version
	if x := buildcfg.Experiment.String(); x != "" {
		sep := " "
		if !strings.Contains(v, "-") {
			sep = "-"
		}
		v += sep + "X:" + x
	}
	return v
}
