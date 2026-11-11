// plan.go: the package graph without `go list`.
//
// The driver finds the build's packages itself, in the same pass that reads their sources:
//
//	round k:  list the directories of the packages discovered in round k-1, read every candidate file,
//	          keep the files whose name and build constraints match (go/build's own MatchFile, run on
//	          the bytes already in memory), concatenate them and run Strand's declaration pass over the
//	          concatenation (examples/godecl.strand: package clauses and import paths, among others);
//	          resolve the imports to directories: those are round k+1.
//
// What is Strand here: every byte of Go source is lexed and its package-level declarations found by
// godecl. What is not: module resolution (go.mod, go.work, vendor/modules.txt: a few hundred lines of
// text), build-constraint evaluation (the comment block at the head of each file) and the breadth-first
// loop. See SCALAR_LEDGER.md.
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Facts about the installed toolchain, written once by `sb -mkfacts` when the driver is built.
type Facts struct {
	GOROOT, GOVERSION, GOMODCACHE string
	ToolTags, ReleaseTags         []string
}

func mkfacts(path string) {
	out, err := exec.Command("go", "env", "GOROOT", "GOVERSION", "GOMODCACHE").Output()
	if err != nil {
		die("go env: %v", err)
	}
	f := strings.Fields(string(out))
	fa := Facts{GOROOT: f[0], GOVERSION: f[1], GOMODCACHE: f[2]}
	out, err = exec.Command("go", "list", "-f", `{{join context.ToolTags " "}}|{{join context.ReleaseTags " "}}`, "runtime").Output()
	if err != nil {
		die("go list: %v", err)
	}
	tt, rt, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	fa.ToolTags, fa.ReleaseTags = strings.Fields(tt), strings.Fields(rt)
	b, _ := json.MarshalIndent(fa, "", " ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		die("%v", err)
	}
}

type modInfo struct {
	Path, Version, Dir string
	GoVersion          string
	Main               bool
	goVersionKnown     bool // set at construction when the version needs no file read
	once               sync.Once
	modFile            string // where the go directive is read from (lazily)
}

// srcFile is one source file held in memory.
type srcFile struct {
	pkg  *Pkg
	name string
	data []byte
}

type planner struct {
	facts    Facts
	root     string // directory of the main module (or of go.work)
	mains    []*modInfo
	deps     []*modInfo          // build list, module cache
	vendor   string              // vendor directory, "" unless vendoring
	vendPkg  map[string]*modInfo // vendored package -> module (vendor/modules.txt)
	godebugs map[string]string   // godebug lines of go.mod / go.work
	mainGo   string              // go version of the main module (for default GODEBUG)
	ctxt     build.Context
	mu       sync.Mutex
	mem      map[string][]byte // path -> bytes, for go/build's OpenFile
	stdDir   map[string]bool   // GOROOT/src/<path> exists
	pkgs     map[string]*Pkg
	order    []*Pkg
	files    []*srcFile
	decl     string // godecl binary
	workdir  string
	jobs     int
	readers  int
	rounds   int
	declMs   float64
	declCPU  float64
	bytes    int64
	nfiles   int
}

// ---- go.mod / go.work / modules.txt

type modFile struct {
	module, goVersion string
	require           [][2]string
	replace           map[string][2]string // old path (any version) -> new path, version ("" = directory)
	use               []string
	godebug           map[string]string
}

func parseModFile(data []byte) *modFile {
	m := &modFile{replace: map[string][2]string{}, godebug: map[string]string{}}
	block := ""
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if block != "" {
			if f[0] == ")" {
				block = ""
				continue
			}
			f = append([]string{block}, f...)
		} else if len(f) >= 2 && f[1] == "(" {
			block = f[0]
			continue
		}
		for i := range f {
			if u, err := strconv.Unquote(f[i]); err == nil {
				f[i] = u
			}
		}
		switch f[0] {
		case "module":
			m.module = f[1]
		case "go":
			m.goVersion = f[1]
		case "require":
			if len(f) >= 3 {
				m.require = append(m.require, [2]string{f[1], f[2]})
			}
		case "replace":
			// old [v] => new [v]
			for i, x := range f {
				if x == "=>" && i+1 < len(f) {
					nv := ""
					if i+2 < len(f) {
						nv = f[i+2]
					}
					m.replace[f[1]] = [2]string{f[i+1], nv}
				}
			}
		case "use":
			m.use = append(m.use, f[1])
		case "godebug":
			if k, v, ok := strings.Cut(f[1], "="); ok {
				m.godebug[k] = v
			}
		}
	}
	return m
}

