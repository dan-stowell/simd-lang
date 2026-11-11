#!/usr/bin/env python3
"""Summarize already-authorized runs; never executes a benchmark."""
import collections
import json
import pathlib
import statistics

HERE = pathlib.Path(__file__).resolve().parent
trials = json.loads((HERE / "timings.json").read_text())
grouped = collections.defaultdict(lambda: collections.defaultdict(list))
paired = collections.defaultdict(dict)
for t in trials:
    condition = t["condition"] + "/" + t.get("binary", "controls")
    job = t["rows"][0]
    assert not job["check_only"] and job["hash_bits"] == 64
    for r in t["rows"][1:]:
        assert not r.get("mismatches", 0) and not r.get("mismatch", 0) and not r.get("unplaced", 0)
        grouped[condition][r["variant"]].append(r)
        paired[condition, t["rep"]][r["variant"]] = r

summary = {}
for condition, variants in grouped.items():
    rows = {}
    for variant, runs in sorted(variants.items()):
        measures = {}
        for key in ("ms", "build_ms", "lookup_ms", "values_ms", "expand_ms", "minor_faults", "major_faults"):
            if key in runs[0]:
                values = [r[key] for r in runs]
                measures[key] = {"median": statistics.median(values), "min": min(values), "max": max(values)}
        rows[variant] = {"n": len(runs), **measures}
    summary[condition] = {"variants": rows}

# Ratios <1 mean numerator faster. Only compare matched runs in one condition.
contrasts = [
    ("soa_pre_exact/soa_hash_exact", "soa_pre_exact", "soa_hash_exact", "ms"),
    ("aos24_hash_exact/soa_hash_exact", "aos24_hash_exact", "soa_hash_exact", "ms"),
    ("aos16_hash_exact/soa_hash_exact", "aos16_hash_exact", "soa_hash_exact", "ms"),
    ("soa_pre_exact/soa_pre_fp", "soa_pre_exact", "soa_pre_fp", "lookup_ms"),
    ("aos24_pre_exact/aos24_pre_fp", "aos24_pre_exact", "aos24_pre_fp", "lookup_ms"),
    ("race_simd/race_scalar_total", "race_simd", "race_scalar", "ms"),
    ("race_simd/race_scalar_lookup", "race_simd", "race_scalar", "lookup_ms"),
    ("unit/whole", "copy_unit_patch", "copy_whole_patch", "ms"),
    ("block/whole", "copy_block_patch", "copy_whole_patch", "ms"),
    ("stream_simd/unit", "stream_simd_values", "copy_unit_patch", "ms"),
    ("stream_simd/block", "stream_simd_values", "copy_block_patch", "ms"),
    ("stream_simd/stream_scalar", "stream_simd_values", "stream_scalar_values", "ms"),
    ("simd_values/scalar_values", "stream_simd_values", "stream_scalar_values", "values_ms"),
]
for condition in summary:
    comparisons = {}
    for label, numerator, denominator, field in contrasts:
        ratios = [rows[numerator][field]/rows[denominator][field]
                  for (cond, rep), rows in paired.items() if cond == condition]
        comparisons[label] = {"field": field, "median_paired_ratio": statistics.median(ratios),
                              "min": min(ratios), "max": max(ratios)}
    summary[condition]["comparisons"] = comparisons
(HERE / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
lines = ["# Authorized control timings", "", "Milliseconds; each cell median [min, max].", ""]
for condition, data in summary.items():
    lines += [f"## {condition}", "", "| variant | total ms | build ms | lookup ms | minor faults |", "|---|---:|---:|---:|---:|"]
    for name, row in data["variants"].items():
        def cell(field):
            if field not in row:
                return "—"
            x=row[field]
            return f'{x["median"]:.3f} [{x["min"]:.3f}, {x["max"]:.3f}]'
        lines.append(f"| {name} | {cell('ms')} | {cell('build_ms')} | {cell('lookup_ms')} | {cell('minor_faults')} |")
    lines += ["", "Paired ratios (<1 means numerator faster):", ""]
    for label, value in data["comparisons"].items():
        lines.append(f'* `{label}` ({value["field"]}): {value["median_paired_ratio"]:.3f} [{value["min"]:.3f}, {value["max"]:.3f}]')
    lines.append("")
(HERE / "timing-summary.md").write_text("\n".join(lines))
print("\n".join(lines))
