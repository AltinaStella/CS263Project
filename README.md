# Evaluating Adaptive Garbage Collection Tuning in Go: Memory, Throughput, and Tail Latency

CS 263 final project - Yi Lin

## Description

This project investigates whether adaptive garbage collection tuning can improve the trade-off between memory usage, throughput, and tail latency in Go applications. I will survey Go's garbage collector and tuning mechanisms, implement workloads with different allocation rates and object lifetimes, and develop a lightweight controller that adjusts GC settings using runtime metrics. I will compare the controller against default and tuned fixed configurations under different memory budgets and workload patterns, measuring memory consumption, throughput, p99 request latency, and GC CPU overhead. The project will proceed through five weekly milestones: background research and experimental design, workload implementation, baseline evaluation, adaptive controller implementation, and comparative evaluation and analysis.

## Milestones

| Week | Focus | Status |
|---|---|---|
| 1 | Background research and experimental design | Done |
| 2 | Workload implementation | Done |
| 3 | Baseline evaluation (default and fixed GC configurations) | Done |
| 4 | Adaptive controller implementation | In progress |
| 5 | Comparative evaluation, analysis, and final report | |

Weekly progress notes are in [`docs/`](docs/). Week 1: [progress](docs/week1-progress.md),
[reading list](readings/README.md), [reading log](docs/reading-log.md). Week 2: [progress](docs/week2-progress.md). Week 3: [progress](docs/week3-progress.md).

## Repository layout

```
cmd/gcprobe/     minimal allocation workload + runtime/metrics sampler (week 1)
cmd/gcserve/     request-serving workload with open-loop load and latency (week 2)
experiments/     experiment matrices for scripts/run_matrix.py
docs/            background notes, experiment design, weekly progress
readings/        reading list by topic, plus local copies of openly licensed Go docs
scripts/         experiment and plotting scripts
results/         experiment outputs (CSV, figures, run metadata)
```

## Quick start

Experiments use Go 1.27.3 (pinned by the `toolchain` line in `go.mod`; any Go 1.21+
fetches it automatically). The sweep script fixes `GOMAXPROCS=4` by default.

```bash
# one run: 200 MiB/s allocation, objects live ~500 ms, 10 s, GOGC=100
GOGC=100 go run ./cmd/gcprobe -rate 200 -lifetime 500ms -duration 10s -out results/probe.csv

# week 1 sweep over GOGC = 50/100/200/400, then plot (needs pandas + matplotlib)
bash scripts/sweep_gogc.sh
python scripts/plot_week1.py
```

`gcprobe` flags: `-rate` (MiB/s), `-obj` (object size, bytes), `-lifetime`, `-duration`, `-interval` (sampling period), `-gogc` (integer or `off`), `-memlimit` (MiB), `-out` (CSV path or `-`).

```bash
# request-serving workload: 8000 req/s Poisson arrivals, 128 MiB cache, GOGC=200
GOMAXPROCS=4 go run ./cmd/gcserve -rate 8000 -cache 128 -gogc 200 -duration 30s -out results/run.csv

# phase change: low load with a small cache, then high load with a large cache
GOMAXPROCS=4 go run ./cmd/gcserve -phases "15s:rate=2000,cache=64;15s:rate=8000,cache=256"
```

`gcserve` writes a per-interval CSV (latency percentiles + runtime metrics) and a JSON
summary of the run after warm-up. Run `go run ./cmd/gcserve -h` for all flags.

```bash
# experiment matrix (workloads x GC configs x repetitions), then median + CI and plot
python scripts/run_matrix.py experiments/pilot.json        # --dry-run to preview
python scripts/aggregate.py results/week2-pilot
```

Matrix files live in `experiments/`; see the docstring of `scripts/run_matrix.py`
for the format.
