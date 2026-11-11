# LOOP: how an overnight session runs

Read `CHARTER.md` first. This file is the procedure; the charter is the goal.

## Files

| file | who writes | what |
|---|---|---|
| `CHARTER.md` | user only | goal, ladder, rules |
| `TRACE.md` | agent, via script | the build trace: the one number |
| `JOURNAL.md` | agent, append-only | timestamped: what I did, what moved, scope check |
| `NEXT.md` | agent, overwritten | the next 1-3 steps, so a fresh context can resume |
| `IDEAS.md` | agent + subagents | weird ideas, each with a one-line kill test and status |
| `reviews/inbox/` | background reviewers | raw reviews, one file each |
| `REVIEWS.md` | agent | triage of every inbox item: accept / reject (why) / defer |
| `MORNING.md` | agent, overwritten at end | the report for the user |

## Each iteration

1. Read `CHARTER.md`, the tail of `JOURNAL.md`, `NEXT.md`, `TRACE.md`.
2. **Triage** `reviews/inbox/*`: one line each in `REVIEWS.md`, then move the file to
   `reviews/done/`. Accepted items go into `NEXT.md`. Never wait for a reviewer that hasn't finished.
3. Every 5th iteration (count journal entries): **divergence step**. Run
   `prompts/divergence.md` with 3-4 subagents that see only the charter and the trace, not
   the design docs. Append their ideas to `IDEAS.md`.
4. **Pick:** the next rung (default), an idea spike (about 1 in 5, in a git worktree, time-boxed
   to one iteration), or critical-path optimization (budgeted).
5. **Scope check** into `JOURNAL.md`.
6. Build, test (`./test.sh` plus differential tests for the touched phase), regenerate
   `TRACE.md` if a phase changed, commit, push.
7. **Fire background reviews** on the new commit (see below). Don't wait.
8. Overwrite `NEXT.md`.

## Background reviewers (never block on them)

Run each in the background with a time limit; output goes to `reviews/inbox/`.
Run them from a second clone (`/home/exedev/strand-w` or a `git worktree`) so they don't
fight over the build directory. Reviewers must not edit code.

    # Codex skeptic
    timeout 40m codex exec --model gpt-6-astra -c model_provider=exe-llm \
      -c 'model_providers.exe-llm.name="exe-llm"' \
      -c 'model_providers.exe-llm.base_url="https://llm.int.exe.xyz/v1"' \
      "$(cat prompts/skeptic.md)" > reviews/inbox/$(date +%Y%m%dT%H%M)-codex-skeptic.md 2>&1 &

    # sol, narrow correctness (only when a phase was replaced)
    timeout 40m shelley client chat -cwd /home/exedev/strand-w -model gpt-6.1-sol -tag review \
      -p "$(cat prompts/correctness.md)" > reviews/inbox/$(date +%Y%m%dT%H%M)-sol.md 2>&1 &

A stalled or refused review gets one line in `REVIEWS.md` and is dropped. If a provider
flags a prompt, reword it around build speed and correctness; don't retry it unchanged.
Correctness findings can block a merge; opinions never do.

## VM safety

- 31 GiB RAM, no swap, and a runaway child OOM-kills this whole session. Cap fuzzers and
  anything with data-dependent allocation: `prlimit --as=8G`, `timeout`, or
  `systemd-run --user --scope -p MemoryMax=8G`.
- Big target projects: clone them under `/home/exedev/targets/`, not inside this repo.

## Stopping

The Stop hook (`.claude/hooks/keep-going.sh`) refuses to let the session end until the
deadline in `.claude/deadline` (epoch seconds) passes or a file `STOP` exists. When it
lets you go: write `MORNING.md` (trace before and after, rungs climbed, ideas tried,
review disagreements worth the user's attention), commit, push.
