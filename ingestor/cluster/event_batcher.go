// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"cmp"
	"context"
	"slices"
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

	// maxLatency is the maximum time a closed segment of prefix waits to be batched with others.
	maxLatency(prefix string) time.Duration

	// sizeTrigger is the size of a prefix's unbatched segments at which they are batched without waiting for
	// maxLatency.
	sizeTrigger() int64

	// maxBatches is the most batches of a prefix that may be outstanding, queued or running, at once, or 0 for no
	// limit.
	maxBatches() int

	// batches splits the unbatched segments of a prefix into at most maxBatches batches, or any number when
	// maxBatches is 0, and marks each segment as part of a batch.  owned batches are uploaded by this node and
	// notOwned batches are transferred to a peer.
	batches(b *batcher, prefix string, segments []wal.SegmentInfo, maxBatches int) (owned, notOwned []*Batch)
}

// eventBatcher batches segments as they close rather than on the periodic scan.  A policy determines which segments
// are batched and how.
type eventBatcher struct {
	b      *batcher
	policy eventPolicy

	retryDelay    time.Duration
	sweepInterval time.Duration

	// maxRetryDelay, when set, doubles retryDelay for each consecutive release of a prefix up to maxRetryDelay.
	maxRetryDelay time.Duration

	// seq is the last sequence number assigned to a pending prefix.  It is guarded by mu.
	seq uint64

	mu sync.Mutex
	// pending tracks prefixes with unbatched segments.
	pending map[string]pendingPrefix
	wake    chan struct{}
	// releases counts consecutive releases of a prefix without a removal.
	releases map[string]int
	// failed holds prefixes backing off because a batch failed, rather than because a queue was full.
	failed map[string]bool
	// backlog holds prefixes with unbatched segments that wait for room to be queued: their last batching made as many
	// batches as the policy allows, or one of their batches was released, such as by a full queue.  They are batched
	// again as soon as one of their batches is delivered.
	backlog map[string]bool
	// outstanding counts each prefix's batches that are queued or running, when the policy limits them.
	outstanding map[string]int

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
		releases:      make(map[string]int),
		failed:        make(map[string]bool),
		backlog:       make(map[string]bool),
		outstanding:   make(map[string]int),
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
	r.markPending(si.Prefix, time.Now().Add(r.policy.maxLatency(si.Prefix)))
}

// released is called when a batch is released without being removed so its segments are retried.  A batch that asked
// to be retried after retryAfter, such as one its peer was briefly too busy to accept, is retried then.  Otherwise the
// retry backs off for consecutive releases of prefix.
func (r *eventBatcher) released(prefix string, retryAfter time.Duration) {
	r.release(prefix, retryAfter, true)
}

// queueFull is called when a batch is released unsent because its destination's queue was full.  It is retried like
// a failed batch, but its delivery later does not mean that a failing destination recovered.
func (r *eventBatcher) queueFull(prefix string) {
	r.release(prefix, 0, false)
}

func (r *eventBatcher) release(prefix string, retryAfter time.Duration, failed bool) {
	// The released segments wait for the retry, or for one of the prefix's batches to be delivered.  The failure is
	// recorded and the retry set under one hold of mu, so a delivery that recovers the destination in between
	// either sees the failure and retries the prefix, or finds the retry already set and clears it.
	r.mu.Lock()
	r.backlog[prefix] = true
	var at time.Time
	if retryAfter > 0 {
		delete(r.failed, prefix)
		at = time.Now().Add(retryAfter)
	} else {
		delay := r.retryDelay
		if r.maxRetryDelay > 0 {
			n := r.releases[prefix]
			r.releases[prefix] = n + 1
			if failed {
				r.failed[prefix] = true
			}
			for i := 0; i < n && delay < r.maxRetryDelay; i++ {
				delay *= 2
			}
			delay = min(delay, r.maxRetryDelay)
		}
		at = time.Now().Add(delay)
	}
	r.markPendingLocked(prefix, at, at)
	r.mu.Unlock()
	r.signal()
}

