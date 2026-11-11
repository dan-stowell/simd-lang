// Independent inventory of the reference corpus selection and normalization.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/scanner"
	"go/token"
	"os"
	"unicode/utf8"
)

func main() {
	list, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	lines := bufio.NewScanner(list)
	var retained, excluded []map[string]any
	h := sha256.New()
	total := 0
	appended := 0
	for lines.Scan() {
		name := lines.Text()
		src, err := os.ReadFile(name)
		reasons := []string{}
		if err != nil {
			reasons = append(reasons, err.Error())
		}
		if bytes.IndexByte(src, 0) >= 0 {
			reasons = append(reasons, "NUL")
		}
		if bytes.IndexByte(src, '\r') >= 0 {
			reasons = append(reasons, "CR")
		}
		if bytes.HasPrefix(src, []byte("\xef\xbb\xbf")) {
			reasons = append(reasons, "leading BOM")
		}
		if !utf8.Valid(src) {
			reasons = append(reasons, "invalid UTF-8")
		}
		scanErrors := []string{}
		fs := token.NewFileSet()
		f := fs.AddFile(name, fs.Base(), len(src))
		var s scanner.Scanner
		s.Init(f, src, func(pos token.Position, msg string) {
			scanErrors = append(scanErrors, pos.String()+": "+msg)
		}, 0)
		for {
			_, t, _ := s.Scan()
			if t == token.EOF {
				break
			}
		}
		if len(scanErrors) > 0 {
			reasons = append(reasons, "scanner errors")
		}
		if len(reasons) > 0 {
			excluded = append(excluded, map[string]any{"path": name, "reasons": reasons, "errors": scanErrors})
			continue
		}
		wasAppended := len(src) > 0 && src[len(src)-1] != '\n'
		if wasAppended {
			src = append(src, '\n')
			appended++
		}
		sum := sha256.Sum256(src)
		retained = append(retained, map[string]any{"path": name, "bytes": len(src), "sha256": hex.EncodeToString(sum[:]), "newline_appended": wasAppended})
		h.Write(src)
		total += len(src)
	}
	if err := lines.Err(); err != nil {
		panic(err)
	}
	// Full per-file manifests stay in build/; concise inventory is printable.
	manifest, _ := json.MarshalIndent(retained, "", "  ")
	if err := os.WriteFile(os.Args[2], append(manifest, '\n'), 0644); err != nil {
		panic(err)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{
		"retained_files": len(retained), "excluded_files": excluded,
		"appended_newline_files": appended, "bytes": total,
		"sha256": hex.EncodeToString(h.Sum(nil)),
	})
}
