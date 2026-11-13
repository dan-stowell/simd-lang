// resolve.go: symbol resolution. Every slot of every object is resolved to the id of its canonical
// definition, by the stock loader's rules: package-indexed references by table read; references by name
// through a table keyed by name and ABI; content-addressed symbols through tables keyed by their hash.
// The tables are open-addressing arrays filled by all cores at once without locks: a claim installs its key
// with a compare-and-swap and then raises the slot's value to the best definition seen, where "best" is a
// total order (so the result does not depend on which thread got there first):
//
//	a definition that may not be duplicated beats one that may (DUPOK);
//	then one with content (or code) beats one without (BSS);
//	then the larger one wins (the stock loader's rule for DUPOK and BSS symbols and for hashed ones);
//	then the one in the earlier object, at the lower index.
package main

import (
	"cmd/internal/goobj"
	"cmd/internal/objabi"
	"hash/maphash"
	"sync/atomic"
)

var (
	canon []uint32 // per id: the id of the canonical definition (itself, for one); 0: none
	state []uint32 // per id: st* bits, written with atomic operations while passes run in parallel
	addr  []uint32 // per id: the address (pc-value tables: the offset in pctab); 0: not in the output
	aux   []uint32 // per id: for a function, the offset of its name in funcnametab

	nameTab, hash64Tab, hashTab table
	hash64Zero                  atomic.Uint64 // the slot of the 64-bit hash whose mixed key is 0
	builtins                    []uint32

	synthByName = map[synthKey]uint32{} // symbols no object defines: referenced by name, or made by the linker
	synthNames  []synthKey              // id nslots+1+i
)

type synthKey struct {
	name string
	abi  uint16
}

const (
	stLive        uint32 = 1 << iota // reachable
	stUsedInIface                    // a type converted to an interface: its methods may be called dynamically
	stIfaceDone                      // (deadcode) already flooded as such
	stPC                             // a pc-value table of a live function: goes in pctab
	stFuncdata                       // funcdata of a live function: goes in the go:func.* area
	stNameNeeded                     // a function inlined into a live one: its name goes in funcnametab
	stOverride                       // the linker writes this symbol's content itself
	stBucketShift = 8                // bits 8..12: the bucket; bits 13..16: the alignment class
)

// table: key and value words interleaved. Key 0 is an empty slot. A value is rank<<32 | ^id, maximised.
type table struct {
	s    []uint64
	mask uint64
}

func (t *table) init(n int) {
	size := 1024
	for size < 2*n {
		size <<= 1
	}
	t.s = make([]uint64, 2*size)
	t.mask = uint64(size - 1)
}

const (
	kindName = iota
	kindHash64
	kindHash
)

var seed = maphash.MakeSeed()

func nameKey(name string, abi uint16) uint64 {
	k := maphash.String(seed, name) ^ uint64(abi)*0x9E3779B97F4A7C15
	if k == 0 {
		k = 1
	}
	return k
}

func mix64(h uint64) uint64 { // a bijection: equal keys mean equal hashes
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	return h ^ h>>31
}

func sameIdentity(kind int, gid uint32, o *object, j uint32) bool {
	o2, j2 := objOf(gid)
	if kind == kindHash {
		return *o.r.Hash(j - o.ndef - o.nh64) == *o2.r.Hash(j2 - o2.ndef - o2.nh64)
	}
	s, s2 := o.r.Sym(j), o2.r.Sym(j2)
	return s.ABI() == s2.ABI() && s.Name(o.r) == s2.Name(o2.r)
}

// claim enters definition j of o under key and returns the table slot that will hold the winner.
func (t *table) claim(kind int, key, val uint64, o *object, j uint32) uint32 {
	for i := key & t.mask; ; i = (i + 1) & t.mask {
		kp, vp := &t.s[2*i], &t.s[2*i+1]
		k := atomic.LoadUint64(kp)
		created := false
		if k == 0 {
			if atomic.CompareAndSwapUint64(kp, 0, key) {
				k, created = key, true
			} else {
				k = atomic.LoadUint64(kp)
			}
		}
		if k != key {
			continue
		}
		if !created && kind != kindHash64 {
			// the same 64 bits: the same name (or hash)? compare with whoever holds the slot
			v := atomic.LoadUint64(vp)
			for v == 0 {
				v = atomic.LoadUint64(vp) // its first claimant is between its two stores
			}
			if !sameIdentity(kind, ^uint32(v), o, j) {
				continue
			}
		}
		for {
			old := atomic.LoadUint64(vp)
			if old >= val || atomic.CompareAndSwapUint64(vp, old, val) {
				return uint32(i)
			}
		}
	}
}

