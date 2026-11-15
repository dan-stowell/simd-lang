#!/usr/bin/env python3
"""tools/fc/freecheck.py <GOROOT/src/runtime> <out dir>: a copy of the runtime whose collector reports the first
pointer it is shown that points to a slot nothing is allocated in (past the last object of a span, or a free slot),
naming where it found it: the function, pc and frame offset of a precisely scanned stack frame; a global; a heap
object; the write-barrier buffer. The stock runtime only notices such a pointer at the next sweep, if the slot is
still free then ("found pointer to free object"), and cannot say who held it.

The stock compiler never shows the collector such a pointer: a pointer one past the end of an object, or into a
span's own bitmaps, is dead at every safepoint. fc's stack maps are coarser (a slot lives to the end of its block,
arguments to the end of the function), so a safepoint the stock compiler does not have (a call it inlines) can
expose one: runtime.reflectcallmove's dst was the first found this way (JOURNAL 2026-11-15).

Used by tools/fc/wptest.sh when WPTEST_FREECHECK=1: the test binary's plan is pointed at the copy."""
import os, re, shutil, sys

src, out = sys.argv[1], sys.argv[2]
os.makedirs(os.path.join(out, "cgo"), exist_ok=True)
for f in os.listdir(src):
    if f.endswith((".go", ".s", ".h")):   # (the _test.go files too: the runtime's own tests are in its package)
        shutil.copy(os.path.join(src, f), out)
for f in os.listdir(os.path.join(src, "cgo")):
    if f.endswith(".h"):
        shutil.copy(os.path.join(src, "cgo", f), os.path.join(out, "cgo"))

def edit(name, fn):
    p = os.path.join(out, name)
    s = open(p).read()
    open(p, "w").write(fn(s))

def mgcmark(s):
    old = "\t\t\t\t\t} else {\n\t\t\t\t\t\tif !tryDeferToSpanScan(p, gcw) {\n\t\t\t\t\t\t\tif obj, span, objIndex := findObject(p, b, i); obj != 0 {"
    assert s.count(old) == 1, "scanblock changed"
    s = s.replace(old, "\t\t\t\t\t} else {\n\t\t\t\t\t\tsbCheckFree(p, b, i, stk, \"scanblock\")\n" + old[len("\t\t\t\t\t} else {\n"):])
    old = "func scanframeworker(frame *stkframe, state *stackScanState, gcw *gcWork) {\n"
    assert s.count(old) == 1
    s = s.replace(old, old + "\tstate.sbFrame = frame\n")
    s = heap(s, "mgcmark.go", skip=("p, gcw", "b, gcw", "c.tiny, gcw"))
    return s + r'''
// sbCheckFree (tools/fc/freecheck.py): p, found in the word at b+i, points to a slot nothing is allocated in.
func sbCheckFree(p, b, i uintptr, stk *stackScanState, where string) {
	s := spanOfHeap(p)
	if s == nil || s.spanclass.sizeclass() == 0 {
		return
	}
	idx := s.objIndex(p)
	if idx < uintptr(s.nelems) && !s.isFree(idx) {
		return
	}
	printlock()
	print("freecheck: pointer to a free slot: p=", hex(p), " span base=", hex(s.base()), " elemsize=", s.elemsize, " index=", idx, " freeindex=", s.freeindex, " nelems=", s.nelems, " found by ", where, " in the word at ", hex(b+i), "\n")
	if stk != nil && stk.sbFrame != nil {
		f := stk.sbFrame
		print("freecheck: in the frame of ", funcname(f.fn), " pc=", hex(f.pc), " (+", hex(f.pc-f.fn.entry()), ") varp-", hex(f.varp-(b+i)), " argp+", hex(b+i-f.argp), "\n")
	} else if b != 0 {
		print("freecheck: not in a stack frame; data ", hex(firstmoduledata.data), "-", hex(firstmoduledata.edata), " bss ", hex(firstmoduledata.bss), "-", hex(firstmoduledata.ebss), "\n")
		hexdumpWords(b+i-64, 128, nil)
	}
	printunlock()
	throw("freecheck: the collector was shown a pointer to a free slot")
}
'''

def heap(s, name, skip=()):
    # every other place a pointer is handed to the marker: a heap object's word, the write-barrier buffer
    def sub(m):
        if m.group(3) in skip:
            return m.group(0)
        arg = m.group(3).split(",")[0]
        return '%ssbCheckFree(%s, 0, 0, nil, "%s")\n%s' % (m.group(1), arg, name, m.group(0))
    return re.sub(r"(?m)^(\t+)(if !?tryDeferToSpanScan\(([^)]*)\))", sub, s)

edit("mgcmark.go", mgcmark)
edit("mgcmark_greenteagc.go", lambda s: heap(s, "mgcmark_greenteagc.go"))
edit("mwbbuf.go", lambda s: heap(s, "mwbbuf.go"))
def mgcstack(s):
    old = "type stackScanState struct {\n"
    assert s.count(old) == 1
    return s.replace(old, old + "\tsbFrame *stkframe // (freecheck) the frame being scanned\n")
edit("mgcstack.go", mgcstack)
