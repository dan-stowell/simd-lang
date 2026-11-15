// bump.go: the runtime's bump allocator (driver/rtbump/sbbump.go, patched into the runtime of fc's GOROOT).
package main

import _ "unsafe" // for go:linkname

// sbBumpEnable turns bump allocation on for the rest of the process: nothing is freed, the collector never
// runs, and past limit bytes the process stops. FC_NOBUMP=1 keeps the Go allocator (so does -memprofile:
// the profiler lives in it).
//
//go:linkname sbBumpEnable runtime.sbBumpEnable
func sbBumpEnable(limit uintptr)

// sbBumpMapped is the address space the bump allocator has mapped, in bytes.
//
//go:linkname sbBumpMapped runtime.sbBumpMapped
func sbBumpMapped() uintptr
