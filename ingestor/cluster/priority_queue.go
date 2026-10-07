// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"context"
)

// DefaultQueuedReservedWorkersPercent is the default percentage of workers reserved for queued batches.
const DefaultQueuedReservedWorkersPercent = 10

// PriorityQueues are the work queues for realtime and queued batches.
type PriorityQueues struct {
	Realtime chan *Batch
	Queued   chan *Batch
}

// next returns the next batch to process.  Reserved workers only process queued batches so realtime batches cannot
// starve them.  Other workers process realtime batches before queued batches.  It returns false when ctx is done.
func (q PriorityQueues) next(ctx context.Context, reserved bool) (*Batch, bool) {
	if reserved {
		select {
		case <-ctx.Done():
			return nil, false
		case b := <-q.Queued:
			return b, true
		}
	}

	// Prefer realtime batches when both are ready.
	select {
	case b := <-q.Realtime:
		return b, true
	default:
	}

	select {
	case <-ctx.Done():
		return nil, false
	case b := <-q.Realtime:
		return b, true
	case b := <-q.Queued:
		return b, true
	}
}

// ReservedWorkers returns how many of n workers are reserved for queued batches.  At least one worker is reserved
// and at least one worker is left to process realtime batches.  A single worker is never reserved.
func ReservedWorkers(n, percent int) int {
	if n <= 1 {
		return 0
	}
	if percent <= 0 {
		percent = DefaultQueuedReservedWorkersPercent
	}
	reserved := (n*percent + 99) / 100
	return max(1, min(reserved, n-1))
}

// RunWorkers starts n workers that call fn for each batch from q until ctx is done.  The first ReservedWorkers(n,
// percent) workers only process queued batches.  done is called when each worker exits.
func RunWorkers(ctx context.Context, q PriorityQueues, n, percent int, fn func(*Batch), done func()) {
	reserved := ReservedWorkers(n, percent)
	for i := 0; i < n; i++ {
		go func(reserved bool) {
			defer done()
			for {
				b, ok := q.next(ctx, reserved)
				if !ok {
					return
				}
				fn(b)
			}
		}(i < reserved)
	}
}
