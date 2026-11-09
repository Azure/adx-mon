package sync

import (
	"context"
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

func TestCountingRWMutexTryLockFor(t *testing.T) {
	t.Run("unheld", func(t *testing.T) {
		rw := NewCountingRWMutex(5)
		require.True(t, rw.TryLockFor(context.Background(), time.Second))
		rw.mu.Unlock()
	})

	t.Run("released within the wait", func(t *testing.T) {
		rw := NewCountingRWMutex(5)
		rw.Lock()
		time.AfterFunc(10*time.Millisecond, rw.Unlock)
		require.True(t, rw.TryLockFor(context.Background(), time.Second))
		rw.mu.Unlock()
	})

	t.Run("held past the wait", func(t *testing.T) {
		rw := NewCountingRWMutex(5)
		rw.Lock()
		defer rw.Unlock()
		start := time.Now()
		require.False(t, rw.TryLockFor(context.Background(), 30*time.Millisecond))
		require.GreaterOrEqual(t, time.Since(start), 30*time.Millisecond)
		require.EqualValues(t, 1, rw.Waiters(), "a caller that gave up is not counted")
	})

	t.Run("no wait", func(t *testing.T) {
		rw := NewCountingRWMutex(5)
		rw.Lock()
		defer rw.Unlock()
		start := time.Now()
		require.False(t, rw.TryLockFor(context.Background(), 0))
		require.Less(t, time.Since(start), 10*time.Millisecond)
	})

	t.Run("context done", func(t *testing.T) {
		rw := NewCountingRWMutex(5)
		rw.Lock()
		defer rw.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(5*time.Millisecond, cancel)
		start := time.Now()
		require.False(t, rw.TryLockFor(ctx, time.Minute))
		require.Less(t, time.Since(start), time.Second)
	})

	// While more goroutines than the limit hold or wait for the lock, TryLockFor keeps trying rather than giving up,
	// and gets the lock once they are done.
	t.Run("above the waiter limit", func(t *testing.T) {
		rw := NewCountingRWMutex(1)
		release := make(chan struct{})
		var readers sync.WaitGroup
		for i := 0; i < 2; i++ {
			readers.Add(1)
			go func() {
				defer readers.Done()
				rw.RLock()
				<-release
				rw.RUnlock()
			}()
		}
		require.Eventually(t, func() bool { return rw.Waiters() == 2 }, time.Second, time.Millisecond)
		time.AfterFunc(10*time.Millisecond, func() { close(release) })
		require.True(t, rw.TryLockFor(context.Background(), time.Second))
		rw.mu.Unlock()
		readers.Wait()
	})
}

// TryLock does not count its holder, but Unlock uncounts it, so the waiter count goes negative with every TryLock and
// the limit never sheds a caller.
func TestCountingRWMutexTryLockUnlockGoesNegative(t *testing.T) {
	rw := NewCountingRWMutex(5)
	for i := 0; i < 10; i++ {
		require.True(t, rw.TryLock())
		rw.Unlock()
	}
	require.EqualValues(t, -10, rw.Waiters())
}
