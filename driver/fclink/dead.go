// dead.go: the init order, and reachability with the stock linker's method pruning (cmd/link/internal/ld
// deadcode.go), as a flood that all cores advance one frontier at a time. Marking is a fetch-or on the
// symbol's state word; whoever sets the bit queues the symbol for the next level.
package fclinklib

import (
	"cmd/internal/goobj"
	"cmd/internal/objabi"
	"container/heap"
	"encoding/binary"
	"internal/abi"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

var (
	mainInittasks    []uint32 // the inittask records the runtime runs at start, in order
	runtimeInittasks []uint32
)

type nameHeap []struct {
	name string
	gid  uint32
}

func (h nameHeap) Len() int           { return len(h) }
func (h nameHeap) Less(i, j int) bool { return h[i].name < h[j].name }
func (h nameHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *nameHeap) Push(x any) {
	*h = append(*h, x.(struct {
		name string
		gid  uint32
	}))
}
func (h *nameHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// inittaskOrder: every inittask record reachable from root over R_INITORDER edges (package imports), in
// an order that respects the edges and is otherwise lexicographic by name; records with nothing to run
// are left out. This is ld.(*Link).inittaskSym.
func inittaskOrder(root string) []uint32 {
	r0 := lookupName(root, 0)
	if !isDef(r0) {
		return nil
	}
	type edge struct{ from, to uint32 }
	var edges []edge
	m := map[uint32]int{r0: 0}
	q := []uint32{r0}
	var h nameHeap
	push := func(g uint32) {
		heap.Push(&h, struct {
			name string
			gid  uint32
		}{symName(g), g})
	}
	for len(q) > 0 {
		x := q[len(q)-1]
		q = q[:len(q)-1]
		ndeps := 0
		if isDef(x) {
			o, j := objOf(x)
			rs := o.relocs(j)
			for i := 0; i < rs.Len(); i++ {
				rl := rs.At(i)
				if objabi.RelocType(rl.Type()) != objabi.R_INITORDER {
					continue
				}
				ndeps++
				s := canon[o.ref(rl.Sym())]
				edges = append(edges, edge{x, s})
				if _, ok := m[s]; !ok {
					q = append(q, s)
					m[s] = 0
				}
			}
		}
		m[x] = ndeps
		if ndeps == 0 {
			push(x)
		}
	}
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].to < edges[j].to })
	var sched []uint32
	for h.Len() > 0 {
		s := heap.Pop(&h).(struct {
			name string
			gid  uint32
		}).gid
		if isDef(s) {
			if o, j := objOf(s); o.sym(j).Siz() > 8 {
				sched = append(sched, s)
			}
		}
		a := sort.Search(len(edges), func(i int) bool { return edges[i].to >= s })
		for ; a < len(edges) && edges[a].to == s; a++ {
			if m[edges[a].from]--; m[edges[a].from] == 0 {
				push(edges[a].from)
			}
		}
	}
	for s, n := range m {
		if n != 0 {
			fatalf("inittask for %s is not schedulable", symName(s))
		}
	}
	return sched
}

func inittasks() {
	mainInittasks = inittaskOrder("main..inittask")
	if lookupName("runtime.runtime_inittasks", 0) != 0 {
		runtimeInittasks = inittaskOrder("runtime..inittask")
	}
	lap("inittasks", "%d + %d packages with initialization to run", len(mainInittasks), len(runtimeInittasks))
}

type methodsig struct {
	name string
	typ  uint32 // the method's function type
}

// A methodref is a method of a live type that is converted to an interface: its three R_METHODOFF
// relocations (type, interface-call entry, direct entry) are followed only once something can call it.
type methodref struct {
	methodsig
	src uint32 // the receiver type
	r   uint32 // index of the first relocation
}

type floodWorker struct {
	next    []uint32
	methods []methodref
	ifaceM  []methodsig
	generic []string
	reflect bool
	tmp     []uint32
	_       [64]byte
}

