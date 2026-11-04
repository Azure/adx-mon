package adx

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kustoerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
	"github.com/stretchr/testify/require"
)

// schemaMgmt answers .show database cslschema with fixed table schemas and records every management command.
type schemaMgmt struct {
	schemas map[string]string
	mu      sync.Mutex
	queries []string
}

func (m *schemaMgmt) Mgmt(ctx context.Context, _ string, query azkustodata.Statement, _ ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	q := query.String()
	m.mu.Lock()
	m.queries = append(m.queries, q)
	m.mu.Unlock()
	table := kustov1.RawTable{TableName: "Table_0", Columns: []kustov1.RawColumn{{ColumnName: "Name", DataType: "String", ColumnType: "string"}}}
	switch {
	case strings.Contains(q, "ingestion mappings"):
		table.Columns = []kustov1.RawColumn{{ColumnName: "Name", DataType: "String", ColumnType: "string"}}
	case strings.Contains(q, "cslschema"):
		table.Columns = []kustov1.RawColumn{
			{ColumnName: "TableName", DataType: "String", ColumnType: "string"},
			{ColumnName: "Schema", DataType: "String", ColumnType: "string"},
		}
		for name, cols := range m.schemas {
			table.Rows = append(table.Rows, kustov1.RawRow{Row: []interface{}{name, cols}})
		}
	default:
		table.Rows = []kustov1.RawRow{{Row: []interface{}{"ok"}}}
	}
	return kustov1.NewDataset(ctx, kustoerrors.OpMgmt, kustov1.V1{Tables: []kustov1.RawTable{table}})
}

func (m *schemaMgmt) count(prefix string) int {
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

// cslSchema returns mapping in the format of .show database cslschema.
func cslSchema(mapping schema.SchemaMapping) string {
	var cols []string
	for _, c := range mapping {
		cols = append(cols, c.Column+":"+c.DataType)
	}
	return strings.Join(cols, ", ")
}

// After a restart, tables that already have the columns a mapping needs are not created again on their first upload,
// so a rollout does not send one management command per table per ingestor.
func TestSyncerSkipsCreatingExistingTables(t *testing.T) {
	mapping := schema.DefaultMetricsMapping
	mgmt := &schemaMgmt{schemas: map[string]string{
		"Cpu":    cslSchema(mapping),
		"Memory": cslSchema(mapping),
		// A table missing one of the mapping's columns still needs .create-merge to add it.
		"Disk": cslSchema(mapping[:len(mapping)-1]),
	}}
	s := NewSyncer(mgmt, "db", mapping, PromMetrics)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	for _, table := range []string{"Cpu", "Memory", "Disk", "Network"} {
		require.NoError(t, s.EnsureTable(context.Background(), table, mapping))
	}
	created := mgmt.count(".create-merge table")
	t.Logf("tables created for 2 existing, 1 missing a column and 1 new table: %d", created)
	require.Equal(t, 2, created)
}

// Column names that need quoting in the schema are matched by their unquoted name.
func TestSyncerLoadsQuotedColumnNames(t *testing.T) {
	mapping := schema.SchemaMapping{{Column: "Has Space", DataType: "string"}, {Column: "Plain", DataType: "long"}}
	mgmt := &schemaMgmt{schemas: map[string]string{"Quoted": "['Has Space']:string,Plain:long", "Other": "Plain:long"}}
	s := NewSyncer(mgmt, "db", mapping, PromMetrics)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()

	require.NoError(t, s.EnsureTable(context.Background(), "Quoted", mapping))
	require.Zero(t, mgmt.count(".create-merge table"))

	// A column whose type differs still runs .create-merge, so Kusto reports the mismatch.
	require.NoError(t, s.EnsureTable(context.Background(), "Other", schema.SchemaMapping{{Column: "Plain", DataType: "string"}}))
	require.Equal(t, 1, mgmt.count(".create-merge table"))
}

// A table dropped after startup is created again on its next upload rather than skipped because it existed when
// the schemas were loaded.
func TestSyncerRecreatesTablesDroppedAfterStartup(t *testing.T) {
	mapping := schema.DefaultMetricsMapping
	mgmt := &schemaMgmt{schemas: map[string]string{"Cpu": cslSchema(mapping)}}
	s := NewSyncer(mgmt, "db", mapping, PromMetrics)
	require.NoError(t, s.Open(context.Background()))
	defer s.Close()
	require.NoError(t, s.EnsureTable(context.Background(), "Cpu", mapping))
	require.Zero(t, mgmt.count(".create-merge table"))

	// Reconciliation removes a dropped table from the cache.
	s.mu.Lock()
	delete(s.tables, "Cpu")
	s.mu.Unlock()
	require.NoError(t, s.EnsureTable(context.Background(), "Cpu", mapping))
	require.Equal(t, 1, mgmt.count(".create-merge table"), "a dropped table must be created again")
}
