package cluster

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A prefix released while another prefix's delivery recovers the destination either stays marked failed, so a later
// recovery wakes it, or is retried now; it is never left on its full backoff and unmarked.
func TestQueuedBatcher_RecoveryRacingReleaseKeepsWakeUp(t *testing.T) {
	for i := 0; i < 20000; i++ {
		r := newEventBatcher(nil, nil, time.Minute)
		r.maxRetryDelay = time.Minute
		r.released("A", 0)
		for j := 0; j < 10; j++ {
			r.released("B", 0)
		}
		var start, wg sync.WaitGroup
		start.Add(1)
		wg.Add(2)
		go func() { defer wg.Done(); start.Wait(); r.released("B", 0) }()
		go func() { defer wg.Done(); start.Wait(); r.removed("A") }()
		start.Done()
		wg.Wait()
		r.mu.Lock()
		lost := !r.failed["B"] && r.pending["B"].retryAfter.After(time.Now().Add(5*time.Second))
		r.mu.Unlock()
		require.False(t, lost, "iteration %d lost B's recovery wake-up", i)
	}
}
