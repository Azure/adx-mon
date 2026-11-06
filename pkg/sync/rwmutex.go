package sync

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// CountingRWMutex is a RWMutex that keeps track of the number of goroutines waiting for the lock.
type CountingRWMutex struct {
	mu      sync.RWMutex
	waiting atomic.Int64
	limit   int64
}

func NewCountingRWMutex(limit int) *CountingRWMutex {
	return &CountingRWMutex{limit: int64(limit)}
}

// RLock locks rw for reading.
func (rw *CountingRWMutex) RLock() {
	rw.waiting.Add(1)
	rw.mu.RLock()
}

// RUnlock undoes a single RLock call; it does not affect other simultaneous readers.
func (rw *CountingRWMutex) RUnlock() {
	rw.waiting.Add(-1)
	rw.mu.RUnlock()
}

// Lock locks rw for writing.
func (rw *CountingRWMutex) Lock() {
	rw.waiting.Add(1)
	rw.mu.Lock()
}

// Unlock undoes a single Lock call; it does not affect other simultaneous readers.
func (rw *CountingRWMutex) Unlock() {
	rw.waiting.Add(-1)
	rw.mu.Unlock()
}

// TryLock tries to lock rw for writing if the pending waitinger is less than the limit.
func (rw *CountingRWMutex) TryLock() bool {
	if rw.waiting.Load() > rw.limit {
		return false
	}
	return rw.mu.TryLock()
}

// Waiters returns the number of goroutines waiting for the lock.
func (rw *CountingRWMutex) Waiters() int64 {
	return rw.waiting.Load()
}

// Polling intervals of TryLockFor.  The interval starts short since the lock is usually held only briefly, and grows so
// a waiter does not spin while it stays held.
const (
	minTryLockInterval = 50 * time.Microsecond
	maxTryLockInterval = 2 * time.Millisecond
)

// TryLockFor is like TryLock but keeps trying for up to d, or until ctx is done, so a lock held briefly by another
// writer does not fail the caller.  It polls rather than waits in Lock, so it is not counted as a waiter and the lock
// cannot be held up by callers that have given up.  It returns whether it locked rw.
func (rw *CountingRWMutex) TryLockFor(ctx context.Context, d time.Duration) bool {
	if rw.TryLock() {
		return true
	}
	if d <= 0 {
		return false
	}
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	interval := minTryLockInterval
	poll := time.NewTimer(interval)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return rw.TryLock()
		case <-poll.C:
		}
		if rw.TryLock() {
			return true
		}
		interval = min(2*interval, maxTryLockInterval)
		poll.Reset(interval)
	}
}
