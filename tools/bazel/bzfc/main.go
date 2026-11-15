// bzfc: build a Bazel go_binary with fc (driver/fc), from Bazel's own action graph.
//
//	bzfc -aquery aq.json -ob OUTPUT_BASE -ws WORKSPACE -label //pkg:bin -work DIR -fc build/fc -bin OUT [-j N]
//
// aq.json is `bazel aquery --output=jsonproto --include_file_write_contents --include_param_files 'deps(LABEL)'`
// from an output base whose external repositories are fetched (the aquery fetches them). Nothing is built by
// Bazel: bzfc reads the DAG and executes it its own way.
//
//   - The packages. The binary's GoLink action (target configuration) names the main package's archive; the
//     closure is followed from there through rules_go's GoCompilePkg actions (-arc importpath=importmap=archive),
//     so every package is the one Bazel would compile, in the configuration Bazel would compile it in (select()s
//     and transitions resolved by Bazel's analysis). From each action: -importpath, -p (importmap), -src (.go,
//     .s, .h), -embedsrc with -embedroot/-embedlookupdir, -tags. Files are filtered by build constraints as
//     rules_go's builder does (go/build), with CGO_ENABLED=0. The standard library is not Bazel's GoStdlib: it is
//     `go list -deps std` of the GOROOT fc is built with (-go), which fc compiles from source like every package.
//   - Generated sources (protoc-gen-go, goyacc, genrules). The actions that write them, and the actions those
//     need, are run here, in DIR/execroot (symlinks to the workspace's top-level entries and to the output base's
//     external/; DIR/execroot/bazel-out real). Go tools those actions run (their GoLink actions) are built by fc
//     too, each in one process; GoCompilePkg, GoStdlib and rules_go's builder are never run. FileWrite, Symlink,
//     ExecutableSymlink and TemplateExpand are done in process; runfiles trees are not made (the tools seen so
//     far do not read runfiles); anything else runs its argv with its environment, like Bazel's local strategy
//     without the sandbox.
//   - The binary: one fc build of the closure, x_defs passed as -X unless they name stamp keys (rules_go drops
//     those when stamping is off, the default).
//
// Prints a line per stage with wall ms and, for the generation stage, the core-seconds of its children.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type kv struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type action struct {
	TargetID        int      `json:"targetId"`
	Mnemonic        string   `json:"mnemonic"`
	ConfigurationID int      `json:"configurationId"`
	Arguments       []string `json:"arguments"`
	Env             []kv     `json:"environmentVariables"`
	InputDepSetIDs  []int    `json:"inputDepSetIds"`
	OutputIDs       []int    `json:"outputIds"`
	PrimaryOutputID int      `json:"primaryOutputId"`
	FileContents    string   `json:"fileContents"`
	IsExecutable    bool     `json:"isExecutable"`
	TemplateContent string   `json:"templateContent"`
	Substitutions   []kv     `json:"substitutions"`
	ParamFiles      []struct {
		ExecPath  string   `json:"execPath"`
		Arguments []string `json:"arguments"`
	} `json:"paramFiles"`

	args    map[string][]string
	label   string
	tool    bool
	outs    []string
	pkg     *Pkg // GoCompilePkg
	done    chan struct{}
	err     error
	started bool
}

type aquery struct {
	Artifacts []struct {
		ID             int  `json:"id"`
		PathFragmentID int  `json:"pathFragmentId"`
		IsTreeArtifact bool `json:"isTreeArtifact"`
	} `json:"artifacts"`
	Actions []*action `json:"actions"`
	DepSets []struct {
		ID                  int   `json:"id"`
		DirectArtifactIDs   []int `json:"directArtifactIds"`
		TransitiveDepSetIDs []int `json:"transitiveDepSetIds"`
	} `json:"depSetOfFiles"`
	Targets []struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
	} `json:"targets"`
	Configuration []struct {
		ID       int    `json:"id"`
		Mnemonic string `json:"mnemonic"`
		IsTool   bool   `json:"isTool"`
	} `json:"configuration"`
	PathFragments []struct {
		ID       int    `json:"id"`
		Label    string `json:"label"`
		ParentID int    `json:"parentId"`
	} `json:"pathFragments"`
}

// Pkg: the fields of driver/fc/load.go's Pkg that a plan carries.
type Pkg struct {
	ImportPath string
	Dir        string
	Name       string
	GoFiles    []string
	SFiles     []string          `json:",omitempty"`
	Imports    []string          `json:",omitempty"`
	ImportMap  map[string]string `json:",omitempty"`
	EmbedFiles []string          `json:",omitempty"`

	a    *action
	deps []*Pkg
	cgo  bool // a cgo package, with cgo on: the go command's (cgo.go)
}

