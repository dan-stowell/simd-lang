// queryproto.go: proto_library (protobuf 34's bazel/private/proto_library_rule.bzl, proto_info.bzl,
// common/proto_common.bzl) and go_proto_library with rules_go's _go_proto_compiler (proto/def.bzl, compiler.bzl).
package main

import (
	"fmt"
	"path"
	"strings"
)

// depset: a Starlark depset of strings, "default" (postorder) or "preorder".
type depset struct {
	pre    bool
	direct []string
	trans  []*depset
}

func (d *depset) list() []string {
	var out []string
	seen := map[string]bool{}
	nodes := map[*depset]bool{}
	var walk func(d *depset)
	add := func(xs []string) {
		for _, x := range xs {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	walk = func(d *depset) {
		if d == nil || nodes[d] {
			return
		}
		nodes[d] = true
		if d.pre {
			add(d.direct)
		}
		for _, t := range d.trans {
			walk(t)
		}
		if !d.pre {
			add(d.direct)
		}
	}
	walk(d)
	return out
}

// protoFile: a File of a proto_library: exec path, root, short path, owner repository.
type protoFile struct {
	path, root, short, repo string
}

type protoInfoT struct {
	label           string
	direct          []protoFile
	transSources    *depset // exec paths
	transProtoPath  *depset
	checkDeps       []protoFile // check_deps_sources (flattened, in order)
	transDescSets   *depset
	protoSourceRoot string
	descSet         string
}

var protoMemo = map[memoKey]*protoInfoT{}

func protoInfo(r *qRule, c *qcfg) *protoInfoT {
	c = c.root() // (non_go_tool_transition, and proto_library reads no Go setting)
	k := memoKey{r.Name, c}
	if p := protoMemo[k]; p != nil {
		return p
	}
	qKinds[r.Class]++
	repo, pkg, name := splitLabel(r.Name)
	pbRepo, _, _ := splitLabel(canon(r.Attrs["$protocopt"].Str)) // (protobuf's canonical repository name)
	pb := "@@" + pbRepo
	var deps, optDeps []*protoInfoT
	for _, d := range c.resolve(r, "deps").List {
		deps = append(deps, protoInfo(protoRule(d, c, r.Name), c))
	}
	for _, d := range c.resolve(r, "option_deps").List {
		optDeps = append(optDeps, protoInfo(protoRule(d, c, r.Name), c))
	}
	if len(c.resolve(r, "exports").List) > 0 {
		qfail("%s: proto_library exports are not handled", r.Name)
	}
	importPrefix := c.resolve(r, "import_prefix").Str
	strip := c.resolve(r, "strip_import_prefix").Str
	if strings.HasPrefix(strip, "/") {
		strip = strip[1:]
	} else {
		strip = joinNonEmpty(pkg, strip)
	}
	strip = strings.TrimSuffix(strip, "/")
	wsRoot := ""
	if repo != "" {
		wsRoot = "external/" + repo
	}
	var srcs []protoFile
	for _, s := range c.resolve(r, "srcs").List {
		for _, f := range qfiles(s, c, r.Name) {
			pf := protoFile{path: f, root: rootOf(f), repo: repo}
			rel := strings.TrimPrefix(strings.TrimPrefix(f, pf.root), "/")
			if repo != "" {
				pf.short = "../" + repo + "/" + strings.TrimPrefix(rel, "external/"+repo+"/")
			} else {
				pf.short = rel
			}
			srcs = append(srcs, pf)
		}
	}
	protoPath := ""
	if importPrefix != "" || strip != "" {
		virtual := joinNonEmpty("_virtual_imports", name)
		protoPath = joinNonEmpty(pkg, virtual)
		full := strip
		if repo != "" {
			full = joinNonEmpty("..", repo, strip)
		}
		if full != "" {
			full += "/"
		}
		var vs []protoFile
		for _, s := range srcs {
			if !strings.HasPrefix(s.short, full) {
				qfail("%s: .proto file %s is not under the strip prefix %s", r.Name, s.short, full)
			}
			ip := s.short[len(full):]
			out := outPath(c, r.Name, joinNonEmpty(virtual, importPrefix, ip))
			if byOutput[out] == nil {
				newAction("Symlink", r.Name, c, []string{out}, []string{s.path})
			}
			v := protoFile{path: out, root: c.bin(), repo: repo}
			rel := strings.TrimPrefix(out, c.bin()+"/")
			if repo != "" {
				v.short = "../" + repo + "/" + strings.TrimPrefix(rel, "external/"+repo+"/")
			} else {
				v.short = rel
			}
			vs = append(vs, v)
		}
		srcs = vs
	}
	p := &protoInfoT{label: r.Name, direct: srcs, descSet: outPath(c, r.Name, name+"-descriptor-set.proto.bin")}
	p.transSources = &depset{pre: true}
	for _, s := range srcs {
		p.transSources.direct = append(p.transSources.direct, s.path)
	}
	p.transProtoPath = &depset{}
	seenRoot := map[string]bool{}
	for _, s := range srcs {
		if seenRoot[s.root] {
			continue
		}
		seenRoot[s.root] = true
		var v string
		if s.root == "" {
			v = joinNonEmpty(wsRoot, protoPath)
		} else {
			v = joinNonEmpty(s.root, wsRoot, protoPath)
		}
		if v == "" {
			v = "."
		}
		p.transProtoPath.direct = append(p.transProtoPath.direct, v)
	}
	for _, d := range append(append([]*protoInfoT{}, deps...), optDeps...) {
		p.transSources.trans = append(p.transSources.trans, d.transSources)
		p.transProtoPath.trans = append(p.transProtoPath.trans, d.transProtoPath)
	}
	if len(srcs) > 0 {
		p.checkDeps = srcs
	} else {
		seen := map[string]bool{}
		for _, d := range deps {
			for _, f := range d.checkDeps {
				if !seen[f.path] {
					seen[f.path] = true
					p.checkDeps = append(p.checkDeps, f)
				}
			}
		}
	}
	p.transDescSets = &depset{direct: []string{p.descSet}}
	for _, d := range deps {
		p.transDescSets.trans = append(p.transDescSets.trans, d.transDescSets)
	}
	if protoPath != "" {
		p.protoSourceRoot = joinNonEmpty(c.bin(), wsRoot, protoPath)
	} else {
		p.protoSourceRoot = joinNonEmpty(wsRoot, protoPath)
	}
	if p.protoSourceRoot == "" {
		p.protoSourceRoot = "."
	}
	protoMemo[k] = p
	// _write_descriptor_set
	if len(srcs) == 0 {
		a := newAction("FileWrite", r.Name, c, []string{p.descSet}, nil)
		a.FileContents = ""
		return p
	}
	protoc := protocPath(r.Name)
	argv := []string{protoc, "--descriptor_set_out=" + p.descSet}
	pp := p.transProtoPath.list()
	for _, x := range pp {
		if strings.Count(x, "/") > 4 {
			argv = append(argv, "-I"+x)
		}
	}
	for _, x := range pp {
		if n := strings.Count(x, "/"); n > 2 && n <= 4 {
			argv = append(argv, "-I"+x)
		}
	}
	for _, x := range pp {
		if strings.Count(x, "/") <= 2 && x != "." {
			argv = append(argv, "-I"+x)
		}
	}
	argv = append(argv, "-I.")
	if o := c.flag(pb + "//bazel/flags/cc:protocopt"); o != "" {
		argv = append(argv, strings.Split(o, ",")...)
	}
	for _, s := range srcs {
		argv = append(argv, s.path)
	}
	if c.flag(pb+"//bazel/flags:experimental_proto_descriptor_sets_include_source_info") == "true" {
		argv = append(argv, "--include_source_info")
	}
	argv = append(argv, "--retain_options")
	strictDeps := c.flag(pb + "//bazel/flags:strict_proto_deps")
	if strictDeps == "error" {
		strictDeps, _ = c.native("strict_proto_deps")
	}
	lstr := labelString(r.Name)
	if strictDeps != "" && strings.ToLower(strictDeps) != "off" {
		strictImportable := &depset{}
		for _, s := range srcs {
			strictImportable.direct = append(strictImportable.direct, importPath(s))
		}
		for _, d := range deps {
			cd := &depset{}
			for _, f := range d.checkDeps {
				cd.direct = append(cd.direct, importPath(f))
			}
			strictImportable.trans = append(strictImportable.trans, cd)
		}
		argv = append(argv, "--direct_dependencies", strings.Join(strictImportable.list(), ":"))
		argv = append(argv, fmt.Sprintf("--direct_dependencies_violation_msg=%%s is imported, but %s doesn't have direct `deps` on a proto_library that 'srcs' it.", lstr))
		opt := &depset{}
		for _, s := range srcs {
			opt.direct = append(opt.direct, importPath(s))
		}
		for _, d := range optDeps {
			cd := &depset{}
			for _, f := range d.checkDeps {
				cd.direct = append(cd.direct, importPath(f))
			}
			opt.trans = append(opt.trans, cd)
		}
		argv = append(argv, "--option_dependencies", strings.Join(opt.list(), ":"))
		argv = append(argv, fmt.Sprintf("--option_dependencies_violation_msg=%%s is option imported, but %s doesn't have direct `option_deps` on a proto_library that 'srcs' it.", lstr))
	}
	if si := c.flag(pb + "//bazel/flags:strict_public_imports"); si != "off" && strings.ToUpper(si) != "OFF" {
		qfail("%s: strict_public_imports=%s is not handled", r.Name, si)
	}
	ins := append(p.transSources.list(), protoc)
	for _, d := range append(append([]*protoInfoT{}, deps...), optDeps...) {
		ins = append(ins, d.transDescSets.list()...)
	}
	a := newAction("GenProtoDescriptorSet", r.Name, c, []string{p.descSet}, ins)
	a.setArgv(argv, [][2]string{{"PATH", "/bin:/usr/bin:/usr/local/bin"}})
	return p
}

// labelString: str(Label) as Bazel 9 prints it: //pkg:name in the main repository, @@repo//pkg:name elsewhere.
func labelString(l string) string { return strings.TrimPrefix(l, "@@//") }

func init() { _ = labelString }

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// importPath: proto_common.get_import_path.
func importPath(f protoFile) string {
	rp := f.short
	if f.repo != "" {
		rp = strings.TrimPrefix(rp, "../"+f.repo+"/")
	}
	if i := strings.Index(rp, "_virtual_imports/"); i >= 0 {
		j := strings.Index(rp[i+len("_virtual_imports/"):], "/")
		rp = rp[i+len("_virtual_imports/")+j+1:]
	}
	return rp
}

// protoPathOf: rules_go's proto_path(src, proto).
func protoPathOf(f protoFile, p *protoInfoT) string {
	var prefix string
	switch {
	case p.protoSourceRoot == ".":
		prefix = f.root + "/"
	case strings.HasPrefix(p.protoSourceRoot, f.root):
		prefix = p.protoSourceRoot + "/"
	default:
		prefix = path.Join(f.root, p.protoSourceRoot) + "/"
	}
	if !strings.HasPrefix(f.path, prefix) {
		return f.path
	}
	return f.path[len(prefix):]
}

func protoRule(l string, c *qcfg, from string) *qRule {
	r := follow(l, c, from)
	if r.Class != "proto_library" {
		qfail("%s: %s is a %s, not a proto_library", from, r.Name, r.Class)
	}
	return r
}

var protocMemo string

// protocPath: the proto toolchain's protoc (the one proto_toolchain rule of the query).
func protocPath(from string) string {
	if protocMemo != "" {
		return protocMemo
	}
	var tc []*qRule
	for _, r := range qRules {
		if r.Class == "proto_toolchain" {
			tc = append(tc, r)
		}
	}
	if len(tc) != 1 {
		qfail("%s needs protoc: the query has %d proto_toolchain rules, one expected (add the toolchain's target to the query expression)", from, len(tc))
	}
	qKinds["proto_toolchain"]++
	protocMemo = exe(cfgExec.resolve(tc[0], "proto_compiler").Str, cfgExec, tc[0].Name)
	return protocMemo
}

var protoValidArchive = map[memoKey]bool{}

// goProtoImports: the GoProtoImports the aspect gives a Go rule (get_imports): its own protos' import paths =
// its import path, then those of deps and embed.
var importsMemo = map[memoKey]*depset{}

func goProtoImports(l string, c *qcfg, from string) *depset {
	r := follow(l, c, from)
	if !isGoRule(r.Class) {
		return nil
	}
	k := memoKey{r.Name, c}
	if d, ok := importsMemo[k]; ok {
		return d
	}
	d := &depset{}
	importsMemo[k] = d
	if r.Class == "go_proto_library" {
		ip, _, _ := goContext(r, c)
		var protos []*qRule
		if p := c.resolve(r, "proto").Str; p != "" {
			protos = []*qRule{protoRule(p, c, r.Name)}
		} else {
			for _, p := range c.resolve(r, "protos").List {
				protos = append(protos, protoRule(p, c, r.Name))
			}
		}
		for _, pr := range protos {
			pi := protoInfo(pr, c)
			for _, f := range pi.checkDeps {
				d.direct = append(d.direct, protoPathOf(f, pi)+"="+ip)
			}
		}
	}
	for _, x := range append(c.resolve(r, "deps").List, c.resolve(r, "embed").List...) {
		if t := goProtoImports(x, c, r.Name); t != nil {
			d.trans = append(d.trans, t)
		}
	}
	return d
}

// goProtoLibrary: go_proto_library's GoInfo; its GoProtocGen actions derived.
func goProtoLibrary(r *qRule, c *qcfg) *goInfo {
	ip, _, _ := goContext(r, c)
	var compilers []*qRule
	if x := c.resolve(r, "compiler").Str; x != "" {
		compilers = []*qRule{follow(x, c, r.Name)}
	} else {
		for _, x := range c.resolve(r, "compilers").List {
			compilers = append(compilers, follow(x, c, r.Name))
		}
	}
	var protos []*protoInfoT
	if p := c.resolve(r, "proto").Str; p != "" {
		if len(c.resolve(r, "protos").List) > 0 {
			qfail("%s: both proto and protos", r.Name)
		}
		protos = []*protoInfoT{protoInfo(protoRule(p, c, r.Name), c)}
	} else {
		for _, p := range c.resolve(r, "protos").List {
			protos = append(protos, protoInfo(protoRule(p, c, r.Name), c))
		}
	}
	imports := goProtoImports(r.Name, c, r.Name).list()
	var goSrcs []string
	var merge []*goInfo
	for _, comp := range compilers {
		if comp.Class != "_go_proto_compiler" {
			qfail("%s: compiler %s is a %s", r.Name, comp.Name, comp.Class)
		}
		qKinds[comp.Class]++
		if b := c.resolve(comp, "valid_archive"); b.Int != 0 || b.Str == "true" {
			protoValidArchive[memoKey{r.Name, c}] = true
		}
		goSrcs = append(goSrcs, goProtoCompile(r, c, comp, protos, imports, ip)...)
		merge = append(merge, goInfoOf(comp.Name, c, r.Name))
	}
	return newGoInfo(r, c, false, true, goSrcs, merge)
}

// goProtoCompile: go_proto_compile: one GoProtocGen action.
func goProtoCompile(r *qRule, c *qcfg, comp *qRule, protos []*protoInfoT, imports []string, importpath string) []string {
	var goSrcs []string
	outpath := ""
	var keys []string
	seen := map[string]string{}
	var descs []*depset
	suffixes := c.resolve(comp, "suffixes").List
	if len(suffixes) == 0 {
		suffixes = []string{c.resolve(comp, "suffix").Str}
	}
	for _, p := range protos {
		descs = append(descs, p.transDescSets)
		for _, src := range p.checkDeps {
			pp := protoPathOf(src, p)
			if o, ok := seen[pp]; ok {
				if o != src.path {
					qfail("%s: proto files %s and %s have the same import path %s", r.Name, src.path, o, pp)
				}
				continue
			}
			seen[pp] = src.path
			keys = append(keys, pp)
			base := strings.TrimSuffix(path.Base(src.path), ".proto")
			for _, s := range suffixes {
				goSrcs = append(goSrcs, outPath(c, r.Name, labelName(r.Name)+"_/"+importpath+"/"+base+s))
			}
			if outpath == "" {
				d := path.Dir(goSrcs[0])
				outpath = d[:len(d)-len(importpath)]
			}
		}
	}
	if len(goSrcs) == 0 {
		return nil
	}
	desc := (&depset{trans: descs}).list()
	goProtoc := exe(comp.Attrs["$go_protoc"].Str, cfgExec, comp.Name)
	plugin := exe(c.resolve(comp, "plugin").Str, cfgExec, comp.Name)
	protoc := protocPath(r.Name)
	argv := []string{goProtoc, "-protoc", protoc, "-importpath", importpath, "-out_path", outpath, "-plugin", plugin}
	if b := c.resolve(comp, "always_generates"); b.Int != 0 || b.Str == "true" {
		argv = append(argv, "-strict")
	}
	for _, o := range c.resolve(comp, "options").List {
		argv = append(argv, "-option", o)
	}
	if b := c.resolve(comp, "import_path_option"); b.Int != 0 || b.Str == "true" {
		argv = append(argv, "-option", "import_path="+importpath)
	}
	for _, d := range desc {
		argv = append(argv, "-descriptor_set", d)
	}
	for _, g := range goSrcs {
		argv = append(argv, "-expected", g)
	}
	for _, i := range imports {
		argv = append(argv, "-import", i)
	}
	argv = append(argv, keys...)
	ins := append([]string{goProtoc, protoc, plugin}, desc...)
	a := newAction("GoProtocGen", r.Name, c, goSrcs, ins)
	a.setArgv(argv, goEnv(c))
	return goSrcs
}
