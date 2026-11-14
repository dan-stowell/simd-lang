#!/usr/bin/env bash
# Build fc (driver/fc), the campaign's compiler, inside the patched GOROOT's cmd tree: it writes objects through
# cmd/internal/obj's object-file writer (not its assembler).  Output: build/fc$FC_TAG
# FC_TAG gives a builder working in its own git worktree its own directory in the cmd tree and its own binary.
set -e
cd "$(dirname "$0")/.."
# fc has a GOROOT of its own, a copy of the patched one: its go/ast and go/types carry fields and hooks
# (driver/gotypes: sbbodies.go, sbslots.go) that the other tools built in goroot-sb do not want to depend on.
base=/home/exedev/targets/goroot-sb
dst=${SB_GOROOT:-/home/exedev/targets/goroot-slots}
[ -d "$base" ] || { echo "run driver/mkcompiler.sh first"; exit 1; }
[ -d "$dst" ] || cp -a "$base" "$dst"
python3 driver/gotypes/patch.py "$(GOTOOLCHAIN=local go env GOROOT)/src/go/types" "$dst/src/go/types"   # CheckDecls + Bodies (sbbodies.go)
d="$dst/src/cmd/fc${FC_TAG:-}"
mkdir -p "$d" build
find "$d" -maxdepth 1 -name '*.go' -delete
cp driver/fc/*.go "$d/"
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build ${FC_RACE:+-race} -o "$OLDPWD/build/fc${FC_TAG:-}" "cmd/fc${FC_TAG:-}")
echo "built build/fc${FC_TAG:-}"
