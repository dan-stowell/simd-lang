#!/usr/bin/env python3
"""Independent small-job encoder, decoder, and sequential byte-image oracle.

No invocation of the C reference or Strand interpreter.  The format follows
explore/linkjob.c:4-9.  Export blocks are grouped by owner and ordinal; addresses
and relative fields are uint32, encoded little-endian.  Relocations are applied
in file order (including supplemental overlap/order tests).
"""
import argparse
import hashlib
import json
import pathlib
import random
import struct

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / "build/audit4-linkjob/cases"
MASK32 = (1 << 32) - 1


def digest(data):
    return hashlib.sha256(data).hexdigest()


def encode(sizes, exports, imports, relocs, seed=0):
    """exports = [(unit, offset, fingerprint64)], imports = export indices."""
    rng = random.Random(seed)
    code = rng.randbytes(sum(sizes))
    counts = [sum(e[0] == u for e in exports) for u in range(len(sizes))]
    starts = []
    n = 0
    for count in counts:
        starts.append(n)
        n += count
    u, ne, ni, nr, nc = len(sizes), len(exports), len(imports), len(relocs), len(code)
    words = [u, ne, ni, nr, nc] + sizes + counts
    words += [e[0] for e in exports] + [e[1] for e in exports]
    words += [e[2] & MASK32 for e in exports] + [e[2] >> 32 for e in exports]
    words += [exports[e][0] for e in imports]
    words += [e - starts[exports[e][0]] for e in imports]
    words += [exports[e][2] & MASK32 for e in imports]
    words += [exports[e][2] >> 32 for e in imports]
    for col in range(3):
        words += [r[col] for r in relocs]
    return struct.pack("<" + "I" * len(words), *words) + code


def decode(blob):
    assert len(blob) >= 20
    u, ne, ni, nr, nc = struct.unpack_from("<5I", blob)
    pos = 20
    job = dict(U=u, NE=ne, NI=ni, NR=nr, NC=nc)
    for name, size in [
        ("usize", u), ("uexp", u), ("exunit", ne), ("exoff", ne),
        ("efp1", ne), ("efp2", ne), ("imu", ni), ("imo", ni),
        ("ifp1", ni), ("ifp2", ni), ("runit", nr), ("roff", nr), ("rimp", nr),
    ]:
        job[name] = list(struct.unpack_from("<" + "I" * size, blob, pos))
        pos += size * 4
    assert pos + nc == len(blob)
    job["code"] = blob[pos:]
    assert sum(job["usize"]) == nc
    assert sum(job["uexp"]) == ne
    return job


def oracle(blob):
    j = decode(blob)
    image = bytearray()
    bases, export_blocks = [], []
    cursor = 0
    e = 0
    for u, size in enumerate(j["usize"]):
        bases.append(len(image))
        image += j["code"][cursor:cursor + size]
        image += b"\0" * (-size % 64)
        cursor += size
        block = []
        for _ in range(j["uexp"][u]):
            assert j["exunit"][e] == u
            assert 0 <= j["exoff"][e] < size
            block.append((bases[u] + j["exoff"][e], j["efp1"][e], j["efp2"][e]))
            e += 1
        export_blocks.append(block)
    imports = []
    for u, ordinal, lo, hi in zip(j["imu"], j["imo"], j["ifp1"], j["ifp2"]):
        address, elo, ehi = export_blocks[u][ordinal]
        assert (lo, hi) == (elo, ehi)
        imports.append(address)
    for u, off, imp in zip(j["runit"], j["roff"], j["rimp"]):
        assert off + 4 <= j["usize"][u]
        place = bases[u] + off
        value = (imports[imp] - place) & MASK32
        image[place:place + 4] = value.to_bytes(4, "little")
    return bytes(image)


