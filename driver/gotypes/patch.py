#!/usr/bin/env python3
"""Patch go/types in the patched GOROOT for driver/gotypes/sbbodies.go: a Checker with sbBodies set
collects function bodies instead of dropping them.   patch.py <pristine go/types dir> <patched go/types dir>"""
import shutil, sys
src, dst = sys.argv[1], sys.argv[2]
def patch(name, old, new):
    s = open(f"{src}/{name}").read()
    assert s.count(old) == 1, (name, old)
    open(f"{dst}/{name}", "w").write(s.replace(old, new))
patch("check.go", "\tindent   int          // indentation for tracing\n",
      "\tindent   int          // indentation for tracing\n\n\tsbBodies *Bodies // if set, function bodies are collected here instead of being ignored (sbbodies.go)\n")
patch("decl.go", "\tif !check.conf.IgnoreFuncBodies && fdecl.Body != nil {\n",
      "\tif check.sbBodies != nil && fdecl.Body != nil {\n"
      "\t\tcheck.sbBodies.jobs = append(check.sbBodies.jobs, bodyJob{decl, obj.name, sig, fdecl.Body, nil, check.version, false})\n"
      "\t} else if !check.conf.IgnoreFuncBodies && fdecl.Body != nil {\n")
patch("literals.go", "\t\tif !check.conf.IgnoreFuncBodies && e.Body != nil {\n",
      "\t\tif check.sbBodies != nil && e.Body != nil {\n"
      "\t\t\tcheck.sbBodies.jobs = append(check.sbBodies.jobs, bodyJob{check.decl, \"<function literal>\", sig, e.Body, check.iota, check.version, true})\n"
      "\t\t} else if !check.conf.IgnoreFuncBodies && e.Body != nil {\n")
shutil.copy(__file__.rsplit("/", 1)[0] + "/sbbodies.go", f"{dst}/sbbodies.go")

# ---- sbslots.go: the checker's results stored in the nodes (go/ast gets the fields)
def patchn(name, pairs, src=src, dst=dst):
    s = open(f"{src}/{name}").read()
    for old, new in pairs:
        assert s.count(old) == 1, (name, old)
        s = s.replace(old, new)
    open(f"{dst}/{name}", "w").write(s)
