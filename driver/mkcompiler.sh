#!/usr/bin/env bash
# Build the patched Go compiler the driver uses for pipelining: a copy of GOROOT with
# driver/early_export.py applied to cmd/compile. Output: build/compile-sb
set -e
cd "$(dirname "$0")/.."
src=$(GOTOOLCHAIN=local go env GOROOT)
dst=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
if [ ! -d "$dst" ]; then cp -a "$src" "$dst"; fi
cp "$src/src/cmd/compile/internal/gc/main.go" "$src/src/cmd/compile/internal/gc/obj.go" "$dst/src/cmd/compile/internal/gc/"
python3 driver/early_export.py "$dst/src/cmd/compile/internal/gc"
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build -o "$OLDPWD/build/compile-sb" cmd/compile)
(cd "$dst/src/cmd/compile/internal/gc" && diff -u "$src/src/cmd/compile/internal/gc/main.go" main.go; diff -u "$src/src/cmd/compile/internal/gc/obj.go" obj.go) > driver/early_export.diff || true
echo "built build/compile-sb"
