package main

// wbcheck.go (FC_WBCHECK=1): the stock compiler's proof that code the runtime marks //go:nowritebarrierrec has no
// write barriers, redone on fc's output (cmd/compile/internal/ssagen/nowb.go). From every function so marked,
// follow the calls inside the package (and the closures a function makes: the stock check follows the ones
// handed to systemstack; this follows all of them, so a closure run later elsewhere can be a false alarm) until
// a function marked //go:yeswritebarrierrec; a write barrier fc emitted in any function reached is reported
// with the chain of calls. The runtime's lock order depends on it: a barrier that flushes its buffer allocates,
// and the allocator's locks are held by exactly this code.

import (
	"fmt"
	"os"
	"strings"

	"cmd/internal/obj"
)

var wbCheck = os.Getenv("FC_WBCHECK") != ""

func (c *compiler) wbCheck() {
	bySym := map[*obj.LSym]*funcOut{}
	for _, f := range c.funcs {
		if f.sym != nil {
			bySym[f.sym] = f
		}
	}
	type from struct {
		f    *funcOut
		prev *from
	}
	seen := map[*funcOut]bool{}
	var q []*from
	for _, f := range c.funcs {
		if f.wbRec == 1 {
			seen[f] = true
			q = append(q, &from{f, nil})
		}
	}
	bad := 0
	for len(q) > 0 {
		x := q[0]
		q = q[1:]
		if len(x.f.wbPos) > 0 {
			bad++
			var chain []string
			for y := x; y != nil; y = y.prev {
				chain = append(chain, y.f.sym.Name)
			}
			fmt.Fprintf(os.Stderr, "fc: wbcheck: write barrier at %v in %s, reached from //go:nowritebarrierrec: %s\n",
				c.pr.fset.Position(x.f.wbPos[0]), x.f.sym.Name, strings.Join(chain, " <- "))
		}
		for _, r := range x.f.relocs {
			t := bySym[r.Sym]
			if t == nil || seen[t] || t.wbRec == 2 || wbCheckStop(t.sym.Name) {
				continue
			}
			if r.Type != objabiRCall && !strings.HasPrefix(t.sym.Name, x.f.sym.Name+".") {
				continue // (an address taken that is not one of this function's closures)
			}
			seen[t] = true
			q = append(q, &from{t, x})
		}
	}
	if bad > 0 || len(seen) > 0 {
		fmt.Fprintf(os.Stderr, "fc: wbcheck: %s: %d functions reached from //go:nowritebarrierrec, %d with write barriers\n", c.path, len(seen), bad)
	}
}

// wbCheckStop: calls the stock check does not follow. A failed bounds check, nil check or division is a call
// to an assembly stub there (runtime.panicBounds, sigpanic), not to a Go function of the package; fc calls
// the Go functions (goPanicIndex and the rest), and a panic or a throw is the end of the path anyway.
func wbCheckStop(name string) bool {
	for _, p := range []string{"runtime.goPanic", "runtime.panic", "runtime.gopanic", "runtime.throw", "runtime.fatal"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