// escapePath is the module cache's spelling of a module path: an upper-case letter becomes ! + lower case.
func escapePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (pl *planner) loadModules(srcdir string) {
	// the workspace or module root: nearest go.work above srcdir, else nearest go.mod
	dir := srcdir
	var work, mod string
	for d := dir; ; d = filepath.Dir(d) {
		if mod == "" {
			if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
				mod = d
			}
		}
		if _, err := os.Stat(filepath.Join(d, "go.work")); err == nil {
			work = d
			break
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	if mod == "" {
		die("no go.mod above %s", srcdir)
	}
	pl.godebugs = map[string]string{}
	addMain := func(d string) *modFile {
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err != nil {
			die("%v", err)
		}
		mf := parseModFile(b)
		pl.mains = append(pl.mains, &modInfo{Path: mf.module, Dir: d, GoVersion: mf.goVersion, Main: true, goVersionKnown: true})
		return mf
	}
	var mf *modFile
	if work != "" {
		pl.root = work
		b, _ := os.ReadFile(filepath.Join(work, "go.work"))
		wf := parseModFile(b)
		for _, u := range wf.use {
			addMain(filepath.Join(work, u))
		}
		pl.mainGo = wf.goVersion
		for k, v := range wf.godebug {
			pl.godebugs[k] = v
		}
	} else {
		pl.root = mod
		mf = addMain(mod)
		pl.mainGo = mf.goVersion
		for k, v := range mf.godebug {
			pl.godebugs[k] = v
		}
	}
	if b, err := os.ReadFile(filepath.Join(pl.root, "vendor", "modules.txt")); err == nil {
		// vendoring: every package outside the main modules lives under vendor/
		pl.vendor = filepath.Join(pl.root, "vendor")
		pl.vendPkg = map[string]*modInfo{}
		var cur *modInfo
		for _, line := range strings.Split(string(b), "\n") {
			switch {
			case strings.HasPrefix(line, "# "):
				f := strings.Fields(line[2:])
				cur = &modInfo{Path: f[0], goVersionKnown: true}
				if len(f) >= 2 && f[1] != "=>" {
					cur.Version = f[1]
				}
			case strings.HasPrefix(line, "## "):
				if cur != nil {
					for _, part := range strings.Split(line[3:], ";") {
						if g, ok := strings.CutPrefix(strings.TrimSpace(part), "go "); ok {
							cur.GoVersion = g
						}
					}
				}
			case line != "" && cur != nil:
				pl.vendPkg[line] = cur
			}
		}
	} else if mf != nil {
		for _, r := range mf.require {
			path, ver := r[0], r[1]
			mi := &modInfo{Path: path, Version: ver}
			if rp, ok := mf.replace[path]; ok {
				if rp[1] == "" { // a directory
					mi.Dir = filepath.Join(mod, rp[0])
					mi.modFile = filepath.Join(mi.Dir, "go.mod")
				} else {
					mi.Dir = filepath.Join(pl.facts.GOMODCACHE, escapePath(rp[0])+"@"+rp[1])
					mi.modFile = filepath.Join(pl.facts.GOMODCACHE, "cache", "download", escapePath(rp[0]), "@v", rp[1]+".mod")
				}
			} else {
				mi.Dir = filepath.Join(pl.facts.GOMODCACHE, escapePath(path)+"@"+ver)
				mi.modFile = filepath.Join(pl.facts.GOMODCACHE, "cache", "download", escapePath(path), "@v", ver+".mod")
			}
			pl.deps = append(pl.deps, mi)
		}
	}
	byLen := func(m []*modInfo) {
		sort.Slice(m, func(i, j int) bool { return len(m[i].Path) > len(m[j].Path) })
	}
	byLen(pl.mains)
	byLen(pl.deps)
}

func (mi *modInfo) goVer() string {
	if mi.goVersionKnown {
		return mi.GoVersion
	}
	mi.once.Do(func() {
		if b, err := os.ReadFile(mi.modFile); err == nil {
			mi.GoVersion = parseModFile(b).goVersion
		}
	})
	return mi.GoVersion
}

func hasPrefixPath(p, mod string) bool {
	return p == mod || (strings.HasPrefix(p, mod) && p[len(mod)] == '/')
}

func (pl *planner) isStd(path string) bool {
	pl.mu.Lock()
	v, ok := pl.stdDir[path]
	pl.mu.Unlock()
	if ok {
		return v
	}
	fi, err := os.Stat(filepath.Join(pl.facts.GOROOT, "src", path))
	v = err == nil && fi.IsDir()
	pl.mu.Lock()
	pl.stdDir[path] = v
	pl.mu.Unlock()
	return v
}

// resolve an import seen in package from: the package's real import path, its directory, its module.
func (pl *planner) resolve(path string, from *Pkg) (string, string, *modInfo, bool) {
	first, _, _ := strings.Cut(path, "/")
	if from.Standard && strings.Contains(first, ".") {
		// the standard library's own vendored dependencies
		if v := "vendor/" + path; pl.isStd(v) {
			return v, filepath.Join(pl.facts.GOROOT, "src", v), nil, true
		}
	}
	if !strings.Contains(first, ".") && pl.isStd(path) {
		return path, filepath.Join(pl.facts.GOROOT, "src", path), nil, true
	}
	for _, m := range pl.mains {
		if hasPrefixPath(path, m.Path) {
			return path, filepath.Join(m.Dir, strings.TrimPrefix(path, m.Path)), m, false
		}
	}
	if pl.vendor != "" {
		return path, filepath.Join(pl.vendor, path), pl.vendPkg[path], false
	}
	for _, m := range pl.deps {
		if hasPrefixPath(path, m.Path) {
			d := filepath.Join(m.Dir, strings.TrimPrefix(path, m.Path))
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				return path, d, m, false
			}
		}
	}
	die("%s imports %s: no module provides it", from.ImportPath, path)
	return "", "", nil, false
}

