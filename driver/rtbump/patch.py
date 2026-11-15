#!/usr/bin/env python3
"""Patch package runtime in fc's GOROOT for the bump allocator (sbbump.go).   patch.py <runtime dir>
Idempotent: a tree that already has the patch is left alone."""
import re, shutil, sys
d = sys.argv[1]
here = __file__.rsplit("/", 1)[0]
shutil.copy(here + "/sbbump.go", d + "/sbbump.go")
def patch(name, edit):
    s = open(f"{d}/{name}").read()
    if "sbBump" in s or "sbb " in s:
        return
    t = edit(s)  # (before the file is opened for writing: a failed edit must not truncate it)
    open(f"{d}/{name}", "w").write(t)
def one(old, new):
    def f(s):
        assert s.count(old) == 1, (old, s.count(old))
        return s.replace(old, new)
    return f
hook = "\tif sbBump {\n\t\tif size == 0 {\n\t\t\treturn unsafe.Pointer(&zerobase)\n\t\t}\n\t\treturn sbBumpAlloc(size)\n\t}\n"
patch("malloc.go", one("func mallocgc(size uintptr, typ *_type, needzero bool) unsafe.Pointer {\n",
                       "func mallocgc(size uintptr, typ *_type, needzero bool) unsafe.Pointer {\n" + hook))
def gen(s):
    # every size-specialized entry point (the compiler calls them directly)
    s, n = re.subn(r"(\nfunc malloc\w+\(size uintptr, typ \*_type, needzero bool\) unsafe\.Pointer \{\n)", lambda m: m.group(1) + hook, s)
    assert n >= 14, n
    return s
patch("malloc_generated.go", gen)
for name in ["mcleanup.go", "mfinal.go", "mheap.go"]:
    patch(name, one("\tif debug.sbrk != 0 {\n", "\tif debug.sbrk != 0 || sbBump {\n"))
patch("mgc.go", one("func gcStart(trigger gcTrigger) {\n", "func gcStart(trigger gcTrigger) {\n\tif sbBump {\n\t\treturn // (sbbump.go: nothing is ever freed)\n\t}\n"))
patch("runtime2.go", one("type p struct {\n\tid          int32\n", "type p struct {\n\tsbb sbBumpState // sbbump.go: the chunk this P allocates from\n\n\tid          int32\n"))
