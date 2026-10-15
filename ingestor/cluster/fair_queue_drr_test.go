package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Deficit round robin shares throughput by bytes: a destination sending large
// batches is served proportionally less often than one sending small batches,
// so neither dominates the pipeline.
func TestSchedulerSharesThroughputByBytes(t *testing.T) {
	const (
		large   = 1024 * 1024
		small   = 64 * 1024
		quantum = 64 * 1024
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := make(chan *Batch, 256)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 4, Quantum: quantum, QueueDepth: 64})

	// Keep both destinations continuously backlogged.
	go func() {
		for {
			select {
			case input <- fairBatch("Large", "s", large):
			case <-ctx.Done():
				return
			}
			select {
			case input <- fairBatch("Small", "s", small):
			case <-ctx.Done():
				return
			}
		}
	}()

	bytesByTable := map[string]int64{}
	for n := 0; n < 120; n++ {
		select {
		case b := <-out:
			bytesByTable[b.Table] += batchBytes(b)
			b.Release()
		case <-time.After(5 * time.Second):
			t.Fatalf("stalled after %d dispatches", n)
		}
	}

	require.Positive(t, bytesByTable["Small"])
	require.Positive(t, bytesByTable["Large"])
	ratio := float64(bytesByTable["Large"]) / float64(bytesByTable["Small"])
	require.Less(t, ratio, 4.0,
		"byte share should be roughly balanced, got Large=%d Small=%d", bytesByTable["Large"], bytesByTable["Small"])
}

// Credit must not accumulate while a destination is idle, otherwise it bursts
// ahead of others when it returns.
func TestIdleDestinationDoesNotHoardCredit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := make(chan *Batch, 16)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2, Quantum: 1024})

	input <- fairBatch("Idle", "s", 1)
	first := <-out
	first.Release()

	// Let several rounds pass with the destination idle.
	time.Sleep(50 * time.Millisecond)

	input <- fairBatch("Idle", "s", 8192)
	select {
	case b := <-out:
		require.Equal(t, "Idle", b.Table)
		b.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("destination never scheduled")
	}
}

// Scheduling cost must not grow with the number of known destinations: idle
// ones are retired rather than scanned forever.
func TestSchedulerRetiresIdleDestinations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := make(chan *Batch, 2048)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 2, Pipeline: "retire-test"})

	for n := 0; n < 500; n++ {
		input <- fairBatch(fmt.Sprintf("T%d", n), "s", 16)
	}
	for n := 0; n < 500; n++ {
		b := <-out
		b.Release()
	}

	// Once drained, a new destination is still scheduled promptly.
	input <- fairBatch("Fresh", "s", 16)
	select {
	case b := <-out:
		require.Equal(t, "Fresh", b.Table)
		b.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not keep up after many destinations drained")
	}
}

// The reserved worker guarantees a newcomer starts without waiting for an
// in-flight batch.
func TestReservedWorkerAdmitsNewDestination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	input := make(chan *Batch, 32)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 3, ReservedWorkers: 1})

	var busy []*Batch
	for n := 0; n < 5; n++ {
		input <- fairBatch("Busy", "s", 16)
	}
	for n := 0; n < 2; n++ {
		busy = append(busy, <-out)
	}

	input <- fairBatch("New", "s", 16)
	select {
	case b := <-out:
		require.Equal(t, "New", b.Table, "reserved worker serves the new destination")
		b.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("new destination starved behind a saturated pool")
	}

	for _, b := range busy {
		b.Release()
	}
}

func TestFairQueueOptsDefaults(t *testing.T) {
	opts := FairQueueOpts{Workers: 4}.withDefaults()
	require.Equal(t, 1, opts.ReservedWorkers)
	require.Equal(t, 8, opts.QueueDepth)
	require.EqualValues(t, 1024*1024, opts.Quantum)

	single := FairQueueOpts{Workers: 1}.withDefaults()
	require.Zero(t, single.ReservedWorkers, "a single worker is never reserved away")

	clamped := FairQueueOpts{Workers: 2, ReservedWorkers: 5}.withDefaults()
	require.Equal(t, 1, clamped.ReservedWorkers)
}
