package cluster

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// releaseN releases a batch of prefix n times in a row, as consecutive failed deliveries do.
func releaseN(r *eventBatcher, prefix string, n int) {
	for i := 0; i < n; i++ {
		r.released(prefix, 0)
	}
}

// A delivery resets only its own prefix's backoff: other prefixes that failed alongside it wait out theirs.
func TestQueuedBatcher_DeliveryLeavesOtherPrefixesBackingOff(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := newQueuedBatcher(env.b, time.Millisecond, 0)

	releaseN(r, "db_Cpu", 10)
	releaseN(r, "db_Mem", 10)
	r.removed("db_Cpu")

	require.Zero(t, r.releases["db_Cpu"])
	require.Equal(t, 10, r.releases["db_Mem"])
	require.WithinDuration(t, time.Now().Add(queuedSweepInterval), r.pending["db_Mem"].retryAfter, time.Second)
}
