// fc: the campaign's compiler (PLAN.md, campaign 1). It compiles whole Go packages from source to machine
// code without the stock compiler: go/types in front, its own lowering and code generator, its own x86
// encoder. Objects are written through cmd/internal/obj's object-file writer for now (the linker is still
// the stock one), which is why it is built inside the patched GOROOT's cmd tree (driver/mkfc.sh).
//
// Mixed mode (how single packages are tested): fc takes the stock compiler's own command line for one
// package, after the stock compiler has run, and replaces the object in the archive the stock compiler
// wrote (the export data stays: stock-compiled importers need it). Dependencies are type-checked from
// source; FC_PLAN names a `go list -json` stream that says where their sources are.
//
//	fc [compile flags] -o _pkg_.a -p path -importcfg cfg file.go...
//
// Exit status: 0 the archive now holds fc's object; 3 the package uses something fc does not handle yet
// (reasons on stderr, archive untouched); anything else is a failure.
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"cmd/internal/goobj"
	"cmd/internal/obj"
	"cmd/internal/objabi"
)

var valueFlags = map[string]bool{"o": true, "p": true, "importcfg": true, "trimpath": true, "symabis": true,
	"asmhdr": true, "lang": true, "goversion": true, "embedcfg": true, "c": true, "buildid": true, "D": true,
	"coveragecfg": true, "pgoprofile": true, "installsuffix": true, "spectre": true, "d": true, "linkobj": true,
	"json": true, "env": true, "memprofile": true, "cpuprofile": true, "I": true}

type options struct {
	out, path, importcfg, trimpath, symabis, lang string
	std                                           bool
	files                                         []string
}

func parseArgs(args []string) options {
	var o options
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			o.files = append(o.files, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		val := ""
		hasVal := false
		if j := strings.Index(name, "="); j >= 0 {
			name, val, hasVal = name[:j], name[j+1:], true
		}
		if valueFlags[name] && !hasVal {
			i++
			if i < len(args) {
				val = args[i]
			}
		}
		switch name {
		case "o":
			o.out = val
		case "p":
			o.path = val
		case "importcfg":
			o.importcfg = val
		case "trimpath":
			o.trimpath = val
		case "symabis":
			o.symabis = val
		case "lang":
			o.lang = val
		case "std":
			o.std = true
		}
	}
	return o
}

// readImportcfg returns the import map and the archive of each package.
func readImportcfg(path string) (importMap, files map[string]string) {
	importMap, files = map[string]string{}, map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		verb, rest, _ := strings.Cut(line, " ")
		a, b, ok := strings.Cut(rest, "=")
		if !ok {
			continue
		}
		switch verb {
		case "importmap":
			importMap[a] = b
		case "packagefile":
			files[a] = b
		}
	}
	return
}

// ---- archives

type arMember struct {
	name string
	hdr  []byte
	data []byte
}

func readArchive(path string) []arMember {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("!<arch>\n")) {
		log.Fatalf("fc: %s is not an archive", path)
	}
	var ms []arMember
	off := 8
	for off+60 <= len(data) {
		hdr := data[off : off+60]
		name := strings.TrimSpace(string(hdr[:16]))
		var size int
		fmt.Sscanf(strings.TrimSpace(string(hdr[48:58])), "%d", &size)
		ms = append(ms, arMember{name, hdr, data[off+60 : off+60+size]})
		off += 60 + size
		if off&1 != 0 {
			off++
		}
	}
	return ms
}

func writeArchive(path string, ms []arMember) {
	var b bytes.Buffer
	b.WriteString("!<arch>\n")
	for _, m := range ms {
		hdr := append([]byte(nil), m.hdr...)
		copy(hdr[48:58], fmt.Sprintf("%-10d", len(m.data)))
		b.Write(hdr)
		b.Write(m.data)
		if b.Len()&1 != 0 {
			b.WriteByte(0)
		}
	}
	if err := os.WriteFile(path, b.Bytes(), 0o666); err != nil {
		log.Fatal(err)
	}
}

