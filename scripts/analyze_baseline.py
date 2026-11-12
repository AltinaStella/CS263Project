"""Week 3 baseline analysis: best fixed configuration per workload and RQ1 figures.

Usage:
    python scripts/analyze_baseline.py results/week3-baseline

Reads summary.csv (from aggregate.py) and matrix.json, plus the per-run CSVs of
the phase workload, and writes into the same directory:
    best_fixed.csv      best config per workload and memory budget
    rq1_tradeoff.png    median p99 vs. peak memory, log scale, budget lines
    rq1_gc_cost.png     GC demand share and assist share per config
    rq1_phase.png       p99 and memory over time in the phase workload

Selection rule: among configs with no dropped requests and median peak memory
within the budget (budget = factor x the workload's live_mib, with 5% slack
because GOMEMLIMIT is a soft limit), pick the lowest median p99.
"""
import json
import sys
from pathlib import Path

import matplotlib.pyplot as plt
import pandas as pd

BUDGETS = {"none": None, "1.5x": 1.5, "1.2x": 1.2}
SLACK = 1.05
ORDER = ["gogc50", "default", "gogc200", "gogc400", "lim1.5", "lim1.2", "off-lim1.5", "off-lim1.2"]


def main():
    root = Path(sys.argv[1] if len(sys.argv) > 1 else "results/week3-baseline")
    m = json.loads((root / "matrix.json").read_text())
    live = {w: v["live_mib"] for w, v in m["workloads"].items()}
    s = pd.read_csv(root / "summary.csv")
    wide = s.pivot_table(index=["workload", "config"], columns="metric", values="median").reset_index()
    workloads = [w for w in m["workloads"] if w in set(wide.workload)]

    rows = []
    for w in workloads:
        g = wide[(wide.workload == w) & (wide.dropped == 0)]
        for name, x in BUDGETS.items():
            ok = g if x is None else g[g.peak_mem_total_mib <= x * live[w] * SLACK]
            if ok.empty:
                rows.append({"workload": w, "budget": name, "config": "(none fits)"})
                continue
            b = ok.sort_values("p99_ms").iloc[0]
            rows.append({"workload": w, "budget": name,
                         "budget_mib": None if x is None else round(x * live[w]),
                         "config": b.config, "p99_ms": round(b.p99_ms, 2),
                         "p999_ms": round(b.p999_ms, 2), "peak_mem_mib": round(b.peak_mem_total_mib),
                         "gc_demand": round(b.gc_demand_frac, 3)})
    best = pd.DataFrame(rows)
    best.to_csv(root / "best_fixed.csv", index=False)
    print(best.to_string(index=False))

    # Trade-off: p99 vs. memory, log y so the 1.2x outliers do not flatten the rest.
    ncol = 3
    nrow = -(-len(workloads) // ncol)
    fig, axes = plt.subplots(nrow, ncol, figsize=(5 * ncol, 4 * nrow), squeeze=False)
    for ax in axes.flat[len(workloads):]:
        ax.set_visible(False)
    for ax, w in zip(axes.flat, workloads):
        g = wide[wide.workload == w].set_index("config")
        for c in ORDER:
            if c in g.index:
                r = g.loc[c]
                ax.scatter(r.peak_mem_total_mib, r.p99_ms, marker="x" if r.dropped > 0 else "o",
                           label=c + (" (drops)" if r.dropped > 0 else ""))
        ax.set(title=f"{w} (live {live[w]} MiB)", xlabel="peak runtime memory, MiB",
               ylabel="p99 latency, ms", yscale="log")
        for name, x in BUDGETS.items():
            if x is not None:
                ax.axvline(x * live[w], ls="--", c="gray", lw=0.8)
                ax.text(x * live[w], ax.get_ylim()[1], f" {name}", fontsize=7, va="top", c="gray")
        ax.grid(alpha=0.3, which="both")
        ax.legend(fontsize=6)
    fig.tight_layout()
    fig.savefig(root / "rq1_tradeoff.png", dpi=150)

    # GC cost per config.
    fig, axes = plt.subplots(1, 2, figsize=(12, 4))
    for ax, col, title in ((axes[0], "gc_demand_frac", "GC share of paid CPU (demand)"),
                           (axes[1], "assist_busy_frac", "Mark assist share of busy CPU")):
        p = wide.pivot(index="config", columns="workload", values=col).reindex(ORDER)[workloads]
        p.plot.bar(ax=ax, width=0.8)
        ax.set(title=title, xlabel="", ylabel="fraction")
        ax.grid(alpha=0.3, axis="y")
        ax.legend(fontsize=7)
    fig.tight_layout()
    fig.savefig(root / "rq1_gc_cost.png", dpi=150)

    # Phase workload over time (median across reps per sampling interval).
    if (root / "phase").exists():
        fig, (a1, a2) = plt.subplots(1, 2, figsize=(12, 4))
        for c in ("gogc50", "default", "gogc400", "lim1.5"):
            d = root / "phase" / c
            if not d.exists():
                continue
            ts = pd.concat([pd.read_csv(f).reset_index() for f in sorted(d.glob("rep*.csv"))])
            med = ts.groupby("index")[["t_sec", "p99_ms", "mem_total_mib"]].median()
            a1.plot(med.t_sec, med.p99_ms, label=c)
            a2.plot(med.t_sec, med.mem_total_mib, label=c)
        for a in (a1, a2):
            a.axvline(10, ls="--", c="gray", lw=0.8)
            a.grid(alpha=0.3)
            a.legend(fontsize=8)
            a.set_xlabel("time (s); phase switch at 10 s")
        a1.set(ylabel="p99 per 0.5 s interval, ms", title="phase: tail latency")
        a2.set(ylabel="runtime memory, MiB", title="phase: memory")
        fig.tight_layout()
        fig.savefig(root / "rq1_phase.png", dpi=150)
    print(f"wrote best_fixed.csv, rq1_tradeoff.png, rq1_gc_cost.png, rq1_phase.png in {root}")


if __name__ == "__main__":
    main()
