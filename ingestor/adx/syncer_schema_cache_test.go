package adx

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
	"github.com/stretchr/testify/require"
)

// A stalled streaming policy command for one table must not block realtime batches for other tables, including
// tables whose policy is already enabled.
func TestStreamingPolicyDoesNotBlockOtherTables(t *testing.T) {
	mgmt := &blockingMgmt{block: make(chan struct{}), blockOn: "Slow", calls: make(chan string, 16)}
	s := NewSyncer(mgmt, "DB", schema.DefaultMetricsMapping, PromMetrics)
	s.streamingPolicies["Enabled"] = streamingPolicyState{enabled: true}

	stuck := make(chan struct{})
	go func() {
		defer close(stuck)
		_ = s.EnsureStreamingPolicy(context.Background(), "Slow")
	}()
	require.Eventually(t, func() bool { return len(mgmt.calls) > 0 }, time.Second, time.Millisecond)

	for _, table := range []string{"Enabled", "Healthy"} {
		done := make(chan error, 1)
		go func() { done <- s.EnsureStreamingPolicy(context.Background(), table) }()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatalf("streaming policy for %s blocked behind another table's stalled command", table)
		}
	}

	close(mgmt.block)
	<-stuck
}

// showTablesMgmt answers .show tables with a fixed table list and records every management command.
type showTablesMgmt struct {
	tables []string

	mu      sync.Mutex
	queries []string
}

func (m *showTablesMgmt) Mgmt(ctx context.Context, _ string, query azkustodata.Statement, _ ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	m.mu.Lock()
	m.queries = append(m.queries, query.String())
	m.mu.Unlock()
	var rows []kustov1.RawRow
	if strings.HasPrefix(query.String(), ".show tables") {
		for _, name := range m.tables {
			rows = append(rows, kustov1.RawRow{Row: []any{name}})
		}
	}
	return kustov1.NewDataset(ctx, kustoerrors.OpMgmt, kustov1.V1{Tables: []kustov1.RawTable{{
		TableName: "Table_0",
		Columns:   []kustov1.RawColumn{{ColumnName: "TableName", DataType: "String", ColumnType: "string"}},
		Rows:      rows,
	}}})
}

func (m *showTablesMgmt) count(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, q := range m.queries {
		if strings.HasPrefix(q, prefix) {
			n++
		}
	}
	return n
}

// Cache reconciliation keeps the mappings of tables that still exist, including tables whose names are not already
// ProperCase, so their mappings are not recreated on the upload path after every reconcile.
func TestReconcileKeepsMappingsForExistingTables(t *testing.T) {
	defer func(d time.Duration) { reconcileInterval = d }(reconcileInterval)
	reconcileInterval = 10 * time.Millisecond

	mgmt := &showTablesMgmt{tables: []string{"Cpu", "my_logs"}}
	s := NewSyncer(mgmt, "DB", schema.DefaultMetricsMapping, PromMetrics)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); s.wg.Wait() }()

	for _, table := range []string{"Cpu", "my_logs"} {
		_, err := s.EnsureMapping(ctx, table, schema.DefaultMetricsMapping)
		require.NoError(t, err)
	}
	created := mgmt.count(".create-or-alter")
	require.Equal(t, 2, created)

	s.wg.Add(1)
	go s.reconcileTables(ctx)
	require.Eventually(t, func() bool { return mgmt.count(".show tables") >= 2 }, time.Second, time.Millisecond)

	for _, table := range []string{"Cpu", "my_logs"} {
		_, err := s.EnsureMapping(ctx, table, schema.DefaultMetricsMapping)
		require.NoError(t, err)
	}
	t.Logf("mappings recreated after reconcile: %d", mgmt.count(".create-or-alter")-created)
	require.Equal(t, created, mgmt.count(".create-or-alter"), "mappings for existing tables should stay cached")
}
