package cluster

import (
	"context"
	"sync"
	"testing"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
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

func tenfold(s wal.SegmentInfo) int64 { return 10 * s.Size }

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
		// Both batches are 1 byte on disk and 10 bytes as MergeSize measures them.
		"merge size limit":        {FairQueueOpts{Workers: 2, MergeSegments: 10, MergeBytes: 15, MergeSize: tenfold}, pathBatch("A", "DB_A", "3", 1), false},
		"within merge size limit": {FairQueueOpts{Workers: 2, MergeSegments: 10, MergeBytes: 20, MergeSize: tenfold}, pathBatch("A", "DB_A", "3", 1), true},
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

// Batches of destinations in a group that is running its limit wait, and keep merging, while other groups run.
func TestFairQueueMaxRunningPerGroup(t *testing.T) {
	rec := &releaseRecorder{}
	groups := map[string]string{"A": "peer1", "B": "peer1", "C": "peer2"}
	input, out := mergeQueue(t, FairQueueOpts{Workers: 8, MergeSegments: 10, MaxRunningPerGroup: 1,
		Group: func(b *Batch) string { return groups[b.Table] }})
	batch := func(table, path string) *Batch {
		b := pathBatch(table, "DB_"+table, path, 1)
		b.batcher = rec
		return b
	}

	input <- batch("A", "a1")
	a := receiveFair(t, out)
	input <- batch("B", "b1")
	input <- batch("B", "b2")
	input <- batch("C", "c1")
	c := receiveFair(t, out)
	require.Equal(t, "C", c.Table, "another group runs while peer1 is full")
	requireNoDispatch(t, out)

	a.Release()
	b := receiveFair(t, out)
	require.Equal(t, []string{"b1", "b2"}, b.Paths(), "batches that waited for the group were merged")
	b.Release()
	c.Release()
}

func TestFairQueueGroupLimitNeedsGroupAndLimit(t *testing.T) {
	for name, opts := range map[string]FairQueueOpts{
		"no group": {Workers: 4, MaxRunningPerGroup: 1},
		"no limit": {Workers: 4, Group: func(*Batch) string { return "peer" }},
	} {
		t.Run(name, func(t *testing.T) {
			input, out := mergeQueue(t, opts)
			input <- fairBatch("A", "s", 1)
			input <- fairBatch("B", "s", 1)
			a, b := receiveFair(t, out), receiveFair(t, out)
			a.Release()
			b.Release()
		})
	}
}

// A group that has room goes to the destinations waiting for it in the order they started waiting.
func TestFairQueueGroupServesLongestWaitingFirst(t *testing.T) {
	input, out := mergeQueue(t, FairQueueOpts{Workers: 8, MaxRunningPerGroup: 1, Group: func(*Batch) string { return "peer" }})
	input <- pathBatch("A", "DB_A", "a", 1)
	running := receiveFair(t, out)
	for _, table := range []string{"B", "C", "D"} {
		input <- pathBatch(table, "DB_"+table, table, 1)
		requireNoDispatch(t, out)
	}
	for _, want := range []string{"B", "C", "D"} {
		running.Release()
		running = receiveFair(t, out)
		require.Equal(t, want, running.Table)
	}
	running.Release()
}

// A destination whose next batch moved to another group stops waiting for the old one.
func TestFairQueueGroupWaitFollowsOwnerChange(t *testing.T) {
	var mu sync.Mutex
	groups := map[string]string{"A": "peer1", "B": "peer1"}
	group := func(b *Batch) string { mu.Lock(); defer mu.Unlock(); return groups[b.Table] }
	input, out := mergeQueue(t, FairQueueOpts{Workers: 8, MaxRunningPerGroup: 1, Group: group})

	input <- pathBatch("A", "DB_A", "a", 1)
	a := receiveFair(t, out)
	input <- pathBatch("B", "DB_B", "b", 1)
	requireNoDispatch(t, out)

	// B's table moves to peer2, which has room, so B runs while A still holds peer1.
	mu.Lock()
	groups["B"] = "peer2"
	mu.Unlock()
	b := receiveFair(t, out)
	require.Equal(t, "B", b.Table)
	b.Release()
	a.Release()
}

func TestGroupWaitsNext(t *testing.T) {
	g := newGroupWaits(func(b *Batch) string { return b.Table[:1] }, 1)
	a, b := wal.Destination{Database: "DB", Table: "x1"}, wal.Destination{Database: "DB", Table: "x2"}
	tables := map[wal.Destination]*fairTable{
		a: {queue: []*Batch{fairBatch("x1", "s", 1)}},
		b: {queue: []*Batch{fairBatch("x2", "s", 1)}},
	}
	ready := func(t *fairTable) bool { return t != nil && len(t.queue) > 0 }
	g.running["x"] = 1
	g.wait(a, tables[a].queue[0])
	g.wait(a, tables[a].queue[0])
	g.wait(b, tables[b].queue[0])
	require.Equal(t, []wal.Destination{a, b}, g.waiting["x"], "a destination waits once")

	_, ok := g.next(tables, ready)
	require.False(t, ok, "the group is still full")

	g.done("x")
	require.Empty(t, g.running)
	// a has no work any more, so it stops waiting and b, next in line, goes.
	tables[a].queue = nil
	d, ok := g.next(tables, ready)
	require.True(t, ok)
	require.Equal(t, b, d)
	require.Equal(t, []wal.Destination{b}, g.waiting["x"], "b keeps its place until its batch is dispatched")

	g.dispatched(b)
	require.Empty(t, g.waiting)
	require.Empty(t, g.queued)
}

// A destination waiting for a full group whose next batch moves to another full group keeps its place in line: it
// joins the new group's waiters ahead of destinations that started waiting after it.
func TestGroupWaitsKeepsPlaceWhenOwnerChanges(t *testing.T) {
	groups := map[string]string{"x1": "peer1", "x2": "peer2"}
	g := newGroupWaits(func(b *Batch) string { return groups[b.Table] }, 1)
	a, b := wal.Destination{Database: "DB", Table: "x1"}, wal.Destination{Database: "DB", Table: "x2"}
	tables := map[wal.Destination]*fairTable{
		a: {queue: []*Batch{fairBatch("x1", "s", 1)}},
		b: {queue: []*Batch{fairBatch("x2", "s", 1)}},
	}
	ready := func(t *fairTable) bool { return t != nil && len(t.queue) > 0 }
	g.running["peer1"], g.running["peer2"] = 1, 1

	g.wait(a, tables[a].queue[0])
	groups["x1"] = "peer2"
	g.wait(b, tables[b].queue[0])
	g.wait(a, tables[a].queue[0])
	require.Equal(t, []wal.Destination{a, b}, g.waiting["peer2"], "a started waiting first")
	require.NotContains(t, g.waiting, "peer1")

	g.done("peer2")
	d, ok := g.next(tables, ready)
	require.True(t, ok)
	require.Equal(t, a, d)
}

// A waiting destination that is dispatched by the round robin, such as after its table moved to a group with room,
// stops waiting.
func TestGroupWaitsDispatchedOutsideGroupStopsWaiting(t *testing.T) {
	groups := map[string]string{"x1": "peer1"}
	g := newGroupWaits(func(b *Batch) string { return groups[b.Table] }, 1)
	a := wal.Destination{Database: "DB", Table: "x1"}
	g.running["peer1"] = 1
	g.wait(a, fairBatch("x1", "s", 1))

	groups["x1"] = "peer2"
	g.dispatched(a)
	require.Empty(t, g.waiting)
	require.Empty(t, g.queued)
}

// A waiter that moves to a group with room still goes after the destinations that waited for that group longer.
func TestGroupWaitsMovedWaiterQueuesBehindOlderWaiters(t *testing.T) {
	groups := map[string]string{"c": "peer2", "a": "peer1"}
	g := newGroupWaits(func(b *Batch) string { return groups[b.Table] }, 1)
	c, a := wal.Destination{Database: "DB", Table: "c"}, wal.Destination{Database: "DB", Table: "a"}
	tables := map[wal.Destination]*fairTable{
		c: {queue: []*Batch{fairBatch("c", "s", 1)}},
		a: {queue: []*Batch{fairBatch("a", "s", 1)}},
	}
	ready := func(t *fairTable) bool { return t != nil && len(t.queue) > 0 }
	g.running["peer1"], g.running["peer2"] = 1, 1
	g.wait(c, tables[c].queue[0])
	g.wait(a, tables[a].queue[0])
	groups["a"] = "peer2"
	g.done("peer1")
	g.done("peer2")

	for i := 0; i < 20; i++ { // next ranges over a map, so try both orders.
		d, ok := g.next(tables, ready)
		require.True(t, ok)
		require.Equal(t, c, d)
	}

	// c runs on peer2, so a waits for it, and goes next once c is done.
	g.dispatched(c)
	g.running["peer2"]++
	tables[c].queue = nil
	_, ok := g.next(tables, ready)
	require.False(t, ok, "peer2 is full")
	g.done("peer2")
	d, ok := g.next(tables, ready)
	require.True(t, ok)
	require.Equal(t, a, d)
}

// A waiter whose table moved to another group is found even while its old group stays full: when only the new group
// frees a slot, the waiter goes before destinations that started waiting for the new group after it.
func TestGroupWaitsMovesWaiterOutOfFullGroup(t *testing.T) {
	groups := map[string]string{"a": "peer1", "b": "peer2"}
	g := newGroupWaits(func(b *Batch) string { return groups[b.Table] }, 1)
	a, b := wal.Destination{Database: "DB", Table: "a"}, wal.Destination{Database: "DB", Table: "b"}
	tables := map[wal.Destination]*fairTable{
		a: {queue: []*Batch{fairBatch("a", "s", 1)}},
		b: {queue: []*Batch{fairBatch("b", "s", 1)}},
	}
	ready := func(t *fairTable) bool { return t != nil && len(t.queue) > 0 }
	g.running["peer1"], g.running["peer2"] = 1, 1
	g.wait(a, tables[a].queue[0])
	g.wait(b, tables[b].queue[0])

	groups["a"] = "peer2"
	g.done("peer2")
	d, ok := g.next(tables, ready)
	require.True(t, ok)
	require.Equal(t, a, d)
}
