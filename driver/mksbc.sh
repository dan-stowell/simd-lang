#!/usr/bin/env bash
# Build sbc (driver/sbc) inside the patched GOROOT's cmd tree: it uses cmd/internal/obj.  Output: build/sbc
set -e
cd "$(dirname "$0")/.."
dst=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
[ -d "$dst" ] || { echo "run driver/mkcompiler.sh first"; exit 1; }
mkdir -p "$dst/src/cmd/sbc"
find "$dst/src/cmd/sbc" -maxdepth 1 -name '*.go' -delete
cp driver/sbc/*.go "$dst/src/cmd/sbc/"
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build -o "$OLDPWD/build/sbc" cmd/sbc)
echo "built build/sbc"