// ---- reading and selecting files

type memFile struct{ *bytes.Reader }

func (memFile) Close() error { return nil }

func (pl *planner) openFile(path string) (io.ReadCloser, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pl.mu.Lock()
	pl.mem[path] = b
	pl.mu.Unlock()
	return memFile{bytes.NewReader(b)}, nil
}

// loadDir reads a package directory: the Go and assembly files that match the build.
func (pl *planner) loadDir(p *Pkg) (gofiles []*srcFile, err error) {
	ents, err := os.ReadDir(p.Dir)
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || name[0] == '_' || name[0] == '.' {
			continue
		}
		isGo := strings.HasSuffix(name, ".go")
		if !isGo && !strings.HasSuffix(name, ".s") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		full := filepath.Join(p.Dir, name)
		if !e.Type().IsRegular() {
			if fi, err := os.Stat(full); err != nil || !fi.Mode().IsRegular() {
				continue
			}
		}
		// MatchFile checks the name first (x_windows.go never gets opened) and then reads the head of the
		// file through openFile, which reads the whole file once and keeps the bytes for us.
		ok, err := pl.ctxt.MatchFile(p.Dir, name)
		pl.mu.Lock()
		b, read := pl.mem[full]
		delete(pl.mem, full)
		pl.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("%s: %v", full, err)
		}
		if !ok || !read {
			continue
		}
		if isGo {
			gofiles = append(gofiles, &srcFile{pkg: p, name: name, data: b})
		} else {
			p.SFiles = append(p.SFiles, name)
		}
	}
	return gofiles, nil
}

var embedLine = regexp.MustCompile(`(?m)^[ \t]*//go:embed[ \t]+(.*)$`)
var debugLine = regexp.MustCompile(`(?m)^//go:debug[ \t]+(\S+)=(\S+)[ \t]*$`)

