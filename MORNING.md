# MORNING: 2026-11-13 09:20 UTC (campaign 1, between M2 and M4)

## The oracle number

| | now | at M2 (2026-11-12 23:23) |
|---|---|---|
| standard-library packages compiled by fc whose own tests pass | **228** (tests/fc/passing.txt) | 205 |
| language corpus (tests/fc/lang, differential against stock) | **22 of 22** | 21 of 21 |
| MinIO binary, every package above the runtime by fc | **passes its smoke test, cold** | did not run |
| MinIO's own tests, every package above the runtime by fc (`FC_ALL=1`) | **45 of 46 packages pass; cmd fails one test that stock fails too** | not run |
| corpus projects that build, link and pass their check through fc | **7 of 7** (minio, kube-apiserver, prometheus, caddy, tailscaled, runsc, buildifier) | 0 |

## The scoreboard (SCOREBOARD.md, regenerated)

Cold builds on an idle machine at 08:56-09:04 UTC, one run per cell, fc + fclink, binaries checked. 8 cores, seconds:

| project | stock | fair stock | fc | fc core-s |
|---|---|---|---|---|
| minio | 51.3 | 29.5 | **6.8** | 31 |
| kube-apiserver | 74.1 | 52.2 | **7.6** | 40 |
| prometheus | | | **11.1** | 59 |
| caddy | | | **4.6** | 22 |
| tailscaled | | | **4.3** | 18 |
| runsc | | | **2.8** | 10 |
| buildifier | | | **2.4** | 7 |

(Stock columns for the last five are in SCOREBOARD.md.) The exit test is MinIO at 3.3 s. Committed after that
run and not yet in the table: the two-pass front end, worth 0.6-0.9 s of MinIO's warm build (5.5 -> 4.8 s,
three interleaved pairs). The column has no critical-path figure yet, and TRACE.md still describes the
previous campaign's path.

## What is not true yet

- The runtime and what it imports (about 30 packages) are compiled by the stock compiler inside the timer.
  fc compiles them (`FC_RUNTIME=1`), but 6 nosplit call chains exceed the stack bound, mostly through write
  barriers on variables that escape analysis would keep in the frame. That is M3.
- Escape analysis exists (driver/fc/escape*.go, 3,900 lines, oracle `tools/fc/escdiff.sh` against the stock
  compiler's -m) and is wired in but off: with it on, 2 of 22 language-corpus packages fail and the oracle
  shows 4 unsound decisions there. A builder is fixing them.
- The linker is ours but a separate process; fc still writes object files for it to read back.

## Where the 6.8 s go (MinIO, 8 cores, cold)

About 31 core-seconds on 8 cores is 3.9 s if nothing waited; the link is 0.3 s. The rest was the front end's
dependency chain (now shortened by the two-pass change) and the back end not filling the cores early.
CPU by part, from a profile: code generation about 10 core-s, type checking 9, type descriptors 3.4, object
writing 2.7, parsing 2.

## Found on the way

- The default Go build cache gave a broken stock test binary (cleared). Stock comparisons use their own cache.
- Codex's second run was cut short by its provider flagging a prompt; its report was still usable
  (reviews/done/fc-grind-1.md, REVIEWS.md).
