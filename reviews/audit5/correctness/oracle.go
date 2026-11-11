// A JSON-lines oracle used by probe.py. No changes to the audited implementation.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
)

type Request struct {
	Source string `json:"source"`
	Check  bool   `json:"check"`
}
type Tok struct {
	Offset int    `json:"offset"`
	Kind   int    `json:"kind"`
	Name   string `json:"name"`
}
type Result struct {
	Tokens      []Tok    `json:"tokens"`
	Errors      []string `json:"errors"`
	ParseError  string   `json:"parse_error,omitempty"`
	TypeError   string   `json:"type_error,omitempty"`
	Checked     bool     `json:"checked"`
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 4096), 32<<20)
	out := bufio.NewWriter(os.Stdout)
	for in.Scan() {
		var req Request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			panic(err)
		}
		src, err := base64.StdEncoding.DecodeString(req.Source)
		if err != nil {
			panic(err)
		}
		r := Result{Tokens: []Tok{}, Errors: []string{}, Checked: req.Check}
		fs := token.NewFileSet()
		f := fs.AddFile("probe.go", fs.Base(), len(src))
		var s scanner.Scanner
		s.Init(f, src, func(pos token.Position, msg string) {
			r.Errors = append(r.Errors, fmt.Sprintf("%d: %s", pos.Offset, msg))
		}, 0)
		for {
			pos, tok, _ := s.Scan()
			if tok == token.EOF {
				break
			}
			r.Tokens = append(r.Tokens, Tok{int(pos) - f.Base(), int(tok), tok.String()})
		}
		if req.Check {
			pfs := token.NewFileSet()
			a, pe := parser.ParseFile(pfs, "probe.go", src, parser.AllErrors)
			if pe != nil {
				r.ParseError = pe.Error()
			} else {
				conf := types.Config{}
				_, te := conf.Check("probe", pfs, []*ast.File{a}, nil)
				if te != nil {
					r.TypeError = te.Error()
				}
			}
		}
		b, _ := json.Marshal(r)
		out.Write(b)
		out.WriteByte('\n')
		out.Flush()
	}
	if err := in.Err(); err != nil {
		panic(err)
	}
}
