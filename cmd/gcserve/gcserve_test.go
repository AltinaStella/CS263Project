package main

import (
	"math"
	"testing"
	"time"
)

func TestBucketBounds(t *testing.T) {
	prev := -1
	for _, v := range []uint64{0, 1, 127, 128, 129, 255, 256, 1000, 12345, 1 << 20, 987654321, 1<<40 - 1} {
		i := bucketIndex(v)
		if i < prev || i >= histBuckets {
			t.Fatalf("bucketIndex(%d) = %d, not monotonic or out of range", v, i)
		}
		prev = i
		up := bucketUpper(i)
		if up < v || float64(up-v) > float64(v)/histSub+1 {
			t.Errorf("v=%d: bucket upper %d is below v or more than 1/%d above it", v, up, histSub)
		}
		if i > 0 && bucketUpper(i-1) >= v {
			t.Errorf("v=%d also fits the previous bucket (upper %d)", v, bucketUpper(i-1))
		}
	}
}

func TestQuantile(t *testing.T) {
	var h hist
	for v := uint64(1); v <= 10000; v++ { // 1 us .. 10 ms, uniform
		h.record(v)
	}
	var s histSnap
	h.drainInto(&s)
	if s.n != 10000 {
		t.Fatalf("n = %d", s.n)
	}
	for _, q := range []float64{0.5, 0.99, 0.999} {
		want := q * 10 // ms
		if got := s.quantileMs(q); got < want || got > want*(1+1.0/histSub)+0.001 {
			t.Errorf("q%.3f = %.4f ms, want about %.4f", q, got, want)
		}
	}
	var empty histSnap
	h.drainInto(&empty)
	if empty.n != 0 {
		t.Errorf("drain did not reset the histogram")
	}
}

func TestParsePhases(t *testing.T) {
	base := phase{rate: 100, reqKiB: 32, cacheMiB: 64, work: 1, churn: 0.1}
	ph, err := parsePhases("2s:rate=500; 3s:cache=128,churn=0", base)
	if err != nil {
		t.Fatal(err)
	}
	if len(ph) != 2 || ph[0].rate != 500 || ph[0].cacheMiB != 64 ||
		ph[1].rate != 100 || ph[1].cacheMiB != 128 || ph[1].churn != 0 || ph[1].dur != 3*time.Second {
		t.Fatalf("unexpected phases: %v", ph)
	}
	for _, bad := range []string{"", "2s:rate", "x:rate=1", "1s:foo=1", "1s:churn=2", "0s:rate=1"} {
		if _, err := parsePhases(bad, base); err == nil {
			t.Errorf("parsePhases(%q) succeeded, want error", bad)
		}
	}
}

func TestScheduleAt(t *testing.T) {
	ph := []phase{{dur: 4 * time.Second}, {dur: time.Second}}
	s := time.Second
	loop := newSchedule(ph, true)
	for _, tc := range []struct {
		t       time.Duration
		idx     int
		phaseTo time.Duration
	}{{0, 0, 4 * s}, {4 * s, 1, 5 * s}, {5 * s, 0, 9 * s}, {9500 * time.Millisecond, 1, 10 * s}} {
		if i, e := loop.at(tc.t); i != tc.idx || e != tc.phaseTo {
			t.Errorf("loop.at(%v) = %d, %v; want %d, %v", tc.t, i, e, tc.idx, tc.phaseTo)
		}
	}
	once := newSchedule(ph, false)
	if i, e := once.at(7 * s); i != 1 || e != time.Duration(math.MaxInt64) {
		t.Errorf("once.at(7s) = %d, %v; want last phase without end", i, e)
	}
}
