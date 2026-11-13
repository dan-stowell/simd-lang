// abiwrap.go: wrappers between Go's two calling conventions. Assembly functions are ABI0 (everything on the
// stack); Go code calls and is called with ABIInternal (registers). For an assembly function declared in
// this package the compiler writes the ABIInternal entry; for a Go function that assembly refers to, the
// ABI0 entry. Which is which comes from the assembler's -symabis file.
package main

import (
	"encoding/binary"
	"go/ast"
	"go/types"
	"log"
	"os"
	"strings"

	"cmd/internal/obj"
	"cmd/internal/objabi"
)

type symABIs struct {
	defs         map[string]bool // defined in assembly as ABI0
	refs         map[string]bool // referenced from assembly as ABI0
	defsInternal map[string]bool // defined in assembly with the register ABI
}

func (c *compiler) readSymABIs(file string) {
	c.abis = &symABIs{defs: map[string]bool{}, refs: map[string]bool{}, defsInternal: map[string]bool{}}
	if file == "" {
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		log.Fatalf("fc: -symabis: %v", err)
	}
	prefix := objabi.PathToPrefix(c.path)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		sym := parts[1]
		if strings.HasPrefix(sym, `"".`) {
			sym = prefix + sym[2:]
		}
		if parts[2] != "ABI0" {
			if parts[0] == "def" && parts[2] == "ABIInternal" {
				c.abis.defsInternal[sym] = true
			}
			continue
		}
		switch parts[0] {
		case "def":
			c.abis.defs[sym] = true
		case "ref":
			c.abis.refs[sym] = true
		}
	}
}

func sigTypes(sig *types.Signature) (in, out []types.Type) {
	if r := sig.Recv(); r != nil {
		in = append(in, r.Type())
	}
	for i := 0; i < sig.Params().Len(); i++ {
		in = append(in, sig.Params().At(i).Type())
	}
	for i := 0; i < sig.Results().Len(); i++ {
		out = append(out, sig.Results().At(i).Type())
	}
	return
}

// abiWrappers writes the wrappers the package's declarations need.
func (c *compiler) abiWrappers(try func(string, func())) {
	for _, fd := range c.funcDecls() {
		fd := fd
		fn, _ := c.info.Defs[fd.Name].(*types.Func)
		if fn == nil || fd.Name.Name == "_" || fd.Recv != nil {
			continue
		}
		sig := fn.Type().(*types.Signature)
		if sig.TypeParams().Len() > 0 {
			continue
		}
		sym := c.funcSym(fn)
		name := sym.Name
		if fd.Body == nil && c.abis.defs[name] {
			// the argument stack map assembly functions refer to (the assembler adds the reference): the
			// collector and stack growth read the arguments of an assembly frame through it
			in, out := sigTypes(sig)
			ai := c.abi0Of(in, out)
			n := int(ai.argWidth / 8)
			args := make([]bool, n)
			for _, p := range ai.in {
				if c.sizeof(p.typ) != 0 && c.hasPointers(p.typ) {
					c.setPtrBits(p.typ, p.off, args)
				}
			}
			maps := [][]bool{args}
			if len(ai.out) > 0 {
				res := append([]bool(nil), args...)
				for _, p := range ai.out {
					if c.sizeof(p.typ) != 0 && c.hasPointers(p.typ) {
						c.setPtrBits(p.typ, p.off, res)
					}
				}
				maps = append(maps, res)
			}
			data := binary.LittleEndian.AppendUint32(nil, uint32(len(maps)))
			data = binary.LittleEndian.AppendUint32(data, uint32(n))
			for _, m := range maps {
				bs := make([]byte, (n+7)/8)
				for i, b := range m {
					if b {
						bs[i/8] |= 1 << (i % 8)
					}
				}
				data = append(data, bs...)
			}
			ms := c.lookup(name+".args_stackmap", obj.ABI0, sym.Pkg)
			if !ms.OnList() {
				ms.P = data
				ms.Set(obj.AttrLocal, true)
				c.defData(ms, objabi.SRODATA, int64(len(data)))
			}
		}
		if fd.Body == nil && (c.abis.defs[name] || c.abis.defsInternal[name]) {
			// what a traceback prints for the arguments of an assembly frame (the assembler refers to it)
			in, out := sigTypes(sig)
			ai, abiN := c.abiOfTypes(in, out), 1
			if c.abis.defs[name] {
				ai, abiN = c.abi0Of(in, out), 0
			}
			as := c.lookup(name+".arginfo"+itoa(abiN), obj.ABI0, sym.Pkg)
			if !as.OnList() {
				as.P = c.argInfo(ai)
				as.Align = 1
				as.Set(obj.AttrLocal, true)
				c.defData(as, objabi.SRODATA, int64(len(as.P)))
			}
		}
		c.pr.linknames(c.pkg)
		linknamed := c.pkg.linknamed[fn.Name()] || c.pkg.linknamedStd[fn.Name()]
		switch {
		case fd.Body == nil && c.abis.defs[name]:
			// assembly: Go callers come in through an ABIInternal wrapper
			try("ABI wrapper for "+name, func() { c.wrapToABI0(fd, sig, sym) })
		case fd.Body == nil && c.abis.defsInternal[name] && (c.abis.refs[name] || linknamed):
			// assembly with the register ABI (memmove): assembly elsewhere may still call it with ABI0
			try("ABI0 wrapper for "+name, func() { c.wrapFromABI0(fd, sig, sym) })
		case fd.Body != nil && (c.abis.refs[name] || linknamed):
			// Go: assembly callers (and whoever pulls the symbol by linkname) may come in with ABI0
			try("ABI0 wrapper for "+name, func() { c.wrapFromABI0(fd, sig, sym) })
		}
	}
}

