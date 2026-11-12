#!/usr/bin/env bash
# Build build/compile-esc: the stock compiler with one change, used only by fc's mixed-mode tests to write
# the export data of the package under test: every parameter that holds pointers is reported as escaping.
# Why: stock-compiled callers put arguments on their stack when the stock compiler's escape analysis of the
# callee says they do not escape; fc has no escape analysis and may store such an argument in the heap
# (an address-taken parameter lives there), which the collector rejects and a stack move breaks. In a build
# done entirely by fc there are no such callers.
set -e
cd "$(dirname "$0")/../.."
src=$(GOTOOLCHAIN=local go env GOROOT)
dst=/home/exedev/targets/goroot-esc
[ -d "$dst" ] || cp -a "$src" "$dst"
f=$dst/src/cmd/compile/internal/escape/escape.go
cp "$src/src/cmd/compile/internal/escape/escape.go" "$f"
python3 - "$f" <<'PY'
import sys
p=sys.argv[1]; s=open(p).read()
a='''	if fn.Pragma&ir.UintptrEscapes != 0 {
		if f.Type.IsUintptr() {'''
assert a in s
s=s.replace(a,'''	if f.Type.HasPointers() {
		// fc mixed mode: callers must assume the argument reaches the heap
		var esc leaks
		esc.AddHeap(0)
		return esc.Encode()
	}

'''+a,1)
open(p,'w').write(s)
PY
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build -o "$OLDPWD/build/compile-esc" cmd/compile)
echo "built build/compile-esc"
