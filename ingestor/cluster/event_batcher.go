// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
)

// defaultEventRetryDelay is how long a prefix waits before its segments are batched again after a batch is released
// without being removed, such as after a failed upload.
const defaultEventRetryDelay = time.Second

// segmentSubscriber is implemented by segmenters that notify when segments close.
type segmentSubscriber interface {
	Subscribe(fn func(wal.SegmentInfo)) (unsubscribe func())
}

// eventPolicy determines which segments an eventBatcher batches and how.
type eventPolicy interface {
	// accepts returns true if the event batcher batches the segment.
	accepts(si wal.SegmentInfo) bool

	// maxLatency is the maximum time a closed segment waits to be batched with others.
	maxLatency() time.Duration

	// sizeTrigger is the size of a prefix's unbatched segments at which they are batched without waiting for
	// maxLatency.
	sizeTrigger() int64

	// batches splits the unbatched segments of a prefix into batches and marks each segment as part of a batch.
	// owned batches are uploaded by this node and notOwned batches are transferred to a peer.
	batches(b *batcher, prefix string, segments []wal.SegmentInfo) (owned, notOwned []*Batch)
}

// eventBatcher batches segments as they close rather than on the periodic scan.  A policy determines which segments
// are batched and how.
type eventBatcher struct {
	b      *batcher
	policy eventPolicy

	retryDelay    time.Duration
	sweepInterval time.Duration

	// seq is the last sequence number assigned to a pending prefix.  It is guarded by mu.
	seq uint64

	mu sync.Mutex
	// pending tracks prefixes with unbatched segments.
	pending map[string]pendingPrefix
	wake    chan struct{}

	// emitMu serializes building batches so segments are never added to more than one batch.
	emitMu  sync.Mutex
	tempSet []wal.SegmentInfo

	unsubscribe func()
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func newEventBatcher(b *batcher, policy eventPolicy, sweepInterval time.Duration) *eventBatcher {
	return &eventBatcher{
		b:             b,
		policy:        policy,
		retryDelay:    defaultEventRetryDelay,
		sweepInterval: sweepInterval,
		pending:       make(map[string]pendingPrefix),
		wake:          make(chan struct{}, 1),
	}
}

func (r *eventBatcher) Open(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)

	// Subscribe before sweeping so segments that close in between are not missed.
	if sub, ok := r.b.Segmenter.(segmentSubscriber); ok {
		r.unsubscribe = sub.Subscribe(r.onSegment)
	}
	r.sweep()

	r.wg.Add(1)
	go r.run(ctx)
}

