// query.go: the second front end. Instead of Bazel's action graph (`bazel aquery`: loading, analysis, actions), it
// reads the target graph of the loading phase alone (`bazel query --output=streamed_proto --consistent_labels
// 'deps(LABEL)'`: BUILD files and macros evaluated, no configured targets, no toolchain resolution, no actions) and
// derives from the rules' attributes the actions the aquery front end reads: GoCompilePkg (its arguments), GoLink,
// and the actions that write generated sources and the tools they run (GoProtocGen, GenProtoDescriptorSet, Genrule,
// rules of a workspace's own), with their command lines and environments, as the rules' Starlark would write them
// (rules_go 0.63's go/private, proto/; protobuf 34's bazel/; Bazel 9's genrule). Everything downstream (generation,
// cgo, the plan for fc) is main.go's, shared with the aquery front end.
//
// Toolchains are not resolved by a query. What bzfc needs of them: the Go SDK, nothing (the standard library is
// fc's, the builder is never run); the C toolchain, assumed present (cgo on unless pure: the four rows' Bazel builds
// have one); protoc, the proto_toolchain rule the query must contain (cold.sh adds the prebuilt protoc toolchain's
// target to the query expression: it is what protobuf's toolchain resolution picks for linux_x86_64 when
// prefer_prebuilt_protoc is on, its default; a second proto_toolchain in the graph is an error).
//
// A rule kind the closure needs that is not handled here stops the build with its kind and label.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	flagQuery    = flag.String("query", "", "bazel query --output=streamed_proto --consistent_labels 'deps(LABEL)' output (instead of -aquery)")
	flagPlanOnly = flag.Bool("planonly", false, "generate, build the tools, write the binary's plan (WORK/builds/*/plan.json), do not build the binary (tools/bazel/plandiff.sh)")
	flagQDump    = flag.String("qdump", "", "with -query: only print the rules whose label or class matches this regexp, with their explicit attributes (QDUMP_ALL=1: all)")
)

var (
	qTargets []*qTarget
	qRules   = map[string]*qRule{} // canonical label -> rule
	qSrc     = map[string]bool{}   // source files
	qGen     = map[string]string{} // generated file -> its rule
	qKinds   = map[string]int{}    // rule kinds handled (rules visited, per configuration)
	rulesGo  string                // rules_go's canonical repository name
	artIDs   = map[string]int{}    // (synthesized artifacts for inputs())
)

func qfail(format string, a ...any) { log.Fatalf("query: "+format, a...) }

