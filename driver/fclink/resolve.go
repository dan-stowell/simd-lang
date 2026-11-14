// resolve.go: symbol resolution. Every slot of every object is resolved to the id of its canonical
// definition, by the stock loader's rules: package-indexed references are a table read; references by
// name join definitions by name and ABI; content-addressed symbols join by their hash.
//
// The joins are partitioned hash joins. One parallel pass over the units writes a record per symbol that
// takes part (key, id, rank) into one of 256 partitions chosen by the key's top bits; then each partition,
// small enough to stay in a core's cache, is grouped by key on its own thread and every record's slot
// learns its group's winner. The winner is the maximum of a total order, so the result does not depend on
// which thread saw what first:
//
//	a definition beats a reference; one that may not be duplicated beats one that may (DUPOK);
//	then one with content (or code) beats one without (BSS);
//	then the larger one wins (the stock loader's rule for DUPOK and BSS symbols and for hashed ones);
//	then the one in the earlier object, at the lower index.
//
// A name nothing defines resolves to its first reference: that slot stands for the undefined symbol.
// Definitions referenced by package index take part in the join by name only if some object refers to
// their name (or the linker itself looks it up): a bit filter of the referenced names decides, so most of
// them cost a hash and one bit test.
package fclinklib

import (
	"cmd/internal/goobj"
	"cmd/internal/objabi"
	"hash/maphash"
	"sync/atomic"
	"unsafe"
)

var (
	canon []uint32 // per id: the id of the canonical definition (itself, for one); 0: none
	state []uint32 // per id: st* bits, written with atomic operations while passes run in parallel
	addr  []uint32 // per id: the address (pc-value tables: the offset in pctab); 0: not in the output
	aux   []uint32 // per id: for a function, the offset of its name in funcnametab

	builtins   []uint32
	synthNames []string // symbols the linker makes that nothing refers to by name: id nslots+1+i
	nameParts  [][nParts]plist[nameRec]
	nameFilter []uint64
)

const (
	stLive        uint32 = 1 << iota // reachable
	stUsedInIface                    // a type converted to an interface: its methods may be called dynamically
	stIfaceDone                      // (deadcode) already flooded as such
	stPC                             // a pc-value table of a live function: goes in pctab
	stFuncdata                       // funcdata of a live function: goes in the go:func.* area
	stNameNeeded                     // a function inlined into a live one: its name goes in funcnametab
	stOverride                       // the linker writes this symbol's content itself
	stUndef                          // a reference that stands for a symbol no object defines
	stBucketShift = 8                // bits 8..12: the bucket; bits 13..16: the alignment class

	nParts     = 256
	filterBits = 1 << 23

	rankNoDup   = 1 << 31
	rankContent = 1 << 30
	rankDef     = 1 << 29
	rankIface   = 1 << 28 // (not part of the order: the UsedInIface flag rides along)
)

type nameRec struct {
	key  uint64
	name *byte
	nlen uint32
	gid  uint32
	rank uint32
	abi  uint16
}

type hashRec struct {
	k1, k2    uint64
	gid, size uint32
}

type hash64Rec struct {
	key       uint64
	gid, size uint32
}

var seed = maphash.MakeSeed()

func nameKey(name string, abi uint16) uint64 {
	return maphash.String(seed, name) ^ uint64(abi)*0x9E3779B97F4A7C15
}

func mix64(h uint64) uint64 {
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	return h ^ h>>31
}

func (r *nameRec) str() string { return unsafe.String(r.name, r.nlen) }

func mkNameRec(name string, abi uint16, gid, rank uint32) nameRec {
	return nameRec{nameKey(name, abi), unsafe.StringData(name), uint32(len(name)), gid, rank, abi}
}

// isDef: is canonical id c a definition in some object?
func isDef(c uint32) bool { return c != 0 && c <= nslots && state[c]&stUndef == 0 }

// lookupName finds the canonical symbol of a name: a definition, or the reference that stands for an
// undefined symbol; 0 if no object mentions the name. Names that only package-indexed definitions carry
// are found only if they were announced with wantName before resolution.
func lookupName(name string, abi uint16) uint32 {
	key := nameKey(name, abi)
	for w := range nameParts {
		for _, recs := range nameParts[w][key>>56].chunks {
			for i := range recs {
				if r := &recs[i]; r.key == key && r.abi == abi && r.str() == name {
					return canon[r.gid]
				}
			}
		}
	}
	return 0
}

func lookup(name string, abi uint16) uint32 { return lookupName(name, abi) }

func wantName(name string, abi uint16) {
	k := nameKey(name, abi) % filterBits
	nameFilter[k>>6] |= 1 << (k & 63)
}

// synth makes a symbol of the linker's own (nothing refers to it by name) and returns its id.
func synth(name string) uint32 {
	g := nslots + 1 + uint32(len(synthNames))
	synthNames = append(synthNames, name)
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
		return synthNames[gid-nslots-1]
	}
	o, j := objOf(gid)
	return o.name(j)
}