shutil.copy(__file__.rsplit("/", 1)[0] + "/sbslots.go", f"{dst}/sbslots.go")
# (check.go was written above: patch the patched copy)
patchn("check.go", [
    ("\tsbBodies *Bodies // if set,", "\tsbTVs    []TypeAndValue // sbslots.go: the chunk the next recorded TypeAndValue comes from\n\tsbBodies *Bodies // if set,"),
    ("func (check *Checker) recordTypeAndValueInSyntax(x ast.Expr, mode operandMode, typ Type, val constant.Value) {\n\t// nothing to do\n}",
     "func (check *Checker) recordTypeAndValueInSyntax(x ast.Expr, mode operandMode, typ Type, val constant.Value) {\n\tif SBSlots {\n\t\tcheck.sbRecordTV(x, TypeAndValue{mode, typ, val})\n\t}\n}"),
    ("func (check *Checker) recordCommaOkTypesInSyntax(x ast.Expr, t0, t1 Type) {\n\t// nothing to do\n}",
     "func (check *Checker) recordCommaOkTypesInSyntax(x ast.Expr, t0, t1 Type) {\n\t// (sbslots.go; the caller's loop over the maps is repeated here over the nodes)\n}"),
], src=dst)
patchn("recording.go", [
    ("\tif m := check.Types; m != nil {\n\t\tfor {\n\t\t\ttv := m[x]\n",
     "\tif SBSlots {\n\t\tfor x := x; ; {\n\t\t\ttv, _ := SBTypeOf(x)\n\t\t\tassert(tv.Type != nil) // should have been recorded already\n\t\t\tpos := x.Pos()\n\t\t\ttv.Type = NewTuple(\n\t\t\t\tnewVar(LocalVar, pos, check.pkg, \"\", t0),\n\t\t\t\tnewVar(LocalVar, pos, check.pkg, \"\", t1),\n\t\t\t)\n\t\t\tcheck.sbRecordTV(x, tv)\n\t\t\tp, _ := x.(*ast.ParenExpr)\n\t\t\tif p == nil {\n\t\t\t\tbreak\n\t\t\t}\n\t\t\tx = p.X\n\t\t}\n\t}\n\tif m := check.Types; m != nil {\n\t\tfor {\n\t\t\ttv := m[x]\n"),
    ("\tassert(id != nil)\n\tif m := check.Defs; m != nil {", "\tassert(id != nil)\n\tif SBSlots {\n\t\tcheck.sbRecordDef(id, obj)\n\t}\n\tif m := check.Defs; m != nil {"),
    ("\tassert(obj != nil)\n\tif m := check.Uses; m != nil {", "\tassert(obj != nil)\n\tif SBSlots {\n\t\tcheck.sbRecordUse(id, obj)\n\t}\n\tif m := check.Uses; m != nil {"),
    ("\tcheck.recordUse(x.Sel, obj)\n\tif m := check.Selections; m != nil {\n\t\tm[x] = &Selection{kind, recv, obj, index, indirect}\n\t}",
     "\tcheck.recordUse(x.Sel, obj)\n\tif SBSlots {\n\t\tsel := &Selection{kind, recv, obj, index, indirect}\n\t\tx.SBSel = unsafe.Pointer(sel)\n\t\tif m := check.Selections; m != nil {\n\t\t\tm[x] = sel\n\t\t}\n\t\treturn\n\t}\n\tif m := check.Selections; m != nil {\n\t\tm[x] = &Selection{kind, recv, obj, index, indirect}\n\t}"),
    ('import (\n', 'import (\n\t"unsafe"\n'),
])
patchn("api.go", [("\treturn info.Types != nil\n", "\treturn info.Types != nil || SBSlots\n")])
asrc, adst = src + "/../ast", dst + "/../ast"
exprs = ["BadExpr", "Ident", "Ellipsis", "BasicLit", "FuncLit", "CompositeLit", "ParenExpr", "SelectorExpr", "IndexExpr",
         "IndexListExpr", "SliceExpr", "TypeAssertExpr", "CallExpr", "StarExpr", "UnaryExpr", "BinaryExpr", "KeyValueExpr",
         "ArrayType", "StructType", "FuncType", "InterfaceType", "MapType", "ChanType"]
pairs = [(f"\t{n} struct {{\n", f"\t{n} struct {{\n\t\tSBSlot\n" +
          ("\t\tSBObj  [2]unsafe.Pointer // go/types sbslots.go: the types.Object defined or used\n\t\tSBKind uint8\n" if n == "Ident" else "") +
          ("\t\tSBSel unsafe.Pointer // go/types sbslots.go: *types.Selection\n" if n == "SelectorExpr" else "")) for n in exprs]
pairs += [("type Expr interface {\n\tNode\n\texprNode()\n}",
           "type Expr interface {\n\tNode\n\texprNode()\n\tSB() *SBSlot\n}\n\n"
           "// SBSlot (driver/gotypes/patch.py) is in every expression node: what the type checker recorded for it\n"
           "// (go/types sbslots.go). TV is a *types.TypeAndValue.\n"
           "type SBSlot struct{ TV unsafe.Pointer }\n\n"
           "func (s *SBSlot) SB() *SBSlot { return s }"),
          ('import (\n\t"go/token"\n', 'import (\n\t"go/token"\n\t"unsafe"\n'),
          ("return &Ident{token.NoPos, name, nil}", "return &Ident{NamePos: token.NoPos, Name: name}")]
patchn("ast.go", pairs, src=asrc, dst=adst)
