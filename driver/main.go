// sb: the build driver. Builds a Go main package end to end, cold, with every phase of the
// charter's trace present and timed. Phases not yet replaced delegate to the stock toolchain
// (go list for the plan, go tool compile/asm/link for the rest).
//
//	sb -C srcdir -o out [-j 8] [-trace sb.json] [-lexer build/golex_strand] [-work /dev/shm] [pkg]
//
// This file is orchestration (process spawning, a ready queue): not data-parallel work, and
// listed in SCALAR_LEDGER.md as such.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Module struct {
	Path, Version, GoVersion string
	Main                     bool
}

type Pkg struct {
	ImportPath, Dir, Name string
	GoFiles, SFiles       []string
	EmbedPatterns         []string
	EmbedFiles            []string
	Imports               []string
	ImportMap             map[string]string
	Standard              bool
	Module                *Module
	DefaultGODEBUG        string
	Deps                  []string

	// driver state
	goBytes  int64
	deps     []*Pkg // direct, resolved
	users    []*Pkg
	waiting  int
	prio     float64 // cost of the longest chain from here to the link
	objdir   string
	archive  string // what the linker reads
	export   string // what importers read (the same file unless pipelining)
	stubOnly bool
}

type Action struct {
	Pkg    string  `json:"pkg"`
	Kind   string  `json:"kind"`
	Ready  float64 `json:"ready_ms"`
	Start  float64 `json:"start_ms"`
	End    float64 `json:"end_ms"`
	User   float64 `json:"user_ms"`
	Sys    float64 `json:"sys_ms"`
	Export float64 `json:"export_ms,omitempty"`
	C      int     `json:"c"`
	Bytes  int64   `json:"go_bytes"`
	Deps   []string `json:"deps,omitempty"`
}

type Phase struct {
	Name  string  `json:"name"`
	Start float64 `json:"start_ms"`
	End   float64 `json:"end_ms"`
	User  float64 `json:"user_ms"`
	Sys   float64 `json:"sys_ms"`
	Bytes int64   `json:"bytes"`
	Note  string  `json:"note,omitempty"`
}

type Trace struct {
	Phases  []Phase           `json:"phases"`
	Actions []Action          `json:"actions"`
	Meta    map[string]string `json:"meta"`
}

var (
	t0      = time.Now()
	trace   = Trace{Meta: map[string]string{}}
	traceMu sync.Mutex
	goroot  string
	tooldir string
)

func ms() float64 { return float64(time.Since(t0).Microseconds()) / 1000 }

var cleanup string
var dryRun bool
var compiler string
var pipeline bool
var noEarly bool

func die(f string, a ...any) {
	if cleanup != "" {
		os.RemoveAll(cleanup)
	}
	fmt.Fprintf(os.Stderr, "sb: "+f+"\n", a...)
	os.Exit(1)
}

func phase(name string, start float64, bytes int64, user, sys float64, note string) {
	traceMu.Lock()
	trace.Phases = append(trace.Phases, Phase{name, start, ms(), user, sys, bytes, note})
	traceMu.Unlock()
}

// run a tool; returns user and sys ms
func run(dir string, argv []string, stdin []byte, stdout *os.File) (float64, float64, []byte, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var outb bytes.Buffer
	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = &outb
	}
	err := cmd.Run()
	var u, s float64
	if cmd.ProcessState != nil {
		u = float64(cmd.ProcessState.UserTime().Microseconds()) / 1000
		s = float64(cmd.ProcessState.SystemTime().Microseconds()) / 1000
	}
	if err != nil {
		err = fmt.Errorf("%s: %v\n%s", strings.Join(argv, " "), err, errb.String())
	}
	return u, s, outb.Bytes(), err
}

