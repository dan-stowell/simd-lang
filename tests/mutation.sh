#!/bin/sh
# Does the suite catch bugs? Inject five into the stencil generator, one at a time, and require
# the semantic fuzzer to report a MISMATCH for each. Restores the generator afterwards.
cd "$(dirname "$0")/.."
cp tools/mkstencils.py build/mk.bak
fail=0
try() {
  python3 tools/mkstencils.py >/dev/null
  if timeout 300 python3 tests/fuzz_sem.py --n 150 --seed 77 2>&1 | grep -q '^MISMATCH'; then echo "caught:  $1"; else echo "MISSED:  $1"; fail=1; fi
  cp build/mk.bak tools/mkstencils.py
}
sed -i "s/CMP = {'<': 1,/CMP = {'<': 2,/" tools/mkstencils.py; try "'<' compiled as '<='"
python3 - <<'PY'
s=open('tools/mkstencils.py').read(); s=s.replace("  cmp rsi,[rbx-40]\n  jb 4f\n","  test rsi,rsi\n  jz 4f\n"); open('tools/mkstencils.py','w').write(s)
PY
try "release ignores the static-data check"
python3 - <<'PY'
s=open('tools/mkstencils.py').read(); s=s.replace("valignd zmm24,zmm{r},zmm{16+r},15\\n  vmovdqa32 zmm{16+r},zmm{r}\\n","valignd zmm24,zmm{r},zmm{16+r},15\\n",1); open('tools/mkstencils.py','w').write(s)
PY
try "prev forgets its carry across chunks"
sed -i 's/for sh in (15, 14, 12, 8):/for sh in (15, 14, 12, 9):/' tools/mkstencils.py; try "scan loses a lane (shift 8 -> 9)"
python3 - <<'PY'
s=open('tools/mkstencils.py').read(); s=s.replace('''  lea rcx,[rbx+{DISP}]""" + RELEASE + """
  mov [rcx],rdi
  mov [rcx+8],r12""")''','''  lea rcx,[rbx+{DISP}]
  mov [rcx],rdi
  mov [rcx+8],r12""" + RELEASE)''',1); open('tools/mkstencils.py','w').write(s)
PY
try "store sink frees the block it just wrote"
python3 tools/mkstencils.py >/dev/null
exit $fail
