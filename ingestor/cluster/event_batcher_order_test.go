package cluster

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/stretchr/testify/require"
)

// Pending prefixes are batched in deadline order, so the most overdue data enters a full queue first rather than in
// map order.
func TestEventBatcher_BatchesOldestFirst(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := newQueuedBatcher(env.b, time.Hour)

	now := time.Now()
	var want []string
	for i := range 20 {
		want = append(want, fmt.Sprintf("db_T%02d", i))
	}
	for _, i := range rand.Perm(len(want)) {
		env.add(t, want[i][3:], 100, ingestpolicy.PriorityQueued)
		r.markPending(want[i], now.Add(-time.Duration(len(want)-i)*time.Second))
	}

	r.process(context.Background(), now, false)
	var got []string
	for range want {
		got = append(got, receiveBatch(t, env.transfer, time.Second).Prefix)
	}
	t.Logf("batched in order %v", got)
	require.Equal(t, want, got)
}

// A prefix backing off after a release is not due until its retryAfter, even when a new segment gives it an earlier
// deadline.  Scheduling the earlier deadline would spin the event loop on an expired timer until the backoff ends.
func TestEventBatcher_NextDeadlineWaitsForRetryAfter(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	r := newQueuedBatcher(env.b, time.Hour)

	now := time.Now()
	retryAfter := now.Add(time.Minute)
	r.markPendingWithRetry("db_Backoff", retryAfter, retryAfter)
	r.markPending("db_Backoff", now.Add(-time.Second))

	next, ok := r.nextDeadline()
	require.True(t, ok)
	require.Equal(t, retryAfter, next)

	r.markPending("db_Other", now.Add(5*time.Second))
	next, ok = r.nextDeadline()
	require.True(t, ok)
	require.Equal(t, now.Add(5*time.Second), next)
}
