package cluster

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHealthStateEvictsHealthyPeers guards the growth of Health.state.
//
// Health.state is a map[string]*HealthStatus keyed by peer hostname.  SetPeerUnhealthyFor and SetPeerHealthy
// create or reset an entry for every hostname they see, and nothing ever evicted a fully healthy one, so the map
// grew with the total number of distinct peer hostnames observed for the lifetime of the process as peers rotate
// (pod restarts change names, StatefulSet scaling changes counts).  The fix deletes entries that are fully
// healthy with no pending pause or backoff, which is safe because IsPeerHealthy treats a missing entry as
// healthy.  This test demonstrates the leak: before the fix, len(h.state) grows to N after N healthy cycles;
// after the fix, fully healthy peers are evicted and the map stays small.
func TestHealthStateEvictsHealthyPeers(t *testing.T) {
	const peers = 500

	h := NewHealth(HealthOpts{
		UnhealthyTimeout: 100 * time.Millisecond,
		QueueSizer:       &stateTestQueueSizer{},
	})

	for i := 0; i < peers; i++ {
		peer := fmt.Sprintf("ingestor-%d", i)
		// A full unhealthy->healthy cycle for each distinct peer, ending fully healthy.  retryAfter is zero so the
		// recovery leaves no pending pause: the peer is fully healthy with nothing to track.
		h.SetPeerUnhealthyFor(peer, 0)
		h.SetPeerHealthy(peer)
		require.True(t, h.IsPeerHealthy(peer), "peer %q should be healthy after being restored", peer)
	}

	// Every peer ended fully healthy with no pending pause or backoff, so none of them should be retained in
	// the state map.  Before the fix the map kept all of them (len == peers), which is the leak.
	h.mu.RLock()
	require.Empty(t, h.state, "fully healthy peers should be evicted from the state map; %d peers were retained", len(h.state))
	h.mu.RUnlock()

	// A peer that is unhealthy or paused still has state to track, so it must not be evicted.
	h.SetPeerUnhealthyFor("paused-peer", 10*time.Second)
	require.False(t, h.IsPeerHealthy("paused-peer"))

	h.mu.RLock()
	require.Contains(t, h.state, "paused-peer", "a paused/unhealthy peer must keep its state entry")
	require.Len(t, h.state, 1, "only the paused peer should be retained, got %d entries", len(h.state))
	h.mu.RUnlock()
}

// stateTestQueueSizer satisfies QueueSizer with all-zero values, enough for the health tests in this package.
// It is distinct from the cluster_test package's fakeQueueSizer, which is not visible here.
type stateTestQueueSizer struct{}

func (stateTestQueueSizer) TransferQueueSize() int       { return 0 }
func (stateTestQueueSizer) UploadQueueSize() int         { return 0 }
func (stateTestQueueSizer) SegmentsTotal() int64         { return 0 }
func (stateTestQueueSizer) SegmentsSize() int64          { return 0 }
func (stateTestQueueSizer) MaxSegmentAge() time.Duration { return 0 }