var (
	flagAq    = flag.String("aquery", "", "aquery jsonproto output")
	flagOB    = flag.String("ob", "", "Bazel output base")
	flagWS    = flag.String("ws", "", "workspace directory")
	flagLabel = flag.String("label", "", "the go_binary's label")
	flagWork  = flag.String("work", "", "work directory (made empty)")
	flagFC    = flag.String("fc", "", "fc binary")
	flagBin   = flag.String("bin", "", "the binary to write")
	flagJ     = flag.Int("j", 8, "actions at once; fc's -j")
	flagGo    = flag.String("go", "/home/exedev/targets/goroot-sb/bin/go", "the go command of the GOROOT fc is built with (the standard library)")
	flagStats = flag.Bool("stats", false, "only print what the closure has: packages, generated files, cgo, assembly, embeds")
	flagDump  = flag.String("dump", "", "only print the actions (mnemonic, label, outputs, argv) whose label or an output matches this regexp")
)

var (
	aq       aquery
	paths    map[int]string
	depsets  map[int][2][]int
	byOutput = map[string]*action{}
	execroot string
	t0       = time.Now()
	stdJSON  []json.RawMessage
	stdErr   error
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("bzfc: ")
	flag.Parse()
	go stdList() // in parallel with reading the DAG
	f, err := os.Open(*flagAq)
	if err != nil {
		log.Fatal(err)
	}
	if err := readAquery(f); err != nil {
		log.Fatal(err)
	}
	f.Close()
	index()
	// (what only reading the DAG needed: about half of the executor's 6 GB heap)
	aq.PathFragments, aq.Artifacts, aq.DepSets = nil, nil, nil
	runtime.GC()
	debug.FreeOSMemory()
	stage("read the DAG (%d actions)", len(aq.Actions))
	if *flagDump != "" {
		re := regexp.MustCompile(*flagDump)
		for _, a := range aq.Actions {
			if re.MatchString(a.label) || re.MatchString(strings.Join(a.outs, " ")) {
				o, _ := json.Marshal(a.outs)
				v, _ := json.Marshal(a.Arguments)
				fmt.Printf("== %s %s tool=%v\nouts %s\nargv %s\nenv %v\n", a.Mnemonic, a.label, a.tool, o, v, a.Env)
			}
		}
		return
	}
	label := trimRepo(*flagLabel)
	var link *action
	for _, a := range aq.Actions {
		if a.Mnemonic == "GoLink" && a.label == label && !a.tool {
			link = a
		}
	}
	if link == nil {
		log.Fatalf("no GoLink action of %s in the target configuration", label)
	}
	if *flagStats {
		stats(link)
		return
	}
	execroot = filepath.Join(*flagWork, "execroot")
	os.RemoveAll(*flagWork)
	if err := mkExecroot(); err != nil {
		log.Fatal(err)
	}
	// generation: every generated input of the closure, and what that needs
	var c0 float64 = childCPU()
	root, gen := closure(link)
	nact := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, *flagJ)
	var errs []error
	var emu sync.Mutex
	var run func(a *action) // schedule a and its producers; idempotent
	var mu sync.Mutex
	run = func(a *action) {
		mu.Lock()
		if a.started {
			mu.Unlock()
			return
		}
		a.started = true
		a.done = make(chan struct{})
		nact++
		mu.Unlock()
		pre := producers(a)
		for _, p := range pre {
			run(p)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(a.done)
			for _, p := range pre {
				<-p.done
				if p.err != nil {
					a.err = fmt.Errorf("after %s", p.outs[0])
					return
				}
			}
			if a.Mnemonic != "GoLink" {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			ts := time.Now()
			a.err = execute(a)
			if a.err != nil {
				emu.Lock()
				errs = append(errs, fmt.Errorf("%s %s: %v", a.Mnemonic, a.outs[0], a.err))
				emu.Unlock()
			} else if os.Getenv("BZFC_V") != "" {
				fmt.Fprintf(os.Stderr, "  %6.0f ms %s %s\n", ms(time.Since(ts)), a.Mnemonic, a.outs[0])
			}
		}()
	}
	for _, g := range gen {
		w := byOutput[g]
		if w == nil {
			log.Fatalf("nothing in the DAG writes %s", g)
		}
		run(w)
	}
	// the binary's cgo closure (the go command's) as soon as its own generated files are there, beside
	// the rest of the generation (cgo.go: stockCgo is once per closure; fcBuild's call then waits for it)
	go prestock(root)
	wg.Wait()
	if len(errs) > 0 {
		for _, e := range errs {
			log.Print(e)
		}
		log.Fatalf("generation failed")
	}
	stage("generate: %d files, %d actions run, %.2f core-s, %d retried", len(gen), nact, childCPU()-c0, retries.Load())
	// the binary
	// x_defs as rules_go's link does: {KEY} from the workspace status when the workspace stamps (its
	// .bazelrc says --stamp), and an x_def that names a key there is no value for is dropped
	stamp := stampValues()
	var xdefs []string
	for _, x := range link.args["-X"] {
		missing := false
		x = regexp.MustCompile(`\{.+?\}`).ReplaceAllStringFunc(x, func(k string) string {
			if v, ok := stamp[k[1:len(k)-1]]; ok {
				return v
			}
			missing = true
			return k
		})
		if !missing {
			xdefs = append(xdefs, x)
		}
	}
	_ = root
	if err := fcBuild(link, *flagBin, xdefs, "main"); err != nil {
		log.Fatal(err)
	}
	stage("fc build %s", *flagBin)
}

