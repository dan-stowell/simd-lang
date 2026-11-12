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
ssagend = os.path.join(os.path.dirname(d), 'ssagen')
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

# Fourth change, a diagnostic: with SB_OPSTATS=<dir>, every function's lowered tree (what the back end is
# handed after walk) is summarised in <dir>/<pid>.txt: name, node count, back-end nanoseconds, flags, and
# how many nodes of each operation it contains. tools/trace/opstats.py turns that into "how much of the
# back end's work would a code generator supporting this set of operations cover".
p = os.path.join(ssagend, 'pgen.go'); s = open(p).read()
old = '''func Compile(fn *ir.Func, worker int, profile *pgoir.Profile) {
'''
assert s.count(old) == 1
s = s.replace(old, old + '''\tif sbOpStatsDir != "" {
\t\tdefer sbRecord(fn, time.Now())
\t}
\tif sbBaseline && baselineCompile(fn, worker) { // fifth change: driver/baseline/sbbaseline.go
\t\treturn
\t}
''')
s = s.replace('import (\n\t"fmt"', 'import (\n\t"fmt"\n\t"time"', 1)
s += '''
var sbOpStatsDir = os.Getenv("SB_OPSTATS")
var sbOpMu sync.Mutex
var sbOpFile *os.File

func sbRecord(fn *ir.Func, start time.Time) {
	ns := time.Since(start).Nanoseconds()
	ops := map[ir.Op]int{}
	n := 0
	float := false
	ir.VisitList(fn.Body, func(x ir.Node) {
		ops[x.Op()]++
		n++
		if t := x.Type(); t != nil && (t.IsFloat() || t.IsComplex()) {
			float = true
		}
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%s\\t%d\\t%d\\t", fn.LSym.Name, n, ns)
	if fn.HasDefer() {
		b.WriteString("defer,")
	}
	if len(fn.ClosureVars) > 0 {
		b.WriteString("closurevars,")
	}
	if fn.OClosure != nil {
		b.WriteString("isclosure,")
	}
	if float {
		b.WriteString("float,")
	}
	if fn.Dupok() {
		b.WriteString("dupok,")
	}
	b.WriteString("\\t")
	for op, c := range ops {
		fmt.Fprintf(&b, "%v:%d,", op, c)
	}
	b.WriteString("\\n")
	sbOpMu.Lock()
	if sbOpFile == nil {
		sbOpFile, _ = os.OpenFile(fmt.Sprintf("%s/%d.txt", sbOpStatsDir, os.Getpid()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	sbOpFile.WriteString(b.String())
	sbOpMu.Unlock()
}
'''
open(p, 'w').write(s)

# Sixth change: package shards (driver/baseline/sbshard.go has the description and the body-dropping).
noderd = os.path.join(os.path.dirname(d), 'noder')
reflectd = os.path.join(os.path.dirname(d), 'reflectdata')
p = os.path.join(noderd, 'unified.go'); s = open(p).read()
old = '\tpkg, info, otherInfo := checkFiles(m, noders)\n\n\tpw := newPkgWriter(m, pkg, info, otherInfo)\n'
assert s.count(old) == 1
s = s.replace(old, '\tpkg, info, otherInfo := checkFiles(m, noders)\n\tsbShardFiles(noders, info)\n\n\tpw := newPkgWriter(m, pkg, info, otherInfo)\n')
open(p, 'w').write(s)

p = os.path.join(d, 'main.go'); s = open(p).read()
old = '''\t\t\tcase ir.ONAME:
\t\t\t\tdumpGlobal(n)
\t\t\tcase ir.OLITERAL:
\t\t\t\tdumpGlobalConst(n)
\t\t\tcase ir.OTYPE:
\t\t\t\treflectdata.NeedRuntimeType(n.Type())
\t\t\t}
'''
assert s.count(old) == 1
s = s.replace(old, '''\t\t\tcase ir.ONAME:
\t\t\t\t// The package's variables and types are shard 0's to emit. Static temporaries (the data of
\t\t\t\t// composite literals) belong to whichever shard compiled the function that uses them.
\t\t\t\tif name := n.Sym().Name; noder.SbShard == 0 || (len(name) >= len(obj.StaticNamePrefix) && name[:len(obj.StaticNamePrefix)] == obj.StaticNamePrefix) {
\t\t\t\t\tdumpGlobal(n)
\t\t\t\t}
\t\t\tcase ir.OLITERAL:
\t\t\t\tif noder.SbShard == 0 {
\t\t\t\t\tdumpGlobalConst(n)
\t\t\t\t}
\t\t\tcase ir.OTYPE:
\t\t\t\tif noder.SbShard == 0 {
\t\t\t\t\treflectdata.NeedRuntimeType(n.Type())
\t\t\t\t}
\t\t\t}
''')
old = '\tpkginit.MakeTask()\n'
assert s.count(old) == 1
s = s.replace(old, '\tif noder.SbShard == 0 {\n\t\tpkginit.MakeTask()\n\t}\n')
open(p, 'w').write(s)

