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

// sbShardFiles runs after type checking: it empties every function body that belongs to another shard.
func sbShardFiles(noders []*noder, info *types2.Info) {
	if SbShards == 0 {
		return
	}
	load := make([]int, SbShards)
	for _, p := range noders {
		decls := p.file.DeclList[:0]
		for _, d := range p.file.DeclList {
			fd, ok := d.(*syntax.FuncDecl)
			if !ok || fd.Body == nil {
				decls = append(decls, d)
				continue
			}
			if fd.Recv == nil && fd.Name.Value == "init" {
				// initialization belongs to shard 0, like everything else that is not just a function
				if SbShard == 0 {
					decls = append(decls, d)
				}
				continue
			}
			decls = append(decls, d)
			if fd.Name.Value == "_" {
				continue
			}
			if obj, ok := info.Defs[fd.Name].(*types2.Func); ok {
				sig := obj.Type().(*types2.Signature)
				if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
					continue // generic: every shard may need to instantiate it; instances are deduplicated by the linker
				}
			} else {
				continue
			}
			// the least loaded shard so far gets it; every shard computes the same assignment
			best := 0
			for i := range load {
				if load[i] < load[best] {
					best = i
				}
			}
			load[best] += int(fd.Body.Rbrace.Line()) - int(fd.Body.Pos().Line()) + 1
			if best != SbShard {
				fd.Body = nil
			}
		}
		p.file.DeclList = decls
	}
	if SbShard != 0 {
		info.InitOrder = nil // package-level variables are initialized by shard 0's init function
	}
}
