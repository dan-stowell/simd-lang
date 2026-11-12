// Differential test for the baseline build's "no speculative *T descriptors" change: PtrToThis is 0 for
// unnamed types, so reflect must still give one identity to *T however it is reached (statically, by
// PointerTo, by New, through another package's use). Output must match the stock compiler's exactly.
package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type S struct {
	A int
	B string
}

func show(name string, ok bool) { fmt.Printf("%-28s %v\n", name, ok) }

func main() {
	var f func() string
	var g func(int, ...string) (S, error)
	var sl []map[string][]S
	var m map[S]*[4]func()
	var ar [3]struct{ X, Y float64 }
	var ch chan<- []byte
	var pp **int
	vals := []any{f, g, sl, m, ar, ch, pp, struct{ sort.Interface }{}, [0]func(){}, strings.Builder{}}
	for _, v := range vals {
		t := reflect.TypeOf(v)
		p1 := reflect.PointerTo(t)
		p2 := reflect.New(t).Type()
		show("PointerTo==New "+t.Kind().String(), p1 == p2 && p1.Elem() == t)
		show("PointerTo twice", reflect.PointerTo(t) == p1)
		show("PtrTo of PtrTo elem", reflect.PointerTo(p1).Elem() == p1)
	}
	// static *T against the run-time one
	show("static *func", reflect.TypeOf(&f) == reflect.PointerTo(reflect.TypeOf(f)))
	show("static *[]map", reflect.TypeOf(&sl) == reflect.PointerTo(reflect.TypeOf(sl)))
	show("static *[3]struct", reflect.TypeOf(&ar) == reflect.PointerTo(reflect.TypeOf(ar)))
	show("static ***int", reflect.TypeOf(&pp) == reflect.PointerTo(reflect.TypeOf(pp)))
	// interface identity and type switches on pointers to unnamed types
	var i any = &f
	_, ok := i.(*func() string)
	show("assert *func", ok)
	var j any = reflect.New(reflect.TypeOf(sl)).Interface()
	_, ok = j.(*[]map[string][]S)
	show("assert run-time *[]map", ok)
	switch reflect.New(reflect.TypeOf(m)).Interface().(type) {
	case *map[S]*[4]func():
		show("switch run-time *map", true)
	default:
		show("switch run-time *map", false)
	}
	// method sets and names
	fmt.Println(reflect.PointerTo(reflect.TypeOf(g)).String(), reflect.PointerTo(reflect.TypeOf(S{})).NumMethod(),
		reflect.PointerTo(reflect.TypeOf(struct{ sort.Interface }{})).NumMethod(), reflect.TypeOf(&ar).Elem().Elem().Field(1).Name)
	fmt.Printf("%v %T %T\n", reflect.ValueOf(&sl).Elem().CanSet(), &ch, &pp)
}
