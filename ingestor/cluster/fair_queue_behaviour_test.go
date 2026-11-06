package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// receiveFair returns the next batch the queue dispatches, failing after a second.
func receiveFair(t *testing.T, out <-chan *Batch) *Batch {
	t.Helper()
	select {
	case b := <-out:
		return b
	case <-time.After(time.Second):
		t.Fatal("no batch dispatched")
		return nil
	}
}

// requireNoDispatch fails if the queue dispatches a batch within a short wait.
func requireNoDispatch(t *testing.T, out <-chan *Batch) {
	t.Helper()
	select {
	case b := <-out:
		t.Fatalf("unexpected batch for %s: %v", b.Table, b.Segments)
	case <-time.After(50 * time.Millisecond):
	}
}

// pathBatch returns a batch of one segment with the given path.
func pathBatch(table, prefix, path string, size int64) *Batch {
	return &Batch{Database: "DB", Table: table, Prefix: prefix, Segments: []wal.SegmentInfo{{Path: path, Size: size}}}
}

// A destination runs on every worker except the reserved ones, and its next batch waits for one to finish.
func TestFairQueueRunsUpToUnreservedWorkersPerTable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan *Batch, 10)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 4})
	for n := 0; n < 4; n++ {
		input <- fairBatch("A", "s", 1)
	}

	running := []*Batch{receiveFair(t, out), receiveFair(t, out), receiveFair(t, out)}
	requireNoDispatch(t, out)

	running[0].Release()
	receiveFair(t, out).Release()
	for _, b := range running[1:] {
		b.Release()
	}
}

// Batches queued for a busy destination are dispatched one at a time, in order, and are not combined.
func TestFairQueueDispatchesQueuedBatchesSeparately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan *Batch, 10)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2})

	input <- pathBatch("A", "DB_A", "1", 1)
	first := receiveFair(t, out)
	input <- pathBatch("A", "DB_A", "2", 1)
	input <- pathBatch("A", "DB_A", "3", 1)
	requireNoDispatch(t, out)

	first.Release()
	second := receiveFair(t, out)
	require.Equal(t, []string{"2"}, second.Paths())
	second.Release()
	third := receiveFair(t, out)
	require.Equal(t, []string{"3"}, third.Paths())
	third.Release()
}

// A batch for a destination whose queue is full is released so its segments are batched again later.
func TestFairQueueReleasesBatchesBeyondQueueDepth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan *Batch, 10)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2, QueueDepth: 2})

	input <- fairBatch("A", "s", 1)
	running := receiveFair(t, out)
	queued := []*Batch{fairBatch("A", "s", 1), fairBatch("A", "s", 1)}
	for _, b := range queued {
		input <- b
	}
	overflow := fairBatch("A", "s", 1)
	input <- overflow

	require.Eventually(t, overflow.IsReleased, time.Second, time.Millisecond)
	for _, b := range queued {
		require.False(t, b.IsReleased())
	}
	running.Release()
}

// When its input closes the queue closes its output and releases the batches it still holds.
func TestFairQueueClosesWhenInputCloses(t *testing.T) {
	input := make(chan *Batch, 10)
	out := FairQueue(context.Background(), input, FairQueueOpts{Workers: 2})
	input <- fairBatch("A", "s", 1)
	running := receiveFair(t, out)
	queued := fairBatch("A", "s", 1)
	input <- queued
	close(input)

	select {
	case _, open := <-out:
		require.False(t, open)
	case <-time.After(time.Second):
		t.Fatal("output not closed")
	}
	require.True(t, queued.IsReleased())
	running.Release()
}

func TestFairQueueIgnoresNilBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan *Batch, 10)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2})
	input <- nil
	input <- fairBatch("A", "s", 1)
	b := receiveFair(t, out)
	require.Equal(t, "A", b.Table)
	b.Release()
}

// A batch without a table is scheduled by the destination of its first segment's filename.
func TestFairQueueSchedulesBatchWithoutTableBySegmentFilename(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan *Batch, 10)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2})

	input <- &Batch{Segments: []wal.SegmentInfo{{Path: "/data/" + wal.Filename("DB", "A", "", "2f2c74ffa19e6000"), Size: 1}}}
	first := receiveFair(t, out)
	// The batch counts against table A, so another batch for A waits for it.
	input <- fairBatch("A", "s", 1)
	requireNoDispatch(t, out)
	first.Release()
	receiveFair(t, out).Release()
}

func TestFairQueueOptsDefaultsWorkers(t *testing.T) {
	opts := FairQueueOpts{}.withDefaults()
	require.Equal(t, 1, opts.Workers)
	require.Zero(t, opts.ReservedWorkers)
}
