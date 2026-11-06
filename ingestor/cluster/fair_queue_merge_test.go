package cluster

import (
	"context"
	"sync"
	"testing"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/stretchr/testify/require"
)

// releaseRecorder is a batcher that records the segments of the batches released to it.
type releaseRecorder struct {
	fakeBatcher
	mu       sync.Mutex
	released [][]string
}

func (r *releaseRecorder) Release(b *Batch) {
	r.mu.Lock()
	r.released = append(r.released, b.Paths())
	r.mu.Unlock()
}

func (r *releaseRecorder) releases() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.released...)
}

func mergeQueue(t *testing.T, opts FairQueueOpts) (chan *Batch, chan *Batch) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	input := make(chan *Batch, 20)
	return input, FairQueue(ctx, input, opts)
}

// Batches for a destination that arrive while its batch waits for a worker are added to that batch.
func TestFairQueueMergesBatchesWaitingForWorker(t *testing.T) {
	rec := &releaseRecorder{}
	input, out := mergeQueue(t, FairQueueOpts{Workers: 2, MergeSegments: 10})
	batch := func(path string) *Batch {
		b := pathBatch("A", "DB_A", path, 1)
		b.batcher = rec
		return b
	}

	input <- batch("1")
	running := receiveFair(t, out)
	input <- batch("2")
	input <- batch("3")
	input <- batch("4")
	requireNoDispatch(t, out)

	running.Release()
	merged := receiveFair(t, out)
	require.Equal(t, []string{"2", "3", "4"}, merged.Paths())

	// Releasing the merged batch releases every segment merged into it, once.
	merged.Release()
	require.Equal(t, [][]string{{"1"}, {"2", "3", "4"}}, rec.releases())
}

func TestFairQueueMergeLimits(t *testing.T) {
	rec := &releaseRecorder{}
	for name, tc := range map[string]struct {
		opts   FairQueueOpts
		second *Batch
		merged bool
	}{
		"same prefix":       {FairQueueOpts{Workers: 2, MergeSegments: 10}, pathBatch("A", "DB_A", "3", 1), true},
		"other prefix":      {FairQueueOpts{Workers: 2, MergeSegments: 10}, pathBatch("A", "DB_A_schema", "3", 1), false},
		"segment limit":     {FairQueueOpts{Workers: 2, MergeSegments: 1}, pathBatch("A", "DB_A", "3", 1), false},
		"byte limit":        {FairQueueOpts{Workers: 2, MergeSegments: 10, MergeBytes: 10}, pathBatch("A", "DB_A", "3", 10), false},
		"within byte limit": {FairQueueOpts{Workers: 2, MergeSegments: 10, MergeBytes: 11}, pathBatch("A", "DB_A", "3", 10), true},
		"disabled":          {FairQueueOpts{Workers: 2}, pathBatch("A", "DB_A", "3", 1), false},
		"other priority": {FairQueueOpts{Workers: 2, MergeSegments: 10}, func() *Batch {
			b := pathBatch("A", "DB_A", "3", 1)
			b.Priority = ingestpolicy.PriorityRealtime
			return b
		}(), false},
		"other batcher": {FairQueueOpts{Workers: 2, MergeSegments: 10}, func() *Batch {
			b := pathBatch("A", "DB_A", "3", 1)
			b.batcher = &releaseRecorder{}
			return b
		}(), false},
	} {
		t.Run(name, func(t *testing.T) {
			input, out := mergeQueue(t, tc.opts)
			first := pathBatch("A", "DB_A", "1", 10)
			first.batcher = rec
			input <- first
			running := receiveFair(t, out)

			queued := pathBatch("A", "DB_A", "2", 1)
			queued.batcher = rec
			if tc.second.batcher == nil {
				tc.second.batcher = rec
			}
			input <- queued
			input <- tc.second
			requireNoDispatch(t, out)
			running.Release()

			next := receiveFair(t, out)
			if tc.merged {
				require.Equal(t, []string{"2", "3"}, next.Paths())
				next.Release()
				return
			}
			require.Equal(t, []string{"2"}, next.Paths())
			next.Release()
			last := receiveFair(t, out)
			require.Equal(t, []string{"3"}, last.Paths())
			last.Release()
		})
	}
}

// A merged batch does not count against the destination's queue depth.
func TestFairQueueMergedBatchesDoNotFillQueue(t *testing.T) {
	rec := &releaseRecorder{}
	input, out := mergeQueue(t, FairQueueOpts{Workers: 2, QueueDepth: 1, MergeSegments: 10})
	var batches []*Batch
	for _, p := range []string{"1", "2", "3", "4"} {
		b := pathBatch("A", "DB_A", p, 1)
		b.batcher = rec
		batches = append(batches, b)
	}
	input <- batches[0]
	running := receiveFair(t, out)
	for _, b := range batches[1:] {
		input <- b
	}
	requireNoDispatch(t, out)
	for _, b := range batches[1:] {
		require.False(t, b.IsReleased())
	}
	running.Release()
	merged := receiveFair(t, out)
	require.Equal(t, []string{"2", "3", "4"}, merged.Paths())
	merged.Release()
}

// MaxRunningPerTable caps how many batches of a destination run at once, so later batches wait and merge.
func TestFairQueueMaxRunningPerTable(t *testing.T) {
	rec := &releaseRecorder{}
	input, out := mergeQueue(t, FairQueueOpts{Workers: 4, MaxRunningPerTable: 1, MergeSegments: 10})
	batch := func(path string) *Batch {
		b := pathBatch("A", "DB_A", path, 1)
		b.batcher = rec
		return b
	}
	input <- batch("1")
	running := receiveFair(t, out)
	require.Equal(t, []string{"1"}, running.Paths())
	input <- batch("2")
	input <- batch("3")
	requireNoDispatch(t, out)

	// Other destinations still run alongside it.
	other := pathBatch("B", "DB_B", "b", 1)
	input <- other
	receiveFair(t, out).Release()

	running.Release()
	require.Equal(t, []string{"2", "3"}, receiveFair(t, out).Paths())
}

// A limit at or above the pool's own does not change it.
func TestFairQueueMaxRunningPerTableAbovePool(t *testing.T) {
	input, out := mergeQueue(t, FairQueueOpts{Workers: 3, MaxRunningPerTable: 10})
	for n := 0; n < 3; n++ {
		input <- fairBatch("A", "s", 1)
	}
	a, b := receiveFair(t, out), receiveFair(t, out)
	requireNoDispatch(t, out)
	a.Release()
	b.Release()
	receiveFair(t, out).Release()
}
