// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

type realtimeTestEnv struct {
	idx       *wal.Index
	b         *batcher
	upload    chan *Batch
	realtime  chan *Batch
	transfer  chan *Batch
	dir       string
	idgen     *flake.Flake
	cancelCtx context.CancelFunc
}

func newRealtimeTestEnv(t *testing.T, opts RealtimeBatchOpts, open bool) *realtimeTestEnv {
	t.Helper()
	idgen, err := flake.New()
	require.NoError(t, err)

	env := &realtimeTestEnv{
		idx:      wal.NewIndex(),
		upload:   make(chan *Batch, 100),
		realtime: make(chan *Batch, 100),
		transfer: make(chan *Batch, 100),
		dir:      t.TempDir(),
		idgen:    idgen,
	}

	countMetric, sizeMetric, ageMetric := newTestMetrics()
	bi, err := NewBatcher(BatcherOpts{
		StorageDir:              env.dir,
		MaxTransferAge:          30 * time.Second,
		MaxTransferSize:         100 * 1024 * 1024,
		MinUploadSize:           100 * 1024 * 1024,
		Partitioner:             &fakePartitioner{owner: "peer"},
		Segmenter:               env.idx,
		UploadQueue:             env.upload,
		TransferQueue:           env.transfer,
		RealtimeUploadQueue:     env.realtime,
		PeerHealthReporter:      &fakeHealthChecker{healthy: true},
		Realtime:                opts,
		SegmentsCountMetric:     countMetric,
		SegmentsSizeBytesMetric: sizeMetric,
		SegmentsMaxAgeMetric:    ageMetric,
	})
	require.NoError(t, err)
	env.b = bi.(*batcher)

	if open {
		env.open(t)
	}
	return env
}

func (e *realtimeTestEnv) open(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.cancelCtx = cancel
	require.NoError(t, e.b.Open(ctx))
	t.Cleanup(func() { require.NoError(t, e.b.Close()) })
}

func (e *realtimeTestEnv) add(t *testing.T, table string, size int64, priority ingestpolicy.Priority) wal.SegmentInfo {
	t.Helper()
	id := e.idgen.NextId()
	created, err := flakeutil.ParseFlakeID(id.String())
	require.NoError(t, err)
	si := wal.SegmentInfo{
		Prefix:    "db_" + table,
		Ulid:      id.String(),
		Path:      filepath.Join(e.dir, wal.Filename("db", table, "", id.String())),
		Size:      size,
		CreatedAt: created,
		Priority:  priority,
	}
	e.idx.Add(si)
	return si
}

func receiveBatch(t *testing.T, ch chan *Batch, timeout time.Duration) *Batch {
	t.Helper()
	select {
	case b := <-ch:
		return b
	case <-time.After(timeout):
		t.Fatal("timed out waiting for batch")
		return nil
	}
}

func TestRealtimeBatcher_DisabledByDefault(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	require.Nil(t, env.b.realtime)

	// Without the realtime batcher, the scan batches realtime segments.
	si := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	require.NoError(t, env.b.BatchSegments())
	batch := receiveBatch(t, env.realtime, time.Second)
	require.Equal(t, []string{si.Path}, batch.Paths())
}

func TestRealtimeBatcher_BatchesAfterLatency(t *testing.T) {
	const latency = 50 * time.Millisecond
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: latency, MaxBatchBytes: 1 << 20}, true)

	start := time.Now()
	a := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	b := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	batch := receiveBatch(t, env.realtime, time.Second)
	require.GreaterOrEqual(t, time.Since(start), latency)
	require.Equal(t, []string{a.Path, b.Path}, batch.Paths())
	require.Equal(t, ingestpolicy.PriorityRealtime, batch.Priority)
	require.Equal(t, "db", batch.Database)
	require.Equal(t, "Realtime", batch.Table)
	require.Empty(t, env.upload)
	require.Empty(t, env.transfer)
}

func TestRealtimeBatcher_BatchesWhenFull(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Hour, MaxBatchBytes: 150}, true)

	a := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	b := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	// The pending bytes exceed MaxBatchBytes so batches are emitted without waiting for the latency deadline.
	first := receiveBatch(t, env.realtime, time.Second)
	second := receiveBatch(t, env.realtime, time.Second)
	require.Equal(t, []string{a.Path}, first.Paths())
	require.Equal(t, []string{b.Path}, second.Paths())
}

