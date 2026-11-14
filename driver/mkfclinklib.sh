#!/usr/bin/env bash
# Put fclink's library (driver/fclink/*.go) into the patched GOROOT's cmd tree as cmd/internal/fclinklib$FC_TAG,
# for the fclink command (driver/mkfclink.sh) and for fc (driver/mkfc.sh), which links in process.
set -e
cd "$(dirname "$0")/.."
dst=${SB_GOROOT:-/home/exedev/targets/goroot-sb}
d="$dst/src/cmd/internal/fclinklib${FC_TAG:-}"
mkdir -p "$d"
find "$d" -maxdepth 1 -name '*.go' -delete
cp driver/fclink/*.go "$d/"