p = os.path.join(d, 'obj.go'); s = open(p).read()
old = '\tdumpembeds()\n'
assert s.count(old) == 1
s = s.replace(old, '\tif noder.SbShard == 0 {\n\t\tdumpembeds()\n\t}\n')
open(p, 'w').write(s)

p = os.path.join(reflectd, 'reflect.go'); s = open(p).read()
old = '''\tcase sym.Pkg == types.LocalPkg:
\t\t// Local defined type; our responsibility.
\t\treturn true
'''
assert s.count(old) == 1
s = s.replace(old, '''\tcase sym.Pkg == types.LocalPkg:
\t\t// Local defined type; our responsibility.
\t\t// (sb: unless this process is a shard other than 0 and the type is declared at package level:
\t\t// shard 0 emits those. A type declared inside a function belongs to whoever compiles the function;
\t\t// its symbol name carries a "\\u00b7" and a number. An instantiated generic type belongs to whoever needs
\t\t// it, as it does across packages; the linker keeps one copy.)
\t\tif SbShardOther && !strings.Contains(sym.Name, "\\u00b7") && !typ.IsFullyInstantiated() && !typ.HasShape() {
\t\t\treturn false
\t\t}
\t\treturn true
''')
s += '''
// SbShardOther: this compiler process is a shard of its package, and not shard 0 (see noder/sbshard.go).
var SbShardOther = func() bool {
	k, n, ok := strings.Cut(os.Getenv("SB_SHARD"), "/")
	return ok && k != "0" && n != ""
}()
'''
if '"os"' not in s.split(')')[0]:
    s = s.replace('import (\n', 'import (\n\t"os"\n', 1)
open(p, 'w').write(s)

# (shards, continued) A method named init whose body went to another shard is not a missing package
# initializer: the stock check looks only at the name.
p = os.path.join(noderd, 'writer.go'); s = open(p).read()
old = 'if base.Flag.Complete || decl.Name.Value == "init" {'
assert s.count(old) == 1
s = s.replace(old, 'if base.Flag.Complete || (decl.Name.Value == "init" && (decl.Recv == nil || SbShards == 0)) {')
open(p, 'w').write(s)

# (shards, continued) Only shard 0's object speaks for the package's export data: the others carry no
# fingerprint, like objects made by the assembler, or the linker would see two packages in one archive.
p = os.path.join(d, 'obj.go'); s = open(p).read()
old = '''	if !earlyExport {
		dumpobj1(base.Flag.LowerO, modeCompilerObj)
	}
'''
assert s.count(old) == 1
s = s.replace(old, old + '''	if noder.SbShard != 0 {
		for i := range base.Ctxt.Fingerprint {
			base.Ctxt.Fingerprint[i] = 0
		}
	}
''')
open(p, 'w').write(s)

# Seventh change: with the baseline on and inlining off (-l), skip the devirtualize-and-inline pass. With
# nothing to inline all it does is turn some interface calls into direct ones: an optimization.
p = os.path.join(d, 'main.go'); s = open(p).read()
old = '\tinterleaved.DevirtualizeAndInlinePackage(typecheck.Target, profile)\n'
assert s.count(old) == 1
s = s.replace(old, '\tif os.Getenv("SB_BASELINE") == "" || base.Flag.LowerL != 0 {\n\t\tinterleaved.DevirtualizeAndInlinePackage(typecheck.Target, profile)\n\t}\n')
open(p, 'w').write(s)

