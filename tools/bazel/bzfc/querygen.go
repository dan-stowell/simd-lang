// querygen.go: rules that write generated sources: Bazel's genrule, and the workspaces' own Starlark rules,
// translated by hand from their .bzl (each one names its source; a rule of the same class name defined in another
// file is not assumed to be the same rule: ruleBzl checks where it came from).
package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// outOf: an output attribute's file in c.
func outOf(r *qRule, c *qcfg, attr string) string {
	o := c.resolve(r, attr).Str
	if o == "" {
		qfail("%s: no %s", r.Name, attr)
	}
	return outPath(c, canon(o), labelName(canon(o)))
}

// ruleBzl: the .bzl a Starlark rule class comes from (its rule_class_key: "<bzl label>%<class>"), checked against
// the file the translation here was made from.
func checkBzl(r *qRule, want string) {
	if r.ClassKey == "" {
		return
	}
	bzl, _, _ := strings.Cut(r.ClassKey, "%")
	if !strings.HasSuffix(bzl, want) {
		qfail("%s: rule %s comes from %s, not %s: not handled", r.Name, r.Class, bzl, want)
	}
}

// _go_yacc: buildtools' build/build_defs.bzl.
func goYacc(r *qRule, c *qcfg) {
	checkBzl(r, "//build:build_defs.bzl")
	tool := exe(r.Attrs["$go_yacc_tool"].Str, cfgExec, r.Name)
	src := file1(c.resolve(r, "src").Str, c, r.Name)
	out := outOf(r, c, "out")
	a := newAction("Action", r.Name, c, []string{out}, []string{tool, src})
	a.setArgv([]string{tool, "-o", out, src}, [][2]string{{"GOROOT", path.Dir(tool) + "/.."}})
}

// generate_tables: buildtools' lang/defs.bzl.
func generateTables(r *qRule, c *qcfg) {
	checkBzl(r, "//lang:defs.bzl")
	tool := exe(c.resolve(r, "bin").Str, cfgExec, r.Name)
	src := file1(c.resolve(r, "src").Str, c, r.Name)
	out := outOf(r, c, "out")
	a := newAction("Action", r.Name, c, []string{out}, []string{tool, src})
	a.setArgv([]string{tool, "-input", src, "-output", out}, nil)
}

// std_package_list: gazelle's language/go/def.bzl (its input: sdkPackageList).
func stdPackageList(r *qRule, c *qcfg) {
	tool := exe(r.Attrs["$gen_std_package_list"].Str, cfgExec, r.Name)
	out := outOf(r, c, "out")
	pl := sdkPackageList()
	a := newAction("GoStdPackageList", r.Name, c, []string{out}, []string{tool, pl})
	a.setArgv([]string{tool, pl, out}, nil)
}

var sdkPackages string

// sdkPackageList: the Go SDK's packages.txt as rules_go's _build_package_list writes it: every directory under the
// SDK's src/ that holds a file of its :srcs filegroup (glob src/**, STDLIB_SRCS_EXCLUDE applied by the BUILD file,
// so by the query). The SDK's repository is not in deps(LABEL) (toolchain resolution): cold.sh adds its :srcs to the
// query expression; exactly one such filegroup must be in the graph.
func sdkPackageList() string {
	if sdkPackages != "" {
		return sdkPackages
	}
	var srcs []string
	for l, r := range qRules {
		repo, pkg, name := splitLabel(l)
		if r.Class == "filegroup" && pkg == "" && name == "srcs" && strings.Contains(repo, "go_sdk") {
			srcs = append(srcs, l)
		}
	}
	if len(srcs) != 1 {
		qfail("the Go SDK's package list: %d go_sdk //:srcs filegroups in the query's graph %v, one expected (add the SDK's //:srcs to the query expression)", len(srcs), srcs)
	}
	repo, _, _ := splitLabel(srcs[0])
	srcDir := "external/" + repo + "/src/"
	pkgs := map[string]bool{}
	for _, f := range qfiles(srcs[0], cfgExec, "the Go SDK") {
		if d := path.Dir(f); strings.HasPrefix(d, srcDir) { // (src.dirname: a file directly in src/ is no package)
			pkgs[d[len(srcDir):]] = true
		}
	}
	var list []string
	for p := range pkgs {
		list = append(list, p)
	}
	sort.Strings(list)
	sdkPackages = path.Join(cfgExec.bin(), "external", repo, "packages.txt")
	a := newAction("FileWrite", "@@"+repo+"//:package_list", cfgExec, []string{sdkPackages}, nil)
	a.FileContents = strings.Join(list, "\n") + "\n"
	return sdkPackages
}

// known_imports: gazelle's language/proto/gen/def.bzl.
func knownImports(r *qRule, c *qcfg) {
	tool := exe(r.Attrs["$bin"].Str, cfgExec, r.Name)
	src := file1(c.resolve(r, "src").Str, c, r.Name)
	out := outOf(r, c, "out")
	a := newAction("KnownImports", r.Name, c, []string{out}, []string{tool, src})
	a.setArgv([]string{tool, "-proto_csv", src, "-known_imports", out, "-package", c.resolve(r, "package").Str, "-var", c.resolve(r, "var").Str,
		"-key", fmt.Sprint(c.resolve(r, "key").Int), "-value", fmt.Sprint(c.resolve(r, "value").Int)}, nil)
}

