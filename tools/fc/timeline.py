#!/usr/bin/env python3
"""Cores busy over time for one command (the process itself, user + kernel, sampled from /proc).
   tools/fc/timeline.py [-b ms] <command...>     one line per bin: start ms, cores busy (user, kernel), RSS GB
The question it answers: where in the build are cores idle (the critical path) and where is kernel time."""
import os, subprocess, sys, time
bin_ms = 250
a = sys.argv[1:]
if a and a[0] == "-b":
    bin_ms = int(a[1]); a = a[2:]
tick = os.sysconf("SC_CLK_TCK"); page = os.sysconf("SC_PAGE_SIZE")
p = subprocess.Popen(a, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
t0 = time.time(); rows = []
def sample():
    try:
        f = open(f"/proc/{p.pid}/stat").read().rsplit(") ", 1)[1].split()
        return time.time() - t0, int(f[11]) / tick, int(f[12]) / tick, int(f[21]) * page / 2**30
    except Exception:
        return None
prev = sample()
while p.poll() is None:
    time.sleep(bin_ms / 1000)
    cur = sample()
    if cur is None or prev is None:
        break
    dt = cur[0] - prev[0]
    rows.append((prev[0] * 1000, (cur[1] - prev[1]) / dt, (cur[2] - prev[2]) / dt, cur[3]))
    prev = cur
wall = time.time() - t0
for ms, u, k, rss in rows:
    print(f"{ms:6.0f} ms  user {u:4.1f}  kernel {k:4.1f}  {'#' * round(u + k)}{'':{9 - round(u + k)}} rss {rss:4.1f} GB")
print(f"wall {wall * 1000:.0f} ms (exit included), rc {p.returncode}")
