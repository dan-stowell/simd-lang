// querygo.go: rules_go's rules from their attributes (rules_go 0.63: go/private/context.bzl new_go_info,
// _merge_embed, _dedup_archives, _infer_importpath; actions/archive.bzl, compilepkg.bzl, link.bzl;
// rules/binary.bzl, library.bzl, source.bzl, transition.bzl).
package main

import (
	"path"
	"sort"
	"strings"
)

type omap struct {
	keys []string
	m    map[string]string
}

func (o *omap) set(k, v string) {
	if o.m == nil {
		o.m = map[string]string{}
	}
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *omap) update(p *omap) {
	for _, k := range p.keys {
		o.set(k, p.m[k])
	}
}

const (
	inferredPath = iota
	explicitPath
	exportPath
)

// goInfo: rules_go's GoInfo (the source of a package before it is compiled).
type goInfo struct {
	label                 string
	c                     *qcfg
	name                  string
	importpath, importmap string
	aliases               []string
	pathtype              int
	isMain                bool
	srcs, embedsrcs       []string
	deps                  []*goArc
	xdefs                 omap
	cgo                   bool
}

// goArc: GoArchive.
type goArc struct {
	info   *goInfo
	a, x   string
	xdefs  omap // the archive's and its deps' x_defs
	action *action
}

var (
	infoMemo = map[memoKey]*goInfo{}
	arcMemo  = map[memoKey]*goArc{}
	binMemo  = map[memoKey]*action{}
)

func rulesGoLabel(rel string) string { return "@@" + rulesGo + "//" + rel }

// pure: rules_go's mode.pure in c (the pure flag, or a platform with cgo_off; a C toolchain is assumed).
func pure(c *qcfg) bool {
	return c.flag(rulesGoLabel("go/config:pure")) == "true" || c.platformHas(rulesGoLabel("go/toolchain:cgo_off"))
}

func goTags(c *qcfg) []string {
	var tags []string
	if t := c.flag(rulesGoLabel("go/config:tags")); t != "" {
		tags = append(tags, strings.Split(t, ",")...)
	}
	if t := rcDefines["gotags"]; t != "" && !c.exec {
		tags = append(tags, strings.Split(t, ",")...)
	}
	if c.flag(rulesGoLabel("go/config:race")) == "true" {
		tags = append(tags, "race")
	}
	if c.flag(rulesGoLabel("go/config:msan")) == "true" {
		tags = append(tags, "msan")
	}
	return tags
}

// goEnv: go.env (context.bzl), the C toolchain's PATH and ZERO_AR_DATE as the aquery shows them.
func goEnv(c *qcfg) [][2]string {
	cgo := "1"
	if pure(c) {
		cgo = "0"
	}
	env := [][2]string{{"GOARCH", "amd64"}, {"GOOS", "linux"}, {"GOEXPERIMENT", ""},
		{"GOROOT", path.Join(c.bin(), "external", rulesGo, "stdlib_")}, {"GOROOT_FINAL", "GOROOT"}, {"CGO_ENABLED", cgo},
		{"GOPATH", ""}, {"GOTOOLCHAIN", "local"}, {"GODEBUG", "winsymlink=0"}}
	if cgo == "1" {
		env = append(env, [2]string{"ZERO_AR_DATE", "1"}, [2]string{"PATH", "/usr/bin:/bin"}) // (the C toolchain's)
	}
	return env
}

func isGoRule(class string) bool {
	switch class {
	case "go_library", "go_tool_library", "go_source", "go_proto_library", "go_binary", "_go_proto_compiler":
		return true
	}
	return false
}

// follow: a label through aliases (select()s in actual resolved in c).
func follow(l string, c *qcfg, from string) *qRule {
	r := rule(l, from)
	for r.Class == "alias" {
		r = rule(c.resolve(r, "actual").Str, r.Name)
	}
	return r
}

// goContext: the import path, import map and path type go_context gives a rule (_infer_importpath).
func goContext(r *qRule, c *qcfg) (string, string, int) {
	ip := c.resolve(r, "importpath").Str
	im := c.resolve(r, "importmap").Str
	eip, eim := "", ""
	for _, e := range c.resolve(r, "embed").List {
		ei := goInfoOf(e, c, r.Name)
		if ei.pathtype == explicitPath {
			eip, eim = ei.importpath, ei.importmap
			break
		}
	}
	if ip == "" {
		ip = eip
	}
	if im == "" {
		im = eim
	}
	if im == "" {
		im = ip
	}
	if ip != "" {
		return ip, im, explicitPath
	}
	_, pkg, name := splitLabel(r.Name)
	ip = pkg
	if !strings.HasSuffix(ip, name) {
		ip += "/" + name
	}
	if i := strings.LastIndex(ip, "/vendor/"); i >= 0 {
		ip = ip[i+len("/vendor/"):]
	}
	ip = strings.TrimPrefix(ip, "/")
	return ip, ip, inferredPath
}