# (shards, second version) Function shards drop the other shards' bodies before type checking, and are
# excused the two complaints that causes.
p = os.path.join(noderd, 'irgen.go'); s = open(p).read()
old = '\tdidMidway := false\n'
assert s.count(old) == 1
s = s.replace(old, '\tsbShardFilesPre(noders)\n' + old)
old = '\t\tterr := err.(types2.Error)\n\t\tmsg := terr.Msg\n'
assert s.count(old) == 1
s = s.replace(old, '\t\tterr := err.(types2.Error)\n\t\tif SbShard != 0 && (terr.Code == errors.UnusedImport || terr.Code == errors.MissingInitBody) {\n\t\t\treturn\n\t\t}\n\t\tmsg := terr.Msg\n')
old = '\tbase.ExitIfErrors()\n\tif err != nil {\n\t\tbase.FatalfAt(src.NoXPos, "conf.Check error: %v", err)\n\t}\n'
assert s.count(old) == 1
s = s.replace(old, '\tbase.ExitIfErrors()\n\tif err != nil && SbShard == 0 {\n\t\tbase.FatalfAt(src.NoXPos, "conf.Check error: %v", err)\n\t}\n')
open(p, 'w').write(s)

# Eighth change, in the runtime the compiler is linked with: with SB_POPULATE set, each piece of address
# space the heap grows into (4 MB at a time) is populated with one madvise(MADV_POPULATE_WRITE) call
# instead of a thousand page faults. The compiler runs with the collector off and touches every page once.
p = os.path.join(os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(d)))), 'runtime', 'mheap.go'); s = open(p).read()
old = '\tsysMap(unsafe.Pointer(v), nBase-v, &gcController.heapReleased, "heap")\n'
assert s.count(old) == 1
s = s.replace(old, old + '''\tif environ() != nil && gogetenv("SB_HUGE") != "" && v%(2<<20) == 0 && (nBase-v)%(2<<20) == 0 {
\t\t// Explicit huge pages (the kernel here has no transparent ones): map the new piece of heap from the
\t\t// hugetlb pool. If the pool is empty the call fails before it touches the mapping made above.
\t\tif p, err := mmap(unsafe.Pointer(v), nBase-v, _PROT_READ|_PROT_WRITE, _MAP_ANON|_MAP_FIXED|_MAP_PRIVATE|0x40000, -1, 0); err != 0 || uintptr(p) != v {
\t\t\tmmap(unsafe.Pointer(v), nBase-v, _PROT_READ|_PROT_WRITE, _MAP_ANON|_MAP_FIXED|_MAP_PRIVATE, -1, 0)
\t\t}
\t} else if environ() != nil && gogetenv("SB_POPULATE") != "" {
\t\tmadvise(unsafe.Pointer(v), nBase-v, 23) // MADV_POPULATE_WRITE
\t}
''')
open(p, 'w').write(s)

# Ninth change (measurement only): with SB_CPUPHASES=<dir>, every phase boundary of the compiler's own timer
# appends the process's CPU time and page faults so far to <dir>/<pid>.txt. Phases are sequential, so the
# difference between two lines is that phase's CPU, whatever the machine was doing (the wall-clock timers of
# -bench are useless when back ends run at low priority on a busy machine).
p = os.path.join(os.path.dirname(d), 'base', 'timings.go'); s = open(p).read()
old = '\tt.list = append(t.list, timestamp{time.Now(), strings.Join(labels, ":"), start})\n'
assert s.count(old) == 1
s = s.replace(old, old + '\tsbCPUPhase(strings.Join(labels, ":"), start)\n')
s = s.replace('import (', 'import (\n\t"os"\n\t"syscall"', 1)
s += '''
var sbCPUDir = os.Getenv("SB_CPUPHASES")
var sbCPUFile *os.File

func sbCPUPhase(label string, start bool) {
	if sbCPUDir == "" {
		return
	}
	if sbCPUFile == nil {
		sbCPUFile, _ = os.OpenFile(fmt.Sprintf("%s/%d.txt", sbCPUDir, os.Getpid()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		fmt.Fprintf(sbCPUFile, "pid %d\\n", os.Getpid())
	}
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	fmt.Fprintf(sbCPUFile, "%s %v %d %d %d\\n", label, start, ru.Utime.Sec*1e6+ru.Utime.Usec, ru.Stime.Sec*1e6+ru.Stime.Usec, ru.Minflt)
}
'''
open(p, 'w').write(s)

