// rtype_diff.go: FC_TYPEDIFF, the oracle for the descriptors. For the package being compiled, every symbol
// named type:*, go:itab.* or runtime.gcbits.* that the stock compiler's object defines is compared with the
// one fc writes for the same name: kind, size, alignment, flags, data, relocations. The descriptors are
// asked for by name: every type the package's type information mentions is mapped from its link name, so a
// stock descriptor that names one of them is forced out of fc, whatever fc's code generator would ask for.
//
//	FC_TYPEDIFF=1     compare, then compile as usual
//	FC_TYPEDIFF=only  compare, then leave the package to the stock compiler (exit 3)
//	FC_TYPEDIFF_OUT   the report (appended; default stderr): tab-separated lines
//	    <pkg> SUMMARY stock=N same=N diff=N missing=N extra=N
//	    <pkg> DIFF <symbol> <what differs>
//	    <pkg> MISSING <symbol> <why fc has no such symbol>
package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"go/types"
	"os"
	"sort"
	"strconv"
	"strings"

	"cmd/internal/goobj"
	"cmd/internal/objabi"
)

// forEachType calls visit for every type the package's type information mentions, and their parts.
func (c *compiler) forEachType(visit func(types.Type)) {
	seen := map[types.Type]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		if t == nil {
			return
		}
		t = types.Unalias(t)
		if seen[t] {
			return
		}
		seen[t] = true
		switch u := t.(type) {
		case *types.Tuple:
			for i := 0; i < u.Len(); i++ {
				walk(u.At(i).Type())
			}
			return
		case *types.TypeParam:
			return
		case *types.Basic:
			if u.Info()&types.IsUntyped != 0 || u.Kind() == types.Invalid {
				return
			}
		}
		visit(t)
		switch u := t.(type) {
		case *types.Named:
			for i := 0; i < u.TypeArgs().Len(); i++ {
				walk(u.TypeArgs().At(i))
			}
			walk(u.Underlying())
			for i := 0; i < u.NumMethods(); i++ {
				walk(u.Method(i).Type())
			}
		case *types.Pointer:
			walk(u.Elem())
		case *types.Slice:
			walk(u.Elem())
		case *types.Array:
			walk(u.Elem())
		case *types.Chan:
			walk(u.Elem())
		case *types.Map:
			walk(u.Key())
			walk(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				walk(u.Field(i).Type())
			}
		case *types.Signature:
			walk(u.Params())
			walk(u.Results())
			if u.Variadic() {
				if s, ok := u.Params().At(u.Params().Len() - 1).Type().Underlying().(*types.Slice); ok {
					walk(s.Elem())
				}
			}
		case *types.Interface:
			for i := 0; i < u.NumMethods(); i++ {
				walk(u.Method(i).Type())
			}
		}
	}
	for _, info := range c.pkg.infos() {
		for _, tv := range info.Types {
			walk(tv.Type)
		}
		for _, o := range info.Defs {
			if o != nil {
				walk(o.Type())
			}
		}
		for _, o := range info.Uses {
			walk(o.Type())
		}
		for _, in := range info.Instances {
			walk(in.Type)
			for i := 0; i < in.TypeArgs.Len(); i++ {
				walk(in.TypeArgs.At(i))
			}
		}
		for _, o := range info.Implicits {
			walk(o.Type())
		}
		for _, s := range info.Selections {
			walk(s.Recv())
			walk(s.Type())
		}
	}
}

// ---- reading an object

type diffRel struct {
	off    int32
	siz    uint8
	typ    objabi.RelocType
	add    int64
	target string
}

type diffSym struct {
	name   string
	kind   objabi.SymKind
	flag   uint8
	flag2  uint8
	size   uint32
	align  uint32
	hashed bool
	data   []byte
	relocs []diffRel
}

// goObject returns the Go object of an archive (or a bare object file).
func goObject(data []byte) []byte {
	i := bytes.Index(data, []byte("\n!\n"))
	if i < 0 {
		return nil
	}
	return data[i+3:]
}

