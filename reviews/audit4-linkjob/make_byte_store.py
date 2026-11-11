#!/usr/bin/env python3
"""Keep original ref algorithm/flags; replace only its fixed-four-byte memcpy."""
from pathlib import Path

root = Path(__file__).resolve().parents[2]
src = (root / "explore/linkjob.c").read_text()
old = "memcpy(o + roff[k], &v, 4);"
new = """uint8_t *field = o + roff[k];
      field[0] = (uint8_t)v; field[1] = (uint8_t)(v >> 8);
      field[2] = (uint8_t)(v >> 16); field[3] = (uint8_t)(v >> 24);"""
assert src.count(old) == 1
(root / "reviews/audit4-linkjob/ref_bytestore.c").write_text(
    "// AUDIT ONLY: explore/linkjob.c with only the relocation memcpy replaced by\n"
    "// explicit little-endian byte stores; compiled with the original gcc -O2.\n"
    + src.replace(old, new))
