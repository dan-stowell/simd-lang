# LOOP: how a continuous run works

Read `CHARTER.md` first (the goal and the rules), then `PLAN.md` (the campaign). This file is
the procedure. A run has no deadline: `./overnight.sh` keeps sessions going until a `STOP`
file exists, and starts a fresh session on PLAN.md whenever one ends. A session ending is not
an event. Never decline or shrink a step because of time, usage or the session ending.

## Files

| file | who writes | what |
|---|---|---|
| `CHARTER.md` | user only | goal, rules, corpus |
| `PLAN.md` | user: the campaign; agent: Milestones log and the Now line | the campaign, its oracle, kill and exit tests |
| `STATUS.md` | agent, at most ten lines | the oracle number, what moved, decisions for Dan |
| `JOURNAL.md` | agent, append-only | one line per commit; a paragraph at each milestone |
| `TRACE.md`, `SCOREBOARD.md` | agent, via script | the build trace, and the corpus at 1/2/4/8 cores |
| `MORNING.md` | agent, at milestones | the full report (format below) |
| `IDEAS.md` | agent | ideas, each with a kill test and status |
| `reviews/inbox/` | background reviewers | raw reviews, one file each |
| `REVIEWS.md` | agent | triage of every inbox item: accept / reject (why) / defer |

`NEXT.md` is retired (the state at the end of 2026-11-12, still useful as notes); the Now
line of PLAN.md replaces it.

## Working

Work the campaign continuously. There are no numbered iterations.

- Keep the Now line in PLAN.md exact: the next concrete step, ideally the command to run.
  A fresh session with no memory resumes from it.
- Commit and push every working step, with tests for the code it touches
  (`./test.sh`, plus the differential tests of the phase), and append one line to JOURNAL.md.
- Split the campaign into parts that a stock oracle can check, and hand independent parts to
  subagents in their own git worktrees, so they build in parallel.
- Run long measurements (soaks, corpus builds) as background scripts that report once.
- Triage `reviews/inbox/*` when something is there: one line each in `REVIEWS.md`, then move
  it to `reviews/done/`. Never wait for a reviewer.

## At each milestone (PLAN.md lists them)

1. Measure the oracle number; append the milestone to PLAN.md's log.
2. Regenerate `TRACE.md` and, once it exists, `SCOREBOARD.md`. The trace may be flat or worse
   during a campaign; say so plainly.
3. Write a JOURNAL.md paragraph that ends with: **"What did the oracle number do since the last
   milestone? If it stalled, what is the blocker, and what am I doing about it?"**
4. Fire the background reviewers (below) on the milestone commit.
5. Overwrite `STATUS.md` and `MORNING.md`.

If several hours pass without a milestone, do steps 1, 3 and 5 anyway.

The divergence step (`prompts/divergence.md`) runs only between campaigns, or when the
kill test fires. During a campaign it produces one-hour detours.

## Background reviewers (never block on them)

Run each in the background with a time limit; output goes to `reviews/inbox/`.
Run them from a second clone (`/home/exedev/strand-w` or a `git worktree`) so they don't
fight over the build directory. Reviewers must not edit code.

    # Codex skeptic
    timeout 40m codex exec --model gpt-6-astra -c model_provider=exe-llm \
      -c 'model_providers.exe-llm.name="exe-llm"' \
      -c 'model_providers.exe-llm.base_url="https://llm.int.exe.xyz/v1"' \
      "$(cat prompts/skeptic.md)" > reviews/inbox/$(date +%Y%m%dT%H%M)-codex-skeptic.md 2>&1 &

    # sol, narrow correctness (when a phase was replaced)
    timeout 40m shelley client chat -cwd /home/exedev/strand-w -model gpt-6.1-sol -tag review \
      -p "$(cat prompts/correctness.md)" > reviews/inbox/$(date +%Y%m%dT%H%M)-sol.md 2>&1 &

A stalled or refused review gets one line in `REVIEWS.md` and is dropped. If a provider
flags a prompt, reword it around build speed and correctness; don't retry it unchanged.
Correctness findings can block a merge. A finding that work is being avoided must be
answered with a measurement or a commit, not "defer".

## Usage limits

Your 5-hour model limit is shared with Dan's other VMs. Codex runs on its own budget
(`codex exec`, configured as in the reviewer commands above), so give it the work it does as
well as you:
- writing tests and differential cases for a part you've specified
- running and triaging the oracle, soaks and corpus builds, and reporting failures
- mechanical edits and re-reviews of a diff after a fix

Keep Fable, yourself and Fable subagents, for design and the hard parts of the compiler. A
subagent that only searches or summarizes runs on `model: "sonnet"`.

Within that, don't ration. If the model limit is reached the session goes idle; `overnight.sh`'s watchdog
types a "continue" once it has been quiet for 30 minutes, so work resumes when the limit
resets. Commit often and nothing is lost.

## VM safety

- 31 GiB RAM, no swap, and a runaway child OOM-kills this whole session. Cap fuzzers and
  anything with data-dependent allocation: `prlimit --as=8G`, `timeout`, or
  `systemd-run --user --scope -p MemoryMax=8G`.
- Big target projects: clone them under `/home/exedev/targets/`, not inside this repo.

## Stopping

The Stop hook (`.claude/hooks/keep-going.sh`) lets a session end only when `STOP` exists.
Then: bring STATUS.md and the Now line up to date, commit, push.

`MORNING.md` contains: the trace and scoreboard (with the fair stock row and the
materialization tax), the oracle number and milestones reached, ideas tried and killed,
review disagreements worth the user's attention, every behaviour difference of the dev
build, and decisions that are the user's.
