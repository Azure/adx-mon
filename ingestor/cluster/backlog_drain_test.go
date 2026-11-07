package cluster

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/stretchr/testify/require"
)

// Releasing several batches of a prefix at once, as a full transfer queue does, backs the prefix off as if each one
// had failed.
func TestQueuedBatcher_ReleasesInARowBackOff(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := newQueuedBatcher(env.b, time.Millisecond, 0)

	before := time.Now()
	for i := 0; i < 10; i++ {
		r.released("db_Cpu", 0)
	}
	require.WithinDuration(t, before.Add(queuedSweepInterval), r.pending["db_Cpu"].retryAfter, time.Second)
}

// A delivered batch resets the prefix's backoff count but leaves a retry that is already pending in place, so segments
// waiting for it are not batched until then.
func TestQueuedBatcher_RemovalKeepsPendingRetry(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := newQueuedBatcher(env.b, time.Millisecond, 0)

	r.released("db_Cpu", 0)
	retryAfter := r.pending["db_Cpu"].retryAfter
	r.removed("db_Cpu")
	require.Equal(t, retryAfter, r.pending["db_Cpu"].retryAfter)
	require.Zero(t, r.releases["db_Cpu"])
}

// newBacklogEnv returns an event batcher env holding n segments of 100 bytes of one table, found when it opens like a
// backlog after a restart.  Batches reach the size trigger at three segments.
func newBacklogEnv(t *testing.T, n int) *realtimeTestEnv {
	t.Helper()
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	env.b.queued = newQueuedBatcher(env.b, time.Hour, 0)
	env.b.Partitioner = &prefixPartitioner{defaultOwner: env.b.hostname}
	env.b.minUploadSize = 250
	for i := 0; i < n; i++ {
		env.add(t, "Cpu", 100, ingestpolicy.PriorityQueued)
	}
	return env
}

// A backlog is split into batches in one pass, however many batches it makes.
func TestQueuedBatcher_BacklogIsBatchedInOnePass(t *testing.T) {
	env := newBacklogEnv(t, 30)
	require.Equal(t, 2, env.b.maxTableBatches)
	env.open(t)
	// Each batch reaches the size trigger at three segments.
	for i := 0; i < 10; i++ {
		require.Len(t, receiveBatch(t, env.upload, time.Second).Segments, 3)
	}
}
