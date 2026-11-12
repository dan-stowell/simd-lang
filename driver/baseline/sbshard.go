// sbshard.go: compiling one package as several shards. (Part of the sb patch set; driver/mkcompiler.sh
// copies this file into cmd/compile/internal/noder.)
//
// With SB_SHARD=k/K the compiler is one of K processes compiling the same package at the same time.
// Each of them parses and type-checks the whole package, so that all agree on every type, every
// initialization order and every symbol name. Then each keeps the bodies of its own share of the
// functions and treats the others as "implemented elsewhere", exactly like functions written in
// assembly. Shard 0 is the package as far as anyone else can tell: it alone emits the package's data
// (global variables and their initialization, type descriptors of the package's types, embedded files)
// and the export data. The other shards emit only the code of their functions; the driver adds their
// object files to shard 0's archive.
//
// What it is for: a package's front end and its code generation are one thread and then a handful.
// MinIO's largest package is 5 MB of Go and was the last 4.7 s of the build, alone on eight cores.
package noder

import (
	"os"
	"strconv"
	"strings"

	"cmd/compile/internal/syntax"
	"cmd/compile/internal/types2"
)

// SbShard, SbShards: this process is shard SbShard of SbShards (SbShards == 0: not sharded).
var SbShard, SbShards = func() (int, int) {
	ks, ns, ok := strings.Cut(os.Getenv("SB_SHARD"), "/")
	if !ok {
		return 0, 0
	}
	k, err1 := strconv.Atoi(ks)
	n, err2 := strconv.Atoi(ns)
	if err1 != nil || err2 != nil || n < 2 || k < 0 || k >= n {
		return 0, 0
	}
	return k, n
}()

// sbAssign gives every function declaration with a body its shard, from the syntax alone so that every
// shard computes the same answer: -1 for a function every shard keeps (generic ones: any shard may need
// to instantiate them, and the linker keeps one copy of each instance), 0 for package initializers,
// 1..K-1 for the rest, each going to the least loaded function shard so far. Shard 0 compiles no
// ordinary function: it is the one that type-checks every body (the order in which package-level
// variables are initialized depends on what functions refer to), and it emits the package's data.
func sbAssign(noders []*noder) map[*syntax.FuncDecl]int {
	out := map[*syntax.FuncDecl]int{}
	load := make([]int, SbShards)
	for _, p := range noders {
		for _, d := range p.file.DeclList {
			fd, ok := d.(*syntax.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			switch {
			case fd.Recv == nil && fd.Name.Value == "init":
				out[fd] = 0
			case fd.Name.Value == "_" || fd.TParamList != nil || sbGenericRecv(fd):
				out[fd] = -1
			default:
				best := 1
				for i := 2; i < SbShards; i++ {
					if load[i] < load[best] {
						best = i
					}
				}
				load[best] += int(fd.Body.Rbrace.Line()) - int(fd.Body.Pos().Line()) + 1
				out[fd] = best
			}
		}
	}
	return out
}

// sbGenericRecv: a method of a generic type (its receiver type has type arguments)
func sbGenericRecv(fd *syntax.FuncDecl) bool {
	if fd.Recv == nil {
		return false
	}
	t := fd.Recv.Type
	for {
		switch x := t.(type) {
		case *syntax.ParenExpr:
			t = x.X
			continue
		case *syntax.Operation:
			if x.Y == nil { // *T
				t = x.X
				continue
			}
		case *syntax.IndexExpr:
			return true
		}
		return false
	}
}

func sbDrop(noders []*noder, assign map[*syntax.FuncDecl]int) {
	for _, p := range noders {
		decls := p.file.DeclList[:0]
		for _, d := range p.file.DeclList {
			if fd, ok := d.(*syntax.FuncDecl); ok {
				if k, has := assign[fd]; has && k != -1 && k != SbShard {
					if k == 0 {
						continue // a package initializer, in a shard other than 0: not this shard's business at all
					}
					fd.Body = nil
				}
			}
			decls = append(decls, d)
		}
		p.file.DeclList = decls
	}
}

// sbShardFilesPre runs before type checking in the function shards: bodies of other shards go at once,
// so this shard does not pay to check them. (It then sees imports that nothing uses and a main without
// a body; checkFiles ignores exactly those two complaints in a function shard.)
func sbShardFilesPre(noders []*noder) {
	if SbShards == 0 || SbShard == 0 {
		return
	}
	sbDrop(noders, sbAssign(noders))
}

// sbShardFiles runs after type checking. In shard 0 this is where the function bodies go.
func sbShardFiles(noders []*noder, info *types2.Info) {
	if SbShards == 0 {
		return
	}
	if SbShard != 0 {
		info.InitOrder = nil // package-level variables are initialized by shard 0's init function
		return
	}
	sbDrop(noders, sbAssign(noders))
}