// genrule: Bazel's (GenRuleBase): cmd_bash or cmd, $(location)-style and make variables expanded, run by
// bash after genrule-setup.sh. Labels in $(location ...) are matched against the labels of srcs, outs and tools.
func genrule(r *qRule, c *qcfg) {
	cmd := c.resolve(r, "cmd_bash").Str
	if cmd == "" {
		cmd = c.resolve(r, "cmd").Str
	}
	if cmd == "" {
		qfail("%s: genrule without cmd or cmd_bash", r.Name)
	}
	if len(c.resolve(r, "toolchains").List) > 0 {
		qfail("%s: genrule toolchains (their make variables) are not handled", r.Name)
	}
	type loc struct {
		label string
		files []string
	}
	var locs []loc
	var srcs, outs, ins []string
	for _, s := range c.resolve(r, "srcs").List {
		f := qfiles(s, c, r.Name)
		srcs = append(srcs, f...)
		locs = append(locs, loc{canon(s), f})
	}
	for _, o := range c.resolve(r, "outs").List {
		f := outPath(c, canon(o), labelName(canon(o)))
		outs = append(outs, f)
		locs = append(locs, loc{canon(o), []string{f}})
	}
	for _, attr := range []string{"tools", "exec_tools"} {
		for _, t := range c.resolve(r, attr).List {
			var f []string
			if tr := qRules[canon(t)]; tr != nil && (tr.Class == "go_binary" || tr.Class == "alias" || tr.Class == "go_reset_target") {
				f = []string{exe(t, cfgExec, r.Name)}
			} else {
				f = qfiles(t, cfgExec, r.Name)
			}
			ins = append(ins, f...)
			locs = append(locs, loc{canon(t), f})
		}
	}
	repo, pkg, _ := splitLabel(r.Name)
	find := func(ref string) []string {
		ref = strings.TrimSpace(ref)
		var want string
		switch {
		case strings.HasPrefix(ref, "@@"), strings.HasPrefix(ref, "//"):
			want = canon(ref)
			if strings.HasPrefix(ref, "//") {
				want = canon("@@" + repo + ref)
			}
		case strings.HasPrefix(ref, "@"): // an apparent repository name: by package and name only
			_, rest, _ := strings.Cut(ref, "//")
			want = "*//" + rest
			if !strings.Contains(rest, ":") {
				want += ":" + path.Base(rest)
			}
		default:
			want = canon("@@" + repo + "//" + pkg + ":" + strings.TrimPrefix(ref, ":"))
		}
		var hit []loc
		for _, l := range locs {
			if l.label == want || strings.HasPrefix(want, "*//") && strings.HasSuffix(l.label, want[1:]) {
				hit = append(hit, l)
			}
		}
		if len(hit) == 0 {
			qfail("%s: $(location %s): not a label of srcs, outs or tools", r.Name, ref)
		}
		return hit[0].files
	}
	one := func(what string, xs []string) string {
		if len(xs) != 1 {
			qfail("%s: %s is %d files, one expected", r.Name, what, len(xs))
		}
		return xs[0]
	}
	ruleDir := path.Join(c.bin(), pkgDir(repo, pkg))
	var b strings.Builder
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if ch != '$' || i+1 == len(cmd) {
			b.WriteByte(ch)
			continue
		}
		n := cmd[i+1]
		switch n {
		case '$':
			b.WriteByte('$')
			i++
		case '@':
			b.WriteString(one("$@", outs))
			i++
		case '<':
			b.WriteString(one("$<", srcs))
			i++
		case '(':
			j := strings.IndexByte(cmd[i:], ')')
			if j < 0 {
				qfail("%s: unterminated $( in cmd", r.Name)
			}
			v := cmd[i+2 : i+j]
			i += j
			fn, arg, hasArg := strings.Cut(v, " ")
			switch {
			case hasArg && (fn == "location" || fn == "execpath" || fn == "rootpath" && repo == ""):
				b.WriteString(one("$("+v+")", find(arg)))
			case hasArg && (fn == "locations" || fn == "execpaths" || fn == "rootpaths" && repo == ""):
				b.WriteString(strings.Join(find(arg), " "))
			case v == "SRCS":
				b.WriteString(strings.Join(srcs, " "))
			case v == "OUTS":
				b.WriteString(strings.Join(outs, " "))
			case v == "@D":
				if len(outs) == 1 {
					b.WriteString(path.Dir(outs[0]))
				} else {
					b.WriteString(ruleDir)
				}
			case v == "RULEDIR":
				b.WriteString(ruleDir)
			case v == "GENDIR", v == "BINDIR":
				b.WriteString(c.bin())
			case v == "TARGET_CPU":
				b.WriteString("k8")
			case v == "COMPILATION_MODE":
				b.WriteString(c.mode)
			default:
				qfail("%s: genrule variable $(%s) is not handled", r.Name, v)
			}
		default:
			qfail("%s: genrule cmd has $%c (a make variable bzfc does not know)", r.Name, n)
		}
	}
	full := "source external/bazel_tools/tools/genrule/genrule-setup.sh; " + b.String()
	a := newAction("Genrule", r.Name, c, outs, append(srcs, ins...))
	a.setArgv([]string{"/bin/bash", "-c", full}, [][2]string{{"PATH", "/bin:/usr/bin:/usr/local/bin"}})
}