func stage(format string, a ...any) {
	fmt.Printf("bzfc: %6.0f ms  %s\n", ms(time.Since(t0)), fmt.Sprintf(format, a...))
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func childCPU() float64 {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_CHILDREN, &ru)
	return float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6 + float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
}

func trimRepo(l string) string { return strings.TrimPrefix(strings.TrimPrefix(l, "@@"), "@") }

func index() {
	frag := make(map[int]int, len(aq.PathFragments))
	for i, f := range aq.PathFragments {
		frag[f.ID] = i
	}
	memo := map[int]string{}
	var fpath func(id int) string
	fpath = func(id int) string {
		if s, ok := memo[id]; ok {
			return s
		}
		f := aq.PathFragments[frag[id]]
		s := f.Label
		if f.ParentID != 0 {
			s = fpath(f.ParentID) + "/" + s
		}
		memo[id] = s
		return s
	}
	paths = make(map[int]string, len(aq.Artifacts))
	for _, a := range aq.Artifacts {
		paths[a.ID] = fpath(a.PathFragmentID)
	}
	depsets = make(map[int][2][]int, len(aq.DepSets))
	for _, d := range aq.DepSets {
		depsets[d.ID] = [2][]int{d.DirectArtifactIDs, d.TransitiveDepSetIDs}
	}
	tool := map[int]bool{}
	for _, c := range aq.Configuration {
		tool[c.ID] = c.IsTool
	}
	labels := map[int]string{}
	for _, t := range aq.Targets {
		labels[t.ID] = trimRepo(t.Label)
	}
	for _, a := range aq.Actions {
		a.label, a.tool = labels[a.TargetID], tool[a.ConfigurationID]
		if a.args == nil {
			a.args = parseArgs(a.Arguments) // (GoCompilePkg's: as read, readAquery)
		}
		for _, o := range a.OutputIDs {
			a.outs = append(a.outs, paths[o])
			byOutput[paths[o]] = a
		}
		if len(a.outs) == 0 {
			a.outs = []string{"(no output)"}
		}
	}
}

// inputs: the exec paths of an action's inputs.
func inputs(a *action) []string {
	var out []string
	seen := map[int]bool{}
	var walk func(id int)
	walk = func(id int) {
		if seen[id] {
			return
		}
		seen[id] = true
		d := depsets[id]
		for _, x := range d[0] {
			out = append(out, paths[x])
		}
		for _, t := range d[1] {
			walk(t)
		}
	}
	for _, id := range a.InputDepSetIDs {
		walk(id)
	}
	return out
}

// skipped: actions whose work fc does (or that only Bazel needs).
var skipped = map[string]bool{
	"GoCompilePkg": true, "GoStdlibList": true, "GoToolchainBinaryBuild": true,
	"RunfilesTree": true, "SymlinkTree": true, "SourceSymlinkManifest": true, "RepoMappingManifest": true,
	"BazelWorkspaceStatusAction": true, "TranslateBuildInfo": true, "ProtocAuthenticityCheck": true,
	"GoCover": true, "GoNogo": true, "ValidateNogo": true, "Middleman": true,
}

