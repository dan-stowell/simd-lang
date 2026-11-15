// queryselect.go: the configurations the query front end builds in, and select() resolved against them.
//
// Two configurations, as Bazel's analysis would have them for a go_binary built on linux/amd64 for linux/amd64:
// the target configuration (compilation_mode fastbuild) and the exec configuration of tools (opt), the exec
// platform equal to the target platform. Each may be split further by rules_go's go_transition (a go_binary's
// pure/static/race/msan attributes); a split gets its own bazel-out directory, so that two configurations never
// write the same path. The workspace's .bazelrc is read for its unconditional `build`/`common` options (Starlark
// flags --@repo//pkg:flag=value, --define, --compilation_mode, --platforms, --stamp); an option this file does not
// know and that a config_setting asks about is an error.
//
// config_setting is evaluated here: values (native options: a fixed table of their defaults, the .bazelrc's
// values over them), define_values, flag_values (the flag's value: the .bazelrc's, else its build_setting_default),
// constraint_values (the target platform: @platforms//os:linux and //cpu:x86_64, and every constraint_setting's
// default_constraint_value). A select with no matching condition and no default is an error naming the rule; of
// several matching conditions the most specialized wins (its conditions a superset of the others'), as Bazel does.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type qcfg struct {
	dir      string // bazel-out/<dir>/bin
	exec     bool
	mode     string            // compilation_mode
	set      map[string]string // Starlark settings set by go_transition (canonical label -> value)
	base     *qcfg             // the configuration before the last go_transition (rules_go's non_go_transition)
	kids     map[string]*qcfg  // go_transition results, by their settings
	platform map[string]bool   // its target platform's constraint values (nil: the host's; platformHas)
}

var (
	cfgTarget, cfgExec *qcfg
	rcFlags            = map[string]string{} // .bazelrc: Starlark flags (canonical label -> value)
	rcNative           = map[string]string{} // .bazelrc: native options
	rcDefines          = map[string]string{}
	rcUnmapped         = map[string]string{} // .bazelrc flags of repositories not mapped: //pkg:name -> as written
)

func newCfgs() {
	cfgTarget = &qcfg{dir: "k8-fastbuild", mode: "fastbuild", set: map[string]string{}}
	cfgExec = &qcfg{dir: "k8-opt-exec", exec: true, mode: "opt", set: map[string]string{}}
	if m := rcNative["compilation_mode"]; m != "" {
		cfgTarget.mode = m
		cfgTarget.dir = "k8-" + m
	}
	cfgTarget.base, cfgExec.base = cfgTarget, cfgExec
}