func loadQuery() {
	f, err := os.Open(*flagQuery)
	if err != nil {
		log.Fatal(err)
	}
	qTargets, err = readQueryStream(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range qTargets {
		switch {
		case t.Rule != nil:
			t.Rule.Name = canon(t.Rule.Name)
			qRules[t.Rule.Name] = t.Rule
		case t.Type == 2:
			qSrc[canon(t.Name)] = true
		case t.Type == 3:
			qGen[canon(t.Name)] = canon(t.GenRule)
		}
	}
}

// canon: a label as @@repo//pkg:name.
func canon(l string) string {
	if strings.HasPrefix(l, "//") {
		l = "@@" + l
	} else if strings.HasPrefix(l, "@") && !strings.HasPrefix(l, "@@") {
		l = "@" + l // (with --consistent_labels every label is canonical; apparent ones are not expected)
	}
	if !strings.Contains(l, ":") {
		_, pkg, _ := strings.Cut(l, "//")
		l += ":" + path.Base(pkg)
	}
	return l
}

func splitLabel(l string) (repo, pkg, name string) {
	l = strings.TrimPrefix(l, "@@")
	repo, rest, _ := strings.Cut(l, "//")
	pkg, name, _ = strings.Cut(rest, ":")
	return
}

// repoDir: the exec path of a label's package directory.
func pkgDir(repo, pkg string) string {
	d := pkg
	if repo != "" {
		d = path.Join("external", repo, pkg)
	}
	return d
}

func srcPath(l string) string {
	repo, pkg, name := splitLabel(l)
	return path.Join(pkgDir(repo, pkg), name)
}

// outPath: a file a rule declares (name relative to the rule's package) in c.
func outPath(c *qcfg, l, rel string) string {
	repo, pkg, _ := splitLabel(l)
	return path.Join(c.bin(), pkgDir(repo, pkg), rel)
}

func labelName(l string) string { _, _, n := splitLabel(l); return n }

func rule(l string, from string) *qRule {
	r := qRules[canon(l)]
	if r == nil {
		qfail("%s: %s is not a rule in the query's graph", from, l)
	}
	return r
}

// readQuery: the query front end: the graph read, the actions derived (into aq.Actions, byOutput, and the
// inputs of each action as one depset).
func readQuery() {
	loadQuery()
	readBazelrc(*flagWS)
	newCfgs()
	label := canon(*flagLabel)
	r := rule(label, "-label")
	if r.Class != "go_binary" {
		qfail("%s is a %s, not a go_binary", label, r.Class)
	}
	rulesGo, _, _ = splitLabel(canon(r.Attrs["$go_context_data"].Str))
	goBinary(r, cfgTarget)
	paths = map[int]string{}
	for p, id := range artIDs {
		paths[id] = p
	}
	var ks []string
	for k := range qKinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var b strings.Builder
	for _, k := range ks {
		fmt.Fprintf(&b, " %s=%d", k, qKinds[k])
	}
	stage("query: %d targets, %d rules; actions derived from:%s", len(qTargets), len(qRules), b.String())
}

// newAction: a synthesized action with its outputs indexed and its inputs as one depset.
func newAction(mnemonic, label string, c *qcfg, outs, ins []string) *action {
	a := &action{Mnemonic: mnemonic, label: trimRepo(label), tool: c.exec, outs: outs}
	for _, o := range outs {
		if p := byOutput[o]; p != nil {
			qfail("%s and %s both write %s", p.label, a.label, o)
		}
		byOutput[o] = a
	}
	if len(ins) > 0 {
		id := len(depsets) + 1
		var direct []int
		for _, p := range ins {
			x, ok := artIDs[p]
			if !ok {
				x = len(artIDs) + 1
				artIDs[p] = x
			}
			direct = append(direct, x)
		}
		depsets[id] = [2][]int{direct, nil}
		a.InputDepSetIDs = []int{id}
	}
	aq.Actions = append(aq.Actions, a)
	return a
}

func init() { depsets = map[int][2][]int{} }

func (a *action) setArgv(argv []string, env [][2]string) {
	a.Arguments = argv
	a.args = parseArgs(argv)
	for _, e := range env {
		a.Env = append(a.Env, kv{e[0], e[1]})
	}
}

type memoKey struct {
	l string
	c *qcfg
}

var filesMemo = map[memoKey][]string{}

// files: the default outputs of a label (or the source file it is) in c, the actions writing them derived.
func qfiles(l string, c *qcfg, from string) []string {
	l = canon(l)
	if qSrc[l] {
		return []string{srcPath(l)}
	}
	if g, ok := qGen[l]; ok {
		ensureRule(g, c)
		return []string{outPath(c, l, labelName(l))}
	}
	k := memoKey{l, c}
	if f, ok := filesMemo[k]; ok {
		return f
	}
	r := rule(l, from)
	var out []string
	switch r.Class {
	case "alias":
		out = qfiles(c.resolve(r, "actual").Str, c, l)
	case "filegroup":
		seen := map[string]bool{}
		for _, s := range c.resolve(r, "srcs").List {
			for _, f := range qfiles(s, c, l) {
				if !seen[f] {
					seen[f] = true
					out = append(out, f)
				}
			}
		}
	case "proto_library":
		out = []string{protoInfo(r, c).descSet}
	case "go_binary":
		out = []string{goBinary(r, c).outs[0]}
	default:
		ensureRule(l, c)
		for _, o := range r.Outputs {
			out = append(out, outPath(c, o, labelName(o)))
		}
		if len(out) == 0 {
			qfail("%s: files of a %s (%s) are not known", from, r.Class, l)
		}
	}
	filesMemo[k] = out
	return out
}

func file1(l string, c *qcfg, from string) string {
	f := qfiles(l, c, from)
	if len(f) != 1 {
		qfail("%s: %s is %d files, one expected", from, l, len(f))
	}
	return f[0]
}

// exe: the executable of a tool label in c (its action derived).
func exe(l string, c *qcfg, from string) string {
	l = canon(l)
	if qSrc[l] {
		return srcPath(l)
	}
	if _, ok := qGen[l]; ok {
		return file1(l, c, from)
	}
	r := rule(l, from)
	switch r.Class {
	case "alias":
		return exe(c.resolve(r, "actual").Str, c, l)
	case "go_binary":
		return goBinary(r, c).outs[0]
	case "go_reset_target":
		return goResetTarget(r, c)
	case "filegroup", "genrule":
		return file1(l, c, from)
	}
	qfail("%s: the executable of a %s (%s) is not known", from, r.Class, l)
	return ""
}

var ensured = map[memoKey]bool{}

// ensureRule: the actions of a rule whose outputs are generated files, derived once per configuration.
func ensureRule(l string, c *qcfg) {
	k := memoKey{l, c}
	if ensured[k] {
		return
	}
	ensured[k] = true
	r := rule(l, "generated file")
	qKinds[r.Class]++
	switch r.Class {
	case "genrule":
		genrule(r, c)
	case "_go_yacc":
		goYacc(r, c)
	case "generate_tables":
		generateTables(r, c)
	default:
		qfail("rule kind %s (%s) is not handled by the query front end", r.Class, l)
	}
}

// goResetTarget: rules_go's go_reset_target: the dep in the tool transition (Go settings to their defaults),
// its executable symlinked under <name>/.
func goResetTarget(r *qRule, c *qcfg) string {
	k := memoKey{r.Name, c}
	if o, ok := resetMemo[k]; ok {
		return o
	}
	qKinds[r.Class]++
	dep := exe(c.resolve(r, "dep").Str, c.root(), r.Name)
	out := outPath(c, r.Name, path.Join(labelName(r.Name), path.Base(dep)))
	newAction("ExecutableSymlink", r.Name, c, []string{out}, []string{dep})
	resetMemo[k] = out
	return out
}

var resetMemo = map[memoKey]string{}

func (c *qcfg) root() *qcfg {
	if c.exec {
		return cfgExec
	}
	return cfgTarget
}

// qdump: the -qdump mode.
func qdump() {
	re := regexp.MustCompile(*flagQDump)
	kinds := map[string]int{}
	for _, t := range qTargets {
		if t.Rule == nil {
			continue
		}
		kinds[t.Rule.Class]++
		if !re.MatchString(t.Rule.Name) && !re.MatchString(t.Rule.Class) {
			continue
		}
		fmt.Printf("== %s %s %s\n", t.Rule.Class, t.Rule.Name, t.Rule.ClassKey)
		var names []string
		for n := range t.Rule.Attrs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			a := t.Rule.Attrs[n]
			if !a.Explicit && os.Getenv("QDUMP_ALL") == "" {
				continue
			}
			var b []byte
			if a.Sel != nil {
				b, _ = json.Marshal(a.Sel)
			} else {
				b, _ = json.Marshal(a.qVal)
			}
			fmt.Printf("  %s %d %s\n", n, a.Type, b)
		}
	}
	var ks []string
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("kind %s %d\n", k, kinds[k])
	}
}
