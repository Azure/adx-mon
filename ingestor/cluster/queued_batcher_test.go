// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestParseBatcherMode(t *testing.T) {
	for in, want := range map[string]BatcherMode{"": BatcherModeScan, "scan": BatcherModeScan, "event": BatcherModeEvent} {
		got, err := ParseBatcherMode(in)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := ParseBatcherMode("other")
	require.ErrorContains(t, err, `invalid batcher mode "other"`)
}

func TestNewBatcher_Mode(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	require.Nil(t, env.b.queued, "scan is the default mode")

	newBatcher := func(mode BatcherMode, segmenter Segmenter) (*batcher, error) {
		countMetric, sizeMetric, ageMetric := newTestMetrics()
		b, err := NewBatcher(BatcherOpts{
			Segmenter:               segmenter,
			Mode:                    mode,
			SegmentsCountMetric:     countMetric,
			SegmentsSizeBytesMetric: sizeMetric,
			SegmentsMaxAgeMetric:    ageMetric,
		})
		if err != nil {
			return nil, err
		}
		return b.(*batcher), nil
	}

	b, err := newBatcher(BatcherModeEvent, wal.NewIndex())
	require.NoError(t, err)
	require.NotNil(t, b.queued)

	_, err = newBatcher("other", wal.NewIndex())
	require.ErrorContains(t, err, `invalid batcher mode "other"`)

	_, err = newBatcher(BatcherModeEvent, noEventsSegmenter{wal.NewIndex()})
	require.ErrorContains(t, err, "requires a segmenter that publishes segment events")
}

// noEventsSegmenter hides the index's Subscribe method.
type noEventsSegmenter struct{ *wal.Index }

func (s noEventsSegmenter) Subscribe() {}

func newEventModeEnv(t *testing.T, realtime RealtimeBatchOpts, linger time.Duration) *realtimeTestEnv {
	t.Helper()
	env := newRealtimeTestEnv(t, realtime, false)
	env.b.queued = newQueuedBatcher(env.b, linger)
	env.open(t)
	return env
}

func TestQueuedBatcher_BatchesAfterLinger(t *testing.T) {
	const linger = 50 * time.Millisecond
	env := newEventModeEnv(t, RealtimeBatchOpts{}, linger)
	env.b.Partitioner = &prefixPartitioner{owners: map[string]string{"db_Peer": "peer"}, defaultOwner: env.b.hostname}

	start := time.Now()
	owned := env.add(t, "Owned", 100, ingestpolicy.PriorityQueued)
	peer := env.add(t, "Peer", 100, ingestpolicy.PriorityQueued)

	// Owned prefixes are uploaded and peer owned prefixes are transferred, after the linger.
	require.Equal(t, []string{owned.Path}, receiveBatch(t, env.upload, time.Second).Paths())
	require.Equal(t, []string{peer.Path}, receiveBatch(t, env.transfer, time.Second).Paths())
	require.GreaterOrEqual(t, time.Since(start), linger)
}

func TestQueuedBatcher_LingerMergesSegments(t *testing.T) {
	env := newEventModeEnv(t, RealtimeBatchOpts{}, 100*time.Millisecond)
	env.b.Partitioner = &prefixPartitioner{defaultOwner: env.b.hostname}

	a := env.add(t, "Cpu", 100, ingestpolicy.PriorityQueued)
	b := env.add(t, "Cpu", 100, ingestpolicy.PriorityQueued)

	// Segments that close within the linger are merged into one batch.
	require.Equal(t, []string{a.Path, b.Path}, receiveBatch(t, env.upload, time.Second).Paths())
}

func TestQueuedBatcher_SizeTriggerSkipsLinger(t *testing.T) {
	env := newEventModeEnv(t, RealtimeBatchOpts{}, time.Hour)
	env.b.Partitioner = &prefixPartitioner{defaultOwner: env.b.hostname}
	env.b.minUploadSize = 150

	env.add(t, "Cpu", 100, ingestpolicy.PriorityQueued)
	env.add(t, "Cpu", 100, ingestpolicy.PriorityQueued)

	batch := receiveBatch(t, env.upload, time.Second)
	require.Len(t, batch.Segments, 2)
}

func TestQueuedBatcher_RealtimeBatcherOwnsRealtimeSegments(t *testing.T) {
	env := newEventModeEnv(t, RealtimeBatchOpts{MaxBatchLatency: time.Millisecond, MaxBatchBytes: 1 << 20}, time.Millisecond)
	env.b.Partitioner = &prefixPartitioner{defaultOwner: env.b.hostname}

	rt := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)
	q := env.add(t, "Queued", 100, ingestpolicy.PriorityQueued)

	require.Equal(t, []string{rt.Path}, receiveBatch(t, env.realtime, time.Second).Paths())
	require.Equal(t, []string{q.Path}, receiveBatch(t, env.upload, time.Second).Paths())

	time.Sleep(20 * time.Millisecond)
	require.Empty(t, env.realtime)
	require.Empty(t, env.upload)
}

