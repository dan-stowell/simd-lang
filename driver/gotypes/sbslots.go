// sbslots.go (added to go/types in the patched GOROOT by driver/gotypes/patch.py): what the checker learns
// about an expression is stored in the expression's node, not only in Info's maps.
//
// Why: Info.Types, Defs, Uses and Selections are hash maps keyed by node. For MinIO's closure that is five
// million expressions: a quarter of a whole build's CPU was map code, about half of it the checker writing
// these maps and the code generator reading them back, and the maps were a gigabyte of fresh memory. A
// node the reader is already looking at answers with one load. (types2 does the same for cmd/compile:
// StoreTypesInSyntax; go/types kept the hooks as empty functions.)
//
// With SBSlots set (before any check starts), and whether or not Info has the maps:
//
//	every ast.Expr   SB().TV      *TypeAndValue          SBTypeOf
//	*ast.Ident       SBObj/SBKind the object it defines or uses    SBDef, SBUse
//	*ast.SelectorExpr SBSel       *Selection             SBSelection
//
// An identifier that both defines and uses an object (an embedded field: the field, and its type's name)
// keeps the definition in the node and the use in a side table. Implicits, Instances and Scopes stay maps.
// The fields are written by the one checker that checks the node and read after that check is over.
package types

import (
	"go/ast"
	"sync"
	"unsafe"
)

// SBSlots turns recording into the nodes on.
var SBSlots bool

const (
	sbDef     = 1 << iota // SBObj is the object the identifier defines (it may be nil)
	sbUse                 // SBObj is the object the identifier uses
	sbUseSide             // the object the identifier uses is in sbSideUses
)

var sbSideUses sync.Map // *ast.Ident -> Object

const sbChunk = 256

func (check *Checker) sbRecordTV(x ast.Expr, tv TypeAndValue) {
	s := x.SB()
	if p := (*TypeAndValue)(s.TV); p != nil {
		*p = tv
		return
	}
	if len(check.sbTVs) == cap(check.sbTVs) {
		check.sbTVs = make([]TypeAndValue, 0, sbChunk)
	}
	check.sbTVs = append(check.sbTVs, tv)
	s.TV = unsafe.Pointer(&check.sbTVs[len(check.sbTVs)-1])
}

func sbObj(id *ast.Ident) *Object { return (*Object)(unsafe.Pointer(&id.SBObj)) }

func (check *Checker) sbRecordDef(id *ast.Ident, obj Object) {
	if id.SBKind&sbUse != 0 {
		sbSideUses.Store(id, *sbObj(id))
		id.SBKind = id.SBKind&^sbUse | sbUseSide
	}
	*sbObj(id) = obj
	id.SBKind |= sbDef
}

func (check *Checker) sbRecordUse(id *ast.Ident, obj Object) {
	if id.SBKind&(sbDef|sbUseSide) != 0 {
		sbSideUses.Store(id, obj)
		id.SBKind |= sbUseSide
		return
	}
	*sbObj(id) = obj
	id.SBKind |= sbUse
}

// SBTypeOf is Info.Types[x] read from the node.
func SBTypeOf(x ast.Expr) (TypeAndValue, bool) {
	if x == nil {
		return TypeAndValue{}, false
	}
	if p := (*TypeAndValue)(x.SB().TV); p != nil {
		return *p, true
	}
	return TypeAndValue{}, false
}

// SBSetType records tv for x, as the checker would (for nodes a tool makes itself).
func SBSetType(x ast.Expr, tv TypeAndValue) {
	s := x.SB()
	if p := (*TypeAndValue)(s.TV); p != nil {
		*p = tv
		return
	}
	s.TV = unsafe.Pointer(&tv)
}

// SBDef is Info.Defs[id] read from the node.
func SBDef(id *ast.Ident) (Object, bool) {
	if id == nil || id.SBKind&sbDef == 0 {
		return nil, false
	}
	return *sbObj(id), true
}

// SBUse is Info.Uses[id] read from the node.
func SBUse(id *ast.Ident) (Object, bool) {
	if id == nil {
		return nil, false
	}
	if id.SBKind&sbUse != 0 {
		return *sbObj(id), true
	}
	if id.SBKind&sbUseSide != 0 {
		if o, ok := sbSideUses.Load(id); ok {
			return o.(Object), true
		}
	}
	return nil, false
}

// SBSetDef and SBSetUse record an object for an identifier a tool makes itself.
func SBSetDef(id *ast.Ident, obj Object) { (*Checker)(nil).sbRecordDef(id, obj) }
func SBSetUse(id *ast.Ident, obj Object) { (*Checker)(nil).sbRecordUse(id, obj) }

// SBSelection is Info.Selections[x] read from the node.
func SBSelection(x *ast.SelectorExpr) (*Selection, bool) {
	if x == nil || x.SBSel == nil {
		return nil, false
	}
	return (*Selection)(x.SBSel), true
}

// sbFactSlot is a word in every type object but *Basic and *Union (driver/gotypes/patch.py).
type sbFactSlot struct{ p unsafe.Pointer }

// SBFacts is the address of a word that belongs to the type object t, for whatever its user keeps about the
// type (a link name, a size): read and written with sync/atomic. nil for *Basic, *Union and types of other
// implementations. A fact on the object is one load where a map keyed by the type is a hash and a miss.
func SBFacts(t Type) *unsafe.Pointer {
	switch t := t.(type) {
	case *Named:
		return &t.sbf.p
	case *Pointer:
		return &t.sbf.p
	case *Struct:
		return &t.sbf.p
	case *Signature:
		return &t.sbf.p
	case *Slice:
		return &t.sbf.p
	case *Interface:
		return &t.sbf.p
	case *Map:
		return &t.sbf.p
	case *Array:
		return &t.sbf.p
	case *Tuple:
		return &t.sbf.p
	case *Chan:
		return &t.sbf.p
	case *TypeParam:
		return &t.sbf.p
	case *Alias:
		return &t.sbf.p
	}
	return nil
}
