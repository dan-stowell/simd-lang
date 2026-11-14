// render.go: gVisor's nogo "render" actions (GoStaticAnalysis ... render -template=T -output=O), done here.
//
// The nogo tool fills a template from facts its analyzers computed (checkconst: constants, sizes and field
// offsets of packages, the runtime's above all): pkg/sync's runtime constants and the offsets of g.goid and
// schedt.nmspinning in assembly. In Bazel those facts come from analyzing the whole standard library from
// Bazel's compiled archives (GoStdlib, GoStandardLibraryAnalysis) and the export data of compiled packages:
// the long chain fc removes. Here the same template language is evaluated against the package type-checked from
// source (go/types, the GOROOT fc compiles, so the values are those of the runtime that is linked in):
// Import, Constant (constant.Value.ExactString, as checkconst records it), Offset, Size, Alignment.
package main

import (
	"bytes"
	"fmt"
	"go/build"
	"go/format"
	"go/importer"
	"go/token"
	"go/types"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"text/template"
)

func isRender(a *action) bool {
	if a.Mnemonic != "GoStaticAnalysis" {
		return false
	}
	for _, x := range a.Arguments {
		if x == "render" {
			return true
		}
	}
	return false
}

var (
	srcImpMu sync.Mutex
	srcImp   types.Importer
	sizes    = types.SizesFor("gc", "amd64")
)

func importSource(p string) (*types.Package, error) {
	srcImpMu.Lock()
	defer srcImpMu.Unlock()
	if srcImp == nil {
		ctxt := build.Default
		ctxt.GOROOT, ctxt.GOOS, ctxt.GOARCH, ctxt.CgoEnabled = goroot(), "linux", "amd64", false
		build.Default = ctxt // (the source importer reads build.Default)
		srcImp = importer.ForCompiler(token.NewFileSet(), "source", nil)
	}
	return srcImp.Import(p)
}

// lookup: a constant, or a type, field or variable named by dotted path ("g.goid") in pkg.
func lookup(pkg *types.Package, name string) (types.Object, *types.Struct, []*types.Var, int, error) {
	parts := strings.Split(name, ".")
	obj := pkg.Scope().Lookup(parts[0])
	if obj == nil {
		return nil, nil, nil, 0, fmt.Errorf("%s: no %s", pkg.Path(), parts[0])
	}
	if len(parts) == 1 {
		return obj, nil, nil, 0, nil
	}
	var st *types.Struct
	var fields []*types.Var
	var off int64
	t := obj.Type()
	for _, f := range parts[1:] {
		s, ok := t.Underlying().(*types.Struct)
		if !ok {
			return nil, nil, nil, 0, fmt.Errorf("%s: %s is not a struct", pkg.Path(), name)
		}
		st, fields = s, nil
		for i := 0; i < s.NumFields(); i++ {
			fields = append(fields, s.Field(i))
		}
		offs := sizes.Offsetsof(fields)
		found := -1
		for i, fv := range fields {
			if fv.Name() == f {
				found = i
			}
		}
		if found < 0 {
			return nil, nil, nil, 0, fmt.Errorf("%s: no field %s", pkg.Path(), name)
		}
		off += offs[found]
		obj, t = fields[found], fields[found].Type()
	}
	_ = st
	return obj, st, fields, int(off), nil
}

func render(a *action) error {
	var tmpl, out string
	doFormat := false
	for _, x := range a.Arguments {
		switch {
		case strings.HasPrefix(x, "-template="):
			tmpl = strings.TrimPrefix(x, "-template=")
		case strings.HasPrefix(x, "-output="):
			out = strings.TrimPrefix(x, "-output=")
		case x == "-format" || x == "-format=true":
			doFormat = true
		}
	}
	if tmpl == "" || out == "" {
		return fmt.Errorf("render: no -template or -output")
	}
	contents, err := os.ReadFile(abs(tmpl))
	if err != nil {
		return err
	}
	contents = regexp.MustCompile(`/\*.*?->\*/.*?/\*<-\s*(.*?)\s*\*/`).ReplaceAll(contents, []byte("$1"))
	funcs := template.FuncMap{
		"Import": func(p string) (*types.Package, error) { return importSource(p) },
		"Constant": func(pkg *types.Package, s string) (string, error) {
			obj, _, _, _, err := lookup(pkg, s)
			if err != nil {
				return "", err
			}
			c, ok := obj.(*types.Const)
			if !ok {
				return "", fmt.Errorf("%s.%s is not a constant", pkg.Path(), s)
			}
			return c.Val().ExactString(), nil
		},
		"Offset": func(pkg *types.Package, s string) (int64, error) {
			_, _, _, off, err := lookup(pkg, s)
			if err != nil {
				return 0, err
			}
			if !strings.Contains(s, ".") {
				return 0, fmt.Errorf("Offset of %s: not a field", s)
			}
			return int64(off), nil
		},
		"Size": func(pkg *types.Package, s string) (int64, error) {
			obj, _, _, _, err := lookup(pkg, s)
			if err != nil {
				return 0, err
			}
			return sizes.Sizeof(obj.Type()), nil
		},
		"Alignment": func(pkg *types.Package, s string) (int64, error) {
			obj, _, _, _, err := lookup(pkg, s)
			if err != nil {
				return 0, err
			}
			return sizes.Alignof(obj.Type()), nil
		},
	}
	t, err := template.New(path.Base(tmpl)).Funcs(funcs).Parse(string(contents))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, nil); err != nil {
		return err
	}
	b := buf.Bytes()
	if doFormat {
		if b, err = format.Source(b); err != nil {
			return err
		}
	}
	return os.WriteFile(abs(out), b, 0o666)
}