func embedPatterns(data []byte) (out []string) {
	for _, m := range embedLine.FindAllSubmatch(data, -1) {
		s := strings.TrimSpace(string(m[1]))
		for s != "" {
			var tok string
			switch s[0] {
			case '"':
				end := 1
				for end < len(s) && s[end] != '"' {
					if s[end] == '\\' {
						end++
					}
					end++
				}
				if end >= len(s) {
					return
				}
				tok, _ = strconv.Unquote(s[:end+1])
				s = s[end+1:]
			case '`':
				end := strings.IndexByte(s[1:], '`')
				if end < 0 {
					return
				}
				tok = s[1 : 1+end]
				s = s[end+2:]
			default:
				end := strings.IndexAny(s, " \t")
				if end < 0 {
					end = len(s)
				}
				tok = s[:end]
				s = s[end:]
			}
			out = append(out, tok)
			s = strings.TrimLeft(s, " \t")
		}
	}
	return
}

// ---- the declaration pass

// declare runs godecl over the concatenation of files and gives back, per file, the package name and the
// import paths. godecl writes every declaration's byte offset (little-endian dwords) and then every
// declaration's kind (bytes); kind 6 is an import path (the offset of its string literal) and kind 7 the
// name in a package clause.
func (pl *planner) declare(files []*srcFile) (names []string, imports [][]string) {
	names = make([]string, len(files))
	imports = make([][]string, len(files))
	if len(files) == 0 {
		return
	}
	starts := make([]int, len(files)+1)
	n := 0
	for i, f := range files {
		starts[i] = n
		n += len(f.data)
		if len(f.data) == 0 || f.data[len(f.data)-1] != '\n' {
			n++
		}
	}
	starts[len(files)] = n
	t := ms()
	tA := t
	// The concatenation exists only as a tmpfs file that godecl maps: one copy of each byte, no more.
	cpath := filepath.Join(pl.workdir, fmt.Sprintf("round%d.go", pl.rounds))
	cf, err := os.Create(cpath)
	if err != nil {
		die("%v", err)
	}
	w := bufio.NewWriterSize(cf, 1<<20)
	for _, f := range files {
		w.Write(f.data)
		if len(f.data) == 0 || f.data[len(f.data)-1] != '\n' {
			w.WriteByte('\n')
		}
	}
	if err := w.Flush(); err != nil {
		die("%v", err)
	}
	cf.Close()
	tB := ms()
	in, _ := os.Open(cpath)
	cmd := exec.Command(pl.decl)
	var out bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, &out, os.Stderr
	if err := cmd.Run(); err != nil {
		die("godecl: %v", err)
	}
	in.Close()
	os.Remove(cpath)
	if os.Getenv("SB_PLAN_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "   decl: %d bytes  write %5.1f ms  run %5.1f ms (cpu %v)\n", n, tB-tA, ms()-tB, cmd.ProcessState.UserTime()+cmd.ProcessState.SystemTime())
	}
	pl.declMs += ms() - t
	pl.declCPU += float64(cmd.ProcessState.UserTime().Microseconds()+cmd.ProcessState.SystemTime().Microseconds()) / 1000
	o := out.Bytes()
	nd := len(o) / 5
	kinds := o[4*nd:]
	fi := 0
	for i := 0; i < nd; i++ {
		k := kinds[i]
		if k != 6 && k != 7 {
			continue
		}
		off := int(binary.LittleEndian.Uint32(o[4*i:]))
		for off >= starts[fi+1] {
			fi++
		}
		src := files[fi].data
		off -= starts[fi]
		if k == 7 {
			end := off
			for end < len(src) && (src[end] == '_' || src[end] >= 0x80 || src[end] >= '0' && src[end] <= '9' ||
				src[end]|0x20 >= 'a' && src[end]|0x20 <= 'z') {
				end++
			}
			if names[fi] == "" {
				names[fi] = string(src[off:end])
			}
			continue
		}
		q := src[off]
		end := off + 1
		for end < len(src) && src[end] != q {
			if q == '"' && src[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(src) {
			die("%s: unterminated import path", files[fi].name)
		}
		lit := string(src[off : end+1])
		if u, err := strconv.Unquote(lit); err == nil {
			imports[fi] = append(imports[fi], u)
		} else {
			die("%s: import path %s", files[fi].name, lit)
		}
	}
	pl.bytes += int64(n)
	return
}

// ---- the loop

func (pl *planner) run(mainDir string) []*Pkg {
	mainDir, _ = filepath.Abs(mainDir)
	var mainMod *modInfo
	for _, m := range pl.mains {
		if mainDir == m.Dir || strings.HasPrefix(mainDir, m.Dir+string(filepath.Separator)) {
			mainMod = m
			break
		}
	}
	if mainMod == nil {
		die("%s is not inside a main module", mainDir)
	}
	rel, _ := filepath.Rel(mainMod.Dir, mainDir)
	mainPath := mainMod.Path
	if rel != "." {
		mainPath += "/" + filepath.ToSlash(rel)
	}
	root := &Pkg{ImportPath: mainPath, Dir: mainDir, mod: mainMod}
	pl.pkgs[mainPath] = root
	frontier := []*Pkg{root}
	for len(frontier) > 0 {
		pl.rounds++
		tRound := ms()
		// read the frontier's directories, in parallel
		lists := make([][]*srcFile, len(frontier))
		errs := make([]error, len(frontier))
		var wg sync.WaitGroup
		next := make(chan int, len(frontier))
		for i := range frontier {
			next <- i
		}
		close(next)
		for w := 0; w < pl.readers; w++ { // cold, these wait on the disk, not on a core: more readers than cores
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range next {
					lists[i], errs[i] = pl.loadDir(frontier[i])
				}
			}()
		}
		wg.Wait()
		var files []*srcFile
		for i, l := range lists {
			if errs[i] != nil {
				die("%s: %v", frontier[i].ImportPath, errs[i])
			}
			files = append(files, l...)
		}
		tRead := ms()
		names, imps := pl.declare(files)
		tDecl := ms()
		// assemble the packages
		seen := map[*Pkg]map[string]bool{}
		for i, f := range files {
			p := f.pkg
			cgo := false
			for _, im := range imps[i] {
				if im == "C" {
					cgo = true
				}
			}
			if cgo || names[i] == "documentation" {
				continue // cgo is off: a file that imports "C" is not part of the build
			}
			if p.Name == "" {
				p.Name = names[i]
			} else if p.Name != names[i] {
				die("%s: packages %s and %s in one directory", p.Dir, p.Name, names[i])
			}
			p.GoFiles = append(p.GoFiles, f.name)
			p.goBytes += int64(len(f.data))
			pl.nfiles++
			if seen[p] == nil {
				seen[p] = map[string]bool{}
			}
			embed := false
			for _, im := range imps[i] {
				if !seen[p][im] {
					seen[p][im] = true
					p.Imports = append(p.Imports, im)
				}
				if im == "embed" {
					embed = true
				}
			}
			if embed {
				p.EmbedPatterns = append(p.EmbedPatterns, embedPatterns(f.data)...)
			}
			if p == root {
				for _, m := range debugLine.FindAllSubmatch(f.data, -1) {
					pl.godebugs[string(m[1])] = string(m[2])
				}
			}
			pl.files = append(pl.files, f)
		}
		// resolve every import of the frontier to a directory (stat calls and go.mod reads: in parallel)
		type res struct {
			path, dir string
			mod       *modInfo
			std       bool
		}
		resolved := make([][]res, len(frontier))
		next = make(chan int, len(frontier))
		for i := range frontier {
			next <- i
		}
		close(next)
		for w := 0; w < pl.jobs; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range next {
					p := frontier[i]
					if len(p.GoFiles) == 0 {
						die("%s: no Go files in %s", p.ImportPath, p.Dir)
					}
					sort.Strings(p.Imports)
					sort.Strings(p.EmbedPatterns)
					p.EmbedPatterns = uniq(p.EmbedPatterns)
					if p.mod != nil {
						p.Module = &Module{Path: p.mod.Path, Version: p.mod.Version, GoVersion: p.mod.goVer(), Main: p.mod.Main}
					}
					for _, im := range p.Imports {
						if im == "unsafe" {
							resolved[i] = append(resolved[i], res{})
							continue
						}
						rp, dir, mod, std := pl.resolve(im, p)
						resolved[i] = append(resolved[i], res{rp, dir, mod, std})
					}
				}
			}()
		}
		wg.Wait()
		var nf []*Pkg
		for i, p := range frontier {
			for j, im := range p.Imports {
				r := resolved[i][j]
				if r.path == "" {
					continue
				}
				if r.path != im {
					if p.ImportMap == nil {
						p.ImportMap = map[string]string{}
					}
					p.ImportMap[im] = r.path
				}
				if pl.pkgs[r.path] == nil {
					q := &Pkg{ImportPath: r.path, Dir: r.dir, mod: r.mod, Standard: r.std}
					pl.pkgs[r.path] = q
					nf = append(nf, q)
				}
			}
		}
		if os.Getenv("SB_PLAN_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "round %2d: %4d packages %5d files  read %6.1f ms  decl %6.1f ms  resolve %6.1f ms\n",
				pl.rounds, len(frontier), len(files), tRead-tRound, tDecl-tRead, ms()-tDecl)
		}
		frontier = nf
	}
	// dependencies before dependents, the main package last
	state := map[*Pkg]int{}
	var visit func(p *Pkg)
	visit = func(p *Pkg) {
		if state[p] != 0 {
			return
		}
		state[p] = 1
		for _, im := range p.Imports {
			if im == "unsafe" {
				continue
			}
			if m, ok := p.ImportMap[im]; ok {
				im = m
			}
			visit(pl.pkgs[im])
		}
		pl.order = append(pl.order, p)
	}
	visit(root)
	if root.Name == "main" {
		root.DefaultGODEBUG = pl.defaultGODEBUG()
	}
	return pl.order
}

