#!/usr/bin/env bash
# Stop hook: keep the session on the campaign until a STOP file exists. There is no deadline.
dir="${CLAUDE_PROJECT_DIR:-$(pwd)}"
cat >/dev/null   # hook input JSON (unused)
if [ -f "$dir/STOP" ]; then
  # Allow one last turn to leave STATUS.md current, then let it stop.
  if [ ! -f "$dir/.claude/status-asked" ]; then
    touch "$dir/.claude/status-asked"
    printf '{"decision":"block","reason":"STOP requested. Bring STATUS.md and the Now line of PLAN.md up to date, commit and push, then stop."}\n'
  fi
  exit 0
fi
now=$(grep -m1 '^Now:' "$dir/PLAN.md" 2>/dev/null | cut -c6- | tr -d '"\\' | cut -c1-400)
printf '{"decision":"block","reason":"Keep going. There is no deadline and no session end to plan around. Do not ask the user anything. Continue the campaign in PLAN.md (LOOP.md is the procedure). Now: %s"}\n' "$now"
