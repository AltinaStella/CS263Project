// Command gcserve is the request-serving workload for the project (week 2).
//
// It runs an in-process server driven by an open-loop load generator.
// Requests arrive as a Poisson process and are handled by a pool of worker
// goroutines. Each request allocates a short-lived, pointer-rich tree (so the
// GC has real marking work), walks it, reads an entry of a long-lived cache,
// and sometimes replaces a cache entry. The cache size sets the long-lived
// part of the live heap.
//
// Latency is measured from each request's scheduled arrival time, not from
// when a worker picks it up, so queueing delay caused by GC is included and
// the measurement avoids coordinated omission.
//
// The load can change over time with -phases; -loop repeats the phase list
// until -duration, which gives bursty load:
//
//	gcserve -phases "10s:rate=2000;10s:rate=6000,cache=256"       # phase change
//	gcserve -phases "4s:rate=1000;1s:rate=8000" -loop -duration 40s # bursts
//
// Output: a CSV with one row per -interval (latency percentiles and runtime
// metrics) and a JSON summary of the run after -warmup.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"time"
)

type config struct {
	sched    *schedule
	total    time.Duration
	entryKiB int
	warmup   time.Duration
	interval time.Duration
	workers  int
	queueCap int
	seed     uint64
	gogc     int   // -2 = not set, -1 = off
	memLimit int64 // bytes, 0 = not set
	out      string
	summary  string
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "gcserve: "+format+"\n", a...)
	os.Exit(2)
}

func parseFlags() config {
	var c config
	var base phase
	var phases, gogc string
	var loop bool
	var memLimitMiB int64
	flag.Float64Var(&base.rate, "rate", 2000, "request arrival rate (requests/s, Poisson)")
	flag.IntVar(&base.reqKiB, "req", 32, "short-lived allocation per request, KiB")
	flag.IntVar(&base.cacheMiB, "cache", 128, "long-lived cache size, MiB (nominal)")
	flag.IntVar(&base.work, "work", 1, "passes over the request tree (CPU per request)")
	flag.Float64Var(&base.churn, "churn", 0.05, "probability that a request replaces a cache entry")
	flag.DurationVar(&base.dur, "duration", 30*time.Second, "run length (single phase, or with -loop)")
	flag.StringVar(&phases, "phases", "", `phase list, e.g. "10s:rate=2000;10s:rate=6000,cache=256"`)
	flag.BoolVar(&loop, "loop", false, "repeat -phases until -duration")
	flag.IntVar(&c.entryKiB, "entry", 4, "size of one cache entry, KiB")
	flag.DurationVar(&c.warmup, "warmup", 5*time.Second, "initial period left out of the summary")
	flag.DurationVar(&c.interval, "interval", 500*time.Millisecond, "sampling interval for the CSV")
	flag.IntVar(&c.workers, "workers", 32, "worker goroutines")
	flag.IntVar(&c.queueCap, "queue", 100000, "request queue capacity; arrivals beyond it are dropped")
	flag.Uint64Var(&c.seed, "seed", 1, "random seed")
	flag.StringVar(&gogc, "gogc", "", "override GOGC (integer or 'off'); empty = use environment")
	flag.Int64Var(&memLimitMiB, "memlimit", 0, "override GOMEMLIMIT in MiB; 0 = use environment")
	flag.StringVar(&c.out, "out", "results/gcserve.csv", "per-interval CSV, '-' for stdout")
	flag.StringVar(&c.summary, "summary", "", "summary JSON path (default: -out with .json)")
	flag.Parse()

	if err := base.validate(); err != nil {
		fail("%v", err)
	}
	ph := []phase{base}
	if phases != "" {
		var err error
		if ph, err = parsePhases(phases, base); err != nil {
			fail("%v", err)
		}
	}
	c.sched = newSchedule(ph, loop)
	c.total = c.sched.cycle
	if loop {
		c.total = base.dur
	}
	if c.entryKiB <= 0 || c.workers <= 0 || c.queueCap <= 0 || c.interval <= 0 {
		fail("entry, workers, queue and interval must be positive")
	}

	c.gogc = -2
	switch gogc {
	case "":
	case "off":
		c.gogc = -1
	default:
		v, err := strconv.Atoi(gogc)
		if err != nil || v < 0 {
			fail("invalid -gogc %q", gogc)
		}
		c.gogc = v
	}
	c.memLimit = memLimitMiB << 20
	if c.summary == "" && c.out != "-" {
		c.summary = strings.TrimSuffix(c.out, filepath.Ext(c.out)) + ".json"
	}
	return c
}

