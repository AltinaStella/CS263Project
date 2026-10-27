# Background and Related Work (week 1 notes)

> Working notes. Fill in each section while reading; each section should end with
> 2-3 sentences on what it means for this project. The full reading list is in
> [../readings/README.md](../readings/README.md); dated notes go in [reading-log.md](reading-log.md).

## 1. How the Go GC works

- Concurrent, non-moving, tri-color mark-sweep collector
- Write barrier; stop-the-world phases are short
- TODO: notes

## 2. Tuning knobs

- **GOGC** / `debug.SetGCPercent`: heap growth target relative to the live heap
- **GOMEMLIMIT** / `debug.SetMemoryLimit` (Go 1.19+): soft memory limit; the GC runs more often as total memory nears the limit
- `GOGC=off` together with a memory limit
- TODO: the GC CPU cost model from the GC guide, and the "death spiral" risk when the limit is too low

## 3. The pacer: what Go already adapts automatically

- TODO: notes from the pacer redesign proposal

## 4. Green Tea GC (default in Go 1.26)

- TODO: what changed, and whether it affects how GOGC and GOMEMLIMIT behave

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
