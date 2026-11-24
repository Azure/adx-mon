// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
)

// defaultSlotIncreaseAfter is the number of consecutive successful requests before the adaptive limit is raised by
// one slot.
const defaultSlotIncreaseAfter = 10

// slotShare returns this node's share of a streaming budget divided across peers.  Each peer receives budget/count
// slots and the remainder is given to the lowest ranked peers so the shares of all peers sum to the budget.  The
// share is clamped to [minSlots, maxSlots]; maxSlots <= 0 means no maximum.
func slotShare(budget int, peers cluster.PeerInfo, minSlots, maxSlots int) int {
	count := max(1, peers.Count)
	share := budget / count
	if peers.Rank < budget%count {
		share++
	}
	if maxSlots > 0 {
		share = min(share, maxSlots)
	}
	return max(share, minSlots)
}

// SlotStats describes the state of a StreamingSlots pool.
type SlotStats struct {
	// Budget is the total number of concurrent streaming requests shared by all peers.
	Budget int
	// Peers is the number of peers sharing the budget.
	Peers int
	// Share is this node's share of the budget.
	Share int
	// Limit is the current adaptive limit, at most Share.
	Limit int
	// InUse is the number of slots currently held.
	InUse int
	// Waiting is the number of prepared batches waiting for a slot.
	Waiting int
	// Overcommitted is true when the minimum slots of all peers exceeds the budget.
	Overcommitted bool
}

// StreamingSlots limits concurrent streaming ingestion requests to a Kusto endpoint.  The endpoint's budget is
// divided across ingestor peers and the limit adapts to throttling.
type StreamingSlots struct {
	budget, minSlots, maxSlots int
	increaseAfter              int

	mu        sync.Mutex
	peers     cluster.PeerInfo
	share     int
	limit     int
	inUse     int
	successes int
	// waiters are ordered by oldest WAL segment, then by arrival for equal ages.
	waiters []*slotWaiter
}

type slotWaiter struct {
	oldest time.Time
	ready  chan struct{}
}

// NewStreamingSlots returns a pool for budget concurrent requests shared by peers.  At least one slot is always
// available so requests can make progress.
func NewStreamingSlots(budget, minSlots, maxSlots int, peers cluster.PeerInfo) *StreamingSlots {
	minSlots = max(1, minSlots)
	s := &StreamingSlots{
		budget:        budget,
		minSlots:      minSlots,
		maxSlots:      maxSlots,
		increaseAfter: defaultSlotIncreaseAfter,
	}
	s.peers = peers
	s.share = slotShare(budget, peers, minSlots, maxSlots)
	s.limit = s.share
	return s
}

// SetPeers updates the peers sharing the budget.  When the share grows, the limit grows by the same amount.  When it
// shrinks, requests in flight are allowed to finish but no new slots are granted until usage is below the new limit.
func (s *StreamingSlots) SetPeers(peers cluster.PeerInfo) {
	s.mu.Lock()
	share := slotShare(s.budget, peers, s.minSlots, s.maxSlots)
	s.peers = peers
	if share > s.share {
		s.limit += share - s.share
	}
	s.share = share
	s.limit = min(s.limit, share)
	overcommitted := s.overcommittedLocked()
	s.grantWaitersLocked()
	s.mu.Unlock()

	if overcommitted {
		logger.Warnf("Streaming budget %d is overcommitted: %d peers with at least %d slots each", s.budget, peers.Count, s.minSlots)
	}
}

// Acquire waits for a slot.  The returned func must be called when the request completes and reports whether the
// request was throttled.
func (s *StreamingSlots) Acquire(ctx context.Context) (release func(throttled bool), err error) {
	if err := s.acquire(ctx, time.Now()); err != nil {
		return nil, err
	}
	return s.releaseFunc(), nil
}

