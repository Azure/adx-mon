package cluster

import (
	"context"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func fairBatch(table, prefix string, size int64) *Batch {
	return &Batch{Database: "DB", Table: table, Prefix: prefix, Segments: []wal.SegmentInfo{{Size: size}}}
}

func TestFairQueueReservesWorkersAcrossSchemas(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan *Batch, 20)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 4})
	for n := 0; n < 10; n++ {
		input <- fairBatch("A", "schema", 100)
	}
	first := <-out
	second := <-out
	third := <-out
	input <- fairBatch("B", "schema", 1)
	select {
	case third := <-out:
		require.Equal(t, "B", third.Table)
		third.Release()
	case <-time.After(time.Second):
		t.Fatal("healthy table starved")
	}
	first.Release()
	second.Release()
	third.Release()
}

func TestFairQueueRetryAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan *Batch, 10)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2})
	input <- fairBatch("A", "one", 10)
	running := <-out
	running.RetryAfter(100 * time.Millisecond)
	running.Release()
	queued := fairBatch("A", "two", 10)
	input <- queued
	input <- fairBatch("B", "one", 10)
	b := <-out
	require.Equal(t, "B", b.Table)
	b.Release()
	select {
	case <-out:
		t.Fatal("retry deadline ignored")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	require.Eventually(t, queued.IsReleased, time.Second, time.Millisecond)
}

func TestFairQueueByteCreditAndIdleCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan *Batch, 2)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2})
	input <- fairBatch("A", "one", 100*1024*1024)
	select {
	case b := <-out:
		b.Release()
	case <-time.After(time.Second):
		t.Fatal("large batch never scheduled")
	}
}

func TestFairQueueLateCompletionsDoNotBlockAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan *Batch, 10)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2})
	var running []*Batch
	for _, table := range []string{"A", "B", "C", "D", "E"} {
		input <- fairBatch(table, "one", 1)
		running = append(running, <-out)
	}
	cancel()
	_, open := <-out
	require.False(t, open)
	var wg sync.WaitGroup
	for _, b := range running {
		wg.Add(1)
		go func(b *Batch) { defer wg.Done(); b.Release() }(b)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("late worker completion blocked after shutdown")
	}
}
