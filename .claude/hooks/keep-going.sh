#!/usr/bin/env bash
# Stop hook: keep the overnight session working until the deadline or a STOP file.
dir="${CLAUDE_PROJECT_DIR:-$(pwd)}"
cat >/dev/null   # hook input JSON (unused)
[ -f "$dir/STOP" ] && exit 0
deadline=$(cat "$dir/.claude/deadline" 2>/dev/null || echo 0)
now=$(date +%s)
if [ "$now" -ge "$deadline" ]; then
  # Past the deadline: allow one final turn to write MORNING.md, then let it stop.
  if [ ! -f "$dir/.claude/morning-asked" ]; then
    touch "$dir/.claude/morning-asked"
    printf '{"decision":"block","reason":"Deadline reached. Write MORNING.md per LOOP.md, commit and push, then stop."}\n'
  fi
  exit 0
fi
left=$(( (deadline - now) / 60 ))
printf '{"decision":"block","reason":"Not done: %s minutes left before the deadline. Do not ask the user anything; they are asleep. Follow LOOP.md: triage reviews/inbox, read NEXT.md, scope-check, and start the next iteration toward the CHARTER north star."}\n' "$left"
