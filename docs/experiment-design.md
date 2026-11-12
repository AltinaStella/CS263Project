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
| Latency p50 / p99 / p99.9 | per-request timing from the scheduled arrival, log-linear histogram (at most 1.6% error), per interval and over the measured period. Two diagnostic splits: **service latency** (from the moment the generator actually queued the request) and **dispatch lag** (actual minus scheduled queue time). |
| Memory: total runtime memory, live heap, heap goal | `runtime/metrics`: `/memory/classes/total:bytes`, `/gc/heap/live:bytes`, `/gc/heap/goal:bytes` |
| Process RSS | OS (`/proc/self/status` on Linux) |
| GC CPU fraction | `/cpu/classes/gc/total:cpu-seconds` divided by non-idle CPU (`total - idle`), so idle Ps do not dilute it. The week 1 definition (divided by all CPU) is kept as `gc_cpu_frac`. These metrics only advance at GC cycle ends, so per-interval values are 0 between cycles; use whole-run values. |
| GC assist fraction | `/cpu/classes/gc/mark/assist:cpu-seconds` / non-idle CPU (the latency mechanism from the Twitch post) |
| GC demand fraction | `(GC - idle mark) / (non-idle - idle mark)`, using `/cpu/classes/gc/mark/idle:cpu-seconds`. Idle-priority mark workers run on Ps that have nothing else to do, so they do not take CPU from the program; this is the GC cost the program actually pays. Also reported as `demand_cpu_ms_per_request`. **Primary GC CPU metric** (see Calibration). |
| GC frequency | `/gc/cycles/total:gc-cycles`, cross-checked with `GODEBUG=gctrace=1` |

## Experimental control

- **Toolchain:** Go 1.27.3, pinned in `go.mod` (exact `go version` recorded per run). The Green Tea GC is the default since Go 1.26, so all main experiments use it. A comparison with `GOEXPERIMENT=nogreenteagc` is optional extra work, not part of the core.
- **Machine:** one fixed machine and OS (Linux or WSL2), on AC power, with other heavy programs closed. Record CPU model, core count, and RAM.
- **GOMAXPROCS:** fixed explicitly for every run (default 4). This matters for the GC CPU fraction: `/cpu/classes/total` counts all GOMAXPROCS, including idle time, so on a 32-thread machine with a light workload the fraction looks tiny (0.0002 in the day 1 smoke test). A small fixed GOMAXPROCS keeps the number meaningful and comparable across runs.
- **Warm-up:** the summary leaves out the first 5 s by default (`-warmup`). The cache
  is prefilled and a GC runs before the clock starts, so steady-state runs settle
  quickly. Phase-change runs need their own warm-up choice, since the live heap
  keeps growing after the switch.
- **Repetitions:** at least 5 runs per configuration; report the median with a
  distribution-free (order-statistic) confidence interval. With 5 runs this is
  [min, max] at 93.8% coverage; 95% needs at least 6 runs. `scripts/run_matrix.py`
  shuffles run order within each repetition (against machine drift) and gives all
  configs in a repetition the same seed (same arrival sequence).
- **Metadata:** every run writes Go version, flags, environment variables, and a
  timestamp next to its CSV. Each matrix also writes `run_info.json` with CPU model,
  RAM, OS, git commit (and whether the code was dirty), and the matrix itself.

## Week 1 validation

`cmd/gcprobe` checks that the knobs and metrics behave as expected before the real workload is built: with fixed allocation rate and lifetime, raising GOGC should reduce GC cycles and GC CPU while increasing memory, and a memory limit should cap total memory. Results: `results/week1/`.

## Open questions

- ~~Load generator: closed-loop or open-loop?~~ Decided in week 2: open-loop, in-process (see Workload).
- The p50 latency floor is about 0.35 ms on the Windows dev machine, independent of
  load. Is this timer or wake-up latency? Check before trusting sub-millisecond
  differences.
- Controller inputs: heap goal vs. limit, GC CPU fraction, latency feedback, or a combination?
- ~~Raise `-work` so GC is a smaller CPU share?~~ Decided in week 3: keep
  `-work 1`. At 30000 req/s, `-work 8` only raises demand CPU per request from
  14.7 to 19.6 us and lowers the GC demand share from 0.42 to 0.30; getting near a
  typical service would need `-work` in the tens and a new saturation calibration.
  The GC share is therefore high compared with real services; state this as a
  limitation in the report.
- ~~Why does dispatch lag rise during GC?~~ Answered in week 3 with
  `runtime/trace` (see "Dispatch lag and the GC mark phase" in Calibration).

## Dispatch lag and the GC mark phase (week 3, 11/12)

`gcserve -trace` records a `runtime/trace`; a small analyzer (built on
`golang.org/x/exp/trace`, kept outside the repo) measures how long the generator
goroutine waits between becoming runnable (timer fired) and running, and what
was running on the Ps at that moment. 30000 req/s, cache 128 MiB, 8 s run:

