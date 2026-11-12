#!/usr/bin/env python3
"""Patch cmd/compile so that, when SB_EARLY_EXPORT is set and -linkobj is given, the export data (-o)
is written as soon as the front end has finished (after inlining decisions and escape analysis, which
the export data records) and before any function is compiled. The compiler then prints
"sb:export-ready" on stdout. Packages that import this one need nothing else from it, so they can start
while this package's back end is still running.     early_export.py <dir of cmd/compile/internal/gc> <dir of cmd/internal/obj>

Second change, in the object writer. Symbol indices are assigned after the back end, so early export data
has none, and importers refer to this package's symbols by name. Content-addressed symbols (interface
tables above all) are deduplicated by a hash that covers how each relocation target is addressed: by
(package, index) in the defining package, by name in such an importer. Two spellings of one table then
both survive the link, and a type assertion comparing table addresses fails. With SB_EARLY_EXPORT set,
the hash addresses every target by name, so all packages agree."""
import sys, os
d = sys.argv[1]
objd = sys.argv[2]
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
# Third change: functions another compiler provides. With SB_DROP_BODIES set, a function that has a
# definition in the -symabis file (the same mechanism that says "this one is written in assembly") loses
# its Go body right after type checking: the stock compiler then treats it as implemented elsewhere, with
# the ABI the symabis line gives, and compiles nothing for it. sbc emits it into another object of the
# same package archive.
anchor2 = '\tnoder.LoadPackage(flag.Args())\n'
assert s.count(anchor2) == 1
s = s.replace(anchor2, anchor2 + '''
\tif os.Getenv("SB_DROP_BODIES") != "" {
\t\tfor _, fn := range typecheck.Target.Funcs {
\t\t\tif len(fn.Body) != 0 && fn.Nname != nil && !ir.IsBlank(fn.Nname) && symABIs.HasDef(fn.Sym()) {
\t\t\t\tfn.Body = nil
\t\t\t\tvar params []*ir.Name
\t\t\t\tfor _, n := range fn.Dcl {
\t\t\t\t\tif n.Class == ir.PPARAM || n.Class == ir.PPARAMOUT {
\t\t\t\t\t\tparams = append(params, n)
\t\t\t\t\t}
\t\t\t\t}
\t\t\t\tfn.Dcl = params
\t\t\t}
\t\t}
\t}
''')
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

p = os.path.join(objd, 'objfile.go'); s = open(p).read()
old = '''		case goobj.PkgIdxSelf:
			io.WriteString(h, w.pkgpath)
			binary.LittleEndian.PutUint32(tmp[:4], uint32(rs.SymIdx))
			h.Write(tmp[:4])
		default:
			io.WriteString(h, rs.Pkg)
			binary.LittleEndian.PutUint32(tmp[:4], uint32(rs.SymIdx))
			h.Write(tmp[:4])
'''
assert s.count(old) == 1
new = '''		case goobj.PkgIdxSelf:
			if sbHashByName {
				h.Write([]byte{2})
				io.WriteString(h, rs.Name)
				break
			}
			io.WriteString(h, w.pkgpath)
			binary.LittleEndian.PutUint32(tmp[:4], uint32(rs.SymIdx))
			h.Write(tmp[:4])
		default:
			if sbHashByName {
				h.Write([]byte{2})
				io.WriteString(h, rs.Name)
				break
			}
			io.WriteString(h, rs.Pkg)
			binary.LittleEndian.PutUint32(tmp[:4], uint32(rs.SymIdx))
			h.Write(tmp[:4])
'''
s = s.replace(old, new)
s += '''
// sbHashByName (sb): hash relocation targets of content-addressed symbols by name, never by index,
// so that packages that import by name and packages that import by index compute the same hash.
var sbHashByName = os.Getenv("SB_EARLY_EXPORT") != ""
'''
open(p, 'w').write(s)
