package cluster

import (
	"context"
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

func nextCheck(h *Health, peer string) time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state[peer].NextCheck
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

// Without InitialUnhealthyTimeout every failure skips the peer for the full UnhealthyTimeout, as before.
func TestHealthPeerBackoffDefaultIsUnhealthyTimeout(t *testing.T) {
	h := NewHealth(HealthOpts{UnhealthyTimeout: time.Minute})
	for i := 0; i < 40; i++ {
		require.Equal(t, time.Minute, failAfterBackoff(h, "peer"))
	}

	// A failure while the peer is skipped restarts the full UnhealthyTimeout.
	h.SetPeerUnhealthy("peer")
	require.Equal(t, time.Minute, time.Until(nextCheck(h, "peer")).Round(time.Second))
}
