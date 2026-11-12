#!/usr/bin/env bash
set -uo pipefail
g=${1:?usage: stdlib.sh private-GOROOT}
out=${2:-/tmp/strand-ptr-stdlib}
mkdir -p "$out"
failed=0
for mode in stock keep skip; do
 root=$g; flags=(); case $mode in stock) root=/usr/local/go;;keep) flags=(SB_BASELINE=1 SB_KEEP_WEAKPTR=1);;skip) flags=(SB_BASELINE=1);;esac
 env -u SB_BASELINE -u SB_KEEP_WEAKPTR GOROOT=$root GOTOOLCHAIN=local GOFLAGS= GOCACHE="$out/cache-$mode" "${flags[@]}" "$root/bin/go" test -vet=off '-gcflags=all=-dwarf=false -l' -count=1 -timeout=120s -run '^Test(PtrTo|PtrToMethods|PtrPointTo|TypeFor|EmbeddedMethods|UnexportedMethods|NestedMethods|Method|MethodValue|StructOf|StructOfWithInterface|StructOfEmbeddedIfaceMethodCall|FuncOf|ChanOf|ChanOfDir|MapOf|ArrayOf|ArrayOfDirectIface|ArrayOfGenericAlg|StructOfDifferentPkgPath|StructOfAnonymous)$' reflect > "$out/reflect-$mode.log" 2>&1
 status=$?; echo "$mode reflect status=$status"; if ((status)); then failed=1; fi; cat "$out/reflect-$mode.log"
 env -u SB_BASELINE -u SB_KEEP_WEAKPTR GOROOT=$root GOTOOLCHAIN=local GOFLAGS= GOCACHE="$out/cache-$mode" "${flags[@]}" "$root/bin/go" test -vet=off '-gcflags=all=-dwarf=false -l' -count=1 -timeout=120s internal/reflectlite internal/abi > "$out/internal-$mode.log" 2>&1
 status=$?; echo "$mode internals status=$status"; if ((status)); then failed=1; fi; cat "$out/internal-$mode.log"
done
exit "$failed"
