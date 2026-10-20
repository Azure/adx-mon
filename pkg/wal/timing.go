package wal

import (
	"sync"
	"time"
)

// SegmentTiming is shared by copies of SegmentInfo, so history survives rebatching without a separate lookup map.
// It is process-local. Recovered segments retain their creation time but not their detailed stage history.
type SegmentTiming struct {
	mu sync.Mutex

	createdAt, availableAt, activeAt               time.Time
	buffer, batching, retryWait, queue, processing time.Duration
	attempts                                       int
	incomplete                                     string
	completed                                      bool
}

type SegmentDurations struct {
	Total, Buffer, Batching, RetryWait, Queue, Processing time.Duration
	Retries                                               int
	Incomplete                                            string
}

func NewSegmentTiming(createdAt, readyAt time.Time, recovered bool) *SegmentTiming {
	buffer := readyAt.Sub(createdAt)
	// Anchor the persisted wall-clock creation time to the publication clock so later elapsed time is monotonic.
	t := &SegmentTiming{createdAt: readyAt.Add(-buffer), availableAt: readyAt, buffer: buffer}
	if recovered {
		t.incomplete = "recovered"
	}
	if createdAt.IsZero() || readyAt.Before(createdAt) {
		t.incomplete = "invalid_clock"
	}
	if createdAt.IsZero() {
		t.createdAt = time.Time{}
	}
	return t
}

func (t *SegmentTiming) Start(queuedAt, startedAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.completed {
		return
	}
	if !t.activeAt.IsZero() {
		t.incomplete = "overlapping_attempts"
		return
	}
	if queuedAt.IsZero() {
		t.incomplete = "missing_enqueue"
	} else if queuedAt.Before(t.availableAt) || startedAt.Before(queuedAt) {
		t.incomplete = "invalid_clock"
	} else {
		wait := queuedAt.Sub(t.availableAt)
		if t.attempts == 0 {
			t.batching += wait
		} else {
			t.retryWait += wait
		}
		t.queue += startedAt.Sub(queuedAt)
	}
	t.attempts++
	t.activeAt = startedAt
}

func (t *SegmentTiming) Release(at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.completed || t.activeAt.IsZero() {
		return
	}
	t.finishAttempt(at)
	t.availableAt = at
}

func (t *SegmentTiming) finishAttempt(at time.Time) {
	if at.Before(t.activeAt) {
		t.incomplete = "invalid_clock"
	} else {
		t.processing += at.Sub(t.activeAt)
	}
	t.activeAt = time.Time{}
}

// Complete returns a snapshot exactly once, at successful handoff rather than later disk cleanup.
func (t *SegmentTiming) Complete(at time.Time) (SegmentDurations, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.completed {
		return SegmentDurations{}, false
	}
	t.completed = true
	if t.activeAt.IsZero() {
		t.incomplete = "missing_start"
	} else {
		t.finishAttempt(at)
	}
	if at.Before(t.createdAt) {
		t.incomplete = "invalid_clock"
	}
	total := at.Sub(t.createdAt)
	if t.createdAt.IsZero() {
		t.incomplete = "invalid_clock"
		total = -1
	}
	return SegmentDurations{
		Total: total, Buffer: t.buffer, Batching: t.batching,
		RetryWait: t.retryWait, Queue: t.queue, Processing: t.processing,
		Retries: max(0, t.attempts-1), Incomplete: t.incomplete,
	}, true
}
