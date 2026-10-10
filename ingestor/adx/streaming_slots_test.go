// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestSlotShare_SumsToBudget(t *testing.T) {
	for _, budget := range []int{1, 7, 50, 100, 101} {
		for count := 1; count <= budget; count++ {
			sum := 0
			for rank := 0; rank < count; rank++ {
				share := slotShare(budget, cluster.PeerInfo{Count: count, Rank: rank}, 0, 0)
				require.GreaterOrEqual(t, share, budget/count)
				require.LessOrEqual(t, share, budget/count+1)
				sum += share
			}
			require.Equal(t, budget, sum, "budget=%d count=%d", budget, count)
		}
	}
}

func TestSlotShare(t *testing.T) {
	tests := []struct {
		name               string
		budget             int
		peers              cluster.PeerInfo
		minSlots, maxSlots int
		want               int
	}{
		{name: "even split", budget: 100, peers: cluster.PeerInfo{Count: 50, Rank: 49}, minSlots: 1, want: 2},
		{name: "remainder to low ranks", budget: 100, peers: cluster.PeerInfo{Count: 30, Rank: 9}, minSlots: 1, want: 4},
		{name: "no remainder for high ranks", budget: 100, peers: cluster.PeerInfo{Count: 30, Rank: 10}, minSlots: 1, want: 3},
		{name: "single peer", budget: 100, peers: cluster.PeerInfo{Count: 1}, minSlots: 1, want: 100},
		{name: "zero peers treated as one", budget: 100, peers: cluster.PeerInfo{}, minSlots: 1, want: 100},
		{name: "min slots when overcommitted", budget: 100, peers: cluster.PeerInfo{Count: 150, Rank: 120}, minSlots: 1, want: 1},
		{name: "max slots", budget: 100, peers: cluster.PeerInfo{Count: 2}, minSlots: 1, maxSlots: 10, want: 10},
		{name: "min above max", budget: 100, peers: cluster.PeerInfo{Count: 200}, minSlots: 3, maxSlots: 2, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, slotShare(tt.budget, tt.peers, tt.minSlots, tt.maxSlots))
		})
	}
}

func TestStreamingSlots_AcquireUpToLimit(t *testing.T) {
	s := NewStreamingSlots(4, 1, 0, cluster.PeerInfo{Count: 2, Rank: 0})
	require.Equal(t, SlotStats{Budget: 4, Peers: 2, Share: 2, Limit: 2}, s.Stats())

	r1, ok := s.TryAcquire()
	require.True(t, ok)
	r2, err := s.Acquire(context.Background())
	require.NoError(t, err)
	_, ok = s.TryAcquire()
	require.False(t, ok)
	require.Equal(t, 2, s.Stats().InUse)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = s.Acquire(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	r1(false)
	// Releasing twice has no effect.
	r1(false)
	require.Equal(t, 1, s.Stats().InUse)
	r2(false)
	require.Zero(t, s.Stats().InUse)
}

func TestStreamingSlots_ReleaseWakesWaiter(t *testing.T) {
	s := NewStreamingSlots(1, 1, 0, cluster.PeerInfo{Count: 1})
	release, err := s.Acquire(context.Background())
	require.NoError(t, err)

	acquired := make(chan struct{})
	go func() {
		r, err := s.Acquire(context.Background())
		if err == nil {
			r(false)
		}
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("acquired slot while pool was full")
	case <-time.After(20 * time.Millisecond):
	}
	release(false)
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("waiter was not woken")
	}
}

func TestStreamingSlots_ThrottleHalvesLimitAndSuccessRestores(t *testing.T) {
	s := NewStreamingSlots(8, 1, 0, cluster.PeerInfo{Count: 1})
	s.increaseAfter = 3

	release := func(throttled bool) {
		r, ok := s.TryAcquire()
		require.True(t, ok)
		r(throttled)
	}

	release(true)
	require.Equal(t, 4, s.Stats().Limit)
	release(true)
	require.Equal(t, 2, s.Stats().Limit)
	release(true)
	release(true)
	// The limit never drops below one slot.
	require.Equal(t, 1, s.Stats().Limit)

	// Consecutive successes raise the limit by one.
	release(false)
	release(false)
	require.Equal(t, 1, s.Stats().Limit)
	release(false)
	require.Equal(t, 2, s.Stats().Limit)

	// A throttle resets the success streak.
	release(false)
	release(false)
	release(true)
	require.Equal(t, 1, s.Stats().Limit)

	for i := 0; i < 3*8; i++ {
		release(false)
	}
	// The limit never exceeds the share.
	require.Equal(t, 8, s.Stats().Limit)
}

