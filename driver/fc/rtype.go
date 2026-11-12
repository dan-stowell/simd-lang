// rtype.go: type descriptors and interface tables.  PROVISIONAL: only the names of types whose descriptors
// some other object defines (basic types, named package-level types, and pointers to those are NOT defined
// here yet). The real implementation writes the descriptors (runtime layout, byte for byte what the stock
// compiler writes) and itabs.
package main

import (
	"go/types"
	"strconv"

	"cmd/internal/obj"
)

// typeSymName is the name of t's descriptor after "type:".
func (c *compiler) typeLinkName(t types.Type) string {
	t = types.Unalias(t)
	switch u := t.(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.UnsafePointer:
			return "unsafe.Pointer"
		case types.Byte:
			return "uint8"
		case types.Rune:
			return "int32"
		}
		return u.Name()
	case *types.Named:
		o := u.Obj()
		if o.Pkg() == nil {
			return o.Name() // error
		}
		if u.TypeArgs().Len() > 0 || o.Parent() != o.Pkg().Scope() {
			giveUp("type descriptor for %v", t)
		}
		return c.pkgPrefix(o.Pkg()) + "." + o.Name()
	case *types.Pointer:
		return "*" + c.typeLinkName(u.Elem())
	case *types.Slice:
		return "[]" + c.typeLinkName(u.Elem())
	case *types.Array:
		return "[" + strconv.FormatInt(u.Len(), 10) + "]" + c.typeLinkName(u.Elem())
	}
	giveUp("type descriptor for %v", t)
	return ""
}

// typeSym is the symbol of t's type descriptor.
func (c *compiler) typeSym(t types.Type) *obj.LSym {
	switch types.Unalias(t).(type) {
	case *types.Basic, *types.Named:
	default:
		giveUp("type descriptor for %v (not written yet)", t)
	}
	return c.lookup("type:"+c.typeLinkName(t), obj.ABI0, "")
}

// itabSym is the symbol of the interface table for the concrete type t as the interface type iface.
func (c *compiler) itabSym(t, iface types.Type) *obj.LSym {
	giveUp("itab for %v as %v (not written yet)", t, iface)
	return nil
}

// isDirectIface: a value of type t is stored in an interface's data word itself (it is one pointer).
func isDirectIface(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Chan, *types.Map, *types.Signature:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	case *types.Array:
		return u.Len() == 1 && isDirectIface(u.Elem())
	case *types.Struct:
		return u.NumFields() == 1 && isDirectIface(u.Field(0).Type())
	}
	return false
}
