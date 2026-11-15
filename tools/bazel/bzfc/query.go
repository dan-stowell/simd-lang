package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
)

var (
	flagQuery = flag.String("query", "", "bazel query --output=streamed_proto 'deps(LABEL)' output (instead of -aquery)")
	flagQDump = flag.String("qdump", "", "with -query: only print the rules whose label or class matches this regexp, with every attribute")
)

var qTargets []*qTarget

func qdump() {
	re := regexp.MustCompile(*flagQDump)
	kinds := map[string]int{}
	for _, t := range qTargets {
		if t.Rule == nil {
			continue
		}
		kinds[t.Rule.Class]++
		if !re.MatchString(t.Rule.Name) && !re.MatchString(t.Rule.Class) {
			continue
		}
		fmt.Printf("== %s %s\n", t.Rule.Class, t.Rule.Name)
		var names []string
		for n := range t.Rule.Attrs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			a := t.Rule.Attrs[n]
			b, _ := json.Marshal(a)
			fmt.Printf("  %s\n", b)
		}
	}
	var ks []string
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("kind %s %d\n", k, kinds[k])
	}
}

func readQuery() { log.Fatal("todo") }

func loadQuery() {
	f, err := os.Open(*flagQuery)
	if err != nil {
		log.Fatal(err)
	}
	qTargets, err = readQueryStream(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
}
