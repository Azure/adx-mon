// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
)

const (
	// defaultRealtimeRetryDelay is how long a realtime prefix waits before its segments are batched again after a
	// batch is released without being removed, such as after a failed upload.
	defaultRealtimeRetryDelay = time.Second

	// defaultRealtimeSweepInterval is how often the index is scanned for realtime segments that were not batched in
	// response to an event.
	defaultRealtimeSweepInterval = 5 * time.Second
)

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

// segmentSubscriber is implemented by segmenters that notify when segments close.
type segmentSubscriber interface {
	Subscribe(fn func(wal.SegmentInfo)) (unsubscribe func())
}

// realtimeBatcher batches realtime segments as they close rather than on the periodic scan.  Realtime batches are
// always uploaded by this node and are never transferred to peers.
type realtimeBatcher struct {
	b    *batcher
	opts RealtimeBatchOpts

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

func newRealtimeBatcher(b *batcher, opts RealtimeBatchOpts) *realtimeBatcher {
	return &realtimeBatcher{
		b:             b,
		opts:          opts,
		retryDelay:    defaultRealtimeRetryDelay,
		sweepInterval: defaultRealtimeSweepInterval,
		pending:       make(map[string]pendingPrefix),
		wake:          make(chan struct{}, 1),
	}
}

func (r *realtimeBatcher) Open(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)

	// Subscribe before sweeping so segments that close in between are not missed.
	if sub, ok := r.b.Segmenter.(segmentSubscriber); ok {
		r.unsubscribe = sub.Subscribe(r.onSegment)
	}
	r.sweep()

	r.wg.Add(1)
	go r.run(ctx)
}

func (r *realtimeBatcher) Close() {
	if r.unsubscribe != nil {
		r.unsubscribe()
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

// onSegment is called when a segment closes.  It must not block.
func (r *realtimeBatcher) onSegment(si wal.SegmentInfo) {
	if si.Priority != ingestpolicy.PriorityRealtime {
		return
	}
	r.markPending(si.Prefix, time.Now().Add(r.opts.MaxBatchLatency))
}

// released is called when a realtime batch is released without being removed so its segments are retried.
func (r *realtimeBatcher) released(prefix string) {
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
func (r *realtimeBatcher) markPending(prefix string, deadline time.Time) {
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

// sweep marks every realtime prefix in the index with unbatched segments as pending.
func (r *realtimeBatcher) sweep() {
	deadline := time.Now().Add(r.opts.MaxBatchLatency)
	var segments []wal.SegmentInfo
	for _, prefix := range r.b.Segmenter.PrefixesByAge() {
		segments = r.b.Segmenter.Get(segments[:0], prefix)
		if len(segments) > 0 && segments[0].Priority == ingestpolicy.PriorityRealtime {
			r.markPending(prefix, deadline)
		}
	}
}

func (r *realtimeBatcher) run(ctx context.Context) {
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
func (r *realtimeBatcher) nextDeadline() (time.Time, bool) {
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

// Flush batches all realtime segments immediately.
func (r *realtimeBatcher) Flush(ctx context.Context) {
	r.sweep()
	r.process(ctx, time.Now(), true)
}

// process batches the segments of pending prefixes that reached their deadline or MaxBatchBytes.  When force is true,
// all pending prefixes are batched.
func (r *realtimeBatcher) process(ctx context.Context, now time.Time, force bool) {
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

func (r *realtimeBatcher) processPrefix(ctx context.Context, prefix string, now time.Time, force bool) {
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
	if !force && size < r.opts.MaxBatchBytes && now.Before(p.deadline) {
		return
	}

	r.clearPending(prefix, p.seq)
	batches := r.newBatches(prefix, segments)
	for i, batch := range batches {
		select {
		case r.b.uploadQueueFor(ingestpolicy.PriorityRealtime) <- batch:
		case <-ctx.Done():
			// Release unsent batches so their segments are batched again.
			for _, unsent := range batches[i:] {
				unsent.Release()
			}
			return
		}
	}
}

// clearPending removes prefix from pending unless it was marked pending again after seq was read.
func (r *realtimeBatcher) clearPending(prefix string, seq uint64) {
	r.mu.Lock()
	if p, ok := r.pending[prefix]; ok && p.seq == seq {
		delete(r.pending, prefix)
	}
	r.mu.Unlock()
}

// unbatched returns the segments of prefix that are not part of a batch, sorted by path.  emitMu must be held.
func (r *realtimeBatcher) unbatched(prefix string) []wal.SegmentInfo {
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

// newBatches splits segments into batches of at most MaxBatchBytes and maxBatchSegments and marks each segment as
// part of a batch.  emitMu must be held.
func (r *realtimeBatcher) newBatches(prefix string, segments []wal.SegmentInfo) []*Batch {
	db, table, _, _, err := wal.ParseFilename(segments[0].Path)
	if err != nil {
		logger.Errorf("Failed to parse segment filename: %s", err)
		return nil
	}

	var (
		batches []*Batch
		batch   *Batch
		size    int64
	)
	for _, si := range segments {
		full := batch != nil && (size+si.Size > r.opts.MaxBatchBytes || len(batch.Segments) >= r.b.maxBatchSegments)
		if batch == nil || full {
			batch = &Batch{
				Prefix:   prefix,
				Database: db,
				Table:    table,
				Priority: ingestpolicy.PriorityRealtime,
				batcher:  r.b,
			}
			batches = append(batches, batch)
			size = 0
		}
		batch.Segments = append(batch.Segments, si)
		size += si.Size

		_ = r.b.segments.Mutate(si.Path, func(n int) (int, error) {
			return n + 1, nil
		})
	}
	return batches
}

func sortSegmentsByPath(segments []wal.SegmentInfo) {
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Path < segments[j].Path
	})
}
