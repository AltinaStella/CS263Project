# Reading Log

One row per reading session. Add a row only after actually reading the item.
Detailed notes go in [background.md](background.md).

| Date | Item (see readings/README.md) | Key takeaway for this project |
|---|---|---|
| 2026-10-28 | A. A Guide to the Go Garbage Collector | Target heap = live heap + (live heap + roots) * GOGC/100. Doubling GOGC roughly halves the number of GC cycles but doubles the extra memory; this is the trade-off RQ1 measures. |
| 2026-10-28 | B. Proposal 48409: Soft memory limit | GOMEMLIMIT is a soft cap on Go-managed memory. GC CPU is capped near 50%, so a limit set too low lets memory overshoot instead of causing a death spiral. A controller must not fight this cap. |
| 2026-10-28 | B. Proposal 44167: GC pacer redesign | The pacer decides when a cycle starts and how much assist work to request, but never changes the heap target. A controller has to act one level above it, on GOGC or the memory limit. |
| 2026-10-28 | D. Green Tea blog + Go 1.26 release notes | Marking scans 8 KiB pages instead of single objects (with AVX-512 where available), cutting GC time by 10-40%. Gains from tuning may be smaller than in pre-Green Tea reports such as Uber's. |
