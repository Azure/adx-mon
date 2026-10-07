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
}

func newQueuedBatcher(b *batcher, linger time.Duration) *eventBatcher {
	return newEventBatcher(b, queuedPolicy{b: b, linger: linger}, queuedSweepInterval)
}

// accepts returns true for queued segments and for realtime segments when realtime segments are not batched by the
// realtime batcher.
func (p queuedPolicy) accepts(si wal.SegmentInfo) bool {
	return si.Priority != ingestpolicy.PriorityRealtime || p.b.realtime == nil
}

func (p queuedPolicy) maxLatency() time.Duration {
	return p.linger
}

// sizeTrigger is the upload size at which the scan splits a batch, so a full batch is not delayed.
func (p queuedPolicy) sizeTrigger() int64 {
	return p.b.minUploadSize
}

func (p queuedPolicy) batches(b *batcher, prefix string, segments []wal.SegmentInfo) (owned, notOwned []*Batch) {
	return b.splitPrefix(prefix, segments)
}
