// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
)

// defaultRealtimeSweepInterval is how often the index is scanned for realtime segments that were not batched in
// response to an event.
const defaultRealtimeSweepInterval = 5 * time.Second

// RealtimeBatchOpts configures event driven batching of realtime segments.
type RealtimeBatchOpts struct {
	// MaxBatchLatency is the maximum time a closed realtime segment waits to be batched with others.
	MaxBatchLatency time.Duration

	// MaxBatchBytes is the maximum size of a realtime batch.  A single larger segment is batched alone.
	MaxBatchBytes int64
}

func (o RealtimeBatchOpts) enabled() bool {
	return o.MaxBatchLatency > 0 && o.MaxBatchBytes > 0
}

// realtimePolicy batches realtime segments as they close.
type realtimePolicy struct {
	opts RealtimeBatchOpts
}

func newRealtimeBatcher(b *batcher, opts RealtimeBatchOpts) *eventBatcher {
	return newEventBatcher(b, realtimePolicy{opts: opts}, defaultRealtimeSweepInterval)
}

func (p realtimePolicy) accepts(si wal.SegmentInfo) bool {
	return si.Priority == ingestpolicy.PriorityRealtime
}

func (p realtimePolicy) maxLatency() time.Duration {
	return p.opts.MaxBatchLatency
}

func (p realtimePolicy) sizeTrigger() int64 {
	return p.opts.MaxBatchBytes
}

// batches splits segments into batches of at most MaxBatchBytes and maxBatchSegments and marks each segment as part of
// a batch.  Realtime batches are always uploaded by this node and never transferred to peers.
func (p realtimePolicy) batches(b *batcher, prefix string, segments []wal.SegmentInfo) (owned, notOwned []*Batch) {
	db, table, _, _, err := wal.ParseFilename(segments[0].Path)
	if err != nil {
		logger.Errorf("Failed to parse segment filename: %s", err)
		return nil, nil
	}

	var (
		batches []*Batch
		batch   *Batch
		size    int64
	)
	for _, si := range segments {
		full := batch != nil && (size+si.Size > p.opts.MaxBatchBytes || len(batch.Segments) >= b.maxBatchSegments)
		if batch == nil || full {
			batch = &Batch{
				Prefix:   prefix,
				Database: db,
				Table:    table,
				Priority: ingestpolicy.PriorityRealtime,
				batcher:  b,
			}
			batches = append(batches, batch)
			size = 0
		}
		batch.Segments = append(batch.Segments, si)
		size += si.Size

		_ = b.segments.Mutate(si.Path, func(n int) (int, error) {
			return n + 1, nil
		})
	}
	return batches, nil
}
