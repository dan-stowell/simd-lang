// querygvisor.go: gVisor's own Starlark rules, translated from its .bzl files (gvisor-bazel at the corpus's pin):
// tools/bazeldefs/go.bzl (_go_imports), tools/go_stateify/defs.bzl, tools/go_marshal/defs.bzl,
// tools/go_generics/defs.bzl (go_template, go_template_instance), tools/nogo/defs.bzl (nogo_facts_render),
// tools/arch.bzl (_arch_genrule), tools/bazeldefs/defs.bzl (bpf_program), pkg/seccomp/precompiledseccomp/defs.bzl
// (_run_generator); and what bpf_program reads of others: rules_cc's cc_library (headers and include directories
// only) and skylib's directory/subdirectory.
//
// Differences from Bazel, by design: _go_imports' `go` symlink points to bzfc's -go (the action's GOROOT is fc's
// standard library either way, GoStdlib in main.go); _arch_genrule's copy is cp(1), not bazel_lib's coreutils
// toolchain (a query resolves no toolchain); its amd64 split configuration is the configuration itself (checked:
// the platform it names must have the constraints linux/x86_64 only).
package main

import (
	"fmt"
	"path"
	"strings"
)

var stdlibMemo = map[*qcfg]string{}

// goStdlib: the GoStdlib action's output in c (main.go's GoStdlib fills its directory with fc's GOROOT).
func goStdlib(c *qcfg) string {
	if o, ok := stdlibMemo[c]; ok {
		return o
	}
	o := path.Join(c.bin(), "external", rulesGo, "stdlib_", "pkg")
	newAction("GoStdlib", rulesGoLabel(":stdlib"), c, []string{o}, nil)
	stdlibMemo[c] = o
	return o
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func srcFiles(r *qRule, c *qcfg, attr string) []string {
	var out []string
	for _, s := range c.resolve(r, attr).List {
		out = append(out, qfiles(s, c, r.Name)...)
	}
	return out
}

// _go_imports
func goImports(r *qRule, c *qcfg) {
	src := file1(c.resolve(r, "src").Str, c, r.Name)
	out := outOf(r, c, "out")
	tool := exe(r.Attrs["$goimports"].Str, cfgExec, r.Name)
	tools := labelName(r.Name) + "_tools"
	goLink := outPath(c, r.Name, tools+"/go")
	newAction("ExecutableSymlink", r.Name, c, []string{goLink}, []string{*flagGo})
	launcher := outPath(c, r.Name, tools+"/goimports.sh")
	w := newAction("FileWrite", r.Name, c, []string{launcher}, nil)
	w.IsExecutable = true
	w.FileContents = fmt.Sprintf("#!/bin/sh\nPATH=$PWD/%s exec %s %s > %s\n", shellQuote(path.Dir(goLink)), shellQuote(tool), shellQuote(src), shellQuote(out))
	a := newAction("GoImports", r.Name, c, []string{out}, []string{src, tool, goLink, launcher, goStdlib(c)})
	a.setArgv([]string{launcher}, goEnv(c))
}

// go_stateify
func goStateify(r *qRule, c *qcfg) {
	out := outOf(r, c, "out")
	argv := []string{exe(r.Attrs["$tool"].Str, cfgExec, r.Name), "-output=" + out, "-fullpkg=" + c.resolve(r, "package").Str}
	if s := c.resolve(r, "$statepkg").Str; s != "" {
		argv = append(argv, "-statepkg="+s)
	}
	if im := c.resolve(r, "imports").List; len(im) > 0 {
		argv = append(argv, "-imports="+strings.Join(im, ","))
	}
	srcs := srcFiles(r, c, "srcs")
	argv = append(append(argv, "--"), srcs...)
	a := newAction("GoStateify", r.Name, c, []string{out}, append(srcs, argv[0]))
	a.setArgv(argv, nil)
}

// go_marshal
func goMarshal(r *qRule, c *qcfg) {
	n := labelName(r.Name)
	lib, test, testU := outPath(c, r.Name, n+"_unsafe.go"), outPath(c, r.Name, n+"_test.go"), outPath(c, r.Name, n+"_unconditional_test.go")
	argv := []string{exe(r.Attrs["$tool"].Str, cfgExec, r.Name), "-output=" + lib, "-pkg=" + c.resolve(r, "package").Str,
		"-output_test=" + test, "-output_test_unconditional=" + testU}
	if d := c.resolve(r, "debug"); d.Int != 0 || d.Str == "true" {
		argv = append(argv, "-debug")
	}
	srcs := srcFiles(r, c, "srcs")
	argv = append(append(argv, "--"), srcs...)
	a := newAction("GoMarshal", r.Name, c, []string{lib, test, testU}, append(srcs, argv[0]))
	a.setArgv(argv, nil)
}

var templateMemo = map[memoKey]string{}

// go_template: the merged template (TemplateInfo.template).
func goTemplate(l string, c *qcfg, from string) string {
	r := follow(l, c, from)
	if r.Class != "go_template" {
		qfail("%s: template %s is a %s", from, r.Name, r.Class)
	}
	k := memoKey{r.Name, c}
	if t, ok := templateMemo[k]; ok {
		return t
	}
	seeRule(r)
	t := outPath(c, r.Name, labelName(r.Name)+"_template.go")
	srcs := srcFiles(r, c, "srcs")
	tool := exe(r.Attrs["$tool"].Str, cfgExec, r.Name)
	a := newAction("GoGenericsTemplate", r.Name, c, []string{t}, append(srcs, tool))
	a.setArgv(append([]string{tool, "-o=" + t}, srcs...), nil)
	templateMemo[k] = t
	return t
}

// go_template_instance
func goTemplateInstance(r *qRule, c *qcfg) {
	t := goTemplate(c.resolve(r, "template").Str, c, r.Name)
	out := outOf(r, c, "out")
	tool := exe(r.Attrs["$tool"].Str, cfgExec, r.Name)
	argv := []string{tool, "-i=" + t, "-o=" + out}
	if p := c.resolve(r, "package").Str; p != "" {
		argv = append(argv, "-p="+p)
	}
	if p := c.resolve(r, "prefix").Str; p != "" {
		argv = append(argv, "-prefix="+p)
	}
	if p := c.resolve(r, "suffix").Str; p != "" {
		argv = append(argv, "-suffix="+p)
	}
	for _, x := range []struct{ attr, flag string }{{"types", "-t"}, {"consts", "-c"}, {"imports", "-import"}, {"input_substrs", "-in-substr"}, {"substrs", "-out-substr"}} {
		for _, kv := range c.resolve(r, x.attr).Dict {
			argv = append(argv, x.flag+"="+kv[0]+"="+kv[1])
		}
	}
	if b := c.resolve(r, "anon"); b.Int != 0 || b.Str == "true" {
		argv = append(argv, "-anon")
	}
	a := newAction("GoGenericsInstance", r.Name, c, []string{out}, []string{t, tool})
	a.setArgv(argv, nil)
}

// nogo_facts_render: bzfc renders it itself (render.go): only the command line's render part matters.
func nogoFactsRender(r *qRule, c *qcfg) {
	tmpl := qfiles(c.resolve(r, "template").Str, c, r.Name)
	out := outOf(r, c, "output")
	argv := []string{"(nogo)", "render", "-template=" + tmpl[0], "-output=" + out}
	argv = append(argv, srcFiles(r, c, "srcs")...)
	a := newAction("GoStaticAnalysis", r.Name, c, []string{out}, nil)
	a.setArgv(argv, goEnv(c))
}

var archMemo = map[memoKey][]string{}

// _arch_genrule: the src (in the amd64 configuration: here, c) copied under template % "amd64".
func archGenrule(r *qRule, c *qcfg) []string {
	k := memoKey{r.Name, c}
	if o, ok := archMemo[k]; ok {
		return o
	}
	seeRule(r)
	if ta := c.resolve(r, "target_arch").Str; ta != "amd64" {
		qfail("%s: target_arch %q: only amd64 is handled", r.Name, ta)
	}
	if v, err := c.flagValue("@@//tools:export_go_sources"); err == nil && v == "true" {
		qfail("%s: //tools:export_go_sources is on: every architecture's copy is not handled", r.Name)
	}
	src := file1(c.resolve(r, "src").Str, inPlatform(c, "@@//tools/bazeldefs:linux_amd64", r.Name), r.Name)
	out := outPath(c, r.Name, strings.Replace(c.resolve(r, "template").Str, "%s", "amd64", 1))
	a := newAction("CopyFile", r.Name, c, []string{out}, []string{src})
	a.setArgv([]string{"cp", src, out}, nil)
	archMemo[k] = []string{out}
	return archMemo[k]
}

// inPlatform: c with the platform a transition names (when the platform is in the query's graph; otherwise nothing
// can be known of it: c itself).
func inPlatform(c *qcfg, l, from string) *qcfg {
	if qRules[l] == nil {
		return c
	}
	return c.withPlatform(l, from)
}

// directoryPath: skylib's DirectoryInfo.path of a directory or subdirectory rule.
func directoryPath(l string, c *qcfg, from string) string {
	r := follow(l, c, from)
	seeRule(r)
	switch r.Class {
	case "directory":
		repo, pkg, _ := splitLabel(r.Name)
		return pkgDir(repo, pkg)
	case "subdirectory":
		return path.Join(directoryPath(c.resolve(r, "parent").Str, c, r.Name), c.resolve(r, "path").Str)
	case "_headers_directory": // (llvm's directory.bzl: the DirectoryInfo of its directory attribute)
		return directoryPath(c.resolve(r, "directory").Str, c, r.Name)
	}
	qfail("%s: %s is a %s, not a directory", from, r.Name, r.Class)
	return ""
}

type ccCtx struct {
	includes, quote, system []string
	headers                 []string
}

var ccMemo = map[memoKey]*ccCtx{}

// ccCompilation: a cc_library's compilation context, headers and include directories only (no sources, defines,
// copts: bpf_program reads no more). _virtual_includes when strip_include_prefix or include_prefix is set.
func ccCompilation(l string, c *qcfg, from string) *ccCtx {
	r := follow(l, c, from)
	if r.Class != "cc_library" {
		qfail("%s: %s is a %s, not a cc_library", from, r.Name, r.Class)
	}
	k := memoKey{r.Name, c}
	if x := ccMemo[k]; x != nil {
		return x
	}
	seeRule(r)
	if len(c.resolve(r, "srcs").List) > 0 {
		qfail("%s: cc_library with srcs is not handled", r.Name)
	}
	repo, pkg, name := splitLabel(r.Name)
	x := &ccCtx{}
	root := pkgDir(repo, "")
	if root == "" {
		root = "."
	}
	x.quote = []string{root, path.Join(c.bin(), pkgDir(repo, ""))}
	for _, inc := range c.resolve(r, "includes").List {
		x.system = append(x.system, path.Join(pkgDir(repo, pkg), inc))
	}
	strip, prefix := c.resolve(r, "strip_include_prefix").Str, c.resolve(r, "include_prefix").Str
	hdrs := srcFiles(r, c, "hdrs")
	if strip != "" || prefix != "" {
		if strings.HasPrefix(strip, "/") {
			strip = strip[1:]
		} else {
			strip = joinNonEmpty(pkg, strip)
		}
		vroot := outPath(c, r.Name, "_virtual_includes/"+name)
		x.system = append(x.system, vroot)
		base := pkgDir(repo, strip)
		for _, h := range hdrs {
			if !strings.HasPrefix(h, base+"/") {
				qfail("%s: header %s is not under %s", r.Name, h, base)
			}
			v := path.Join(vroot, prefix, strings.TrimPrefix(h, base+"/"))
			if byOutput[v] == nil {
				newAction("Symlink", r.Name, c, []string{v}, []string{h})
			}
			x.headers = append(x.headers, v)
		}
	} else {
		x.headers = hdrs
	}
	for _, d := range c.resolve(r, "deps").List {
		dc := ccCompilation(d, c, r.Name)
		x.includes = appendUniq(x.includes, dc.includes...)
		x.quote = appendUniq(x.quote, dc.quote...)
		x.system = appendUniq(x.system, dc.system...)
		x.headers = append(x.headers, dc.headers...)
	}
	ccMemo[k] = x
	return x
}

func appendUniq(xs []string, ys ...string) []string {
	have := map[string]bool{}
	for _, x := range xs {
		have[x] = true
	}
	for _, y := range ys {
		if !have[y] {
			have[y] = true
			xs = append(xs, y)
		}
	}
	return xs
}

// bpf_program
func bpfProgram(r *qRule, c *qcfg) {
	clang := exe(r.Attrs["$clang"].Str, cfgExec, r.Name)
	rd := directoryPath(r.Attrs["$clang_resource_dir"].Str, cfgExec, r.Name)
	kh := directoryPath(c.resolve(r, "$kernel_headers").Str, c, r.Name)
	lib := ccCompilation(r.Attrs["$libbpf"].Str, c, r.Name)
	src := file1(c.resolve(r, "src").Str, c, r.Name)
	out := outOf(r, c, "bpf_object")
	argv := []string{clang, "-O2", "-Wall", "-Werror", "--target=bpfel", "-mcpu=v1", "-nostdinc", "-resource-dir", rd, "-isystem", rd + "/include", "-isystem", kh}
	for _, i := range lib.includes {
		argv = append(argv, "-isystem", i)
	}
	for _, i := range lib.quote {
		argv = append(argv, "-iquote", i)
	}
	for _, i := range lib.system {
		argv = append(argv, "-isystem", i)
	}
	argv = append(argv, "-c", src, "-o", out)
	ins := append(append([]string{clang, src}, srcFiles(r, c, "hdrs")...), lib.headers...)
	a := newAction("BPFCompile", r.Name, c, []string{out}, ins)
	a.setArgv(argv, nil)
}

// _run_generator (precompiled seccomp programs)
func runGenerator(r *qRule, c *qcfg) {
	g := c.resolve(r, "exec_generator").Str
	if g == "" {
		qfail("%s: _run_generator without exec_generator (a target generator, maybe under qemu) is not handled", r.Name)
	}
	gen := exe(g, cfgExec, r.Name)
	out := outOf(r, c, "out")
	a := newAction("PrecompileSeccomp", r.Name, c, []string{out}, []string{gen})
	a.setArgv([]string{gen, "--package=" + c.resolve(r, "out_package_name").Str, "--out=" + out}, nil)
}
