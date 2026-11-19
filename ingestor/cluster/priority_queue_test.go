// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReservedWorkers(t *testing.T) {
	tests := []struct {
		n, percent, want int
	}{
		{n: 0, percent: 10, want: 0},
		{n: 1, percent: 10, want: 0},
		{n: 2, percent: 10, want: 1},
		{n: 5, percent: 10, want: 1},
		{n: 10, percent: 10, want: 1},
		{n: 11, percent: 10, want: 2},
		{n: 50, percent: 10, want: 5},
		{n: 50, percent: 0, want: 5},
		{n: 50, percent: -1, want: 5},
		{n: 50, percent: 1, want: 1},
		{n: 10, percent: 99, want: 9},
		{n: 10, percent: 100, want: 9},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, ReservedWorkers(tt.n, tt.percent), "n=%d percent=%d", tt.n, tt.percent)
	}
}

func TestPriorityQueues_NextPrefersRealtime(t *testing.T) {
	q := PriorityQueues{Realtime: make(chan *Batch, 10), Queued: make(chan *Batch, 10)}
	queued, realtime := &Batch{Prefix: "queued"}, &Batch{Prefix: "realtime"}
	q.Queued <- queued
	q.Realtime <- realtime

	b, ok := q.Next(context.Background(), false)
	require.True(t, ok)
	require.Same(t, realtime, b)

	b, ok = q.Next(context.Background(), false)
	require.True(t, ok)
	require.Same(t, queued, b)
}

func TestPriorityQueues_NextDrainsClosedQueues(t *testing.T) {
	ctx := t.Context()
	q := PriorityQueues{Realtime: make(chan *Batch, 1), Queued: make(chan *Batch, 1)}
	realtime, queued := &Batch{Prefix: "realtime"}, &Batch{Prefix: "queued"}
	q.Realtime <- realtime
	q.Queued <- queued
	close(q.Realtime)
	close(q.Queued)

	for _, want := range []*Batch{realtime, queued} {
		b, ok := q.Next(ctx, false)
		require.True(t, ok)
		require.Same(t, want, b)
	}
	b, ok := q.Next(ctx, false)
	require.False(t, ok)
	require.Nil(t, b)
}

func TestPriorityQueues_ReservedOnlyTakesQueued(t *testing.T) {
	q := PriorityQueues{Realtime: make(chan *Batch, 10), Queued: make(chan *Batch, 10)}
	realtime := &Batch{Prefix: "realtime"}
	q.Realtime <- realtime

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, ok := q.Next(ctx, true)
	require.False(t, ok)
	require.Len(t, q.Realtime, 1)

	queued := &Batch{Prefix: "queued"}
	q.Queued <- queued
	close(q.Queued)
	b, ok := q.Next(context.Background(), true)
	require.True(t, ok)
	require.Same(t, queued, b)

	b, ok = q.Next(context.Background(), true)
	require.False(t, ok)
	require.Nil(t, b)
	require.Len(t, q.Realtime, 1)
}

func TestPriorityQueues_NilRealtimeQueue(t *testing.T) {
	q := PriorityQueues{Queued: make(chan *Batch, 1)}
	queued := &Batch{}
	q.Queued <- queued
	b, ok := q.Next(context.Background(), false)
	require.True(t, ok)
	require.Same(t, queued, b)
}

