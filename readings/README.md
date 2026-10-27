# Reading List

Readings for the project, grouped by topic. Each item says why it matters for this
project. Openly licensed Go docs have a local copy in [go-official/](go-official/)
(see [ATTRIBUTION.md](go-official/ATTRIBUTION.md)); papers and third-party posts are
linked only. Notes from each reading go in [../docs/background.md](../docs/background.md),
and a dated entry goes in [../docs/reading-log.md](../docs/reading-log.md).

Status: `[x]` read and noted, `[ ]` not yet. Priority: P1 = needed in week 1,
P2 = needed before the controller (week 4), P3 = background.

## A. Go GC fundamentals

- [ ] **P1** A Guide to the Go Garbage Collector - [local](go-official/gc-guide.html) / https://go.dev/doc/gc-guide
  The main reference. Explains the cost model (GC CPU vs. heap size), GOGC, the
  memory limit, and the "death spiral" risk. Background sections 1-2.
- [ ] **P3** Getting to Go: The Journey of Go's Garbage Collector (ISMM 2018 keynote) - [local](go-official/blog-getting-to-go-ismm-keynote.html) / https://go.dev/blog/ismmkeynote
  Why Go chose a concurrent, non-moving collector that favors latency.

## B. Tuning knobs and the pacer

- [ ] **P1** Proposal 48409: Soft memory limit - [local](go-official/proposal-48409-soft-memory-limit.md)
  Design of GOMEMLIMIT, including the GC CPU cap (about 50%) that limits the death
  spiral. Defines the knob the controller will adjust.
- [ ] **P1** Proposal 44167: GC pacer redesign - [local](go-official/proposal-44167-gc-pacer-redesign.md)
  What the runtime already adapts on its own. The controller must add something
  the pacer does not already do.
- [ ] **P3** Go 1.5 concurrent GC pacing (original design) - https://golang.org/s/go15gcpacing
  History of the pacer; read only if proposal 44167 is unclear.

## C. Observability

- [ ] **P1** Package runtime/metrics - [local](go-official/runtime-metrics-package.html)
  The metric names gcprobe samples, and the ones the controller will read.
- [ ] **P2** Package runtime/debug - [local](go-official/runtime-debug-package.html)
  `SetGCPercent` and `SetMemoryLimit`, the two calls the controller will make.
- [ ] **P2** `GODEBUG=gctrace=1` output format - see the "Environment Variables"
  section of https://pkg.go.dev/runtime . Used to cross-check GC cycle counts.

## D. Recent runtime changes

- [ ] **P1** The Green Tea Garbage Collector (Go blog) - [local](go-official/blog-greenteagc.html) / https://go.dev/blog/greenteagc
  The new default marking algorithm since Go 1.26. Need to know whether it changes
  how GOGC and GOMEMLIMIT behave.
- [ ] **P1** Go 1.26 release notes - [local](go-official/release-notes-go1.26.html) / https://go.dev/doc/go1.26
  Green Tea becomes the default; check runtime and GC sections.
- [ ] **P2** Go 1.27 release notes - [local](go-official/release-notes-go1.27.html) / https://go.dev/doc/go1.27
  The toolchain used for experiments (1.27.3). Check runtime changes such as
  size-specialized malloc that may affect allocation cost.

## E. Industry adaptive tuning (closest prior work)

- [ ] **P1** Uber: How we saved 70K cores across 30 mission-critical services - https://www.uber.com/blog/how-we-saved-70k-cores-across-30-mission-critical-services/
  GOGCTuner: sets GOGC dynamically from the container memory limit. The main
  baseline idea to compare with.
- [ ] **P2** Twitch: Go memory ballast - https://blog.twitch.tv/en/2019/04/10/go-memory-ballast-how-i-learnt-to-stop-worrying-and-love-the-heap/
  An early workaround for a heap target that is too small; mostly replaced by GOMEMLIMIT.
- [ ] **P2** bytedance gopkg `gctuner` - https://pkg.go.dev/github.com/bytedance/gopkg/util/gctuner
  Open-source GOGC tuner based on a memory threshold. Possible baseline for week 5.
- [ ] **P3** cch123/gogctuner - https://github.com/cch123/gogctuner
  Another open-source tuner in the same style as Uber's.

## F. Academic work on GC performance and tuning

- [ ] **P2** Blackburn, Cheng, McKinley. Myths and Realities: The Performance Impact of Garbage Collection. SIGMETRICS 2004. https://doi.org/10.1145/1005686.1005693
  Classic study of the space-time trade-off of GC; supports the RQ1 framing.
- [ ] **P2** Hertz, Berger. Quantifying the Performance of Garbage Collection vs. Explicit Memory Management. OOPSLA 2005. https://doi.org/10.1145/1094811.1094836
  Shows how GC cost depends on heap size relative to live data.
- [ ] **P2** Lengauer, Mossenbock. The Taming of the Shrew: Increasing Performance by Automatic Parameter Tuning for Java Garbage Collectors. ICPE 2014. https://doi.org/10.1145/2568088.2568091
  Automatic GC parameter tuning for the JVM; closest academic related work.
- [ ] **P2** Singer, Kovoor, Brown, Lujan. Garbage Collection Auto-Tuning for Java MapReduce on Multi-Cores. ISMM 2011. https://doi.org/10.1145/1993478.1993495
  Machine-learning-based GC tuning; another related approach.
- [ ] **P3** Cai, Blackburn, Bond, Maas. Distilling the Real Cost of Production Garbage Collectors. ISPASS 2022. https://arxiv.org/abs/2112.07880
  How to measure GC cost fairly; useful for the methodology of RQ3.
- [ ] **P3** Jones, Hosking, Moss. The Garbage Collection Handbook, 2nd ed. CRC Press, 2023.
  Reference for terminology (tri-color marking, write barriers, pacing).

## G. Measurement methodology

- [ ] **P2** Dean, Barroso. The Tail at Scale. CACM 2013. https://doi.org/10.1145/2408776.2408794
  Why p99 latency matters; motivates the tail latency metric.
- [ ] **P2** Gil Tene. How NOT to Measure Latency (talk on coordinated omission). https://www.youtube.com/watch?v=lJ8ydIuPFeU
  Why the week 2 load generator must be open-loop.
