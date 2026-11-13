package adx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

// A table that has a streaming policy enabled (or a cached failure) and is later
// dropped from the database must have its streamingPolicies entry pruned on the
// next reconcile.  Without the prune, every distinct table that ever enabled a
// streaming policy keeps its entry for the process lifetime: unbounded growth.
//
// The map is keyed by bare table name, and reconcileTables builds tableExists
// from the live tables, so the lookup is a direct map access.  This drives the
// full reconcile (via a fake KustoCli that reports the table as absent) rather
// than a factored helper, so it exercises the prune where it lives.
func TestReconcilePrunesStreamingPoliciesForDroppedTables(t *testing.T) {
	defer func(d time.Duration) { reconcileInterval = d }(reconcileInterval)
	reconcileInterval = 10 * time.Millisecond

	// The fake reports a surviving table, "Kept", while the cached policies
	// below ("Cpu", "my_logs") are absent from the live table list.  A non-empty
	// list is required: reconcileTables skips cleanup entirely when the database
	// has zero tables.  showTablesMgmt answers ".show tables" with this list and
	// ignores every other management command.
	mgmt := &showTablesMgmt{tables: []string{"Kept"}}
	s := NewSyncer(mgmt, "DB", schema.DefaultMetricsMapping, PromMetrics)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.wg.Wait() }()

	// A table that had its streaming policy enabled, then was dropped.
	s.setStreamingPolicy("Cpu", streamingPolicyState{enabled: true})
	// A table whose policy enable failed and is cached for the retry interval.
	s.setStreamingPolicy("my_logs", streamingPolicyState{err: errors.New("no permission"), retryAt: time.Now().Add(streamingPolicyRetryInterval)})

	s.streamingMu.Lock()
	pre := len(s.streamingPolicies)
	s.streamingMu.Unlock()
	require.Equal(t, 2, pre, "both stale policies must be cached before the reconcile")

	s.wg.Add(1)
	go s.reconcileTables(ctx)

	// Once the reconcile has observed the live table list (which lacks both
	// "Cpu" and "my_logs"), both stale entries must be gone.  require.Eventually
	// reports the final state on failure.
	require.Eventually(t, func() bool {
		s.streamingMu.Lock()
		defer s.streamingMu.Unlock()
		_, cpu := s.streamingPolicies["Cpu"]
		_, logs := s.streamingPolicies["my_logs"]
		return !cpu && !logs
	}, 3*time.Second, time.Millisecond, "stale streaming policy entries must be pruned on reconcile")

	// The map must be back to empty: no lingering entries for the dropped tables.
	s.streamingMu.Lock()
	post := len(s.streamingPolicies)
	s.streamingMu.Unlock()
	require.Equal(t, 0, post, "streamingPolicies must hold no entries for dropped tables after reconcile")
}
