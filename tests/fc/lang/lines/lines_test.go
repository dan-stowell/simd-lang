package lines_test

import (
	"os"
	"strings"
	"testing"

	"fclang/lines"
)

// marks: the lines of lines.go marked "// at <name>".
func marks(t *testing.T) map[string]int {
	b, err := os.ReadFile("lines.go")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]int{}
	for i, s := range strings.Split(string(b), "\n") {
		if _, name, ok := strings.Cut(s, "// at "); ok {
			m[strings.TrimSpace(name)] = i + 1
		}
	}
	return m
}

func TestLines(t *testing.T) {
	m := marks(t)
	outer, inner := lines.SplitArgCall()
	for _, c := range []struct {
		name string
		got  int
	}{
		{"SplitMethodCall", lines.SplitMethodCall()},
		{"SplitArgCallOuter", outer},
		{"SplitArgCallInner", inner},
		{"NilIfaceSplit", lines.NilIfaceSplit()},
		{"IndexSplit", lines.IndexSplit()},
		{"SliceSplit", lines.SliceSplit()},
		{"SelectCase", lines.SelectCase()},
	} {
		want, ok := m[c.name]
		if !ok {
			t.Fatalf("no mark for %s", c.name)
		}
		if c.got != want {
			t.Errorf("%s: line %d, want %d", c.name, c.got, want)
		}
	}
}

// TestEndLines: a function that ends in a return has no code at its closing brace.
func TestEndLines(t *testing.T) {
	b, _ := os.ReadFile("lines.go")
	brace := 0
	for i, s := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(s, "} // (no code here") {
			brace = i + 1
		}
	}
	seen := lines.EndLines()
	if seen[brace] {
		t.Errorf("FuncForPC reports line %d (the closing brace) in endsInReturn: %v", brace, seen)
	}
	if len(seen) == 0 {
		t.Errorf("no lines")
	}
}

func TestDirectives(t *testing.T) {
	got := lines.Directives()
	want := []struct {
		suffix string
		line   int
	}{
		{"/virtual/abs.go", 100},
		{"/lines/rel.go", 200}, // relative to the directory of the file with the directive
		{"/lines/rel.go", 300},
		{"/virtual/inline.go", 400},
		{"/virtual/inline.go", 401},
	}
	if len(got) != len(want) {
		t.Fatalf("%d results, want %d", len(got), len(want))
	}
	for i, w := range want {
		f, l := got[i][0].(string), got[i][1].(int)
		if !strings.HasSuffix(f, w.suffix) || l != w.line {
			t.Errorf("directive %d: %s:%d, want ...%s:%d", i, f, l, w.suffix, w.line)
		}
	}
}
