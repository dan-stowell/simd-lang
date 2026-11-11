#!/usr/bin/env python3
"""Independently oracle generated large jobs, then hash every implementation.

Correctness only; no wallclock/performance samples. Python uses mmap views to
avoid a giant Python-int parse, but all layout/import/patch logic is independent.
Writes only under build/audit4-linkjob and reviews/audit4-linkjob.
"""
import argparse
import array
import hashlib
import json
import mmap
import os
import pathlib
import struct
import sys
from measure import B, D, COMMANDS, output_check, sha_file


def independent_oracle(path):
    assert sys.byteorder == "little"
    with path.open("rb") as f:
        raw = mmap.mmap(f.fileno(), 0, access=mmap.ACCESS_READ)
    U, NE, NI, NR, NC = struct.unpack_from("<5I", raw)
    pos = 20
    j = {}
    for name, count in [("size", U), ("counts", U), ("exunit", NE), ("exoff", NE),
                        ("elo", NE), ("ehi", NE), ("imu", NI), ("imo", NI),
                        ("ilo", NI), ("ihi", NI), ("ru", NR), ("ro", NR), ("ri", NR)]:
        j[name] = memoryview(raw)[pos:pos + count * 4].cast("I")
        pos += count * 4
    assert pos + NC == len(raw)
    bases, ins, exs = [0], [0], [0]
    for n, count in zip(j["size"], j["counts"]):
        bases.append(bases[-1] + ((n + 63) // 64) * 64)
        ins.append(ins[-1] + n)
        exs.append(exs[-1] + count)
    assert ins[-1] == NC and exs[-1] == NE
    for u in range(U):
        for e in range(exs[u], exs[u + 1]):
            assert j["exunit"][e] == u and j["exoff"][e] < j["size"][u]
    imports = array.array("I", [0]) * NI
    for i in range(NI):
        u, ordinal = j["imu"][i], j["imo"][i]
        assert u < U and ordinal < j["counts"][u]
        e = exs[u] + ordinal
        assert j["elo"][e] == j["ilo"][i] and j["ehi"][e] == j["ihi"][i]
        imports[i] = bases[u] + j["exoff"][e]
    image = bytearray(bases[-1])
    for u in range(U):
        image[bases[u]:bases[u] + j["size"][u]] = raw[pos + ins[u]:pos + ins[u + 1]]
    previous_end = -1
    sorted_disjoint = True
    for u, off, imp in zip(j["ru"], j["ro"], j["ri"]):
        assert u < U and off + 4 <= j["size"][u] and imp < NI
        place = bases[u] + off
        if place < previous_end:
            sorted_disjoint = False
        previous_end = place + 4
        struct.pack_into("<I", image, place, (imports[imp] - place) & 0xFFFFFFFF)
    result = dict(header=dict(U=U, NE=NE, NI=NI, NR=NR, NC=NC),
                  image_bytes=len(image), image_sha256=hashlib.sha256(image).hexdigest(),
                  input_bytes=len(raw), input_sha256=sha_file(path),
                  sorted_disjoint=sorted_disjoint,
                  fingerprint_low_max=max(j["elo"], default=0),
                  fingerprint_high_max=max(j["ehi"], default=0))
    j.clear()
    raw.close()
    return result


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--job", type=pathlib.Path, required=True)
    ap.add_argument("--output", type=pathlib.Path, required=True)
    ap.add_argument("--variants", default=",".join(COMMANDS))
    args = ap.parse_args()
    result = dict(job=str(args.job), oracle=independent_oracle(args.job), outputs={})
    print(json.dumps(result["oracle"]), flush=True)
    cpus = sorted(os.sched_getaffinity(0))
    for name in args.variants.split(","):
        cmd = COMMANDS[name]
        for affinity in [cpus[:1], cpus]:
            env = dict(os.environ, OMP_NUM_THREADS=str(len(affinity)), OMP_DYNAMIC="FALSE", OMP_PROC_BIND="FALSE")
            got = output_check(cmd, affinity, args.job, env)
            got["affinity"] = affinity
            got["match"] = got["exit"] == 0 and got["sha256"] == result["oracle"]["image_sha256"]
            result["outputs"][name + ":" + str(len(affinity))] = got
            args.output.write_text(json.dumps(result, indent=2) + "\n")
            print(name, len(affinity), got["match"], got["sha256"], flush=True)
            if not got["match"]:
                raise SystemExit("output mismatch")


if __name__ == "__main__":
    main()
