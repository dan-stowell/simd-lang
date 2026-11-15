package main

// Module sources read from the module cache's downloaded zips (GOMODCACHE/cache/download/<module>/@v/<version>.zip)
// instead of from the extracted trees beside them.
//
// Why: on a cold page cache the disk answers a fixed number of requests a second whatever their size
// (20-50 thousand here; sequential bytes come at 3 GB/s). A program's sources as extracted files are one
// request per file and per directory (MinIO: 7,766 files in 1,323 directories, about 10,000 requests,
// 0.3-0.5 s before the program's graph is even known). A module's zip is one file: its directory of
// contents is one read, and the files of one package lie next to each other in it, so a package is one
// read of the compressed bytes of its files (a quarter of the bytes, too). The price is inflating them.
//
// The zip is what `go mod download` verified against go.sum; the extracted tree was made from it. FC_ZIPCHECK=1
// compares every file taken from a zip with the extracted file; FC_NOZIP=1 reads the extracted trees only.
// Anything unexpected in a zip (zip64, an unknown method, a bad checksum) falls back to the extracted file.

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	noZip    = os.Getenv("FC_NOZIP") == "1"
	zipCheck = os.Getenv("FC_ZIPCHECK") == "1"
)

type zipEnt struct {
	name         string // base name
	hdr          int64  // offset of the local header
	csize, usize uint32
	crc          uint32
	method       uint16
	nameLen      int // of the full name, as the local header repeats it
}

type zipMod struct {
	f     *os.File
	cdOff int64               // where the central directory starts: no file's bytes go past it
	dirs  map[string][]zipEnt // directory, relative to the module's root with slashes ("" = the root) -> its files, by offset
	once  sync.Once
	sub   map[string]bool // every directory, the ones without files of their own included
}

func (z *zipMod) subOnce() *sync.Once { return &z.once }

// zipSrc: one file of a package directory, its compressed bytes in memory (buf is a read of the zip that
// starts at file offset base).
type zipSrc struct {
	e    *zipEnt
	buf  []byte
	base int64
}

var zipStat struct {
	mods, reads, bytes, files, inflated atomic.Int64
	fallbacks                           atomic.Int64
}

func (mi *modInfo) zip() *zipMod {
	if mi == nil || mi.zipPath == "" || noZip {
		return nil
	}
	mi.zipOnce.Do(func() {
		z, err := openZipMod(mi.zipPath, mi.zipPrefix)
		if err != nil {
			zipStat.fallbacks.Add(1)
			return
		}
		mi.z = z
		zipStat.mods.Add(1)
	})
	return mi.z
}

func openZipMod(path, prefix string) (*zipMod, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*zipMod, error) { f.Close(); return nil, err }
	st, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	size := st.Size()
	// the end record is the last 22 bytes when the zip has no comment (the go command writes none); the
	// directory lies just before it, so one read of the tail usually has both
	tailLen := int64(256 << 10)
	if tailLen > size {
		tailLen = size
	}
	tail := make([]byte, tailLen)
	if _, err := f.ReadAt(tail, size-tailLen); err != nil {
		return fail(err)
	}
	zipStat.reads.Add(1)
	zipStat.bytes.Add(tailLen)
	eo := -1
	for i := len(tail) - 22; i >= 0 && i >= len(tail)-22-65535; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) == 0x06054b50 {
			eo = i
			break
		}
	}
	if eo < 0 {
		return fail(fmt.Errorf("%s: no end record", path))
	}
	n := int(binary.LittleEndian.Uint16(tail[eo+10:]))
	cdSize := int64(binary.LittleEndian.Uint32(tail[eo+12:]))
	cdOff := int64(binary.LittleEndian.Uint32(tail[eo+16:]))
	if n == 0xffff || cdSize == 0xffffffff || cdOff == 0xffffffff || cdOff+cdSize > size {
		return fail(fmt.Errorf("%s: zip64 or a bad directory", path))
	}
	var cd []byte
	if tailStart := size - tailLen; cdOff >= tailStart {
		cd = tail[cdOff-tailStart : cdOff-tailStart+cdSize]
	} else {
		cd = make([]byte, cdSize)
		if _, err := f.ReadAt(cd, cdOff); err != nil {
			return fail(err)
		}
		zipStat.reads.Add(1)
		zipStat.bytes.Add(cdSize)
	}
	z := &zipMod{f: f, cdOff: cdOff, dirs: map[string][]zipEnt{}}
	for i := 0; i < n; i++ {
		if len(cd) < 46 || binary.LittleEndian.Uint32(cd) != 0x02014b50 {
			return fail(fmt.Errorf("%s: bad directory entry %d", path, i))
		}
		nl, xl, cl := int(binary.LittleEndian.Uint16(cd[28:])), int(binary.LittleEndian.Uint16(cd[30:])), int(binary.LittleEndian.Uint16(cd[32:]))
		if len(cd) < 46+nl+xl+cl {
			return fail(fmt.Errorf("%s: short directory", path))
		}
		e := zipEnt{method: binary.LittleEndian.Uint16(cd[10:]), crc: binary.LittleEndian.Uint32(cd[16:]),
			csize: binary.LittleEndian.Uint32(cd[20:]), usize: binary.LittleEndian.Uint32(cd[24:]),
			hdr: int64(binary.LittleEndian.Uint32(cd[42:])), nameLen: nl}
		flags := binary.LittleEndian.Uint16(cd[8:])
		full := cd[46 : 46+nl]
		cd = cd[46+nl+xl+cl:]
		if e.csize == 0xffffffff || e.usize == 0xffffffff || e.hdr == 0xffffffff || flags&1 != 0 {
			e.method = 0xffff // (zip64 or encrypted: this file comes from the extracted tree)
		}
		if len(full) <= len(prefix) || string(full[:len(prefix)]) != prefix || full[len(full)-1] == '/' {
			continue
		}
		rel := string(full[len(prefix):])
		dir := ""
		if j := strings.LastIndexByte(rel, '/'); j >= 0 {
			dir, rel = rel[:j], rel[j+1:]
		}
		e.name = rel
		z.dirs[dir] = append(z.dirs[dir], e)
	}
	return z, nil
}

