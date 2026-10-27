// Command gcprobe is a minimal allocation workload used to validate the
// measurement pipeline for the project.
//
// It allocates objects at a configurable rate and keeps each object alive for
// roughly a configurable lifetime (using a ring buffer of retained objects),
// so the live heap is approximately rate * lifetime. While it runs, it samples
// runtime/metrics at a fixed interval and writes one CSV row per sample.
//
// Example:
//
//	GOGC=100 go run ./cmd/gcprobe -rate 200 -lifetime 500ms -duration 10s -out results/probe_gogc100.csv
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"sync/atomic"
	"time"
)

// Metric names from the runtime/metrics package (available since Go 1.21).
const (
	mGCCycles   = "/gc/cycles/total:gc-cycles"
	mHeapLive   = "/gc/heap/live:bytes"
	mHeapGoal   = "/gc/heap/goal:bytes"
	mHeapAllocs = "/gc/heap/allocs:bytes"
	mHeapObjs   = "/memory/classes/heap/objects:bytes"
	mMemTotal   = "/memory/classes/total:bytes"
	mGCCPU      = "/cpu/classes/gc/total:cpu-seconds"
	mTotalCPU   = "/cpu/classes/total:cpu-seconds"
	mGOGC       = "/gc/gogc:percent"
	mMemLimit   = "/gc/gomemlimit:bytes"
)

var metricNames = []string{
	mGCCycles, mHeapLive, mHeapGoal, mHeapAllocs, mHeapObjs,
	mMemTotal, mGCCPU, mTotalCPU, mGOGC, mMemLimit,
}

type config struct {
	rateMBps float64       // allocation rate in MiB/s
	objSize  int           // bytes per object
	lifetime time.Duration // approximate time each object stays reachable
	duration time.Duration // total run time
	interval time.Duration // metrics sampling interval
	gogc     int           // if >= -1 and set, overrides GOGC (-1 = off)
	memLimit int64         // if > 0, overrides GOMEMLIMIT (bytes)
	out      string        // CSV output path, "-" for stdout
}

func parseFlags() config {
	var c config
	var gogc string
	var memLimitMB int64
	flag.Float64Var(&c.rateMBps, "rate", 100, "allocation rate in MiB/s")
	flag.IntVar(&c.objSize, "obj", 1024, "object size in bytes")
	flag.DurationVar(&c.lifetime, "lifetime", 500*time.Millisecond, "approximate object lifetime")
	flag.DurationVar(&c.duration, "duration", 10*time.Second, "total run time")
	flag.DurationVar(&c.interval, "interval", 100*time.Millisecond, "metrics sampling interval")
	flag.StringVar(&gogc, "gogc", "", "override GOGC (integer, or 'off'); empty = use environment")
	flag.Int64Var(&memLimitMB, "memlimit", 0, "override GOMEMLIMIT in MiB; 0 = use environment")
	flag.StringVar(&c.out, "out", "-", "CSV output file, '-' for stdout")
	flag.Parse()

	c.gogc = -2 // sentinel: not set
	switch gogc {
	case "":
	case "off":
		c.gogc = -1
	default:
		v, err := strconv.Atoi(gogc)
		if err != nil || v < 0 {
			fmt.Fprintf(os.Stderr, "invalid -gogc %q\n", gogc)
			os.Exit(2)
		}
		c.gogc = v
	}
	c.memLimit = memLimitMB << 20

	if c.rateMBps <= 0 || c.objSize <= 0 || c.lifetime <= 0 || c.interval <= 0 {
		fmt.Fprintln(os.Stderr, "rate, obj, lifetime and interval must be positive")
		os.Exit(2)
	}
	return c
}

// sink prevents the compiler from optimizing away allocations.
var sink atomic.Pointer[[]byte]

