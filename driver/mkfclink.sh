#!/usr/bin/env bash
# Build fclink (driver/fclink), the campaign's linker, inside the patched GOROOT's cmd tree: it reads objects
# with cmd/internal/goobj.  Output: build/fclink$FC_TAG
# FC_TAG gives a builder working in its own git worktree its own directory in the cmd tree and its own binary.
set -e
cd "$(dirname "$0")/.."
dst=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
[ -d "$dst" ] || { echo "run driver/mkcompiler.sh first"; exit 1; }
d="$dst/src/cmd/fclink${FC_TAG:-}"
mkdir -p "$d" build
find "$d" -maxdepth 1 -name '*.go' -delete
cp driver/fclink/*.go "$d/"
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build -o "$OLDPWD/build/fclink${FC_TAG:-}" "cmd/fclink${FC_TAG:-}")
echo "built build/fclink${FC_TAG:-}"
