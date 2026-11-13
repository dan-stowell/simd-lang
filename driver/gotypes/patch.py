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
