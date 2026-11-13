package cluster

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// transferAfterTransientFailure fails the first transfer to the peer with a 500, then re-offers the segment the way
// the batcher would and reports whether it was delivered within the given window.
func transferAfterTransientFailure(t *testing.T, health *Health, within time.Duration) bool {
	t.Helper()
	var calls atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer s.Close()

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "remote", addr: s.URL},
		Health:                 health,
		Hostname:               "local",
		MaxTransferConcurrency: 1,
		DisableGzip:            true,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	seg, err := wal.NewSegment(t.TempDir(), "DB_Table")
	require.NoError(t, err)
	_, err = seg.Write(ctx, []byte("payload"))
	require.NoError(t, err)
	require.NoError(t, seg.Close())
	newBatch := func() *Batch {
		return &Batch{Database: "DB", Table: "Table", Segments: []wal.SegmentInfo{seg.Info()}, batcher: &fakeBatcher{}}
	}

	first := newBatch()
	rep.TransferQueue() <- first
	require.Eventually(t, first.IsReleased, time.Second, time.Millisecond)
	require.EqualValues(t, 1, calls.Load())
	require.False(t, first.IsRemoved())

	start := time.Now()
	cur := newBatch()
	rep.TransferQueue() <- cur
	for time.Since(start) < within {
		if cur.IsRemoved() {
			t.Logf("delivered %s after the transient failure", time.Since(start).Round(10*time.Millisecond))
			require.True(t, health.IsPeerHealthy("remote"))
			return true
		}
		if cur.IsReleased() {
			cur = newBatch()
			rep.TransferQueue() <- cur
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("not delivered within %s of the transient failure", within)
	return false
}

// The collector sends everything to one load-balanced "remote" peer, so a single transient failure must not stop
// all shipping for the full UnhealthyTimeout.
func TestReplicatorInitialUnhealthyTimeoutRetriesQuickly(t *testing.T) {
	health := NewHealth(HealthOpts{UnhealthyTimeout: time.Minute, InitialUnhealthyTimeout: time.Second})
	require.True(t, transferAfterTransientFailure(t, health, 5*time.Second))
}

// Without InitialUnhealthyTimeout, as on the ingestor, a failed peer is still skipped for the full UnhealthyTimeout so
// its segments keep falling back to local upload.
func TestReplicatorDefaultUnhealthyTimeoutUnchanged(t *testing.T) {
	health := NewHealth(HealthOpts{UnhealthyTimeout: time.Minute})
	require.False(t, transferAfterTransientFailure(t, health, 3*time.Second))
}

// nextCheck reports the peer's NextCheck.  A fully healthy peer with nothing pending is evicted from the state map, so
// a missing entry means there is no backoff (zero).
func nextCheck(h *Health, peer string) time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s := h.state[peer]
	if s == nil {
		return time.Time{}
	}
	return s.NextCheck
}

// failAfterBackoff fails the peer once its previous backoff has expired and returns the new backoff.
func failAfterBackoff(h *Health, peer string) time.Duration {
	h.mu.Lock()
	if s := h.state[peer]; s != nil {
		s.NextCheck = time.Now().Add(-time.Millisecond)
	}
	h.mu.Unlock()
	before := time.Now()
	h.SetPeerUnhealthy(peer)
	return nextCheck(h, peer).Sub(before).Round(time.Second)
}

// Repeated failures back off exponentially up to UnhealthyTimeout, and a success resets the backoff.
func TestHealthPeerBackoffEscalatesAndResets(t *testing.T) {
	h := NewHealth(HealthOpts{UnhealthyTimeout: 10 * time.Second, InitialUnhealthyTimeout: time.Second})
	h.jitter = func(n time.Duration) time.Duration { return n }

	require.Equal(t, 1*time.Second, failAfterBackoff(h, "peer"))

	// Failures while the peer is backing off, such as concurrent in-flight requests, don't escalate it.
	h.SetPeerUnhealthy("peer")
	h.SetPeerUnhealthy("peer")
	require.Equal(t, time.Second, time.Until(nextCheck(h, "peer")).Round(time.Second))

	require.Equal(t, 2*time.Second, failAfterBackoff(h, "peer"))
	require.Equal(t, 4*time.Second, failAfterBackoff(h, "peer"))
	require.Equal(t, 8*time.Second, failAfterBackoff(h, "peer"))
	require.Equal(t, 10*time.Second, failAfterBackoff(h, "peer"))
	require.Equal(t, 10*time.Second, failAfterBackoff(h, "peer"))

	h.SetPeerHealthy("peer")
	require.True(t, h.IsPeerHealthy("peer"))
	require.Equal(t, 1*time.Second, failAfterBackoff(h, "peer"))
}

// A concurrent request failing while the peer honors a longer Retry-After must not shorten that pause.
func TestHealthConcurrentFailureKeepsRetryAfter(t *testing.T) {
	h := NewHealth(HealthOpts{UnhealthyTimeout: time.Minute, InitialUnhealthyTimeout: time.Second})
	h.jitter = func(n time.Duration) time.Duration { return n }

	h.SetPeerUnhealthyFor("peer", 30*time.Second)
	h.SetPeerUnhealthy("peer")
	require.Equal(t, 30*time.Second, time.Until(nextCheck(h, "peer")).Round(time.Second))
	require.False(t, h.IsPeerHealthy("peer"))
}