// hasDir: is rel a directory of the module (one with files in it or below it)?
func (z *zipMod) hasDir(rel string) bool {
	if _, ok := z.dirs[rel]; ok || rel == "" {
		return true
	}
	z.subOnce().Do(func() {
		z.sub = map[string]bool{}
		for d := range z.dirs {
			for d != "" {
				if z.sub[d] {
					break
				}
				z.sub[d] = true
				if j := strings.LastIndexByte(d, '/'); j >= 0 {
					d = d[:j]
				} else {
					d = ""
				}
			}
		}
	})
	return z.sub[rel]
}

// read: the compressed bytes of ents (files of one directory), as few reads as their places in the zip
// allow: neighbours closer than zipGap are one read.
const zipGap = 128 << 10

func (z *zipMod) read(ents []*zipEnt) (map[*zipEnt]zipSrc, error) {
	sort.Slice(ents, func(i, j int) bool { return ents[i].hdr < ents[j].hdr })
	end := func(e *zipEnt) int64 {
		// local header (30 bytes), the name, an extra field (the go command writes none; 64 bytes of room), the bytes
		x := e.hdr + 30 + int64(e.nameLen) + 64 + int64(e.csize)
		if x > z.cdOff {
			x = z.cdOff
		}
		return x
	}
	out := map[*zipEnt]zipSrc{}
	for i := 0; i < len(ents); {
		j, hi := i+1, end(ents[i])
		for j < len(ents) && ents[j].hdr-hi <= zipGap {
			if e := end(ents[j]); e > hi {
				hi = e
			}
			j++
		}
		base := ents[i].hdr
		buf := make([]byte, hi-base)
		if _, err := z.f.ReadAt(buf, base); err != nil {
			return nil, err
		}
		zipStat.reads.Add(1)
		zipStat.bytes.Add(int64(len(buf)))
		for ; i < j; i++ {
			out[ents[i]] = zipSrc{ents[i], buf, base}
		}
	}
	return out, nil
}

var inflaters = sync.Pool{New: func() any { return flate.NewReader(bytes.NewReader(nil)) }}

// bytes: the file, inflated and checked against the directory's checksum.
func (s zipSrc) bytes() ([]byte, error) {
	e := s.e
	b := s.buf[e.hdr-s.base:]
	if len(b) < 30 || binary.LittleEndian.Uint32(b) != 0x04034b50 {
		return nil, fmt.Errorf("zip: bad local header")
	}
	off := 30 + int(binary.LittleEndian.Uint16(b[26:])) + int(binary.LittleEndian.Uint16(b[28:]))
	if off+int(e.csize) > len(b) {
		return nil, fmt.Errorf("zip: file past the bytes read")
	}
	comp := b[off : off+int(e.csize)]
	var out []byte
	switch e.method {
	case 0:
		if e.csize != e.usize {
			return nil, fmt.Errorf("zip: stored file with two sizes")
		}
		out = comp
	case 8:
		out = make([]byte, e.usize)
		fr := inflaters.Get().(io.ReadCloser)
		fr.(flate.Resetter).Reset(bytes.NewReader(comp), nil)
		_, err := io.ReadFull(fr, out)
		if err == nil {
			var one [1]byte
			if n, _ := fr.Read(one[:]); n != 0 {
				err = fmt.Errorf("zip: file longer than the directory says")
			}
		}
		inflaters.Put(fr)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("zip: method %d", e.method)
	}
	if crc32.ChecksumIEEE(out) != e.crc {
		return nil, fmt.Errorf("zip: checksum")
	}
	zipStat.files.Add(1)
	zipStat.inflated.Add(int64(len(out)))
	return out, nil
}
