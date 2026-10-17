package adx

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
	"github.com/stretchr/testify/require"
)

// blockingMgmt blocks management calls for one table so a stalled schema
// operation can be observed from another table's perspective.
type blockingMgmt struct {
	block   chan struct{}
	blockOn string
	calls   chan string
}

func (m *blockingMgmt) Mgmt(ctx context.Context, _ string, query azkustodata.Statement, _ ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	text := query.String()
	m.calls <- text
	if m.blockOn != "" && contains(text, m.blockOn) {
		select {
		case <-m.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, nil
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// A schema operation stuck on one table must not block schema work for another.
func TestSchemaOperationsDoNotBlockOtherTables(t *testing.T) {
	mgmt := &blockingMgmt{block: make(chan struct{}), blockOn: "Slow", calls: make(chan string, 16)}
	s := NewSyncer(mgmt, "DB", schema.DefaultMetricsMapping, PromMetrics)

	stuck := make(chan struct{})
	go func() {
		defer close(stuck)
		_ = s.EnsureTable(context.Background(), "Slow", schema.DefaultMetricsMapping)
	}()
	require.Eventually(t, func() bool { return len(mgmt.calls) > 0 }, time.Second, time.Millisecond)

	done := make(chan error, 1)
	go func() {
		done <- s.EnsureTable(context.Background(), "Healthy", schema.DefaultMetricsMapping)
	}()

	select {
	case err := <-done:
		require.NoError(t, err, "healthy table proceeds while another is stuck")
	case <-time.After(2 * time.Second):
		t.Fatal("healthy table blocked behind a stuck schema operation")
	}

	close(mgmt.block)
	<-stuck
}

// Cancelling the upload context must release a stuck schema operation rather
// than holding the worker indefinitely.
func TestSchemaOperationHonoursContextCancellation(t *testing.T) {
	mgmt := &blockingMgmt{block: make(chan struct{}), blockOn: "Slow", calls: make(chan string, 16)}
	s := NewSyncer(mgmt, "DB", schema.DefaultMetricsMapping, PromMetrics)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.EnsureTable(ctx, "Slow", schema.DefaultMetricsMapping) }()
	require.Eventually(t, func() bool { return len(mgmt.calls) > 0 }, time.Second, time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not release the schema operation")
	}
	close(mgmt.block)
}

// A cached mapping must not issue a management call at all.
func TestCachedSchemaSkipsManagementCalls(t *testing.T) {
	mgmt := &blockingMgmt{calls: make(chan string, 16)}
	s := NewSyncer(mgmt, "DB", schema.DefaultMetricsMapping, PromMetrics)

	require.NoError(t, s.EnsureTable(context.Background(), "Cached", schema.DefaultMetricsMapping))
	first := len(mgmt.calls)

	require.NoError(t, s.EnsureTable(context.Background(), "Cached", schema.DefaultMetricsMapping))
	require.Equal(t, first, len(mgmt.calls), "second call is served from cache")
}
