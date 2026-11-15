package lines

import "runtime"

func where() (string, int) {
	_, f, l, _ := runtime.Caller(1)
	return f, l
}

// Directives: the file and line runtime.Caller reports under //line directives of several forms.
func Directives() [][2]any {
	var out [][2]any
	add := func(f string, l int) { out = append(out, [2]any{f, l}) }
//line /virtual/abs.go:100
	add(where())
//line rel.go:200
	add(where())
//line :300:1
	add(where()) // no file but a column: the file stays
	/*line /virtual/inline.go:400:1*/ add(where())
	add(where())
	return out
}
