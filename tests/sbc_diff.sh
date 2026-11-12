#!/usr/bin/env bash
# Differential test of sbc: random programs inside its subset, built by the stock compiler alone and with
# sbc taking the functions, must print the same. Also requires that sbc really took (almost) all of them.
#   tests/sbc_diff.sh [programs=6] [functions per program=150] [first seed=1]
set -e
cd "$(dirname "$0")/.."
n=${1:-6}; nf=${2:-150}; s0=${3:-1}
t=$(mktemp -d /dev/shm/sbcdiff.XXXXXX)
S=$PWD/build
total=0
for seed in $(seq "$s0" $((s0 + n - 1))); do
  d=$t/p$seed; mkdir -p "$d"
  printf 'module sbcdiff\ngo 1.27\n' > "$d/go.mod"
  python3 tests/sbc_gen.py "$seed" "$nf" > "$d/main.go"
  common=(-C "$d" -plan=self -decl "$S/godecl" -pipeline -compiler "$S/compile-sb" -gcflags=-l)
  systemd-run --user --scope -q -p MemoryMax=8G "$S/sb" "${common[@]}" -o "$d/stock.bin" > "$d/stock.log" 2>&1 || { tail -5 "$d/stock.log"; echo "FAIL seed $seed: stock build"; exit 1; }
  systemd-run --user --scope -q -p MemoryMax=8G "$S/sb" "${common[@]}" -sbc "$S/sbc" -sbconly sbcdiff -o "$d/sbc.bin" > "$d/sbc.log" 2>&1 || { tail -5 "$d/sbc.log"; echo "FAIL seed $seed: sbc build"; exit 1; }
  taken=$(grep -o '[0-9]* taken' "$d/sbc.log" | cut -d' ' -f1)
  "$d/stock.bin" > "$d/stock.out" 2>&1 || true
  "$d/sbc.bin" > "$d/sbc.out" 2>&1 || true
  if ! cmp -s "$d/stock.out" "$d/sbc.out"; then
    diff "$d/stock.out" "$d/sbc.out" | head -5; echo "FAIL seed $seed: outputs differ (kept in $d)"; exit 1
  fi
  [ "$taken" -ge $((nf * 9 / 10)) ] || { grep 'not taken' "$d/sbc.log" | head -5; echo "FAIL seed $seed: sbc took only $taken of $nf"; exit 1; }
  total=$((total + taken))
  echo "seed $seed: $taken of $nf functions by sbc, $(wc -l < "$d/stock.out") results equal ($(grep -c panic "$d/stock.out") panics)"
done
rm -rf "$t"
echo "PASS sbc differential: $total functions over $n programs"
