// selfplan.go: the package graph without `go list` and without a separate planning step. Starting from the
// main package's directory, fc finds each package's directory (go.mod, go.work, vendor/modules.txt, the
// module cache, GOROOT), keeps the files whose name and build constraints match the target, parses them
// from the bytes it read to decide that, and follows their imports. (The rules are driver/plan.go's, which
// the frozen stock path uses; there the files are read by a planning process and again by each compiler.)
package main

import (
	"bytes"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

type modInfo struct {
	Path, Version, Dir string
	GoVersion          string
	goVersionKnown     bool
	once               sync.Once
	modFile            string // where the go directive is read from (lazily)
}

type selfPlan struct {
	goroot, modcache string
	root             string
	mains, deps      []*modInfo
	vendor           string
	vendPkg          map[string]*modInfo
	ctxt             build.Context
	mu               sync.Mutex
	mem              map[string][]byte // path -> bytes read by go/build's MatchFile
	stdDir           map[string]bool
}

type modFile struct {
	module, goVersion string
	require           [][2]string
	replace           map[string][2]string // old path -> new path, version ("" = directory)
	use               []string
}

func parseModFile(data []byte) *modFile {
	m := &modFile{replace: map[string][2]string{}}
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

func newSelfPlan(srcdir string) (*selfPlan, error) {
	sp := &selfPlan{mem: map[string][]byte{}, stdDir: map[string]bool{}}
	sp.goroot, sp.modcache = os.Getenv("FC_GOROOT"), os.Getenv("GOMODCACHE")
	// Without asking the go command (a process start, and 10-20 ms): GOROOT from the environment or from
	// where the go on PATH lives, the module cache from GOPATH or the home directory. Anything set in the go
	// command's own env file is not seen this way, so the go command is asked when a guess does not exist.
	if sp.goroot == "" {
		sp.goroot = os.Getenv("GOROOT")
	}
	if sp.goroot == "" {
		if g, err := exec.LookPath("go"); err == nil {
			if g, err = filepath.EvalSymlinks(g); err == nil {
				sp.goroot = filepath.Dir(filepath.Dir(g))
			}
		}
	}
	if sp.modcache == "" {
		if gp := filepath.SplitList(os.Getenv("GOPATH")); len(gp) > 0 && gp[0] != "" {
			sp.modcache = filepath.Join(gp[0], "pkg", "mod")
		} else if home, err := os.UserHomeDir(); err == nil {
			sp.modcache = filepath.Join(home, "go", "pkg", "mod")
		}
	}
	if _, err := os.Stat(filepath.Join(sp.goroot, "src", "runtime", "proc.go")); err != nil {
		sp.goroot = ""
	}
	if _, err := os.Stat(filepath.Join(sp.modcache, "cache")); err != nil {
		sp.modcache = ""
	}
	if sp.goroot == "" || sp.modcache == "" {
		out, err := exec.Command("go", "env", "GOROOT", "GOMODCACHE").Output()
		if err != nil {
			return nil, fmt.Errorf("go env: %v", err)
		}
		f := strings.Fields(string(out))
		sp.goroot, sp.modcache = f[0], f[1]
	}
	sp.ctxt = build.Default
	sp.ctxt.GOOS, sp.ctxt.GOARCH = "linux", "amd64"
	sp.ctxt.GOROOT = sp.goroot
	sp.ctxt.CgoEnabled = false
	sp.ctxt.Compiler = "gc"
	sp.ctxt.BuildTags = nil
	sp.ctxt.OpenFile = sp.openFile
	// the workspace or module root: nearest go.work above srcdir, else nearest go.mod
	var work, mod string
	for d := srcdir; ; d = filepath.Dir(d) {
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
		return nil, fmt.Errorf("no go.mod above %s", srcdir)
	}
	addMain := func(d string) (*modFile, error) {
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err != nil {
			return nil, err
		}
		mf := parseModFile(b)
		sp.mains = append(sp.mains, &modInfo{Path: mf.module, Dir: d, GoVersion: mf.goVersion, goVersionKnown: true})
		return mf, nil
	}
	var mf *modFile
	if work != "" {
		// a workspace: the build list is the highest version any of its modules requires
		sp.root = work
		b, _ := os.ReadFile(filepath.Join(work, "go.work"))
		mf = &modFile{replace: map[string][2]string{}}
		at := map[string]int{}
		for _, u := range parseModFile(b).use {
			m, err := addMain(filepath.Join(work, u))
			if err != nil {
				return nil, err
			}
			for _, r := range m.require {
				if i, ok := at[r[0]]; !ok {
					at[r[0]] = len(mf.require)
					mf.require = append(mf.require, r)
				} else if semverLess(mf.require[i][1], r[1]) {
					mf.require[i] = r
				}
			}
			for k, v := range m.replace {
				if v[1] == "" && !filepath.IsAbs(v[0]) {
					v[0] = filepath.Join(work, u, v[0]) // (joined with mod below; absolute paths win)
				}
				mf.replace[k] = v
			}
		}
	} else {
		sp.root = mod
		var err error
		if mf, err = addMain(mod); err != nil {
			return nil, err
		}
	}
	if b, err := os.ReadFile(filepath.Join(sp.root, "vendor", "modules.txt")); err == nil {
		// vendoring: every package outside the main modules lives under vendor/
		sp.vendor = filepath.Join(sp.root, "vendor")
		sp.vendPkg = map[string]*modInfo{}
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
				sp.vendPkg[line] = cur
			}
		}
	} else if mf != nil {
		for _, r := range mf.require {
			path, ver := r[0], r[1]
			mi := &modInfo{Path: path, Version: ver}
			if rp, ok := mf.replace[path]; ok {
				if rp[1] == "" { // a directory
					mi.Dir = filepath.Join(mod, rp[0])
					if filepath.IsAbs(rp[0]) {
						mi.Dir = rp[0]
					}
					mi.modFile = filepath.Join(mi.Dir, "go.mod")
				} else {
					mi.Dir = filepath.Join(sp.modcache, escapePath(rp[0])+"@"+rp[1])
					mi.modFile = filepath.Join(sp.modcache, "cache", "download", escapePath(rp[0]), "@v", rp[1]+".mod")
				}
			} else {
				mi.Dir = filepath.Join(sp.modcache, escapePath(path)+"@"+ver)
				mi.modFile = filepath.Join(sp.modcache, "cache", "download", escapePath(path), "@v", ver+".mod")
			}
			sp.deps = append(sp.deps, mi)
		}
	}
	byLen := func(m []*modInfo) {
		sort.Slice(m, func(i, j int) bool { return len(m[i].Path) > len(m[j].Path) })
	}
	byLen(sp.mains)
	byLen(sp.deps)
	return sp, nil
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

func (sp *selfPlan) isStd(path string) bool {
	sp.mu.Lock()
	v, ok := sp.stdDir[path]
	sp.mu.Unlock()
	if ok {
		return v
	}
	fi, err := os.Stat(filepath.Join(sp.goroot, "src", path))
	v = err == nil && fi.IsDir()
	sp.mu.Lock()
	sp.stdDir[path] = v
	sp.mu.Unlock()
	return v
}

// resolve an import seen in a package: the package's real import path, its directory, its module.
func (sp *selfPlan) resolve(path string, fromStd bool) (real, dir string, mi *modInfo, std bool, err error) {
	first, _, _ := strings.Cut(path, "/")
	if fromStd && strings.Contains(first, ".") {
		// the standard library's own vendored dependencies
		if v := "vendor/" + path; sp.isStd(v) {
			return v, filepath.Join(sp.goroot, "src", v), nil, true, nil
		}
	}
	if !strings.Contains(first, ".") && sp.isStd(path) {
		return path, filepath.Join(sp.goroot, "src", path), nil, true, nil
	}
	for _, m := range sp.mains {
		if hasPrefixPath(path, m.Path) {
			return path, filepath.Join(m.Dir, strings.TrimPrefix(path, m.Path)), m, false, nil
		}
	}
	if sp.vendor != "" {
		return path, filepath.Join(sp.vendor, path), sp.vendPkg[path], false, nil
	}
	for _, m := range sp.deps {
		if hasPrefixPath(path, m.Path) {
			d := filepath.Join(m.Dir, strings.TrimPrefix(path, m.Path))
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				return path, d, m, false, nil
			}
		}
	}
	return "", "", nil, false, fmt.Errorf("import %s: no module provides it", path)
}