// producers: the actions that must run before a, by its inputs. A GoLink's are the generated inputs of the
// tool's own package closure (its compile actions are fc's work).
func producers(a *action) []*action {
	var in []string
	if a.Mnemonic == "GoLink" {
		_, in = closure(a)
	} else if isRender(a) {
		return nil // (rendered here from source: render.go)
	} else {
		in = inputs(a)
	}
	var out []*action
	seen := map[*action]bool{}
	for _, x := range in {
		p := byOutput[x]
		if p == nil || seen[p] || skipped[p.Mnemonic] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

var closureMu sync.Mutex

// closure: the package closure of a GoLink action (main first), and its generated inputs.
func closure(link *action) (*Pkg, []string) {
	closureMu.Lock()
	defer closureMu.Unlock()
	mainA := first(link.args["-main"])
	ca := byOutput[mainA]
	if ca == nil || ca.Mnemonic != "GoCompilePkg" {
		log.Fatalf("%s: no compile action writes %s", link.outs[0], mainA)
	}
	var gen []string
	seen := map[*action]bool{}
	var walk func(a *action) *Pkg
	walk = func(a *action) *Pkg {
		if a.pkg != nil && seen[a] {
			return a.pkg
		}
		seen[a] = true
		if a.pkg == nil {
			p := &Pkg{ImportPath: first(a.args["-p"]), a: a, ImportMap: map[string]string{}}
			if p.ImportPath == "" {
				p.ImportPath = first(a.args["-importpath"])
			}
			a.pkg = p
			for _, arc := range a.args["-arc"] {
				parts := strings.SplitN(arc, "=", 3)
				if len(parts) != 3 {
					log.Fatalf("%s: -arc %q", p.ImportPath, arc)
				}
				d := byOutput[parts[2]]
				if d == nil || d.Mnemonic != "GoCompilePkg" {
					log.Fatalf("%s: no compile action writes %s", p.ImportPath, parts[2])
				}
				if parts[0] != parts[1] {
					p.ImportMap[parts[0]] = parts[1]
				}
				p.deps = append(p.deps, walk(d))
			}
		} else {
			for _, d := range a.pkg.deps {
				walk(d.a)
			}
		}
		for _, s := range append(append([]string{}, a.args["-src"]...), a.args["-embedsrc"]...) {
			if strings.HasPrefix(s, "bazel-out/") {
				gen = append(gen, s)
			}
		}
		return a.pkg
	}
	root := walk(ca)
	return root, gen
}

func allPkgs(root *Pkg) []*Pkg {
	var order []*Pkg
	seen := map[*Pkg]bool{}
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
	walk(root)
	return order
}

func abs(x string) string {
	switch {
	case filepath.IsAbs(x):
		return x
	case execroot != "":
		return filepath.Join(execroot, x)
	case strings.HasPrefix(x, "external/"):
		return filepath.Join(*flagOB, x)
	}
	return filepath.Join(*flagWS, x)
}

func mkExecroot() error {
	if err := os.MkdirAll(filepath.Join(execroot, "bazel-out"), 0o777); err != nil {
		return err
	}
	ents, err := os.ReadDir(*flagWS)
	if err != nil {
		return err
	}
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "bazel-") || n == "external" || n == ".git" {
			continue
		}
		if err := os.Symlink(filepath.Join(*flagWS, n), filepath.Join(execroot, n)); err != nil {
			return err
		}
	}
	return os.Symlink(filepath.Join(*flagOB, "external"), filepath.Join(execroot, "external"))
}

