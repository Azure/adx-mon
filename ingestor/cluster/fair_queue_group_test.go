package cluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A table dispatched many times as the longest waiter for its peer is not left in debt: once each table has a peer
// with room, the next batch goes out at once while workers are free.
func TestFairQueueGroupDispatchLeavesNoDebt(t *testing.T) {
	var mu sync.Mutex
	groups := map[string]string{"A": "peer", "B": "peer"}
	group := func(b *Batch) string { mu.Lock(); defer mu.Unlock(); return groups[b.Table] }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const cycles = 120
	input := make(chan *Batch, 2*cycles+10)
	out := FairQueue(ctx, input, FairQueueOpts{Workers: 8, Quantum: 100, QueueDepth: 500, MaxRunningPerGroup: 1, Group: group})
	for i := 0; i < cycles+5; i++ {
		input <- fairBatch("A", "a", 100)
		input <- fairBatch("B", "b", 100)
	}
	time.Sleep(20 * time.Millisecond)
	running := receiveFair(t, out)
	for i := 0; i < cycles; i++ {
		running.Release()
		running = receiveFair(t, out)
	}

	mu.Lock()
	groups["A"], groups["B"] = "pA", "pB"
	mu.Unlock()
	running.Release()
	start := time.Now()
	receiveFair(t, out)
	require.Less(t, time.Since(start), 100*time.Millisecond, "the next batch waited to repay credit it was never given")
}

// The table that has waited longest for its peer is served first, even when no worker is free for a while after the
// peer has room.
func TestFairQueueGroupServesLongestWaiterFirst(t *testing.T) {
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		input := make(chan *Batch, 20)
		out := FairQueue(ctx, input, FairQueueOpts{Workers: 8, MaxRunningPerGroup: 1, Group: func(*Batch) string { return "peer" }})
		input <- pathBatch("A", "DB_A", "a", 1)
		running := receiveFair(t, out)
		for _, table := range []string{"B", "C", "D"} {
			input <- pathBatch(table, "DB_"+table, table, 1)
			time.Sleep(time.Duration(1+i%7) * time.Millisecond)
		}
		time.Sleep(15 * time.Millisecond)
		running.Release()
		// No worker takes a batch for a few scheduler ticks.
		time.Sleep(time.Duration(5+i%20) * time.Millisecond)
		require.Equal(t, "B", receiveFair(t, out).Table, "trial %d", i)
		cancel()
	}
}
