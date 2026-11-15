// sbbump.go (added to package runtime in fc's GOROOT by driver/rtbump/patch.py): allocation by bumping a
// pointer. Nothing is ever freed and the collector never runs.
//
// Why: fc builds a program in one process with the collector off; every object lives until the process
// exits. The allocator's own work was then a sixth of the build's CPU (size classes, span and bitmap
// bookkeeping, type headers: 2.1 of 13.6 core-s on MinIO), all of it for a collector that never comes.
// Here an allocation is an add and a compare on the P's chunk; chunks are 4 MB of address space mapped
// from the kernel (from the hugetlb pool when SB_HUGE is set, as the heap is), zero as they arrive.
//
// The program turns it on (sbBumpEnable, linknamed) after it has turned the collector off. What the Go
// runtime special-cases for its own never-free allocator (GODEBUG=sbrk=1) is special-cased here the same
// way: no finalizers, no cleanups, immortal weak handles. gcStart returns at once: memory allocated
// before the switch may be referenced only from memory the collector knows nothing about.
package runtime

import (
	"internal/runtime/atomic"
	"unsafe"
)

var (
	sbBump      bool
	sbBumpHuge  bool
	sbBumpLimit uintptr
	sbBumpBytes atomic.Uintptr
)

type sbBumpState struct {
	cur, end uintptr
}

var sbBumpGlobal struct {
	lock mutex
	st   sbBumpState
}

const (
	sbBumpChunk = 4 << 20
	sbHugePage  = 2 << 20
)

// sbBumpEnable turns bump allocation on. limit is the most address space it may map, in bytes: past it the
// program stops with a message (the machine has no swap: better than taking the whole session down).
//
//go:linkname sbBumpEnable
func sbBumpEnable(limit uintptr) {
	sbBumpHuge = gogetenv("SB_HUGE") != ""
	sbBumpLimit = limit
	sbBump = true
}

// sbBumpMapped is how much has been mapped so far.
//
//go:linkname sbBumpMapped
func sbBumpMapped() uintptr { return sbBumpBytes.Load() }

func sbBumpMap(n uintptr) uintptr {
	n = alignUp(n, sbHugePage)
	if sbBumpBytes.Add(n) > sbBumpLimit {
		print("runtime: bump allocator: more than ", sbBumpLimit>>20, " MB\n")
		throw("out of memory budget")
	}
	if sbBumpHuge {
		// (if the pool is empty the call fails and ordinary pages are used)
		if p, err := mmap(nil, n, _PROT_READ|_PROT_WRITE, _MAP_ANON|_MAP_PRIVATE|0x40000, -1, 0); err == 0 {
			return uintptr(p)
		}
	}
	p, err := mmap(nil, n, _PROT_READ|_PROT_WRITE, _MAP_ANON|_MAP_PRIVATE, -1, 0)
	if err != 0 {
		throw("runtime: bump allocator: cannot map memory")
	}
	return uintptr(p)
}

// sbBumpAlloc returns size bytes of zeroed memory, aligned to 8 bytes (16 for 16 bytes and more, as the
// size classes give).
func sbBumpAlloc(size uintptr) unsafe.Pointer {
	if size >= sbBumpChunk/2 {
		return unsafe.Pointer(sbBumpMap(size))
	}
	align := uintptr(8)
	if size >= 16 {
		align = 16
	}
	// An M's address is packed into mutex words (lock_spinbit.go): the low ten bits must be those of the
	// heap's size class with a malloc header (mutexMOffset). Any object of an M's size gets that address.
	isM := size == unsafe.Sizeof(mPadded{})
	if isM {
		align = mutexMMask + 1
		size += mutexMOffset
	}
	size = (size + 7) &^ 7
	mp := acquirem()
	var st *sbBumpState
	global := mp.p == 0
	if global {
		lock(&sbBumpGlobal.lock)
		st = &sbBumpGlobal.st
	} else {
		st = &mp.p.ptr().sbb
	}
	x := (st.cur + align - 1) &^ (align - 1)
	if x+size > st.end || st.cur == 0 {
		x = sbBumpMap(sbBumpChunk)
		st.end = x + sbBumpChunk
	}
	st.cur = x + size
	if global {
		unlock(&sbBumpGlobal.lock)
	}
	releasem(mp)
	if isM {
		x += mutexMOffset
	}
	return unsafe.Pointer(x)
}