// rtSample holds the runtime/metrics values gcserve records.
type rtSample struct {
	gcCycles, heapLive, heapGoal, heapAllocs, memTotal float64
	cpuTotal, cpuIdle, cpuGC, cpuAssist                float64
	gogc, memLimit                                     float64
	cpuIdleMark                                        float64
}

var rtNames = []string{
	"/gc/cycles/total:gc-cycles",
	"/gc/heap/live:bytes",
	"/gc/heap/goal:bytes",
	"/gc/heap/allocs:bytes",
	"/memory/classes/total:bytes",
	"/cpu/classes/total:cpu-seconds",
	"/cpu/classes/idle:cpu-seconds",
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/gc/mark/assist:cpu-seconds",
	"/gc/gogc:percent",
	"/gc/gomemlimit:bytes",
	"/cpu/classes/gc/mark/idle:cpu-seconds",
}

type rtReader struct{ samples []metrics.Sample }

func newRTReader() *rtReader {
	r := &rtReader{samples: make([]metrics.Sample, len(rtNames))}
	for i, n := range rtNames {
		r.samples[i].Name = n
	}
	return r
}

func (r *rtReader) read() rtSample {
	metrics.Read(r.samples)
	v := make([]float64, len(r.samples))
	for i, s := range r.samples {
		switch s.Value.Kind() {
		case metrics.KindUint64:
			v[i] = float64(s.Value.Uint64())
		case metrics.KindFloat64:
			v[i] = s.Value.Float64()
		}
	}
	return rtSample{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9], v[10], v[11]}
}

// busy is CPU time spent on anything but idling; it is the denominator for
// GC fractions, so idle Ps (see week 1 known issue) do not dilute them.
func (s rtSample) busy() float64 { return s.cpuTotal - s.cpuIdle }

// Idle-priority mark workers run GC on Ps that would otherwise be idle, so
// their time is not taken from the program. demand() is non-idle CPU minus
// that time, and gcDemand() is the GC CPU the program actually paid for.
func (s rtSample) demand() float64   { return s.busy() - s.cpuIdleMark }
func (s rtSample) gcDemand() float64 { return s.cpuGC - s.cpuIdleMark }

func frac(num, den float64) float64 {
	if den <= 0 {
		return 0
	}
	return num / den
}

const mib = 1 << 20

func gogcString(v float64) string {
	if v < 0 || v >= 1e9 {
		return "off"
	}
	return strconv.Itoa(int(v))
}

func limitString(v float64) string {
	if v >= float64(1<<62) {
		return "inf"
	}
	return f(v/mib, 1)
}

func f(v float64, prec int) string { return strconv.FormatFloat(v, 'f', prec, 64) }

type latencySummary struct {
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
	P999 float64 `json:"p999"`
	Max  float64 `json:"max"`
}

type summary struct {
	Date        string            `json:"date"`
	GoVersion   string            `json:"go_version"`
	OS          string            `json:"os"`
	NumCPU      int               `json:"num_cpu"`
	GOMAXPROCS  int               `json:"gomaxprocs"`
	Env         map[string]string `json:"env"`
	Args        []string          `json:"args"`
	Phases      []string          `json:"phases"`
	Loop        bool              `json:"loop"`
	WarmupSec   float64           `json:"warmup_sec"`
	MeasuredSec float64           `json:"measured_sec"`

	Completed     uint64         `json:"completed"`
	Dropped       uint64         `json:"dropped"`
	ThroughputRPS float64        `json:"throughput_rps"`
	LatencyMs     latencySummary `json:"latency_ms"`
	ServiceMs     latencySummary `json:"service_ms"`      // from dispatch, no generator lag
	DispatchLagMs latencySummary `json:"dispatch_lag_ms"` // generator lateness

	GCCycles        uint64  `json:"gc_cycles"`
	GCCPUFrac       float64 `json:"gc_cpu_frac"`         // GC / all CPU incl. idle (as in week 1)
	GCBusyFrac      float64 `json:"gc_busy_frac"`        // GC / non-idle CPU
	AssistBusyFrac  float64 `json:"assist_busy_frac"`    // mark assists / non-idle CPU
	CPUMsPerRequest float64 `json:"cpu_ms_per_request"`  // non-idle CPU / completed requests
	IdleMarkFrac    float64 `json:"idle_mark_busy_frac"` // idle-priority marking / non-idle CPU
	// Same as gc_busy_frac and cpu_ms_per_request, but leaving out idle marking.
	GCDemandFrac       float64 `json:"gc_demand_frac"`
	DemandMsPerRequest float64 `json:"demand_cpu_ms_per_request"`
	PeakMemTotalMiB    float64 `json:"peak_mem_total_mib"`
	MeanMemTotalMiB    float64 `json:"mean_mem_total_mib"`
	PeakHeapLiveMiB    float64 `json:"peak_heap_live_mib"`
	FinalGOGC          string  `json:"final_gogc"`
	FinalLimitMiB      string  `json:"final_memlimit_mib"`
}