func (w *floodWorker) mark(c uint32) {
	if c != 0 && atomic.OrUint32(&state[c], stLive)&(stLive|stUndef) == 0 && c <= nslots {
		w.next = append(w.next, c)
	}
}

// setIface records that type c is converted to an interface. If it was already flooded as a plain type it
// is flooded again, this time following its methods' types and its element types as such.
func (w *floodWorker) setIface(c uint32) {
	if old := atomic.OrUint32(&state[c], stUsedInIface); old&stUsedInIface == 0 && old&stLive != 0 {
		w.next = append(w.next, c)
	}
}

func relocAt(relocs relocs, off int32) (reloc, bool) {
	for i := 0; i < relocs.Len(); i++ {
		if r := relocs.At(i); r.Off() == off {
			return r, true
		}
	}
	return reloc{}, false
}

// dataOf returns the content of the canonical definition c.
func dataOf(c uint32) []byte {
	if !isDef(c) {
		return nil
	}
	o, j := objOf(c)
	return o.data(j)
}

// nameData decodes a type:.namedata symbol: a flag byte, a varint length, the name.
func nameData(d []byte) string {
	if len(d) < 2 {
		return ""
	}
	n, k := binary.Uvarint(d[1:])
	return string(d[1+k : 1+k+int(n)])
}

func nameAt(o *object, relocs relocs, off int32) string {
	rl, ok := relocAt(relocs, off)
	if !ok {
		return ""
	}
	return nameData(dataOf(canon[o.ref(rl.Sym())]))
}

func typeKind(p []byte) abi.Kind { return abi.Kind(p[23]) }

