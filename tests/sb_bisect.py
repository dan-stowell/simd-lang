#!/usr/bin/env python3
"""Find which baseline-compiled functions break a standard-library package's tests.
   tests/sb_bisect.py <package> [go test args...]
Halves the set of functions the baseline may take (by a hash of the symbol name: SB_BASELINE_HASH=k/n)
until the failing set is small, then lists it."""
import os, subprocess, sys, tempfile, glob, shutil
G = os.environ.get('SB_GOROOT', '/home/exedev/targets/goroot-sb')
here = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
shutil.copy(os.path.join(here, 'build/compile-sb'), os.path.join(G, 'pkg/tool/linux_amd64/compile'))
pkg, extra = sys.argv[1], sys.argv[2:]
def run(hashspec):
    log = tempfile.mkdtemp(prefix='sbbis', dir='/dev/shm'); cache = tempfile.mkdtemp(prefix='sbbisc', dir='/dev/shm')
    env = dict(os.environ, GOROOT=G, GOTOOLCHAIN='local', GOFLAGS='', GOCACHE=cache, SB_BASELINE='1', SB_BASELINE_LOG=log)
    if hashspec: env['SB_BASELINE_HASH'] = hashspec
    p = subprocess.run(['systemd-run', '--user', '--scope', '-q', '-p', 'MemoryMax=10G', os.path.join(G, 'bin/go'), 'test', '-short', '-count=1',
                        '-gcflags=all=-dwarf=false', '-ldflags=-w', pkg] + extra, cwd=os.path.join(G, 'src'), env=env, capture_output=True, text=True, timeout=900)
    taken = []
    for f in glob.glob(log + '/[0-9]*.txt'):
        for line in open(f):
            a = line.rstrip('\n').split('\t')
            if len(a) >= 2 and a[1] == 'baseline': taken.append(a[0])
    shutil.rmtree(log); shutil.rmtree(cache)
    return p.returncode == 0, taken, p.stdout + p.stderr
ok, taken, out = run('')
print(f'all {len(taken)} baseline functions: {"pass" if ok else "FAIL"}')
if ok: sys.exit(0)
k, n = 0, 1
last_out = out
while True:
    found = False
    for kk in (k, k + n):
        ok, taken, out = run(f'{kk}/{2 * n}')
        print(f'  hash {kk}/{2 * n}: {len(taken)} functions: {"pass" if ok else "FAIL"}', flush=True)
        if not ok:
            k, n, found, last_out, last_taken = kk, 2 * n, True, out, taken
            break
    if not found:
        print('neither half fails alone: the failure needs functions from both'); break
    if len(last_taken) <= 3: break
print('failing set:'); [print('   ', t) for t in sorted(last_taken)[:40]]
print(last_out[-1500:])
