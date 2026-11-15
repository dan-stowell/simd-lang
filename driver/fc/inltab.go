// inltab.go: what the stock compiler inlines, by asking it.
//
// The runtime calls small helpers from code that must not be stopped ((*mspan).base, markBits.setMarked,
// (*Type).Pointers, runqempty from sync's spin check, chacha8rand's Refill from rand) and counts on their being
// inlined: no stack check, so no synchronous preemption and no stack growth, and no asynchronous safe point of
// their own. fc does not inline; in the runtime's packages it compiles such a function without a stack check and
// as one unsafe region instead. Which functions: driver/mkfc.sh runs the stock compiler over the runtime and what
// it imports with -m and compiles every "can inline" into fc (stockInl; driver/mkinltab.py). Without the table
// (an fc built some other way) the guess of static.go's inlinable stands in.
package main

import (
	"go/ast"
)

// stockInl: "import path\tname" of every function the stock compiler can inline in the runtime's packages
// (set by the generated zz_inltab.go).
var stockInl map[string]struct{}

// stockIntrinsic: functions outside intrinsicPkg that the stock compiler replaces by instructions, so that a
// call of one is no call at all: runtime.KeepAlive (nosplit runtime.cgocall calls it).
var stockIntrinsic = map[string]bool{"runtime\tKeepAlive": true}

// declName: fd's name as the stock compiler prints it, without type arguments: F, T.M, (*T).M.
func declName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	t, ptr := fd.Recv.List[0].Type, false
	for {
		switch x := t.(type) {
		case *ast.ParenExpr:
			t = x.X
			continue
		case *ast.StarExpr:
			t, ptr = x.X, true
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		}
		break
	}
	id, ok := t.(*ast.Ident)
	if !ok {
		return fd.Name.Name
	}
	if ptr {
		return "(*" + id.Name + ")." + fd.Name.Name
	}
	return id.Name + "." + fd.Name.Name
}

// stockInlines: does the stock compiler inline fd of the package at path? known is false without the table.
func stockInlines(path string, fd *ast.FuncDecl) (yes, known bool) {
	if stockIntrinsic[plainPath(path)+"\t"+declName(fd)] {
		return true, true
	}
	if stockInl == nil {
		return false, false
	}
	_, yes = stockInl[plainPath(path)+"\t"+declName(fd)]
	return yes, true
}

// hasLoop: does the body loop (a function the stock compiler inlines into code a signal may stop, when it is not
// the runtime's own, keeps its safe points if it can run long: bytealg's and stringslite's searches).
func hasLoop(fd *ast.FuncDecl) bool {
	loop := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			loop = true
		}
		return !loop
	})
	return loop
}
