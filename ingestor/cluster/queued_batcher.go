// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
)

const (
	// DefaultQueuedLinger is the default time a closed queued segment waits to be batched with others.  It matches
	// the interval of the periodic scan it replaces so batches are similar.
	DefaultQueuedLinger = 5 * time.Second

	// queuedSweepInterval is how often the index is scanned for queued segments that were not batched in response to
	// an event.
	queuedSweepInterval = time.Minute
)

// queuedPolicy batches queued segments as they close using the same splitting and ownership rules as the scan.
type queuedPolicy struct {
	b      *batcher
	linger time.Duration

	// transferLinger, when positive, replaces linger for prefixes owned by another node, whose batches are transferred
	// rather than uploaded.  Transfers are merged while they wait for a worker, so they need not linger as long to
	// be combined.
	transferLinger time.Duration
}

func newQueuedBatcher(b *batcher, linger, transferLinger time.Duration) *eventBatcher {
	r := newEventBatcher(b, queuedPolicy{b: b, linger: linger, transferLinger: transferLinger}, queuedSweepInterval)
	// A prefix that keeps failing, such as while ADX or the peer is unavailable, backs off to the sweep interval so
	// it is retried no more often than before retries were event driven.
	r.maxRetryDelay = queuedSweepInterval
	return r
}

// accepts returns true for queued segments and for realtime segments when realtime segments are not batched by the
// realtime batcher.
func (p queuedPolicy) accepts(si wal.SegmentInfo) bool {
	return si.Priority != ingestpolicy.PriorityRealtime || p.b.realtime == nil
}

func (p queuedPolicy) maxLatency(prefix string) time.Duration {
	if p.transferLinger > 0 && !p.b.transferDisabled {
		if owner, _ := p.b.Partitioner.Owner([]byte(prefix)); owner != p.b.hostname {
			return p.transferLinger
		}
	}
	return p.linger
}

// sizeTrigger is the upload size at which the scan splits a batch, so a full batch is not delayed.
func (p queuedPolicy) sizeTrigger() int64 {
	return p.b.minUploadSize
}

// maxBatches is maxTableBatches, the most batches a table may have queued, so a backlog is batched as earlier batches
// are delivered rather than released by a full queue.
func (p queuedPolicy) maxBatches() int {
	return p.b.maxTableBatches
}

// batches makes at most maxBatches batches, the room left by the prefix's outstanding batches.
func (p queuedPolicy) batches(b *batcher, prefix string, segments []wal.SegmentInfo, maxBatches int) (owned, notOwned []*Batch) {
	return b.splitPrefixN(prefix, segments, maxBatches)
}
