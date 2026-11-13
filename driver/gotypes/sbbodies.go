// sbbodies.go (added to go/types in the patched GOROOT by driver/gotypes/patch.py): a package checked in two
// steps that share one *Package. CheckDecls checks everything but function bodies, as Check does with
// IgnoreFuncBodies, and keeps what Check would have done next: the list of bodies. (*Bodies).Check checks
// some of them with a Checker of its own, so disjoint sets can be checked at the same time, each
// recording into its own Info, while importers already use the package.
//
// Why: a full check is serial, and a build's wall clock is its longest chain; MinIO's cmd package is
// checked for 1.3 s on one core. And a build that checks declarations first for the importers' sake need
// not check them again for the bodies.
//
// What is shared between body checkers is read only after CheckDecls returns: the package's scope and
// objects (complete: every package-level object has its type and the cleanup has computed the type sets
// of its interfaces), and objMap. What a body checker writes is its own: the scopes below the function's
// signature, its Info, its untyped-expression map, its delayed actions, and the deps of the declInfo of
// the function it is checking. Function literals outside functions (in the initializers of package-level
// variables) share their variable's declInfo, so all of them must go to one Check call (IsLit).
package types

import (
	"go/ast"
	"go/constant"
	"go/token"
)

type bodyJob struct {
	decl    *declInfo
	name    string
	sig     *Signature
	body    *ast.BlockStmt
	iota    constant.Value
	version goVersion
	lit     bool
}

// Bodies is the work a declarations-only check left: the function bodies of a package.
type Bodies struct {
	conf    Config
	fset    *token.FileSet
	pkg     *Package
	objMap  map[Object]*declInfo
	objList []Object
	nextID  uint64
	jobs    []bodyJob
}

// CheckDecls is Check without function bodies; the bodies are returned to be checked later.
func (conf *Config) CheckDecls(path string, fset *token.FileSet, files []*ast.File, info *Info) (*Package, *Bodies, error) {
	c := *conf
	c.IgnoreFuncBodies = true
	pkg := NewPackage(path, "")
	check := NewChecker(&c, fset, pkg, info)
	b := &Bodies{conf: *conf, fset: fset, pkg: pkg}
	check.sbBodies = b
	err := check.Files(files)
	b.objMap, b.objList, b.nextID = check.objMap, check.objList, check.nextID
	return pkg, b, err
}

// Len is the number of bodies.
func (b *Bodies) Len() int { return len(b.jobs) }

// Size is the length of body i in source bytes.
func (b *Bodies) Size(i int) int { return int(b.jobs[i].body.End() - b.jobs[i].body.Pos()) }

// IsLit: is body i a function literal outside any function? All of those belong in one Check call.
func (b *Bodies) IsLit(i int) bool { return b.jobs[i].lit }

// Check checks the bodies idx, recording into info and reporting errors to onErr. Calls with disjoint
// idx and distinct info may run concurrently. shard distinguishes the callers (any small number).
func (b *Bodies) Check(shard int, idx []int, info *Info, onErr func(error)) (err error) {
	c := b.conf
	c.IgnoreFuncBodies = false
	c.Error = onErr
	check := NewChecker(&c, b.fset, b.pkg, info)
	check.objMap = b.objMap
	check.nextID = b.nextID + uint64(shard+1)<<40
	defer check.handleBailout(&err)
	for _, i := range idx {
		j := &b.jobs[i]
		check.version = j.version
		check.funcBody(j.decl, j.name, j.sig, j.body, j.iota)
		check.processDelayed(0) // the function literals inside
	}
	check.cleanup()
	check.recordUntyped()
	return
}

// InitOrder computes info.InitOrder; call it when every body has been checked (the order depends on
// which package-level variables and functions the bodies refer to).
func (b *Bodies) InitOrder(info *Info, onErr func(error)) (err error) {
	c := b.conf
	c.Error = onErr
	check := NewChecker(&c, b.fset, b.pkg, info)
	check.objMap, check.objList = b.objMap, b.objList
	defer check.handleBailout(&err)
	check.initOrder()
	return
}
