// asmlib.go: the stock assembler, in this process. The code is cmd/asm's (its internal packages, copied into
// fc's GOROOT as cmd/internal/fcasm by driver/mkfcasmlib.sh); what is here is cmd/asm's main function with
// the flags as arguments. A MinIO build started the assembler 170 times (61 packages with assembly: once
// each for the symbol ABIs, once per file for the object): 1.06 core-s of process starts and 103,000 page
// faults for 3.4 MB of objects. FC_ASMPROC=1 starts processes as before.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"internal/buildcfg"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"cmd/internal/bio"
	"cmd/internal/fcasm/arch"
	stockasm "cmd/internal/fcasm/asm"
	"cmd/internal/fcasm/flags"
	"cmd/internal/fcasm/lex"
	"cmd/internal/obj"
	"cmd/internal/objabi"
)

var asmInProc = os.Getenv("FC_ASMPROC") == ""

// asmMu: the assembler's packages keep the include path and the predefined macros in package variables,
// and the x86 back end fills its tables on first use.
var asmMu sync.Mutex

// asmSerial (FC_ASMSERIAL=1): one assembly at a time, start to end.
var asmSerial = os.Getenv("FC_ASMSERIAL") != ""

var (
	asmArch                   *arch.Arch
	asmNS, asmWaitNS, asmRuns int64 // time assembling, time waiting for asmMu, runs
	asmABINS                  int64 // of asmNS: the symbol ABI passes
)

// assemble runs the assembler over files of package p: with symabis, the symbol ABI listing of all of them
// (returned); otherwise one object, written to out. dir is the package's directory of generated headers.
func assemble(p *Pkg, dir string, files []string, out string, symabis bool) ([]byte, error) {
	t0 := time.Now()
	asmMu.Lock()
	locked := true
	unlock := func() {
		if locked && !asmSerial {
			asmMu.Unlock()
			locked = false
		}
	}
	defer func() {
		if locked {
			asmMu.Unlock()
		}
	}()
	t1 := time.Now()
	defer func() {
		atomic.AddInt64(&asmWaitNS, int64(t1.Sub(t0)))
		atomic.AddInt64(&asmNS, int64(time.Since(t1)))
		if symabis {
			atomic.AddInt64(&asmABINS, int64(time.Since(t1)))
		}
		atomic.AddInt64(&asmRuns, 1)
	}()
	if asmArch == nil {
		asmArch = arch.Set("amd64", false) // (its tables are only read)
	}
	architecture := asmArch
	ctxt := obj.Linknew(architecture.LinkArch)
	ctxt.CompressInstructions = true
	ctxt.IsAsm = true
	ctxt.Pkgpath = asmPkgpath(p)
	ctxt.Std = p.Standard
	ctxt.DwTextCount = objabi.DummyDwarfFunctionCountForAssembler()
	ctxt.Bso = bufio.NewWriter(os.Stdout)
	defer ctxt.Bso.Flush()
	architecture.Init(ctxt)
	flags.I = flags.MultiFlag{dir, filepath.Join(runtime.GOROOT(), "pkg/include")}
	flags.D = flags.MultiFlag{"GOOS_linux", "GOARCH_amd64", "GOAMD64_v1"}
	if objabi.LookupPkgSpecial(ctxt.Pkgpath).AllowAsmABI {
		for _, exp := range buildcfg.Experiment.Enabled() {
			flags.D = append(flags.D, "GOEXPERIMENT_"+exp)
		}
	}
	var diags bytes.Buffer
	ctxt.DiagFunc = func(format string, args ...any) { fmt.Fprintf(&diags, format+"\n", args...) }
	var abis bytes.Buffer
	ok := true
	lexers := make([]lex.TokenReader, len(files))
	for i, f := range files {
		lexers[i] = lex.NewLexer(f) // (reads flags.I and flags.D)
	}
	unlock() // the rest touches only this run's context and the back end's tables, which are filled
	for i, f := range files {
		lexer := lexers[i]
		parser := stockasm.NewParser(ctxt, architecture, lexer)
		if symabis {
			ok = parser.ParseSymABIs(&abis)
		} else {
			pl := new(obj.Plist)
			pl.Firstpc, ok = parser.Parse()
			if ok {
				obj.Flushplist(ctxt, pl, nil)
			}
		}
		if !ok || diags.Len() > 0 {
			return nil, fmt.Errorf("assembly of %s failed: %s", f, diags.String())
		}
	}
	if symabis {
		return abis.Bytes(), nil
	}
	buf, err := bio.Create(out)
	if err != nil {
		return nil, err
	}
	buf.WriteString(objabi.HeaderString())
	fmt.Fprintf(buf, "!\n")
	ctxt.NumberSyms()
	obj.WriteObjFile(ctxt, buf)
	return nil, buf.Close()
}