// execute runs one action of the generation stage.
func execute(a *action) error {
	for _, o := range a.outs {
		os.MkdirAll(filepath.Dir(abs(o)), 0o777)
	}
	switch a.Mnemonic {
	case "GoLink":
		return fcBuild(a, abs(a.outs[0]), nil, "tool")
	case "GoStdlib":
		// a tool that wants a GOROOT (goimports, through GOROOT=<stdlib_>) gets fc's: the standard library is
		// fc's work, from that GOROOT's sources, and nothing reads Bazel's compiled archives
		d := filepath.Dir(abs(a.outs[0]))
		os.MkdirAll(d, 0o777)
		ents, err := os.ReadDir(goroot())
		if err != nil {
			return err
		}
		for _, e := range ents {
			if err := os.Symlink(filepath.Join(goroot(), e.Name()), filepath.Join(d, e.Name())); err != nil && !os.IsExist(err) {
				return err
			}
		}
		return nil
	case "FileWrite":
		mode := os.FileMode(0o666)
		if a.IsExecutable {
			mode = 0o777
		}
		return os.WriteFile(abs(a.outs[0]), []byte(a.FileContents), mode)
	case "TemplateExpand":
		s := a.TemplateContent
		for _, kv := range a.Substitutions {
			s = strings.ReplaceAll(s, kv.Key, kv.Value)
		}
		mode := os.FileMode(0o666)
		if a.IsExecutable {
			mode = 0o777
		}
		return os.WriteFile(abs(a.outs[0]), []byte(s), mode)
	case "Symlink", "ExecutableSymlink", "SolibSymlink":
		in := inputs(a)
		if len(in) != 1 {
			return fmt.Errorf("%d inputs", len(in))
		}
		return os.Symlink(abs(in[0]), abs(a.outs[0]))
	}
	if isRender(a) {
		return render(a)
	}
	if len(a.Arguments) == 0 {
		return fmt.Errorf("no command line")
	}
	for _, pf := range a.ParamFiles {
		os.MkdirAll(filepath.Dir(abs(pf.ExecPath)), 0o777)
		if err := os.WriteFile(abs(pf.ExecPath), []byte(strings.Join(pf.Arguments, "\n")+"\n"), 0o666); err != nil {
			return err
		}
	}
	argv0 := a.Arguments[0]
	if !strings.Contains(argv0, "/") {
		if p, err := exec.LookPath(argv0); err == nil {
			argv0 = p
		}
	} else if !filepath.IsAbs(argv0) {
		argv0 = abs(argv0)
	}
	// A tool fc built hung once in about a thousand runs (go_generics, 2026-11-14; not reproduced in 320 runs
	// of 8 builds): an action that outlives -action_timeout is killed and run again, once, and counted.
	for try := 0; ; try++ {
		ctx, cancel := context.WithTimeout(context.Background(), *flagActionTimeout)
		cmd := exec.CommandContext(ctx, argv0, a.Arguments[1:]...)
		cmd.Dir = execroot
		cmd.Env = []string{"PATH=/bin:/usr/bin", "HOME=" + os.Getenv("HOME"), "TMPDIR=" + filepath.Join(*flagWork, "tmp")}
		os.MkdirAll(filepath.Join(*flagWork, "tmp"), 0o777)
		for _, kv := range a.Env {
			cmd.Env = append(cmd.Env, kv.Key+"="+kv.Value)
		}
		out, err := cmd.CombinedOutput()
		timedOut := ctx.Err() != nil
		cancel()
		if err == nil {
			return nil
		}
		if timedOut && try == 0 {
			retries.Add(1)
			fmt.Fprintf(os.Stderr, "bzfc: %s %s: no exit after %v; run again\n", a.Mnemonic, a.outs[0], *flagActionTimeout)
			continue
		}
		return fmt.Errorf("%v\n%s", err, bytes.TrimSpace(out))
	}
}

var (
	flagActionTimeout = flag.Duration("action_timeout", 60*time.Second, "kill an action that runs longer, and run it again once")
	retries           atomic.Int64
)

type built struct {
	done chan struct{}
	bin  string
	err  error
}

var (
	builtMu sync.Mutex
	builtBy = map[[32]byte]*built{} // plan content -> its build: the same tool in two configurations is built once
)

// fcBuild plans a GoLink action's closure and builds it with fc into bin.
func fcBuild(link *action, bin string, xdefs []string, what string) error {
	root, _ := closure(link)
	pkgs := allPkgs(root)
	name := strings.NewReplacer("/", "%", ":", "%").Replace(link.outs[0])
	dir := filepath.Join(*flagWork, "builds", name)
	os.MkdirAll(dir, 0o777)
	plan := filepath.Join(dir, "plan.json")
	b, stock, err := writePlan(pkgs, plan, filepath.Join(*flagWork, "farm"))
	if err != nil {
		return err
	}
	key := sha256.Sum256(append(b, []byte(strings.Join(xdefs, "\n"))...))
	builtMu.Lock()
	if o := builtBy[key]; o != nil {
		builtMu.Unlock()
		<-o.done
		if o.err != nil {
			return o.err
		}
		return os.Symlink(o.bin, bin)
	}
	me := &built{done: make(chan struct{}), bin: bin}
	builtBy[key] = me
	builtMu.Unlock()
	defer close(me.done)
	args := []string{"build", "-j", strconv.Itoa(*flagJ), "-plan", plan, "-o", filepath.Join(dir, "objs"), "-link", bin}
	for _, x := range xdefs {
		args = append(args, "-X", x)
	}
	cmd := exec.Command(*flagFC, append(args, root.ImportPath)...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local")
	if stock != nil {
		cmd.Env = append(cmd.Env, stock.env...) // (CGO_ENABLED=1 among them, later wins)
	}
	out, err := cmd.CombinedOutput()
	os.WriteFile(filepath.Join(dir, "build.log"), out, 0o666)
	if err != nil {
		me.err = fmt.Errorf("fc build of %s: %v\n%s", link.label, err, tail(out, 20))
		return me.err
	}
	if what == "main" {
		os.Stdout.Write(out)
	}
	os.RemoveAll(filepath.Join(dir, "objs"))
	return nil
}

func tail(b []byte, n int) string {
	l := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

var stdReady = make(chan struct{})

func stdList() {
	defer close(stdReady)
	cmd := exec.Command(*flagGo, "list", "-deps", "-json", "std")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOFLAGS=", "GOTOOLCHAIN=local", "GO111MODULE=off")
	cmd.Dir = "/"
	cmd.Stderr = os.Stderr
	outb, err := cmd.Output()
	if err != nil {
		stdErr = fmt.Errorf("go list std: %v", err)
		return
	}
	dec := json.NewDecoder(bytes.NewReader(outb))
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			stdErr = err
			return
		}
		stdJSON = append(stdJSON, raw)
	}
}

