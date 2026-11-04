# Experiment Design (draft, updated week 2)

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

## Workload (`cmd/gcserve`, week 2)

An in-process server driven by an open-loop load generator:

- **Arrivals:** Poisson process at a set rate (`-rate`). Each request carries its
  *scheduled* arrival time, and latency is measured from that time. Queueing delay
  caused by GC therefore counts as latency, and a stalled generator does not hide
  load (no coordinated omission). A full queue drops the request and counts it.
- **Requests:** each request allocates a short-lived binary tree of 64-byte nodes
  with two pointers each (`-req` KiB), walks it `-work` times, and reads one entry of
  a long-lived cache. Unlike gcprobe's `[]byte` objects, these have pointers, so
  marking does real work (week 1 known issue).
- **Live heap:** the cache (`-cache` MiB of 4 KiB trees) is the long-lived part of the
  heap. A request replaces a random entry with probability `-churn`, so entries
  have a spread of lifetimes (mean about slots / (rate x churn)). An empty slot is
  filled on a miss, so a cache that grows in a new phase fills at the request rate.
- **Load patterns:** `-phases "8s:rate=2000,cache=64;8s:rate=8000,cache=256"` gives a
  phase change; adding `-loop` repeats the list until `-duration`, which gives bursts.

How the independent variables map to flags: allocation rate is about
`rate x req`; live heap is about `cache` plus in-flight requests; object lifetime
is bimodal (request-scoped vs. cache entries).

Why in-process rather than a separate HTTP client: it avoids network and
serialization noise in the latency numbers, and the generator does almost no
allocation, so it adds little GC work. The cost is that the generator shares the
runtime with the server: a stop-the-world pause also delays arrivals. Because
latency is measured from the scheduled time, such a delay still shows up as
request latency, which is what an external client would see.

## GC configurations compared

1. **Default**: `GOGC=100`, no memory limit
2. **Fixed GOGC**: 50, 100, 200, 400
3. **Memory limit**: `GOMEMLIMIT` at each budget, with default GOGC and with `GOGC=off`
4. **Best fixed**: the best configuration per workload found in week 3 (an oracle-style baseline)
5. **Adaptive controller** (week 4)

## Metrics

| Metric | Source |
|---|---|
| Throughput | Under open-loop load, completed rate equals the offered rate until saturation. Report **CPU per request** (non-idle CPU / completed requests) and the **saturation rate** (highest rate without queue growth or drops) instead. |
| Latency p50 / p99 / p99.9 | per-request timing from the scheduled arrival, log-linear histogram (at most 1.6% error), per interval and over the measured period |
| Memory: total runtime memory, live heap, heap goal | `runtime/metrics`: `/memory/classes/total:bytes`, `/gc/heap/live:bytes`, `/gc/heap/goal:bytes` |
| Process RSS | OS (`/proc/self/status` on Linux) |
| GC CPU fraction | `/cpu/classes/gc/total:cpu-seconds` divided by non-idle CPU (`total - idle`), so idle Ps do not dilute it. The week 1 definition (divided by all CPU) is kept as `gc_cpu_frac`. These metrics only advance at GC cycle ends, so per-interval values are 0 between cycles; use whole-run values. |
| GC assist fraction | `/cpu/classes/gc/mark/assist:cpu-seconds` / non-idle CPU (the latency mechanism from the Twitch post) |
| GC frequency | `/gc/cycles/total:gc-cycles`, cross-checked with `GODEBUG=gctrace=1` |

## Experimental control

- **Toolchain:** Go 1.27.3, pinned in `go.mod` (exact `go version` recorded per run). The Green Tea GC is the default since Go 1.26, so all main experiments use it. A comparison with `GOEXPERIMENT=nogreenteagc` is optional extra work, not part of the core.
- **Machine:** one fixed machine and OS (Linux or WSL2), on AC power, with other heavy programs closed. Record CPU model, core count, and RAM.
- **GOMAXPROCS:** fixed explicitly for every run (default 4). This matters for the GC CPU fraction: `/cpu/classes/total` counts all GOMAXPROCS, including idle time, so on a 32-thread machine with a light workload the fraction looks tiny (0.0002 in the day 1 smoke test). A small fixed GOMAXPROCS keeps the number meaningful and comparable across runs.
- **Warm-up:** the summary leaves out the first 5 s by default (`-warmup`). The cache
  is prefilled and a GC runs before the clock starts, so steady-state runs settle
  quickly. Phase-change runs need their own warm-up choice, since the live heap
  keeps growing after the switch.
- **Repetitions:** at least 5 runs per configuration; report median and 95% confidence interval.
- **Metadata:** every run writes Go version, flags, environment variables, and a timestamp next to its CSV.

## Week 1 validation

`cmd/gcprobe` checks that the knobs and metrics behave as expected before the real workload is built: with fixed allocation rate and lifetime, raising GOGC should reduce GC cycles and GC CPU while increasing memory, and a memory limit should cap total memory. Results: `results/week1/`.

## Open questions

- ~~Load generator: closed-loop or open-loop?~~ Decided in week 2: open-loop, in-process (see Workload).
- The p50 latency floor is about 0.35 ms on the Windows dev machine, independent of
  load. Is this timer or wake-up latency? Check before trusting sub-millisecond
  differences.
- Controller inputs: heap goal vs. limit, GC CPU fraction, latency feedback, or a combination?
