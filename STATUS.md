# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)

Campaign 1, M1 reached 2026-11-12 19:57 UTC: fc (driver/fc), our own compiler, compiles `unicode/utf8` whole; its tests pass.
Oracle: 1 standard-library package (of ~190 with tests); corpus 0. Core-seconds per MB: measured at M2.
M0 scoreboard run in progress in the background (SCOREBOARD.md when it ends). Trace unchanged (frozen path).

## Decisions for Dan

1. Tests that assert an optimization (zero allocations that need escape analysis) are skipped and listed in
   tests/fc/skips.txt, like the charter's finalizer tests. Say so if they should instead block a package.
