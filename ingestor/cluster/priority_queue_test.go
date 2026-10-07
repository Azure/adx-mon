// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
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
	b, ok := q.Next(context.Background(), true)
	require.True(t, ok)
	require.Same(t, queued, b)
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

func TestRunWorkers_QueuedNotStarvedByRealtime(t *testing.T) {
	q := PriorityQueues{Realtime: make(chan *Batch, 1000), Queued: make(chan *Batch, 1000)}
	for i := 0; i < 1000; i++ {
		q.Realtime <- &Batch{Priority: 1}
	}
	for i := 0; i < 10; i++ {
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
	for i := 0; i < 100; i++ {
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
