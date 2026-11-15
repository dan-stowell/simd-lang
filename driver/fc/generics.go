// generics.go: generic functions and types are compiled once per instantiation (no shapes, no dictionaries):
// the body is walked again with the type arguments substituted wherever its type information is read.
package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"cmd/internal/obj"
)

// subst applies the type arguments of the instance being compiled to a type from the body's type information.
func (g *gen) subst(t types.Type) types.Type {
	if g.env == nil || t == nil {
		return t
	}
	if r, ok := g.substCache[t]; ok {
		return r
	}
	r := substType(g.c.pr, g.env, t)
	g.substCache[t] = r
	return r
}

func substType(pr *Program, env map[*types.TypeParam]types.Type, t types.Type) types.Type {
	sub := func(t types.Type) types.Type { return substType(pr, env, t) }
	switch t := t.(type) {
	case nil:
		return nil
	case *types.TypeParam:
		if r := env[t]; r != nil {
			return r
		}
		return t
	case *types.Basic:
		return t
	case *types.Alias:
		return sub(types.Unalias(t))
	case *types.Named:
		if o := t.Origin().Obj(); o.Pkg() != nil && o.Parent() != o.Pkg().Scope() && o.Parent() != nil {
			if n := pr.localInstance(env, t); n != nil {
				return n // a type declared inside the generic function
			}
		}
		targs := t.TypeArgs()
		if targs.Len() == 0 {
			return t
		}
		changed := false
		args := make([]types.Type, targs.Len())
		for i := range args {
			args[i] = sub(targs.At(i))
			if args[i] != targs.At(i) {
				changed = true
			}
		}
		if !changed {
			return t
		}
		r, err := types.Instantiate(pr.tctx, t.Origin(), args, false)
		if err != nil {
			giveUp("instantiate %v: %v", t, err)
		}
		return r
	case *types.Pointer:
		if e := sub(t.Elem()); e != t.Elem() {
			return types.NewPointer(e)
		}
		return t
	case *types.Slice:
		if e := sub(t.Elem()); e != t.Elem() {
			return types.NewSlice(e)
		}
		return t
	case *types.Array:
		if e := sub(t.Elem()); e != t.Elem() {
			return types.NewArray(e, t.Len())
		}
		return t
	case *types.Chan:
		if e := sub(t.Elem()); e != t.Elem() {
			return types.NewChan(t.Dir(), e)
		}
		return t
	case *types.Map:
		k, e := sub(t.Key()), sub(t.Elem())
		if k != t.Key() || e != t.Elem() {
			return types.NewMap(k, e)
		}
		return t
	case *types.Struct:
		changed := false
		fs := make([]*types.Var, t.NumFields())
		tags := make([]string, t.NumFields())
		for i := range fs {
			f := t.Field(i)
			ft := sub(f.Type())
			if ft != f.Type() {
				changed = true
				f = types.NewField(f.Pos(), f.Pkg(), f.Name(), ft, f.Embedded())
			}
			fs[i] = f
			tags[i] = t.Tag(i)
		}
		if !changed {
			return t
		}
		return types.NewStruct(fs, tags)
	case *types.Tuple:
		changed := false
		vs := make([]*types.Var, t.Len())
		for i := range vs {
			v := t.At(i)
			vt := sub(v.Type())
			if vt != v.Type() {
				changed = true
				v = types.NewParam(v.Pos(), v.Pkg(), v.Name(), vt)
			}
			vs[i] = v
		}
		if !changed {
			return t
		}
		return types.NewTuple(vs...)
	case *types.Signature:
		ps, rs := sub(t.Params()).(*types.Tuple), sub(t.Results()).(*types.Tuple)
		if ps == t.Params() && rs == t.Results() {
			return t
		}
		return types.NewSignatureType(nil, nil, nil, ps, rs, t.Variadic())
	case *types.Interface:
		changed := false
		var ms []*types.Func
		for i := 0; i < t.NumExplicitMethods(); i++ {
			m := t.ExplicitMethod(i)
			s := sub(m.Type())
			if s != m.Type() {
				changed = true
				m = types.NewFunc(m.Pos(), m.Pkg(), m.Name(), s.(*types.Signature))
			}
			ms = append(ms, m)
		}
		var es []types.Type
		for i := 0; i < t.NumEmbeddeds(); i++ {
			e := sub(t.EmbeddedType(i))
			if e != t.EmbeddedType(i) {
				changed = true
			}
			es = append(es, e)
		}
		if !changed {
			return t
		}
		return types.NewInterfaceType(ms, es).Complete()
	case *types.Union:
		return t
	}
	giveUp("substitution in %T", t)
	return nil
}