func ctxtFor(a *action, cgo bool) build.Context {
	ctxt := build.Default
	ctxt.GOOS, ctxt.GOARCH, ctxt.CgoEnabled, ctxt.Compiler = "linux", "amd64", cgo, "gc"
	ctxt.BuildTags = nil
	for _, t := range a.args["-tags"] {
		ctxt.BuildTags = append(ctxt.BuildTags, strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == ' ' })...)
	}
	return ctxt
}

// files: an action's sources split as fc takes them, with CGO_ENABLED=0. cgo: the package has cgo code in a
// cgo configuration. A cgo package in a configuration with cgo on (the action's CGO_ENABLED=1: rules_go's
// pure=off with a C toolchain) is split in that configuration instead: its Go files, cgo ones included (the
// package is the go command's: cgo.go).
func files(a *action) (gofiles, sfiles, hfiles []string, cgo bool, err error) {
	ctxt, cgoCtxt := ctxtFor(a, false), ctxtFor(a, true)
	for _, s := range a.args["-src"] {
		x := abs(s)
		switch ext := filepath.Ext(s); ext {
		case ".c", ".cc", ".cpp", ".cxx", ".m", ".S", ".go":
			if okc, _ := cgoCtxt.MatchFile(filepath.Dir(x), filepath.Base(x)); okc && (ext != ".go" || usesCgo(x)) {
				cgo = true
			}
		}
	}
	if cgo && cgoOn(a) {
		ctxt = cgoCtxt
	}
	for _, s := range a.args["-src"] {
		x := abs(s)
		ext := filepath.Ext(s)
		switch ext {
		case ".h", ".hh", ".hpp", ".inc":
			hfiles = append(hfiles, x)
			continue
		case ".go", ".s", ".S", ".c", ".cc", ".cpp", ".cxx", ".m", ".syso":
		default:
			continue
		}
		ok, err := ctxt.MatchFile(filepath.Dir(x), filepath.Base(x))
		if err != nil {
			return nil, nil, nil, false, err
		}
		if !ok {
			continue
		}
		switch ext {
		case ".go":
			gofiles = append(gofiles, x)
		case ".s":
			sfiles = append(sfiles, x)
		default:
			if !ctxt.CgoEnabled {
				return nil, nil, nil, cgo, fmt.Errorf("%s needs cgo", s)
			}
		}
	}
	return
}

// cgoOn: does the action compile in a configuration with cgo on?
func cgoOn(a *action) bool {
	for _, kv := range a.Env {
		if kv.Key == "CGO_ENABLED" {
			return kv.Value == "1"
		}
	}
	return false
}

// writePlan: fc's plan of a closure; with cgo packages in it, also the go command's part (cgo.go) and the
// environment fc needs for it.
func writePlan(pkgs []*Pkg, plan, farm string) ([]byte, *stockSet, error) {
	have := map[string]bool{}
	for _, p := range pkgs {
		have[p.ImportPath] = true
	}
	for _, p := range pkgs {
		if err := fill(p, farm); err != nil {
			return nil, nil, fmt.Errorf("%s: %v", p.ImportPath, err)
		}
	}
	stock, err := stockCgo(pkgs)
	if err != nil {
		return nil, nil, err
	}
	var all []json.RawMessage
	for _, p := range pkgs {
		if stock != nil && stock.entries[p.ImportPath] != nil {
			all = append(all, stock.entries[p.ImportPath])
			continue
		}
		b, _ := json.Marshal(p)
		all = append(all, b)
	}
	<-stdReady
	if stdErr != nil {
		return nil, nil, stdErr
	}
	for _, s := range stdJSON {
		var hdr struct{ ImportPath string }
		json.Unmarshal(s, &hdr)
		if have[hdr.ImportPath] {
			continue
		}
		have[hdr.ImportPath] = true
		if stock != nil && stock.entries[hdr.ImportPath] != nil {
			s = stock.entries[hdr.ImportPath]
		}
		all = append(all, s)
	}
	if stock != nil {
		var extra []string
		for ip := range stock.entries {
			if !have[ip] {
				extra = append(extra, ip)
			}
		}
		sort.Strings(extra)
		for _, ip := range extra {
			all = append(all, stock.entries[ip])
		}
	}
	b, _ := json.Marshal(all)
	return b, stock, os.WriteFile(plan, b, 0o666)
}