func uniq(s []string) []string {
	out := s[:0]
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}

var godebugRow = regexp.MustCompile(`\{Name: "(\w+)"[^}]*?Changed: (\d+), Old: "([^"]*)"`)

// defaultGODEBUG: the settings a main package gets because its module's go version is older than the
// toolchain (cmd/go/internal/load/godebug.go), from the table in GOROOT/src/internal/godebugs.
func (pl *planner) defaultGODEBUG() string {
	m := map[string]string{}
	for k, v := range pl.godebugs {
		m[k] = v
	}
	v := pl.mainGo
	if d, ok := m["default"]; ok {
		delete(m, "default")
		v = strings.TrimPrefix(d, "go")
	}
	parts := strings.Split(v, ".")
	minor := 0
	if len(parts) >= 2 {
		minor, _ = strconv.Atoi(parts[1])
	}
	tab, err := os.ReadFile(filepath.Join(pl.facts.GOROOT, "src", "internal", "godebugs", "table.go"))
	if err != nil {
		die("%v", err)
	}
	def := map[string]string{}
	for _, r := range godebugRow.FindAllSubmatch(tab, -1) {
		if ch, _ := strconv.Atoi(string(r[2])); minor < ch {
			def[string(r[1])] = string(r[3])
		}
	}
	for k, v := range m {
		def[k] = v
	}
	keys := make([]string, 0, len(def))
	for k := range def {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k + "=" + def[k])
	}
	return b.String()
}

func newPlanner(factsPath, decl, workdir string, jobs int) *planner {
	pl := &planner{mem: map[string][]byte{}, stdDir: map[string]bool{}, pkgs: map[string]*Pkg{}, decl: decl, workdir: workdir, jobs: jobs, readers: 4 * jobs}
	b, err := os.ReadFile(factsPath)
	if err != nil {
		die("%v (run sb -mkfacts %s)", err, factsPath)
	}
	if err := json.Unmarshal(b, &pl.facts); err != nil {
		die("%s: %v", factsPath, err)
	}
	if r, err := strconv.Atoi(os.Getenv("SB_READERS")); err == nil && r > 0 {
		pl.readers = r
	}
	pl.ctxt = build.Default
	pl.ctxt.GOOS, pl.ctxt.GOARCH = "linux", "amd64"
	pl.ctxt.GOROOT = pl.facts.GOROOT
	pl.ctxt.CgoEnabled = false
	pl.ctxt.Compiler = "gc"
	pl.ctxt.BuildTags = nil
	pl.ctxt.ToolTags = pl.facts.ToolTags
	pl.ctxt.ReleaseTags = pl.facts.ReleaseTags
	pl.ctxt.OpenFile = pl.openFile
	return pl
}