func (w *floodWorker) process(c uint32) {
	o, j := objOf(c)
	s := o.sym(j)
	if s.ReflectMethod() {
		w.reflect = true
	}
	isType := s.IsGoType()
	uii := false
	if isType && allIface {
		atomic.OrUint32(&state[c], stUsedInIface) // (a measurement: every reachable type as if converted to an interface)
	}
	if isType && atomic.LoadUint32(&state[c])&stUsedInIface != 0 {
		uii = true
		if atomic.OrUint32(&state[c], stIfaceDone)&stIfaceDone != 0 {
			return
		}
	}
	relocs := o.relocs(j)
	w.tmp = w.tmp[:0]
	for i := 0; i < relocs.Len(); i++ {
		rl := relocs.At(i)
		t := objabi.RelocType(rl.Type())
		if t&objabi.R_WEAK != 0 {
			continue
		}
		switch t {
		case objabi.R_METHODOFF:
			if i+2 >= relocs.Len() {
				fatalf("%s: expect three consecutive R_METHODOFF relocs", o.name(j))
			}
			if uii {
				w.tmp = append(w.tmp, uint32(i))
				// the method's type is itself reachable through reflection
				if rs := canon[o.ref(rl.Sym())]; rs != 0 {
					w.setIface(rs)
				}
			}
			i += 2
			continue
		case objabi.R_USETYPE, objabi.R_INITORDER:
			continue
		case objabi.R_USEIFACE:
			rs := canon[o.ref(rl.Sym())]
			if !isDef(rs) {
				continue
			}
			if o2, j2 := objOf(rs); o2.sym(j2).IsItab() { // "the type of that itab"
				rl2, ok := relocAt(o2.relocs(j2), 8)
				if !ok {
					continue
				}
				if rs = canon[o2.ref(rl2.Sym())]; !isDef(rs) {
					continue
				}
			}
			w.setIface(rs)
			continue
		case objabi.R_USEIFACEMETHOD:
			if rs := canon[o.ref(rl.Sym())]; isDef(rs) {
				o2, j2 := objOf(rs)
				rr := o2.relocs(j2)
				off := int32(rl.Add())
				m := methodsig{name: nameAt(o2, rr, off)}
				if rl2, ok := relocAt(rr, off+4); ok {
					m.typ = canon[o2.ref(rl2.Sym())]
				}
				w.ifaceM = append(w.ifaceM, m)
			}
			continue
		case objabi.R_USENAMEDMETHOD:
			w.generic = append(w.generic, string(dataOf(canon[o.ref(rl.Sym())])))
			continue
		}
		rs := o.ref(rl.Sym())
		if rs == 0 {
			continue
		}
		rs = canon[rs]
		if uii && isDef(rs) && atomic.LoadUint32(&state[rs])&stUsedInIface == 0 {
			// a type reachable by reflection from one converted to an interface ([]T from T, ...)
			if o2, j2 := objOf(rs); o2.sym(j2).IsGoType() {
				if old := atomic.OrUint32(&state[rs], stUsedInIface|stLive); old&stUsedInIface == 0 || old&stLive == 0 {
					w.next = append(w.next, rs)
				}
				continue
			}
		}
		w.mark(rs)
	}
	if objabi.SymKind(s.Type()).IsText() {
		auxs := o.auxs(j)
		ninl := uint32(0)
		for i := range auxs {
			if auxs[i].Type() == goobj.AuxFuncInfo {
				fi := dataOf(canon[o.ref(auxs[i].Sym())])
				if fi == nil {
					break
				}
				l := (*goobj.FuncInfo)(nil).ReadFuncInfoLengths(fi)
				ninl = l.NumInlTree
				for k := uint32(0); k < ninl; k++ {
					node := (*goobj.FuncInfo)(nil).ReadInlTree(fi, l.InlTreeOff, k)
					if callee := o.ref(node.Func); callee != 0 {
						atomic.OrUint32(&state[canon[callee]], stNameNeeded)
					}
				}
				break
			}
		}
		for i := range auxs {
			a := &auxs[i]
			switch a.Type() {
			case goobj.AuxFuncdata:
				if g := o.ref(a.Sym()); g != 0 {
					c := canon[g]
					atomic.OrUint32(&state[c], stFuncdata)
					w.mark(c)
				}
			case goobj.AuxPcinline:
				if ninl == 0 {
					continue
				}
				fallthrough
			case goobj.AuxPcsp, goobj.AuxPcfile, goobj.AuxPcline, goobj.AuxPcdata:
				if g := o.ref(a.Sym()); g != 0 {
					atomic.OrUint32(&state[canon[g]], stPC)
				}
			}
		}
	}
	if len(w.tmp) != 0 {
		// which methods does this type have? (name and function type of each, from its uncommon type)
		p := o.data(j)
		if p[abi.TFlagOff(8)]&byte(abi.TFlagUncommon) == 0 {
			fatalf("%s: methods on a type with no uncommon type", o.name(j))
		}
		off := abi.RTypeSize(typeKind(p), 8)
		mcount := int(binary.LittleEndian.Uint16(p[off+4:]))
		moff := int(binary.LittleEndian.Uint32(p[off+8:]))
		if mcount != len(w.tmp) {
			fatalf("%s: %d method relocations for %d methods", o.name(j), len(w.tmp), mcount)
		}
		for i, ri := range w.tmp {
			at := int32(off + moff + 16*i)
			name := ""
			if ri > 0 {
				if prev := relocs.At(int(ri) - 1); prev.Off() == at {
					name = nameData(dataOf(canon[o.ref(prev.Sym())]))
				}
			}
			if name == "" {
				name = nameAt(o, relocs, at)
			}
			rm := relocs.At(int(ri))
			w.methods = append(w.methods, methodref{methodsig{name, canon[o.ref(rm.Sym())]}, c, ri})
		}
	}
}

