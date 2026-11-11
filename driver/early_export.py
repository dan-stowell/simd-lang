#!/usr/bin/env python3
"""Patch cmd/compile so that, when SB_EARLY_EXPORT is set and -linkobj is given, the export data (-o)
is written as soon as the front end has finished (after inlining decisions and escape analysis, which
the export data records) and before any function is compiled. The compiler then prints
"sb:export-ready" on stdout. Packages that import this one need nothing else from it, so they can start
while this package's back end is still running.     early_export.py <dir of cmd/compile/internal/gc>"""
import sys, os
d = sys.argv[1]
p = os.path.join(d, 'main.go'); s = open(p).read()
anchor = '\tbase.Timer.Start("be", "compilefuncs")\n'
assert s.count(anchor) == 1
s = s.replace(anchor, '''\t// sb: pipelining. Everything the export data records is decided by now.
\tif base.Flag.LinkObj != "" && os.Getenv("SB_EARLY_EXPORT") != "" {
\t\tbase.Timer.Start("fe", "earlyexport")
\t\tdumpobj1(base.Flag.LowerO+".tmp", modeCompilerObj)
\t\tif err := os.Rename(base.Flag.LowerO+".tmp", base.Flag.LowerO); err != nil {
\t\t\tbase.Fatalf("%v", err)
\t\t}
\t\tearlyExport = true
\t\tos.Stdout.WriteString("sb:export-ready\\n")
\t}
''' + anchor)
open(p, 'w').write(s)
p = os.path.join(d, 'obj.go'); s = open(p).read()
old = '''	dumpobj1(base.Flag.LowerO, modeCompilerObj)
	dumpobj1(base.Flag.LinkObj, modeLinkerObj)
'''
assert s.count(old) == 1
s = s.replace(old, '''	if !earlyExport {
		dumpobj1(base.Flag.LowerO, modeCompilerObj)
	}
	dumpobj1(base.Flag.LinkObj, modeLinkerObj)
''')
s = s.replace('func dumpobj() {', '// earlyExport: the compiler object was already written, before the back end ran (sb).\nvar earlyExport bool\n\nfunc dumpobj() {')
open(p, 'w').write(s)
