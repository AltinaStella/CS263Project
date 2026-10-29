# Background and Related Work (week 1 notes)

> Working notes. Fill in each section while reading; each section should end with
> 2-3 sentences on what it means for this project. The full reading list is in
> [../readings/README.md](../readings/README.md); dated notes go in [reading-log.md](reading-log.md).

## 1. How the Go GC works

Source: GC guide (sections "Tracing Garbage Collection" and "The GC cycle").

- Go uses a **tracing, mark-sweep** collector. Marking starts from the roots
  (goroutine stacks and global variables) and follows pointers; everything not
  reached is swept and its memory reused.
- It is **non-moving**: objects are never relocated or compacted, so there is no
  copying cost, but fragmentation is handled by the size-class allocator instead.
- It is **concurrent**: marking runs alongside the program. A write barrier is on
  during marking so the program cannot hide a pointer from the collector. Only two
  short stop-the-world phases remain (turning the write barrier on and off).
- Values that do not escape are stack allocated and never touch the GC. Only heap
  allocations create GC work, so the **allocation rate** drives how often the GC runs.
- If the program allocates faster than marking can finish, allocating goroutines
  are asked to do some marking themselves (**GC assists**). Assists add latency to
  the goroutine that happens to allocate, which is one way GC shows up in p99.

**For this project:** GC cost appears in two places: background CPU (dedicated mark
workers) and foreground delay (assists, short pauses). Throughput measures the
first; p99 request latency is the main way to observe the second. The workload
must control allocation rate and object lifetime directly, which `gcprobe` does.

## 2. Tuning knobs

Sources: GC guide ("GOGC", "Memory limit"); proposal 48409.

- **GOGC** / `debug.SetGCPercent` (default 100). The GC guide defines
  `Target heap = Live heap + (Live heap + GC roots) * GOGC / 100`.
  With GOGC=100 the heap may grow to about 2x the live heap before the next cycle.
  Since Go 1.18 the roots (stacks, globals) are part of this formula.
- The guide's cost model:
  - `GC CPU per cycle = (Live heap + GC roots) * cost per byte + fixed cost`
  - `GC frequency = allocation rate / ((Live heap + GC roots) * GOGC / 100)`
  So doubling GOGC roughly halves the number of cycles (and GC CPU) at the price
  of roughly doubling the extra memory. This is the basic trade-off RQ1 measures.
- The total heap never goes below a 4 MiB minimum, so very small heaps collect
  less often than the formula suggests.
- **GOMEMLIMIT** / `debug.SetMemoryLimit` (Go 1.19+). A *soft* limit on total
  memory managed by the Go runtime (heap, stacks, runtime metadata; not memory
  outside Go such as C allocations). As memory nears the limit, the GC runs
  more often, even with `GOGC=off`.
- **`GOGC=off` + GOMEMLIMIT**: GC runs only when the limit is reached. Best
  throughput when the live heap is far below the limit, worst when it is close.
- **Death spiral / thrashing**: if the live heap grows close to the limit, the GC
  would run almost continuously. To prevent this, the runtime caps GC CPU at
  roughly 50% (measured over a window of about `2 * GOMAXPROCS` CPU-seconds) and
  lets memory exceed the limit instead. Proposal 48409 explains the choice of
  50%: in the worst case the program still keeps half its CPU.

**For this project:** GOGC sets memory *relative to* the live heap, and GOMEMLIMIT
sets an *absolute* cap. Neither looks at latency or at how the workload changes.
A controller has two dials to turn (`SetGCPercent`, `SetMemoryLimit`), and the
50% CPU cap is a safety net it must not fight. The memory budget levels in the
experiment design (1.5x and 1.2x of peak live heap) should be chosen so that the
tightest one gets near this thrashing region.

## 3. The pacer: what Go already adapts automatically

Source: proposal 44167 (GC pacer redesign, shipped in Go 1.18).

- Because the GC is concurrent, it must decide *when to start* a cycle so that
  marking finishes just as the heap reaches the target. This decision is the
  **pacer**. The old pacer was a proportional controller with many corner cases.
