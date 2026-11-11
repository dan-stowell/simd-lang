// Audit-only lexer baseline controls. No project or GOROOT code is modified.
package main

import (
	"audit5speed/syntax"
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

// Copied verbatim from explore/golex/main.go at 25a887e.
func scan(src []byte, emit func(off int, tok token.Token)) (n int, errs int) {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, func(pos token.Position, msg string) { errs++ }, 0)
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
		n++
		if emit != nil {
			emit(int(pos)-file.Base(), tok)
		}
	}
	return
}

func direct(src []byte, handler bool) (n, errs int) {
	file := token.NewFileSet().AddFile("", -1, len(src))
	var s scanner.Scanner
	var eh scanner.ErrorHandler
	if handler {
		eh = func(_ token.Position, _ string) { errs++ }
	}
	s.Init(file, src, eh, 0)
	for {
		_, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
		n++
	}
	return n, s.ErrorCount
}

func reuse(src []byte, s *scanner.Scanner, f *token.File) (n, errs int) {
	s.Init(f, src, nil, 0)
	for {
		_, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
		n++
	}
	return n, s.ErrorCount
}

type sample struct {
	Mode       string `json:"mode"`
	Rep        int    `json:"rep"`
	Bytes      int    `json:"bytes"`
	Tokens     int    `json:"tokens"`
	Errors     int    `json:"errors"`
	Ns         int64  `json:"ns"`
	AllocBytes uint64 `json:"alloc_bytes"`
	Allocs     uint64 `json:"allocs"`
	GCs        uint32 `json:"gcs"`
	GCPauseNs  uint64 `json:"gc_pause_ns"`
	HeapBefore uint64 `json:"heap_before"`
	HeapAfter  uint64 `json:"heap_after"`
	GCPolicy   string `json:"gc_policy"`
	GCPercent  int    `json:"gc_percent"`
	GOMAXPROCS int    `json:"gomaxprocs"`
}

func main() {
	mode := flag.String("mode", "original", "original, direct, handler, reuse, syntax, syntax-reuse, check, syntax-dump")
	file := flag.String("file", "", "input path")
	reps := flag.Int("reps", 7, "measured scans, no hidden warmup except reuse modes")
	gc := flag.String("gc", "natural", "natural, before (untimed runtime.GC before each scan), off")
	flag.Parse()
	src, err := os.ReadFile(*file)
	must(err)
	if *mode == "check" {
		check(src)
		return
	}
	if *mode == "syntax-dump" {
		dump(src)
		return
	}
	pct := debug.SetGCPercent(100)
	debug.SetGCPercent(pct)
	if *gc == "off" {
		debug.SetGCPercent(-1)
		pct = -1
	}
	if *gc != "natural" && *gc != "before" && *gc != "off" {
		panic("unknown GC policy")
	}
	var fn func() (int, int)
	switch *mode {
	case "original":
		fn = func() (int, int) { return scan(src, nil) }
	case "direct":
		fn = func() (int, int) { return direct(src, false) }
	case "handler":
		fn = func() (int, int) { return direct(src, true) }
	case "reuse":
		f := token.NewFileSet().AddFile("", -1, len(src))
		var s scanner.Scanner
		n, e := reuse(src, &s, f)
		fmt.Fprintf(os.Stderr, "untimed reuse warmup: %d tokens, %d errors, %d lines\n", n, e, f.LineCount())
		fn = func() (int, int) { return reuse(src, &s, f) }
	case "syntax":
		fn = func() (int, int) { return syntax.Scan(src) }
	case "syntax-reuse":
		var c syntax.Counter
		c.Init(src)
		n, e := c.Count()
		fmt.Fprintf(os.Stderr, "untimed syntax buffer warmup: %d tokens, %d errors\n", n, e)
		fn = func() (int, int) { c.Init(src); return c.Count() }
	default:
		panic("unknown mode")
	}
	// Print only after all measurements: JSON encoder allocation does not skew
	// GC thresholds between repetitions. ReadMemStats itself is outside time.
	results := make([]sample, 0, *reps)
	for i := 0; i < *reps; i++ {
		if *gc == "before" {
			runtime.GC()
		}
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		start := time.Now()
		n, e := fn()
		d := time.Since(start)
		runtime.ReadMemStats(&b)
		results = append(results, sample{*mode, i, len(src), n, e, d.Nanoseconds(),
			b.TotalAlloc - a.TotalAlloc, b.Mallocs - a.Mallocs, b.NumGC - a.NumGC,
			b.PauseTotalNs - a.PauseTotalNs, a.HeapAlloc, b.HeapAlloc, *gc, pct, runtime.GOMAXPROCS(0)})
	}
	enc := json.NewEncoder(os.Stdout)
	for _, r := range results {
		must(enc.Encode(r))
		if r.Errors != 0 {
			os.Exit(2)
		}
	}
	runtime.KeepAlive(src)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func dump(src []byte) {
	v := syntax.NewVisitor(src)
	var offs []uint32
	var kinds []byte
	for {
		_, off, tok := v.Next()
		if tok == token.EOF {
			break
		}
		offs = append(offs, uint32(off))
		kinds = append(kinds, byte(tok))
	}
	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	must(binary.Write(w, binary.LittleEndian, offs))
	must(binary.Write(w, binary.LittleEndian, kinds))
	must(w.Flush())
	fmt.Fprintln(os.Stderr, "compiler scanner errors:", v.Errors())
	if v.Errors() > 0 {
		os.Exit(2)
	}
}

func check(src []byte) {
	v := syntax.NewVisitor(src)
	n, kinds, rawOffsets, canonicalOffsets := 0, 0, 0, 0
	first := []string{}
	f := token.NewFileSet().AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(f, src, nil, 0)
	// Hash interleaved uint64 offset/kind pairs, including EOF, not dump format.
	hStd, hSyntax := sha256.New(), sha256.New()
	var b [16]byte
	for {
		pos, tok, _ := s.Scan()
		raw, off, other := v.Next()
		stdOff := int(pos) - f.Base()
		if tok != other {
			kinds++
		}
		if stdOff != raw {
			rawOffsets++
		}
		if stdOff != off {
			canonicalOffsets++
		}
		if (tok != other || stdOff != off || stdOff != raw) && len(first) < 12 {
			first = append(first, fmt.Sprintf("index=%d std=(%d,%s) syntax=(raw:%d canonical:%d,%s)", n, stdOff, tok, raw, off, other))
		}
		binary.LittleEndian.PutUint64(b[:8], uint64(stdOff))
		binary.LittleEndian.PutUint64(b[8:], uint64(tok))
		hStd.Write(b[:])
		binary.LittleEndian.PutUint64(b[:8], uint64(off))
		binary.LittleEndian.PutUint64(b[8:], uint64(other))
		hSyntax.Write(b[:])
		if tok == token.EOF || other == token.EOF {
			break
		}
		n++
	}
	result := map[string]any{"bytes": len(src), "tokens": n, "std_errors": s.ErrorCount, "syntax_errors": v.Errors(),
		"kind_mismatches": kinds, "raw_offset_mismatches": rawOffsets, "canonical_offset_mismatches": canonicalOffsets,
		"first_mismatches": first, "std_interleaved_hash": hex.EncodeToString(hStd.Sum(nil)),
		"syntax_interleaved_hash": hex.EncodeToString(hSyntax.Sum(nil))}
	must(json.NewEncoder(os.Stdout).Encode(result))
	if kinds != 0 || canonicalOffsets != 0 || s.ErrorCount != 0 || v.Errors() != 0 {
		os.Exit(2)
	}
}
