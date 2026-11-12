// sbc: a second Go compiler that takes over single functions inside a build done by the stock compiler.
// It lives in the cmd tree of the patched GOROOT (driver/mksbc.sh copies it there) because it writes
// its machine code through cmd/internal/obj, the stock assembler's back half.
//
// This file: the emitter (machine code, line tables and the object file all come from cmd/internal/obj)
// and -selftest, two hand-made functions that pinned down the boundary: object format, ABIInternal,
// symbol naming, what the linker and the runtime insist on. compile.go is the compiler.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"

	"cmd/internal/bio"
	"cmd/internal/obj"
	"cmd/internal/obj/x86"
	"cmd/internal/objabi"
	"cmd/internal/src"
)

type emitter struct {
	ctxt  *obj.Link
	base  *src.PosBase
	plist *obj.Plist
	last  *obj.Prog
	line  uint
	bases  map[string]*src.PosBase
	rename func(string) string // source file path -> the name recorded in the line table
}

func newEmitter(pkgpath string) *emitter {
	ctxt := obj.Linknew(&x86.Linkamd64)
	ctxt.IsAsm = true // every symbol is referenced by name, like hand-written assembly
	ctxt.Pkgpath = pkgpath
	ctxt.DwTextCount = objabi.DummyDwarfFunctionCountForAssembler() // as cmd/asm: sizes the debug-info address references
	ctxt.Bso = bufio.NewWriter(os.Stdout)
	ctxt.DiagFunc = func(format string, args ...any) { log.Fatalf("sbc: "+format, args...) }
	x86.Linkamd64.Init(ctxt)
	return &emitter{ctxt: ctxt, base: src.NewFileBase("sbc", "sbc"), plist: new(obj.Plist), line: 1}
}

func (e *emitter) pos() src.XPos {
	return e.ctxt.PosTable.XPos(src.MakePos(e.base, e.line, 1))
}

func (e *emitter) add(p *obj.Prog) *obj.Prog {
	p.Ctxt = e.ctxt
	p.Pos = e.pos()
	if e.last == nil {
		e.plist.Firstpc = p
	} else {
		e.last.Link = p
	}
	e.last = p
	return p
}

// text starts a function: name is the full linker symbol name, argsize the bytes of stack the caller
// set aside for it (stack-assigned arguments and results, and the spill slots of register arguments).
func (e *emitter) text(name string, flags int, frame int64, argsize int32) *obj.LSym {
	s := e.ctxt.LookupABI(name, obj.ABIInternal)
	e.ctxt.InitTextSym(s, flags, e.pos())
	p := e.add(&obj.Prog{As: obj.ATEXT,
		From: obj.Addr{Type: obj.TYPE_MEM, Name: obj.NAME_EXTERN, Sym: s},
		To:   obj.Addr{Type: obj.TYPE_TEXTSIZE, Offset: frame, Val: argsize}})
	s.Func().Text = p
	return s
}

func reg(r int16) obj.Addr            { return obj.Addr{Type: obj.TYPE_REG, Reg: r} }
func mem(r int16, off int64) obj.Addr { return obj.Addr{Type: obj.TYPE_MEM, Reg: r, Offset: off} }
func imm(v int64) obj.Addr            { return obj.Addr{Type: obj.TYPE_CONST, Offset: v} }

func (e *emitter) ins(as obj.As, from, to obj.Addr) *obj.Prog {
	return e.add(&obj.Prog{As: as, From: from, To: to})
}

func (e *emitter) ret() { e.add(&obj.Prog{As: obj.ARET}) }

func (e *emitter) write(path string) {
	obj.Flushplist(e.ctxt, e.plist, nil)
	e.ctxt.NumberSyms()
	out, err := bio.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	out.WriteString(objabi.HeaderString())
	fmt.Fprintf(out, "!\n")
	obj.WriteObjFile(e.ctxt, out)
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}
	e.ctxt.Bso.Flush()
}

func selftest(pkg, out string) {
	e := newEmitter(pkg)
	// func add3(a, b, c int) int: arguments in AX, BX, CX; result in AX
	e.text(pkg+".add3", obj.NOSPLIT|obj.NOFRAME, 0, 24)
	e.ins(x86.AADDQ, reg(x86.REG_BX), reg(x86.REG_AX))
	e.ins(x86.AADDQ, reg(x86.REG_CX), reg(x86.REG_AX))
	e.ret()
	// func (p *T) Get() int, with the field at offset 8: a load through the receiver; a nil receiver faults
	// in the first page and the runtime turns that into the usual nil-dereference panic
	e.text(pkg+".(*T).Get", obj.NOSPLIT|obj.NOFRAME, 0, 8)
	e.ins(x86.AMOVQ, mem(x86.REG_AX, 8), reg(x86.REG_AX))
	e.ret()
	e.write(out)
}

func main() {
	st := flag.Bool("selftest", false, "write an object with two hand-made functions")
	pkg := flag.String("p", "main", "package path")
	out := flag.String("o", "sbc.o", "output object")
	plan := flag.String("plan", "", "the build's plan (JSON from sb -dumpplan)")
	work := flag.String("work", "", "the build's work directory: objects go to <work>/bNNNN/sbc.o, the lists of taken functions to sbc.symabis")
	only := flag.String("only", "", "take functions only from packages whose path contains this (for bisecting)")
	flag.Parse()
	if *st {
		selftest(*pkg, *out)
		return
	}
	if *plan == "" || *work == "" {
		flag.Usage()
		os.Exit(2)
	}
	compileProgram(*plan, *work, *only)
}