// A transfer that was in flight when the peer asked for a pause, and then succeeds, does not end the pause early, but
// it does reset the backoff from failures.
func TestHealthConcurrentSuccessKeepsRetryAfter(t *testing.T) {
	h := NewHealth(HealthOpts{UnhealthyTimeout: time.Minute, InitialUnhealthyTimeout: time.Second})
	h.jitter = func(n time.Duration) time.Duration { return n }

	h.SetPeerUnhealthyFor("peer", 30*time.Second)
	h.SetPeerHealthy("peer")
	require.False(t, h.IsPeerHealthy("peer"), "the peer asked for 30s")
	require.Equal(t, 30*time.Second, time.Until(nextCheck(h, "peer")).Round(time.Second))
	require.Zero(t, h.state["peer"].Failures)

	// Once the pause has ended, a success makes the peer healthy again.
	h.mu.Lock()
	h.state["peer"].pausedUntil = time.Now().Add(-time.Millisecond)
	h.mu.Unlock()
	h.SetPeerHealthy("peer")
	require.True(t, h.IsPeerHealthy("peer"))
	require.True(t, nextCheck(h, "peer").IsZero())
}

// A success clears a backoff from ordinary failures at once, as before.
func TestHealthSuccessClearsFailureBackoff(t *testing.T) {
	h := NewHealth(HealthOpts{UnhealthyTimeout: time.Minute, InitialUnhealthyTimeout: time.Second})
	h.SetPeerUnhealthy("peer")
	require.False(t, h.IsPeerHealthy("peer"))
	h.SetPeerHealthy("peer")
	require.True(t, h.IsPeerHealthy("peer"))
}

// Without InitialUnhealthyTimeout every failure skips the peer for the full UnhealthyTimeout, as before.
func TestHealthPeerBackoffDefaultIsUnhealthyTimeout(t *testing.T) {
	h := NewHealth(HealthOpts{UnhealthyTimeout: time.Minute})
	for range 40 {
		require.Equal(t, time.Minute, failAfterBackoff(h, "peer"))
	}

	// A failure while the peer is skipped restarts the full UnhealthyTimeout.
	h.SetPeerUnhealthy("peer")
	require.Equal(t, time.Minute, time.Until(nextCheck(h, "peer")).Round(time.Second))
}

// overloadedPeerBackoff sends one transfer to a peer that rejects it as overloaded with the given Retry-After and
// returns how long the peer is then skipped.
func overloadedPeerBackoff(t *testing.T, health *Health, retryAfter string) time.Duration {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", retryAfter)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer s.Close()

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "remote", addr: s.URL},
		Health:                 health,
		Hostname:               "local",
		MaxTransferConcurrency: 1,
		DisableGzip:            true,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	seg, err := wal.NewSegment(t.TempDir(), "DB_Table")
	require.NoError(t, err)
	_, err = seg.Write(ctx, []byte("payload"))
	require.NoError(t, err)
	require.NoError(t, seg.Close())

	batch := &Batch{Database: "DB", Table: "Table", Segments: []wal.SegmentInfo{seg.Info()}, batcher: &fakeBatcher{}}
	rep.TransferQueue() <- batch
	require.Eventually(t, batch.IsReleased, time.Second, time.Millisecond)
	require.False(t, health.IsPeerHealthy("remote"))
	return time.Until(nextCheck(health, "remote")).Round(100 * time.Millisecond)
}

// An overloaded peer's Retry-After is honored instead of retrying sooner, capped at UnhealthyTimeout.
func TestReplicatorPeerOverloadHonorsRetryAfter(t *testing.T) {
	collector := func() *Health {
		return NewHealth(HealthOpts{UnhealthyTimeout: time.Minute, InitialUnhealthyTimeout: time.Second})
	}
	require.Equal(t, 3*time.Second, overloadedPeerBackoff(t, collector(), "3"))
	require.Equal(t, time.Minute, overloadedPeerBackoff(t, collector(), "300"))

	// Without InitialUnhealthyTimeout, as on the ingestor, the peer is still skipped for the full UnhealthyTimeout.
	require.Equal(t, time.Minute, overloadedPeerBackoff(t, NewHealth(HealthOpts{UnhealthyTimeout: time.Minute}), "3"))
}

// Senders that fail at the same time must not all retry at the same instant.
func TestHealthPeerBackoffIsJittered(t *testing.T) {
	h := NewHealth(HealthOpts{UnhealthyTimeout: time.Minute, InitialUnhealthyTimeout: time.Second})
	seen := map[time.Duration]bool{}
	for i := range 50 {
		peer := fmt.Sprintf("peer-%d", i)
		before := time.Now()
		h.SetPeerUnhealthy(peer)
		d := nextCheck(h, peer).Sub(before)
		require.GreaterOrEqual(t, d, 500*time.Millisecond)
		require.LessOrEqual(t, d, time.Second+10*time.Millisecond)
		seen[d.Round(10*time.Millisecond)] = true
	}
	require.Greater(t, len(seen), 5, "first backoffs should be spread out, got %v", seen)

	// A fixed UnhealthyTimeout, as on the ingestor, is not jittered.
	h = NewHealth(HealthOpts{UnhealthyTimeout: time.Minute})
	before := time.Now()
	h.SetPeerUnhealthy("peer")
	require.Equal(t, time.Minute, nextCheck(h, "peer").Sub(before).Round(time.Second))
}
