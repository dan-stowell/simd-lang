// f4link: a spike for rung F4. How long do the parts of a link that every design must do take on the
// real objects of a build, done the data-parallel way? It reads every archive of an importcfg.link,
// resolves every symbol reference (by package index, by name, by content hash), lays every defined
// symbol out by prefix sums, copies the data into one image and applies every relocation, on all cores.
// It is NOT a linker: no dead-code elimination, no pclntab, no symbol table, no ELF, relocation types
// are not distinguished (each writes target+addend at its offset). It measures; it does not produce a
// program. Built inside the Go tree (it uses cmd/internal/goobj): see mk.sh.
//
//	f4link <importcfg.link> [more archives...]
package main

import (
	"bytes"
	"cmd/internal/goobj"
	"cmd/internal/objabi"
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type object struct {
	pkg                                  string
	r                                    *goobj.Reader
	ndef, nh64, nh, nnonpkg, nref, total int
	canon                                []uint64 // per symbol index: global id of the symbol it resolves to (0 = unresolved)
	addr                                 []int64  // per defined symbol that this object owns: offset in the image, else -1
	base                                 int64
	size                                 int64
	gid0                                 uint64 // global id of symbol 0 is gid0+1
}

// The resolution table: open addressing over 64-bit keys, filled by all cores at once without locks.
// A key is a hash of what identifies a definition across objects: its name and ABI, or the first eight
// bytes of its content hash. The value is the global id of the lowest-numbered object's definition
// (min wins, so the result does not depend on which thread got there first). Two different names with
// the same 64-bit key would be merged: at a million keys the chance is about 3e-8; a linker would
// compare the full keys on a hit, this spike does not (F4LINK_CHECK=1 lists any that collide).
type table struct {
	keys []atomic.Uint64
	vals []atomic.Uint64
	mask uint64
}

func newTable(n int) *table {
	size := 1
	for size < 2*n {
		size <<= 1
	}
	return &table{keys: make([]atomic.Uint64, size), vals: make([]atomic.Uint64, size), mask: uint64(size - 1)}
}

func (t *table) claim(key, gid uint64) {
	if key == 0 { // 0 marks an empty slot
		key = 1
	}
	for i := (key * 0x9E3779B97F4A7C15) >> 20 & t.mask; ; i = (i + 1) & t.mask {
		k := t.keys[i].Load()
		if k == 0 {
			if !t.keys[i].CompareAndSwap(0, key) {
				k = t.keys[i].Load()
			} else {
				k = key
			}
		}
		if k == key {
			for {
				old := t.vals[i].Load()
				if old != 0 && old <= gid || t.vals[i].CompareAndSwap(old, gid) {
					return
				}
			}
		}
	}
}

func (t *table) get(key uint64) uint64 {
	if key == 0 { // 0 marks an empty slot
		key = 1
	}
	for i := (key * 0x9E3779B97F4A7C15) >> 20 & t.mask; ; i = (i + 1) & t.mask {
		switch t.keys[i].Load() {
		case key:
			return t.vals[i].Load()
		case 0:
			return 0
		}
	}
}

var seed = maphash.MakeSeed()
var noNames = os.Getenv("F4LINK_NONAMES") != ""

func nameKey(name string, abi uint16) uint64 {
	return maphash.String(seed, name) ^ uint64(abi)*0x9E3779B97F4A7C15
}

// hash64Key: a short symbol's "hash" is its content, so the key must depend on every bit of it. (The
// first version forced the key's top bit to keep it non-zero, which merged float constants that differ
// only in sign, +x and -x: 19 false merges in MinIO, found by F4LINK_CHECK.)
func hash64Key(h uint64) uint64 {
	h ^= 0x1111
	h = (h ^ h>>30) * 0xBF58476D1CE4E5B9
	h = (h ^ h>>27) * 0x94D049BB133111EB
	return h ^ h>>31
}

func hashKey(h []byte) uint64 { return maphash.Bytes(seed, h) ^ 0x2222 } // all 32 bytes: short symbols' "hashes" share prefixes

func par(n int, f func(i int)) {
	var wg sync.WaitGroup
	var next atomic.Int64
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				f(i)
			}
		}()
	}
	wg.Wait()
}

