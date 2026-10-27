"""Plot heap size and GC cycles over time for the week 1 GOGC sweep.

Usage: python scripts/plot_week1.py [results/week1]
Requires: pandas, matplotlib
"""
import sys
from pathlib import Path

import matplotlib.pyplot as plt
import pandas as pd

d = Path(sys.argv[1] if len(sys.argv) > 1 else "results/week1")
files = sorted(d.glob("gogc_*.csv"), key=lambda p: int(p.stem.split("_")[1]))
if not files:
    sys.exit(f"no gogc_*.csv files in {d}")

fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4))
for f in files:
    df = pd.read_csv(f)
    label = f"GOGC={f.stem.split('_')[1]}"
    ax1.plot(df.t_sec, df.mem_total_mib, label=label)
    ax2.plot(df.t_sec, df.gc_cycles, label=label)

ax1.set(xlabel="time (s)", ylabel="runtime memory, MiB", title="Memory footprint")
ax2.set(xlabel="time (s)", ylabel="GC cycles (cumulative)", title="GC frequency")
for ax in (ax1, ax2):
    ax.grid(alpha=0.3)
    ax.legend()
fig.tight_layout()
out = d / "week1_gogc_sweep.png"
fig.savefig(out, dpi=150)
print(f"saved {out}")