// readDiffSyms reads the data symbols an object defines, by name. Name data whose name is only a counter
// is known by its content.
func readDiffSyms(obj []byte) map[string]*diffSym {
	r := goobj.NewReaderFromBytes(obj, false)
	if r == nil {
		return nil
	}
	refNames := map[goobj.SymRef]string{}
	for i := 0; i < r.NRefName(); i++ {
		rn := r.RefName(i)
		refNames[rn.Sym()] = rn.Name(r)
	}
	nsym, n64, nh := uint32(r.NSym()), uint32(r.NHashed64def()), uint32(r.NHasheddef())
	ndef := nsym + n64 + nh + uint32(r.NNonpkgdef())
	memo := map[uint32]string{}
	var content func(i uint32) string
	var resolve func(s goobj.SymRef) string
	resolve = func(s goobj.SymRef) string {
		var i uint32
		switch s.PkgIdx {
		case goobj.PkgIdxInvalid:
			return ""
		case goobj.PkgIdxHashed64:
			i = s.SymIdx + nsym
		case goobj.PkgIdxHashed:
			i = s.SymIdx + nsym + n64
		case goobj.PkgIdxNone:
			i = s.SymIdx + nsym + n64 + nh
		case goobj.PkgIdxBuiltin:
			name, _ := goobj.BuiltinName(int(s.SymIdx))
			return name
		case goobj.PkgIdxSelf:
			i = s.SymIdx
		default:
			return refNames[s]
		}
		name := r.Sym(i).Name(r)
		if i < ndef && strings.HasPrefix(name, "type:.namedata.") {
			return content(i)
		}
		return name
	}
	rels := func(i uint32) []diffRel {
		rs := r.Relocs(i)
		out := make([]diffRel, len(rs))
		for j := range rs {
			out[j] = diffRel{rs[j].Off(), rs[j].Siz(), objabi.RelocType(rs[j].Type()), rs[j].Add(), resolve(rs[j].Sym())}
		}
		sort.SliceStable(out, func(a, b int) bool {
			if out[a].off != out[b].off {
				return out[a].off < out[b].off
			}
			if out[a].typ != out[b].typ {
				return out[a].typ < out[b].typ
			}
			return out[a].target < out[b].target
		})
		return out
	}
	content = func(i uint32) string {
		if s, ok := memo[i]; ok {
			return s
		}
		memo[i] = "<cycle>"
		var b strings.Builder
		fmt.Fprintf(&b, "<name %q", r.Data(i))
		for _, rel := range rels(i) {
			fmt.Fprintf(&b, " %d:%v:%s+%d", rel.off, rel.typ, rel.target, rel.add)
		}
		b.WriteByte('>')
		memo[i] = b.String()
		return memo[i]
	}
	out := map[string]*diffSym{}
	for i := uint32(0); i < ndef; i++ {
		s := r.Sym(i)
		name := s.Name(r)
		k := objabi.SymKind(s.Type())
		if name == "" || k.IsText() {
			continue
		}
		out[name] = &diffSym{name: name, kind: k, flag: s.Flag(), flag2: s.Flag2(), size: s.Siz(), align: s.Align(),
			hashed: i >= nsym && i < nsym+n64+nh, data: r.Data(i), relocs: rels(i)}
	}
	return out
}

// diffWanted: is the symbol one the oracle compares?
func diffWanted(name string) bool {
	if strings.HasPrefix(name, "type:.namedata.") || strings.HasPrefix(name, "type:.importpath.") {
		// compared through the symbols that point to them: the names of some are only counters
		return false
	}
	for _, suffix := range []string{".arginfo0", ".arginfo1", ".argliveinfo", ".args_stackmap", ".stkobj", ".opendefer", ".wrapinfo"} {
		if strings.HasSuffix(name, suffix) {
			return false // belongs to the code of an equality or hash function
		}
	}
	return strings.HasPrefix(name, "type:") || strings.HasPrefix(name, "go:itab.") || strings.HasPrefix(name, "runtime.gcbits.")
}