// stockDef is a symbol the stock object defines in its indexed block: importers compiled by the stock
// compiler refer to it by that index, not by name.
type stockDef struct {
	name string
	abi  uint16
	text bool
}

// definedSyms lists the symbols an archive's Go object defines.
func definedSyms(path string) map[string]bool {
	out, _ := definedSymsOrder(path)
	return out
}

func definedSymsOrder(path string) (map[string]bool, []stockDef) {
	out := map[string]bool{}
	var defs []stockDef
	for _, m := range readArchive(path) {
		if m.name != "_go_.o" {
			continue
		}
		data := m.data
		// the object proper starts after the "go object ...\n!\n" header
		i := bytes.Index(data, []byte("\n!\n"))
		if i < 0 {
			continue
		}
		r := goobj.NewReaderFromBytes(data[i+3:], false)
		if r == nil {
			continue
		}
		n := r.NSym() + r.NHashed64def() + r.NHasheddef() + r.NNonpkgdef()
		for j := 0; j < n; j++ {
			sym := r.Sym(uint32(j))
			out[sym.Name(r)] = true
			if j < r.NSym() {
				defs = append(defs, stockDef{sym.Name(r), sym.ABI(), objabi.SymKind(sym.Type()).IsText()})
			}
		}
	}
	return out, defs
}

// alignWithStock orders the object's symbols so that every symbol the stock object defines by index has
// the same index here (mixed mode: stock-compiled importers use those indices). A stock symbol fc does not
// define gets a placeholder; a symbol only fc defines is referenced by name.
func (c *compiler) alignWithStock(defs []stockDef) (missing []string) {
	type key struct {
		name string
		abi  uint16
	}
	indexed := func(s *obj.LSym) bool {
		return !s.DuplicateOK() && !s.ContentAddressable() && !s.Static() && s.Pkg != "_"
	}
	mine := map[key]*obj.LSym{}
	for _, list := range [][]*obj.LSym{c.ctxt.Text, c.ctxt.Data} {
		for _, s := range list {
			if indexed(s) {
				mine[key{s.Name, uint16(s.ABI())}] = s
			}
		}
	}
	used := map[*obj.LSym]bool{}
	var text, data []*obj.LSym
	for _, d := range defs {
		if d.name == "" {
			continue // function info and the like: after everything else, never referenced from outside
		}
		s := mine[key{d.name, d.abi}]
		if s != nil && s.Type.IsText() != d.text {
			log.Fatalf("fc: %s is text in one object and data in the other", d.name)
		}
		if s == nil {
			missing = append(missing, d.name)
			if d.abi == goobj.SymABIstatic {
				s = c.ctxt.LookupStatic(d.name + "..fcplaceholder")
			} else {
				s = c.ctxt.LookupABIInit(d.name, obj.ABI(d.abi), func(s *obj.LSym) { s.Pkg = objabi.PathToPrefix(c.path) })
			}
			if s.OnList() {
				log.Fatalf("fc: %s: defined, but not as an indexed symbol", d.name)
			}
			if d.text {
				c.placeholderFunc(s)
			} else {
				s.Type = objabi.SNOPTRBSS
				s.Set(obj.AttrOnList, true)
			}
		}
		used[s] = true
		if d.text {
			text = append(text, s)
		} else {
			data = append(data, s)
		}
	}
	for _, s := range c.ctxt.Text {
		if !used[s] {
			if indexed(s) {
				s.Pkg = "_"
			}
			text = append(text, s)
		}
	}
	for _, s := range c.ctxt.Data {
		if !used[s] {
			data = append(data, s)
		}
	}
	c.ctxt.Text, c.ctxt.Data = text, data
	return missing
}

