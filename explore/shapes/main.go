// shapes: kill test for "compile each function shape once" (divergence step, 2026-11-12).
// For every function body in the files listed on stdin, hash the token stream with identifiers and
// literal values erased (kinds kept). Report the bytes of bodies whose shape occurs at least twice.
// Token shape overcounts true duplicates (types must match too): an upper bound.
package main

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"sort"
)

func main() {
	type rec struct{ n, bytes int }
	shapes := map[[32]byte]*rec{}
	exact := map[[32]byte]*rec{}
	total, funcs := 0, 0
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		path := in.Text()
		src, err := os.ReadFile(path)
		if err != nil || len(path) < 3 || path[len(path)-3:] != ".go" {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			lo, hi := fset.Position(fd.Body.Pos()).Offset, fset.Position(fd.Body.End()).Offset
			body := src[lo:hi]
			var s scanner.Scanner
			fs2 := token.NewFileSet()
			s.Init(fs2.AddFile("", -1, len(body)), body, nil, 0)
			h, hx := sha256.New(), sha256.New()
			for {
				_, tok, lit := s.Scan()
				if tok == token.EOF {
					break
				}
				h.Write([]byte{byte(tok)})
				hx.Write([]byte{byte(tok)})
				hx.Write([]byte(lit))
				hx.Write([]byte{0})
			}
			var k, kx [32]byte
			copy(k[:], h.Sum(nil))
			copy(kx[:], hx.Sum(nil))
			for _, m := range []struct {
				m map[[32]byte]*rec
				k [32]byte
			}{{shapes, k}, {exact, kx}} {
				r := m.m[m.k]
				if r == nil {
					r = &rec{}
					m.m[m.k] = r
				}
				r.n++
				r.bytes += len(body)
			}
			total += len(body)
			funcs++
		}
	}
	rep := func(name string, m map[[32]byte]*rec) {
		dup, dupFuncs, saved := 0, 0, 0
		var big []int
		for _, r := range m {
			if r.n > 1 {
				dup += r.bytes
				dupFuncs += r.n
				saved += r.bytes - r.bytes/r.n
				big = append(big, r.bytes)
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(big)))
		fmt.Printf("%s: %d distinct; in shapes seen twice or more: %d functions, %d bytes (%.1f%% of body bytes); bytes beyond the first copy: %d (%.1f%%)\n",
			name, len(m), dupFuncs, dup, 100*float64(dup)/float64(total), saved, 100*float64(saved)/float64(total))
	}
	fmt.Printf("functions with bodies: %d, body bytes: %d\n", funcs, total)
	rep("token shape (names and literals erased)", shapes)
	rep("exact token text", exact)
}