func main() {
	srcdir := flag.String("C", ".", "source directory (the main module)")
	out := flag.String("o", "a.out", "output binary")
	jobs := flag.Int("j", runtime.NumCPU(), "CPU tokens")
	tracePath := flag.String("trace", "", "write the build trace (JSON) here")
	lexer := flag.String("lexer", "", "Strand Go lexer binary; run over every Go file")
	work := flag.String("work", "/dev/shm", "parent of the work directory (tmpfs: objects never touch the disk)")
	gcflags := flag.String("gcflags", "", "extra flags for every compile")
	ldflags := flag.String("ldflags", "", "extra flags for the link")
	keep := flag.Bool("keep", false, "keep the work directory")
	flag.StringVar(&compiler, "compiler", "", "compiler binary (default: go tool compile)")
	flag.BoolVar(&pipeline, "pipeline", false, "release a package's importers when its export data is written, before its back end runs (needs the patched compiler)")
	workdirFlag := flag.String("workdir", "", "use exactly this work directory (for reproducible comparisons)")
	flag.BoolVar(&noEarly, "noearly", false, "pipelining plumbing without the early export (the compiler behaves as stock): for differential tests")
	maxBE := flag.Int("backends", 32, "pipelining: most compilers allowed in their back end at once (memory)")
	flag.BoolVar(&dryRun, "n", false, "dry run: write the config files and log the commands, run nothing")
	dumpCmds := flag.String("dumpcmds", "", "write every tool command line here (for diffing against go build)")
	flag.Parse()
	pkgArg := "."
	if flag.NArg() > 0 {
		pkgArg = flag.Arg(0)
	}
	os.Setenv("GOTOOLCHAIN", "local")
	os.Setenv("CGO_ENABLED", "0")
	os.Setenv("GOFLAGS", "")
	runtime.GOMAXPROCS(*jobs)

	// ---- plan: the package graph (delegated to go list)
	st := ms()
	u, s, js, err := run(*srcdir, []string{"go", "list", "-deps",
		"-json=ImportPath,Dir,Name,GoFiles,SFiles,EmbedPatterns,EmbedFiles,Imports,ImportMap,Standard,Module,DefaultGODEBUG,Deps", pkgArg}, nil, nil)
	if err != nil {
		die("%v", err)
	}
	var pkgs []*Pkg
	byPath := map[string]*Pkg{}
	dec := json.NewDecoder(bytes.NewReader(js))
	for dec.More() {
		p := new(Pkg)
		if err := dec.Decode(p); err != nil {
			die("go list: %v", err)
		}
		if p.ImportPath == "unsafe" {
			continue
		}
		pkgs = append(pkgs, p)
		byPath[p.ImportPath] = p
	}
	main := pkgs[len(pkgs)-1]
	if main.Name != "main" {
		die("%s is not a main package", main.ImportPath)
	}
	u2, s2, envOut, err := run(*srcdir, []string{"go", "env", "GOROOT", "GOVERSION"}, nil, nil)
	if err != nil {
		die("%v", err)
	}
	envv := strings.Fields(string(envOut))
	goroot = envv[0]
	goversion := envv[1]
	tooldir = filepath.Join(goroot, "pkg", "tool", "linux_amd64")
	if compiler == "" {
		compiler = filepath.Join(tooldir, "compile")
	}
	for _, p := range pkgs {
		for _, imp := range p.Imports {
			if m, ok := p.ImportMap[imp]; ok {
				imp = m
			}
			if imp == "unsafe" || imp == "C" {
				continue
			}
			d := byPath[imp]
			if d == nil {
				die("%s imports %s: not in the plan", p.ImportPath, imp)
			}
			p.deps = append(p.deps, d)
			d.users = append(d.users, p)
		}
	}
	phase("plan (go list)", st, int64(len(js)), u+u2, s+s2, fmt.Sprintf("%d packages", len(pkgs)))

	// ---- read inputs: every source file, in parallel, into one buffer per kind
	st = ms()
	type file struct {
		p    *Pkg
		path string
		data []byte
	}
	var gofiles []*file
	var sbytes int64
	for _, p := range pkgs {
		for _, f := range p.GoFiles {
			gofiles = append(gofiles, &file{p: p, path: filepath.Join(p.Dir, f)})
		}
	}
	var wg sync.WaitGroup
	chunk := (len(gofiles) + *jobs - 1) / *jobs
	for w := 0; w < *jobs; w++ {
		lo, hi := w*chunk, min((w+1)*chunk, len(gofiles))
		if lo >= hi {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, f := range gofiles[lo:hi] {
				b, err := os.ReadFile(f.path)
				if err != nil {
					die("%v", err)
				}
				f.data = b
			}
		}()
	}
	wg.Wait()
	var total int64
	for _, f := range gofiles {
		f.p.goBytes += int64(len(f.data))
		total += int64(len(f.data)) + 1
	}
	for _, p := range pkgs {
		for _, f := range p.SFiles {
			if fi, err := os.Stat(filepath.Join(p.Dir, f)); err == nil {
				sbytes += fi.Size()
			}
		}
	}
	var workdir string
	if *workdirFlag != "" {
		workdir = *workdirFlag
		err = os.MkdirAll(workdir, 0o755)
	} else {
		workdir, err = os.MkdirTemp(*work, "sb-")
	}
	if err != nil {
		die("%v", err)
	}
	if !*keep {
		cleanup = workdir
		defer os.RemoveAll(workdir)
	}
	phase("read inputs", st, total, 0, 0, fmt.Sprintf("%d Go files; %d bytes of assembly left to asm", len(gofiles), sbytes))

	// ---- lex: Strand's Go lexer over the concatenation of every Go file
	if *lexer != "" {
		st = ms()
		corpus := make([]byte, 0, total)
		for _, f := range gofiles {
			corpus = append(corpus, f.data...)
			if n := len(f.data); n > 0 && f.data[n-1] != '\n' {
				corpus = append(corpus, '\n')
			}
		}
		cpath := filepath.Join(workdir, "all.go")
		if err := os.WriteFile(cpath, corpus, 0o644); err != nil {
			die("%v", err)
		}
		in, _ := os.Open(cpath)
		tok, _ := os.Create(filepath.Join(workdir, "all.tok"))
		cmd := exec.Command(*lexer)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, tok, os.Stderr
		if err := cmd.Run(); err != nil {
			die("lexer: %v", err)
		}
		fi, _ := tok.Stat()
		in.Close()
		tok.Close()
		phase("lex (Strand golex)", st, int64(len(corpus)),
			float64(cmd.ProcessState.UserTime().Microseconds())/1000, float64(cmd.ProcessState.SystemTime().Microseconds())/1000,
			fmt.Sprintf("%d tokens; output not yet consumed downstream", fi.Size()/5))
		corpus = nil
	}
	for _, f := range gofiles {
		f.data = nil
	}

	// ---- compile: stock compile/asm per package, in dependency order
	st = ms()
	// priority = own cost + the most expensive chain of users above
	var prio func(p *Pkg) float64
	seen := map[*Pkg]bool{}
	prio = func(p *Pkg) float64 {
		if seen[p] {
			return p.prio
		}
		seen[p] = true
		best := 0.0
		for _, u := range p.users {
			best = max(best, prio(u))
		}
		p.prio = best + float64(p.goBytes) + 20000
		return p.prio
	}
	for _, p := range pkgs {
		prio(p)
	}
	var cmdLog []string
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	tokens := *jobs
	var ready []*Pkg
	readyAt := map[*Pkg]float64{}
	for _, p := range pkgs {
		p.waiting = len(p.deps)
		if p.waiting == 0 {
			ready = append(ready, p)
			readyAt[p] = ms()
		}
	}
	done := 0
	backends := 0 // compilers past their export, running at low priority without a token
	maxBackends := *maxBE
	var firstErr error
	var cu, cs float64
	extra := strings.Fields(*gcflags)
	for i, p := range pkgs {
		p.objdir = filepath.Join(workdir, fmt.Sprintf("b%04d", i))
		p.archive = filepath.Join(p.objdir, "_pkg_.a")
		p.export = p.archive
		if pipeline {
			p.export = filepath.Join(p.objdir, "_pkg_.x")
		}
	}
	mu.Lock()
	for done < len(pkgs) && firstErr == nil {
		if len(ready) == 0 || tokens == 0 {
			cond.Wait()
			continue
		}
		sort.Slice(ready, func(i, j int) bool { return ready[i].prio > ready[j].prio })
		p := ready[0]
		ready = ready[1:]
		// CPU tokens: one always; more (for the compiler's -c) if the package is big and cores are free
		want := int(p.goBytes/150000) + 1
		c := min(want, tokens, 8)
		if len(ready) > 0 { // leave cores for other ready packages unless this one is huge
			c = min(c, max(1, tokens-len(ready)))
		}
		held := c
		if pipeline {
			// A front end is one thread and is what importers wait for: it holds one token. When the export
			// data is out the token goes back and the back end continues at the lowest CPU priority, so
			// back ends fill whatever the front ends leave. -c is by size alone.
			if backends >= maxBackends {
				ready = append(ready, p)
				cond.Wait()
				continue
			}
			held = 1
			switch {
			case p.goBytes > 1500000:
				c = 8
			case p.goBytes > 300000:
				c = 4
			case p.goBytes > 100000:
				c = 2
			default:
				c = 1
			}
		}
		tokens -= held
		go func(p *Pkg, c int, rdy float64) {
			a := Action{Pkg: p.ImportPath, Kind: "compile", Ready: rdy, Start: ms(), C: c, Bytes: p.goBytes}
			for _, d := range p.deps {
				a.Deps = append(a.Deps, d.ImportPath)
			}
			if len(p.SFiles) > 0 {
				a.Kind = "compile+asm"
			}
			released := false
			inBackend := false
			release := func() { // importers may start: called with mu held
				if released {
					return
				}
				released = true
				for _, usr := range p.users {
					usr.waiting--
					if usr.waiting == 0 {
						ready = append(ready, usr)
						readyAt[usr] = ms()
					}
				}
				cond.Broadcast()
			}
			u, s, cmds, err := buildPkg(p, c, goversion, extra, func(pid int) {
				renice(pid)
				mu.Lock()
				a.Export = ms()
				tokens += held
				held = 0
				backends++
				inBackend = true
				release()
				mu.Unlock()
			})
			a.End, a.User, a.Sys = ms(), u, s
			mu.Lock()
			trace.Actions = append(trace.Actions, a)
			cmdLog = append(cmdLog, cmds...)
			cu += u
			cs += s
			tokens += held
			if inBackend {
				backends--
			}
			done++
			if err != nil && firstErr == nil {
				firstErr = err
			}
			release()
			cond.Broadcast()
			mu.Unlock()
		}(p, c, readyAt[p])
	}
	mu.Unlock()
	if firstErr != nil {
		die("%v", firstErr)
	}
	var objBytes int64
	for _, p := range pkgs {
		if fi, err := os.Stat(p.archive); err == nil {
			objBytes += fi.Size()
		}
	}
	phase("compile (go tool compile/asm)", st, objBytes, cu, cs, "bytes = object archives written to tmpfs")

	// ---- link (delegated), writing the binary to tmpfs
	st = ms()
	var cfg bytes.Buffer
	for _, p := range pkgs {
		fmt.Fprintf(&cfg, "packagefile %s=%s\n", p.ImportPath, p.archive)
	}
	cfgPath := filepath.Join(workdir, "importcfg.link")
	os.WriteFile(cfgPath, cfg.Bytes(), 0o644)
	exe := filepath.Join(workdir, "a.out")
	argv := []string{filepath.Join(tooldir, "link"), "-o", exe, "-importcfg", cfgPath}
	if main.DefaultGODEBUG != "" {
		argv = append(argv, "-X=runtime.godebugDefault="+main.DefaultGODEBUG)
	}
	argv = append(argv, "-buildmode=exe")
	argv = append(argv, strings.Fields(*ldflags)...)
	argv = append(argv, main.archive)
	cmdLog = append(cmdLog, strings.Join(argv, " "))
	if dryRun {
		sort.Strings(cmdLog)
		os.WriteFile(*dumpCmds, []byte(strings.ReplaceAll(strings.Join(cmdLog, "\n")+"\n", workdir, "$WORK")), 0o644)
		fmt.Println(workdir)
		return
	}
	u, s, _, err = run(workdir, argv, nil, nil)
	if err != nil {
		die("%v", err)
	}
	fi, _ := os.Stat(exe)
	trace.Actions = append(trace.Actions, Action{Pkg: main.ImportPath, Kind: "link", Start: st, End: ms(), User: u, Sys: s})
	phase("link (go tool link)", st, fi.Size(), u, s, "")

	// ---- write output: the binary goes to the disk, and is synced so the write is counted
	st = ms()
	b, err := os.ReadFile(exe)
	if err != nil {
		die("%v", err)
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		die("%v", err)
	}
	if _, err := f.Write(b); err != nil {
		die("%v", err)
	}
	f.Sync()
	f.Close()
	phase("write output", st, int64(len(b)), 0, 0, "")

	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	trace.Meta["self_user_ms"] = fmt.Sprint(ru.Utime.Sec*1000 + ru.Utime.Usec/1000)
	trace.Meta["self_sys_ms"] = fmt.Sprint(ru.Stime.Sec*1000 + ru.Stime.Usec/1000)
	trace.Meta["wall_ms"] = fmt.Sprintf("%.0f", ms())
	trace.Meta["jobs"] = fmt.Sprint(*jobs)
	trace.Meta["gcflags"] = *gcflags
	trace.Meta["pipeline"] = fmt.Sprint(pipeline)
	trace.Meta["compiler"] = compiler
	trace.Meta["ldflags"] = *ldflags
	trace.Meta["packages"] = fmt.Sprint(len(pkgs))
	if *tracePath != "" {
		j, _ := json.Marshal(trace)
		os.WriteFile(*tracePath, j, 0o644)
	}
	if *dumpCmds != "" {
		sort.Strings(cmdLog)
		os.WriteFile(*dumpCmds, []byte(strings.ReplaceAll(strings.Join(cmdLog, "\n")+"\n", workdir, "$WORK")), 0o644)
	}
	for _, ph := range trace.Phases {
		fmt.Printf("%-32s %9.0f ms  cpu %9.0f ms  %12d bytes  %s\n", ph.Name, ph.End-ph.Start, ph.User+ph.Sys, ph.Bytes, ph.Note)
	}
	fmt.Printf("%-32s %9.0f ms\n", "TOTAL", ms())
}