| GOGC | Wakes | Sched latency outside mark (p50 / p99) | During mark (p50 / p99) |
|---|---|---|---|
| 100 | 6273 | 61 / 245 us | 892 us / 13.7 ms |
| off | 7294 | 110 / 402 us | (no GC) |

With GOGC = 100, 93 of the 96 waits longer than 1 ms happened during the mark
phase, and in 95 of them at least one GC mark worker was running. The 13.7 ms p99
wait during marking matches the 12.5 ms p99 dispatch lag of the same run. So the
lag is the generator waiting for a P while mark workers occupy them. (All 4 Ps
were busy in only 53 of the 96 cases, so the wait is not only lack of a free P;
the exact scheduler path is not examined further.)

Consequence for the design: a goroutine reading requests from the network in a
real server would wait in the same way, so the in-process generator is kept and
latency stays measured from the scheduled arrival.

Side observation: GOGC = off *without* a memory limit grew to 8 GiB in 8 s and
service p99 rose to 72 ms (the cost of constantly mapping fresh memory), so it is
not a useful baseline on its own; the matrix uses GOGC = off only together with
GOMEMLIMIT.

## Calibration (11/4, Windows dev machine, GOMAXPROCS = 4, single runs)

Single 8-12 s runs, not yet repeated; the week 2 runner will redo these with
repetitions. Raw files are in `results/scratch/` (not committed).

**Latency floor.** `time.Sleep` wakes up late by about 0.35-0.55 ms at the median
and about 1 ms at p99 on Windows. WSL2 is worse: wake-ups snap to a tick of about
1.1 ms. With `GOGC=off`, measured latency is almost entirely this generator lag
(rate 2000: p50 0.34 / p99 0.95 ms total vs. 0.34 / 0.94 ms lag; service time
below 5 us). So **differences below about 1 ms at p50/p99 are timer noise**, on
either OS. GC effects show up from a few ms upward, so the scheduled-time
latency stays the main metric.

**GC delays dispatch, not just service.** With GOGC = 100 at 2000 req/s, p99.9 is
7.9 ms, of which dispatch lag is 7.4 ms and service time only 1.7 ms. At 30000
req/s, p99 is 14.9 ms with dispatch lag p99 12.4 ms. During a GC cycle the
generator goroutine is scheduled late. A network-reading goroutine in a real
server would be delayed the same way, which is why measuring from the scheduled
time matters. Assists are small here (2-6% of non-idle CPU at GOGC >= 100).

**Saturation** (GOGC = 100, cache 128 MiB): no queue build-up up to 60000 req/s
(p50 0.61 ms); at 70000, p50 jumps to 3.6 ms and the queue grows. Saturation is
about **65000 req/s**. Load levels for the experiments:

| Level | Rate | Share of saturation | Allocation rate (32 KiB/req) |
|---|---|---|---|
| low | 10000 req/s | ~15% | ~310 MiB/s |
| medium | 30000 req/s | ~45% | ~940 MiB/s |
| high | 50000 req/s | ~75% | ~1.5 GiB/s |

These allocation rates are higher than the week 1 plan (50/200/800 MiB/s); lower
`-req` if those levels are needed. Saturation at GOGC = 50 was not measured and is
probably lower, since CPU per request is higher.

**Live heap vs. cache size.** Peak live heap is the cache plus an overhead that
grows with both rate and cache size (objects allocated during marking count as
live for that cycle):

| Cache | Peak live @ 2000 req/s | Peak live @ 30000 req/s |
|---|---|---|
| 64 MiB | 66.5 MiB | 92.3 MiB |
| 256 MiB | 261.4 MiB | 309.7 MiB |
| 512 MiB | 517.9 MiB | 601.5 MiB |

So memory budgets must come from the peak live heap measured *at the same rate*,
not from `-cache`. Example: cache 128 MiB at 30000 req/s has a peak live heap of
about 150 MiB, so the 1.5x and 1.2x budgets are about 225 and 180 MiB. Note that
GOMEMLIMIT caps total runtime memory, not just the heap.

**GOGC still works as the cost model predicts** (30000 req/s, cache 128 MiB):

| GOGC | GC cycles (9 s) | p99 | CPU/req (incl. idle mark) | Peak memory |
|---|---|---|---|---|
| 50 | 125 | 20.0 ms | 0.058 ms | 260 MiB |
| 100 | 70 | 17.2 ms | 0.035 ms | 346 MiB |
| 200 | 37 | 15.1 ms | 0.019 ms | 455 MiB |
| 400 | 17 | 10.8 ms | 0.010 ms | 775 MiB |

(This sweep ran before idle marking was split out. A rerun of GOGC = 50 and 400
gave a demand CPU per request of 0.027 vs. 0.005 ms.)

**Why GC demand, not GC busy.** In that sweep, GC's share of non-idle CPU barely
moved (0.67 to 0.51) while CPU per request fell 5x. The reason: during each cycle,
idle-priority mark workers fill otherwise idle Ps, and they make up 36-44% of
non-idle CPU at this load. Leaving them out, GC's share is 0.45 at GOGC = 50 and
0.25 at GOGC = 400, which tracks the setting as expected.
