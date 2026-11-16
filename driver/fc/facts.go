// facts.go: what the program knows about a type, kept on the type object. Our go/types has a word in every
// type object but *Basic and *Union (driver/gotypes: types.SBFacts); a fact there is one load, where the maps
// keyed by the type that held them (Program.linkNames, Program.noalg) were a hash and several cache misses
// per question, half a million questions a build.
package main

import (
	"go/types"
	"sync/atomic"
	"unsafe"
)

type typeFacts struct {
	linkName atomic.Pointer[string] // typeLinkName, in a whole-program build
	noalg    atomic.Bool            // a compiler-made type with no equality and no hash
	alg      atomic.Uint32          // algKind + 1, in a whole-program build; 0: not asked yet
	ptr      atomic.Pointer[types.Pointer]
}

// factsOf is t's record, made if need be; nil for a type object without the word.
func factsOf(t types.Type) *typeFacts {
	p := types.SBFacts(t)
	if p == nil {
		return nil
	}
	if f := (*typeFacts)(atomic.LoadPointer(p)); f != nil {
		return f
	}
	atomic.CompareAndSwapPointer(p, nil, unsafe.Pointer(new(typeFacts)))
	return (*typeFacts)(atomic.LoadPointer(p))
}

// factsIf is t's record if it has one.
func factsIf(t types.Type) *typeFacts {
	if p := types.SBFacts(t); p != nil {
		return (*typeFacts)(atomic.LoadPointer(p))
	}
	return nil
}

// ptrTo is the pointer type to t: one object for the program where t has the word (a new object each time
// is a name built each time).
func ptrTo(t types.Type) *types.Pointer {
	f := factsOf(t)
	if f == nil {
		return types.NewPointer(t)
	}
	if p := f.ptr.Load(); p != nil {
		return p
	}
	f.ptr.CompareAndSwap(nil, types.NewPointer(t))
	return f.ptr.Load()
}

// setNoalg marks t as a type with no algorithms; isNoalg asks.
func (pr *Program) setNoalg(t types.Type) {
	if f := factsOf(t); f != nil {
		f.noalg.Store(true)
		return
	}
	pr.noalg.Store(t, true)
}

func (pr *Program) isNoalg(t types.Type) bool {
	if p := types.SBFacts(t); p != nil {
		f := (*typeFacts)(atomic.LoadPointer(p))
		return f != nil && f.noalg.Load()
	}
	_, na := pr.noalg.Load(t)
	return na
}
