#!/usr/bin/env bash
# Build f4link inside the patched GOROOT's cmd tree (it uses cmd/internal/goobj). Output: build/f4link
set -e
cd "$(dirname "$0")/../.."
dst=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
mkdir -p "$dst/src/cmd/sbf4link" && cp explore/f4link/main.go "$dst/src/cmd/sbf4link/"
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build -o "$OLDPWD/build/f4link" cmd/sbf4link)
echo "built build/f4link"