var fillMu sync.Mutex

// fill: a package's files, directory, name and imports, from its compile action (once).
func fill(p *Pkg, farm string) error {
	fillMu.Lock()
	defer fillMu.Unlock()
	if p.Dir != "" {
		return nil
	}
	a := p.a
	gofiles, sfiles, hfiles, cgo, err := files(a)
	if err != nil {
		return err
	}
	p.cgo = cgo && cgoOn(a)
	if p.cgo {
		sfiles, hfiles = nil, nil // (the go command builds this package from its directory: cgo.go)
	}
	for _, g := range gofiles {
		if !p.cgo && usesCgo(g) {
			return fmt.Errorf("%s imports \"C\" with CGO_ENABLED=0", g)
		}
	}
	// embeds: relative to the first root (and lookup dir) they are found under
	var embeds [][2]string // rel, abs
	for _, e := range a.args["-embedsrc"] {
		rel := ""
	roots:
		for _, r := range a.args["-embedroot"] {
			for _, l := range a.args["-embedlookupdir"] {
				pre := filepath.Join(r, l) + "/"
				if pre == "/" {
					pre = ""
				}
				if strings.HasPrefix(e, pre) {
					rel = strings.TrimPrefix(e, pre)
					break roots
				}
			}
		}
		if rel == "" {
			return fmt.Errorf("embed %s under no embed root", e)
		}
		embeds = append(embeds, [2]string{rel, abs(e)})
	}
	// Dir: the directory of the first source not generated; one farm directory of symlinks when the assembly,
	// headers or embeds are not all in it (the assembler and fc's embed read files by Dir)
	dir := ""
	for _, s := range a.args["-src"] {
		if !strings.HasPrefix(s, "bazel-out/") {
			dir = filepath.Dir(abs(s))
			break
		}
	}
	if dir == "" && len(gofiles) > 0 {
		dir = filepath.Dir(gofiles[0])
	}
	needFarm := false
	for _, x := range append(append([]string{}, sfiles...), hfiles...) {
		if filepath.Dir(x) != dir {
			needFarm = true
		}
	}
	for _, e := range embeds {
		if filepath.Join(dir, e[0]) != e[1] {
			needFarm = true
		}
	}
	if needFarm {
		dir = filepath.Join(farm, strings.ReplaceAll(first(a.args["-o"]), "/", "%")) // (one per compile action: a package may be in several configurations)
		link := func(rel, target string) error {
			dst := filepath.Join(dir, rel)
			os.MkdirAll(filepath.Dir(dst), 0o777)
			return os.Symlink(target, dst)
		}
		for _, x := range append(append([]string{}, sfiles...), hfiles...) {
			if err := link(filepath.Base(x), x); err != nil {
				return err
			}
		}
		for _, e := range embeds {
			if err := link(e[0], e[1]); err != nil {
				return err
			}
		}
	}
	p.Dir = dir
	for _, g := range gofiles {
		if filepath.Dir(g) == dir {
			g = filepath.Base(g)
		}
		p.GoFiles = append(p.GoFiles, g)
	}
	for _, s := range sfiles {
		p.SFiles = append(p.SFiles, filepath.Base(s))
	}
	for _, e := range embeds {
		p.EmbedFiles = append(p.EmbedFiles, e[0])
	}
	imps := map[string]bool{}
	fset := token.NewFileSet()
	for _, g := range gofiles {
		af, err := parser.ParseFile(fset, g, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		if p.Name == "" {
			p.Name = af.Name.Name
		}
		for _, is := range af.Imports {
			ip, _ := strconv.Unquote(is.Path.Value)
			imps[ip] = true
		}
	}
	for ip := range imps {
		p.Imports = append(p.Imports, ip)
	}
	sort.Strings(p.Imports)
	if len(p.ImportMap) == 0 {
		p.ImportMap = nil
	}
	return nil
}

// stats: what the closure has, without building anything (files that do not exist yet are counted as
// generated and not opened).
func stats(link *action) {
	root, gen := closure(link)
	pkgs := allPkgs(root)
	var nGo, nS, nEmbed, nTags int
	var cgoPkgs []string
	byMn := map[string]int{}
	for _, g := range gen {
		if w := byOutput[g]; w != nil {
			byMn[w.Mnemonic]++
		}
	}
	for _, p := range pkgs {
		a := p.a
		cgo := false
		for _, s := range a.args["-src"] {
			switch filepath.Ext(s) {
			case ".go":
				nGo++
			case ".s":
				nS++
			case ".c", ".cc", ".cpp", ".S", ".m":
				cgo = true
			}
		}
		if first(a.args["-cgo"]) != "" || hasFlag(a.Arguments, "-cgo") {
			cgo = true
		}
		if cgo {
			cgoPkgs = append(cgoPkgs, p.ImportPath)
		}
		nEmbed += len(a.args["-embedsrc"])
		if len(a.args["-tags"]) > 0 {
			nTags++
		}
	}
	fmt.Printf("packages %d (Bazel's; the standard library apart)\ngo_srcs %d\nasm_srcs %d\nembedsrcs %d\npackages_with_tags %d\ngenerated %d %v\ncgo_packages %d\n",
		len(pkgs), nGo, nS, nEmbed, nTags, len(gen), byMn, len(cgoPkgs))
	for _, c := range cgoPkgs {
		fmt.Println("cgo", c)
	}
	var x []string
	x = append(x, link.args["-X"]...)
	fmt.Printf("x_defs %d %v\n", len(x), x)
	// what Bazel itself would run for the binary: every action its GoLink depends on, by mnemonic and
	// configuration (target or tool)
	seenA := map[*action]bool{}
	var walkA func(a *action)
	walkA = func(a *action) {
		if seenA[a] {
			return
		}
		seenA[a] = true
		for _, in := range inputs(a) {
			if p := byOutput[in]; p != nil {
				walkA(p)
			}
		}
	}
	walkA(link)
	byMnCfg := map[string]int{}
	for a := range seenA {
		c := "target"
		if a.tool {
			c = "tool"
		}
		byMnCfg[a.Mnemonic+" "+c]++
	}
	var keys []string
	for k := range byMnCfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("bazel_actions %d\n", len(seenA))
	for _, k := range keys {
		fmt.Printf("bazel_action %s %d\n", k, byMnCfg[k])
	}
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func parseArgs(a []string) map[string][]string {
	m := map[string][]string{}
	for i := 0; i < len(a); i++ {
		if a[i] == "--" {
			break
		}
		switch a[i] {
		case "-src", "-embedsrc", "-embedroot", "-embedlookupdir", "-arc", "-importpath", "-p", "-o", "-lo", "-tags", "-main", "-X", "-importcfg":
			if i+1 < len(a) {
				m[a[i]] = append(m[a[i]], a[i+1])
				i++
			}
		}
	}
	return m
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// stampValues: the workspace status (KEY value lines of --workspace_status_command) when the workspace's
// .bazelrc turns stamping on for builds; nil otherwise (Bazel's default: no stamping).
func stampValues() map[string]string {
	b, err := os.ReadFile(filepath.Join(*flagWS, ".bazelrc"))
	if err != nil {
		return nil
	}
	stamp, cmdline := false, ""
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || (f[0] != "build" && f[0] != "common") {
			continue // (configs such as build:release apply only when asked for)
		}
		for i := 1; i < len(f); i++ {
			switch {
			case f[i] == "--stamp":
				stamp = true
			case f[i] == "--nostamp":
				stamp = false
			case f[i] == "--workspace_status_command" && i+1 < len(f):
				cmdline = f[i+1]
				i++
			case strings.HasPrefix(f[i], "--workspace_status_command="):
				cmdline = strings.TrimPrefix(f[i], "--workspace_status_command=")
			}
		}
	}
	if !stamp {
		return nil
	}
	m := map[string]string{}
	if cmdline == "" {
		return m
	}
	cmd := exec.Command("/bin/bash", "-c", cmdline)
	cmd.Dir = *flagWS
	out, err := cmd.Output()
	if err != nil {
		log.Printf("workspace status: %v", err)
		return m
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, _ := strings.Cut(line, " ")
		if k != "" {
			m[k] = v
		}
	}
	return m
}

// goroot: the GOROOT of -go.
func goroot() string { return filepath.Dir(filepath.Dir(*flagGo)) }

// usesCgo: does the file import "C"?
func usesCgo(path string) bool {
	af, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return false
	}
	for _, is := range af.Imports {
		if is.Path.Value == `"C"` {
			return true
		}
	}
	return false
}