func diffSyms(a, b *diffSym) string {
	var d []string
	if a.kind != b.kind {
		d = append(d, fmt.Sprintf("kind %v/%v", a.kind, b.kind))
	}
	if a.flag != b.flag {
		d = append(d, fmt.Sprintf("flags %#x/%#x", a.flag, b.flag))
	}
	if a.flag2 != b.flag2 {
		d = append(d, fmt.Sprintf("flags2 %#x/%#x", a.flag2, b.flag2))
	}
	if a.size != b.size {
		d = append(d, fmt.Sprintf("size %d/%d", a.size, b.size))
	}
	if a.align != b.align {
		d = append(d, fmt.Sprintf("align %d/%d", a.align, b.align))
	}
	if a.hashed != b.hashed {
		d = append(d, fmt.Sprintf("content-addressable %v/%v", a.hashed, b.hashed))
	}
	if !bytes.Equal(a.data, b.data) {
		n := min(len(a.data), len(b.data))
		i := 0
		for i < n && a.data[i] == b.data[i] {
			i++
		}
		lo := i &^ 7
		d = append(d, fmt.Sprintf("data (%d/%d bytes) at %d: %s/%s", len(a.data), len(b.data), i,
			hex.EncodeToString(a.data[min(lo, len(a.data)):min(lo+8, len(a.data))]), hex.EncodeToString(b.data[min(lo, len(b.data)):min(lo+8, len(b.data))])))
	}
	i, j := 0, 0
	nrel := 0
	for (i < len(a.relocs) || j < len(b.relocs)) && nrel < 4 {
		switch {
		case i < len(a.relocs) && j < len(b.relocs) && a.relocs[i] == b.relocs[j]:
			i++
			j++
		case j == len(b.relocs) || i < len(a.relocs) && a.relocs[i].off < b.relocs[j].off:
			d = append(d, fmt.Sprintf("reloc only stock %s", relString(a.relocs[i])))
			i++
			nrel++
		case i == len(a.relocs) || b.relocs[j].off < a.relocs[i].off:
			d = append(d, fmt.Sprintf("reloc only fc %s", relString(b.relocs[j])))
			j++
			nrel++
		default:
			d = append(d, fmt.Sprintf("reloc %s / %s", relString(a.relocs[i]), relString(b.relocs[j])))
			i++
			j++
			nrel++
		}
	}
	return strings.Join(d, "; ")
}

func relString(r diffRel) string {
	return fmt.Sprintf("%d:%d %v %s+%d", r.off, r.siz, r.typ, r.target, r.add)
}