// instance is one instantiation of a generic function or method, waiting to be compiled.
type instance struct {
	fn  *types.Func // the generic declaration
	env map[*types.TypeParam]types.Type
	sym *obj.LSym
}

// funcRef: the symbol to call for fn as it is referenced at id (which carries the type arguments of an
// instantiated generic function), and fn's signature with its receiver.
func (g *gen) funcRef(fn *types.Func, id *ast.Ident) (*obj.LSym, *types.Signature) {
	sig := fn.Type().(*types.Signature)
	origin := fn.Origin()
	osig := origin.Type().(*types.Signature)
	switch {
	case osig.TypeParams().Len() > 0:
		// a generic function: the type arguments are recorded at the identifier
		inst, ok := g.info.Instances[id]
		if !ok {
			giveUp("generic function %s without type arguments", fn.Name())
		}
		targs := make([]types.Type, inst.TypeArgs.Len())
		for i := range targs {
			targs[i] = g.subst(inst.TypeArgs.At(i))
		}
		if osig.RecvTypeParams().Len() > 0 {
			// a generic method of a generic type: the receiver's type arguments and the method's own
			rt := sig.Recv().Type()
			if p, ok := rt.(*types.Pointer); ok {
				rt = p.Elem()
			}
			named := types.Unalias(rt).(*types.Named)
			rargs := make([]types.Type, named.TypeArgs().Len())
			for i := range rargs {
				rargs[i] = g.subst(named.TypeArgs().At(i))
			}
			if len(rargs) == 0 {
				giveUp("generic method %s of an uninstantiated generic type", fn.Name())
			}
			rn, err := types.Instantiate(g.c.pr.tctx, named.Origin(), rargs, false)
			if err != nil {
				giveUp("instantiate %v: %v", named, err)
			}
			var recv types.Type = rn
			if _, ok := sig.Recv().Type().(*types.Pointer); ok {
				recv = types.NewPointer(rn)
			}
			rsig := types.NewSignatureType(types.NewParam(0, nil, "", recv), nil, nil, sig.Params(), sig.Results(), sig.Variadic())
			return g.c.instantiateM(origin, osig.RecvTypeParams(), rargs, rn.(*types.Named), osig.TypeParams(), targs), rsig
		}
		return g.c.instantiate(origin, osig.TypeParams(), targs, nil), sig
	case osig.RecvTypeParams().Len() > 0:
		// a method of a generic type: the type arguments are the receiver's
		rt := sig.Recv().Type()
		if p, ok := rt.(*types.Pointer); ok {
			rt = p.Elem()
		}
		named := types.Unalias(rt).(*types.Named)
		targs := make([]types.Type, named.TypeArgs().Len())
		for i := range targs {
			targs[i] = g.subst(named.TypeArgs().At(i))
		}
		if len(targs) == 0 {
			giveUp("method %s of an uninstantiated generic type", fn.Name())
		}
		rn, err := types.Instantiate(g.c.pr.tctx, named.Origin(), targs, false)
		if err != nil {
			giveUp("instantiate %v: %v", named, err)
		}
		// the instantiated receiver decides the ABI
		var recv types.Type = rn
		if _, ok := sig.Recv().Type().(*types.Pointer); ok {
			recv = types.NewPointer(rn)
		}
		rsig := types.NewSignatureType(types.NewParam(0, nil, "", recv), nil, nil, sig.Params(), sig.Results(), sig.Variadic())
		return g.c.instantiate(origin, osig.RecvTypeParams(), targs, rn.(*types.Named)), rsig
	}
	return g.c.funcSym(fn), sig
}