func TestRealtimeBatcher_SplitsBatches(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Hour, MaxBatchBytes: 250}, false)
	env.b.maxBatchSegments = 3

	var paths []string
	for i := 0; i < 5; i++ {
		paths = append(paths, env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime).Path)
	}
	big := env.add(t, "Realtime", 1000, ingestpolicy.PriorityRealtime)

	env.b.realtime.Flush(context.Background())
	require.Len(t, env.realtime, 4)
	require.Equal(t, paths[0:2], (<-env.realtime).Paths())
	require.Equal(t, paths[2:4], (<-env.realtime).Paths())
	require.Equal(t, paths[4:5], (<-env.realtime).Paths())
	// A segment larger than MaxBatchBytes is batched alone.
	require.Equal(t, []string{big.Path}, (<-env.realtime).Paths())

	env.b.maxBatchSegments = 2
	for i := 0; i < 3; i++ {
		env.add(t, "Other", 1, ingestpolicy.PriorityRealtime)
	}
	env.b.realtime.Flush(context.Background())
	require.Len(t, (<-env.realtime).Segments, 2)
	require.Len(t, (<-env.realtime).Segments, 1)
}

func TestRealtimeBatcher_IgnoresQueuedSegments(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Millisecond, MaxBatchBytes: 1}, true)

	rt := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	q := env.add(t, "Queued", 100, ingestpolicy.PriorityQueued)

	require.Equal(t, []string{rt.Path}, receiveBatch(t, env.realtime, time.Second).Paths())

	// The scan batches queued segments and skips realtime segments.
	env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	owned, notOwned, err := env.b.processSegments()
	require.NoError(t, err)
	require.Empty(t, owned)
	require.Len(t, notOwned, 1)
	require.Equal(t, []string{q.Path}, notOwned[0].Paths())
}

func TestRealtimeBatcher_BatchesExistingSegmentsOnOpen(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: 10 * time.Millisecond, MaxBatchBytes: 1 << 20}, false)
	si := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	env.open(t)
	require.Equal(t, []string{si.Path}, receiveBatch(t, env.realtime, time.Second).Paths())
}

func TestRealtimeBatcher_RetriesReleasedBatches(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: 10 * time.Millisecond, MaxBatchBytes: 1 << 20}, false)
	env.b.realtime.retryDelay = 50 * time.Millisecond
	env.open(t)

	si := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	batch := receiveBatch(t, env.realtime, time.Second)

	// A failed upload releases the batch without removing it so it is retried after the retry delay.
	released := time.Now()
	batch.Release()
	retry := receiveBatch(t, env.realtime, time.Second)
	require.GreaterOrEqual(t, time.Since(released), 40*time.Millisecond)
	require.Equal(t, []string{si.Path}, retry.Paths())

	// A batch that was uploaded and removed is not retried.
	require.NoError(t, retry.Remove())
	retry.Release()
	select {
	case b := <-env.realtime:
		t.Fatalf("unexpected batch %v", b.Paths())
	case <-time.After(150 * time.Millisecond):
	}
}

func TestRealtimeBatcher_FullReleasedBatchHonorsRetryDelay(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Hour, MaxBatchBytes: 100}, false)
	env.b.realtime.retryDelay = time.Hour

	env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	env.b.realtime.Flush(context.Background())
	batch := receiveBatch(t, env.realtime, time.Second)
	batch.Release()

	env.b.realtime.process(context.Background(), time.Now(), false)
	require.Empty(t, env.realtime, "full released batch must wait for the retry deadline")
}

func TestRealtimeBatcher_RetryDeadlineSurvivesSegmentArrival(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: 10 * time.Millisecond, MaxBatchBytes: 100}, false)
	r := env.b.realtime
	r.retryDelay = time.Minute

	env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	r.Flush(context.Background())
	receiveBatch(t, env.realtime, time.Second).Release()

	arrived := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	eventAt := time.Now()
	r.onSegment(arrived)
	r.process(context.Background(), eventAt.Add(20*time.Millisecond), false)

	require.Empty(t, env.realtime, "a normal segment event must not shorten a failed batch's retry delay")
}

func TestRealtimeBatcher_RetryDeadlineSurvivesSweep(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: 10 * time.Millisecond, MaxBatchBytes: 100}, false)
	r := env.b.realtime
	r.retryDelay = time.Minute

	env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	r.Flush(context.Background())
	receiveBatch(t, env.realtime, time.Second).Release()
	env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	sweptAt := time.Now()
	r.sweep()
	r.process(context.Background(), sweptAt.Add(20*time.Millisecond), false)

	require.Empty(t, env.realtime, "a periodic sweep must not shorten a failed batch's retry delay")
}