// typeDiff compares the stock object in the archive with the descriptors of a fresh compiler for the same
// package, and writes the report.
func typeDiff(pr *Program, p *Pkg, path, trimpath, archive string) {
	var stockObj []byte
	for _, m := range readArchive(archive) {
		if m.name == "_go_.o" {
			stockObj = goObject(m.data)
		}
	}
	stock := readDiffSyms(stockObj)
	if stock == nil {
		fmt.Fprintf(os.Stderr, "fc: typediff: %s: no Go object in %s\n", path, archive)
		return
	}
	c := newCompiler(pr, p, path, trimpath)
	var report strings.Builder
	bails := map[string]string{}
	try := func(what string, f func()) {
		defer func() {
			if r := recover(); r != nil {
				if b, ok := r.(bail); ok {
					bails[what] = string(b)
					return
				}
				bails[what] = fmt.Sprintf("internal error: %v", r)
			}
		}()
		f()
	}
	// the declared types, as in a compile
	if path == "runtime" {
		try("basic types", c.writeBasicTypes)
	}
	for _, t := range c.declaredTypes() {
		try("type:"+t.String(), func() { c.typeSym(t) })
	}
	// the names of every type the package mentions
	byName := map[string]types.Type{}
	c.forEachType(func(t types.Type) {
		if mentionsTypeParamRT(t, nil) {
			return
		}
		try("name", func() {
			name := func() string {
				c.rtMu.Lock()
				defer c.rtMu.Unlock()
				c.rtInit()
				return c.typeSymName(t)
			}()
			if _, ok := byName[name]; !ok {
				byName[name] = t
			}
		})
	})
	delete(bails, "name")
	for _, b := range types.Typ {
		if b.Info()&types.IsUntyped == 0 && b.Kind() != types.Invalid {
			byName[basicName(b)] = b
		}
	}
	c.rtInit()
	var names []string
	for name := range stock {
		if diffWanted(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	// a name the package does not mention is parsed: types of other packages, instances of generic types,
	// and pointers, slices, arrays, maps, channels and functions of whatever can be found
	np := &nameParser{c: c, byName: byName, pkgs: map[string]*types.Package{}}
	for _, d := range pr.byPath {
		if d.tpkg != nil {
			np.pkgs[c.pkgPrefix(d.tpkg)] = d.tpkg
		}
	}
	derive := np.lookup
	// force the descriptors the stock object has: maps first (they bring their groups), then the rest
	force := func(maps bool) {
		for _, name := range names {
			if !strings.HasPrefix(name, "type:") {
				continue
			}
			t := derive(name[5:])
			if t == nil {
				continue
			}
			if _, isMap := t.Underlying().(*types.Map); isMap != maps {
				continue
			}
			try(name, func() { c.typeSym(t) })
		}
	}
	force(true)
	force(false)
	for _, name := range names {
		rest, ok := strings.CutPrefix(name, "go:itab.")
		if !ok {
			continue
		}
		for i := 0; i < len(rest); i++ {
			if rest[i] != ',' {
				continue
			}
			t, it := derive(rest[:i]), derive(rest[i+1:])
			if t != nil && it != nil {
				try(name, func() { c.itabSym(t, it) })
				break
			}
		}
	}
	// fc's object, read back the way the stock one was
	tmp, err := os.CreateTemp("", "fc-typediff-*.o")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fc: typediff: %v\n", err)
		return
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	c.writeObject(tmp.Name())
	data, _ := os.ReadFile(tmp.Name())
	ours := readDiffSyms(goObject(data))
	same, diff, missing, extra := 0, 0, 0, 0
	for _, name := range names {
		a, b := stock[name], ours[name]
		if b == nil {
			missing++
			why := bails[name]
			switch {
			case why != "":
			case strings.Contains(name, "go.shape."):
				why = "a shape type of the stock compiler's generics (fc instantiates fully)"
			case strings.HasPrefix(name, "type:.eqfunc.") || strings.HasPrefix(name, "type:.hashfunc.") || strings.HasPrefix(name, "type:.gcmask."):
				why = "belongs to a descriptor fc was not asked for"
			case strings.Contains(name, "struct { Size_ uintptr; PtrBytes") || strings.Contains(name, "struct { Name int32; Typ int32 }") || strings.Contains(name, "struct { Bytes *uint8 }"):
				why = "the stock compiler's own picture of the runtime's descriptors (rttype)"
			case strings.Contains(name, "struct { Typ uintptr;") || strings.Contains(name, "struct { Mask uintptr; Entries") || strings.Contains(name, "struct { Itab uintptr"):
				why = "a cache of the stock compiler's type switches and assertions"
			case strings.HasPrefix(name, "type:noalg."):
				why = "a compiler-made type (closure, defer or map internals) no type in the package maps to"
			case strings.HasPrefix(name, "go:itab."):
				why = "no type in the package's type information has the name of one of its sides"
			case strings.HasPrefix(name, "runtime.gcbits."):
				why = "the mask of a descriptor or variable fc was not asked for"
			default:
				why = "no type in the package's type information has this name"
			}
			fmt.Fprintf(&report, "%s\tMISSING\t%s\t%s\n", path, name, why)
			continue
		}
		if d := diffSyms(a, b); d != "" {
			diff++
			fmt.Fprintf(&report, "%s\tDIFF\t%s\t%s\n", path, name, d)
		} else {
			same++
		}
	}
	for name := range ours {
		if diffWanted(name) && stock[name] == nil {
			extra++
			if os.Getenv("FC_TYPEDIFF_EXTRA") != "" {
				fmt.Fprintf(&report, "%s\tEXTRA\t%s\n", path, name)
			}
		}
	}
	for what, why := range bails {
		if !diffWanted(what) {
			fmt.Fprintf(&report, "%s\tBAIL\t%s\t%s\n", path, what, why)
		}
	}
	fmt.Fprintf(&report, "%s\tSUMMARY\tstock=%d same=%d diff=%d missing=%d extra=%d\n", path, len(names), same, diff, missing, extra)
	if out := os.Getenv("FC_TYPEDIFF_OUT"); out != "" {
		f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
		if err == nil {
			f.WriteString(report.String())
			f.Close()
			return
		}
	}
	os.Stderr.WriteString(report.String())
}

// nameParser turns the link name of a type back into the type.
type nameParser struct {
	c      *compiler
	byName map[string]types.Type // the types the package mentions, and what was parsed before
	pkgs   map[string]*types.Package
	s      string
	pos    int
	fresh  bool // the outermost struct must be a new type, not one the package mentions
}

// lookup returns the type with the descriptor name (after "type:"), or nil.
func (p *nameParser) lookup(name string) (t types.Type) {
	if t, ok := p.byName[name]; ok {
		return t
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bail); !ok {
				panic(r)
			}
			t = nil
		}
		p.byName[name] = t
	}()
	switch {
	case strings.HasPrefix(name, "noalg.*"):
		if e := p.lookup("noalg." + name[7:]); e != nil {
			t = types.NewPointer(e)
		}
		return t
	case strings.HasPrefix(name, "noalg.["):
		// the array behind a slice literal and the like: an array type marked as having no algorithms
		if i := strings.Index(name, "]"); i > 0 {
			if n, err := strconv.ParseInt(name[7:i], 10, 64); err == nil {
				if e := p.lookup(name[i+1:]); e != nil {
					t = types.NewArray(e, n)
					p.c.rtNoalg[t] = true
				}
			}
		}
		return t
	case strings.HasPrefix(name, "noalg.struct {"):
		// a closure and the like: a struct of the compiler's own, marked as having no algorithms
		q := &nameParser{c: p.c, byName: p.byName, pkgs: p.pkgs, s: name[6:], fresh: true}
		t = q.typ()
		if t == nil || q.pos != len(q.s) {
			return nil
		}
		p.c.rtNoalg[t] = true
	case strings.HasPrefix(name, "noalg."):
		return nil
	default:
		q := &nameParser{c: p.c, byName: p.byName, pkgs: p.pkgs, s: name}
		t = q.typ()
		if t == nil || q.pos != len(name) {
			return nil
		}
	}
	// the parse stands only if the type's name is the name asked for
	p.c.rtMu.Lock()
	got := p.c.typeSymName(t)
	p.c.rtMu.Unlock()
	if got != name {
		return nil
	}
	return t
}

