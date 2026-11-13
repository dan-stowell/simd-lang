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
	defs map[string]bool // defined in assembly as ABI0
	refs map[string]bool // referenced from assembly as ABI0
}

func (c *compiler) readSymABIs(file string) {
	c.abis = &symABIs{defs: map[string]bool{}, refs: map[string]bool{}}
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
		if len(parts) != 3 || parts[2] != "ABI0" {
			continue
		}
		sym := parts[1]
		if strings.HasPrefix(sym, `"".`) {
			sym = prefix + sym[2:]
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
		if fd.Body == nil {
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
		c.pr.linknames(c.pkg)
		linknamed := c.pkg.linknamed[fn.Name()] || c.pkg.linknamedStd[fn.Name()]
		switch {
		case fd.Body == nil && c.abis.defs[name]:
			// assembly: Go callers come in through an ABIInternal wrapper
			try("ABI wrapper for "+name, func() { c.wrapToABI0(fd, sig, sym) })
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