func TestRealtimeBatcher_DoesNotBatchSegmentsTwice(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Millisecond, MaxBatchBytes: 1 << 20}, false)
	env.realtime = make(chan *Batch, 10000)
	env.b.realtimeUploadQueue = env.realtime
	env.open(t)

	const segments = 500
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < segments; i++ {
			env.add(t, "Realtime", 1, ingestpolicy.PriorityRealtime)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			env.b.realtime.Flush(context.Background())
		}
	}()
	wg.Wait()
	env.b.realtime.Flush(context.Background())

	seen := map[string]int{}
	require.Eventually(t, func() bool {
		for {
			select {
			case b := <-env.realtime:
				for _, p := range b.Paths() {
					seen[p]++
				}
			default:
				return len(seen) == segments
			}
		}
	}, time.Second, time.Millisecond)
	for path, n := range seen {
		require.Equal(t, 1, n, path)
	}
}

func TestRealtimeBatcher_BatchSegmentsFlushesRealtime(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Hour, MaxBatchBytes: 1 << 20}, true)
	si := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	// Shutdown calls BatchSegments to flush everything, including realtime segments waiting for their deadline.
	require.NoError(t, env.b.BatchSegments())
	require.Equal(t, []string{si.Path}, receiveBatch(t, env.realtime, time.Second).Paths())
}

func TestRealtimeBatcher_ReleasesUnsentBatchesOnCancel(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Hour, MaxBatchBytes: 1}, false)
	env.b.realtimeUploadQueue = make(chan *Batch) // Unbuffered with no reader.

	a := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	b := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env.b.realtime.Flush(ctx)

	// Both segments are released so they can be batched again.
	for _, si := range []wal.SegmentInfo{a, b} {
		n, _ := env.b.segments.Get(si.Path)
		require.Zero(t, n, si.Path)
	}
	_, pending := env.b.realtime.pending["db_Realtime"]
	require.True(t, pending)
}

func TestRealtimeBatcher_CollectorRoutesToTransferQueue(t *testing.T) {
	// The collector shares its realtime upload and transfer queues, so realtime batches are transferred.
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Millisecond, MaxBatchBytes: 1 << 20}, false)
	env.b.realtimeUploadQueue = env.transfer
	env.b.realtimeTransferQueue = env.transfer
	env.open(t)

	si := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	require.Equal(t, []string{si.Path}, receiveBatch(t, env.transfer, time.Second).Paths())
}

func TestRealtimeBatcher_SegmentArrivingDuringBatchStaysPending(t *testing.T) {
	// Regression: a segment that closed while a batch was being built kept the earlier deadline, so clearing the
	// prefix after the batch was emitted lost the new segment until the next sweep.
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Hour, MaxBatchBytes: 1 << 20}, false)
	r := env.b.realtime
	const prefix = "db_Realtime"

	r.markPending(prefix, time.Now(), false)
	r.mu.Lock()
	snapshot := r.pending[prefix]
	r.mu.Unlock()

	// A segment closes with a later deadline after the snapshot was read.
	r.markPending(prefix, time.Now().Add(time.Hour), false)
	r.clearPending(prefix, snapshot.seq)

	r.mu.Lock()
	p, ok := r.pending[prefix]
	r.mu.Unlock()
	require.True(t, ok)
	// The earlier deadline is kept so the new segment is batched promptly.
	require.Equal(t, snapshot.deadline, p.deadline)

	r.clearPending(prefix, p.seq)
	_, ok = r.pending[prefix]
	require.False(t, ok)
}

func TestRealtimeBatcher_NoSegmentsLostUnderConcurrency(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Millisecond, MaxBatchBytes: 1 << 20}, false)
	env.realtime = make(chan *Batch, 10000)
	env.b.realtimeUploadQueue = env.realtime
	// Disable the sweep so lost segments are not recovered.
	env.b.realtime.sweepInterval = time.Hour
	env.open(t)

	const segments = 500
	for i := 0; i < segments; i++ {
		env.add(t, "Realtime", 1, ingestpolicy.PriorityRealtime)
	}

	seen := map[string]bool{}
	require.Eventually(t, func() bool {
		for {
			select {
			case b := <-env.realtime:
				for _, p := range b.Paths() {
					seen[p] = true
				}
			default:
				return len(seen) == segments
			}
		}
	}, 2*time.Second, time.Millisecond)
}