// completeExempt: standard packages with Go declarations whose bodies come from elsewhere (linkname),
// so the compiler must not be told the package is complete. Same list as cmd/go.
var completeExempt = map[string]bool{"bytes": true, "internal/poll": true, "net": true, "os": true,
	"runtime/metrics": true, "runtime/pprof": true, "runtime/trace": true, "sync": true, "syscall": true, "time": true}

func buildPkg(p *Pkg, c int, goversion string, extra []string, exported func(pid int)) (user, sys float64, cmds []string, err error) {
	if err = os.MkdirAll(p.objdir, 0o755); err != nil {
		return
	}
	do := func(argv []string) error {
		if dryRun {
			cmds = append(cmds, strings.Join(argv, " "))
			return nil
		}
		u, s, _, e := run(p.Dir, argv, nil, nil)
		user += u
		sys += s
		cmds = append(cmds, strings.Join(argv, " "))
		return e
	}
	var cfg bytes.Buffer
	cfg.WriteString("# import config\n")
	keys := make([]string, 0, len(p.ImportMap))
	for k := range p.ImportMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&cfg, "importmap %s=%s\n", k, p.ImportMap[k])
	}
	for _, d := range p.deps {
		fmt.Fprintf(&cfg, "packagefile %s=%s\n", d.ImportPath, d.export)
	}
	cfgPath := filepath.Join(p.objdir, "importcfg")
	if err = os.WriteFile(cfgPath, cfg.Bytes(), 0o644); err != nil {
		return
	}
	trim := p.Dir + "=>" + p.ImportPath + ";" + p.objdir + "=>"
	if p.Module != nil && p.Module.Version != "" {
		trim = p.Dir + "=>" + p.ImportPath + "@" + p.Module.Version + ";" + p.objdir + "=>" // informational only
		trim = p.Dir + "=>" + p.Module.Path + "@" + p.Module.Version + strings.TrimPrefix(p.ImportPath, p.Module.Path) + ";" + p.objdir + "=>"
	}
	asmBase := []string{filepath.Join(tooldir, "asm"), "-p", p.ImportPath, "-trimpath", trim, "-I", p.objdir + "/",
		"-I", filepath.Join(goroot, "pkg", "include"), "-D", "GOOS_linux", "-D", "GOARCH_amd64"}
	if p.Standard {
		asmBase = append(asmBase, "-std")
	}
	asmBase = append(asmBase, "-D", "GOAMD64_v1")
	lang := "go" + strings.TrimPrefix(goversion, "go")
	if i := strings.Index(lang[2:], "."); i >= 0 {
		if j := strings.Index(lang[2+i+1:], "."); j >= 0 {
			lang = lang[:2+i+1+j]
		}
	}
	if p.Module != nil && p.Module.GoVersion == "" {
		lang = "go1.16" // a module with no go directive: cmd/go assumes 1.16 (older loop-variable semantics, etc.)
	}
	if p.Module != nil && p.Module.GoVersion != "" {
		v := strings.Split(p.Module.GoVersion, ".")
		if len(v) >= 2 {
			lang = "go" + v[0] + "." + v[1]
		}
	}
	argv := []string{compiler, "-o", p.export, "-trimpath", trim, "-p", p.ImportPath, "-lang=" + lang}
	if p.Name == "main" {
		argv[6] = "main"
	}
	if p.Standard {
		argv = append(argv, "-std")
	}
	if len(p.SFiles) == 0 && !(p.Standard && completeExempt[p.ImportPath]) {
		argv = append(argv, "-complete")
	}
	argv = append(argv, "-goversion", goversion)
	if len(p.SFiles) > 0 {
		symabis := filepath.Join(p.objdir, "symabis")
		os.WriteFile(filepath.Join(p.objdir, "go_asm.h"), nil, 0o644) // empty for this pass, as cmd/go does
		a := append(append([]string{}, asmBase...), "-gensymabis", "-o", symabis)
		for _, f := range p.SFiles {
			a = append(a, filepath.Join(p.Dir, f))
		}
		if err = do(a); err != nil {
			return
		}
		argv = append(argv, "-symabis", symabis)
	}
	argv = append(argv, fmt.Sprintf("-c=%d", c), "-nolocalimports", "-importcfg", cfgPath)
	if len(p.EmbedPatterns) > 0 {
		var ecfg []byte
		if ecfg, err = embedcfg(p); err != nil {
			return
		}
		ep := filepath.Join(p.objdir, "embedcfg")
		os.WriteFile(ep, ecfg, 0o644)
		argv = append(argv, "-embedcfg", ep)
	}
	argv = append(argv, "-pack")
	if len(p.SFiles) > 0 {
		argv = append(argv, "-asmhdr", filepath.Join(p.objdir, "go_asm.h"))
	}
	if pipeline {
		argv = append(argv, "-linkobj", p.archive)
	}
	for _, x := range extra { // %p in a flag becomes the package's work directory name (per-package profiles)
		argv = append(argv, strings.ReplaceAll(x, "%p", filepath.Base(p.objdir)))
	}
	for _, f := range p.GoFiles {
		argv = append(argv, filepath.Join(p.Dir, f))
	}
	if pipeline && !dryRun {
		u, s, e := runCompile(p.Dir, argv, exported)
		user += u
		sys += s
		cmds = append(cmds, strings.Join(argv, " "))
		if err = e; err != nil {
			return
		}
	} else if err = do(argv); err != nil {
		return
	}
	if len(p.SFiles) > 0 {
		var objs []string
		for _, f := range p.SFiles {
			o := filepath.Join(p.objdir, strings.TrimSuffix(f, ".s")+".o")
			a := append(append([]string{}, asmBase...), "-o", o, filepath.Join(p.Dir, f))
			if err = do(a); err != nil {
				return
			}
			objs = append(objs, o)
		}
		if !dryRun {
			err = appendArchive(p.archive, objs)
		}
	}
	return
}

