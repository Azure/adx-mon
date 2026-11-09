package sync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRWMutexTryLock(t *testing.T) {
	var rw RWMutex
	require.True(t, rw.TryLock(), "an unheld lock is acquired")
	require.False(t, rw.TryLock(), "a held lock is not acquired")
	rw.Unlock()

	rw.RLock()
	require.False(t, rw.TryLock(), "a read-held lock is not acquired")
	rw.RUnlock()
	require.True(t, rw.TryLock())
	rw.Unlock()
}

func TestRWMutexTryLockFor(t *testing.T) {
	t.Run("unheld", func(t *testing.T) {
		var rw RWMutex
		require.True(t, rw.TryLockFor(context.Background(), time.Second))
		rw.Unlock()
	})

	t.Run("released within the wait", func(t *testing.T) {
		var rw RWMutex
		rw.Lock()
		time.AfterFunc(10*time.Millisecond, rw.Unlock)
		require.True(t, rw.TryLockFor(context.Background(), time.Second))
		rw.Unlock()
	})

	t.Run("held past the wait", func(t *testing.T) {
		var rw RWMutex
		rw.Lock()
		defer rw.Unlock()
		start := time.Now()
		require.False(t, rw.TryLockFor(context.Background(), 30*time.Millisecond))
		require.GreaterOrEqual(t, time.Since(start), 30*time.Millisecond)
	})

	t.Run("no wait", func(t *testing.T) {
		var rw RWMutex
		rw.Lock()
		defer rw.Unlock()
		start := time.Now()
		require.False(t, rw.TryLockFor(context.Background(), 0))
		require.Less(t, time.Since(start), 10*time.Millisecond)
	})

	t.Run("context done", func(t *testing.T) {
		var rw RWMutex
		rw.Lock()
		defer rw.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(5*time.Millisecond, cancel)
		start := time.Now()
		require.False(t, rw.TryLockFor(ctx, time.Minute))
		require.Less(t, time.Since(start), time.Second)
	})

	// Readers holding the lock delay TryLockFor, which gets the lock once they are done.
	t.Run("read-held", func(t *testing.T) {
		var rw RWMutex
		release := make(chan struct{})
		var readers, holding sync.WaitGroup
		for i := 0; i < 2; i++ {
			readers.Add(1)
			holding.Add(1)
			go func() {
				defer readers.Done()
				rw.RLock()
				holding.Done()
				<-release
				rw.RUnlock()
			}()
		}
		holding.Wait()
		time.AfterFunc(10*time.Millisecond, func() { close(release) })
		require.True(t, rw.TryLockFor(context.Background(), time.Second))
		rw.Unlock()
		readers.Wait()
	})
}
