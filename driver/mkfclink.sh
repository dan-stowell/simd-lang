#!/usr/bin/env bash
# Build fclink (driver/fclink), the campaign's linker, inside the patched GOROOT's cmd tree: it reads objects
# with cmd/internal/goobj.  Output: build/fclink$FC_TAG
# The linker is the library cmd/internal/fclinklib$FC_TAG (driver/fclink/*.go; fc links it too: driver/mkfc.sh),
# the command cmd/fclink$FC_TAG (driver/fclink/cmd).
# FC_TAG gives a builder working in its own git worktree its own directories in the cmd tree and its own binary.
set -e
cd "$(dirname "$0")/.."
dst=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
[ -d "$dst" ] || { echo "run driver/mkcompiler.sh first"; exit 1; }
driver/mkfclinklib.sh
d="$dst/src/cmd/fclink${FC_TAG:-}"
mkdir -p "$d" build
find "$d" -maxdepth 1 -name '*.go' -delete
sed "s#\"cmd/internal/fclinklib\"#\"cmd/internal/fclinklib${FC_TAG:-}\"#" driver/fclink/cmd/main.go > "$d/main.go"
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build -o "$OLDPWD/build/fclink${FC_TAG:-}" "cmd/fclink${FC_TAG:-}")
echo "built build/fclink${FC_TAG:-}"