# Tenth change (measurement only): finer phase boundaries inside the front end, for SB_CPUPHASES.
p = os.path.join(os.path.dirname(d), 'noder', 'unified.go'); s = open(p).read()
for old, new in [
    ('\tdata := writePkgStub(m, noders)\n', '\tbase.Timer.Start("fe", "typecheck")\n\tdata := writePkgStub(m, noders)\n\tbase.Timer.Start("fe", "irread")\n'),
    ('\tpkg, info, otherInfo := checkFiles(m, noders)\n', '\tpkg, info, otherInfo := checkFiles(m, noders)\n\tbase.Timer.Start("fe", "irwrite")\n'),
]:
    assert s.count(old) == 1, old
    s = s.replace(old, new)
open(p, 'w').write(s)

# Eleventh change: one-pass branch sizing in the assembler for the baseline build (obj/x86/sbspan.go).
p = os.path.join(objd, 'x86', 'asm6.go'); s = open(p).read()
old = '\ts.GrowCap(count * 5) // preallocate roughly 5 bytes per instruction\n'
assert s.count(old) == 1
s = s.replace(old, '\tsbPresize(s.Func().Text)\n' + old)
old = '\ts.Size = int64(c)\n\n\tif false { /* debug'
assert s.count(old) == 1
s = s.replace(old, '\ts.Size = int64(c)\n\tsbSpanStat(s.Name, n, s.Size)\n\n\tif false { /* debug')
# and no padding of jumps to 32-byte boundaries in that build (the Intel JCC erratum mitigation, a
# performance measure): it needs every instruction's size from a previous pass, i.e. a second pass always.
old = '\t// Disable jump padding for hand written assembly code.\n'
assert s.count(old) == 1
s = s.replace(old, '\tif sbOnePass {\n\t\treturn padJumpsCtx(0)\n\t}\n' + old)
open(p, 'w').write(s)

# Twelfth change: with -dwarf=false the stock compiler still builds a DWARF subprogram entry and a line
# table for every function (the linker drops them under -w). With SB_NODWARFSYMS set the baseline build
# does not. The stock linker needs those symbols unless it is given -w (without them it panics in
# writedebugaddr: review 2026-11-12), so only the driver sets the variable, and only when its link has -w.
p = os.path.join(objd, 'dwarf.go'); s = open(p).read()
old = '\tinfo, loc, ranges, absfunc, lines := ctxt.dwarfSym(s)\n\tif info.Size != 0 {'
assert s.count(old) == 1
s = s.replace(old, '\tif sbNoDwarf && ctxt.DebugInfo == nil {\n\t\treturn\n\t}\n' + old)
if '\t"os"\n' not in s:
    s = s.replace('import (', 'import (\n\t"os"', 1)
s += '\nvar sbNoDwarf = os.Getenv("SB_BASELINE") != "" && os.Getenv("SB_NODWARFSYMS") != ""\n'
open(p, 'w').write(s)

# Thirteenth change (measurement only): with SB_TYPELOG=<dir>, log every runtime type descriptor this
# process emits: "D name" if any package that needs the type emits it (anonymous, instantiated, shape:
# the linker keeps one copy), "L name" if only this package does.
p = os.path.join(reflectd, 'reflect.go'); s = open(p).read()
old = '''	// Type layout                          Written by               Marker
'''
assert s.count(old) == 1
s = s.replace(old, '''	if sbTypeLogDir != "" {
		sbTypeLogLine(tbase, lsym.Name)
	}
''' + old)
s += '''
var sbTypeLogDir = os.Getenv("SB_TYPELOG")
var sbTypeLogFile *bufio.Writer

func sbTypeLogLine(tbase *types.Type, name string) {
	if sbTypeLogFile == nil {
		f, _ := os.OpenFile(fmt.Sprintf("%s/%d.txt", sbTypeLogDir, os.Getpid()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		sbTypeLogFile = bufio.NewWriterSize(f, 1<<16)
		base.AtExit(func() { sbTypeLogFile.Flush() })
	}
	k := "L"
	if tbase.Sym() == nil || tbase.IsFullyInstantiated() || tbase.HasShape() {
		k = "D"
	}
	fmt.Fprintf(sbTypeLogFile, "%s %s\\n", k, name)
}
'''
for imp in ('"bufio"', '"os"', '"fmt"'):
    if '\t' + imp + '\n' not in s:
        s = s.replace('import (', 'import (\n\t' + imp, 1)
open(p, 'w').write(s)