func TestPriorityQueues_NextStopsOnContextDone(t *testing.T) {
	q := PriorityQueues{Realtime: make(chan *Batch), Queued: make(chan *Batch)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, reserved := range []bool{true, false} {
		_, ok := q.Next(ctx, reserved)
		require.False(t, ok)
	}
}

func TestPriorityQueues_NextDoesNotConsumeWorkAfterContextDone(t *testing.T) {
	tests := []struct {
		name     string
		reserved bool
		realtime bool
		queued   bool
	}{
		{name: "unreserved realtime", realtime: true},
		{name: "reserved queued", reserved: true, queued: true},
		{name: "both unreserved", realtime: true, queued: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := PriorityQueues{Realtime: make(chan *Batch, 1), Queued: make(chan *Batch, 1)}
			if tt.realtime {
				q.Realtime <- &Batch{Prefix: "realtime"}
			}
			if tt.queued {
				q.Queued <- &Batch{Prefix: "queued"}
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			batch, ok := q.Next(ctx, tt.reserved)

			require.False(t, ok)
			require.Nil(t, batch)
			wantRealtime, wantQueued := 0, 0
			if tt.realtime {
				wantRealtime = 1
			}
			if tt.queued {
				wantQueued = 1
			}
			require.Len(t, q.Realtime, wantRealtime)
			require.Len(t, q.Queued, wantQueued)
		})
	}
}

func TestRunWorkers_FairQueueCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		queueCtx, stopQueue := context.WithCancel(ctx)
		q := PriorityQueues{
			Realtime: make(chan *Batch, 1),
			Queued:   FairQueue(queueCtx, make(chan *Batch), FairQueueOpts{}),
		}
		processed := make(chan *Batch, 1)
		done := make(chan struct{})
		RunWorkers(ctx, q, 1, 10, func(b *Batch) {
			select {
			case processed <- b:
			case <-ctx.Done():
			}
		}, func() { close(done) })
		synctest.Wait()

		// Close FairQueue's output while the worker is blocked in Next. Keep the worker's context alive so the
		// receive observes closure rather than cancellation.
		stopQueue()
		synctest.Wait()
		require.Empty(t, processed, "closing a queue must not dispatch a nil batch")

		realtime := &Batch{Prefix: "realtime"}
		q.Realtime <- realtime
		synctest.Wait()
		require.Len(t, processed, 1, "the worker must keep taking realtime batches")
		require.Same(t, realtime, <-processed)

		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("worker did not exit after cancellation")
		}
	})
}

func TestRunWorkers_QueuedNotStarvedByRealtime(t *testing.T) {
	q := PriorityQueues{Realtime: make(chan *Batch, 1000), Queued: make(chan *Batch, 1000)}
	for range 1000 {
		q.Realtime <- &Batch{Priority: 1}
	}
	for range 10 {
		q.Queued <- &Batch{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var (
		wg               sync.WaitGroup
		queued, realtime atomic.Int64
	)
	wg.Add(4)
	RunWorkers(ctx, q, 4, 10, func(b *Batch) {
		if b.Priority == 1 {
			realtime.Add(1)
			time.Sleep(10 * time.Millisecond)
			return
		}
		queued.Add(1)
	}, wg.Done)

	// The reserved worker drains queued batches while realtime batches keep the other workers busy.
	require.Eventually(t, func() bool { return queued.Load() == 10 }, time.Second, time.Millisecond)
	require.Less(t, realtime.Load(), int64(1000))

	cancel()
	wg.Wait()
}

func TestRunWorkers_AllWorkersProcessQueuedWithoutRealtime(t *testing.T) {
	q := PriorityQueues{Realtime: make(chan *Batch), Queued: make(chan *Batch, 100)}
	for range 100 {
		q.Queued <- &Batch{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var (
		wg      sync.WaitGroup
		active  atomic.Int64
		maxSeen atomic.Int64
		done    atomic.Int64
	)
	wg.Add(4)
	RunWorkers(ctx, q, 4, 10, func(*Batch) {
		n := active.Add(1)
		for {
			m := maxSeen.Load()
			if n <= m || maxSeen.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		done.Add(1)
	}, wg.Done)

	require.Eventually(t, func() bool { return done.Load() == 100 }, 5*time.Second, time.Millisecond)
	// Reserving a worker does not reduce queued throughput when there is no realtime work.
	require.Equal(t, int64(4), maxSeen.Load())

	cancel()
	wg.Wait()
}