// allocate runs the allocation loop until stop is closed.
// Objects are stored in a ring of fixed size; overwriting a slot makes the
// previous object unreachable, so each object lives for about
// len(ring) / objectsPerSecond seconds.
func allocate(c config, stop <-chan struct{}, done chan<- uint64) {
	bytesPerSec := c.rateMBps * (1 << 20)
	objsPerSec := bytesPerSec / float64(c.objSize)
	ringLen := int(objsPerSec * c.lifetime.Seconds())
	if ringLen < 1 {
		ringLen = 1
	}
	ring := make([][]byte, ringLen)

	const tick = 5 * time.Millisecond
	start := time.Now()
	var allocated uint64
	idx := 0
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			done <- allocated
			return
		case <-t.C:
		}
		// Catch up to the target number of objects for the elapsed time,
		// which keeps the average rate correct even if a tick is delayed.
		target := uint64(objsPerSec * time.Since(start).Seconds())
		for allocated < target {
			b := make([]byte, c.objSize)
			b[0] = byte(allocated) // touch the memory
			ring[idx] = b
			idx++
			if idx == ringLen {
				idx = 0
			}
			allocated++
		}
		sink.Store(&ring[(idx+ringLen-1)%ringLen])
	}
}

func value(s metrics.Sample) float64 {
	switch s.Value.Kind() {
	case metrics.KindUint64:
		return float64(s.Value.Uint64())
	case metrics.KindFloat64:
		return s.Value.Float64()
	default:
		return 0
	}
}

func main() {
	c := parseFlags()

	if c.gogc != -2 {
		debug.SetGCPercent(c.gogc)
	}
	if c.memLimit > 0 {
		debug.SetMemoryLimit(c.memLimit)
	}

	var w io.Writer = os.Stdout
	if c.out != "-" {
		f, err := os.Create(c.out)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}
	cw := csv.NewWriter(w)
	defer cw.Flush()
	cw.Write([]string{
		"t_sec", "gc_cycles", "heap_live_mib", "heap_goal_mib", "heap_objects_mib",
		"mem_total_mib", "alloc_rate_mibps", "gc_cpu_frac", "gogc", "memlimit_mib",
	})

	samples := make([]metrics.Sample, len(metricNames))
	idx := map[string]int{}
	for i, n := range metricNames {
		samples[i].Name = n
		idx[n] = i
	}
	get := func(name string) float64 { return value(samples[idx[name]]) }

	stop := make(chan struct{})
	done := make(chan uint64, 1)
	go allocate(c, stop, done)

	const mib = 1 << 20
	start := time.Now()
	metrics.Read(samples)
	prevT := 0.0
	prevAlloc, prevGC, prevCPU := get(mHeapAllocs), get(mGCCPU), get(mTotalCPU)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	deadline := time.After(c.duration)
loop:
	for {
		select {
		case <-deadline:
			break loop
		case <-ticker.C:
		}
		metrics.Read(samples)
		t := time.Since(start).Seconds()
		alloc, gcCPU, totCPU := get(mHeapAllocs), get(mGCCPU), get(mTotalCPU)

		// The CPU-class metrics are estimates updated at GC boundaries, so the
		// per-interval fraction is noisy; whole-run aggregates are more reliable.
		gcFrac := 0.0
		if d := totCPU - prevCPU; d > 0 {
			gcFrac = (gcCPU - prevGC) / d
		}
		limit := get(mMemLimit)
		gogcStr := "off"
		if g := get(mGOGC); g >= 0 && g < 1e9 {
			gogcStr = strconv.Itoa(int(g))
		}
		limitStr := "inf"
		if limit < float64(1<<62) {
			limitStr = f2(limit / mib)
		}
		cw.Write([]string{
			f2(t),
			strconv.FormatUint(uint64(get(mGCCycles)), 10),
			f2(get(mHeapLive) / mib),
			f2(get(mHeapGoal) / mib),
			f2(get(mHeapObjs) / mib),
			f2(get(mMemTotal) / mib),
			f2((alloc - prevAlloc) / mib / (t - prevT)),
			strconv.FormatFloat(gcFrac, 'f', 4, 64),
			gogcStr,
			limitStr,
		})
		prevT, prevAlloc, prevGC, prevCPU = t, alloc, gcCPU, totCPU
	}
	close(stop)
	n := <-done

	metrics.Read(samples)
	totGC, totCPU := get(mGCCPU), get(mTotalCPU)
	frac := 0.0
	if totCPU > 0 {
		frac = totGC / totCPU
	}
	fmt.Fprintf(os.Stderr,
		"gcprobe: %s, GOMAXPROCS=%d, objects=%d, gc_cycles=%d, gc_cpu_frac=%.4f, final mem_total=%.1f MiB\n",
		runtime.Version(), runtime.GOMAXPROCS(0), n, uint64(get(mGCCycles)), frac, get(mMemTotal)/mib)
}

func f2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
