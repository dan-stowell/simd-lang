// aqread.go: reading the aquery output one array element at a time. json.Decoder.Decode of the whole object
// first buffers all of it (1.5 GB for the BuildBuddy executor) and then holds every action's command line;
// here each element is decoded alone, and the command lines of actions bzfc never runs are dropped as they
// are read (rules_go's nogo actions: about a third of the bytes; GoCompilePkg's are parsed into a.args).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// never run, never read beyond their outputs: their command lines are not kept
var dropArgs = map[string]bool{"RunNogo": true, "ValidateNogo": true, "GoStdlib": true, "GoStdlibList": true, "GoCover": true}

func readAquery(r io.Reader) error {
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<20))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return fmt.Errorf("aquery: not a JSON object (%v)", err)
	}
	keep := *flagDump == "" && !*flagStats
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		switch t {
		case "artifacts":
			err = decodeArray(dec, &aq.Artifacts, nil)
		case "actions":
			err = decodeArray(dec, &aq.Actions, func(a *action) {
				if !keep {
					return
				}
				if dropArgs[a.Mnemonic] {
					a.Arguments, a.ParamFiles = nil, nil
				} else if a.Mnemonic == "GoCompilePkg" {
					a.args = parseArgs(a.Arguments)
					a.Arguments, a.ParamFiles = nil, nil
				}
			})
		case "depSetOfFiles":
			err = decodeArray(dec, &aq.DepSets, nil)
		case "targets":
			err = decodeArray(dec, &aq.Targets, nil)
		case "configuration":
			err = decodeArray(dec, &aq.Configuration, nil)
		case "pathFragments":
			err = decodeArray(dec, &aq.PathFragments, nil)
		default:
			var skip json.RawMessage
			err = dec.Decode(&skip)
		}
		if err != nil {
			return fmt.Errorf("aquery %v: %v", t, err)
		}
	}
	return nil
}

func decodeArray[T any](dec *json.Decoder, out *[]T, each func(T)) error {
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return fmt.Errorf("not an array (%v)", err)
	}
	for dec.More() {
		var x T
		if err := dec.Decode(&x); err != nil {
			return err
		}
		if each != nil {
			each(x)
		}
		*out = append(*out, x)
	}
	_, err := dec.Token() // ]
	return err
}
