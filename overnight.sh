#!/usr/bin/env bash
# Kick off an overnight run. Usage: ./overnight.sh [hours]   (default 9)
set -euo pipefail
cd "$(dirname "$0")"
hours="${1:-9}"
git pull --ff-only
rm -f STOP .claude/morning-asked
echo $(( $(date +%s) + hours * 3600 )) > .claude/deadline
echo "deadline: $(date -d @"$(cat .claude/deadline)")"
exec claude --dangerously-skip-permissions --remote-control \
  "Overnight run. Read CHARTER.md and LOOP.md, then follow LOOP.md until the Stop hook lets you go. Start from NEXT.md. The user is asleep: don't ask questions, decide and write down why."