// transition: cfg with Starlark settings changed (go_transition), or cfg itself when nothing changes.
func (c *qcfg) transition(set map[string]string) *qcfg {
	diff := map[string]string{}
	for k, v := range set {
		if c.flag(k) != v {
			diff[k] = v
		}
	}
	if len(diff) == 0 {
		return c
	}
	var keys []string
	for k := range diff {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	id := ""
	for _, k := range keys {
		id += k + "=" + diff[k] + ";"
	}
	if c.kids == nil {
		c.kids = map[string]*qcfg{}
	}
	if k := c.kids[id]; k != nil {
		return k
	}
	n := &qcfg{dir: fmt.Sprintf("%s-ST-q%d", c.dir, len(c.kids)+1), exec: c.exec, mode: c.mode, set: map[string]string{}, base: c, platform: c.platform}
	for k, v := range c.set {
		n.set[k] = v
	}
	for k, v := range diff {
		n.set[k] = v
	}
	c.kids[id] = n
	return n
}

func (c *qcfg) bin() string { return "bazel-out/" + c.dir + "/bin" }

// flag: a Starlark build setting's value in c ("" with an error for a label the query does not have).
func (c *qcfg) flag(l string) string {
	v, err := c.flagValue(l)
	if err != nil {
		qfail("%v", err)
	}
	return v
}

func (c *qcfg) flagValue(l string) (string, error) {
	l = canon(l)
	for i := 0; i < 20; i++ {
		r := qRules[l]
		if r == nil {
			return "", fmt.Errorf("build setting %s: not in the query's graph", l)
		}
		if r.Class != "alias" {
			break
		}
		l = canon(r.Attrs["actual"].Str)
	}
	if v, ok := c.set[l]; ok {
		return v, nil
	}
	if _, rest, _ := strings.Cut(l, "//"); rcUnmapped["//"+rest] != "" {
		return "", fmt.Errorf("build setting %s: the .bazelrc sets %s, whose repository is not known", l, rcUnmapped["//"+rest])
	}
	if v, ok := rcFlags[l]; ok && !c.exec {
		return v, nil
	}
	r := qRules[l]
	d := r.Attrs["build_setting_default"]
	if d == nil {
		return "", fmt.Errorf("build setting %s (%s): no build_setting_default", l, r.Class)
	}
	switch d.Type {
	case atStringList, atLabelList:
		return strings.Join(d.List, ","), nil
	case atBoolean:
		if d.Int != 0 || d.Str == "true" {
			return "true", nil
		}
		return "false", nil
	}
	return d.Str, nil
}

func boolStr(s string) string {
	switch strings.ToLower(s) {
	case "1", "true", "yes":
		return "true"
	case "0", "false", "no":
		return "false"
	}
	return s
}

// native options a config_setting may ask about, with Bazel's defaults
var nativeDefaults = map[string]string{
	"cpu": "k8", "host_cpu": "k8", "stamp": "false", "strip": "sometimes", "force_pic": "false", "dynamic_mode": "default",
	"experimental_proto_descriptor_sets_include_source_info": "false", "strict_proto_deps": "error", "strict_public_imports": "off",
	"collect_code_coverage": "false", "incompatible_enable_proto_toolchain_resolution": "true", "apple_platform_type": "macos",
	"crosstool_top": "@bazel_tools//tools/cpp:toolchain", "compiler": "", "features": "", "fission": "no", "build_python_zip": "auto",
	"incompatible_use_python_toolchains": "true", "java_runtime_version": "local_jdk", "android_cpu": "armeabi-v7a",
}

func (c *qcfg) native(k string) (string, bool) {
	switch k {
	case "compilation_mode":
		return c.mode, true
	case "stamp":
		if c.exec {
			return "false", true
		}
	case "is exec configuration":
		return boolStr(fmt.Sprint(c.exec)), true
	}
	if v, ok := rcNative[k]; ok && !c.exec {
		return v, true
	}
	v, ok := nativeDefaults[k]
	return v, ok
}

// matches: is the condition (a config_setting, an alias of one, a constraint_value) true in c? cond: its conditions,
// for specialization.
func (c *qcfg) matches(l string, from string) (ok bool, cond []string) {
	l = canon(l)
	r := qRules[l]
	if r == nil {
		qfail("%s: select condition %s is not in the query's graph", from, l)
	}
	switch r.Class {
	case "alias":
		return c.matches(c.resolve(r, "actual").Str, from)
	case "constraint_value":
		return c.platformHas(l), []string{"constraint " + l}
	case "config_setting":
	default:
		qfail("%s: select condition %s is a %s", from, l, r.Class)
	}
	ok = true
	if a := r.Attrs["values"]; a != nil {
		for _, kv := range a.Dict {
			cond = append(cond, "value "+kv[0]+"="+kv[1])
			if kv[0] == "define" {
				k, v, _ := strings.Cut(kv[1], "=")
				if rcDefines[k] != v {
					ok = false
				}
				continue
			}
			have, known := c.native(kv[0])
			if !known {
				qfail("%s: config_setting %s asks for option --%s, which bzfc does not know", from, l, kv[0])
			}
			if boolStr(have) != boolStr(kv[1]) {
				ok = false
			}
		}
	}
	if a := r.Attrs["define_values"]; a != nil {
		for _, kv := range a.Dict {
			cond = append(cond, "define "+kv[0]+"="+kv[1])
			if rcDefines[kv[0]] != kv[1] {
				ok = false
			}
		}
	}
	if a := r.Attrs["flag_values"]; a != nil {
		for _, kv := range a.Dict {
			cond = append(cond, "flag "+canon(kv[0])+"="+kv[1])
			v, err := c.flagValue(kv[0])
			if err != nil {
				qfail("%s: config_setting %s: %v", from, l, err)
			}
			if boolStr(v) != boolStr(kv[1]) {
				ok = false
			}
		}
	}
	if a := r.Attrs["constraint_values"]; a != nil {
		for _, cv := range a.List {
			cond = append(cond, "constraint "+canon(cv))
			if !c.platformHas(canon(cv)) {
				ok = false
			}
		}
	}
	return ok, cond
}

// platformOf: a configuration's target platform as the set of its constraint values: the explicit ones (the host
// platform's linux and x86_64, or those of a platform a transition named), and for every other constraint_setting of
// the graph its default_constraint_value.
func platformOf(explicit []string) map[string]bool {
	setOf := map[string]string{}
	for l, r := range qRules {
		if r.Class == "constraint_value" {
			setOf[l] = canon(r.Attrs["constraint_setting"].Str)
		}
	}
	p := map[string]bool{}
	has := map[string]bool{}
	for _, v := range explicit {
		v = followConstraint(v)
		p[v] = true
		has[setOf[v]] = true
	}
	for l, r := range qRules {
		if r.Class == "constraint_setting" && !has[l] {
			if d := r.Attrs["default_constraint_value"]; d != nil && d.Str != "" {
				p[canon(d.Str)] = true
			}
		}
	}
	return p
}

func followConstraint(cv string) string {
	cv = canon(cv)
	for i := 0; i < 20; i++ { // (aliases of constraint values: rules_go's //go/toolchain:linux)
		r := qRules[cv]
		if r == nil || r.Class != "alias" {
			break
		}
		cv = canon(cfgTarget.resolve(r, "actual").Str)
	}
	return cv
}

// platformHas: c's target platform (= its exec platform) has the constraint value.
func (c *qcfg) platformHas(cv string) bool {
	if c.platform == nil {
		c.platform = platformOf(hostConstraints)
	}
	return c.platform[followConstraint(cv)]
}

var hostConstraints = []string{"@@platforms//os:linux", "@@platforms//cpu:x86_64"}

// withPlatform: c with --platforms=l (a with_cfg or similar transition): the platform's constraint values and its
// flags (Starlark ones), its parents' under them; a platform that changes nothing is c itself.
func (c *qcfg) withPlatform(l, from string) *qcfg {
	r := follow(l, c, from)
	if r.Class != "platform" {
		qfail("%s: platform %s is a %s", from, r.Name, r.Class)
	}
	// constraints and flags, a parent's first (a child's value for the same constraint setting wins)
	setOf := func(cv string) string {
		if x := qRules[cv]; x != nil && x.Class == "constraint_value" {
			return canon(x.Attrs["constraint_setting"].Str)
		}
		return cv
	}
	bySetting := map[string]string{}
	set := map[string]string{}
	for k, v := range c.set {
		set[k] = v
	}
	var walk func(r *qRule, depth int)
	walk = func(r *qRule, depth int) {
		if depth > 10 {
			qfail("%s: platform parents too deep", r.Name)
		}
		for _, p := range c.resolve(r, "parents").List {
			pr := follow(p, c, r.Name)
			if pr.Class != "platform" {
				qfail("%s: parent %s is a %s", r.Name, pr.Name, pr.Class)
			}
			walk(pr, depth+1)
		}
		for _, cv := range c.resolve(r, "constraint_values").List {
			cv = followConstraint(cv)
			bySetting[setOf(cv)] = cv
		}
		repo, _, _ := splitLabel(r.Name)
		for _, f := range c.resolve(r, "flags").List {
			k, v, ok := strings.Cut(strings.TrimLeft(f, "-"), "=")
			if !ok || !(strings.HasPrefix(k, "@") || strings.HasPrefix(k, "//")) {
				qfail("%s: platform flag %q is not handled (Starlark flags with a value only)", r.Name, f)
			}
			var cl string
			if repo == "" {
				cl = canonRepo(k, moduleRepoMapping(*flagWS))
			} else if strings.HasPrefix(k, "@@") {
				cl = k
			}
			if cl == "" {
				qfail("%s: platform flag %q: its repository is not known", r.Name, f)
			}
			set[canon(cl)] = v
		}
	}
	walk(r, 0)
	var cons []string
	for _, cv := range bySetting {
		cons = append(cons, cv)
	}
	p := platformOf(cons)
	if !p["@@platforms//os:linux"] || !p["@@platforms//cpu:x86_64"] {
		qfail("%s: platform %s is not linux/x86_64", from, r.Name)
	}
	if c.platform == nil {
		c.platform = platformOf(hostConstraints)
	}
	same := len(p) == len(c.platform) && len(set) == len(c.set)
	for k := range p {
		if !c.platform[k] {
			same = false
		}
	}
	for k, v := range set {
		if c.set[k] != v {
			same = false
		}
	}
	if same {
		return c
	}
	id := "platform=" + r.Name
	if c.kids == nil {
		c.kids = map[string]*qcfg{}
	}
	if k := c.kids[id]; k != nil {
		return k
	}
	n := &qcfg{dir: fmt.Sprintf("%s-ST-q%d", c.dir, len(c.kids)+1), exec: c.exec, mode: c.mode, set: set, base: c.base, platform: p}
	c.kids[id] = n
	return n
}

func isDefaultCond(l string) bool { return strings.HasSuffix(l, "//conditions:default") }

// resolve: an attribute's value in c, its select()s resolved.
func (c *qcfg) resolve(r *qRule, name string) qVal {
	a := r.Attrs[name]
	if a == nil {
		return qVal{}
	}
	if a.Sel == nil {
		return a.qVal
	}
	var out qVal
	for _, sel := range a.Sel.Elems {
		var pick *qSelEntry
		var pickCond []string
		var def *qSelEntry
		ambiguous := ""
		for i := range sel.Entries {
			e := &sel.Entries[i]
			if isDefaultCond(e.Label) {
				def = e
				continue
			}
			ok, cond := c.matches(e.Label, r.Name)
			if !ok {
				continue
			}
			switch {
			case pick == nil, superset(cond, pickCond):
				pick, pickCond, ambiguous = e, cond, ""
			case superset(pickCond, cond):
			default:
				ambiguous = e.Label
			}
		}
		if ambiguous != "" {
			qfail("%s: attribute %s: select conditions %s and %s both match and neither refines the other", r.Name, name, pick.Label, ambiguous)
		}
		if pick == nil {
			pick = def
		}
		if pick == nil {
			msg := sel.NoMatch
			qfail("%s: attribute %s: no select condition matches in %s (and no default) %s", r.Name, name, c.dir, msg)
		}
		v := pick.qVal
		out.List = append(out.List, v.List...)
		out.Dict = append(out.Dict, v.Dict...)
		out.ListDict = append(out.ListDict, v.ListDict...)
		out.IntList = append(out.IntList, v.IntList...)
		if v.Str != "" || v.Int != 0 || v.Bool {
			out.Str, out.Int, out.Bool = v.Str, v.Int, v.Bool
		}
	}
	return out
}

func superset(a, b []string) bool {
	if len(a) <= len(b) {
		return false
	}
	have := map[string]bool{}
	for _, x := range a {
		have[x] = true
	}
	for _, x := range b {
		if !have[x] {
			return false
		}
	}
	return true
}

// readBazelrc: the workspace .bazelrc's build/common options as Bazel applies them to a build with no --config on the
// command line: the unconditional lines in order (import and try-import followed), each --config=X expanded in place
// (the lines common:X and build:X), and with --enable_platform_specific_config the config linux after them.
func readBazelrc(ws string) {
	repo := moduleRepoMapping(ws)
	type rcLine struct {
		cfg  string
		args []string
	}
	var lines []rcLine
	var read func(path string, must bool)
	read = func(path string, must bool) {
		b, err := os.ReadFile(path)
		if err != nil {
			if must {
				qfail("%v", err)
			}
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			if f[0] == "import" || f[0] == "try-import" {
				if len(f) > 1 {
					read(strings.ReplaceAll(f[1], "%workspace%", ws), f[0] == "import")
				}
				continue
			}
			cmd, cfg, _ := strings.Cut(f[0], ":")
			if cmd != "build" && cmd != "common" {
				continue
			}
			lines = append(lines, rcLine{cfg, f[1:]})
		}
	}
	read(filepath.Join(ws, ".bazelrc"), false)
	expanding := map[string]bool{}
	var apply func(args []string)
	expand := func(cfg string) {
		if expanding[cfg] {
			qfail(".bazelrc: --config=%s expands itself", cfg)
		}
		expanding[cfg] = true
		for _, l := range lines {
			if l.cfg == cfg {
				apply(l.args)
			}
		}
		expanding[cfg] = false
	}
	apply = func(f []string) {
		for i := 0; i < len(f); i++ {
			o := f[i]
			if !strings.HasPrefix(o, "-") {
				continue
			}
			o = strings.TrimLeft(o, "-")
			k, v, hasV := strings.Cut(o, "=")
			starlark := strings.HasPrefix(k, "@") || strings.HasPrefix(k, "//") || strings.HasPrefix(k, "no@") || strings.HasPrefix(k, "no//")
			if !hasV && !starlark && i+1 < len(f) && !strings.HasPrefix(f[i+1], "-") && k != "stamp" && k != "nostamp" {
				v = f[i+1]
				i++
				hasV = true
			}
			switch {
			case starlark:
				if !hasV {
					v = "true"
					if strings.HasPrefix(k, "no") {
						k, v = k[2:], "false"
					}
				}
				if cl := canonRepo(k, repo); cl != "" {
					rcFlags[cl] = v
				}
			case k == "config":
				expand(v)
			case k == "define":
				dk, dv, _ := strings.Cut(v, "=")
				rcDefines[dk] = dv
			case k == "c":
				rcNative["compilation_mode"] = v
			case k == "stamp" || k == "nostamp":
				rcNative["stamp"] = fmt.Sprint(k == "stamp")
			default:
				if hasV {
					rcNative[k] = v
				} else if strings.HasPrefix(k, "no") {
					rcNative[k[2:]] = "false"
				} else {
					rcNative[k] = "true"
				}
			}
		}
	}
	expand("")
	if boolStr(rcNative["enable_platform_specific_config"]) == "true" {
		expand("linux")
	}
	if p := rcNative["platforms"]; p != "" {
		qfail(".bazelrc sets --platforms=%s: only the host platform (linux/x86_64) is handled", p)
	}
}

// moduleRepoMapping: the main module's apparent repository names (bazel_dep repo_name or name) -> canonical names.
var moduleMapping map[string]string

func moduleRepoMapping(ws string) map[string]string {
	if moduleMapping != nil {
		return moduleMapping
	}
	m := map[string]string{}
	re := regexp.MustCompile(`(?s)\bbazel_dep\((.*?)\)`)
	modRe := regexp.MustCompile(`(?s)^\s*module\((.*?)\)`)
	incRe := regexp.MustCompile(`(?m)^\s*include\("//([^:"]*):([^"]+)"\)`)
	nameRe := regexp.MustCompile(`\bname\s*=\s*"([^"]+)"`)
	repoRe := regexp.MustCompile(`\brepo_name\s*=\s*"([^"]+)"`)
	var read func(path string)
	read = func(path string) { // (MODULE.bazel and the files it include()s)
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if x := modRe.FindStringSubmatch(string(b)); x != nil {
			if r := repoRe.FindStringSubmatch(x[1]); r != nil {
				m[r[1]] = "" // (the main module's own repo_name)
			}
		}
		for _, x := range re.FindAllStringSubmatch(string(b), -1) {
			n := nameRe.FindStringSubmatch(x[1])
			if n == nil {
				continue
			}
			app := n[1]
			if r := repoRe.FindStringSubmatch(x[1]); r != nil {
				app = r[1]
			}
			m[app] = n[1] + "+"
		}
		for _, x := range incRe.FindAllStringSubmatch(string(b), -1) {
			read(filepath.Join(ws, x[1], x[2]))
		}
	}
	read(filepath.Join(ws, "MODULE.bazel"))
	moduleMapping = m
	return m
}

func canonRepo(l string, m map[string]string) string {
	if strings.HasPrefix(l, "//") {
		return "@@" + l
	}
	if strings.HasPrefix(l, "@@") {
		return l
	}
	repo, rest, _ := strings.Cut(l[1:], "//")
	if c, ok := m[repo]; ok {
		return "@@" + c + "//" + rest
	}
	// (not a bazel_dep: an extension's repository; kept apart, and an error only if a flag of the same package and
	// name is asked for: flagValue)
	rcUnmapped["//"+rest] = l
	return ""
}
