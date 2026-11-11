#!/usr/bin/env python3
"""Summarize existing measurements; never executes a benchmark."""
import json
import pathlib
import statistics

D = pathlib.Path(__file__).resolve().parent
r = json.loads((D / "timings.json").read_text())
s = r["summary"]
lines = ["# Authorized full-job timings", "",
         "Whole processes; cached regular-file stdin; `/dev/null` stdout; seven",
         "randomized/interleaved rounds. Real outputs checked before and after",
         "against the independent full-job oracle. Times are medians in ms.", "",
         "| implementation | allowed CPUs | wall | user | system | min–max wall | minor faults | max RSS KiB |",
         "|---|---:|---:|---:|---:|---:|---:|---:|"]
for key, v in s.items():
    name, cpus = key.rsplit(":", 1)
    lines.append(f'| {name} | {cpus} | {v["median_ms"]:.2f} | {v["median_user_ms"]:.2f} | '
                 f'{v["median_sys_ms"]:.2f} | {v["min_ms"]:.2f}–{v["max_ms"]:.2f} | '
                 f'{v["median_minflt"]:.0f} | {v["median_maxrss_kib"]:.0f} |')
lines += ["", "## Comparisons", ""]
comparisons = [
    ("Ordinal Strand / original C", "link:1", "ref_o2:1"),
    ("Ordinal Strand / same-O2 explicit-byte-store C", "link:1", "ref_o2_bytes:1"),
    ("Original C / same-O2 byte stores", "ref_o2:1", "ref_o2_bytes:1"),
    ("Original C / same-source O2 no-fortify", "ref_o2:1", "ref_o2_nf:1"),
    ("Ordinal Strand one / eight CPUs", "link:1", "link:8"),
    ("Fingerprint Strand one / eight CPUs", "link2:1", "link2:8"),
    ("Full-image C serial / unit-parallel eight CPUs", "c_full_nf:1", "c_parallel_nf:8"),
]
for label, a, b in comparisons:
    if a in s and b in s:
        lines.append(f'- {label}: {s[a]["median_ms"]/s[b]["median_ms"]:.3f}× (ratio of medians).')
for label, a, b in [
    ("Strand fingerprint minus ordinal, one CPU", "link2:1", "link:1"),
    ("C fingerprint minus ordinal, one CPU (matched full-image path)", "c_fingerprint_nf:1", "c_full_nf:1"),
    ("Strand fingerprint minus ordinal, eight CPUs", "link2:8", "link:8"),
]:
    if a in s and b in s:
        an, ac = a.rsplit(":", 1)
        bn, bc = b.rsplit(":", 1)
        av = {x["round"]: x["wall_ns"]/1e6 for x in r["samples"] if x["variant"] == an and x["cpuset"] == ac}
        bv = {x["round"]: x["wall_ns"]/1e6 for x in r["samples"] if x["variant"] == bn and x["cpuset"] == bc}
        paired = [av[k] - bv[k] for k in sorted(av.keys() & bv.keys())]
        lines.append(f'- {label}: {s[a]["median_ms"]-s[b]["median_ms"]:.2f} ms difference of medians; '
                     f'{statistics.median(paired):.2f} ms median within-round difference '
                     f'(all paired differences: {", ".join(f"{v:.2f}" for v in paired)}).')
lines += ["", "These are whole-process differences, not isolated phase instrumentation.",
          "The affinity column is not a thread count: all reference C variants,",
          "C full/checked/stream/fingerprint remain single-threaded at affinity eight.",
          "The bounded-buffer streaming row does not materialize a full image and is",
          "not directly comparable as a memory-image API. Parallel system time is",
          "summed across threads; it can exceed elapsed time. No claim about real",
          "output-device I/O follows from a `/dev/null` benchmark. Wall/user/system",
          "medians are computed separately and need not add. RSS includes input mappings.", "",
          f'Input SHA-256: `{r["input"]["sha256"]}`.',
          f'Output SHA-256: `{r["expected_output_sha256"]}`.', ""]
(D / "timings.md").write_text("\n".join(lines))
print("\n".join(lines))