def corpus():
    yield "empty", encode([], [], [], []), "core", "zero units/exports/imports/relocations"
    yield "zero_units", encode([0, 0, 0], [], [], []), "core", "zero-sized units"
    for size in [1, 3, 4, 15, 16, 17, 63, 64, 65, 127, 128, 129, 4095, 4096, 4097]:
        exports = [(0, size - 1, 0xFFFFFFFFFFFFFFFF)]
        relocs = [] if size < 4 else [(0, size - 4, 0)]
        yield f"size_{size}", encode([size], exports, [0], relocs, size), "core", "alignment/tail boundary; full-width fingerprint"
    sizes = [0, 4, 63, 64, 65, 128, 0, 129]
    exports = [(1, 0, 0), (2, 62, 1), (2, 0, 0x80000000),
               (3, 63, 0x8000000000000000), (4, 64, 0xFFFFFFFFFFFFFFFF),
               (4, 1, 0x7FFFFFFF7FFFFFFF), (4, 0, 0x00000000FFFFFFFF),
               (7, 128, 0xFFFFFFFF00000000)]
    imports = list(range(len(exports))) * 2
    relocs = [(u, off, (u + off) % len(imports)) for u, n in enumerate(sizes)
              for off in sorted(set([0, n - 4])) if n >= 4]
    yield "variable_exports", encode(sizes, exports, imports, relocs, 48), "core", "0/1/2/3 exports; unused exports/units; signed displacements; high fingerprint bits"
    yield "no_imports", encode([64, 65], [(0, 1, 42)], [], [], 19), "core", "exports without imports"
    yield "no_exports", encode([1, 64, 65], [], [], [], 19), "core", "code without exports"
    # Five DISTINCT fingerprints have the same slot in all four rounds:
    # Ne=5 -> Mask=15; each ea multiple of 4096, eb=0.  No random collision.
    exports = [(0, i, i * 4096) for i in range(5)]
    relocs = [(0, i * 4, i) for i in range(5)]
    yield "five_unique_four_round_collision", encode([64], exports, list(range(5)), relocs, 1), "core", "unique fingerprints; all four keys zero; four slots cannot place five exports"
    yield "five_unique_unused_four_round_collision", encode([64], exports, [], [], 1), "core", "same placement failure even with no imports or relocations"
    exports = [(0, 0, 0xDEADBEEF01234567), (0, 63, 0xDEADBEEF01234567)]
    yield "duplicate_fingerprint", encode([64], exports, [0, 1], [(0, 0, 0), (0, 8, 1)], 2), "collision", "different definitions share a fingerprint; ambiguity is not diagnosed"
    for step in [0, 1, 2, 3]:
        relocs = [(0, i * step, i % 2) for i in range(65)]
        yield f"sorted_overlap_{step}", encode([256], [(0, 0, 17), (0, 255, 99)],
              [0, 1], relocs, step), "overlap", "sorted in-range relocations overlap; file-order overwrite oracle"
    relocs = [(1, 0, 0), (0, 0, 1), (1, 60, 1)]
    yield "ungrouped_relocations", encode([64, 64], [(0, 5, 1), (1, 7, 2)],
          [0, 1], relocs, 16), "unordered", "format comments do not require unit grouping; LINKING benchmark does"
    for seed in range(64):
        rng = random.Random(9941 + seed)
        sizes = [rng.choice([0, 1, 3, 4, 15, 16, 17, 63, 64, 65, 127, 128, 129, 257])
                 for _ in range(rng.randrange(1, 24))]
        exports = [(u, rng.randrange(n), rng.getrandbits(64))
                   for u, n in enumerate(sizes) if n for _ in range(rng.randrange(0, 8))]
        imports = [rng.randrange(len(exports)) for _ in range(rng.randrange(1, 100))] if exports else []
        relocs = []
        if imports:
            for u, n in enumerate(sizes):
                off = rng.randrange(0, 5)
                while off + 4 <= n:
                    relocs.append((u, off, rng.randrange(len(imports))))
                    off += rng.randrange(4, 20)
        yield f"random_{seed:02d}", encode(sizes, exports, imports, relocs, seed), "core", "seeded small job; variable counts, full64 fingerprints, sorted disjoint relocations"


def generate():
    OUT.mkdir(parents=True, exist_ok=True)
    manifest = []
    for name, blob, category, description in corpus():
        image = oracle(blob)
        (OUT / (name + ".job")).write_bytes(blob)
        (OUT / (name + ".expected")).write_bytes(image)
        j = decode(blob)
        manifest.append(dict(name=name, category=category, description=description,
                             header={k: j[k] for k in ("U", "NE", "NI", "NR", "NC")},
                             job_sha256=digest(blob), image_sha256=digest(image),
                             job_bytes=len(blob), image_bytes=len(image)))
    (ROOT / "reviews/audit4-linkjob/cases.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(f"generated {len(manifest)} independently-oracled cases in {OUT}")


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--input", type=pathlib.Path)
    ap.add_argument("--output", type=pathlib.Path)
    args = ap.parse_args()
    if args.input:
        args.output.write_bytes(oracle(args.input.read_bytes()))
    else:
        generate()
