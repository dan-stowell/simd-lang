// escape_dump.go: `fc esc -plan plan.json <import paths... | all>` prints the escape analysis's decisions,
// one per line, in the form of the stock compiler's -m output, for tools/fc/escdiff.sh to compare:
//
//	# import/path
//	file.go:12:6: moved to heap: x [var]
//	file.go:20:13: &literal escapes to heap [site]
//	file.go:21:13: func literal does not escape [site]
//	file.go:30:9: leaking param: p [param]
//	file.go:30:9: leaking param content: p [param]
//	file.go:30:9: leaking param: p to result r level=1 [param]
//	file.go:30:15: q does not escape [param]
//
// A decision inside a generic function is marked [... gen]: the stock compiler reports on instantiations
// only. A heap site that is one for a reason other than the flow of its address says so: (non-constant size).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/token"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func escMode(args []string) {
	fs := flag.NewFlagSet("esc", flag.ExitOnError)
	plan := fs.String("plan", "", "`go list -json` stream of the packages")
	quiet := fs.Bool("q", false, "analyze only, print nothing")
	byval := fs.Bool("byvalue", false, "closures capture by value where the stock compiler's do (see escByValue)")
	fs.Parse(args)
	if *byval {
		escByValue = true
	}
	if *plan == "" || fs.NArg() == 0 {
		log.Fatal("usage: fc esc -plan plan.json <import paths... | all>")
	}
	pr := newProgram()
	if err := pr.readPlan(*plan); err != nil {
		log.Fatal(err)
	}
	var targets []*Pkg
	if fs.NArg() == 1 && fs.Arg(0) == "all" {
		for _, p := range pr.byPath {
			if p.ImportPath != "unsafe" {
				targets = append(targets, p)
			}
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].ImportPath < targets[j].ImportPath })
	} else {
		for _, a := range fs.Args() {
			p := pr.byPath[a]
			if p == nil {
				log.Fatalf("fc esc: %s is not in the plan", a)
			}
			targets = append(targets, p)
		}
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	// the packages are analyzed concurrently, as the compiler will ask for them
	var wg sync.WaitGroup
	for _, p := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if pr.check(p) == nil {
				pr.sem <- struct{}{}
				pr.escape(p)
				<-pr.sem
			}
		}()
	}
	wg.Wait()
	for _, p := range targets {
		if err := pr.check(p); err != nil {
			fmt.Fprintf(w, "# %s\n", p.ImportPath)
			fmt.Fprintf(os.Stderr, "fc esc: %s: %v\n", p.ImportPath, err)
			continue
		}
		info := pr.escape(p)
		if *quiet {
			continue
		}
		fmt.Fprintf(w, "# %s\n", p.ImportPath)
		for _, l := range escLines(pr, info) {
			fmt.Fprintln(w, l)
		}
	}
}

func escLines(pr *Program, info *escInfo) []string {
	type line struct {
		pos  token.Position
		text string
	}
	var lines []line
	add := func(pos token.Pos, text string) {
		lines = append(lines, line{pr.fset.Position(pos), text})
	}
	for _, v := range info.vars {
		tag := " [var"
		if w := strings.TrimSpace(info.varWhy[v]); w != "" {
			tag += " " + w
		}
		pos := v.Pos()
		if p, ok := info.varPos[v]; ok {
			pos = p
		}
		add(pos, "moved to heap: "+v.Name()+tag+"]")
	}
	for _, s := range info.sites {
		text := escSiteNames[s.kind]
		if s.heap {
			text += " escapes to heap"
			if s.why != "" {
				text += " (" + s.why + ")"
			}
		} else {
			text += " does not escape"
		}
		text += " [site"
		if s.gen {
			text += " gen"
		}
		add(s.pos, text+"]")
	}
	for _, p := range info.params {
		tag := " [param"
		if p.gen {
			tag += " gen"
		}
		if p.moved {
			tag += " moved"
		}
		tag += "]"
		name := p.v.Name()
		if !p.leaks.leaks() {
			add(p.v.Pos(), name+" does not escape"+tag)
			continue
		}
		if p.leaks.Heap == 0 {
			add(p.v.Pos(), "leaking param: "+name+tag)
		} else if p.leaks.Heap > 0 {
			add(p.v.Pos(), "leaking param content: "+name+tag)
		}
		for i, d := range p.leaks.Results {
			if d < 0 || p.results == nil || i >= p.results.Len() {
				continue
			}
			rn := p.results.At(i).Name()
			if rn == "" || rn == "_" {
				rn = "~r" + strconv.Itoa(i)
			}
			add(p.v.Pos(), "leaking param: "+name+" to result "+rn+" level="+strconv.Itoa(d)+tag)
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := lines[i].pos, lines[j].pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fmt.Sprintf("%s:%d:%d: %s", l.pos.Filename, l.pos.Line, l.pos.Column, l.text)
	}
	return out
}