// instantiate returns the symbol of an instance, queueing its compilation the first time.
func (c *compiler) instantiate(origin *types.Func, tparams *types.TypeParamList, targs []types.Type, recv *types.Named) *obj.LSym {
	return c.instantiateM(origin, tparams, targs, recv, nil, nil)
}

// instantiateM is instantiate for a generic method of a generic type as well: recv's type arguments
// (tparams, targs) and the method's own (mtparams, margs; nil otherwise).
func (c *compiler) instantiateM(origin *types.Func, tparams *types.TypeParamList, targs []types.Type, recv *types.Named, mtparams *types.TypeParamList, margs []types.Type) *obj.LSym {
	if f := c.pr.full(origin); f != origin {
		// the body is compiled with the full pass's objects: its type parameters are the ones to bind
		fsig := f.Type().(*types.Signature)
		if recv != nil {
			tparams = fsig.RecvTypeParams()
			if margs != nil {
				mtparams = fsig.TypeParams()
			}
		} else {
			tparams = fsig.TypeParams()
		}
		origin = f
	}
	if tparams.Len() != len(targs) || (margs != nil && mtparams.Len() != len(margs)) {
		giveUp("instantiation of %s with %d type arguments", origin.Name(), len(targs)+len(margs))
	}
	for _, t := range append(append([]types.Type(nil), targs...), margs...) {
		if mentionsTypeParam(t) {
			giveUp("instantiation of %s with an open type argument", origin.Name())
		}
	}
	pp := c.pkgPrefix(origin.Pkg())
	var names []string
	for _, t := range targs {
		names = append(names, c.typeLinkName(t))
	}
	args := "[" + strings.Join(names, ",") + "]"
	var name string
	if recv != nil {
		osig := origin.Type().(*types.Signature)
		base := recv.Obj().Name() + args
		if _, ok := osig.Recv().Type().(*types.Pointer); ok {
			name = pp + ".(*" + base + ")." + origin.Name()
		} else {
			name = pp + "." + base + "." + origin.Name()
		}
		if margs != nil {
			var mnames []string
			for _, t := range margs {
				mnames = append(mnames, c.typeLinkName(t))
			}
			name += "[" + strings.Join(mnames, ",") + "]"
		}
	} else if r := origin.Type().(*types.Signature).Recv(); r != nil {
		// a generic method of an ordinary type
		rt := r.Type()
		ptr := false
		if p, ok := rt.(*types.Pointer); ok {
			rt, ptr = p.Elem(), true
		}
		n, ok := types.Unalias(rt).(*types.Named)
		if !ok || n.TypeArgs().Len() > 0 || n.TypeParams().Len() > 0 {
			giveUp("generic method %s of a generic type", origin.Name())
		}
		if ptr {
			name = pp + ".(*" + n.Obj().Name() + ")." + origin.Name() + args
		} else {
			name = pp + "." + n.Obj().Name() + "." + origin.Name() + args
		}
	} else {
		name = pp + "." + origin.Name() + args
	}
	sym := c.lookup(name, obj.ABIInternal, "_")
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.mixed && !c.instances[name] {
		// in a whole-program build an instance is compiled by the first package that needs it
		if owner, loaded := c.pr.instOwner.LoadOrStore(name, c); loaded && owner != c {
			c.instances[name] = true
			return sym
		}
	}
	if !c.instances[name] {
		c.instances[name] = true
		env := map[*types.TypeParam]types.Type{}
		for i, t := range targs {
			env[tparams.At(i)] = t
		}
		for i, t := range margs {
			env[mtparams.At(i)] = t
		}
		c.instQueue = append(c.instQueue, &instance{fn: origin, env: env, sym: sym})
	}
	return sym
}

