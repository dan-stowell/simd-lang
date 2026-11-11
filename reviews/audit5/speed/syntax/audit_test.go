package syntax

import (
	goscanner "go/scanner"
	gotoken "go/token"
	"testing"
)

func TestNormalized(t *testing.T) {
	for _, src := range []string{
		"", "x", "x\n", "x /*one\n two*/ y\n",
		"x //line ignored.go:123\n y\n", "/*line ignored.go:123*/ x\n",
		"package foo\nvar x = 0x1.2p3 + .5i; var y = 'z'; var z = `a\nb`\n",
		"+ - * / % & | ^ << >> &^ += -= *= /= %= &= |= ^= <<= >>= &^= && || <- ++ -- == < > = ! != <= >= := ... ( ) [ ] { } , ; : . ~\n",
		"break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var\n",
		"var _α1 = \"hello\" \n x /*one\n two\n*/\n",
	} {
		b := []byte(src)
		v := NewVisitor(b)
		f := gotoken.NewFileSet().AddFile("", -1, len(b))
		var s goscanner.Scanner
		s.Init(f, b, nil, 0)
		for {
			pos, tok, _ := s.Scan()
			_, off, other := v.Next()
			if other != tok || off != int(pos)-f.Base() {
				t.Fatalf("%q: got (%d,%s), want (%d,%s)", src, off, other, int(pos)-f.Base(), tok)
			}
			if tok == gotoken.EOF {
				break
			}
		}
		if s.ErrorCount != 0 || v.Errors() != 0 {
			t.Fatal("unexpected errors")
		}
	}
}