func main() {
	log.SetFlags(0)
	if len(os.Args) > 1 && os.Args[1] == "build" {
		buildMode(os.Args[2:])
		return
	}
	o := parseArgs(os.Args[1:])
	if o.out == "" || o.path == "" || len(o.files) == 0 {
		log.Fatal("usage: fc [compile flags] -o _pkg_.a -p path -importcfg cfg file.go...")
	}
	plan := os.Getenv("FC_PLAN")
	if plan == "" {
		log.Fatal("fc: FC_PLAN is not set")
	}
	pr := newProgram()
	if err := pr.readPlan(plan); err != nil {
		log.Fatal(err)
	}
	importMap, archives := map[string]string{}, map[string]string{}
	if o.importcfg != "" {
		importMap, archives = readImportcfg(o.importcfg)
	}
	wd, _ := os.Getwd()
	target := &Pkg{ImportPath: o.path, Dir: wd, ImportMap: importMap, Standard: o.std}
	if old := pr.byPath[o.path]; old != nil {
		target.Module = old.Module
	}
	for _, f := range o.files {
		if !filepath.IsAbs(f) {
			f = filepath.Join(wd, f)
		}
		target.GoFiles = append(target.GoFiles, f)
	}
	pr.byPath[o.path] = target
	if err := pr.check(target); err != nil {
		log.Fatalf("fc: type check: %v", err)
	}
	for _, need := range []string{"runtime"} {
		if p := pr.byPath[need]; p != nil && p != target {
			if err := pr.check(p); err != nil {
				log.Fatalf("fc: type check of %s: %v", need, err)
			}
		}
	}
	c := newCompiler(pr, target, o.path, o.trimpath)
	c.readSymABIs(o.symabis)
	stock, stockDefs := definedSymsOrder(o.out)
	if m := os.Getenv("FC_TYPEDIFF"); m != "" {
		typeDiff(pr, target, o.path, o.trimpath, o.out) // rtype_diff.go
		if m == "only" {
			os.Exit(3)
		}
	}
	pp := objabi.PathToPrefix(o.path)
	c.forceInit = stock[pp+"..inittask"]
	depInit := map[*Pkg]bool{}
	c.depInit = func(d *Pkg) bool {
		v, ok := depInit[d]
		if !ok {
			if a := archives[d.ImportPath]; a != "" {
				v = definedSyms(a)[objabi.PathToPrefix(d.ImportPath)+"..inittask"]
			}
			depInit[d] = v
		}
		return v
	}
	errs := c.compilePackage()
	if len(errs) == 0 && c.hasInit && !c.forceInit {
		errs = append(errs, "initialization the stock compiler does at compile time")
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "fc: %s: %s\n", o.path, e)
		}
		os.Exit(3)
	}
	c.ctxt.Imports = stockImports(o.out)
	missing := c.alignWithStock(stockDefs)
	if os.Getenv("FC_SYMDIFF") != "" {
		for _, m := range missing {
			fmt.Fprintf(os.Stderr, "fc: %s: placeholder for %s\n", o.path, m)
		}
	}
	tmp := o.out + ".fc.o"
	c.writeObject(tmp)
	objData, err := os.ReadFile(tmp)
	if err != nil {
		log.Fatal(err)
	}
	os.Remove(tmp)
	ms := readArchive(o.out)
	found := false
	for i := range ms {
		if ms[i].name == "_go_.o" {
			ms[i].data = objData
			found = true
		}
	}
	if !found {
		log.Fatalf("fc: %s has no _go_.o", o.out)
	}
	writeArchive(o.out, ms)
	if os.Getenv("FC_SYMDIFF") != "" {
		ours := definedSyms(o.out)
		for s := range stock {
			if !ours[s] && !strings.HasPrefix(s, "go:") && !strings.HasPrefix(s, "gclocals·") && !strings.Contains(s, "..stmp_") && !strings.Contains(s, "..dict.") && s != "" {
				fmt.Fprintf(os.Stderr, "fc: %s: stock defines %s, fc does not\n", o.path, s)
			}
		}
	}
}

// stockImports: the packages the stock object says it imports (the linker loads them from this list).
func stockImports(path string) []goobj.ImportedPkg {
	for _, m := range readArchive(path) {
		if m.name != "_go_.o" {
			continue
		}
		i := bytes.Index(m.data, []byte("\n!\n"))
		if i < 0 {
			continue
		}
		if r := goobj.NewReaderFromBytes(m.data[i+3:], false); r != nil {
			return r.Autolib()
		}
	}
	return nil
}