type memFile struct{ *bytes.Reader }

func (memFile) Close() error { return nil }

func (sp *selfPlan) openFile(path string) (io.ReadCloser, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sp.mu.Lock()
	sp.mem[path] = b
	sp.mu.Unlock()
	return memFile{bytes.NewReader(b)}, nil
}

// loadDir reads a package directory: the Go and assembly files that match the build. The bytes of the Go
// files stay in p.src for the parser.
func (sp *selfPlan) loadDir(p *Pkg) error {
	ents, err := os.ReadDir(p.Dir)
	if err != nil {
		return err
	}
	p.src = map[string][]byte{}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || name[0] == '_' || name[0] == '.' {
			continue
		}
		isGo := strings.HasSuffix(name, ".go")
		if !isGo && !strings.HasSuffix(name, ".s") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		full := filepath.Join(p.Dir, name)
		if !e.Type().IsRegular() {
			if fi, err := os.Stat(full); err != nil || !fi.Mode().IsRegular() {
				continue
			}
		}
		// MatchFile checks the name first (x_windows.go is never opened), then reads the head of the file
		// through openFile, which reads the whole file once and keeps the bytes.
		ok, err := sp.ctxt.MatchFile(p.Dir, name)
		sp.mu.Lock()
		b, read := sp.mem[full]
		delete(sp.mem, full)
		sp.mu.Unlock()
		if err != nil {
			return fmt.Errorf("%s: %v", full, err)
		}
		if !ok || !read {
			continue
		}
		if isGo {
			p.GoFiles = append(p.GoFiles, name)
			p.src[name] = b
		} else {
			p.SFiles = append(p.SFiles, name)
		}
	}
	return nil
}

