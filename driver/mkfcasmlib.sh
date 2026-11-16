#!/usr/bin/env bash
# Put the stock assembler's packages (cmd/asm/internal/{arch,asm,flags,lex}) into the patched GOROOT's cmd tree
# as cmd/internal/fcasm/..., where fc can import them (cmd/asm/internal is closed to cmd/fc): fc assembles a
# package's .s files in its own process (driver/fc/asmlib.go) instead of starting `asm` 170 times for MinIO.
# The source is the GOROOT's own, unchanged but for the import paths; nothing of it lives in this repository.
set -e
dst=${SB_GOROOT:-/home/exedev/targets/goroot-fc}
for pkg in arch asm flags lex; do
  d="$dst/src/cmd/internal/fcasm/$pkg"
  mkdir -p "$d"
  find "$d" -maxdepth 1 -name '*.go' -delete
  for f in "$dst/src/cmd/asm/internal/$pkg"/*.go; do
    case "$f" in *_test.go) continue;; esac
    sed 's#"cmd/asm/internal/#"cmd/internal/fcasm/#' "$f" > "$d/$(basename "$f")"
  done
done
