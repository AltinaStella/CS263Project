package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// phase is one segment of the load schedule.
type phase struct {
	dur      time.Duration
	rate     float64 // request arrivals per second (Poisson)
	reqKiB   int     // short-lived allocation per request
	cacheMiB int     // nominal size of the long-lived cache
	work     int     // passes over the request tree (CPU per request)
	churn    float64 // probability that a request replaces a cache entry
}

func (p phase) String() string {
	return fmt.Sprintf("%s:rate=%g,req=%d,cache=%d,work=%d,churn=%g",
		p.dur, p.rate, p.reqKiB, p.cacheMiB, p.work, p.churn)
}

func (p phase) validate() error {
	switch {
	case p.dur <= 0:
		return fmt.Errorf("phase duration must be positive")
	case p.rate < 0, p.reqKiB < 0, p.cacheMiB < 0, p.work < 0:
		return fmt.Errorf("rate, req, cache and work must be >= 0")
	case p.churn < 0 || p.churn > 1:
		return fmt.Errorf("churn must be in [0, 1]")
	}
	return nil
}

// parsePhases parses a spec like "10s:rate=2000;20s:rate=6000,cache=256".
// Keys not given in a segment take their value from base (the command-line
// flags), not from the previous segment.
func parsePhases(spec string, base phase) ([]phase, error) {
	var out []phase
	for _, seg := range strings.Split(spec, ";") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		durStr, kvs, _ := strings.Cut(seg, ":")
		d, err := time.ParseDuration(strings.TrimSpace(durStr))
		if err != nil {
			return nil, fmt.Errorf("phase %q: %v", seg, err)
		}
		p := base
		p.dur = d
		if kvs = strings.TrimSpace(kvs); kvs != "" {
			for _, kv := range strings.Split(kvs, ",") {
				k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
				if !ok {
					return nil, fmt.Errorf("phase %q: expected key=value, got %q", seg, kv)
				}
				switch k {
				case "rate":
					p.rate, err = strconv.ParseFloat(v, 64)
				case "req":
					p.reqKiB, err = strconv.Atoi(v)
				case "cache":
					p.cacheMiB, err = strconv.Atoi(v)
				case "work":
					p.work, err = strconv.Atoi(v)
				case "churn":
					p.churn, err = strconv.ParseFloat(v, 64)
				default:
					err = fmt.Errorf("unknown key %q", k)
				}
				if err != nil {
					return nil, fmt.Errorf("phase %q: %v", seg, err)
				}
			}
		}
		if err := p.validate(); err != nil {
			return nil, fmt.Errorf("phase %q: %v", seg, err)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty phase spec")
	}
	return out, nil
}

type schedule struct {
	phases []phase
	starts []time.Duration // start of each phase within one cycle
	cycle  time.Duration   // sum of phase durations
	loop   bool            // repeat the phase list
}

func newSchedule(ph []phase, loop bool) *schedule {
	s := &schedule{phases: ph, loop: loop}
	for _, p := range ph {
		s.starts = append(s.starts, s.cycle)
		s.cycle += p.dur
	}
	return s
}

// at returns the index of the phase active at time t and the time it ends.
// Without looping, the last phase never ends.
func (s *schedule) at(t time.Duration) (int, time.Duration) {
	var base time.Duration
	if s.loop {
		base = t / s.cycle * s.cycle
		t -= base
	} else if t >= s.cycle {
		return len(s.phases) - 1, time.Duration(math.MaxInt64)
	}
	for i, p := range s.phases {
		if end := s.starts[i] + p.dur; t < end {
			return i, base + end
		}
	}
	return len(s.phases) - 1, base + s.cycle
}
