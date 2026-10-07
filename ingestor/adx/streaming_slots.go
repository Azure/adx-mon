// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"sync"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/logger"
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
	// changed is closed and replaced when a slot may have become available and waiters is non-zero.
	changed chan struct{}
	waiters int
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
		changed:       make(chan struct{}),
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
	s.notifyLocked()
	s.mu.Unlock()

	if overcommitted {
		logger.Warnf("Streaming budget %d is overcommitted: %d peers with at least %d slots each", s.budget, peers.Count, s.minSlots)
	}
}

// Acquire waits for a slot.  The returned func must be called when the request completes and reports whether the
// request was throttled.
func (s *StreamingSlots) Acquire(ctx context.Context) (release func(throttled bool), err error) {
	for {
		s.mu.Lock()
		if s.inUse < s.limit {
			s.inUse++
			s.mu.Unlock()
			return s.releaseFunc(), nil
		}
		changed := s.changed
		s.waiters++
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-changed:
		}

		s.mu.Lock()
		s.waiters--
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
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
		once.Do(func() { s.release(throttled) })
	}
}

func (s *StreamingSlots) release(throttled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.inUse--
	if throttled {
		s.successes = 0
		s.limit = max(1, s.limit/2)
	} else if s.limit < s.share {
		s.successes++
		if s.successes >= s.increaseAfter {
			s.successes = 0
			s.limit++
		}
	}
	s.notifyLocked()
}

// notifyLocked wakes goroutines waiting for a slot.  s.mu must be held.
func (s *StreamingSlots) notifyLocked() {
	if s.waiters == 0 {
		return
	}
	close(s.changed)
	s.changed = make(chan struct{})
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
		Overcommitted: s.overcommittedLocked(),
	}
}