func (r *eventBatcher) Close() {
	if r.unsubscribe != nil {
		r.unsubscribe()
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

// onSegment is called when a segment closes.  It must not block.
func (r *eventBatcher) onSegment(si wal.SegmentInfo) {
	if !r.policy.accepts(si) {
		return
	}
	r.markPending(si.Prefix, time.Now().Add(r.policy.maxLatency()))
}

// released is called when a batch is released without being removed so its segments are retried.
func (r *eventBatcher) released(prefix string) {
	r.markPending(prefix, time.Now().Add(r.retryDelay))
}

// pendingPrefix is a prefix with unbatched segments.
type pendingPrefix struct {
	// deadline is when the prefix's segments must be batched.
	deadline time.Time
	// seq changes each time the prefix is marked pending so segments that arrive while a batch is being built are not
	// lost when the batch clears the prefix.
	seq uint64
}

// markPending records that prefix has unbatched segments that must be batched by deadline.  An earlier deadline is
// kept.
func (r *eventBatcher) markPending(prefix string, deadline time.Time) {
	r.mu.Lock()
	r.seq++
	p, ok := r.pending[prefix]
	if !ok || deadline.Before(p.deadline) {
		p.deadline = deadline
	}
	p.seq = r.seq
	r.pending[prefix] = p
	r.mu.Unlock()

	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// sweep marks every prefix in the index with segments accepted by the policy as pending.
func (r *eventBatcher) sweep() {
	deadline := time.Now().Add(r.policy.maxLatency())
	var segments []wal.SegmentInfo
	for _, prefix := range r.b.Segmenter.PrefixesByAge() {
		segments = r.b.Segmenter.Get(segments[:0], prefix)
		if len(segments) > 0 && r.policy.accepts(segments[0]) {
			r.markPending(prefix, deadline)
		}
	}
}

func (r *eventBatcher) run(ctx context.Context) {
	defer r.wg.Done()

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	sweep := time.NewTicker(r.sweepInterval)
	defer sweep.Stop()

	for {
		if deadline, ok := r.nextDeadline(); ok {
			timer.Reset(time.Until(deadline))
		}

		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-timer.C:
		case <-sweep.C:
			r.sweep()
		}
		timer.Stop()

		r.process(ctx, time.Now(), false)
	}
}

// nextDeadline returns the earliest pending deadline.
func (r *eventBatcher) nextDeadline() (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var (
		next time.Time
		ok   bool
	)
	for _, p := range r.pending {
		if !ok || p.deadline.Before(next) {
			next, ok = p.deadline, true
		}
	}
	return next, ok
}

// Flush batches all segments accepted by the policy immediately.
func (r *eventBatcher) Flush(ctx context.Context) {
	r.sweep()
	r.process(ctx, time.Now(), true)
}

// process batches the segments of pending prefixes that reached their deadline or size trigger.  When force is true,
// all pending prefixes are batched.
func (r *eventBatcher) process(ctx context.Context, now time.Time, force bool) {
	r.mu.Lock()
	prefixes := make([]string, 0, len(r.pending))
	for prefix := range r.pending {
		prefixes = append(prefixes, prefix)
	}
	r.mu.Unlock()

	for _, prefix := range prefixes {
		r.processPrefix(ctx, prefix, now, force)
	}
}

func (r *eventBatcher) processPrefix(ctx context.Context, prefix string, now time.Time, force bool) {
	r.emitMu.Lock()
	defer r.emitMu.Unlock()

	r.mu.Lock()
	p, ok := r.pending[prefix]
	r.mu.Unlock()
	if !ok && !force {
		return
	}

	// Read the pending state before the segments so a segment that closes afterwards keeps the prefix pending.
	segments := r.unbatched(prefix)
	if len(segments) == 0 {
		r.clearPending(prefix, p.seq)
		return
	}

	var size int64
	for _, si := range segments {
		size += si.Size
	}
	if !force && size < r.policy.sizeTrigger() && now.Before(p.deadline) {
		return
	}

	r.clearPending(prefix, p.seq)
	owned, notOwned := r.policy.batches(r.b, prefix, segments)
	r.send(ctx, owned, r.b.uploadQueueFor)
	r.send(ctx, notOwned, r.b.transferQueueFor)
}

// send sends batches to the queue for their priority.  If ctx is done, unsent batches are released so their segments
// are batched again.
func (r *eventBatcher) send(ctx context.Context, batches []*Batch, queueFor func(ingestpolicy.Priority) chan *Batch) {
	for i, batch := range batches {
		select {
		case queueFor(batch.Priority) <- batch:
		case <-ctx.Done():
			for _, unsent := range batches[i:] {
				unsent.Release()
			}
			return
		}
	}
}

// clearPending removes prefix from pending unless it was marked pending again after seq was read.
func (r *eventBatcher) clearPending(prefix string, seq uint64) {
	r.mu.Lock()
	if p, ok := r.pending[prefix]; ok && p.seq == seq {
		delete(r.pending, prefix)
	}
	r.mu.Unlock()
}

// unbatched returns the segments of prefix that are not part of a batch, sorted by path.  emitMu must be held.
func (r *eventBatcher) unbatched(prefix string) []wal.SegmentInfo {
	r.tempSet = r.b.Segmenter.Get(r.tempSet[:0], prefix)
	segments := r.tempSet[:0]
	for _, si := range r.tempSet {
		if n, _ := r.b.segments.Get(si.Path); n == 0 {
			segments = append(segments, si)
		}
	}
	sortSegmentsByPath(segments)
	return segments
}

func sortSegmentsByPath(segments []wal.SegmentInfo) {
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Path < segments[j].Path
	})
}
