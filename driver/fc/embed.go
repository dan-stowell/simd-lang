// embed.go: //go:embed variables, written into the data as the stock compiler writes them
// (cmd/compile/internal/staticdata/embed.go).
package main

import (
	"encoding/json"
	"go/ast"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"cmd/internal/hash"
	"cmd/internal/obj"
	"cmd/internal/objabi"
)

// embedCfg is what the go command passes with -embedcfg; without one (whole-program mode) the patterns are
// matched here against the package's EmbedFiles.
type embedCfg struct {
	Patterns map[string][]string
	Files    map[string]string
}

func readEmbedCfg(file string) *embedCfg {
	cfg := &embedCfg{}
	data, err := os.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(data, cfg)
	}
	if err != nil {
		giveUp("embedcfg: %v", err)
	}
	return cfg
}

// embedPatterns: the patterns of the //go:embed lines in a comment group.
func embedPatterns(docs ...*ast.CommentGroup) []string {
	var out []string
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		for _, cm := range doc.List {
			rest, ok := strings.CutPrefix(cm.Text, "//go:embed ")
			if !ok {
				continue
			}
			for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
				if rest[0] == '"' || rest[0] == '`' {
					end := strings.IndexByte(rest[1:], rest[0])
					if end < 0 {
						giveUp("go:embed: unterminated quote")
					}
					out = append(out, rest[1:1+end])
					rest = rest[end+2:]
					continue
				}
				f, r, _ := strings.Cut(rest, " ")
				out = append(out, f)
				rest = r
			}
		}
	}
	return out
}

// embedMatch: the files of the package that a pattern selects.
func (c *compiler) embedMatch(pattern string) []string {
	if c.embed != nil && c.embed.Patterns != nil {
		return c.embed.Patterns[pattern]
	}
	glob, all := strings.CutPrefix(pattern, "all:")
	var out []string
	c.embedOnce.Do(func() {
		if c.pkg.EmbedFiles != nil {
			return
		}
		// the plan does not list the files (the driver's own planner): everything below the package's
		// directory is a candidate, nested modules excepted
		filepath.WalkDir(c.pkg.Dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != c.pkg.Dir {
					if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if rel, err := filepath.Rel(c.pkg.Dir, path); err == nil && d.Type().IsRegular() {
				c.pkg.EmbedFiles = append(c.pkg.EmbedFiles, filepath.ToSlash(rel))
			}
			return nil
		})
	})
	for _, f := range c.pkg.EmbedFiles {
		if ok, _ := path.Match(glob, f); ok {
			out = append(out, f)
			continue
		}
		// a directory the pattern names: everything below it, hidden files only with all:
		elems := strings.Split(f, "/")
		for i := 1; i < len(elems); i++ {
			if ok, _ := path.Match(glob, strings.Join(elems[:i], "/")); !ok {
				continue
			}
			hidden := false
			for _, e := range elems[i:] {
				if strings.HasPrefix(e, ".") || strings.HasPrefix(e, "_") {
					hidden = true
				}
			}
			if all || !hidden {
				out = append(out, f)
			}
			break
		}
	}
	return out
}

func (c *compiler) embedFile(name string) string {
	if c.embed != nil && c.embed.Files != nil {
		return c.embed.Files[name]
	}
	return filepath.Join(c.pkg.Dir, name)
}

func embedSplit(name string) (dir, elem string) {
	name = strings.TrimSuffix(name, "/")
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return ".", name
	}
	return name[:i], name[i+1:]
}

// embedVar writes the value of a //go:embed variable.
func (c *compiler) embedVar(v *types.Var, patterns []string) {
	isFS := false
	if n, ok := types.Unalias(v.Type()).(*types.Named); ok && n.Obj().Name() == "FS" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "embed" {
		isFS = true
	}
	have := map[string]bool{}
	var list []string
	for _, p := range patterns {
		for _, f := range c.embedMatch(p) {
			if !have[f] {
				have[f] = true
				list = append(list, f)
			}
			if isFS {
				for dir := path.Dir(f); dir != "." && !have[dir]; dir = path.Dir(dir) {
					have[dir] = true
					list = append(list, dir+"/")
				}
			}
		}
	}
	sort.Slice(list, func(i, j int) bool {
		xd, xe := embedSplit(list[i])
		yd, ye := embedSplit(list[j])
		return xd < yd || xd == yd && xe < ye
	})
	read := func(name string) []byte {
		data, err := os.ReadFile(c.embedFile(name))
		if err != nil {
			giveUp("go:embed %s: %v", name, err)
		}
		return data
	}
	sym := c.globalSym(v)
	if !isFS {
		if len(list) != 1 {
			giveUp("go:embed: %d files for a variable of type %v", len(list), v.Type())
		}
		data := read(list[0])
		if isString(v.Type()) {
			if len(data) > 0 {
				sym.WriteAddr(c.ctxt, 0, 8, c.stringSym(string(data)), 0)
				sym.WriteInt(c.ctxt, 8, 8, int64(len(data)))
			}
			return
		}
		ds := c.staticSym("embed")
		ds.P = data
		c.defData(ds, objabi.SNOPTRDATA, int64(len(data)))
		sym.WriteAddr(c.ctxt, 0, 8, ds, 0)
		sym.WriteInt(c.ctxt, 8, 8, int64(len(data)))
		sym.WriteInt(c.ctxt, 16, 8, int64(len(data)))
		return
	}
	// embed.FS{files: &[]file{...}}; file is {name string; data string; hash [16]byte}
	sd := c.lookup(sym.Name+".files", obj.ABI0, sym.Pkg)
	off := int64(0)
	sd.WriteAddr(c.ctxt, off, 8, sd, 24)
	sd.WriteInt(c.ctxt, off+8, 8, int64(len(list)))
	sd.WriteInt(c.ctxt, off+16, 8, int64(len(list)))
	off += 24
	for _, f := range list {
		sd.WriteAddr(c.ctxt, off, 8, c.stringSym(f), 0)
		sd.WriteInt(c.ctxt, off+8, 8, int64(len(f)))
		if !strings.HasSuffix(f, "/") {
			data := read(f)
			if len(data) > 0 {
				sd.WriteAddr(c.ctxt, off+16, 8, c.stringSym(string(data)), 0)
			}
			sd.WriteInt(c.ctxt, off+24, 8, int64(len(data)))
			sum := hash.Sum32(data)
			sd.WriteBytes(c.ctxt, off+32, sum[:16])
		}
		off += 48
	}
	sd.Set(obj.AttrLocal, true)
	c.defData(sd, objabi.SRODATA, off)
	sym.WriteAddr(c.ctxt, 0, 8, sd, 0)
}