func TestStreamingSlots_SetPeersShrinksWithoutCancellingInFlight(t *testing.T) {
	s := NewStreamingSlots(10, 1, 0, cluster.PeerInfo{Count: 1})
	var releases []func(bool)
	for i := 0; i < 10; i++ {
		r, ok := s.TryAcquire()
		require.True(t, ok)
		releases = append(releases, r)
	}

	s.SetPeers(cluster.PeerInfo{Count: 5, Rank: 0})
	stats := s.Stats()
	require.Equal(t, 2, stats.Share)
	require.Equal(t, 2, stats.Limit)
	require.Equal(t, 10, stats.InUse)

	// No slots are granted until usage drops below the new limit.
	for i := 0; i < 8; i++ {
		_, ok := s.TryAcquire()
		require.False(t, ok)
		releases[i](false)
	}
	require.Equal(t, 2, s.Stats().InUse)
	_, ok := s.TryAcquire()
	require.False(t, ok)
	releases[8](false)
	r, ok := s.TryAcquire()
	require.True(t, ok)
	r(false)
	releases[9](false)
}

func TestStreamingSlots_SetPeersGrowWakesWaiters(t *testing.T) {
	s := NewStreamingSlots(10, 1, 0, cluster.PeerInfo{Count: 10, Rank: 5})
	r, ok := s.TryAcquire()
	require.True(t, ok)
	defer r(false)

	acquired := make(chan struct{})
	go func() {
		r, err := s.Acquire(context.Background())
		if err == nil {
			r(false)
		}
		close(acquired)
	}()

	s.SetPeers(cluster.PeerInfo{Count: 2, Rank: 1})
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("waiter was not woken when the share grew")
	}
	require.Equal(t, 5, s.Stats().Limit)
}

func TestStreamingSlots_SetPeersKeepsThrottledReduction(t *testing.T) {
	s := NewStreamingSlots(20, 1, 0, cluster.PeerInfo{Count: 2, Rank: 0})
	r, _ := s.TryAcquire()
	r(true)
	require.Equal(t, 5, s.Stats().Limit)

	// Growing the share raises the throttled limit by the same amount rather than resetting it.
	s.SetPeers(cluster.PeerInfo{Count: 1})
	stats := s.Stats()
	require.Equal(t, 20, stats.Share)
	require.Equal(t, 15, stats.Limit)
}

func TestStreamingSlots_Overcommitted(t *testing.T) {
	s := NewStreamingSlots(2, 1, 0, cluster.PeerInfo{Count: 2})
	require.False(t, s.Stats().Overcommitted)
	s.SetPeers(cluster.PeerInfo{Count: 3, Rank: 2})
	stats := s.Stats()
	require.True(t, stats.Overcommitted)
	require.Equal(t, 1, stats.Share)
}

func TestStreamingSlots_MinimumOneSlot(t *testing.T) {
	s := NewStreamingSlots(0, 0, 0, cluster.PeerInfo{Count: 1})
	r, ok := s.TryAcquire()
	require.True(t, ok)
	r(false)
}

func TestStreamingSlots_ConcurrentNeverExceedsLimit(t *testing.T) {
	s := NewStreamingSlots(8, 1, 0, cluster.PeerInfo{Count: 2})
	var inUse, maxSeen atomic.Int64

	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				r, err := s.Acquire(context.Background())
				require.NoError(t, err)
				n := inUse.Add(1)
				for {
					m := maxSeen.Load()
					if n <= m || maxSeen.CompareAndSwap(m, n) {
						break
					}
				}
				inUse.Add(-1)
				r(false)
			}
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, maxSeen.Load(), int64(4))
	require.Zero(t, s.Stats().InUse)
}

func BenchmarkStreamingSlots_AcquireRelease(b *testing.B) {
	s := NewStreamingSlots(1<<20, 1, 0, cluster.PeerInfo{Count: 1})
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r, _ := s.Acquire(context.Background())
			r(false)
		}
	})
}

func TestStreamingSlotsCollector(t *testing.T) {
	s := NewStreamingSlots(10, 2, 0, cluster.PeerInfo{Count: 3, Rank: 0})
	r, ok := s.TryAcquire()
	require.True(t, ok)
	defer r(false)

	reg := prometheus.NewRegistry()
	reg.MustRegister(NewStreamingSlotsCollector(map[string]*StreamingSlots{"https://c.kusto.windows.net": s}))
	families, err := reg.Gather()
	require.NoError(t, err)

	got := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			key := f.GetName()
			for _, l := range m.GetLabel() {
				require.True(t, l.GetName() != "endpoint" || l.GetValue() == "https://c.kusto.windows.net")
				if l.GetName() == "state" {
					key += "/" + l.GetValue()
				}
			}
			got[key] = m.GetGauge().GetValue()
		}
	}
	require.Equal(t, map[string]float64{
		"adxmon_ingestor_realtime_streaming_slots/budget":  10,
		"adxmon_ingestor_realtime_streaming_slots/peers":   3,
		"adxmon_ingestor_realtime_streaming_slots/share":   4,
		"adxmon_ingestor_realtime_streaming_slots/limit":   4,
		"adxmon_ingestor_realtime_streaming_slots/in_use":  1,
		"adxmon_ingestor_realtime_streaming_overcommitted": 0,
	}, got)
}
