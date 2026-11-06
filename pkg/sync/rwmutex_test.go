package sync

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCountingRWMutexCountsHoldersAndWaiters(t *testing.T) {
	rw := NewCountingRWMutex(5)
	require.Zero(t, rw.Waiters())

	rw.RLock()
	rw.RLock()
	require.EqualValues(t, 2, rw.Waiters())
	rw.RUnlock()
	rw.RUnlock()
	require.Zero(t, rw.Waiters())

	rw.Lock()
	require.EqualValues(t, 1, rw.Waiters())

	// A goroutine blocked in Lock is counted while it waits.
	acquired := make(chan struct{})
	go func() {
		rw.Lock()
		close(acquired)
	}()
	require.Eventually(t, func() bool { return rw.Waiters() == 2 }, time.Second, time.Millisecond)
	rw.Unlock()
	<-acquired
	require.EqualValues(t, 1, rw.Waiters())
	rw.Unlock()
	require.Zero(t, rw.Waiters())
}

func TestCountingRWMutexTryLock(t *testing.T) {
	rw := NewCountingRWMutex(5)
	require.True(t, rw.TryLock(), "an unheld lock is acquired")
	require.False(t, rw.TryLock(), "a held lock is not acquired")
	rw.mu.Unlock() // TryLock does not count itself, so it is released directly.
	require.Zero(t, rw.Waiters())

	rw.RLock()
	require.False(t, rw.TryLock(), "a read-held lock is not acquired")
	rw.RUnlock()
	require.True(t, rw.TryLock())
	rw.mu.Unlock()
}

// TryLock fails without trying the lock once more goroutines than the limit hold or wait for it, so callers shed
// work instead of piling onto a contended lock.
func TestCountingRWMutexTryLockFailsAboveWaiterLimit(t *testing.T) {
	const limit = 2
	rw := NewCountingRWMutex(limit)

	var readers sync.WaitGroup
	release := make(chan struct{})
	hold := func() {
		readers.Add(1)
		go func() {
			defer readers.Done()
			rw.RLock()
			<-release
			rw.RUnlock()
		}()
	}
	for i := 0; i < limit+1; i++ {
		hold()
	}
	require.Eventually(t, func() bool { return rw.Waiters() == limit+1 }, time.Second, time.Millisecond)
	require.False(t, rw.TryLock())

	close(release)
	readers.Wait()
	require.Zero(t, rw.Waiters())
	require.True(t, rw.TryLock())
	rw.mu.Unlock()
}
