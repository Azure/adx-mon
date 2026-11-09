package sync

import (
	"context"
	"sync"
	"time"
)

// RWMutex is a sync.RWMutex whose writers can wait a bounded time for the lock with TryLockFor.
type RWMutex struct {
	sync.RWMutex
}

// Polling intervals of TryLockFor.  The interval starts short since the lock is usually held only briefly, and grows so
// a waiter does not spin while it stays held.
const (
	minTryLockInterval = 50 * time.Microsecond
	maxTryLockInterval = 2 * time.Millisecond
)

// TryLockFor is like TryLock but keeps trying for up to d, or until ctx is done, so a lock held briefly by another
// writer does not fail the caller.  It polls rather than waits in Lock, so the lock cannot be held up by callers that
// have given up.  It returns whether it locked rw.
func (rw *RWMutex) TryLockFor(ctx context.Context, d time.Duration) bool {
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
