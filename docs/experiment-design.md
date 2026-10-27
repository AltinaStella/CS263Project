# Experiment Design (draft, week 1)

## Research questions

- **RQ1 - Fixed-configuration trade-offs.** As allocation rate, object lifetime, and memory budget vary, which fixed GC settings (GOGC, GOMEMLIMIT) favor throughput, which favor tail latency, and what do they cost in memory and GC CPU?
- **RQ2 - When does adaptation help?** Under changing workloads (low->high load, short-lived->long-lived objects, bursts), can a controller that adjusts GC settings at run time beat a well-tuned fixed configuration?
- **RQ3 - What does adaptation cost?** How much do metric collection, adjustment overhead, reaction lag, and oscillation offset any gains?

Note: Go's GC is already adaptive (the pacer schedules collections from heap growth and allocation rate). The question here is whether *application-level* adjustment of the tuning knobs on top of that adds value.

## Independent variables

| Variable | Planned levels (to be refined in week 2) |
|---|---|
| Allocation rate | low / medium / high (e.g. 50, 200, 800 MiB/s) |
| Object lifetime / live heap | short (~ms), medium (~100s of ms), long-lived cache |
| Memory budget | unconstrained, and 1.5x / 1.2x of measured peak live heap |
| Load pattern | steady, bursty, phase change |
| GC configuration | see below |

## GC configurations compared

1. **Default**: `GOGC=100`, no memory limit
2. **Fixed GOGC**: 50, 100, 200, 400
3. **Memory limit**: `GOMEMLIMIT` at each budget, with default GOGC and with `GOGC=off`
4. **Best fixed**: the best configuration per workload found in week 3 (an oracle-style baseline)
5. **Adaptive controller** (week 4)

## Metrics

| Metric | Source |
|---|---|
| Throughput (requests/s or work items/s) | workload counters |
| Latency p50 / p99 / p99.9 | per-request timing in the workload (week 2) |
| Memory: total runtime memory, live heap, heap goal | `runtime/metrics`: `/memory/classes/total:bytes`, `/gc/heap/live:bytes`, `/gc/heap/goal:bytes` |
| Process RSS | OS (`/proc/self/status` on Linux) |
| GC CPU fraction | `/cpu/classes/gc/total:cpu-seconds` / `/cpu/classes/total:cpu-seconds` (whole-run; per-interval values are noisy) |
| GC frequency | `/gc/cycles/total:gc-cycles`, cross-checked with `GODEBUG=gctrace=1` |

## Experimental control

- **Toolchain:** Go 1.27.3, pinned in `go.mod` (exact `go version` recorded per run). The Green Tea GC is the default since Go 1.26, so all main experiments use it. A comparison with `GOEXPERIMENT=nogreenteagc` is optional extra work, not part of the core.
- **Machine:** one fixed machine and OS (Linux or WSL2), on AC power, with other heavy programs closed. Record CPU model, core count, and RAM.
- **GOMAXPROCS:** fixed explicitly for every run (default 4). This matters for the GC CPU fraction: `/cpu/classes/total` counts all GOMAXPROCS, including idle time, so on a 32-thread machine with a light workload the fraction looks tiny (0.0002 in the day 1 smoke test). A small fixed GOMAXPROCS keeps the number meaningful and comparable across runs.
- **Warm-up:** discard the first N seconds of each run (N set in week 2).
- **Repetitions:** at least 5 runs per configuration; report median and 95% confidence interval.
- **Metadata:** every run writes Go version, flags, environment variables, and a timestamp next to its CSV.

## Week 1 validation

`cmd/gcprobe` checks that the knobs and metrics behave as expected before the real workload is built: with fixed allocation rate and lifetime, raising GOGC should reduce GC cycles and GC CPU while increasing memory, and a memory limit should cap total memory. Results: `results/week1/`.

## Open questions

- Load generator: closed-loop (fixed concurrency) or open-loop (fixed arrival rate)? Open-loop avoids coordinated omission in tail latency, so it is the current preference.
- Controller inputs: heap goal vs. limit, GC CPU fraction, latency feedback, or a combination?
