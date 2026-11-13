#!/usr/bin/env bash
# go build/test -toolexec wrapper for fc's mixed mode. A package whose compile flags contain -d=fc=<anything>
# (give it with -gcflags='<pkg>=-l -d=fc=<hash of build/fc>', so the go command recompiles the package when
# fc changes) is compiled by the stock compiler and then by fc, which replaces the object in the archive.
# FC_ONLY: the import paths (space-separated) fc compiles, or '*'. FC_REASONS: a file that collects fc's stderr. FC_BIN: the fc binary. FC_PLAN: `go list -json` of the dependencies. FC_LOG: a line per package.
# FC_STRICT=1: a package fc cannot compile fails the build (otherwise the stock object stays and the log says so).
tool=$1; shift
# FC_TEST_LINKER=<fclink>: the campaign's linker links the binary (it does not check nosplit depth, which an
# fc-built runtime exceeds under the stock bound)
if [ -n "$FC_TEST_LINKER" ] && [ "$(basename "$tool")" = link ] && [ "${1:-}" != "-V=full" ]; then exec "$FC_TEST_LINKER" "$@"; fi
if [ "$(basename "$tool")" != compile ]; then exec "$tool" "$@"; fi
fc=0; args=()
for a in "$@"; do case "$a" in -d=fc=*) fc=1;; *) args+=("$a");; esac; done
pkg=; prev=
for a in "${args[@]}"; do [ "$prev" = -p ] && pkg=$a; prev=$a; done
# the flag also reaches the package's external test package (<pkg>_test): that one stays with the stock compiler
mine=$fc
[ "$FC_ONLY" = '*' ] || case " $FC_ONLY " in *" $pkg "*) ;; *) mine=0;; esac
case "$FC_STOCK" in *" $pkg "*) mine=0;; esac
# (the generated test main and external test packages stay with the stock compiler)
if [ "$FC_ONLY" = '*' ]; then case "$pkg" in main|*_test) mine=0;; esac; fi
runtimepkg=0; for a in "${args[@]}"; do [ "$a" = "-+" ] && runtimepkg=1; done
case "$FC_RTPKGS" in *" $pkg "*) runtimepkg=1;; esac   # (the runtime's packages may not let anything escape)
if [ $mine = 1 ] && [ -n "$FC_ESC_COMPILE" ] && [ $runtimepkg = 0 ]; then
  # the export data importers read says every pointer parameter escapes (tools/fc/mkcompile-esc.sh)
  "$FC_ESC_COMPILE" "${args[@]}" || exit $?
else
  "$tool" "${args[@]}" || exit $?
fi
[ $mine = 1 ] || exit 0
if [ -n "$FC_REASONS" ]; then "$FC_BIN" "${args[@]}" 2>> "$FC_REASONS"; rc=$?; else "$FC_BIN" "${args[@]}"; rc=$?; fi
case $rc in
  0) [ -z "$FC_LOG" ] || echo "$pkg	FC" >> "$FC_LOG"; exit 0;;
  3) [ -z "$FC_LOG" ] || echo "$pkg	STOCK" >> "$FC_LOG"; [ -n "$FC_STRICT" ] && exit 1; exit 0;;
  *) [ -n "$FC_STRICT" ] && exit $rc
     [ -z "$FC_LOG" ] || echo "$pkg	CRASH" >> "$FC_LOG"; exit 0;;
esac