func rank(o *object, j uint32, s symv) uint32 {
	r := min(s.Siz(), 1<<28-1) | rankDef
	if s.UsedInIface() {
		r |= rankIface
	}
	if s.Dupok() {
		return r
	}
	r |= rankNoDup
	if k := objabi.SymKind(s.Type()); k.IsText() || o.dataSize(j) != 0 {
		r |= rankContent
	}
	return r
}

// The names the linker itself looks up; definitions by package index of these names must join by name.
var linkerNames = []string{"runtime.firstmoduledata", "runtime.lastmoduledatap", "runtime.runtime_inittasks", "main..inittask",
	"runtime..inittask", "runtime.unreachableMethod", "runtime.mapinitnoop", "runtime.deferreturn", "runtime.memProfileInternal",
	"runtime.disableMemoryProfiling", "main.main"}

type resolveWorker struct {
	names  [nParts]plist[nameRec]
	nslab  []nameRec
	hslab  []hashRec
	h6slab []hash64Rec
	hashes [nParts]plist[hashRec]
	h64s   [nParts]plist[hash64Rec]
	table  []uint32
	grp    []uint32
	best   []uint64
	ndup   []uint8
	_      [64]byte
}

func resolve() {
	n := int(nslots) + 1
	canon = make([]uint32, n, n+64)
	state = make([]uint32, n, n+64)
	addr = make([]uint32, n, n+64)
	aux = make([]uint32, n, n+64)
	nameFilter = make([]uint64, filterBits/64)
	ws := make([]resolveWorker, nthreads)

	// 1. records for the definitions and references by name and for the content-addressed definitions
	par(len(units), func(w, ui int) {
		u, rw := units[ui], &ws[w]
		o := u.o
		h64End, hEnd := o.ndef+o.nh64, o.ndef+o.nh64+o.nh
		for j := max(u.lo, o.ndef); j < u.hi; j++ {
			s := o.sym(j)
			gid := o.base + j
			switch {
			case j < h64End:
				h := o.hash64(j - o.ndef)
				k := mix64(h)
				rw.h64s[k>>56].add(&rw.h6slab, hash64Rec{h, gid, s.Siz()})
			case j < hEnd:
				k1, k2 := o.hash(j - h64End)
				rw.hashes[k1>>56].add(&rw.hslab, hashRec{k1, k2, gid, s.Siz()})
			default:
				rk := uint32(0)
				if j < o.nalldef {
					if s.ABI() == goobj.SymABIstatic || o.nameLen(j) == 0 {
						canon[gid] = gid // file-local, or an unnamed auxiliary symbol
						continue
					}
					rk = rank(o, j, s)
				} else if s.UsedInIface() {
					rk = rankIface
				}
				rec := mkNameRec(o.name(j), s.ABI(), gid, rk)
				rw.names[rec.key>>56].add(&rw.nslab, rec)
				k := rec.key % filterBits
				atomic.OrUint64(&nameFilter[k>>6], 1<<(k&63))
			}
		}
	})
	for i := 0; i < goobj.NBuiltin(); i++ {
		name, abi := goobj.BuiltinName(i)
		wantName(name, uint16(abi))
	}
	for _, name := range linkerNames {
		wantName(name, 0)
		wantName(name, 1)
	}
	for name := range strdata {
		wantName(name, 0)
	}
	wantName(*flagEntry, 0)
	wantName(*flagEntry, 1)
	// 2. definitions by package index: themselves, unless something may refer to their name
	par(len(units), func(w, ui int) {
		u, rw := units[ui], &ws[w]
		o := u.o
		for j := u.lo; j < min(u.hi, o.ndef); j++ {
			s := o.sym(j)
			gid := o.base + j
			if s.ABI() != goobj.SymABIstatic && o.nameLen(j) != 0 {
				name := o.name(j)
				key := nameKey(name, s.ABI())
				if k := key % filterBits; nameFilter[k>>6]>>(k&63)&1 != 0 {
					rw.names[key>>56].add(&rw.nslab, nameRec{key, unsafe.StringData(name), uint32(len(name)), gid, rank(o, j, s), s.ABI()})
					continue
				}
			}
			canon[gid] = gid
			if s.UsedInIface() {
				atomic.OrUint32(&state[gid], stUsedInIface)
			}
		}
	})
	// 3. the joins, one partition at a time
	nameParts = make([][nParts]plist[nameRec], len(ws))
	for w := range ws {
		nameParts[w] = ws[w].names
	}
	var nrec [3]atomic.Int64
	par(3*nParts, func(w, task int) {
		rw := &ws[w]
		p, kind := task%nParts, task/nParts
		n := 0
		for i := range ws {
			switch kind {
			case 0:
				n += ws[i].names[p].n
			case 1:
				n += ws[i].hashes[p].n
			case 2:
				n += ws[i].h64s[p].n
			}
		}
		if n == 0 {
			return
		}
		nrec[kind].Add(int64(n))
		size := 64
		for size < 2*n {
			size <<= 1
		}
		if cap(rw.table) < size {
			rw.table = make([]uint32, size)
		}
		if cap(rw.grp) < n {
			rw.grp, rw.best, rw.ndup = make([]uint32, n+n/2), make([]uint64, n+n/2), make([]uint8, n+n/2)
		}
		table, grp, best, ndup := rw.table[:size], rw.grp[:n], rw.best[:n], rw.ndup[:n]
		clear(table)
		mask := uint64(size - 1)
		// group: grp[i] is the index (over the partition's records, in worker order) of the first record
		// with record i's key; best[that] is the group's maximum of rank<<32 | ^id
		i := uint32(0)
		switch kind {
		case 0:
			flat := make([]*nameRec, 0, n)
			for wi := range ws {
				for _, recs := range ws[wi].names[p].chunks {
					for k := range recs {
						r := &recs[k]
						flat = append(flat, r)
						val := uint64(r.rank&^rankIface)<<32 | uint64(^r.gid)
						h := r.key & mask
						for {
							t := table[h]
							if t == 0 {
								table[h] = i + 1
								grp[i], best[i], ndup[i] = i, val, uint8(r.rank>>28&1)
								break
							}
							if f := flat[t-1]; f.key == r.key && f.abi == r.abi && f.str() == r.str() {
								grp[i] = t - 1
								if r.rank&(rankNoDup|rankContent) == rankNoDup|rankContent && best[t-1]>>62 == 3 {
									o1, _ := objOf(r.gid)
									o2, _ := objOf(^uint32(best[t-1]))
									errorf("duplicated definition of symbol %s, from %s and %s", r.str(), o2.pkg, o1.pkg)
								}
								best[t-1] = max(best[t-1], val)
								ndup[t-1] |= uint8(r.rank >> 28 & 1)
								break
							}
							h = (h + 1) & mask
						}
						i++
					}
				}
			}
			for k, r := range flat {
				g := grp[k]
				c := ^uint32(best[g])
				canon[r.gid] = c
				if uint32(k) == g {
					if best[g]>>32&rankDef == 0 {
						atomic.OrUint32(&state[c], stUndef)
					}
					if ndup[g] != 0 {
						atomic.OrUint32(&state[c], stUsedInIface)
					}
				}
			}
		case 1:
			flat := make([]*hashRec, 0, n)
			for wi := range ws {
				for _, recs := range ws[wi].hashes[p].chunks {
					for k := range recs {
						r := &recs[k]
						flat = append(flat, r)
						val := uint64(r.size)<<32 | uint64(^r.gid)
						h := r.k1 & mask
						for {
							t := table[h]
							if t == 0 {
								table[h] = i + 1
								grp[i], best[i] = i, val
								break
							}
							if f := flat[t-1]; f.k1 == r.k1 && f.k2 == r.k2 {
								grp[i] = t - 1
								best[t-1] = max(best[t-1], val)
								break
							}
							h = (h + 1) & mask
						}
						i++
					}
				}
			}
			for k, r := range flat {
				canon[r.gid] = ^uint32(best[grp[k]])
			}
		case 2:
			flat := make([]*hash64Rec, 0, n)
			for wi := range ws {
				for _, recs := range ws[wi].h64s[p].chunks {
					for k := range recs {
						r := &recs[k]
						flat = append(flat, r)
						val := uint64(r.size)<<32 | uint64(^r.gid)
						h := mix64(r.key) & mask
						for {
							t := table[h]
							if t == 0 {
								table[h] = i + 1
								grp[i], best[i] = i, val
								break
							}
							if flat[t-1].key == r.key {
								grp[i] = t - 1
								best[t-1] = max(best[t-1], val)
								break
							}
							h = (h + 1) & mask
						}
						i++
					}
				}
			}
			for k, r := range flat {
				canon[r.gid] = ^uint32(best[grp[k]])
			}
		}
	})
	builtins = make([]uint32, goobj.NBuiltin())
	for i := range builtins {
		name, abi := goobj.BuiltinName(i)
		if g := lookupName(name, uint16(abi)); isDef(g) {
			builtins[i] = g
		}
	}
	// 4. "this type is converted to an interface" flags on references by package index
	par(len(objs), func(_, i int) {
		o := objs[i]
		for k, n := 0, o.nrefFlags(); k < n; k++ {
			rf := o.r.RefFlags(k)
			if rf.Flag2()&goobj.SymFlagUsedInIface != 0 {
				if g := o.ref(rf.Sym()); g != 0 {
					atomic.OrUint32(&state[canon[g]], stUsedInIface)
				}
			}
		}
	})
	lap("resolve", "%d slots; joined: %d by name, %d + %d by content hash", nslots, nrec[0].Load(), nrec[1].Load(), nrec[2].Load())
}