// goInfoOf: the GoInfo of a label in c.
func goInfoOf(l string, c *qcfg, from string) *goInfo {
	r := follow(l, c, from)
	k := memoKey{r.Name, c}
	if i := infoMemo[k]; i != nil {
		return i
	}
	var info *goInfo
	switch r.Class {
	case "go_library", "go_tool_library", "go_source", "_go_proto_compiler":
		info = newGoInfo(r, c, false, true, nil, nil)
	case "go_proto_library":
		info = goProtoLibrary(r, c)
	default:
		qfail("%s: %s is a %s, not a Go library", from, r.Name, r.Class)
	}
	qKinds[r.Class]++
	infoMemo[k] = info
	return info
}

// newGoInfo: new_go_info. deps nil: the deps attribute's archives. merge: GoInfos merged after the embeds
// (go_proto_library's compilers, its resolver).
func newGoInfo(r *qRule, c *qcfg, isMain, importable bool, generated []string, merge []*goInfo) *goInfo {
	ip, im, pt := goContext(r, c)
	if !importable && pt == explicitPath {
		pt = exportPath
	}
	info := &goInfo{label: r.Name, c: c, name: labelName(r.Name), importpath: ip, importmap: im, pathtype: pt, isMain: isMain,
		aliases: c.resolve(r, "importpath_aliases").List}
	for _, s := range c.resolve(r, "srcs").List {
		info.srcs = append(info.srcs, qfiles(s, c.base, r.Name)...)
	}
	info.srcs = append(info.srcs, generated...)
	for _, s := range c.resolve(r, "embedsrcs").List {
		info.embedsrcs = append(info.embedsrcs, qfiles(s, c.base, r.Name)...)
	}
	for _, d := range c.resolve(r, "deps").List {
		info.deps = append(info.deps, archiveOf(d, c, r.Name))
	}
	if a := r.Attrs["cgo"]; a != nil {
		info.cgo = c.resolve(r, "cgo").Int != 0 || c.resolve(r, "cgo").Str == "true"
	}
	for _, e := range c.resolve(r, "embed").List {
		mergeEmbed(info, goInfoOf(e, c, r.Name), r.Name)
	}
	// _dedup_archives
	seen := map[string]bool{}
	var deps []*goArc
	for _, d := range info.deps {
		if seen[d.info.importpath] {
			continue
		}
		seen[d.info.importpath] = true
		deps = append(deps, d)
	}
	info.deps = deps
	for _, kv := range c.resolve(r, "x_defs").Dict {
		k, v := kv[0], kv[1]
		if strings.Contains(v, "$") {
			v = expandXdef(r, c, v)
		}
		if !strings.Contains(k, ".") {
			k = im + "." + k
		}
		info.xdefs.set(k, v)
	}
	for _, m := range merge {
		mergeEmbed(info, m, r.Name)
	}
	return info
}

// mergeEmbed: _merge_embed.
func mergeEmbed(info, s *goInfo, from string) {
	info.srcs = append(append([]string{}, s.srcs...), info.srcs...)
	info.embedsrcs = append(info.embedsrcs, s.embedsrcs...)
	info.deps = append(info.deps, s.deps...)
	info.xdefs.update(&s.xdefs)
	if s.cgo {
		if info.cgo {
			qfail("%s: multiple libraries with cgo enabled", from)
		}
		info.cgo = true
	}
}

// archiveOf: the GoArchive of a dependency label in c (its compile action derived).
func archiveOf(l string, c *qcfg, from string) *goArc {
	r := follow(l, c, from)
	k := memoKey{r.Name, c}
	if a := arcMemo[k]; a != nil {
		return a
	}
	switch r.Class {
	case "go_library", "go_tool_library", "go_proto_library":
	default:
		qfail("%s: dependency %s is a %s, not a Go library", from, r.Name, r.Class)
	}
	info := goInfoOf(r.Name, c, from)
	if r.Class == "go_proto_library" && !protoValidArchive[k] {
		qfail("%s: %s has no archive (no compiler with valid_archive)", from, r.Name)
	}
	a := emitArchive(info, c, info.name)
	arcMemo[k] = a
	return a
}

func rootOf(p string) string {
	if strings.HasPrefix(p, "bazel-out/") {
		parts := strings.SplitN(p, "/", 4)
		return strings.Join(parts[:3], "/")
	}
	return ""
}