func main() {
	t0 := time.Now()
	lap := func(what string, extra string) {
		fmt.Printf("%-44s %7.1f ms  %s\n", what, float64(time.Since(t0).Microseconds())/1000, extra)
		t0 = time.Now()
	}
	cfg, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	type lib struct{ pkg, file string }
	var libs []lib
	for _, line := range strings.Split(string(cfg), "\n") {
		if rest, ok := strings.CutPrefix(line, "packagefile "); ok {
			p, f, _ := strings.Cut(rest, "=")
			libs = append(libs, lib{p, f})
		}
	}
	// ---- 1. map every archive, find the Go objects in it
	perLib := make([][]*object, len(libs))
	var mapped atomic.Int64
	par(len(libs), func(i int) {
		f, err := os.Open(libs[i].file)
		if err != nil {
			panic(err)
		}
		st, _ := f.Stat()
		b, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE)
		f.Close()
		if err != nil {
			panic(err)
		}
		mapped.Add(st.Size())
		off := 8 // "!<arch>\n"
		for off+60 <= len(b) {
			name := strings.TrimSpace(string(b[off : off+16]))
			size, _ := strconv.Atoi(strings.TrimSpace(string(b[off+48 : off+58])))
			body := b[off+60 : off+60+size]
			off += 60 + size + size&1
			if name == "__.PKGDEF" || !bytes.HasPrefix(body, []byte("go object ")) {
				continue
			}
			k := bytes.Index(body, []byte("\n!\n"))
			if k < 0 {
				continue
			}
			r := goobj.NewReaderFromBytes(body[k+3:], true)
			if r == nil {
				continue
			}
			o := &object{pkg: libs[i].pkg, r: r, ndef: r.NSym(), nh64: r.NHashed64def(), nh: r.NHasheddef(), nnonpkg: r.NNonpkgdef(), nref: r.NNonpkgref()}
			o.total = o.ndef + o.nh64 + o.nh + o.nnonpkg + o.nref
			perLib[i] = append(perLib[i], o)
		}
	})
	var objs []*object
	primary := map[string]*object{} // package path -> the object that holds its package-indexed symbols
	var nsyms uint64
	for i := range perLib {
		for _, o := range perLib[i] {
			o.gid0 = nsyms
			nsyms += uint64(o.total)
			objs = append(objs, o)
			if _, ok := primary[o.pkg]; !ok && o.ndef > 0 {
				primary[o.pkg] = o
			}
		}
	}
	var c [5]int
	for _, o := range objs {
		c[0] += o.ndef
		c[1] += o.nh64
		c[2] += o.nh
		c[3] += o.nnonpkg
		c[4] += o.nref
	}
	lap("1. map archives, find objects", fmt.Sprintf("%d archives, %d objects, %d MB mapped; symbols: %d by package index, %d + %d by content hash, %d defined by name, %d references by name",
		len(libs), len(objs), mapped.Load()>>20, c[0], c[1], c[2], c[3], c[4]))

	// ---- 2. claim: every definition that is found by name or by content hash, into a sharded table
	tab := newTable(int(nsyms))
	var nclaims atomic.Int64
	par(len(objs), func(i int) {
		o := objs[i]
		r := o.r
		n := 0
		for j := o.ndef; j < o.ndef+o.nh64; j++ {
			tab.claim(hash64Key(r.Hash64(uint32(j-o.ndef))), o.gid0+uint64(j)+1)
			n++
		}
		for j := o.ndef + o.nh64; j < o.ndef+o.nh64+o.nh; j++ {
			h := r.Hash(uint32(j - o.ndef - o.nh64))
			tab.claim(hashKey(h[:]), o.gid0+uint64(j)+1)
			n++
		}
		for j := o.ndef + o.nh64 + o.nh; j < o.ndef+o.nh64+o.nh+o.nnonpkg; j++ {
			s := r.Sym(uint32(j))
			if s.ABI() == goobj.SymABIstatic {
				continue // file-local
			}
			tab.claim(nameKey(s.Name(r), s.ABI()), o.gid0+uint64(j)+1)
			n++
		}
		// Package-indexed definitions are found by (package, index), a table read; they are entered by
		// name as well because assembly and linknames refer to them that way and this spike does not
		// know which ones (F4LINK_NONAMES=1 leaves them out: what dense ids everywhere would cost).
		for j := 0; j < o.ndef && !noNames; j++ {
			s := r.Sym(uint32(j))
			if s.ABI() == goobj.SymABIstatic {
				continue
			}
			tab.claim(nameKey(s.Name(r), s.ABI()), o.gid0+uint64(j)+1)
			n++
		}
		nclaims.Add(int64(n))
	})
	if os.Getenv("F4LINK_CHECK") != "" {
		// which different identities share a 64-bit key? (single-threaded, slow, for debugging the table)
		seen := map[uint64]string{}
		bad := 0
		note := func(key uint64, id string) {
			if key == 0 { // 0 marks an empty slot
				key = 1
			}
			if old, ok := seen[key]; !ok {
				seen[key] = id
			} else if old != id {
				if bad++; bad <= 12 {
					fmt.Printf("  same key: %q and %q\n", old, id)
				}
			}
		}
		for _, o := range objs {
			r := o.r
			for j := o.ndef; j < o.ndef+o.nh64; j++ {
				h := r.Hash64(uint32(j - o.ndef))
				note(hash64Key(h), fmt.Sprintf("h64 %x", h))
			}
			for j := o.ndef + o.nh64; j < o.ndef+o.nh64+o.nh; j++ {
				h := r.Hash(uint32(j - o.ndef - o.nh64))
				note(hashKey(h[:]), fmt.Sprintf("h %x", h[:]))
			}
			for j := 0; j < o.ndef+o.nh64+o.nh+o.nnonpkg; j++ {
				if j >= o.ndef && j < o.ndef+o.nh64+o.nh {
					continue
				}
				s := r.Sym(uint32(j))
				if s.ABI() == goobj.SymABIstatic {
					continue
				}
				note(nameKey(s.Name(r), s.ABI()), fmt.Sprintf("name %s abi %d", s.Name(r), s.ABI()))
			}
		}
		fmt.Printf("  check: %d identities collide with another under 64-bit keys; %d distinct keys\n", bad, len(seen))
	}
	var distinct atomic.Int64
	par(64, func(c int) {
		n := 0
		for i := c * len(tab.keys) / 64; i < (c+1)*len(tab.keys)/64; i++ {
			if tab.keys[i].Load() != 0 {
				n++
			}
		}
		distinct.Add(int64(n))
	})
	lap("2. claim names and content hashes", fmt.Sprintf("%d claims, %d distinct", nclaims.Load(), distinct.Load()))

	// ---- 3. resolve: every symbol slot of every object to the global id of its definition
	var unresolved atomic.Int64
	par(len(objs), func(i int) {
		o := objs[i]
		r := o.r
		o.canon = make([]uint64, o.total)
		un := 0
		for j := 0; j < o.total; j++ {
			switch {
			case j < o.ndef:
				o.canon[j] = o.gid0 + uint64(j) + 1
			case j < o.ndef+o.nh64:
				o.canon[j] = tab.get(hash64Key(r.Hash64(uint32(j - o.ndef))))
			case j < o.ndef+o.nh64+o.nh:
				h := r.Hash(uint32(j - o.ndef - o.nh64))
				o.canon[j] = tab.get(hashKey(h[:]))
			default:
				s := r.Sym(uint32(j))
				if s.ABI() == goobj.SymABIstatic && j < o.ndef+o.nh64+o.nh+o.nnonpkg {
					o.canon[j] = o.gid0 + uint64(j) + 1
				} else {
					o.canon[j] = tab.get(nameKey(s.Name(r), s.ABI()))
					if o.canon[j] == 0 {
						un++
					}
				}
			}
		}
		unresolved.Add(int64(un))
	})
	lap("3. resolve every symbol slot", fmt.Sprintf("%d slots, %d references to symbols no object defines (the linker makes those)", nsyms, unresolved.Load()))

	// ---- 3b. reachability: a frontier-parallel flood over relocation and aux edges from the entry
	// points and every package's init task. NOT the Go linker's dead-code pass: that one keeps a method
	// only if an interface or reflection can call it; here an edge is an edge (weak ones are skipped,
	// method-table edges are counted both ways). It measures what a parallel traversal of this graph costs.
	starts := make([]uint64, len(objs))
	for i, o := range objs {
		starts[i] = o.gid0
	}
	objOf := func(gid uint64) (*object, int) {
		k := sort.Search(len(starts), func(i int) bool { return starts[i] >= gid }) - 1
		return objs[k], int(gid - 1 - objs[k].gid0)
	}
	target := func(o *object, ref goobj.SymRef) uint64 {
		switch ref.PkgIdx {
		case goobj.PkgIdxInvalid, goobj.PkgIdxBuiltin:
			return 0
		case goobj.PkgIdxSelf:
			return o.canon[ref.SymIdx]
		case goobj.PkgIdxHashed64:
			return o.canon[o.ndef+int(ref.SymIdx)]
		case goobj.PkgIdxHashed:
			return o.canon[o.ndef+o.nh64+int(ref.SymIdx)]
		case goobj.PkgIdxNone:
			return o.canon[o.ndef+o.nh64+o.nh+int(ref.SymIdx)]
		}
		if p := primary[o.r.Pkg(int(ref.PkgIdx))]; p != nil && int(ref.SymIdx) < p.ndef {
			return p.gid0 + uint64(ref.SymIdx) + 1
		}
		return 0
	}
	for _, methods := range []bool{true, false} {
		live := make([]atomic.Uint32, nsyms/32+1)
		mark := func(gid uint64) bool {
			w, b := &live[gid>>5], uint32(1)<<(gid&31)
			for {
				old := w.Load()
				if old&b != 0 {
					return false
				}
				if w.CompareAndSwap(old, old|b) {
					return true
				}
			}
		}
		var frontier []uint64
		for _, root := range []struct {
			name string
			abi  uint16
		}{{"main.main", 1}, {"_rt0_amd64_linux", 0}, {"runtime.rt0_go", 0}, {"runtime.main", 1}} {
			if g := tab.get(nameKey(root.name, root.abi)); g != 0 && mark(g) {
				frontier = append(frontier, g)
			}
		}
		for _, o := range objs {
			for j := 0; j < o.ndef; j++ {
				if s := o.r.Sym(uint32(j)); (s.IsPkgInit() || strings.HasSuffix(s.Name(o.r), "..inittask")) && mark(o.gid0+uint64(j)+1) {
					frontier = append(frontier, o.gid0+uint64(j)+1)
				}
			}
		}
		nroots, levels := len(frontier), 0
		var nlive, liveBytes, nedges atomic.Int64
		for len(frontier) > 0 {
			levels++
			nw := runtime.GOMAXPROCS(0)
			next := make([][]uint64, nw*8)
			cur := frontier
			par(len(next), func(c int) {
				var out []uint64
				lb, ne := 0, 0
				for _, gid := range cur[c*len(cur)/len(next) : (c+1)*len(cur)/len(next)] {
					o, j := objOf(gid)
					if j >= o.ndef+o.nh64+o.nh+o.nnonpkg {
						continue // a reference nothing defines
					}
					lb += int(o.r.Sym(uint32(j)).Siz())
					for _, rl := range o.r.Relocs(uint32(j)) {
						ne++
						t := objabi.RelocType(rl.Type())
						if t&objabi.R_WEAK != 0 || (!methods && t&^objabi.R_WEAK == objabi.R_METHODOFF) {
							continue
						}
						if g := target(o, rl.Sym()); g != 0 && mark(g) {
							out = append(out, g)
						}
					}
					for _, a := range o.r.Auxs(uint32(j)) {
						if g := target(o, a.Sym()); g != 0 && mark(g) {
							out = append(out, g)
						}
					}
				}
				next[c] = out
				nlive.Add(int64((c+1)*len(cur)/len(next) - c*len(cur)/len(next)))
				liveBytes.Add(int64(lb))
				nedges.Add(int64(ne))
			})
			frontier = frontier[:0]
			for _, l := range next {
				frontier = append(frontier, l...)
			}
		}
		what := "3b. reachability, method tables as edges"
		if !methods {
			what = "3c. reachability, method tables skipped"
		}
		if dir := os.Getenv("F4LINK_DUMPLIVE"); dir != "" {
			// names of the live functions, to compare with what the stock linker keeps (go tool nm)
			var names []string
			for _, o := range objs {
				for j := 0; j < o.ndef+o.nh64+o.nh+o.nnonpkg; j++ {
					gid := o.gid0 + uint64(j) + 1
					if o.canon[j] == gid && live[gid>>5].Load()&(1<<(gid&31)) != 0 {
						if sy := o.r.Sym(uint32(j)); objabi.SymKind(sy.Type()) == objabi.STEXT {
							names = append(names, sy.Name(o.r))
						}
					}
				}
			}
			sort.Strings(names)
			f := dir + "/live-methods.txt"
			if !methods {
				f = dir + "/live-nomethods.txt"
			}
			os.WriteFile(f, []byte(strings.Join(names, "\n")+"\n"), 0o644)
		}
		lap(what, fmt.Sprintf("%d roots, %d levels, %d symbols live, %d MB, %d edges followed", nroots, levels, nlive.Load(), liveBytes.Load()>>20, nedges.Load()))
	}

	// ---- 4. layout: prefix sum of the sizes of the definitions each object owns, then of the objects
	par(len(objs), func(i int) {
		o := objs[i]
		r := o.r
		ndefs := o.ndef + o.nh64 + o.nh + o.nnonpkg
		o.addr = make([]int64, ndefs)
		var off int64
		for j := 0; j < ndefs; j++ {
			if o.canon[j] != o.gid0+uint64(j)+1 {
				o.addr[j] = -1 // a duplicate: another object owns it
				continue
			}
			s := r.Sym(uint32(j))
			a := int64(s.Align())
			if a == 0 {
				a = 1
			}
			off = (off + a - 1) &^ (a - 1)
			o.addr[j] = off
			off += int64(s.Siz())
		}
		o.size = (off + 63) &^ 63
	})
	var total int64
	for _, o := range objs {
		o.base = total
		total += o.size
	}
	addrOf := func(gid uint64) int64 {
		if gid == 0 {
			return 0
		}
		k := sort.Search(len(starts), func(i int) bool { return starts[i] >= gid }) - 1
		o := objs[k]
		j := int(gid - 1 - o.gid0)
		if j >= len(o.addr) || o.addr[j] < 0 {
			return 0
		}
		return o.base + o.addr[j]
	}
	lap("4. layout by prefix sums", fmt.Sprintf("image %d MB (everything: no dead-code elimination)", total>>20))

	// ---- 5. copy the data and apply every relocation
	image, err := syscall.Mmap(-1, 0, int(total), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		panic(err)
	}
	var nrel, nbytes atomic.Int64
	par(len(objs), func(i int) {
		o := objs[i]
		r := o.r
		rel, nb := 0, 0
		for j := range o.addr {
			if o.addr[j] < 0 {
				continue
			}
			at := o.base + o.addr[j]
			d := r.Data(uint32(j))
			copy(image[at:], d)
			nb += len(d)
			size := int64(r.Sym(uint32(j)).Siz())
			for _, rl := range r.Relocs(uint32(j)) {
				ref := rl.Sym()
				var gid uint64
				switch ref.PkgIdx {
				case goobj.PkgIdxInvalid, goobj.PkgIdxBuiltin:
					// (builtins are runtime functions by number: found by name in a real linker)
				case goobj.PkgIdxSelf:
					gid = o.canon[ref.SymIdx]
				case goobj.PkgIdxHashed64:
					gid = o.canon[o.ndef+int(ref.SymIdx)]
				case goobj.PkgIdxHashed:
					gid = o.canon[o.ndef+o.nh64+int(ref.SymIdx)]
				case goobj.PkgIdxNone:
					gid = o.canon[o.ndef+o.nh64+o.nh+int(ref.SymIdx)]
				default:
					if p := primary[r.Pkg(int(ref.PkgIdx))]; p != nil && int(ref.SymIdx) < p.ndef {
						gid = p.gid0 + uint64(ref.SymIdx) + 1
					}
				}
				v := addrOf(gid) + rl.Add()
				off := int64(rl.Off())
				switch {
				case rl.Siz() == 8 && off+8 <= size:
					binary.LittleEndian.PutUint64(image[at+off:], uint64(v))
				case rl.Siz() == 4 && off+4 <= size:
					binary.LittleEndian.PutUint32(image[at+off:], uint32(v-(at+off+4)))
				}
				rel++
			}
		}
		nrel.Add(int64(rel))
		nbytes.Add(int64(nb))
	})
	lap("5. copy data, apply relocations", fmt.Sprintf("%d MB of data, %d relocations", nbytes.Load()>>20, nrel.Load()))
}
