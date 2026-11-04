"""Aggregate a run_matrix.py results directory: median and CI per configuration.

Usage:
    python scripts/aggregate.py results/pilot

Writes <dir>/summary.csv (one row per workload, config, metric) and
<dir>/tradeoff.png (p99 latency vs. peak memory, one panel per workload).
Requires pandas and matplotlib.

The CI for the median uses order statistics (distribution-free): the interval
between the j-th and k-th smallest values that covers the median with at least
95% probability. With fewer than 6 runs no such interval exists, so it falls back
to [min, max] and reports the actual coverage (93.8% for n = 5) in ci_level.
"""
import json
import sys
from math import comb
from pathlib import Path

import matplotlib.pyplot as plt
import pandas as pd

# Metrics pulled from each gcserve summary JSON (column name -> path).
METRICS = {
    "p50_ms": ("latency_ms", "p50"),
    "p99_ms": ("latency_ms", "p99"),
    "p999_ms": ("latency_ms", "p999"),
    "svc_p99_ms": ("service_ms", "p99"),
    "lag_p99_ms": ("dispatch_lag_ms", "p99"),
    "throughput_rps": ("throughput_rps",),
    "dropped": ("dropped",),
    "gc_cycles": ("gc_cycles",),
    "gc_demand_frac": ("gc_demand_frac",),
    "assist_busy_frac": ("assist_busy_frac",),
    "demand_cpu_ms_per_request": ("demand_cpu_ms_per_request",),
    "peak_mem_total_mib": ("peak_mem_total_mib",),
    "mean_mem_total_mib": ("mean_mem_total_mib",),
    "peak_heap_live_mib": ("peak_heap_live_mib",),
}


def median_ci(values, level=0.95):
    """Return (median, lo, hi, achieved level) for the median."""
    x = sorted(values)
    n = len(x)
    med = float(pd.Series(x).median())
    if n < 2:
        return med, med, med, 0.0
    # P(x_(j) <= median <= x_(n-j+1)) = 1 - 2 * P(Binom(n, 1/2) < j)
    best = (0, 1 - 2 * 0.5**n)  # j = 1: [min, max]
    for j in range(2, n // 2 + 1):
        cov = 1 - 2 * sum(comb(n, i) for i in range(j)) / 2**n
        if cov < level:
            break
        best = (j - 1, cov)
    j, cov = best
    return med, x[j], x[n - 1 - j], cov


def load(root):
    rows = []
    for f in sorted(root.glob("*/*/rep*.json")):
        s = json.loads(f.read_text())
        row = {"workload": f.parent.parent.name, "config": f.parent.name, "rep": f.stem}
        for col, path in METRICS.items():
            v = s
            for p in path:
                v = v.get(p) if isinstance(v, dict) else None
            row[col] = v
        rows.append(row)
    return pd.DataFrame(rows)


def main():
    root = Path(sys.argv[1] if len(sys.argv) > 1 else "results/pilot")
    df = load(root)
    if df.empty:
        sys.exit(f"no rep*.json files under {root}")
    df.to_csv(root / "runs.csv", index=False)

    out = []
    for (w, c), g in df.groupby(["workload", "config"]):
        for col in METRICS:
            vals = g[col].dropna().astype(float).tolist()
            if not vals:
                continue
            med, lo, hi, cov = median_ci(vals)
            out.append({"workload": w, "config": c, "metric": col, "n": len(vals),
                        "median": round(med, 4), "ci_lo": round(lo, 4), "ci_hi": round(hi, 4),
                        "ci_level": round(cov, 3)})
    summ = pd.DataFrame(out)
    summ.to_csv(root / "summary.csv", index=False)

    wide = summ.pivot_table(index=["workload", "config"], columns="metric", values="median")
    wide.insert(0, "n", summ.groupby(["workload", "config"]).n.max())
    cols = [c for c in ("n", "p99_ms", "p999_ms", "gc_demand_frac", "demand_cpu_ms_per_request",
                        "peak_mem_total_mib") if c in wide.columns]
    print(wide[cols].round(3).to_string())

    # Trade-off plot: median p99 vs. median peak memory, with CI whiskers.
    loads = sorted(summ.workload.unique())
    fig, axes = plt.subplots(1, len(loads), figsize=(5 * len(loads), 4), squeeze=False)
    for ax, w in zip(axes[0], loads):
        s = summ[summ.workload == w].set_index(["config", "metric"])
        for c in sorted(summ[summ.workload == w].config.unique()):
            try:
                lat, mem = s.loc[(c, "p99_ms")], s.loc[(c, "peak_mem_total_mib")]
            except KeyError:
                continue
            ax.errorbar(mem["median"], lat["median"],
                        xerr=[[mem["median"] - mem.ci_lo], [mem.ci_hi - mem["median"]]],
                        yerr=[[lat["median"] - lat.ci_lo], [lat.ci_hi - lat["median"]]],
                        fmt="o", capsize=3, label=c)
        ax.set(title=w, xlabel="peak runtime memory, MiB", ylabel="p99 latency, ms")
        ax.grid(alpha=0.3)
        ax.legend(fontsize=8)
    fig.tight_layout()
    fig.savefig(root / "tradeoff.png", dpi=150)
    print(f"wrote {root / 'summary.csv'}, {root / 'runs.csv'}, {root / 'tradeoff.png'}")


if __name__ == "__main__":
    main()
