// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package wal

import (
	"container/heap"
	"context"
	"runtime"
	"sync"
	"time"
)

// defaultRotationSweepInterval is how often every WAL is checked for rotation in addition to its scheduled deadline.
// The sweep catches rotations that are not driven by age, such as a segment that exceeded its max size without a
// subsequent write.
const defaultRotationSweepInterval = 10 * time.Second

// minRotationConcurrency is the minimum number of segments rotated concurrently.  Closing a segment flushes and
// optionally fsyncs it so rotations are run in parallel to keep many WALs with the same deadline on schedule.
const minRotationConcurrency = 8

// rotationScheduler rotates WAL segments when they reach their max age.  Deadlines are kept in a min-heap so a single
// goroutine and timer can serve any number of WALs.
//
// Lock ordering: callers may hold WAL.mu when calling schedule or unschedule.  The scheduler never calls into a WAL
// while holding its own lock.
type rotationScheduler struct {
	sweepInterval time.Duration
	sweep         func()
	concurrency   int

	mu      sync.Mutex
	entries rotationHeap
	wake    chan struct{}

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// newRotationScheduler returns a scheduler that also calls sweep every sweepInterval.
func newRotationScheduler(sweepInterval time.Duration, sweep func()) *rotationScheduler {
	return &rotationScheduler{
		sweepInterval: sweepInterval,
		sweep:         sweep,
		concurrency:   max(minRotationConcurrency, runtime.GOMAXPROCS(0)),
		wake:          make(chan struct{}, 1),
	}
}

func (s *rotationScheduler) Open(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.run(ctx)
}

func (s *rotationScheduler) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// schedule sets the rotation deadline for w, replacing any existing deadline.
func (s *rotationScheduler) schedule(w *WAL, deadline time.Time) {
	s.mu.Lock()
	if w.rotationIndex >= 0 {
		w.rotationDeadline = deadline
		heap.Fix(&s.entries, w.rotationIndex)
	} else {
		w.rotationDeadline = deadline
		heap.Push(&s.entries, w)
	}
	earliest := s.entries[0] == w
	s.mu.Unlock()

	if earliest {
		s.notify()
	}
}

// unschedule removes any rotation deadline for w.
func (s *rotationScheduler) unschedule(w *WAL) {
	s.mu.Lock()
	if w.rotationIndex >= 0 {
		heap.Remove(&s.entries, w.rotationIndex)
	}
	s.mu.Unlock()
}

func (s *rotationScheduler) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// next returns the earliest deadline and whether one exists.
func (s *rotationScheduler) next() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == 0 {
		return time.Time{}, false
	}
	return s.entries[0].rotationDeadline, true
}

// popDue removes and returns the WALs whose deadline is at or before now.
func (s *rotationScheduler) popDue(dst []*WAL, now time.Time) []*WAL {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.entries) > 0 && !s.entries[0].rotationDeadline.After(now) {
		dst = append(dst, heap.Pop(&s.entries).(*WAL))
	}
	return dst
}

func (s *rotationScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	var sweepC <-chan time.Time
	if s.sweep != nil && s.sweepInterval > 0 {
		sweepTicker := time.NewTicker(s.sweepInterval)
		defer sweepTicker.Stop()
		sweepC = sweepTicker.C
	}

	var due []*WAL
	for {
		if deadline, ok := s.next(); ok {
			timer.Reset(time.Until(deadline))
		}

		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-sweepC:
			s.sweep()
		case <-timer.C:
		}
		timer.Stop()

		due = s.popDue(due[:0], time.Now())
		s.rotate(due)
		clear(due)
	}
}

// rotate rotates the given WALs with bounded concurrency and waits for them to finish.
func (s *rotationScheduler) rotate(wals []*WAL) {
	if len(wals) == 1 {
		rotateDue(wals[0])
		return
	}

	sem := make(chan struct{}, s.concurrency)
	var wg sync.WaitGroup
	for _, w := range wals {
		sem <- struct{}{}
		wg.Add(1)
		go func(w *WAL) {
			defer func() {
				<-sem
				wg.Done()
			}()
			rotateDue(w)
		}(w)
	}
	wg.Wait()
}

// rotateDue rotates w if required and schedules its next rotation.
func rotateDue(w *WAL) {
	w.rotateSegmentIfNecessary()
	w.scheduleRotation()
}

// rotationHeap is a min-heap of WALs ordered by rotation deadline.  It is guarded by rotationScheduler.mu.
type rotationHeap []*WAL

func (h rotationHeap) Len() int { return len(h) }

func (h rotationHeap) Less(i, j int) bool {
	return h[i].rotationDeadline.Before(h[j].rotationDeadline)
}

func (h rotationHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].rotationIndex = i
	h[j].rotationIndex = j
}

func (h *rotationHeap) Push(x any) {
	w := x.(*WAL)
	w.rotationIndex = len(*h)
	*h = append(*h, w)
}

func (h *rotationHeap) Pop() any {
	old := *h
	n := len(old)
	w := old[n-1]
	old[n-1] = nil
	w.rotationIndex = -1
	*h = old[:n-1]
	return w
}