func lookupName(name string, abi uint16) uint32 {
	key := nameKey(name, abi)
	t := &nameTab
	for i := key & t.mask; ; i = (i + 1) & t.mask {
		switch t.s[2*i] {
		case 0:
			return 0
		case key:
			gid := ^uint32(t.s[2*i+1])
			o, j := objOf(gid)
			if s := o.r.Sym(j); s.ABI() == abi && s.Name(o.r) == name {
				return gid
			}
		}
	}
}

// lookup finds a symbol by name: an object's definition, or a symbol the linker makes; 0 if neither exists.
func lookup(name string, abi uint16) uint32 {
	if g := lookupName(name, abi); g != 0 {
		return g
	}
	return synthByName[synthKey{name, abi}]
}

// synth returns the id of the linker-made symbol of this name, creating it if nothing referred to it.
func synth(name string) uint32 {
	k := synthKey{name, 0}
	if g, ok := synthByName[k]; ok {
		return g
	}
	g := nslots + 1 + uint32(len(synthNames))
	synthByName[k] = g
	synthNames = append(synthNames, k)
	canon = append(canon, g)
	state = append(state, 0)
	addr = append(addr, 0)
	aux = append(aux, 0)
	return g
}

func symName(gid uint32) string {
	if gid == 0 {
		return "<nil>"
	}
	if gid > nslots {
		return synthNames[gid-nslots-1].name
	}
	o, j := objOf(gid)
	return o.r.Sym(j).Name(o.r)
}

// rank orders the definitions of one name; see the top of the file.
func rank(o *object, j uint32, s *goobj.Sym) uint64 {
	siz := uint64(s.Siz())
	if siz >= 1<<30 {
		siz = 1<<30 - 1
	}
	if s.Dupok() {
		return siz
	}
	r := siz | 1<<31
	if k := objabi.SymKind(s.Type()); k.IsText() || o.r.DataSize(j) != 0 {
		r |= 1 << 30
	}
	return r
}

func isClaimed(o *object, j uint32, s *goobj.Sym) (tab int, ok bool) {
	switch {
	case j < o.ndef, j >= o.ndef+o.nh64+o.nh:
		if s.ABI() == goobj.SymABIstatic || s.NameLen(o.r) == 0 {
			return 0, false // file-local, or an unnamed auxiliary symbol
		}
		return kindName, true
	case j < o.ndef+o.nh64:
		return kindHash64, true
	}
	return kindHash, true
}