- The redesign:
  1. counts all GC work, including stacks and globals, not only the heap
     (fixes poor behavior with small heaps);
  2. reframes pacing as a search for the right trigger point, removing a steady
     offset error and allowing assists to be mostly off in steady state;
  3. bounds how far the heap can overshoot when the live heap jumps suddenly.
- The proposal states the gap clearly: with GOGC=100 Go needs about 2x the peak
  live heap, and GOGC alone does not make the GC work harder near a memory limit.
  Users worked around this with tricks like the Twitch "ballast". GOMEMLIMIT
  (proposal 48409) was the official answer.

**For this project:** the pacer already adapts *within* one GOGC setting: it
chooses when to start each cycle and how much assist work to request. It does not
change the target itself. So the controller should work one level above the
pacer: change GOGC or the memory limit when the workload phase changes (live
heap, allocation rate, latency). The pacer's own adaptation is part of every
baseline, so any gain from the controller is on top of it.

## 4. Green Tea GC (default in Go 1.26)

Sources: Go blog "The Green Tea Garbage Collector"; Go 1.26 release notes.

- Observation from the blog: about 90% of GC cost is marking, and a large part of
  marking time (at least about 35%) is spent stalled on memory, because the
  collector jumps between objects scattered across the heap.
- Green Tea scans **pages (spans) instead of single objects**. The work list holds
  8 KiB pages; when a page is taken off the list, all marked objects on it are
  scanned together, which gives better cache locality.
- Regular page-sized work also lets the GC use **vector instructions** (AVX-512 on
  newer x86 such as Intel Ice Lake and AMD Zen 4) for scanning.
- Reported effect: many workloads spend about 10% less time in GC, some up to 40%.
  The Go 1.26 release notes expect a 10-40% reduction in GC overhead. Green Tea is
  the default in Go 1.26; `GOEXPERIMENT=nogreenteagc` turns it off.
- As far as the blog describes, Green Tea changes **how fast** marking is, not
  **when** the GC runs: the pacer, GOGC, and GOMEMLIMIT keep the same meaning.
  The week 3 baselines will check whether the cost model still fits.

**For this project:** all experiments run on Go 1.27.3, so Green Tea is always on.
Lower marking cost shrinks the "cost per byte" term in the cost model, so the
benefit of tuning GOGC may be smaller than older blog posts (for example Uber's,
written before Green Tea) report. The experiment machine's CPU should be recorded,
since AVX-512 support changes GC cost. Comparing against `nogreenteagc` stays
optional.

## 5. Observability

- `runtime/metrics`, `GODEBUG=gctrace=1`, `runtime/trace`
- TODO: which metrics the controller can read cheaply at run time

## 6. Prior work on automatic GC tuning

- **Uber GOGCTuner**: adjusts GOGC from the memory limit of the container
- **Twitch memory ballast**: a large unused allocation to raise the heap target; mostly superseded by GOMEMLIMIT
- TODO: anything else found (papers on GC auto-tuning for the JVM, etc.)
- **How this project differs:** TODO

## References

1. A Guide to the Go Garbage Collector. https://go.dev/doc/gc-guide
2. Proposal: Soft memory limit (#48409). https://github.com/golang/proposal/blob/master/design/48409-soft-memory-limit.md
3. Proposal: GC pacer redesign (#44167). https://github.com/golang/proposal/blob/master/design/44167-gc-pacer-redesign.md
4. Go 1.26 Release Notes. https://go.dev/doc/go1.26
5. Package runtime/metrics. https://pkg.go.dev/runtime/metrics
6. Uber Engineering: How we saved 70K cores across 30 mission-critical services. https://www.uber.com/blog/how-we-saved-70k-cores-across-30-mission-critical-services/
7. Twitch: Go memory ballast. https://blog.twitch.tv/en/2019/04/10/go-memory-ballast-how-i-learnt-to-stop-worrying-and-love-the-heap/
8. The Go Blog: The Green Tea Garbage Collector. https://go.dev/blog/greenteagc
