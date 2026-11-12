#!/usr/bin/env bash
# Start a continuous run on the campaign in PLAN.md. Usage: ./overnight.sh   (run it inside tmux)
# It runs until a file STOP exists in this directory: `touch STOP`. There is no deadline.
#
# If claude exits, a fresh session starts on PLAN.md. If the session stops working
# (usually the model usage limit: claude stays open but idle), a watchdog types a
# "continue" into its tmux pane once the transcript has been quiet for 30 minutes, so
# work resumes by itself when the limit resets.
set -uo pipefail
cd "$(dirname "$0")"
rm -f STOP .claude/status-asked
prompt="Continuous run. Read CHARTER.md, PLAN.md and LOOP.md, then work the campaign in PLAN.md, starting from its Now line. There is no deadline; a session ending costs nothing because you commit often. Don't ask the user questions; decide and write down why."
nudge="Continue the campaign in PLAN.md from its Now line. (Watchdog: no activity for 30 minutes, probably a usage limit that has since reset.)"
transcripts="$HOME/.claude/projects/$(pwd | tr '/.' '--')"

if [ -n "${TMUX_PANE:-}" ]; then
  pane=$TMUX_PANE
  (
    while sleep 600; do
      [ -f STOP ] && exit 0
      newest=$(ls -t "$transcripts"/*.jsonl 2>/dev/null | head -1)
      [ -n "$newest" ] || continue
      quiet=$(( $(date +%s) - $(stat -c %Y "$newest") ))
      if [ "$quiet" -ge 1800 ]; then
        echo "watchdog: transcript quiet for $((quiet / 60)) min, nudging" >&2
        tmux send-keys -t "$pane" -l "$nudge"
        tmux send-keys -t "$pane" Enter
        touch "$newest"   # wait another 30 minutes before the next nudge
      fi
    done
  ) &
  watchdog=$!
  trap 'kill $watchdog 2>/dev/null' EXIT
else
  echo "overnight.sh: not inside tmux, so no watchdog; a usage limit will leave the session idle" >&2
fi

while [ ! -f STOP ]; do
  git pull --ff-only || echo "overnight.sh: git pull failed; continuing with the local tree" >&2
  claude --dangerously-skip-permissions --remote-control -- "$prompt"
  [ -f STOP ] && break
  echo "overnight.sh: claude exited; starting a fresh session in 60 s (touch STOP to end)" >&2
  sleep 60
done
