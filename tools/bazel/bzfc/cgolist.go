// cgolist.go: the default way of cgo.go: the cgo packages are fc's (driver/fc/cgo.go runs cgo and gcc).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// listCgo: the plan entries of the cgo packages as fc compiles them, from `go list -json` in a cgo
// configuration over a farm of their directories, and runtime/cgo's from the standard library in that
// configuration. The go command compiles nothing here.
func (s *stockSet) listCgo(roots []*Pkg, tag string) error {
	t0 := time.Now()
	gopath := filepath.Join(*flagWork, "gopath."+tag)
	src := filepath.Join(gopath, "src")
	if err := farmPkgs(src, roots); err != nil {
		return err
	}
	tags := map[string]bool{"netgo": true, "osusergo": true}
	byPath := map[string]*Pkg{}
	var names []string
	for _, r := range roots {
		byPath[r.ImportPath] = r
		names = append(names, r.ImportPath)
		for _, t := range r.a.args["-tags"] {
			for _, x := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == ' ' }) {
				tags[x] = true
			}
		}
	}
	var tl []string
	for t := range tags {
		tl = append(tl, t)
	}
	sort.Strings(tl)
	gobin := filepath.Dir(*flagGo)
	path := "PATH=" + gobin + ":" + os.Getenv("PATH")
	gocache := "GOCACHE=" + filepath.Join(*flagWork, "gocache")
	listEnv := []string{path, "GOPATH=" + gopath, "GO111MODULE=off", "CGO_ENABLED=1", "GOFLAGS=-tags=" + strings.Join(tl, ","),
		gocache, "GOTOOLCHAIN=local", "GOOS=linux", "GOARCH=amd64"}
	cmd := exec.Command(*flagGo, append(append([]string{"list", "-e", "-json"}, names...), "runtime/cgo")...)
	cmd.Dir = src
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "SB_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, listEnv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outb, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list (cgo packages: %s): %v\n%s", strings.Join(names, " "), err, stderr.String())
	}
	s.entries = map[string]json.RawMessage{}
	dec := json.NewDecoder(bytes.NewReader(outb))
	for dec.More() {
		var e struct {
			Pkg
			Standard bool
			Error    *struct{ Err string }
		}
		if err := dec.Decode(&e); err != nil {
			return err
		}
		if e.Error != nil {
			return fmt.Errorf("go list %s: %s", e.ImportPath, e.Error.Err)
		}
		if e.ImportPath == "runtime/cgo" {
			std := struct {
				Pkg
				Standard bool
			}{e.Pkg, true}
			b, _ := json.Marshal(std)
			s.entries[e.ImportPath] = b
			continue
		}
		r := byPath[e.ImportPath]
		if r == nil {
			return fmt.Errorf("go list: %s is not a cgo package of the closure", e.ImportPath)
		}
		// Bazel's view (imports, import map, embeds), go list's of the files and #cgo flags, in the farm's
		// directory (symlinks to the package's files; fc runs cgo and gcc there)
		q := *r
		q.Dir, q.GoFiles, q.SFiles = e.Dir, e.GoFiles, e.SFiles
		q.CgoFiles, q.CFiles, q.CXXFiles, q.MFiles, q.FFiles, q.HFiles = e.CgoFiles, e.CFiles, e.CXXFiles, e.MFiles, e.FFiles, e.HFiles
		q.SwigFiles, q.SwigCXXFiles = e.SwigFiles, e.SwigCXXFiles
		q.CgoCFLAGS, q.CgoCPPFLAGS, q.CgoCXXFLAGS = e.CgoCFLAGS, e.CgoCPPFLAGS, e.CgoCXXFLAGS
		q.CgoFFLAGS, q.CgoLDFLAGS, q.CgoPkgConfig = e.CgoFFLAGS, e.CgoLDFLAGS, e.CgoPkgConfig
		for _, rel := range r.EmbedFiles {
			// (fc's embed reads files under Dir: the farm has them where Bazel's embed roots put them)
			if _, err := os.Stat(filepath.Join(q.Dir, rel)); err != nil {
				return fmt.Errorf("cgo package %s: embed %s not in the farm", r.ImportPath, rel)
			}
		}
		b, _ := json.Marshal(q)
		s.entries[e.ImportPath] = b
	}
	if s.entries["runtime/cgo"] == nil {
		return fmt.Errorf("go list: no runtime/cgo")
	}
	s.env = []string{path, "GO111MODULE=off", "CGO_ENABLED=1", "GOFLAGS=-tags=netgo,osusergo", gocache, "GOTOOLCHAIN=local",
		"CGO_CFLAGS=-O0", "GOOS=linux", "GOARCH=amd64", "FC_LINKER=", "FC_LINKFLAGS=-linkmode=external"}
	fmt.Fprintf(os.Stderr, "bzfc: cgo: %d packages (%s) for fc's cgo step, listed in %.0f ms\n", len(roots), strings.Join(names, " "), ms(time.Since(t0)))
	return nil
}

// stockCount: the packages the stock compiler compiled for a build: the go command's cgo closure
// (BZFC_STOCKCGO=1) and what fc had the go command build (its "stock packages:" line).
func stockCount(stock *stockSet, fcOut []byte) int {
	n := 0
	if stock != nil {
		n = stock.nstock
	}
	if m := regexp.MustCompile(`(?m)^stock packages: (\d+) built by the go command`).FindSubmatch(fcOut); m != nil {
		k, _ := strconv.Atoi(string(m[1]))
		n += k
	}
	return n
}