// emitArchive: emit_archive and emit_compilepkg: the GoCompilePkg action.
func emitArchive(info *goInfo, c *qcfg, name string) *goArc {
	arc := &goArc{info: info, a: outPath(c, info.label, name+".a"), x: outPath(c, info.label, name+".x")}
	importmap := info.importmap
	if info.isMain {
		importmap = "main"
	}
	importpath := ""
	if info.pathtype != inferredPath {
		importpath = strings.TrimSuffix(info.importpath, "_test")
	}
	if importpath == "" {
		importpath = info.name
	}
	argv := []string{"(builder)", "compilepkg", "-sdk", "(sdk)", "-goroot", path.Join(c.bin(), "external", rulesGo, "stdlib_"), "-installsuffix", "linux_amd64"}
	if t := goTags(c); len(t) > 0 {
		argv = append(argv, "-tags", strings.Join(t, ","))
	}
	for _, s := range info.srcs {
		argv = append(argv, "-src", s)
	}
	for _, d := range info.deps {
		argv = append(argv, "-arc", strings.Join(append([]string{d.info.importpath}, d.info.aliases...), ":")+"="+d.info.importmap+"="+d.x)
	}
	argv = append(argv, "-importpath", importpath)
	if importmap != "" {
		argv = append(argv, "-p", importmap)
	}
	for _, e := range info.embedsrcs {
		argv = append(argv, "-embedsrc", e)
	}
	uniq := func(flag string, xs []string, f func(string) string) {
		seen := map[string]bool{}
		for _, x := range xs {
			v := f(x)
			if !seen[v] {
				seen[v] = true
				argv = append(argv, flag, v)
			}
		}
	}
	srcsLib := append(append([]string{}, info.srcs...), arc.a)
	uniq("-embedroot", append(append([]string{}, srcsLib...), info.embedsrcs...), rootOf)
	uniq("-embedlookupdir", srcsLib, func(p string) string {
		return strings.TrimPrefix(strings.TrimPrefix(path.Dir(p), rootOf(p)), "/")
	})
	argv = append(argv, "-lo", arc.a, "-o", arc.x)
	ins := append(append([]string{}, info.srcs...), info.embedsrcs...)
	for _, d := range info.deps {
		ins = append(ins, d.x)
	}
	a := newAction("GoCompilePkg", info.label, c, []string{arc.a, arc.x}, ins)
	env := goEnv(c)
	if !info.cgo {
		env = append(env[:3], env[4:]...) // (env_for_path_mapping: no GOROOT)
	}
	a.setArgv(argv, env)
	arc.action = a
	for _, d := range info.deps {
		arc.xdefs.update(&d.xdefs)
	}
	arc.xdefs.update(&info.xdefs)
	return arc
}

// goBinary: a go_binary in c: go_transition, its main package's compile and the GoLink action.
func goBinary(r *qRule, c *qcfg) *action {
	k := memoKey{r.Name, c}
	if a := binMemo[k]; a != nil {
		return a
	}
	qKinds[r.Class]++
	set := map[string]string{}
	tern := func(attr string) string {
		v := c.resolve(r, attr).Str
		switch v {
		case "on":
			set[rulesGoLabel("go/config:"+attr)] = "true"
		case "off":
			set[rulesGoLabel("go/config:"+attr)] = "false"
		case "auto", "":
		default:
			qfail("%s: %s = %q", r.Name, attr, v)
		}
		return v
	}
	tern("static")
	race, msan, pureA := tern("race"), tern("msan"), tern("pure")
	if race == "on" || msan == "on" {
		set[rulesGoLabel("go/config:pure")] = "false"
	}
	if pureA == "on" {
		set[rulesGoLabel("go/config:race")] = "false"
		set[rulesGoLabel("go/config:msan")] = "false"
	}
	for _, x := range []string{"goos", "goarch", "linkmode"} {
		if v := c.resolve(r, x).Str; v != "" && v != "auto" {
			qfail("%s: %s = %q is not handled", r.Name, x, v)
		}
	}
	if t := c.resolve(r, "gotags").List; len(t) > 0 {
		m := map[string]bool{}
		var tags []string
		for _, x := range t {
			if !m[x] {
				m[x] = true
				tags = append(tags, x)
			}
		}
		sort.Strings(tags)
		set[rulesGoLabel("go/config:tags")] = strings.Join(tags, ",")
	}
	bc := c.transition(set)
	info := newGoInfo(r, bc, true, false, nil, nil)
	name := c.resolve(r, "basename").Str
	if name == "" {
		name = info.name
	}
	arc := emitArchive(info, bc, name)
	exePath := outPath(bc, r.Name, name+"_/"+name)
	if o := c.resolve(r, "out").Str; o != "" {
		exePath = outPath(bc, r.Name, o)
	}
	argv := []string{"(builder)", "link", "-main", arc.a}
	for _, k := range arc.xdefs.keys {
		argv = append(argv, "-X", k+"="+arc.xdefs.m[k])
	}
	a := newAction("GoLink", r.Name, bc, []string{exePath}, []string{arc.a})
	a.setArgv(argv, goEnv(bc))
	binMemo[k] = a
	return a
}
