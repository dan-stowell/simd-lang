You are reviewing the Strand repository (current directory), working toward the goal in CHARTER.md:
cold builds of real projects an order of magnitude or more faster on the critical path. Read CHARTER.md, PLAN.md, STATUS.md,
TRACE.md and the tail of JOURNAL.md. Do not edit code or tests.

Answer briefly and concretely, citing files and numbers:
1. What serializes the current critical path that the agent is not attacking?
2. Which phase's "floor" in TRACE.md is wrong or optimistic, and what is the real limit?
3. What is the strongest reason the current direction is a dead end, and what would you build instead?
4. Which intermediate output is still materialized (written and read back) that a fused
   build would never create, and what does it cost in TRACE.md?
5. Quote any recent JOURNAL.md passage that declines or shrinks work because of time, usage or a
   session ending. Did PLAN.md's oracle number move since the last milestone? What is the largest
   blocker in the campaign that is not being attacked?
Skip praise and skip auditing benchmark claims.