func mentionsTypeParam(t types.Type) bool {
	found := false
	var walk func(t types.Type, depth int)
	walk = func(t types.Type, depth int) {
		if found || depth > 20 {
			return
		}
		switch t := t.(type) {
		case *types.TypeParam:
			found = true
		case *types.Alias:
			walk(types.Unalias(t), depth+1)
		case *types.Named:
			for i := 0; i < t.TypeArgs().Len(); i++ {
				walk(t.TypeArgs().At(i), depth+1)
			}
			// a type declared inside a function may mention that function's type parameters in its
			// definition (a few levels of such types at most: the walk has no memory)
			if o := t.Obj(); o != nil && o.Pkg() != nil && o.Parent() != nil && o.Parent() != o.Pkg().Scope() {
				walk(t.Underlying(), depth+6)
			}
		case *types.Pointer:
			walk(t.Elem(), depth+1)
		case *types.Slice:
			walk(t.Elem(), depth+1)
		case *types.Array:
			walk(t.Elem(), depth+1)
		case *types.Chan:
			walk(t.Elem(), depth+1)
		case *types.Map:
			walk(t.Key(), depth+1)
			walk(t.Elem(), depth+1)
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				walk(t.Field(i).Type(), depth+1)
			}
		case *types.Tuple:
			for i := 0; i < t.Len(); i++ {
				walk(t.At(i).Type(), depth+1)
			}
		case *types.Signature:
			walk(t.Params(), depth+1)
			walk(t.Results(), depth+1)
		}
	}
	walk(t, 0)
	return found
}

// declOf finds the declaration of a function in the package that declares it.
func (pr *Program) declOf(fn *types.Func) (*Pkg, *ast.FuncDecl) {
	if fn.Pkg() == nil {
		return nil, nil
	}
	fn = pr.full(fn)
	p := pr.byTypes(fn.Pkg())
	if p == nil {
		return nil, nil
	}
	pr.check(p)
	if p.info == nil {
		return nil, nil
	}
	p.declOnce.Do(func() {
		p.decls = map[*types.Func]*ast.FuncDecl{}
		for _, f := range p.files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok {
					if o, ok := defOf(fd.Name).(*types.Func); ok {
						p.decls[o] = fd
					}
				}
			}
		}
	})
	return p, p.decls[fn]
}

// compileInstance compiles one queued instance.
func (c *compiler) compileInstance(in *instance) {
	p, fd := c.pr.declOf(in.fn)
	if fd == nil || fd.Body == nil {
		giveUp("no body for generic function %s", in.fn.FullName())
	}
	sig := in.fn.Type().(*types.Signature)
	g := c.newGen(in.sym.Name, sig, p.infoFor(fd), in.env)
	g.out.sym = in.sym
	g.out.flags |= obj.DUPOK
	g.pragmaNosplit = hasDirective(fd.Doc, "//go:nosplit")
	g.nosplit = g.pragmaNosplit || (in.fn.Pkg() != nil && intrinsicPkg[in.fn.Pkg().Path()])
	g.noAsync = g.nosplit
	if !g.nosplit && c.pr.inRuntime(p) {
		// an instance of a generic function of the runtime's packages that the stock compiler inlines (inltab.go)
		if yes, _ := stockInlines(p.ImportPath, fd); yes {
			g.nosplit = true
			g.noAsync = runtimeNamed(p.ImportPath) || !hasLoop(fd)
		}
	}
	g.compileFunc(sig.Recv(), fd.Pos(), fd.Body.Rbrace, fd.Body, func() { g.block(fd.Body.List) })
}

// funcRefIsGeneric: is fn a generic function or a method of a generic type?
func (g *gen) funcRefIsGeneric(fn *types.Func) (*types.Signature, bool) {
	osig := fn.Origin().Type().(*types.Signature)
	return osig, osig.TypeParams().Len() > 0 || osig.RecvTypeParams().Len() > 0
}