func main() {
	c := parseFlags()
	if c.gogc != -2 {
		debug.SetGCPercent(c.gogc)
	}
	if c.memLimit > 0 {
		debug.SetMemoryLimit(c.memLimit)
	}

	w := os.Stdout
	if c.out != "-" {
		if err := os.MkdirAll(filepath.Dir(c.out), 0o755); err != nil {
			fail("%v", err)
		}
		file, err := os.Create(c.out)
		if err != nil {
			fail("%v", err)
		}
		defer file.Close()
		w = file
	}
	cw := csv.NewWriter(w)
	cw.Write([]string{
		"t_sec", "phase", "offered_rps", "done_rps", "dropped", "queue_len",
		"p50_ms", "p99_ms", "p999_ms", "max_ms",
		"svc_p50_ms", "svc_p99_ms", "lag_p50_ms", "lag_p99_ms",
		"gc_cycles", "heap_live_mib", "heap_goal_mib", "mem_total_mib", "alloc_rate_mibps",
		"gc_cpu_frac", "gc_busy_frac", "assist_busy_frac", "idle_mark_busy_frac", "gc_demand_frac",
		"gogc", "memlimit_mib",
	})

	s := newServer(c.sched, c.entryKiB)
	s.prefill(&c.sched.phases[0])
	s.applyPhase(0, &c.sched.phases[0])
	runtime.GC() // start from a collected heap that holds only the cache

	rt := newRTReader()
	queue := make(chan arrival, c.queueCap)
	var wg sync.WaitGroup
	s.start = time.Now()
	for i := range c.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.worker(i, c.seed, queue)
		}()
	}
	done := make(chan struct{})
	go func() {
		s.generate(c.sched, c.total, c.seed, queue)
		close(queue)
		wg.Wait()
		close(done)
	}()

	// Index 0 = latency from scheduled arrival, 1 = from dispatch, 2 = dispatch lag.
	srcs := [3]*hist{&s.hist, &s.svcHist, &s.lagHist}
	iv, total := new([3]histSnap), new([3]histSnap)
	addAll := func() {
		for k := range total {
			total[k].add(&iv[k])
		}
	}
	prev, prevT, prevDropped := rt.read(), 0.0, uint64(0)
	// warm is the runtime state when the measured period starts.
	var warm *rtSample
	var warmT float64
	var warmDropped uint64
	if c.warmup <= 0 {
		warm = &prev
	}
	var peakMem, sumMem, peakLive float64
	var nIntervals int

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for running := true; running; {
		select {
		case <-ticker.C:
		case <-done:
			running = false
		}
		t := time.Since(s.start).Seconds()
		for k := range iv {
			iv[k].reset()
			srcs[k].drainInto(&iv[k])
		}
		if !running && t-prevT < c.interval.Seconds()/10 {
			// The run ended just after a tick: fold the last few requests into
			// the summary instead of writing a near-empty CSV row.
			if warm != nil {
				addAll()
			}
			break
		}
		cur := rt.read()
		dropped := s.dropped.Load()
		dt := t - prevT
		p := s.cur.Load()

		cw.Write([]string{
			f(t, 3), strconv.Itoa(int(s.phaseIdx.Load())), f(p.rate, 0),
			f(frac(float64(iv[0].n), dt), 1), strconv.FormatUint(dropped-prevDropped, 10),
			strconv.Itoa(len(queue)),
			f(iv[0].quantileMs(0.5), 3), f(iv[0].quantileMs(0.99), 3), f(iv[0].quantileMs(0.999), 3), f(iv[0].maxMs(), 3),
			f(iv[1].quantileMs(0.5), 3), f(iv[1].quantileMs(0.99), 3),
			f(iv[2].quantileMs(0.5), 3), f(iv[2].quantileMs(0.99), 3),
			strconv.FormatUint(uint64(cur.gcCycles), 10),
			f(cur.heapLive/mib, 2), f(cur.heapGoal/mib, 2), f(cur.memTotal/mib, 2),
			f(frac((cur.heapAllocs-prev.heapAllocs)/mib, dt), 1),
			f(frac(cur.cpuGC-prev.cpuGC, cur.cpuTotal-prev.cpuTotal), 4),
			f(frac(cur.cpuGC-prev.cpuGC, cur.busy()-prev.busy()), 4),
			f(frac(cur.cpuAssist-prev.cpuAssist, cur.busy()-prev.busy()), 4),
			f(frac(cur.cpuIdleMark-prev.cpuIdleMark, cur.busy()-prev.busy()), 4),
			f(frac(cur.gcDemand()-prev.gcDemand(), cur.demand()-prev.demand()), 4),
			gogcString(cur.gogc), limitString(cur.memLimit),
		})

		switch {
		case warm == nil && t >= c.warmup.Seconds():
			w := cur
			warm, warmT, warmDropped = &w, t, dropped
		case warm != nil:
			addAll()
			peakMem = max(peakMem, cur.memTotal)
			peakLive = max(peakLive, cur.heapLive)
			sumMem += cur.memTotal
			nIntervals++
		}
		prev, prevT, prevDropped = cur, t, dropped
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		fail("writing CSV: %v", err)
	}

	if warm == nil || nIntervals == 0 {
		fail("run ended before the warm-up period (%s); no summary written", c.warmup)
	}
	end := prev
	sum := summary{
		Date:        time.Now().UTC().Format(time.RFC3339),
		GoVersion:   runtime.Version(),
		OS:          runtime.GOOS + "/" + runtime.GOARCH,
		NumCPU:      runtime.NumCPU(),
		GOMAXPROCS:  runtime.GOMAXPROCS(0),
		Env:         map[string]string{},
		Args:        os.Args[1:],
		Loop:        c.sched.loop,
		WarmupSec:   warmT,
		MeasuredSec: prevT - warmT,

		Completed:          total[0].n,
		Dropped:            s.dropped.Load() - warmDropped,
		LatencyMs:          total[0].summary(),
		ServiceMs:          total[1].summary(),
		DispatchLagMs:      total[2].summary(),
		GCCycles:           uint64(end.gcCycles - warm.gcCycles),
		GCCPUFrac:          frac(end.cpuGC-warm.cpuGC, end.cpuTotal-warm.cpuTotal),
		GCBusyFrac:         frac(end.cpuGC-warm.cpuGC, end.busy()-warm.busy()),
		AssistBusyFrac:     frac(end.cpuAssist-warm.cpuAssist, end.busy()-warm.busy()),
		CPUMsPerRequest:    frac((end.busy()-warm.busy())*1000, float64(total[0].n)),
		IdleMarkFrac:       frac(end.cpuIdleMark-warm.cpuIdleMark, end.busy()-warm.busy()),
		GCDemandFrac:       frac(end.gcDemand()-warm.gcDemand(), end.demand()-warm.demand()),
		DemandMsPerRequest: frac((end.demand()-warm.demand())*1000, float64(total[0].n)),
		PeakMemTotalMiB:    peakMem / mib,
		MeanMemTotalMiB:    sumMem / float64(nIntervals) / mib,
		PeakHeapLiveMiB:    peakLive / mib,
		FinalGOGC:          gogcString(end.gogc),
		FinalLimitMiB:      limitString(end.memLimit),
	}
	sum.ThroughputRPS = frac(float64(total[0].n), sum.MeasuredSec)
	for _, p := range c.sched.phases {
		sum.Phases = append(sum.Phases, p.String())
	}
	for _, k := range []string{"GOGC", "GOMEMLIMIT", "GOMAXPROCS", "GOEXPERIMENT", "GODEBUG"} {
		if v, ok := os.LookupEnv(k); ok {
			sum.Env[k] = v
		}
	}
	if c.summary != "" {
		js, _ := json.MarshalIndent(sum, "", "  ")
		if err := os.WriteFile(c.summary, append(js, '\n'), 0o644); err != nil {
			fail("%v", err)
		}
	}
	fmt.Fprintf(os.Stderr,
		"gcserve: %s GOMAXPROCS=%d | %.0f req/s, dropped %d | p50 %.2f p99 %.2f p99.9 %.2f max %.2f ms | "+
			"svc p50 %.2f p99 %.2f | lag p50 %.2f p99 %.2f | "+
			"gc %d cycles, gc_busy %.3f (idle mark %.3f, demand %.3f), assist_busy %.3f, %.4f cpu-ms/req (demand %.4f) | "+
			"peak mem %.1f MiB, live %.1f MiB\n",
		sum.GoVersion, sum.GOMAXPROCS, sum.ThroughputRPS, sum.Dropped,
		sum.LatencyMs.P50, sum.LatencyMs.P99, sum.LatencyMs.P999, sum.LatencyMs.Max,
		sum.ServiceMs.P50, sum.ServiceMs.P99, sum.DispatchLagMs.P50, sum.DispatchLagMs.P99,
		sum.GCCycles, sum.GCBusyFrac, sum.IdleMarkFrac, sum.GCDemandFrac, sum.AssistBusyFrac,
		sum.CPUMsPerRequest, sum.DemandMsPerRequest,
		sum.PeakMemTotalMiB, sum.PeakHeapLiveMiB)
}
