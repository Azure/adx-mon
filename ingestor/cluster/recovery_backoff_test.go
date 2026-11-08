package cluster

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// releaseN releases a batch of prefix n times in a row, as consecutive failed deliveries do.
func releaseN(r *eventBatcher, prefix string, n int) {
	for i := 0; i < n; i++ {
		r.released(prefix, 0)
	}
}

// requireRetryNow asserts prefix is batched at once, without a backoff.
func requireRetryNow(t *testing.T, r *eventBatcher, prefix string) {
	t.Helper()
	p, ok := r.pending[prefix]
	require.True(t, ok, prefix)
	require.True(t, p.retryAfter.IsZero(), prefix)
	require.False(t, p.deadline.After(time.Now()), prefix)
	require.Zero(t, r.releases[prefix], prefix)
	require.False(t, r.failed[prefix], prefix)
}

// requireBackingOff asserts prefix waits out the longest backoff.
func requireBackingOff(t *testing.T, r *eventBatcher, prefix string) {
	t.Helper()
	require.WithinDuration(t, time.Now().Add(r.maxRetryDelay), r.pending[prefix].retryAfter, time.Second, prefix)
}

// A delivery of a prefix that had failed means its destination recovered, so the other failing prefixes are retried
// at once rather than after their backoffs.
func TestQueuedBatcher_RecoveryRetriesFailingPrefixes(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := env.b.queued

	releaseN(r, "db_Cpu", 10)
	releaseN(r, "db_Mem", 10)
	releaseN(r, "db_Disk", 10)
	requireBackingOff(t, r, "db_Mem")
	r.removed("db_Cpu")

	require.Zero(t, r.releases["db_Cpu"])
	requireRetryNow(t, r, "db_Mem")
	requireRetryNow(t, r, "db_Disk")
}

// A delivery of a prefix that had not failed says nothing about the others, which wait out their backoffs.
func TestQueuedBatcher_DeliveryLeavesOtherPrefixesBackingOff(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := env.b.queued

	releaseN(r, "db_Mem", 10)
	r.removed("db_Cpu")
	require.Equal(t, 10, r.releases["db_Mem"])
	requireBackingOff(t, r, "db_Mem")

	// Nor does a prefix whose batches were only turned away by a full queue.
	for i := 0; i < 10; i++ {
		r.queueFull("db_Cpu")
	}
	requireBackingOff(t, r, "db_Cpu")
	r.removed("db_Cpu")
	require.Equal(t, 10, r.releases["db_Mem"])
	requireBackingOff(t, r, "db_Mem")
}

// A prefix asked to retry after a delay, such as by a peer whose table is over its quota, is not failing, so it does
// not wake the others when it is delivered and is not woken by another's recovery.
func TestQueuedBatcher_RetryAfterIsNotAFailure(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := env.b.queued

	releaseN(r, "db_Cpu", 3)
	r.released("db_Cpu", 30*time.Second)
	require.False(t, r.failed["db_Cpu"])
	releaseN(r, "db_Mem", 10)
	r.removed("db_Cpu")
	requireBackingOff(t, r, "db_Mem")

	r.released("db_Cpu", 30*time.Second)
	releaseN(r, "db_Disk", 1)
	r.removed("db_Disk")
	require.WithinDuration(t, time.Now().Add(30*time.Second), r.pending["db_Cpu"].retryAfter, time.Second)
}

// A batch turned away by a full queue backs off like a failed one, but is not counted as a failure.
func TestBatch_ReleaseQueueFull(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := env.b.queued

	(&Batch{Prefix: "db_Cpu", batcher: env.b}).releaseQueueFull()
	require.Equal(t, 1, r.releases["db_Cpu"])
	require.False(t, r.failed["db_Cpu"])
	require.True(t, r.backlog["db_Cpu"])

	(&Batch{Prefix: "db_Mem", batcher: env.b}).Release()
	require.Equal(t, 1, r.releases["db_Mem"])
	require.True(t, r.failed["db_Mem"])
}

// A full fair queue turns batches away as queue-full releases.
func TestFairQueue_FullQueueReleaseIsNotAFailure(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := env.b.queued

	input := make(chan *Batch)
	ctx := t.Context()
	FairQueue(ctx, input, FairQueueOpts{Workers: 1, QueueDepth: 1})
	for i := 0; i < 3; i++ {
		input <- &Batch{Prefix: "db_Cpu", Database: "db", Table: "Cpu", batcher: env.b}
	}
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.releases["db_Cpu"] > 0
	}, time.Second, 5*time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	require.False(t, r.failed["db_Cpu"])
}

// MaxRetryDelay caps the backoff of failing tables, a minute by default.
func TestBatcherOpts_MaxRetryDelay(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	require.Equal(t, queuedSweepInterval, env.b.queued.maxRetryDelay)

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	b, err := NewBatcher(BatcherOpts{StorageDir: t.TempDir(), Segmenter: env.idx, Partitioner: &fakePartitioner{owner: "peer"},
		PeerHealthReporter: &fakeHealthChecker{healthy: true}, MaxRetryDelay: 10 * time.Second,
		SegmentsCountMetric: countMetric, SegmentsSizeBytesMetric: sizeMetric, SegmentsMaxAgeMetric: ageMetric})
	require.NoError(t, err)
	r := b.(*batcher).queued
	require.Equal(t, 10*time.Second, r.maxRetryDelay)
	releaseN(r, "db_Cpu", 10)
	require.WithinDuration(t, time.Now().Add(10*time.Second), r.pending["db_Cpu"].retryAfter, time.Second)
}
