// querygen.go: rules that write generated sources: Bazel's genrule, and the workspaces' own Starlark rules,
// translated by hand from their .bzl (each one names its source; a rule of the same class name defined in another
// file is not assumed to be the same rule: ruleBzl checks where it came from).
package main

import (
	"path"
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

func genrule(r *qRule, c *qcfg) {
	qfail("%s: genrule is not handled yet", r.Name)
}
