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
//	resolve   partitioned hash joins by name and by content hash: every symbol slot of
//	          every object resolved to its canonical definition                          (parallel per unit, per partition)
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
//
// # DESIGN: the in-process hand-off (M5, "no separate linker")
//
// Today fc builds cmd/internal/obj LSyms, obj.WriteObjFile serialises them per package (content-hashing
// every hashed symbol), and fclink maps the files and undoes that: finds the objects, joins 1.8 million
// records by name and hash to learn which symbols are the same one. Measured on MinIO, 8 cores, warm
// (2026-11-13; 90% of the 2.24 million slots are in fc's own objects, the rest in 30 stock archives):
// load 9-17 ms + resolve 45-54 ms of a 200-220 ms link, 260-380 ms of its 1.0-1.2 core-s; plus about 33 ms
// of single-threaded kernel time unmapping the inputs at exit, and, with the inputs out of the page cache,
// 130-160 ms of load. What the hand-off cannot remove: dead code, layout, write (the other 70%).
//
// Everything after resolve needs exactly five things per symbol, and reads them through objOf/goobj.Reader
// in a handful of places (dead.go process, layout.go sizes, write.go writeUnit): kind, flags, size and
// alignment; the content bytes; the relocations (offset, size, type, addend, target id); the aux list
// (Go type, FuncInfo, pc-value tables, funcdata, as target ids); the name (functions, types, the few names
// the linker looks up). So the interface fclink wants from fc is a "unit source":
//
//	type Unit interface {            // one package's symbols, handed over when its back end is done
//		NSym() int                   // symbols get ids base+0 .. base+NSym-1; fclink assigns base
//		Sym(i int) SymInfo           // kind, flags (dupok, gotype, typelink, itab, usedInIface, reflectMethod, ...), size, align
//		Name(i int) string           // may alias fc's memory; needed for text, "type:" symbols and symbols with Global set
//		Data(i int) []byte           // aliases fc's memory; fclink copies it once, into the output
//		Relocs(i int) []Reloc        // Reloc{Off, Siz, Type, Add, Target Ref}
//		Aux(i int) []Aux             // Aux{Type, Target Ref}
//		Files() []string             // the file table the pc-file tables index
//	}
//	type Ref struct{ Unit, Index uint32 }   // Unit 0: this unit; Unit k: the k-th unit of this unit's import list
//
// with these agreements, which are what make resolve disappear for fc's symbols:
//
//  1. A reference is (unit, index), resolved by fc, which type-checked the whole program in one process
//     and so already holds the pointer to the definition. No names, no package-index tables.
//  2. Symbols that may be defined in several packages (type descriptors, itabs, generic instantiations,
//     wrappers: everything DUPOK or content-addressed today) are interned by fc as it makes them, in one
//     table for the whole build keyed as today (name, or content), and live in the unit that made them
//     first; later packages refer to that one. This is the join fclink does now, moved to where the
//     symbol is created and the key is hot, and it also saves fc compiling the duplicates' bodies and
//     hashing content for the object file. (Determinism: the owner must not depend on thread timing;
//     give the symbol to the lowest-numbered package that needs it, i.e. intern in a deterministic pass
//     or let fclink break ties by unit number as it does now.)
//  3. Only what crosses the boundary to the stock objects keeps a name: symbols the runtime, assembly or
//     a linkname refers to, and fc's references to those. fc marks them Global; fclink joins that small
//     set by name with the archives' symbols (the 30 archives are still loaded and resolved as today:
//     about 10% of the slots).
//  4. Timing: a unit may be handed over as soon as its package is compiled, on any thread; fclink only
//     records it. Resolution, dead code and everything else start when the last unit is in (the flood
//     needs the whole graph). Sizes could be summed per unit at hand-over, but that needs liveness.
//  5. Memory: Data and Relocs stay valid and unchanged until the link returns; fclink never writes them.
//
// The smallest step that gets most of it: keep cmd/internal/obj for now and have fc pass each package's
// []*obj.LSym (after fc's own numbering) instead of calling WriteObjFile; an adapter implements Unit over
// LSym (P, R, Size, Type, Attribute, Func().Pcln, Gotype) with Ref = (package, LSym index). That removes
// the write, the file reads, the hashing and the joins, and leaves the interning (2) as a later step that
// fclink can stand in for by joining DUPOK LSyms by name exactly as it joins object symbols today.
package main

import (
	"flag"
	"fmt"
	"internal/buildcfg"
	"os"
	"os/exec"
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
	flagDetach    = flag.Bool("detach", false, "return when the output is complete, without waiting for the process that wrote it to exit")
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

var phaseS0 float64

func sysSeconds() float64 {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
}

func lap(name string, format string, args ...any) {
	now, c, s := time.Now(), cpuSeconds(), sysSeconds()
	note := fmt.Sprintf(format, args...)
	if *flagStats {
		note = fmt.Sprintf("[kernel %5.1f] %s", (s-phaseS0)*1000, note)
	}
	phases = append(phases, phase{name, now.Sub(phaseT0).Seconds() * 1000, (c - phaseC0) * 1000, note})
	phaseT0, phaseC0, phaseS0 = now, c, s
}

func main() {
	linkT0, linkCPU0 = time.Now(), cpuSeconds()
	phaseT0, phaseC0 = linkT0, linkCPU0
	flag.Var(&flagXDefs, "X", "`importpath.name=value`: set a string variable")
	flag.Parse()
	if flag.NArg() != 1 || *flagImportcfg == "" {
		fatalf("usage: fclink -o out -importcfg importcfg.link [-buildid id] [-stats] main.o")
	}
	if *flagDetach && os.Getenv("FCLINK_CHILD") == "" {
		detach()
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

	if os.Getenv("FCLINK_SYNCRM") != "" {
		os.Remove(*flagOut)
		close(removed)
	} else {
		go func() { os.Remove(*flagOut); close(removed) }()
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
	if os.Getenv("FCLINK_CHILD") != "" {
		os.Stdout.Sync()
		os.NewFile(3, "done").Write([]byte{'k'}) // the output is complete: the parent may return
	}
}

var removed = make(chan struct{})

// detach (-detach): run the link in a child and return as soon as the child says the output is complete,
// leaving the child to exit on its own. What that skips is the kernel unmapping the inputs and the output
// and freeing the heap, 50-60 ms for MinIO, which the caller would otherwise wait for. The price: the
// child's CPU time is not charged to the caller's children (it is never waited for), so do not use it when
// measuring core-seconds.
func detach() {
	r, w, err := os.Pipe()
	if err != nil {
		return
	}
	cmd := exec.Command("/proc/self/exe", os.Args[1:]...)
	cmd.Env = append(os.Environ(), "FCLINK_CHILD=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{w}
	if err := cmd.Start(); err != nil {
		return // link in this process
	}
	w.Close()
	var b [1]byte
	if n, _ := r.Read(b[:]); n == 1 {
		os.Exit(0)
	}
	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		os.Exit(2)
	}
	os.Exit(0)
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
