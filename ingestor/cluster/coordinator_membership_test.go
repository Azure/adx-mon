package cluster

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newOverloadedPeerPod returns a pod whose container has been running for an hour and that stopped being ready at
// notReady, as an ingestor does when its backlog goes over its limits.
func newOverloadedPeerPod(name string, notReady time.Time) *v1.Pod {
	p := newPeerPod(name, false)
	setReadyTransition(p, notReady)
	p.Status.ContainerStatuses = []v1.ContainerStatus{{
		Name:  "ingestor",
		State: v1.ContainerState{Running: &v1.ContainerStateRunning{StartedAt: metav1.NewTime(notReady.Add(-time.Hour))}},
	}}
	return p
}

func setReadyTransition(p *v1.Pod, at time.Time) {
	for i := range p.Status.Conditions {
		if p.Status.Conditions[i].Type == v1.PodReady {
			p.Status.Conditions[i].LastTransitionTime = metav1.NewTime(at)
		}
	}
}

func owners(c *coordinator, n int) map[string]string {
	result := make(map[string]string, n)
	for i := range n {
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

	lister.pods[1] = newOverloadedPeerPod("ingestor-1", time.Now())
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

	lister.pods[1] = newOverloadedPeerPod("ingestor-1", time.Now())
	require.NoError(t, c.syncPeers())
	require.Equal(t, 2, c.Peers().Count, "a not ready peer stays during the delay")

	// The coordinator re-syncs when the delay ends rather than waiting for its periodic resync.
	require.Eventually(t, func() bool { return c.Peers().Count == 1 }, time.Second, 5*time.Millisecond,
		"a peer not ready for longer than the delay is removed")

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

// Membership comes from the pods' status alone, so a coordinator that restarts while a peer is overloaded builds the
// same partition and elects the same leader as one that saw the peer go not ready.  The overloaded peer keeps its
// tables but not the leadership.
func TestCoordinator_RestartedCoordinatorAgreesOnNotReadyPeer(t *testing.T) {
	existing, existingPods := newPeerTestCoordinator(t, "ingestor-1")
	existingPods.pods = []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", true), newPeerPod("ingestor-2", true)}
	require.NoError(t, existing.syncPeers())

	overloaded := newOverloadedPeerPod("ingestor-0", time.Now())
	existingPods.pods[0] = overloaded
	require.NoError(t, existing.syncPeers())

	restarted, restartedPods := newPeerTestCoordinator(t, "ingestor-2")
	restartedPods.pods = []*v1.Pod{overloaded, newPeerPod("ingestor-1", true), newPeerPod("ingestor-2", true)}
	require.NoError(t, restarted.syncPeers())

	require.Equal(t, owners(existing, 1000), owners(restarted, 1000))
	require.True(t, existing.IsLeader(), "ingestor-1 is the first ready ingestor")
	require.False(t, restarted.IsLeader())
}

// A leader that stops being ready keeps its tables during the grace period, but hands over the leadership at once, so
// a stalled leader does not hold up functions, summary rules and management jobs.
func TestCoordinator_NotReadyLeaderStepsDown(t *testing.T) {
	leader, leaderPods := newPeerTestCoordinator(t, "ingestor-0")
	next, nextPods := newPeerTestCoordinator(t, "ingestor-1")
	pods := []*v1.Pod{newPeerPod("ingestor-0", true), newPeerPod("ingestor-1", true)}
	leaderPods.pods, nextPods.pods = pods, pods
	require.NoError(t, leader.syncPeers())
	require.NoError(t, next.syncPeers())
	require.True(t, leader.IsLeader())
	require.False(t, next.IsLeader())

	pods[0] = newOverloadedPeerPod("ingestor-0", time.Now())
	require.NoError(t, leader.syncPeers())
	require.NoError(t, next.syncPeers())
	require.False(t, leader.IsLeader(), "a not ready leader steps down")
	require.True(t, next.IsLeader(), "the first ready ingestor takes over")
	require.Equal(t, 2, leader.Peers().Count, "the not ready ingestor keeps its place in the partition")
	require.Equal(t, owners(leader, 1000), owners(next, 1000))
}

// An ingestor that is starting and not yet ready leaves itself out of the partition, as the other ingestors do, so it
// neither keeps tables they send elsewhere nor acts as a second leader.
func TestCoordinator_StartingSelfDoesNotJoin(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-0")
	starting := newPeerPod("ingestor-0", false)
	setReadyTransition(starting, time.Now().Add(-time.Second))
	starting.Status.ContainerStatuses = []v1.ContainerStatus{{
		Name:  "ingestor",
		State: v1.ContainerState{Running: &v1.ContainerStateRunning{StartedAt: metav1.Now()}},
	}}
	lister.pods = []*v1.Pod{starting, newPeerPod("ingestor-1", true)}
	require.NoError(t, c.syncPeers())

	require.False(t, c.IsLeader())
	for _, owner := range owners(c, 100) {
		require.Equal(t, "ingestor-1", owner)
	}
}

// A pod whose container restarted has not been ready since it started, so it does not keep its place while it starts.
func TestCoordinator_RestartedContainerDoesNotJoin(t *testing.T) {
	c, lister := newPeerTestCoordinator(t, "ingestor-0")
	restarted := newOverloadedPeerPod("ingestor-1", time.Now().Add(-time.Second))
	restarted.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.Now()
	lister.pods = []*v1.Pod{newPeerPod("ingestor-0", true), restarted}
	require.NoError(t, c.syncPeers())
	require.Equal(t, 1, c.Peers().Count)
}