func (p *nameParser) eat(prefix string) bool {
	if strings.HasPrefix(p.s[p.pos:], prefix) {
		p.pos += len(prefix)
		return true
	}
	return false
}

// balanced returns the text from pos through the brace that closes the first one opened.
func (p *nameParser) balanced() string {
	depth := 0
	for i := p.pos; i < len(p.s); i++ {
		switch p.s[i] {
		case '"':
			q, err := strconv.QuotedPrefix(p.s[i:])
			if err != nil {
				return ""
			}
			i += len(q) - 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return p.s[p.pos : i+1]
			}
		}
	}
	return ""
}

func (p *nameParser) typ() types.Type {
	rest := p.s[p.pos:]
	switch {
	case p.eat("*"):
		if e := p.typ(); e != nil {
			return types.NewPointer(e)
		}
	case p.eat("[]"):
		if e := p.typ(); e != nil {
			return types.NewSlice(e)
		}
	case strings.HasPrefix(rest, "["):
		i := strings.Index(rest, "]")
		if i < 0 {
			return nil
		}
		n, err := strconv.ParseInt(rest[1:i], 10, 64)
		if err != nil {
			return nil
		}
		p.pos += i + 1
		if e := p.typ(); e != nil {
			return types.NewArray(e, n)
		}
	case p.eat("map["):
		k := p.typ()
		if k == nil || !p.eat("]") {
			return nil
		}
		if e := p.typ(); e != nil {
			return types.NewMap(k, e)
		}
	case p.eat("<-chan "):
		if e := p.typ(); e != nil {
			return types.NewChan(types.RecvOnly, e)
		}
	case p.eat("chan<- "):
		if e := p.typ(); e != nil {
			return types.NewChan(types.SendOnly, e)
		}
	case p.eat("chan "):
		paren := p.eat("(")
		e := p.typ()
		if e == nil || paren && !p.eat(")") {
			return nil
		}
		return types.NewChan(types.SendRecv, e)
	case p.eat("func("):
		if sig := p.sig(); sig != nil {
			return sig
		}
	case strings.HasPrefix(rest, "struct {"), strings.HasPrefix(rest, "interface {"):
		// a literal type: one the package mentions, or parsed
		fresh := p.fresh
		p.fresh = false
		lit := p.balanced()
		if t := p.byName[lit]; t != nil && lit != "" && !fresh {
			p.pos += len(lit)
			return t
		}
		if p.eat("struct {") {
			return p.structLit()
		}
		p.eat("interface {")
		return p.interfaceLit()
	default:
		return p.named()
	}
	return nil
}