// pkgFor finds or creates the package an import in from refers to.
func (pr *Program) pkgFor(ip string, from *Pkg) (*Pkg, error) {
	real, dir, mi, std, err := pr.plan.resolve(ip, from.Standard)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", from.ImportPath, err)
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if real != ip {
		if from.ImportMap == nil {
			from.ImportMap = map[string]string{}
		}
		from.ImportMap[ip] = real
	}
	p := pr.byPath[real]
	if p == nil {
		p = &Pkg{ImportPath: real, Dir: dir, Standard: std}
		if mi != nil {
			p.Module = &struct{ Path, Version, GoVersion string }{mi.Path, mi.Version, mi.goVer()}
		}
		pr.byPath[real] = p
	}
	return p, nil
}

// discover parses p and everything it imports, creating the packages as their imports are met.
func (pr *Program) discover(p *Pkg) {
	p.discOnce.Do(func() {
		pr.parse(p)
		if p.err != nil {
			return
		}
		var wg sync.WaitGroup
		for _, ip := range p.imps {
			d, err := pr.pkgFor(ip, p)
			if err != nil {
				p.err = err
				return
			}
			p.Imports = append(p.Imports, ip)
			wg.Add(1)
			go func() { defer wg.Done(); pr.discover(d) }()
		}
		wg.Wait()
	})
}

// selfMain starts a self plan at the main package in dir (pattern: a directory relative to dir, "." or
// "./cmd/x") and discovers the whole program, the runtime included.
func (pr *Program) selfMain(dir, pattern string) (*Pkg, error) {
	abs, err := filepath.Abs(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	sp, err := newSelfPlan(abs)
	if err != nil {
		return nil, err
	}
	pr.plan = sp
	var mainPkg *Pkg
	for _, m := range sp.mains {
		if abs == m.Dir || strings.HasPrefix(abs, m.Dir+string(filepath.Separator)) {
			path := m.Path
			if rel, _ := filepath.Rel(m.Dir, abs); rel != "." {
				path += "/" + filepath.ToSlash(rel)
			}
			mainPkg = &Pkg{ImportPath: path, Dir: abs}
			mainPkg.Module = &struct{ Path, Version, GoVersion string }{m.Path, "", m.GoVersion}
		}
	}
	if mainPkg == nil {
		return nil, fmt.Errorf("%s is in no main module", abs)
	}
	pr.byPath[mainPkg.ImportPath] = mainPkg
	rt := &Pkg{ImportPath: "runtime", Dir: filepath.Join(sp.goroot, "src", "runtime"), Standard: true}
	pr.byPath["runtime"] = rt
	var wg sync.WaitGroup
	for _, p := range []*Pkg{mainPkg, rt} {
		wg.Add(1)
		go func() { defer wg.Done(); pr.discover(p) }()
	}
	wg.Wait()
	for _, p := range pr.byPath {
		if p.err != nil {
			return nil, p.err
		}
	}
	return mainPkg, nil
}

// semverLess: a < b for module versions (vMAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]).
func semverLess(a, b string) bool {
	parse := func(v string) (nums [3]int, pre string) {
		v = strings.TrimPrefix(v, "v")
		if i := strings.IndexByte(v, '+'); i >= 0 {
			v = v[:i]
		}
		if i := strings.IndexByte(v, '-'); i >= 0 {
			v, pre = v[:i], v[i+1:]
		}
		for i, f := range strings.SplitN(v, ".", 3) {
			nums[i], _ = strconv.Atoi(f)
		}
		return
	}
	an, ap := parse(a)
	bn, bp := parse(b)
	for i := range an {
		if an[i] != bn[i] {
			return an[i] < bn[i]
		}
	}
	if ap == "" || bp == "" {
		return ap != "" && bp == "" // a prerelease precedes its release
	}
	af, bf := strings.Split(ap, "."), strings.Split(bp, ".")
	for i := 0; i < len(af) && i < len(bf); i++ {
		if af[i] == bf[i] {
			continue
		}
		x, ex := strconv.Atoi(af[i])
		y, ey := strconv.Atoi(bf[i])
		switch {
		case ex == nil && ey == nil:
			return x < y
		case ex == nil:
			return true // numeric identifiers precede alphanumeric ones
		case ey == nil:
			return false
		}
		return af[i] < bf[i]
	}
	return len(af) < len(bf)
}