// localInstance: a type declared inside a generic function, for one instantiation of the function (and for
// one list of its own type arguments, if it is generic itself). As in the stock compiler, the function's type
// parameters are implicit parameters of every type declared in it, used or not: each instantiation of the
// function has its own types, named T[<the function's type arguments>;<the type's own>] (namedSymName).
// t is not such a type (nil is returned) when env has none of the type parameters of a function around it.
func (pr *Program) localInstance(env map[*types.TypeParam]types.Type, t *types.Named) *types.Named {
	o := t.Origin().Obj()
	var outer []*types.TypeParam
	for tp := range env {
		for s := o.Parent(); s != nil; s = s.Parent() {
			if s == tp.Obj().Parent() {
				outer = append(outer, tp)
				break
			}
		}
	}
	if len(outer) == 0 {
		return nil
	}
	sort.Slice(outer, func(i, j int) bool {
		if outer[i].Index() != outer[j].Index() {
			return outer[i].Index() < outer[j].Index()
		}
		return outer[i].Obj().Pos() < outer[j].Obj().Pos()
	})
	li := &localInst{}
	for _, tp := range outer {
		li.outer = append(li.outer, env[tp])
	}
	env2 := env
	if tps := t.Origin().TypeParams(); tps.Len() > 0 {
		env2 = make(map[*types.TypeParam]types.Type, len(env)+tps.Len())
		for k, v := range env {
			env2[k] = v
		}
		for i := 0; i < t.TypeArgs().Len(); i++ {
			a := substType(pr, env, t.TypeArgs().At(i))
			li.own = append(li.own, a)
			env2[tps.At(i)] = a
		}
	}
	same := func(x, y []types.Type) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if !types.Identical(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	// One instance is built at a time (localBuildMu), by the thread that asked first, and nobody else sees
	// it before its definition is in place: the definition may mention the type itself, or another local
	// type that mentions it, so the instance is registered before it is complete, and a second thread
	// that found it there used a type without an underlying type. The requests the build makes itself
	// carry localBuilding in their environment: they take what is registered, complete or not.
	_, nested := env[localBuilding]
	if !nested {
		pr.localBuildMu.Lock()
		defer pr.localBuildMu.Unlock()
	}
	pr.localMu.Lock()
	for _, x := range pr.localInst[o] {
		if same(x.outer, li.outer) && same(x.own, li.own) {
			pr.localMu.Unlock()
			return x.n
		}
	}
	// (this name is for messages and isLocalInstance; descriptors are named by namedSymName, from li)
	var b strings.Builder
	b.WriteString(o.Name() + "[")
	for i, a := range li.outer {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(types.TypeString(a, nil))
	}
	for i, a := range li.own {
		if i == 0 {
			b.WriteByte(';')
		} else {
			b.WriteByte(',')
		}
		b.WriteString(types.TypeString(a, nil))
	}
	b.WriteByte(']')
	li.n = types.NewNamed(types.NewTypeName(o.Pos(), o.Pkg(), b.String(), nil), nil, nil)
	if pr.localInst == nil {
		pr.localInst = map[*types.TypeName][]*localInst{}
		pr.localOf = map[*types.Named]*localInst{}
	}
	pr.localInst[o] = append(pr.localInst[o], li) // before the definition is substituted: it may mention itself
	pr.localOf[li.n] = li
	pr.localMu.Unlock()
	if !nested {
		env3 := make(map[*types.TypeParam]types.Type, len(env2)+1)
		for k, v := range env2 {
			env3[k] = v
		}
		env3[localBuilding] = types.Typ[types.Invalid]
		env2 = env3
	}
	li.n.SetUnderlying(substType(pr, env2, t.Origin().Underlying()))
	return li.n
}

// localBuilding marks the environment of the substitutions made while an instance of a local type is built.
var localBuilding = types.NewTypeParam(types.NewTypeName(token.NoPos, nil, ".building", nil), nil)

// localInst is one instance of a type declared in a generic function: the function's type arguments, and
// the type's own.
type localInst struct {
	n          *types.Named
	outer, own []types.Type
}

// localArgs: the type arguments of a type made by localInstance (ok false for any other type).
func (pr *Program) localArgs(n *types.Named) (outer, own []types.Type, ok bool) {
	pr.localMu.Lock()
	li := pr.localOf[n]
	pr.localMu.Unlock()
	if li == nil {
		return nil, nil, false
	}
	return li.outer, li.own, true
}