func (p *nameParser) params() (vs []*types.Var, variadic, ok bool) {
	if p.eat(")") {
		return nil, false, true
	}
	for {
		dots := p.eat("...")
		t := p.typ()
		if t == nil {
			return nil, false, false
		}
		if dots {
			t, variadic = types.NewSlice(t), true
		}
		vs = append(vs, types.NewParam(0, nil, "", t))
		if p.eat(")") {
			return vs, variadic, true
		}
		if dots || !p.eat(", ") {
			return nil, false, false
		}
	}
}

// named parses a basic type or pkg.Name, pkg.Name[args].
func (p *nameParser) named() types.Type {
	start := p.pos
	for p.pos < len(p.s) && !strings.ContainsRune(",] )[;", rune(p.s[p.pos])) {
		p.pos++
	}
	name := p.s[start:p.pos]
	if name == "" {
		return nil
	}
	if !p.eat("[") {
		if t := p.byName[name]; t != nil {
			return t
		}
		if tn := p.typeName(name); tn != nil {
			if n, ok := tn.Type().(*types.Named); ok && n.TypeParams().Len() == 0 {
				return n
			}
		}
		return nil
	}
	var args []types.Type
	for {
		t := p.typ()
		if t == nil {
			return nil
		}
		args = append(args, t)
		if p.eat("]") {
			break
		}
		if !p.eat(",") {
			return nil
		}
	}
	tn := p.typeName(name)
	if tn == nil {
		return nil
	}
	n, ok := tn.Type().(*types.Named)
	if !ok || n.TypeParams().Len() != len(args) {
		return nil
	}
	t, err := types.Instantiate(p.c.pr.tctx, n, args, false)
	if err != nil {
		return nil
	}
	return t
}

