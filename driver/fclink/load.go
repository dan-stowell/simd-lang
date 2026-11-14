// load.go: the inputs. Every file of the importcfg is mapped, the Go objects in it are found, and every
// symbol slot of every object gets a dense global id.
package fclinklib

import (
	"bytes"
	"cmd/internal/goobj"
	"cmd/internal/objabi"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// An object is one Go object file: a whole bare object, or one member of an archive (the compiler's
// _go_.o, or one assembly file's object). Its symbol slots are, in order: definitions referenced by
// package index (ndef), content-addressed definitions with a 64-bit hash (nh64) and with a longer hash
// (nh), definitions referenced by name (nnonpkg), and references by name to symbols defined elsewhere
// (nref). Slot j has the global id base+j.
//
// An object is read through one of two sources (source.go has the accessors every phase uses): r, a Go
// object file; or m, one of fc's packages handed over in memory (unit.go), whose slots are in the same
// order and use the same kinds of reference.
type object struct {
	idx                           int
	pkg, file                     string
	r                             *goobj.Reader
	m                             *Unit
	base                          uint32
	ndef, nh64, nh, nnonpkg, nref uint32
	nalldef, total                uint32
	pkgBase                       []uint32 // per package index of this object: base of that package's primary object
	fromAsm                       bool
	nfunc                         uint32 // live functions
	cuOff                         uint32 // index of this object's first file in cutab
}

const badBase = ^uint32(0)

var (
	mapMu     sync.Mutex
	inputMaps [][]byte
)

// A unit is a run of slots of one object, the grain of every parallel pass.
type unit struct {
	o      *object
	lo, hi uint32 // slots [lo,hi) of o

	// layout: bytes this unit's live symbols need per (bucket, alignment class), then where they go
	csize    [nBuckets][nClasses]uint32
	cbase    [nBuckets][nClasses]uint32
	nfunc    [2]uint32 // live functions (ordinary, FIPS), and the index of the first of each
	funcBase [2]uint32
	ftabSize uint32 // bytes of _func structs with their pcdata and funcdata offsets
	ftabBase uint32
	nameSize uint32 // bytes of function names
	nameBase uint32
	pcSize   uint32 // bytes of pc-value tables this unit owns
	pcBase   uint32
	inlSize  uint32 // bytes of inline trees
	inlBase  uint32

	typelinks []uint32   // live types that go in the sorted part of the type section
	unres     []unresRef // references by name that no object defines
}

type unresRef struct {
	gid  uint32
	name string
	abi  uint16
}

const unitSlots = 8192

var (
	objs    []*object
	units   []*unit
	nslots  uint32 // ids 1..nslots are object slots; ids above are symbols the linker makes
	objPage []uint32
	primary = map[string]*object{} // package path -> the object that holds its package-indexed symbols
	strdata = map[string]string{}  // string variables the linker sets: runtime.buildVersion, runtime.modinfo, -X
	mainObj *object
)

const pageShift = 9

// objOf returns the object that holds slot gid and the slot's index in it.
func objOf(gid uint32) (*object, uint32) {
	k := objPage[gid>>pageShift]
	for {
		o := objs[k]
		if gid-o.base < o.total {
			return o, gid - o.base
		}
		k++
	}
}

func mapFile(name string) []byte {
	f, err := os.Open(name)
	if err != nil {
		fatalf("%v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		fatalf("%v", err)
	}
	if st.Size() == 0 {
		return nil
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		fatalf("mmap %s: %v", name, err)
	}
	mapMu.Lock()
	inputMaps = append(inputMaps, b)
	mapMu.Unlock()
	return b
}

// goObject returns the object-file part of a "go object" file image (what follows the text header), or nil.
func goObject(body []byte) []byte {
	if !bytes.HasPrefix(body, []byte("go object ")) {
		return nil
	}
	k := bytes.Index(body, []byte("\n!\n"))
	if k < 0 {
		return nil
	}
	return body[k+3:]
}

func newObject(pkg, file string, body []byte) *object {
	if len(body) >= 4 && string(body[:4]) == "\x7fELF" {
		fatalf("%s: host object in package %s: fclink links Go objects only (CGO_ENABLED=0)", file, pkg)
	}
	b := goObject(body)
	if b == nil {
		return nil
	}
	r := goobj.NewReaderFromBytes(b, true)
	if r == nil {
		fatalf("%s: not a Go object of this toolchain", file)
	}
	if r.Unlinkable() {
		fatalf("%s: unlinkable object (package %s)", file, pkg)
	}
	o := &object{pkg: pkg, file: file, r: r, fromAsm: r.FromAssembly(),
		ndef: uint32(r.NSym()), nh64: uint32(r.NHashed64def()), nh: uint32(r.NHasheddef()),
		nnonpkg: uint32(r.NNonpkgdef()), nref: uint32(r.NNonpkgref())}
	o.nalldef = o.ndef + o.nh64 + o.nh + o.nnonpkg
	o.total = o.nalldef + o.nref
	return o
}

func newMemObject(pkg string, u *Unit) *object {
	o := &object{pkg: pkg, file: "fc:" + pkg, m: u, ndef: u.ndef, nh64: u.nh64, nh: u.nh, nnonpkg: u.nnonpkg, nref: u.nref}
	o.nalldef = o.ndef + o.nh64 + o.nh + o.nnonpkg
	o.total = o.nalldef + o.nref
	return o
}

// A lib is one input: a file (an archive or a bare object), or one of fc's units.
type lib struct {
	pkg, file string
	unit      *Unit
}

// readImportcfg: the inputs an importcfg.link and the main object name.
func readImportcfg(cfgFile, mainFile string) []lib {
	cfg, err := os.ReadFile(cfgFile)
	if err != nil {
		fatalf("%v", err)
	}
	var libs []lib
	mainAbs, _ := filepath.Abs(mainFile)
	haveMain := false
	for n, line := range strings.Split(string(cfg), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		verb, args, _ := strings.Cut(line, " ")
		args = strings.TrimSpace(args)
		switch verb {
		case "packagefile":
			p, f, ok := strings.Cut(args, "=")
			if !ok || p == "" || f == "" {
				fatalf("%s:%d: invalid packagefile", cfgFile, n+1)
			}
			if abs, _ := filepath.Abs(f); abs == mainAbs {
				p, haveMain = "main", true
			}
			libs = append(libs, lib{p, f, nil})
		case "modinfo":
			s, err := strconv.Unquote(args)
			if err != nil {
				fatalf("%s:%d: invalid modinfo: %v", cfgFile, n+1, err)
			}
			strdata["runtime.modinfo"] = s
		case "packageshlib":
			fatalf("%s:%d: shared libraries are not supported", cfgFile, n+1)
		default:
			fatalf("%s:%d: unknown directive %q", cfgFile, n+1, verb)
		}
	}
	if !haveMain {
		libs = append(libs, lib{"main", mainFile, nil})
	}
	return libs
}

func load(libs []lib) {
	perLib := make([][]*object, len(libs))
	par(len(libs), func(_, i int) {
		if u := libs[i].unit; u != nil {
			perLib[i] = []*object{newMemObject(libs[i].pkg, u)}
			return
		}
		b := mapFile(libs[i].file)
		if !bytes.HasPrefix(b, []byte("!<arch>\n")) {
			if o := newObject(libs[i].pkg, libs[i].file, b); o != nil {
				perLib[i] = append(perLib[i], o)
			} else {
				errorf("%s: not a Go object or archive", libs[i].file)
			}
			return
		}
		for off := 8; off+60 <= len(b); {
			name := strings.TrimSpace(string(b[off : off+16]))
			size, err := strconv.Atoi(strings.TrimSpace(string(b[off+48 : off+58])))
			if err != nil || off+60+size > len(b) {
				errorf("%s: malformed archive", libs[i].file)
				return
			}
			body := b[off+60 : off+60+size]
			off += 60 + size + size&1
			name = strings.TrimSuffix(name, "/")
			if name == "__.PKGDEF" {
				continue
			}
			if len(name) < 16 {
				if ext := filepath.Ext(name); ext != ".o" && ext != ".syso" {
					continue
				}
			}
			if o := newObject(libs[i].pkg, libs[i].file, body); o != nil {
				perLib[i] = append(perLib[i], o)
			}
		}
	})
	if nerrs.Load() != 0 {
		return
	}
	next := uint32(1)
	var nsym [5]int
	bareSlots := 0
	for i := range perLib {
		for _, o := range perLib[i] {
			o.idx = len(objs)
			o.base = next
			next += o.total
			objs = append(objs, o)
			if pp := objabi.PathToPrefix(o.pkg); primary[pp] == nil {
				primary[pp] = o // (objects name packages by their escaped paths)
			}
			if o.pkg == "main" && mainObj == nil {
				mainObj = o
			}
			nsym[0] += int(o.ndef)
			nsym[1] += int(o.nh64)
			nsym[2] += int(o.nh)
			nsym[3] += int(o.nnonpkg)
			nsym[4] += int(o.nref)
			if len(perLib[i]) == 1 && !strings.HasSuffix(o.file, "-d") && strings.HasSuffix(o.file, ".o") {
				bareSlots += int(o.total)
			}
			for lo := uint32(0); lo < o.total || lo == 0; lo += unitSlots {
				units = append(units, &unit{o: o, lo: lo, hi: min(lo+unitSlots, o.total)})
			}
		}
	}
	nslots = next - 1
	objPage = make([]uint32, nslots>>pageShift+2)
	k := 0
	for p := range objPage {
		g := uint32(p) << pageShift
		for k+1 < len(objs) && objs[k].base+objs[k].total <= g {
			k++
		}
		objPage[p] = uint32(k)
	}
	par(len(objs), func(_, i int) {
		o := objs[i]
		n := o.npkg()
		o.pkgBase = make([]uint32, n)
		for p := 1; p < n; p++ {
			if q := primary[o.pkgName(p)]; q != nil {
				o.pkgBase[p] = q.base
			} else {
				o.pkgBase[p] = badBase
			}
		}
	})
	lap("load", "%d files, %d objects, %d units; slots: %d by index, %d+%d by content hash, %d defined by name, %d references by name; %d of the slots in bare .o files",
		len(libs), len(objs), len(units), nsym[0], nsym[1], nsym[2], nsym[3], nsym[4], bareSlots)
}

// ref returns the id of the slot a symbol reference in object o names (not yet its canonical definition).
func (o *object) ref(s goobj.SymRef) uint32 {
	switch s.PkgIdx {
	case goobj.PkgIdxInvalid:
		return 0
	case goobj.PkgIdxSelf:
		return o.base + s.SymIdx
	case goobj.PkgIdxHashed64:
		return o.base + o.ndef + s.SymIdx
	case goobj.PkgIdxHashed:
		return o.base + o.ndef + o.nh64 + s.SymIdx
	case goobj.PkgIdxNone:
		return o.base + o.ndef + o.nh64 + o.nh + s.SymIdx
	case goobj.PkgIdxBuiltin:
		return builtins[s.SymIdx]
	}
	b := o.pkgBase[s.PkgIdx]
	if b == badBase {
		errorf("%s: reference to package %s, which is not in the importcfg", o.file, o.pkgName(int(s.PkgIdx)))
		return 0
	}
	return b + s.SymIdx
}
