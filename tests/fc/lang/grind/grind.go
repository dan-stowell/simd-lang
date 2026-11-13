// Package grind: reductions from the standard library's and MinIO's own tests (reviews/done/fc-grind-1.md).
package grind

import (
	"embed"
	"iter"
	"math/rand/v2"
)

// a generic method and a generic function of the same name are different symbols
type R struct{}

func (r *R) N[T ~uint](n T) T { return n - 1 }

func N[T ~uint](n T) T {
	var r R
	return r.N(n)
}

func Pick() uint { return N(uint(10)) }

func RandN() uint { return rand.N(uint(10)) }

// iter.Pull2 reaches the runtime's coroutines through //go:linknamestd
func First() (int, int, bool) {
	next, stop := iter.Pull2(func(yield func(int, int) bool) { yield(1, 2) })
	defer stop()
	return next()
}

//go:embed data.txt
var files embed.FS

func Read() (string, error) {
	b, err := files.ReadFile("data.txt")
	return string(b), err
}
