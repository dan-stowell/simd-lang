#!/bin/bash
# Run from this directory. No GOROOT or project compiler/runtime sources modified.
set -euo pipefail
mkdir -p .build logs
for f in scanner.go source.go tokens.go token_string.go; do
  cmp "syntax/$f" "/usr/local/go/src/cmd/compile/internal/syntax/$f"
done
go version
go test ./...
go build -o .build/scanners .
go build -o .build/golex-original ../../../explore/golex/main.go
../../../build/stage2 < ../../../examples/golex.strand > .build/golex-strand
chmod +x .build/golex-strand
sha256sum .build/scanners .build/golex-original .build/golex-strand