func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func deadcode() {
	ws := make([]floodWorker, nthreads)
	w0 := &ws[0]
	var frontier []uint32
	root := func(c uint32) {
		if c != 0 {
			w0.mark(canon[c])
		}
	}
	root(lookup(*flagEntry, 0))
	root(lookup(*flagEntry, 1))
	root(lookupName("runtime.unreachableMethod", 0))
	root(lookupName("runtime.unreachableMethod", 1))
	root(lookupName("runtime.mapinitnoop", 1))
	root(lookupName("runtime.firstmoduledata", 0))
	for _, g := range mainInittasks {
		root(g)
	}
	for _, g := range runtimeInittasks {
		root(g)
	}
	if *flagNoDCE {
		for _, u := range units {
			for j := u.lo; j < min(u.hi, u.o.nalldef); j++ {
				if g := u.o.base + j; canon[g] == g {
					if k := objabi.SymKind(u.o.sym(j).Type()); k != objabi.Sxxx && k < objabi.SDWARFCUINFO {
						root(g)
					}
				}
			}
		}
	}
	ifaceMethod := map[methodsig]bool{}
	genericIfaceMethod := map[string]bool{}
	var markable []methodref
	reflectSeen := *flagNoDCE
	levels, rounds := 0, 0
	const grain = 64
	for {
		rounds++
		for {
			frontier = frontier[:0]
			for i := range ws {
				frontier = append(frontier, ws[i].next...)
				ws[i].next = ws[i].next[:0]
			}
			if len(frontier) == 0 {
				break
			}
			levels++
			par((len(frontier)+grain-1)/grain, func(w, i int) {
				for _, c := range frontier[i*grain : min((i+1)*grain, len(frontier))] {
					ws[w].process(c)
				}
			})
		}
		for i := range ws {
			w := &ws[i]
			for _, m := range w.ifaceM {
				ifaceMethod[m] = true
			}
			for _, n := range w.generic {
				genericIfaceMethod[n] = true
			}
			reflectSeen = reflectSeen || w.reflect
			markable = append(markable, w.methods...)
			w.ifaceM, w.generic, w.methods = w.ifaceM[:0], w.generic[:0], w.methods[:0]
		}
		// methods of live types that something can now call: an interface with that method is live, or
		// reflection may call any exported method
		keep := make([]bool, len(markable))
		const mgrain = 1024
		par((len(markable)+mgrain-1)/mgrain, func(w, i int) {
			for k := i * mgrain; k < min((i+1)*mgrain, len(markable)); k++ {
				m := &markable[k]
				if reflectSeen && isExported(m.name) || ifaceMethod[m.methodsig] || genericIfaceMethod[m.name] || *flagNoDCE || keepMethods {
					o, j := objOf(m.src)
					relocs := o.relocs(j)
					for d := uint32(0); d < 3; d++ {
						rl := relocs.At(int(m.r + d))
						if g := o.ref(rl.Sym()); g != 0 {
							ws[w].mark(canon[g])
						}
					}
				} else {
					keep[k] = true
				}
			}
		})
		rem := markable[:0]
		for k := range markable {
			if keep[k] {
				rem = append(rem, markable[k])
			}
		}
		markable = rem
		more := false
		for i := range ws {
			more = more || len(ws[i].next) != 0
		}
		if !more {
			break
		}
	}
	nlive := 0
	var dump []string
	if *flagStats || *flagDumpLive != "" {
		for g := uint32(1); g <= nslots; g++ {
			if state[g]&stLive != 0 && canon[g] == g {
				nlive++
				if *flagDumpLive != "" {
					dump = append(dump, symName(g))
				}
			}
		}
	}
	if *flagDumpLive != "" {
		sort.Strings(dump)
		os.WriteFile(*flagDumpLive, []byte(strings.Join(dump, "\n")+"\n"), 0o644)
	}
	lap("deadcode", "%d symbols live, %d levels in %d rounds, %d methods pruned, reflection %v", nlive, levels, rounds, len(markable), reflectSeen)
}

// keepMethods (FCLINK_KEEPMETHODS=1, a measurement): no method pruning: every method of a type used in an
// interface is live. What a compiler that follows references while it generates code would have to compile.
var keepMethods = os.Getenv("FCLINK_KEEPMETHODS") != ""

// allIface (FCLINK_KEEPMETHODS=2, a measurement): every live type keeps every method.
var allIface = os.Getenv("FCLINK_KEEPMETHODS") == "2"