func TestQueuedBatcher_HandlesRealtimeWithoutRealtimeBatcher(t *testing.T) {
	env := newEventModeEnv(t, RealtimeBatchOpts{}, time.Millisecond)
	env.b.Partitioner = &prefixPartitioner{defaultOwner: "peer"}

	rt := env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	// Realtime batches are never transferred, even when a peer owns the prefix.
	batch := receiveBatch(t, env.realtime, time.Second)
	require.Equal(t, []string{rt.Path}, batch.Paths())
	require.Equal(t, ingestpolicy.PriorityRealtime, batch.Priority)
	require.Empty(t, env.transfer)
}

func TestQueuedBatcher_BatchSegmentsFlushes(t *testing.T) {
	env := newEventModeEnv(t, RealtimeBatchOpts{}, time.Hour)
	env.b.Partitioner = &prefixPartitioner{defaultOwner: env.b.hostname}
	si := env.add(t, "Cpu", 100, ingestpolicy.PriorityQueued)

	// Shutdown calls BatchSegments, which flushes segments waiting for their linger.
	require.NoError(t, env.b.BatchSegments())
	require.Equal(t, []string{si.Path}, receiveBatch(t, env.upload, time.Second).Paths())

	// The segment is not batched again.
	require.NoError(t, env.b.BatchSegments())
	require.Empty(t, env.upload)
}

// batchKey describes a batch independent of the order batches were produced.
func batchKey(kind string, b *Batch) string {
	return fmt.Sprintf("%s %s %s %v", kind, b.Prefix, b.Priority, b.Paths())
}

func TestQueuedBatcher_MatchesScan(t *testing.T) {
	idgen, err := flake.New()
	require.NoError(t, err)
	dir := t.TempDir()
	idx := wal.NewIndex()

	add := func(table string, size int64, age time.Duration, priority ingestpolicy.Priority) {
		id := idgen.NextId()
		created, err := flakeutil.ParseFlakeID(id.String())
		require.NoError(t, err)
		idx.Add(wal.SegmentInfo{
			Prefix:    "db_" + table,
			Ulid:      id.String(),
			Path:      filepath.Join(dir, wal.Filename("db", table, "", id.String())),
			Size:      size,
			CreatedAt: created.Add(-age),
			Priority:  priority,
		})
	}

	// Exercise every splitting rule and both ownership outcomes.
	for i := 0; i < 5; i++ {
		add("Count", 10, 0, ingestpolicy.PriorityQueued) // split by max segment count
	}
	for i := 0; i < 4; i++ {
		add("Upload", 120, 0, ingestpolicy.PriorityQueued) // split by min upload size
	}
	for i := 0; i < 3; i++ {
		add("Transfer", 90, 0, ingestpolicy.PriorityQueued) // split by max transfer size
	}
	add("Old", 10, time.Hour, ingestpolicy.PriorityQueued) // split by max transfer age
	add("Old", 10, 0, ingestpolicy.PriorityQueued)
	add("Peer", 10, 0, ingestpolicy.PriorityQueued)       // transferred to the owning peer
	add("Realtime", 10, 0, ingestpolicy.PriorityRealtime) // never transferred

	newBatcher := func() *batcher {
		b := newPriorityTestBatcher(t, idx, "peer")
		b.Partitioner = &prefixPartitioner{
			owners:       map[string]string{"db_Count": "node1", "db_Upload": "node1"},
			defaultOwner: "peer",
		}
		b.maxBatchSegments = 3
		b.minUploadSize = 200
		b.maxTransferSize = 150
		b.maxTransferAge = time.Minute
		b.uploadQueue = make(chan *Batch, 100)
		b.transferQueue = make(chan *Batch, 100)
		return b
	}

	scan := newBatcher()
	scanOwned, scanNotOwned, err := scan.processSegments()
	require.NoError(t, err)
	var want []string
	for _, b := range scanOwned {
		want = append(want, batchKey("upload", b))
	}
	for _, b := range scanNotOwned {
		want = append(want, batchKey("transfer", b))
	}

	event := newBatcher()
	newQueuedBatcher(event, time.Hour).Flush(context.Background())
	var got []string
	for len(event.uploadQueue) > 0 {
		got = append(got, batchKey("upload", <-event.uploadQueue))
	}
	for len(event.transferQueue) > 0 {
		got = append(got, batchKey("transfer", <-event.transferQueue))
	}

	sort.Strings(want)
	sort.Strings(got)
	require.Equal(t, want, got)
	require.Greater(t, len(want), 8, strings.Join(want, "\n"))
}
