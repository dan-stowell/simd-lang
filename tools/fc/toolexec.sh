#!/usr/bin/env bash
# go build/test -toolexec wrapper for fc's mixed mode. A package whose compile flags contain -d=fc=<anything>
# (give it with -gcflags='<pkg>=-l -d=fc=<hash of build/fc>', so the go command recompiles the package when
# fc changes) is compiled by the stock compiler and then by fc, which replaces the object in the archive.
# FC_ONLY: the import paths (space-separated) fc compiles. FC_BIN: the fc binary. FC_PLAN: `go list -json` of the dependencies. FC_LOG: a line per package.
# FC_STRICT=1: a package fc cannot compile fails the build (otherwise the stock object stays and the log says so).
tool=$1; shift
if [ "$(basename "$tool")" != compile ]; then exec "$tool" "$@"; fi
fc=0; args=()
for a in "$@"; do case "$a" in -d=fc=*) fc=1;; *) args+=("$a");; esac; done
"$tool" "${args[@]}" || exit $?
[ $fc = 1 ] || exit 0
pkg=; prev=
for a in "${args[@]}"; do [ "$prev" = -p ] && pkg=$a; prev=$a; done
# the flag also reaches the package's external test package (<pkg>_test): that one stays with the stock compiler
case " $FC_ONLY " in *" $pkg "*) ;; *) exit 0;; esac
"$FC_BIN" "${args[@]}"; rc=$?
case $rc in
  0) [ -z "$FC_LOG" ] || echo "$pkg	FC" >> "$FC_LOG"; exit 0;;
  3) [ -z "$FC_LOG" ] || echo "$pkg	STOCK" >> "$FC_LOG"; [ -n "$FC_STRICT" ] && exit 1; exit 0;;
  *) exit $rc;;
esac
