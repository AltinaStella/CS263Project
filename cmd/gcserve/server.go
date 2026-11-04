package main

import (
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// node is the unit of allocation. It holds two pointers so the GC has real
// marking work, unlike gcprobe's pointer-free []byte. 64 bytes = one size class.
type node struct {
	left, right *node
	val         uint64
	_           [40]byte
}

const nodeSize = 64

func buildTree(n int, seed uint64) *node {
	if n <= 0 {
		return nil
	}
	l := (n - 1) / 2
	return &node{left: buildTree(l, 2*seed+1), right: buildTree(n-1-l, 2*seed+2), val: seed}
}

func sumTree(t *node) uint64 {
	if t == nil {
		return 0
	}
	return t.val + sumTree(t.left) + sumTree(t.right)
}

// cache is the long-lived part of the heap: a fixed array of slots, each
// holding a small tree. Requests overwrite random slots, so cache entries
// have a spread of lifetimes. Only the first `active` slots are used.
type cache struct {
	slots  []atomic.Pointer[node]
	active atomic.Int64
}

// resize changes the number of active slots. Shrinking drops the entries
// beyond the new size; growing leaves new slots empty, and requests fill them
// on a miss.
func (c *cache) resize(n int) {
	old := int(c.active.Swap(int64(n)))
	for i := n; i < old; i++ {
		c.slots[i].Store(nil)
	}
}

type server struct {
	start      time.Time
	cur        atomic.Pointer[phase]
	phaseIdx   atomic.Int64
	cache      cache
	entryKiB   int
	entryNodes int
	hist       hist // latency from scheduled arrival (main metric)
	svcHist    hist // latency from actual dispatch (excludes generator lag)
	lagHist    hist // dispatch time minus scheduled time
	dropped    atomic.Uint64
	sink       atomic.Uint64
}

// arrival is one queued request; times are nanoseconds since server.start.
type arrival struct {
	sched int64 // when the request should have arrived
	sent  int64 // when the generator actually queued it
}

func newServer(sch *schedule, entryKiB int) *server {
	s := &server{entryKiB: entryKiB, entryNodes: entryKiB << 10 / nodeSize}
	maxSlots := 0
	for _, p := range sch.phases {
		maxSlots = max(maxSlots, s.slotsFor(&p))
	}
	s.cache.slots = make([]atomic.Pointer[node], maxSlots)
	return s
}

func (s *server) slotsFor(p *phase) int { return p.cacheMiB << 10 / s.entryKiB }

// prefill fills the cache for phase p before the run starts, so the live heap
// starts near its steady-state size.
func (s *server) prefill(p *phase) {
	n := s.slotsFor(p)
	for i := range n {
		s.cache.slots[i].Store(buildTree(s.entryNodes, uint64(i)))
	}
	s.cache.active.Store(int64(n))
}

func (s *server) applyPhase(i int, p *phase) {
	s.cur.Store(p)
	s.cache.resize(s.slotsFor(p))
	s.phaseIdx.Store(int64(i))
}

// worker handles requests until the queue is closed.
func (s *server) worker(id int, seed uint64, queue <-chan arrival) {
	rng := rand.New(rand.NewPCG(seed, uint64(id)))
	var acc uint64
	for a := range queue {
		p := s.cur.Load()
		tree := buildTree(p.reqKiB<<10/nodeSize, rng.Uint64())
		for range p.work {
			acc += sumTree(tree)
		}
		if n := int(s.cache.active.Load()); n > 0 {
			slot := &s.cache.slots[rng.IntN(n)]
			if e := slot.Load(); e != nil {
				acc += sumTree(e)
			} else {
				slot.Store(buildTree(s.entryNodes, acc)) // miss: fill the slot
			}
			if rng.Float64() < p.churn {
				s.cache.slots[rng.IntN(n)].Store(buildTree(s.entryNodes, acc))
			}
		}
		now := time.Since(s.start).Nanoseconds()
		s.hist.record(uint64(max(now-a.sched, 0)) / 1000)
		s.svcHist.record(uint64(max(now-a.sent, 0)) / 1000)
	}
	s.sink.Add(acc)
}

// generate produces Poisson arrivals following sch until end, then returns.
// Arrivals are timestamped with their scheduled time; if the generator wakes
// up late, it catches up by emitting all overdue arrivals at once, so a stall
// (for example a GC pause) shows up as latency instead of as missing load.
// When the queue is full the request is dropped and counted.
//
// Timer wake-ups are coarse (about 0.4 ms late on Windows, about 1 ms under
// WSL2), so lagHist records how late each arrival was actually dispatched.
func (s *server) generate(sch *schedule, end time.Duration, seed uint64, queue chan<- arrival) {
	rng := rand.New(rand.NewPCG(seed, 0xa11ce))
	cur := -1
	next := time.Duration(0)
	emit := false // false when next is a phase boundary, not an arrival
	for next < end {
		if wait := next - time.Since(s.start); wait > 0 {
			time.Sleep(wait)
		}
		idx, phaseEnd := sch.at(next)
		if idx != cur {
			s.applyPhase(idx, &sch.phases[idx])
			cur = idx
		}
		if emit {
			sent := time.Since(s.start).Nanoseconds()
			select {
			case queue <- arrival{sched: int64(next), sent: sent}:
				s.lagHist.record(uint64(max(sent-int64(next), 0)) / 1000)
			default:
				s.dropped.Add(1)
			}
		}
		p := &sch.phases[idx]
		if p.rate <= 0 {
			next, emit = phaseEnd, false
			continue
		}
		// Inter-arrival times are memoryless, so a gap that crosses a phase
		// boundary is redrawn at the boundary with the new phase's rate.
		next += time.Duration(rng.ExpFloat64() / p.rate * float64(time.Second))
		emit = true
		if next >= phaseEnd {
			next, emit = phaseEnd, false
		}
	}
}
