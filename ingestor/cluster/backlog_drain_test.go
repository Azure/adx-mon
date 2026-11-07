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

// A delivered batch resets the prefix's backoff count and batches the segments of its released batches at once, since
// the destination is accepting the prefix's data.  Without a delivery they wait for the backoff.
func TestQueuedBatcher_RemovalRetriesReleasedAtOnce(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := newQueuedBatcher(env.b, time.Millisecond, 0)

	r.released("db_Cpu", 0)
	require.True(t, r.pending["db_Cpu"].retryAfter.After(time.Now()))
	before := time.Now()
	r.removed("db_Cpu")
	require.True(t, r.pending["db_Cpu"].retryAfter.IsZero())
	require.False(t, r.pending["db_Cpu"].deadline.After(time.Now()))
	require.False(t, r.pending["db_Cpu"].deadline.Before(before))
	require.Zero(t, r.releases["db_Cpu"])

	// Once every segment is batched again, a delivery leaves the prefix alone.
	r.setBacklog("db_Cpu", false)
	delete(r.pending, "db_Cpu")
	r.removed("db_Cpu")
	_, pending := r.pending["db_Cpu"]
	require.False(t, pending)
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

// A backlog is batched no more than maxTableBatches at a time, and the rest as earlier batches are delivered.
func TestQueuedBatcher_BacklogIsBatchedAsItIsDelivered(t *testing.T) {
	env := newBacklogEnv(t, 30)
	require.Equal(t, 2, env.b.maxTableBatches)
	env.open(t)

	// Each batch reaches the size trigger at three segments, oldest first.
	first, second := receiveBatch(t, env.upload, time.Second), receiveBatch(t, env.upload, time.Second)
	require.Len(t, first.Segments, 3)
	require.Len(t, second.Segments, 3)
	require.Less(t, first.Segments[2].Path, second.Segments[0].Path)
	select {
	case b := <-env.upload:
		t.Fatalf("unexpected batch %v", b.Paths())
	case <-time.After(50 * time.Millisecond):
	}

	// Each delivered batch makes room for another, without waiting for the sweep.
	require.NoError(t, first.Remove())
	first.Release()
	next := receiveBatch(t, env.upload, time.Second)
	require.Len(t, next.Segments, 3)
	require.Less(t, second.Segments[2].Path, next.Segments[0].Path)

	// A released batch is retried after the backoff, as before.
	second.Release()
	env.b.queued.mu.Lock()
	require.False(t, env.b.queued.pending["db_Cpu"].retryAfter.IsZero())
	env.b.queued.mu.Unlock()
}

// A backlog larger than the queue drains at the rate its batches are delivered.
func TestQueuedBatcher_BacklogDrainsWithoutTheSweep(t *testing.T) {
	env := newBacklogEnv(t, 300)
	env.open(t)

	segments := 0
	deadline := time.After(5 * time.Second)
	for segments < 300 {
		select {
		case b := <-env.upload:
			segments += len(b.Segments)
			require.NoError(t, b.Remove())
			b.Release()
		case <-deadline:
			t.Fatalf("delivered %d of 300 segments", segments)
		}
	}
	require.Zero(t, env.idx.TotalSegments())
}

// Batching stops at maxBatches and leaves the rest of the segments unbatched.
func TestSplitPrefixN(t *testing.T) {
	env := newBacklogEnv(t, 10)
	segments := env.idx.Get(nil, "db_Cpu")

	owned, notOwned := env.b.splitPrefixN("db_Cpu", segments, 2)
	require.Len(t, owned, 2)
	require.Empty(t, notOwned)
	require.Equal(t, 6, batchedSegments(owned))
	for i, si := range segments {
		n, _ := env.b.segments.Get(si.Path)
		require.Equal(t, i < 6, n == 1, si.Path)
	}

	// Without a limit every segment is batched, the last three together and the remaining one on its own.
	owned, _ = env.b.splitPrefixN("db_Cpu", segments[6:], 0)
	require.Equal(t, 4, batchedSegments(owned))
}