// appendArchive adds object files to a Go archive, as cmd/go does itself (no ar, no pack).
func appendArchive(archive string, objs []string) error {
	f, err := os.OpenFile(archive, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, _ := f.Stat()
	if fi.Size()%2 == 1 {
		f.Write([]byte{0})
	}
	for _, o := range objs {
		b, err := os.ReadFile(o)
		if err != nil {
			return err
		}
		name := filepath.Base(o)
		if len(name) > 16 {
			name = name[:16]
		}
		fmt.Fprintf(f, "%-16s%-12d%-6d%-6d%-8o%-10d`\n", name, 0, 0, 0, 0o644, len(b))
		f.Write(b)
		if len(b)%2 == 1 {
			f.Write([]byte{0})
		}
	}
	return nil
}

// embedcfg resolves //go:embed patterns the way cmd/go does: a pattern is a glob relative to the package
// directory; a directory matches every file below it except those whose names begin with . or _
// (unless the pattern has the all: prefix).
func embedcfg(p *Pkg) ([]byte, error) {
	type cfgT struct {
		Patterns map[string][]string
		Files    map[string]string
	}
	cfg := cfgT{map[string][]string{}, map[string]string{}}
	for _, pat := range p.EmbedPatterns {
		glob, all := pat, false
		if strings.HasPrefix(pat, "all:") {
			glob, all = pat[4:], true
		}
		m, err := filepath.Glob(filepath.Join(p.Dir, filepath.FromSlash(glob)))
		if err != nil || len(m) == 0 {
			return nil, fmt.Errorf("%s: embed pattern %s: no match", p.ImportPath, pat)
		}
		var list []string
		for _, path := range m {
			fi, err := os.Lstat(path)
			if err != nil {
				return nil, err
			}
			rel, _ := filepath.Rel(p.Dir, path)
			if !fi.IsDir() {
				list = append(list, filepath.ToSlash(rel))
				continue
			}
			filepath.Walk(path, func(q string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				name := info.Name()
				if q != path && !all && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if info.IsDir() {
					if _, err := os.Stat(filepath.Join(q, "go.mod")); err == nil && q != path {
						return filepath.SkipDir
					}
					return nil
				}
				if !info.Mode().IsRegular() {
					return nil
				}
				r, _ := filepath.Rel(p.Dir, q)
				list = append(list, filepath.ToSlash(r))
				return nil
			})
		}
		sort.Strings(list)
		cfg.Patterns[pat] = list
		for _, r := range list {
			cfg.Files[r] = filepath.Join(p.Dir, filepath.FromSlash(r))
		}
	}
	return json.MarshalIndent(cfg, "", "\t")
}

// runCompile runs the patched compiler, which prints "sb:export-ready" when the file importers need
// is complete; exported is called at that moment.
func runCompile(dir string, argv []string, exported func(pid int)) (float64, float64, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if !noEarly {
		cmd.Env = append(cmd.Env, "SB_EARLY_EXPORT=1")
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.StdoutPipe()
	if err != nil {
		return 0, 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, 0, err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<16), 1<<24)
	var rest bytes.Buffer
	for sc.Scan() {
		if sc.Text() == "sb:export-ready" {
			exported(cmd.Process.Pid)
		} else {
			rest.WriteString(sc.Text() + "\n")
		}
	}
	err = cmd.Wait()
	u := float64(cmd.ProcessState.UserTime().Microseconds()) / 1000
	sy := float64(cmd.ProcessState.SystemTime().Microseconds()) / 1000
	if err != nil {
		err = fmt.Errorf("%s: %v\n%s%s", strings.Join(argv, " "), err, rest.String(), errb.String())
	}
	return u, sy, err
}

// renice puts every thread of a process at the lowest CPU priority. Linux priorities are per thread and
// new threads inherit their creator's, so two passes over /proc/pid/task catch a thread born during the first.
func renice(pid int) {
	for pass := 0; pass < 2; pass++ {
		ents, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
		if err != nil {
			return
		}
		for _, e := range ents {
			if tid, err := strconv.Atoi(e.Name()); err == nil {
				syscall.Setpriority(syscall.PRIO_PROCESS, tid, 19)
			}
		}
	}
}
