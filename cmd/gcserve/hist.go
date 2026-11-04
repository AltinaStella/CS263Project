package main

import (
	"math"
	"math/bits"
	"sync/atomic"
)

// Latency histogram over microseconds with log-linear buckets: values below
// 128 us are exact, larger values fall into 64 buckets per power of two, so a
// reported percentile is at most about 1.6% above the true value. Counters are
// atomic, so request goroutines record without locks or allocation.
const (
	histSubBits = 6
	histSub     = 1 << histSubBits // buckets per power of two
	histExact   = 2 * histSub      // values below this get their own bucket
	histMaxBits = 40               // values are clamped below 2^40 us
	histBuckets = histExact + (histMaxBits-histSubBits-1)*histSub
)

func bucketIndex(v uint64) int {
	if v < histExact {
		return int(v)
	}
	if v >= 1<<histMaxBits {
		v = 1<<histMaxBits - 1
	}
	n := bits.Len64(v)
	shift := n - histSubBits - 1
	return histExact + (n-histSubBits-2)*histSub + int(v>>shift) - histSub
}

// bucketUpper is the largest value that falls into bucket i.
func bucketUpper(i int) uint64 {
	if i < histExact {
		return uint64(i)
	}
	j := i - histExact
	shift := j/histSub + 1
	m := uint64(j%histSub + histSub)
	return (m+1)<<shift - 1
}

type hist struct {
	counts [histBuckets]atomic.Uint64
}

func (h *hist) record(us uint64) { h.counts[bucketIndex(us)].Add(1) }

// drainInto moves all counts into s and resets h. Each recorded value ends
// up in exactly one drain, even while recording continues.
func (h *hist) drainInto(s *histSnap) {
	for i := range h.counts {
		if c := h.counts[i].Swap(0); c != 0 {
			s.counts[i] += c
			s.n += c
		}
	}
}

type histSnap struct {
	counts [histBuckets]uint64
	n      uint64
}

func (s *histSnap) reset() { *s = histSnap{} }

func (s *histSnap) add(o *histSnap) {
	for i, c := range o.counts {
		s.counts[i] += c
	}
	s.n += o.n
}

// quantileMs returns the q-quantile in milliseconds (0 if empty).
func (s *histSnap) quantileMs(q float64) float64 {
	if s.n == 0 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(s.n)))
	if rank < 1 {
		rank = 1
	}
	var cum uint64
	for i, c := range s.counts {
		cum += c
		if cum >= rank {
			return float64(bucketUpper(i)) / 1000
		}
	}
	return s.maxMs()
}

func (s *histSnap) maxMs() float64 {
	for i := len(s.counts) - 1; i >= 0; i-- {
		if s.counts[i] != 0 {
			return float64(bucketUpper(i)) / 1000
		}
	}
	return 0
}

func (s *histSnap) summary() latencySummary {
	return latencySummary{
		P50: s.quantileMs(0.5), P90: s.quantileMs(0.9), P99: s.quantileMs(0.99),
		P999: s.quantileMs(0.999), Max: s.maxMs(),
	}
}