func resolve() {
	n := int(nslots) + 1
	canon = make([]uint32, n, n+4096)
	state = make([]uint32, n, n+4096)
	addr = make([]uint32, n, n+4096)
	aux = make([]uint32, n, n+4096)
	var nnamed, nh64, nh int
	for _, o := range objs {
		nnamed += int(o.ndef + o.nnonpkg)
		nh64 += int(o.nh64)
		nh += int(o.nh)
	}
	nameTab.init(nnamed)
	hash64Tab.init(nh64)
	hashTab.init(nh)

	// 1. every definition claims its key; canon holds the table slot for now
	par(len(units), func(_, ui int) {
		u := units[ui]
		o, r := u.o, u.o.r
		for j := u.lo; j < min(u.hi, o.nalldef); j++ {
			s := r.Sym(j)
			gid := o.base + j
			tab, ok := isClaimed(o, j, s)
			if !ok {
				canon[gid] = gid
				continue
			}
			switch tab {
			case kindName:
				canon[gid] = nameTab.claim(kindName, nameKey(s.Name(r), s.ABI()), rank(o, j, s)<<32|uint64(^gid), o, j)
			case kindHash64:
				val := uint64(s.Siz())<<32 | uint64(^gid)
				key := mix64(r.Hash64(j - o.ndef))
				if key == 0 {
					for {
						old := hash64Zero.Load()
						if old >= val || hash64Zero.CompareAndSwap(old, val) {
							break
						}
					}
					canon[gid] = ^uint32(0)
				} else {
					canon[gid] = hash64Tab.claim(kindHash64, key, val, o, j)
				}
			case kindHash:
				h := r.Hash(j - o.ndef - o.nh64)
				key := uint64(h[0]) | uint64(h[1])<<8 | uint64(h[2])<<16 | uint64(h[3])<<24 | uint64(h[4])<<32 | uint64(h[5])<<40 | uint64(h[6])<<48 | uint64(h[7])<<56
				if key == 0 {
					key = 1
				}
				canon[gid] = hashTab.claim(kindHash, key, uint64(s.Siz())<<32|uint64(^gid), o, j)
			}
		}
	})
	builtins = make([]uint32, goobj.NBuiltin())
	for i := range builtins {
		name, abi := goobj.BuiltinName(i)
		builtins[i] = lookupName(name, uint16(abi))
	}

	// 2. every slot learns its canonical definition
	par(len(units), func(_, ui int) {
		u := units[ui]
		o, r := u.o, u.o.r
		for j := u.lo; j < u.hi; j++ {
			s := r.Sym(j)
			gid := o.base + j
			var c uint32
			if j >= o.nalldef {
				c = lookupName(s.Name(r), s.ABI())
				if c == 0 {
					u.unres = append(u.unres, unresRef{gid, s.Name(r), s.ABI()})
					continue
				}
				canon[gid] = c
			} else {
				tab, ok := isClaimed(o, j, s)
				if !ok {
					continue
				}
				var val uint64
				switch slot := canon[gid]; {
				case tab == kindName:
					val = nameTab.s[2*uint64(slot)+1]
				case tab == kindHash:
					val = hashTab.s[2*uint64(slot)+1]
				case slot == ^uint32(0):
					val = hash64Zero.Load()
				default:
					val = hash64Tab.s[2*uint64(slot)+1]
				}
				c = ^uint32(val)
				canon[gid] = c
				if c != gid && tab == kindName && val>>62 == 3 && rank(o, j, s)>>30 == 3 {
					o2, _ := objOf(c)
					errorf("duplicated definition of symbol %s, from %s and %s", s.Name(r), o2.pkg, o.pkg)
				}
			}
			if s.UsedInIface() {
				atomic.OrUint32(&state[c], stUsedInIface)
			}
		}
	})
	// references by name that no object defines: symbols the linker makes (runtime.text, ...), weak
	// references, references from code that will turn out dead
	for _, u := range units {
		for _, x := range u.unres {
			k := synthKey{x.name, x.abi}
			g, ok := synthByName[k]
			if !ok {
				g = nslots + 1 + uint32(len(synthNames))
				synthByName[k] = g
				synthNames = append(synthNames, k)
			}
			canon[x.gid] = g
		}
	}
	nunres := len(synthNames)
	for i := 0; i < nunres; i++ {
		g := nslots + 1 + uint32(i)
		canon, state, addr, aux = append(canon, g), append(state, 0), append(addr, 0), append(aux, 0)
	}
	for i, b := range builtins {
		if b == 0 {
			name, abi := goobj.BuiltinName(i)
			k := synthKey{name, uint16(abi)}
			if g, ok := synthByName[k]; ok {
				builtins[i] = g
			}
		}
	}
	// 3. "this type is converted to an interface" flags on references by package index
	par(len(objs), func(_, i int) {
		o := objs[i]
		for k, n := 0, o.r.NRefFlags(); k < n; k++ {
			rf := o.r.RefFlags(k)
			if rf.Flag2()&goobj.SymFlagUsedInIface != 0 {
				if g := o.ref(rf.Sym()); g != 0 {
					atomic.OrUint32(&state[canon[g]], stUsedInIface)
				}
			}
		}
	})
	lap("resolve", "%d slots, %d named and %d+%d hashed definitions claimed, %d names no object defines", nslots, nnamed, nh64, nh, nunres)
}
