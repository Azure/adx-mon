package cluster

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func owners(c *coordinator, n int) map[string]string {
	result := make(map[string]string, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("DB_Table%d", i)
		owner, _ := c.Owner([]byte(key))
		result[key] = owner
	}
	return result
}

// An ingestor that is not ready because its backlog is over its limits keeps its tables, so they do not move to
// other ingestors and back as the backlog drains.
func TestCoordinator_OverloadedPeerKeepsItsTables(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-0")
	lister.pods = []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", true), newPeerPod("ingestor-2", true)}
	require.NoError(t, c.syncPeers())
	before := owners(c, 1000)

	lister.pods[1] = newPeerPod("ingestor-1", false)
	require.NoError(t, c.syncPeers())
	moved := 0
	for key, owner := range owners(c, 1000) {
		if owner != before[key] {
			moved++
		}
	}
	t.Logf("tables that changed owner when ingestor-1 went not ready: %d of 1000", moved)
	require.Zero(t, moved)
	require.Equal(t, 3, c.Peers().Count)
}

// An ingestor that stays not ready for the removal delay leaves the partition, and rejoins once it is ready.
func TestCoordinator_NotReadyPeerRemovedAfterDelay(t *testing.T) {
	defer func(d time.Duration) { peerRemovalDelay = d }(peerRemovalDelay)
	peerRemovalDelay = 50 * time.Millisecond

	c, lister := newPeerTestCoordinator(t, "ingestor-0")
	lister.pods = []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", true)}
	require.NoError(t, c.syncPeers())

	lister.pods[1] = newPeerPod("ingestor-1", false)
	require.NoError(t, c.syncPeers())
	require.Equal(t, 2, c.Peers().Count, "a not ready peer stays during the delay")

	time.Sleep(60 * time.Millisecond)
	require.NoError(t, c.syncPeers())
	require.Equal(t, 1, c.Peers().Count, "a peer not ready for longer than the delay is removed")

	// A peer that left does not rejoin while it is not ready, and rejoins once it is.
	require.NoError(t, c.syncPeers())
	require.Equal(t, 1, c.Peers().Count)
	lister.pods[1] = newPeerPod("ingestor-1", true)
	require.NoError(t, c.syncPeers())
	require.Equal(t, 2, c.Peers().Count)
}

// A terminating ingestor leaves the partition at once.
func TestCoordinator_TerminatingPeerRemoved(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-0")
	lister.pods = []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", true)}
	require.NoError(t, c.syncPeers())

	terminating := newPeerPod("ingestor-1", false)
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	lister.pods[1] = terminating
	require.NoError(t, c.syncPeers())
	require.Equal(t, 1, c.Peers().Count)
}

// A pod that is starting and not yet ready does not join.
func TestCoordinator_StartingPeerDoesNotJoin(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-0")
	lister.pods = []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", false)}
	require.NoError(t, c.syncPeers())
	require.Equal(t, 1, c.Peers().Count)
}
