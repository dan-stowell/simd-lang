#!/usr/bin/env bash
# The driver's plan against go build's, without building: command lines (tests/sb_cmds.py, needs a stock
# action graph) and the contents of every embedcfg (against `go build -n`).
#   tests/sb_plan.sh <srcdir> <pkg> <stock actions.json>
set -e
cd "$(dirname "$0")/.."
src=$1; pkg=$2; actions=$3
t=$(mktemp -d /dev/shm/sbplan.XXXXXX)
w=$(build/sb -C "$src" -n -keep -work "$t" -dumpcmds "$t/cmds" "$pkg" | tail -1)
python3 tests/sb_cmds.py "$t/cmds" "$actions"
(cd "$src" && GOTOOLCHAIN=local CGO_ENABLED=0 GOFLAGS=-trimpath GOCACHE=$t/cache go build -n -o /dev/null "$pkg" 2> "$t/buildn.txt")
python3 - "$w" "$t/buildn.txt" <<'PY'
import re, sys, json, glob
t = open(sys.argv[2]).read()
stock = [json.JSONDecoder().raw_decode(t, m.end())[0] for m in re.finditer(r"cat >\S+/embedcfg << 'EOF' # internal\n", t)]
mine = [json.load(open(f)) for f in glob.glob(sys.argv[1] + '/b*/embedcfg')]
key = lambda c: json.dumps(c, sort_keys=True)
ok = sorted(map(key, stock)) == sorted(map(key, mine))
print(f"{'PASS' if ok else 'FAIL'} embedcfg: {len(mine)} files, {sum(len(c['Files']) for c in mine)} embedded paths")
sys.exit(0 if ok else 1)
PY
rm -rf "$t"
