# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)

Campaign 1, M2 reached 2026-11-12 23:23 UTC. fc (driver/fc) compiles 205 standard-library packages whose own tests pass (mixed mode), and tests/fc/lang 21 of 21.
MinIO: builds and links in whole-program mode (1,283 packages by fc, 5.4-6.3 s wall, 26-27 core-s, runtime's 30 packages prebuilt); the binary does not run yet.
Kill test: fc costs 0.45 of the stock baseline mode's core-seconds on the same 205 packages (threshold 0.33). Continuing: the path is measured (JOURNAL M2).
M0 done: SCOREBOARD.md. TRACE.md unchanged (frozen stock path); the fc numbers above are not cold builds yet.

## Decisions for Dan

1. Tests that assert an optimization (zero allocations that need escape analysis) are skipped and listed in tests/fc/skips.txt. Say so if they should block a package.
2. Mixed-mode tests give the package under test export data in which every pointer parameter escapes (stock callers otherwise stack-allocate arguments fc may move to the heap). The other packages stay stock-compiled; say so if that weakens the oracle too much.
3. The kill test is above its threshold (0.45 vs 0.33) and I continued on a measured path, as PLAN.md allows. Stop me if 0.45 at M2 should have ended the campaign.
