package clickhouse

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSyncerEnsureInitialCreatesTables(t *testing.T) {
	t.Parallel()

	fake := &fakeConnection{}
	sync := newSyncer("observability", fake, nil)

	defaults := map[string]Schema{
		"metrics": {
			Table: "metrics_samples",
			Columns: []Column{
				{Name: "Timestamp", Type: "DateTime64"},
				{Name: "SeriesId", Type: "UInt64"},
			},
		},
	}

	require.NoError(t, sync.EnsureInitial(context.Background(), defaults))

	queries := fake.execQueries()
	require.NotEmpty(t, queries)

	requireCondition(t, queries, func(query string) bool {
		return strings.Contains(query, "CREATE TABLE IF NOT EXISTS `observability`.`"+schemaMetadataTable+"`")
	})

	requireCondition(t, queries, func(query string) bool {
		return strings.Contains(query, "CREATE TABLE IF NOT EXISTS `observability`.`metrics_samples`")
	})

	requireCondition(t, queries, func(query string) bool {
		return strings.Contains(query, "INSERT INTO `observability`.`"+schemaMetadataTable+"` (Table, SchemaID, Version, Header, UpdatedAt) VALUES ('metrics_samples'")
	})
}

func TestSyncerEnsureTableIsIdempotent(t *testing.T) {
	t.Parallel()

	fake := &fakeConnection{}
	sync := newSyncer("metricsdb", fake, nil)

	columns := []Column{{Name: "Timestamp", Type: "DateTime64"}}

	require.NoError(t, sync.EnsureTable(context.Background(), "CpuUsage", "schemaA", columns))
	first := fake.execQueries()
	require.NoError(t, sync.EnsureTable(context.Background(), "CpuUsage", "schemaA", columns))
	second := fake.execQueries()

	require.Equal(t, len(first), len(second))
}

func TestSyncerEnsureInitialDecodesCacheKeys(t *testing.T) {
	t.Parallel()

	fake := &fakeConnection{}
	sync := newSyncer("metricsdb", fake, nil)

	columns := []Column{{Name: "Timestamp", Type: "DateTime64"}}
	defaults := map[string]Schema{
		schemaCacheKey("CpuUsage", "schema"): {
			Table:   "CpuUsage",
			Columns: columns,
		},
	}

	require.NoError(t, sync.EnsureInitial(context.Background(), defaults))

	queries := fake.execQueries()
	requireCondition(t, queries, func(query string) bool {
		return strings.Contains(query, "VALUES ('CpuUsage', 'schema'")
	})

	require.Condition(t, func() bool {
		for _, query := range queries {
			if strings.Contains(query, "'CpuUsage@schema'") {
				return false
			}
		}
		return true
	})

	initial := len(queries)
	require.NoError(t, sync.EnsureTable(context.Background(), "CpuUsage", "schema", columns))
	require.Equal(t, initial, len(fake.execQueries()))
}

func requireCondition(t *testing.T, queries []string, fn func(string) bool) {
	t.Helper()
	for _, query := range queries {
		if fn(query) {
			return
		}
	}
	t.Fatalf("no query satisfied condition\nqueries: %v", queries)
}