func (s *StreamingSlots) acquire(ctx context.Context, oldest time.Time) error {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.inUse < s.limit {
		s.inUse++
		s.mu.Unlock()
		return nil
	}

	w := &slotWaiter{oldest: oldest, ready: make(chan struct{})}
	i := sort.Search(len(s.waiters), func(i int) bool { return oldest.Before(s.waiters[i].oldest) })
	s.waiters = slices.Insert(s.waiters, i, w)
	s.mu.Unlock()

	select {
	case <-w.ready:
	case <-ctx.Done():
	}
	if err := ctx.Err(); err != nil {
		s.mu.Lock()
		if i := slices.Index(s.waiters, w); i >= 0 {
			s.waiters = slices.Delete(s.waiters, i, i+1)
		} else {
			// Cancellation raced with a grant. Pass the unused slot to the next batch.
			s.inUse--
			s.grantWaitersLocked()
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

// TryAcquire returns a slot if one is available without waiting.
func (s *StreamingSlots) TryAcquire() (release func(throttled bool), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inUse >= s.limit {
		return nil, false
	}
	s.inUse++
	return s.releaseFunc(), true
}

func (s *StreamingSlots) releaseFunc() func(throttled bool) {
	var once sync.Once
	return func(throttled bool) {
		once.Do(func() { s.release(true, throttled) })
	}
}

func (s *StreamingSlots) release(attempted, throttled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.inUse--
	if throttled {
		s.successes = 0
		s.limit = max(1, s.limit/2)
	} else if attempted && s.limit < s.share {
		s.successes++
		if s.successes >= s.increaseAfter {
			s.successes = 0
			s.limit++
		}
	}
	s.grantWaitersLocked()
}

// grantWaitersLocked reserves available slots before waking the selected workers, so new arrivals cannot bypass them.
func (s *StreamingSlots) grantWaitersLocked() {
	for s.inUse < s.limit && len(s.waiters) > 0 {
		w := s.waiters[0]
		s.waiters[0] = nil
		s.waiters = s.waiters[1:]
		s.inUse++
		close(w.ready)
	}
}

func (s *StreamingSlots) overcommittedLocked() bool {
	return max(1, s.peers.Count)*s.minSlots > s.budget
}

// Stats returns the current state of the pool.
func (s *StreamingSlots) Stats() SlotStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SlotStats{
		Budget:        s.budget,
		Peers:         max(1, s.peers.Count),
		Share:         s.share,
		Limit:         s.limit,
		InUse:         s.inUse,
		Waiting:       len(s.waiters),
		Overcommitted: s.overcommittedLocked(),
	}
}

// streamingSlotsCollector reports the state of streaming slot pools when metrics are scraped so acquiring and
// releasing slots does not update metrics.
type streamingSlotsCollector struct {
	slots         map[string]*StreamingSlots
	slotsDesc     *prometheus.Desc
	overcommitted *prometheus.Desc
}

// NewStreamingSlotsCollector returns a collector for streaming slot pools keyed by Kusto endpoint.
func NewStreamingSlotsCollector(slots map[string]*StreamingSlots) prometheus.Collector {
	return &streamingSlotsCollector{
		slots: slots,
		slotsDesc: prometheus.NewDesc(
			prometheus.BuildFQName(metrics.Namespace, "ingestor", "realtime_streaming_slots"),
			"Streaming ingestion slots for a Kusto endpoint by state: budget, peers, share, limit, in_use and waiting",
			[]string{"endpoint", "state"}, nil),
		overcommitted: prometheus.NewDesc(
			prometheus.BuildFQName(metrics.Namespace, "ingestor", "realtime_streaming_overcommitted"),
			"1 if the minimum streaming slots of all ingestor peers exceeds the endpoint's streaming budget",
			[]string{"endpoint"}, nil),
	}
}

func (c *streamingSlotsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.slotsDesc
	ch <- c.overcommitted
}

func (c *streamingSlotsCollector) Collect(ch chan<- prometheus.Metric) {
	for endpoint, s := range c.slots {
		st := s.Stats()
		for state, v := range map[string]int{
			"budget":  st.Budget,
			"peers":   st.Peers,
			"share":   st.Share,
			"limit":   st.Limit,
			"in_use":  st.InUse,
			"waiting": st.Waiting,
		} {
			ch <- prometheus.MustNewConstMetric(c.slotsDesc, prometheus.GaugeValue, float64(v), endpoint, state)
		}
		overcommitted := 0.0
		if st.Overcommitted {
			overcommitted = 1
		}
		ch <- prometheus.MustNewConstMetric(c.overcommitted, prometheus.GaugeValue, overcommitted, endpoint)
	}
}
