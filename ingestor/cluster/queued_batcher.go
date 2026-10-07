// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"fmt"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
)

// BatcherMode selects how queued segments are batched.
type BatcherMode string

const (
	// BatcherModeScan batches queued segments by periodically scanning all segments.
	BatcherModeScan BatcherMode = "scan"

	// BatcherModeEvent batches queued segments as they close.
	BatcherModeEvent BatcherMode = "event"
)

const (
	// DefaultQueuedLinger is the default time a closed queued segment waits to be batched with others in
	// BatcherModeEvent.  It matches the scan interval so batches are similar in both modes.
	DefaultQueuedLinger = 5 * time.Second

	// queuedSweepInterval is how often the index is scanned for queued segments that were not batched in response to
	// an event.
	queuedSweepInterval = time.Minute
)

// ParseBatcherMode returns the batcher mode for s.  An empty string is BatcherModeScan.
func ParseBatcherMode(s string) (BatcherMode, error) {
	switch BatcherMode(s) {
	case "", BatcherModeScan:
		return BatcherModeScan, nil
	case BatcherModeEvent:
		return BatcherModeEvent, nil
	default:
		return "", fmt.Errorf("invalid batcher mode %q: must be %q or %q", s, BatcherModeScan, BatcherModeEvent)
	}
}

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
