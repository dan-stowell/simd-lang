#!/usr/bin/env bash
# Build the patched Go compiler the driver uses for pipelining: a copy of GOROOT with
# driver/early_export.py applied to cmd/compile. Output: build/compile-sb
set -e
cd "$(dirname "$0")/.."
src=$(GOTOOLCHAIN=local go env GOROOT)
dst=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
if [ ! -d "$dst" ]; then cp -a "$src" "$dst"; fi
cp "$src/src/cmd/compile/internal/gc/main.go" "$src/src/cmd/compile/internal/gc/obj.go" "$dst/src/cmd/compile/internal/gc/"
cp "$src/src/cmd/internal/obj/objfile.go" "$dst/src/cmd/internal/obj/"
cp "$src/src/cmd/compile/internal/ssagen/pgen.go" "$dst/src/cmd/compile/internal/ssagen/"
cp driver/baseline/sbbaseline.go "$dst/src/cmd/compile/internal/ssagen/"
cp driver/baseline/sbshard.go "$dst/src/cmd/compile/internal/noder/"
cp "$src/src/cmd/compile/internal/noder/unified.go" "$dst/src/cmd/compile/internal/noder/"
cp "$src/src/cmd/compile/internal/noder/writer.go" "$dst/src/cmd/compile/internal/noder/"
cp "$src/src/cmd/compile/internal/noder/irgen.go" "$dst/src/cmd/compile/internal/noder/"
cp "$src/src/runtime/mheap.go" "$dst/src/runtime/"
cp "$src/src/cmd/compile/internal/reflectdata/reflect.go" "$dst/src/cmd/compile/internal/reflectdata/"
cp "$src/src/cmd/compile/internal/base/timings.go" "$dst/src/cmd/compile/internal/base/"
cp "$src/src/cmd/internal/obj/x86/asm6.go" "$dst/src/cmd/internal/obj/x86/"
cp "$src/src/cmd/internal/obj/dwarf.go" "$dst/src/cmd/internal/obj/"
cp "$src/src/cmd/compile/internal/types/type.go" "$dst/src/cmd/compile/internal/types/"
cp driver/x86/sbspan.go "$dst/src/cmd/internal/obj/x86/"
cp driver/runtime/sbfork.go "$dst/src/runtime/"
cp "$src/src/cmd/compile/main.go" "$dst/src/cmd/compile/main.go"
cp driver/zygote/sbzygote.go "$dst/src/cmd/compile/"
sed -i 's|^\tgc.Main(archInit)|\tsbZygote()\n\tgc.Main(archInit)|' "$dst/src/cmd/compile/main.go"
python3 driver/early_export.py "$dst/src/cmd/compile/internal/gc" "$dst/src/cmd/internal/obj"
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build -o "$OLDPWD/build/compile-sb" cmd/compile)
# the stock linker, linked with the patched runtime (SB_HUGE, SB_POPULATE)
cp "$src/src/cmd/link/internal/ld/lib.go" "$dst/src/cmd/link/internal/ld/"
rm -f "$dst/src/cmd/link/internal/ld/sbstream.go"
(cd "$dst/src" && GOROOT="$dst" GOTOOLCHAIN=local GOFLAGS= "$dst/bin/go" build -o "$OLDPWD/build/link-sb" cmd/link)
(cd "$dst/src/cmd/compile/internal/gc" && diff -u "$src/src/cmd/compile/internal/gc/main.go" main.go; diff -u "$src/src/cmd/compile/internal/gc/obj.go" obj.go; diff -u "$src/src/cmd/internal/obj/objfile.go" ../../../internal/obj/objfile.go; diff -u "$src/src/cmd/compile/internal/ssagen/pgen.go" ../ssagen/pgen.go) > driver/early_export.diff || true
echo "built build/compile-sb"
