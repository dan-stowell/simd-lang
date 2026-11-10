#!/usr/bin/env python3
"""Measure the actual ordered-pair coverage of tests/fsm_all.py's fixture."""
import json
from pathlib import Path
import random

ROOT = Path(__file__).resolve().parents[1]
r = random.Random(3)
pairs = [(a, b) for a in range(16) for b in range(16)]
seq = bytearray()
for shift in range(64):
    r.shuffle(pairs)
    seq += bytes(shift % 7 + 1)
    for a, b in pairs:
        seq += bytes((a, b))
seq += bytes(r.randrange(16) for _ in range(70001))
seen = {(a, b, i % 64) for i, (a, b) in enumerate(zip(seq, seq[1:]))}
missing = [(a, b, i) for a in range(16) for b in range(16) for i in range(64)
           if (a, b, i) not in seen]
# Each table's functions are 16*k..16*k+15. Different tables never
# participate in one run, so no transition pair crossing these groups occurs.
function_pairs = {(16 * k + a, 16 * k + b)
                  for k in range(16) for a in range(16) for b in range(16)}
out = {
    "fixture_bytes": len(seq),
    "all_class_pair_offsets": 16 * 16 * 64,
    "observed_class_pair_offsets": len(seen),
    "missing_count": len(missing),
    "first_missing": missing[:30],
    "all_function_pairs": 256 * 256,
    "function_pairs_possible_in_tables": len(function_pairs),
    "function_pairs_excluded_by_table_partition": 256 * 256 - len(function_pairs),
    "note": "Counts are coverage, not a failing output; default 103022-byte pipe run is below the 524288-element parallel threshold. Eager mode exercises the pool."
}
print(json.dumps(out, indent=2))
