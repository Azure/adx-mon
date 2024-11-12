package sync

import (
	"sync"
	"sync/atomic"
)

// CountingRWMutex is a RWMutex that keeps track of the number of goroutines waiting for the lock.
type CountingRWMutex struct {
	mu      sync.RWMutex
	waiting int64
	limit   int64
}

func NewCountingRWMutex(limit int) *CountingRWMutex {
	return &CountingRWMutex{limit: int64(limit)}
}

// RLock locks rw for reading.
func (rw *CountingRWMutex) RLock() {
	atomic.AddInt64(&rw.waiting, 1)
	rw.mu.RLock()
}

// RUnlock undoes a single RLock call; it does not affect other simultaneous readers.
func (rw *CountingRWMutex) RUnlock() {
	atomic.AddInt64(&rw.waiting, -1)
	rw.mu.RUnlock()
}

// Lock locks rw for writing.
func (rw *CountingRWMutex) Lock() {
	atomic.AddInt64(&rw.waiting, 1)
	rw.mu.Lock()
}

// Unlock undoes a single Lock call; it does not affect other simultaneous readers.
func (rw *CountingRWMutex) Unlock() {
	atomic.AddInt64(&rw.waiting, -1)
	rw.mu.Unlock()
}

// TryLock tries to lock rw for writing if the pending waitinger is less than the limit.
func (rw *CountingRWMutex) TryLock() bool {
	if atomic.LoadInt64(&rw.waiting) > rw.limit {
		return false
	}
	return rw.mu.TryLock()
}

// Waiters returns the number of goroutines waiting for the lock.
func (rw *CountingRWMutex) Waiters() int64 {
	return atomic.LoadInt64(&rw.waiting)
}
