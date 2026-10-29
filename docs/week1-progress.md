# Week 1 Progress - Background Research and Experimental Design

Reading list: [../readings/README.md](../readings/README.md). Reading log: [reading-log.md](reading-log.md).

## Done

- [x] Project repository set up: Go module, directory layout
- [x] `cmd/gcprobe`: allocation workload with configurable rate, object size, and lifetime; samples `runtime/metrics` to CSV
- [x] gcprobe smoke test (10/27, Go 1.27.3, Windows): `-rate 100 -lifetime 300ms -duration 2s` gave a live heap of about 31.5 MiB (expected 100 MiB/s x 0.3 s = 30 MiB), heap goal about 63 MiB (about 2x live heap, as GOGC=100 predicts), 9 GC cycles
- [x] `scripts/sweep_gogc.sh` and `scripts/plot_week1.py` for the GOGC sanity sweep
- [x] Draft research questions and experiment design (`docs/experiment-design.md`)
- [x] Reading list organized by topic, with local copies of the Go docs (`readings/`)
- [x] Background reading and notes, part 1: GC design, tuning knobs, pacer, Green Tea (`docs/background.md` sections 1-4)
- [ ] Background reading and notes, part 2: observability and prior work (sections 5-6)
- [ ] Sweep run on the experiment machine, with the figure added below

## Sanity check result

Setup: rate = 200 MiB/s, lifetime = 500 ms, GOGC in {50, 100, 200, 400}, GOMAXPROCS = 4.

TODO: insert `results/week1/week1_gogc_sweep.png` and a 2-3 sentence reading of it
(expected: higher GOGC -> fewer GC cycles and lower GC CPU, but higher memory).

Known issue: the smoke test reported a GC CPU fraction of 0.0002 with GOMAXPROCS = 32,
because the denominator includes idle CPU on all 32 Ps. The sweep now fixes GOMAXPROCS.

## Key takeaways from the reading

TODO

## Plan for week 2

- Turn gcprobe into a request-serving workload with per-request latency (p50/p99)
- Open-loop load generator with steady, bursty, and phase-change patterns
- Experiment runner that records metadata and repetitions