// typeName finds the package-level type prefix.Name.
func (p *nameParser) typeName(name string) *types.TypeName {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return nil
	}
	pkg := p.pkgs[name[:i]]
	if pkg == nil {
		return nil
	}
	tn, _ := pkg.Scope().Lookup(name[i+1:]).(*types.TypeName)
	if tn == nil || tn.IsAlias() {
		return nil
	}
	return tn
}

// sig parses a signature after its opening parenthesis.
func (p *nameParser) sig() *types.Signature {
	params, variadic, ok := p.params()
	if !ok {
		return nil
	}
	var results []*types.Var
	if p.eat(" (") {
		results, _, ok = p.params()
		if !ok {
			return nil
		}
	} else if r := p.s[p.pos:]; len(r) > 1 && r[0] == ' ' && r[1] != '}' && r[1] != '"' {
		p.pos++
		t := p.typ()
		if t == nil {
			return nil
		}
		results = []*types.Var{types.NewParam(0, nil, "", t)}
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), types.NewTuple(results...), variadic)
}

// qualified splits "prefix.name" (the name of an unexported field or method) into package and name.
func (p *nameParser) qualified(name string) (*types.Package, string, bool) {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return nil, name, true
	}
	pkg := p.pkgs[name[:i]]
	return pkg, name[i+1:], pkg != nil
}

// structLit parses the fields of a struct after "struct {".
func (p *nameParser) structLit() types.Type {
	var fields []*types.Var
	var tags []string
	if p.eat("}") {
		return types.NewStruct(nil, nil)
	}
	for {
		if !p.eat(" ") {
			return nil
		}
		rest := p.s[p.pos:]
		i := strings.IndexAny(rest, " ;}")
		if i <= 0 {
			return nil
		}
		var f *types.Var
		if rest[i] == ' ' && i+1 < len(rest) && rest[i+1] != '}' && rest[i+1] != '"' {
			// name type, or name = type for an embedded field whose name is not its type's
			pkg, name, ok := p.qualified(rest[:i])
			if !ok {
				return nil
			}
			p.pos += i + 1
			embedded := p.eat("= ")
			t := p.typ()
			if t == nil {
				return nil
			}
			f = types.NewField(0, pkg, name, t, embedded)
		} else {
			t := p.typ()
			if t == nil {
				return nil
			}
			e := types.Unalias(t)
			if pt, ok := e.(*types.Pointer); ok {
				e = types.Unalias(pt.Elem())
			}
			n, ok := e.(*types.Named)
			if !ok {
				return nil
			}
			f = types.NewField(0, n.Obj().Pkg(), n.Obj().Name(), t, true)
		}
		tag := ""
		if strings.HasPrefix(p.s[p.pos:], " \"") {
			q, err := strconv.QuotedPrefix(p.s[p.pos+1:])
			if err != nil {
				return nil
			}
			tag, _ = strconv.Unquote(q)
			p.pos += 1 + len(q)
		}
		fields = append(fields, f)
		tags = append(tags, tag)
		if p.eat(" }") {
			return types.NewStruct(fields, tags)
		}
		if !p.eat(";") {
			return nil
		}
	}
}

// interfaceLit parses the methods of an interface after "interface {".
func (p *nameParser) interfaceLit() types.Type {
	var ms []*types.Func
	if p.eat("}") {
		return types.NewInterfaceType(nil, nil).Complete()
	}
	for {
		if !p.eat(" ") {
			return nil
		}
		i := strings.Index(p.s[p.pos:], "(")
		if i <= 0 {
			return nil
		}
		pkg, name, ok := p.qualified(p.s[p.pos : p.pos+i])
		if !ok {
			return nil
		}
		p.pos += i + 1
		sig := p.sig()
		if sig == nil {
			return nil
		}
		ms = append(ms, types.NewFunc(0, pkg, name, sig))
		if p.eat(" }") {
			return types.NewInterfaceType(ms, nil).Complete()
		}
		if !p.eat(";") {
			return nil
		}
	}
}
