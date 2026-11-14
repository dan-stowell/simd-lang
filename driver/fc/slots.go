// slots.go: what the type checker recorded about a node, read from the node (go/types sbslots.go in the
// patched GOROOT; driver/gotypes). The maps of types.Info (Types, Defs, Uses, Selections) are not made:
// for MinIO they were five million entries, a gigabyte, and half of the build's time in hash maps.
package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
)

// slotCheck (FC_SLOTCHECK=1): the differential test of the slots against the stock checker's own record.
// The Infos get their maps as well, and when the build is over every node of every package is compared
// both ways: each map entry against the node, each node's slot against the maps.
var slotCheck = os.Getenv("FC_SLOTCHECK") != ""

func (pr *Program) checkSlots() {
	var nodes, entries, bad, outside int64
	report := func(p *Pkg, what string, n ast.Node) {
		bad++
		if bad <= 20 {
			fmt.Printf("slotcheck: %s: %s at %v\n", p.ImportPath, what, pr.fset.Position(n.Pos()))
		}
	}
	for _, p := range pr.byPath {
		if p.info == nil || len(p.files) == 0 {
			continue
		}
		tys := map[ast.Expr]types.TypeAndValue{}
		defs := map[*ast.Ident]types.Object{}
		uses := map[*ast.Ident]types.Object{}
		sels := map[*ast.SelectorExpr]*types.Selection{}
		for _, in := range p.infos() {
			for k, v := range in.Types {
				tys[k] = v
			}
			for k, v := range in.Defs {
				defs[k] = v
			}
			for k, v := range in.Uses {
				uses[k] = v
			}
			for k, v := range in.Selections {
				sels[k] = v
			}
		}
		entries += int64(len(tys) + len(defs) + len(uses) + len(sels))
		p.eachNode(func(n ast.Node) {
			nodes++
			if e, ok := n.(ast.Expr); ok {
				tv, ok1 := types.SBTypeOf(e)
				mv, ok2 := tys[e]
				if ok1 != ok2 || tv.Type != mv.Type || tv.Value != mv.Value || tv.IsValue() != mv.IsValue() || tv.IsType() != mv.IsType() || tv.Addressable() != mv.Addressable() || tv.HasOk() != mv.HasOk() {
					// (a tuple made for a comma-ok expression is a new object each time it is recorded)
					t1, _ := tv.Type.(*types.Tuple)
					t2, _ := mv.Type.(*types.Tuple)
					if !(ok1 && ok2 && t1 != nil && t2 != nil && types.Identical(t1, t2)) {
						report(p, fmt.Sprintf("type of a %T: node %v %v, maps %v %v (%d infos)", n, ok1, tv.Type, ok2, mv.Type, len(p.infos())), n)
					}
				}
				delete(tys, e)
			}
			switch x := n.(type) {
			case *ast.Ident:
				o, ok1 := types.SBDef(x)
				m, ok2 := defs[x]
				if ok1 != ok2 || o != m {
					report(p, "def of "+x.Name, n)
				}
				o, ok1 = types.SBUse(x)
				m, ok2 = uses[x]
				if ok1 != ok2 || o != m {
					report(p, "use of "+x.Name, n)
				}
				delete(defs, x)
				delete(uses, x)
			case *ast.SelectorExpr:
				sl, ok1 := types.SBSelection(x)
				m, ok2 := sels[x]
				if ok1 != ok2 || sl != m {
					report(p, "selection", n)
				}
				delete(sels, x)
			}
		})
		// what the maps hold for nodes that are not in the files (none expected)
		// What the maps hold for nodes that are not in the files: nodes the checker makes itself (the
		// operand of x++ and the like). They have their slots too; compare them through the maps' keys.
		for e, mv := range tys {
			outside++
			if tv, ok := types.SBTypeOf(e); !ok || tv.Type != mv.Type || tv.Value != mv.Value {
				report(p, fmt.Sprintf("type of a %T outside the files", e), e)
			}
		}
		for id, m := range defs {
			outside++
			if o, ok := types.SBDef(id); !ok || o != m {
				report(p, "def outside the files", id)
			}
		}
		for id, m := range uses {
			outside++
			if o, ok := types.SBUse(id); !ok || o != m {
				report(p, "use outside the files", id)
			}
		}
		for x, m := range sels {
			outside++
			if sl, ok := types.SBSelection(x); !ok || sl != m {
				report(p, "selection outside the files", x)
			}
		}
	}
	fmt.Printf("slotcheck: %d nodes, %d map entries (%d for nodes the checker made), %d differences\n", nodes, entries, outside, bad)
	if bad > 0 {
		os.Exit(1)
	}
}

func init() { types.SBSlots = true }

// tvOf is Info.Types[e]: the zero value when the checker recorded nothing.
func tvOf(e ast.Expr) types.TypeAndValue {
	tv, _ := types.SBTypeOf(e)
	return tv
}

// defOf is Info.Defs[id], useOf Info.Uses[id], selOf Info.Selections[e].
func defOf(id *ast.Ident) types.Object {
	o, _ := types.SBDef(id)
	return o
}

func useOf(id *ast.Ident) types.Object {
	o, _ := types.SBUse(id)
	return o
}

func selnOf(e *ast.SelectorExpr) *types.Selection {
	s, _ := types.SBSelection(e)
	return s
}

// objOf is Info.ObjectOf(id): the object id defines, else the one it uses.
func objOf(id *ast.Ident) types.Object {
	if o := defOf(id); o != nil {
		return o
	}
	return useOf(id)
}

// eachNode calls f for every node of the package's files.
func (p *Pkg) eachNode(f func(n ast.Node)) {
	for _, file := range p.files {
		ast.Inspect(file, func(n ast.Node) bool {
			if n != nil {
				f(n)
			}
			return true
		})
	}
}