// removed resets the retry backoff of prefix after one of its batches is delivered.  A prefix with a backlog is batched
// again at once, without waiting for a retry, since its destination is accepting its data and the delivered batch made
// room for another.
//
// A delivery of a prefix whose batches had failed means its destination recovered, such as after an outage of the
// ingestor or ADX, so every other failing prefix is retried at once too rather than after its own backoff of up to
// maxRetryDelay.  A prefix that fails on its own is retried early only when another failing prefix recovers.
func (r *eventBatcher) removed(prefix string) {
	r.mu.Lock()
	recovered := r.failed[prefix]
	delete(r.releases, prefix)
	delete(r.failed, prefix)
	wake := r.backlog[prefix]
	if wake {
		r.retryNow(prefix)
	}
	if recovered {
		for p := range r.failed {
			delete(r.failed, p)
			delete(r.releases, p)
			r.retryNow(p)
			wake = true
		}
	}
	r.mu.Unlock()

	if wake {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

// retryNow marks prefix to be batched at once, clearing any retry backoff.  r.mu must be held.
func (r *eventBatcher) retryNow(prefix string) {
	r.seq++
	r.pending[prefix] = pendingPrefix{deadline: time.Now(), seq: r.seq}
}

// pendingPrefix is a prefix with unbatched segments.
type pendingPrefix struct {
	// deadline is when the prefix's segments must be batched.
	deadline time.Time
	// retryAfter prevents normal batching triggers from immediately re-emitting a released batch.
	retryAfter time.Time
	// seq changes each time the prefix is marked pending so segments that arrive while a batch is being built are not
	// lost when the batch clears the prefix.
	seq uint64
}

// markPending records that prefix has unbatched segments that must be batched by deadline.  An earlier deadline is
// kept.
func (r *eventBatcher) markPending(prefix string, deadline time.Time) {
	r.markPendingWithRetry(prefix, deadline, time.Time{})
}

// markPendingWithRetry records a pending prefix and, when retryAfter is set, delays normal batching until that time.
func (r *eventBatcher) markPendingWithRetry(prefix string, deadline, retryAfter time.Time) {
	r.mu.Lock()
	r.markPendingLocked(prefix, deadline, retryAfter)
	r.mu.Unlock()
	r.signal()
}

// markPendingLocked is markPendingWithRetry for a caller that holds mu; the caller signals the run loop afterwards.
func (r *eventBatcher) markPendingLocked(prefix string, deadline, retryAfter time.Time) {
	r.seq++
	p, ok := r.pending[prefix]
	if !ok || deadline.Before(p.deadline) {
		p.deadline = deadline
	}
	if !retryAfter.IsZero() {
		p.retryAfter = retryAfter
	}
	p.seq = r.seq
	r.pending[prefix] = p
}

// signal wakes the run loop to recompute its next deadline.
func (r *eventBatcher) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// sweep marks every prefix in the index with unbatched segments accepted by the policy as pending.  Prefixes whose
// segments are all part of a batch are skipped so they are not reprocessed.
func (r *eventBatcher) sweep() {
	now := time.Now()
	var segments []wal.SegmentInfo
	for _, prefix := range r.prefixes() {
		segments = r.b.Segmenter.Get(segments[:0], prefix)
		if len(segments) > 0 && r.policy.accepts(segments[0]) && r.hasUnbatched(segments) {
			r.markPending(prefix, now.Add(r.policy.maxLatency(prefix)))
		}
	}
}

// prefixLister is implemented by segmenters that list prefixes without sorting them.
type prefixLister interface {
	Prefixes(dst []string) []string
}

// prefixes returns the prefixes in the index.  The order does not matter since prefixes are only marked pending.
func (r *eventBatcher) prefixes() []string {
	if l, ok := r.b.Segmenter.(prefixLister); ok {
		return l.Prefixes(nil)
	}
	return r.b.Segmenter.PrefixesByAge()
}

// hasUnbatched returns true if any of the segments is not part of a batch.
func (r *eventBatcher) hasUnbatched(segments []wal.SegmentInfo) bool {
	for _, si := range segments {
		if n, _ := r.b.segments.Get(si.Path); n == 0 {
			return true
		}
	}
	return false
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

// nextDeadline returns the earliest time a pending prefix can be batched: its deadline, or its retryAfter when that is
// later.  processPrefix skips a prefix until retryAfter, so waking for an earlier deadline would only reset an expired
// timer over and over until the backoff ends.
func (r *eventBatcher) nextDeadline() (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var (
		next time.Time
		ok   bool
	)
	for _, p := range r.pending {
		due := p.deadline
		if p.retryAfter.After(due) {
			due = p.retryAfter
		}
		if !ok || due.Before(next) {
			next, ok = due, true
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
	type entry struct {
		prefix string
		p      pendingPrefix
	}
	r.mu.Lock()
	pending := make([]entry, 0, len(r.pending))
	for prefix, p := range r.pending {
		pending = append(pending, entry{prefix, p})
	}
	r.mu.Unlock()

	// The most overdue prefixes go first so they are not queued behind newer data when the queues are full.
	slices.SortFunc(pending, func(a, b entry) int {
		if c := a.p.deadline.Compare(b.p.deadline); c != 0 {
			return c
		}
		return cmp.Compare(a.p.seq, b.p.seq)
	})
	for _, e := range pending {
		r.processPrefix(ctx, e.prefix, now, force)
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
	if !force && now.Before(p.retryAfter) {
		return
	}

	// Read the pending state before the segments so a segment that closes afterwards keeps the prefix pending.  The
	// segments are read and marked as batched, and the backlog flag set from them, under markMu, but the batches are
	// sent without it, since a full queue waits for workers that remove and release batches.
	r.b.markMu.Lock()
	segments := r.unbatched(prefix)
	if len(segments) == 0 {
		// The backlog flag is set before markMu is released, so a batch released after this read, which sets it,
		// isn't overwritten by this stale result.
		r.setBacklog(prefix, false)
		r.b.markMu.Unlock()
		r.clearPending(prefix, p.seq)
		return
	}

	var size int64
	for _, si := range segments {
		size += si.Size
	}
	if !force && size < r.policy.sizeTrigger() && now.Before(p.deadline) {
		r.b.markMu.Unlock()
		return
	}

	r.clearPending(prefix, p.seq)
	room, ok := r.room(prefix)
	if !ok {
		// Every batch the prefix may have is still outstanding.  The prefix is batched again when one finishes.
		r.b.markMu.Unlock()
		return
	}
	owned, notOwned := r.policy.batches(r.b, prefix, segments, room)
	r.track(prefix, owned)
	r.track(prefix, notOwned)
	r.setBacklog(prefix, batchedSegments(owned)+batchedSegments(notOwned) < len(segments))
	r.b.markMu.Unlock()
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

// room returns how many more batches of prefix may be made, or 0 when the policy sets no limit.  It returns false,
// and marks the prefix as having a backlog, when the prefix already has as many batches outstanding as the policy
// allows; the backlog flag is set under the same hold of mu as the count is read, so a batch that finishes in between
// sees it and batches the prefix again.
func (r *eventBatcher) room(prefix string) (int, bool) {
	limit := r.policy.maxBatches()
	if limit <= 0 {
		return 0, true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	room := limit - r.outstanding[prefix]
	if room <= 0 {
		r.backlog[prefix] = true
		return 0, false
	}
	return room, true
}

// track counts batches of prefix as outstanding until each finishes, when the policy limits them.
func (r *eventBatcher) track(prefix string, batches []*Batch) {
	if r.policy.maxBatches() <= 0 || len(batches) == 0 {
		return
	}
	r.mu.Lock()
	r.outstanding[prefix] += len(batches)
	r.mu.Unlock()
	for _, b := range batches {
		b.done = func() { r.batchDone(prefix) }
	}
}

// batchDone is called when an outstanding batch of prefix finishes, by being released or merged into another batch.
// It makes room for another batch, so a prefix with a backlog is batched again at once, unless it is backing off
// because a batch was released for retry, such as after a failed delivery.
func (r *eventBatcher) batchDone(prefix string) {
	r.mu.Lock()
	if r.outstanding[prefix]--; r.outstanding[prefix] <= 0 {
		delete(r.outstanding, prefix)
	}
	wake := r.backlog[prefix]
	if p, ok := r.pending[prefix]; ok && p.retryAfter.After(time.Now()) {
		wake = false
	}
	if wake {
		r.retryNow(prefix)
	}
	r.mu.Unlock()
	if wake {
		r.signal()
	}
}

// setBacklog records whether prefix has segments left unbatched by the batches limit.
func (r *eventBatcher) setBacklog(prefix string, backlog bool) {
	r.mu.Lock()
	if backlog {
		r.backlog[prefix] = true
	} else {
		delete(r.backlog, prefix)
	}
	r.mu.Unlock()
}

// batchedSegments returns the number of segments in batches.
func batchedSegments(batches []*Batch) int {
	n := 0
	for _, b := range batches {
		n += len(b.Segments)
	}
	return n
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
