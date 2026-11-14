package clickhouse

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

// reconcilingConnection models a ClickHouse connection whose Select returns a
// fixed set of existing table names for the system.tables reconcile query.  It
// also records every query it runs so the test can prove the reconcile issued
// the expected system.tables lookup.
type reconcilingConnection struct {
	fakeConnection
	existingTables []string
}

type blockingSchemaConnection struct {
	fakeConnection
	entered chan struct{}
	release chan struct{}
}

func (c *blockingSchemaConnection) Exec(ctx context.Context, query string, args ...any) error {
	if strings.Contains(query, "`Slow`") {
		close(c.entered)
		select {
		case <-c.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.fakeConnection.Exec(ctx, query, args...)
}

func TestSchemaCreationDoesNotBlockOtherTables(t *testing.T) {
	conn := &blockingSchemaConnection{entered: make(chan struct{}), release: make(chan struct{})}
	withFakeConnectionManager(t, conn)
	up, err := NewUploader(Config{Database: "metricsdb", DSN: "clickhouse://localhost"}, nil)
	require.NoError(t, err)
	u := up.(*uploader)
	u.syncer.mu.Lock()
	u.syncer.metadataReady = true
	u.syncer.mu.Unlock()
	finished := make(chan error, 1)
	go func() {
		_, err := u.ensureSchema(context.Background(), "Slow", "v1", schema.DefaultMetricsMapping)
		finished <- err
	}()
	select {
	case <-conn.entered:
	case <-time.After(time.Second):
		t.Fatal("slow schema operation did not start")
	}
	defer func() {
		select {
		case <-conn.release:
		default:
			close(conn.release)
		}
	}()
	fast := make(chan error, 1)
	go func() {
		_, err := u.ensureSchema(context.Background(), "Fast", "v1", schema.DefaultMetricsMapping)
		fast <- err
	}()
	select {
	case err := <-fast:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("unrelated schema creation blocked behind Slow")
	}
	close(conn.release)
	require.NoError(t, <-finished)
}

// Select intercepts the schema-cache reconcile's list-tables query and returns
// the configured existing-table names; any other query falls through to the
// fake.  Overriding fakeConnection.Select here (instead of embedding a real
// driver.Conn) keeps the test hermetic and fast.
func (c *reconcilingConnection) Select(ctx context.Context, dest any, query string, args ...any) error {
	c.mu.Lock()
	c.execs = append(c.execs, strings.TrimSpace(query))
	c.mu.Unlock()

	if strings.Contains(query, "system.tables") {
		names, ok := dest.(*[]catalogTableName)
		if !ok {
			return context.DeadlineExceeded
		}
		for _, name := range c.existingTables {
			*names = append(*names, catalogTableName{Name: name})
		}
		return nil
	}
	return c.fakeConnection.Select(ctx, dest, query, args...)
}

// TestUploaderEvictsSchemaForDroppedTable demonstrates finding M8: a table that
// is dropped from ClickHouse keeps its Schema (a []Column slice) in the
// uploader's u.schemas map forever, because ensureSchema only ever adds and
// nothing ever evicts.  The ADX syncer reconciles its cache every 15 minutes;
// the ClickHouse side had no equivalent.
//
// Pre-fix, evictSchemasForDroppedTables does not exist, so the entry survives
// the call and this test fails.  Post-fix, the evict helper removes the entry
// for the dropped table and keeps the surviving one.
func TestUploaderEvictsSchemaForDroppedTable(t *testing.T) {
	conn := &reconcilingConnection{existingTables: []string{"surviving"}}
	withFakeConnectionManager(t, conn)

	cfg := Config{Database: "metricsdb", DSN: "clickhouse://localhost"}
	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)
	impl := u.(*uploader)

	// Populate the schema cache with two tables: one that survives and one
	// that has been dropped from ClickHouse.
	impl.schemaMu.Lock()
	impl.schemas["surviving"] = Schema{Table: "surviving", Columns: []Column{{Name: "Timestamp", Type: "DateTime64"}}}
	impl.schemas["dropped"] = Schema{Table: "dropped", Columns: []Column{{Name: "Timestamp", Type: "DateTime64"}}}
	impl.schemaMu.Unlock()

	existing := map[string]struct{}{"surviving": {}}
	impl.evictSchemasForDroppedTables(existing)

	impl.schemaMu.Lock()
	_, droppedStillCached := impl.schemas["dropped"]
	_, survivingStillCached := impl.schemas["surviving"]
	impl.schemaMu.Unlock()

	require.False(t, droppedStillCached, "schema for dropped table must be evicted from u.schemas")
	require.True(t, survivingStillCached, "schema for a table still present must be kept")
}

// TestUploaderReconcileQueriesSystemTables proves the periodic reconcile
// actually asks the database which tables exist (the SELECT against
// system.tables) before it evicts.  A fake that records queries returns the
// dropped table absent from the result set.
func TestUploaderReconcileQueriesSystemTables(t *testing.T) {
	conn := &reconcilingConnection{existingTables: []string{}}
	withFakeConnectionManager(t, conn)

	cfg := Config{Database: "metricsdb", DSN: "clickhouse://localhost"}
	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)
	impl := u.(*uploader)

	impl.schemaMu.Lock()
	impl.schemas["gone"] = Schema{Table: "gone", Columns: []Column{{Name: "Timestamp", Type: "DateTime64"}}}
	impl.schemaMu.Unlock()

	impl.reconcileSchemas(context.Background())

	// The reconcile must have issued a list-tables query.
	require.Eventually(t, func() bool {
		for _, q := range conn.execQueries() {
			if strings.Contains(q, "system.tables") {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond, "reconcile did not query system.tables")

	// With no tables reported, the lone cached schema is evicted.
	impl.schemaMu.Lock()
	_, stillCached := impl.schemas["gone"]
	impl.schemaMu.Unlock()
	require.False(t, stillCached, "schema must be evicted when the table is no longer present")
}

func TestUploaderReconcileKeepsDefaultSchemasAndEvictsSyncerCache(t *testing.T) {
	conn := &reconcilingConnection{existingTables: []string{"metrics_samples", "otel_logs"}}
	withFakeConnectionManager(t, conn)
	u, err := NewUploader(Config{Database: "metricsdb", DSN: "clickhouse://localhost"}, nil)
	require.NoError(t, err)
	impl := u.(*uploader)
	impl.syncer.mu.Lock()
	impl.syncer.ensured[schemaCacheKey("dropped", "v1")] = struct{}{}
	impl.syncer.mu.Unlock()
	impl.schemaMu.Lock()
	impl.schemas["dropped@v1"] = Schema{Table: "dropped", Columns: []Column{{Name: "X", Type: "String"}}}
	impl.schemaMu.Unlock()

	impl.reconcileSchemas(context.Background())
	require.Contains(t, impl.Schemas(), "metrics")
	require.Contains(t, impl.Schemas(), "logs")
	require.NotContains(t, impl.Schemas(), "dropped@v1")
	impl.syncer.mu.Lock()
	defer impl.syncer.mu.Unlock()
	require.NotContains(t, impl.syncer.ensured, schemaCacheKey("dropped", "v1"))
}
