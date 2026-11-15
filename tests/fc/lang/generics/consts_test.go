package generics_test

import (
	"fmt"
	"testing"

	g "fclang/generics"
)

func TestConstConversions(t *testing.T) {
	got := fmt.Sprint(g.ConstConversions()...)
	want := "(0+0i) (3.14+0i) (0-1i) (0+0i) (3.14+0i) (0-1i) 1.5 1.5"
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