// wrapToABI0: the ABIInternal entry of an assembly function.
func (c *compiler) wrapToABI0(fd *ast.FuncDecl, sig *types.Signature, sym *obj.LSym) {
	g := c.newGen(sym.Name, sig, c.info, nil)
	g.out.sym = sym
	g.nosplit = true
	g.out.flags |= obj.DUPOK | obj.ABIWRAPPER
	in, out := sigTypes(sig)
	ai0 := c.abi0Of(in, out)
	target := c.lookup(sym.Name, obj.ABI0, sym.Pkg)
	g.compileFunc(sig.Recv(), fd.Pos(), fd.End(), nil, func() {
		var args []loc
		for _, p := range g.abi.in {
			args = append(args, g.paramLoc(p))
		}
		rs := g.emitCall(ai0, args, callee{sym: target, abi0: true})
		for i, r := range rs {
			g.move(g.resHome[i], r, g.abi.out[i].typ)
		}
	})
}

// wrapFromABI0: the ABI0 entry of a Go function.
func (c *compiler) wrapFromABI0(fd *ast.FuncDecl, sig *types.Signature, sym *obj.LSym) {
	g := c.newGen(sym.Name, sig, c.info, nil)
	g.out.sym = c.lookup(sym.Name, obj.ABI0, sym.Pkg)
	g.nosplit = true
	g.abi0 = true
	g.out.flags |= obj.DUPOK | obj.ABIWRAPPER
	in, out := sigTypes(sig)
	inner := g.abi
	g.abi = c.abi0Of(in, out)
	g.compileFunc(sig.Recv(), fd.Pos(), fd.End(), nil, func() {
		var args []loc
		for _, p := range g.abi.in {
			args = append(args, g.paramLoc(p))
		}
		rs := g.emitCall(inner, args, callee{sym: sym})
		for i, r := range rs {
			g.move(g.resHome[i], r, g.abi.out[i].typ)
		}
	})
}

// argInfo: the encoding of a function's arguments for tracebacks (the stock compiler's EmitArgInfo): for
// each argument its offset in the argument area and its size, aggregates bracketed, at most ten components.
func (c *compiler) argInfo(ai *abiInfo) []byte {
	const (
		limit, maxDepth                                        = 10, 5
		endSeq, startAgg, endAgg, dotdotdot, tooLarge, special = 0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xf0
	)
	var b []byte
	n := 0
	uintptrT := types.Typ[types.Uintptr]
	var visit func(off int64, t types.Type, depth int) bool
	visit = func(off int64, t types.Type, depth int) bool {
		if n >= limit {
			b = append(b, dotdotdot)
			return false
		}
		u := t.Underlying()
		agg := false
		switch u := u.(type) {
		case *types.Struct, *types.Array, *types.Interface, *types.Slice:
			agg = true
		case *types.Basic:
			agg = u.Info()&(types.IsComplex|types.IsString) != 0
		}
		if !agg {
			if off >= special {
				b = append(b, tooLarge)
			} else {
				b = append(b, byte(off), byte(c.sizeof(t)))
			}
			n++
			return true
		}
		b = append(b, startAgg)
		depth++
		if depth >= maxDepth {
			b = append(b, dotdotdot, endAgg)
			n++
			return true
		}
		switch u := u.(type) {
		case *types.Interface:
			_ = visit(off, uintptrT, depth) && visit(off+8, uintptrT, depth)
		case *types.Slice:
			_ = visit(off, uintptrT, depth) && visit(off+8, uintptrT, depth) && visit(off+16, uintptrT, depth)
		case *types.Basic:
			if u.Info()&types.IsString != 0 {
				_ = visit(off, uintptrT, depth) && visit(off+8, uintptrT, depth)
			} else {
				ft := types.Typ[types.Float64]
				if c.sizeof(t) == 8 {
					ft = types.Typ[types.Float32]
				}
				_ = visit(off, ft, depth) && visit(off+c.sizeof(t)/2, ft, depth)
			}
		case *types.Array:
			if u.Len() == 0 {
				n++
				break
			}
			es := c.sizeof(u.Elem())
			for i := int64(0); i < u.Len(); i++ {
				if !visit(off+i*es, u.Elem(), depth) {
					break
				}
			}
		case *types.Struct:
			if u.NumFields() == 0 {
				n++
				break
			}
			offs := c.fieldOffsets(u)
			for i := 0; i < u.NumFields(); i++ {
				if !visit(off+offs[i], u.Field(i).Type(), depth) {
					break
				}
			}
		}
		b = append(b, endAgg)
		return true
	}
	for _, p := range ai.in {
		if !visit(p.off, p.typ, 0) {
			break
		}
	}
	return append(b, endSeq)
}
