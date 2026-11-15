package generics_test

import (
	"reflect"
	"strings"
	"testing"

	g "fclang/generics"
)

func typeNames(xs []any) string {
	var b []string
	for _, x := range xs {
		b = append(b, reflect.TypeOf(x).String())
	}
	return strings.Join(b, " | ")
}

func TestLocalTypesOfGenericFunctions(t *testing.T) {
	sets := g.LocalTypeSets()
	for i, want := range []string{
		"generics.L[int,string] | generics.P[int,string] | generics.G[int,string;int] | generics.H[int,string;int] | generics.Q[int,string;int] | generics.G[int,string;fclang/generics.Q[int,string;int]·5] | []generics.L[int,string] | func(generics.L[int,string]) | generics.List[int,string;string]",
		"generics.L[fclang/generics.LI,fclang/generics.M·7] | generics.P[fclang/generics.LI,fclang/generics.M·7] | generics.G[fclang/generics.LI,fclang/generics.M·7;int] | generics.H[fclang/generics.LI,fclang/generics.M·7;int] | generics.Q[fclang/generics.LI,fclang/generics.M·7;int] | generics.G[fclang/generics.LI,fclang/generics.M·7;fclang/generics.Q[fclang/generics.LI,fclang/generics.M·7;fclang/generics.LI]·5] | []generics.L[fclang/generics.LI,fclang/generics.M·7] | func(generics.L[fclang/generics.LI,fclang/generics.M·7]) | generics.List[fclang/generics.LI,fclang/generics.M·7;fclang/generics.M·7]",
		"generics.L[int,string] | generics.P[int,string] | generics.G[int,string;int] | generics.H[int,string;int] | generics.Q[int,string;int] | generics.G[int,string;fclang/generics.Q[int,string;int]·5] | []generics.L[int,string] | func(generics.L[int,string]) | generics.List[int,string;string]",
		"generics.L[fclang/generics.LI,string] | generics.P[fclang/generics.LI,string] | generics.G[fclang/generics.LI,string;int] | generics.H[fclang/generics.LI,string;int] | generics.Q[fclang/generics.LI,string;int] | generics.G[fclang/generics.LI,string;fclang/generics.Q[fclang/generics.LI,string;fclang/generics.LI]·5] | []generics.L[fclang/generics.LI,string] | func(generics.L[fclang/generics.LI,string]) | generics.List[fclang/generics.LI,string;string]",
	} {
		if got := typeNames(sets[i]); got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
	}
	if got, want := typeNames(g.LocalTypesPlain()), "generics.L | generics.G[int] | generics.G[fclang/generics.G[fclang/generics.LI]·9]"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// each instantiation of the function has its own types, and the same instantiation the same ones
	a, a2, b := sets[0], sets[2], sets[3]
	for i := range a {
		if reflect.TypeOf(a[i]) == reflect.TypeOf(b[i]) || reflect.TypeOf(a[i]) != reflect.TypeOf(a2[i]) {
			t.Errorf("%d: %v %v %v", i, reflect.TypeOf(a[i]), reflect.TypeOf(b[i]), reflect.TypeOf(a2[i]))
		}
		if i < 6 && a[i] == b[i] {
			t.Errorf("%d: values of distinct types compare equal", i)
		}
	}
}
