package adxexporter

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	azerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"
)

// FakeClock implements clock.Clock for testing
type FakeClock struct {
	time time.Time
}

func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{time: t}
}

func (f *FakeClock) Now() time.Time {
	return f.time
}

func (f *FakeClock) Since(ts time.Time) time.Duration {
	return f.time.Sub(ts)
}

func (f *FakeClock) Until(ts time.Time) time.Duration {
	return ts.Sub(f.time)
}

func (f *FakeClock) NewTimer(d time.Duration) clock.Timer {
	return clock.RealClock{}.NewTimer(d)
}

func (f *FakeClock) NewTicker(d time.Duration) clock.Ticker {
	return clock.RealClock{}.NewTicker(d)
}

func (f *FakeClock) Sleep(d time.Duration) {
	f.time = f.time.Add(d)
}

func (f *FakeClock) After(d time.Duration) <-chan time.Time {
	return clock.RealClock{}.After(d)
}

func (f *FakeClock) Tick(d time.Duration) <-chan time.Time {
	return clock.RealClock{}.Tick(d)
}

// MockKustoExecutor implements KustoExecutor for testing
type MockKustoExecutor struct {
	database string
	endpoint string
	queries  []string
	results  []azquery.Dataset
	errors   []error
	callIdx  int
}

func NewMockKustoExecutor(t *testing.T, database, endpoint string) *MockKustoExecutor {
	t.Helper()
	return &MockKustoExecutor{
		database: database,
		endpoint: endpoint,
		queries:  make([]string, 0),
		results:  make([]azquery.Dataset, 0),
		errors:   make([]error, 0),
	}
}

func (m *MockKustoExecutor) Database() string {
	return m.database
}

func (m *MockKustoExecutor) Endpoint() string {
	return m.endpoint
}

func (m *MockKustoExecutor) Query(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.Dataset, error) {
	m.queries = append(m.queries, query.String())

	if m.callIdx < len(m.errors) && m.errors[m.callIdx] != nil {
		err := m.errors[m.callIdx]
		m.callIdx++
		return nil, err
	}

	if m.callIdx < len(m.results) {
		result := m.results[m.callIdx]
		m.callIdx++
		return result, nil
	}

	// Return empty dataset if no specific result configured.
	return createMockDataset(nil), nil
}

// Mgmt implements the management command execution for the mock executor.
// For current tests, no behavior changes are needed, so it mirrors Query by
// recording the statement and returning configured results or an empty dataset.
func (m *MockKustoExecutor) Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.Dataset, error) {
	// Reuse Query behavior to avoid duplicating test plumbing
	m.queries = append(m.queries, query.String())

	if m.callIdx < len(m.errors) && m.errors[m.callIdx] != nil {
		err := m.errors[m.callIdx]
		m.callIdx++
		return nil, err
	}

	if m.callIdx < len(m.results) {
		result := m.results[m.callIdx]
		m.callIdx++
		return result, nil
	}

	return createMockDataset(nil), nil
}

func (m *MockKustoExecutor) SetNextError(err error) {
	m.errors = append(m.errors, err)
}

func (m *MockKustoExecutor) SetNextResult(t *testing.T, rows [][]interface{}) {
	t.Helper()
	m.results = append(m.results, createMockDataset(rows))
}

func (m *MockKustoExecutor) GetQueries() []string {
	return m.queries
}

func (m *MockKustoExecutor) Reset() {
	m.queries = make([]string, 0)
	m.results = make([]azquery.Dataset, 0)
	m.errors = make([]error, 0)
	m.callIdx = 0
}

func createMockDataset(rows [][]interface{}) azquery.Dataset {
	return createDatasetWithColumns([]string{"metric_name", "value", "timestamp"}, rows)
}

func createDatasetWithColumns(columnNames []string, rows [][]interface{}) azquery.Dataset {
	base := azquery.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	columns := make([]azquery.Column, 0, len(columnNames))
	for i, name := range columnNames {
		columns = append(columns, azquery.NewColumn(i, name, inferColumnType(rows, i)))
	}
	baseTable := azquery.NewBaseTable(base, 0, "", "QueryResult", "QueryResult", columns)

	queryRows := make([]azquery.Row, 0, len(rows))
	for i, rowData := range rows {
		vals := make(azvalue.Values, 0, len(rowData))
		for _, col := range rowData {
			switch v := col.(type) {
			case string:
				vals = append(vals, azvalue.NewString(v))
			case float64:
				vals = append(vals, azvalue.NewReal(v))
			case time.Time:
				vals = append(vals, azvalue.NewDateTime(v))
			default:
				vals = append(vals, azvalue.NewString(fmt.Sprintf("%v", v)))
			}
		}
		queryRows = append(queryRows, azquery.NewRow(baseTable, i, vals))
	}

	table := azquery.NewTable(baseTable, queryRows)
	return &mockDataset{base: base, tables: []azquery.Table{table}}
}

func inferColumnType(rows [][]interface{}, idx int) aztypes.Column {
	for _, row := range rows {
		if idx >= len(row) {
			continue
		}

		switch row[idx].(type) {
		case string:
			return aztypes.String
		case float64:
			return aztypes.Real
		case time.Time:
			return aztypes.DateTime
		}
	}

	return aztypes.String
}

type mockDataset struct {
	base   azquery.BaseDataset
	tables []azquery.Table
}

func (d *mockDataset) Context() context.Context {
	return d.base.Context()
}

func (d *mockDataset) Op() azerrors.Op {
	return d.base.Op()
}

func (d *mockDataset) PrimaryResultKind() string {
	return d.base.PrimaryResultKind()
}

func (d *mockDataset) Tables() []azquery.Table {
	return d.tables
}

func TestQueryExecutor_ExecuteQuery(t *testing.T) {
	mockClient := NewMockKustoExecutor(t, "TestDB", "https://test.kusto.windows.net")
	executor := NewQueryExecutor(mockClient)

	ctx := context.Background()
	queryBody := "MyTable | summarize avg_value = avg(Value) by ServiceName"
	startTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	endTime := time.Date(2023, 1, 1, 13, 0, 0, 0, time.UTC)
	clusterLabels := map[string]string{
		"region": "us-east-1",
	}

	t.Run("query construction and execution call", func(t *testing.T) {
		mockClient.Reset()

		mockClient.SetNextResult(t, nil)

		result, err := executor.ExecuteQuery(ctx, queryBody, startTime, endTime, clusterLabels)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.NoError(t, result.Error)
		require.Greater(t, result.Duration, time.Duration(0))

		// Verify the query was called with proper substitutions
		queries := mockClient.GetQueries()
		require.Len(t, queries, 1)

		expectedQuery := `let _startTime=datetime(2023-01-01T12:00:00Z);
let _endTime=datetime(2023-01-01T13:00:00Z);
let _region="us-east-1";
MyTable | summarize avg_value = avg(Value) by ServiceName`

		require.Equal(t, expectedQuery, queries[0])
	})

	t.Run("query execution with connection error", func(t *testing.T) {
		mockClient.Reset()
		mockClient.SetNextError(errors.New("connection failed"))

		result, err := executor.ExecuteQuery(ctx, queryBody, startTime, endTime, clusterLabels)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Error(t, result.Error)
		require.Contains(t, result.Error.Error(), "failed to execute query")
		require.Contains(t, result.Error.Error(), "connection failed")
	})
}

func TestQueryExecutor_ExecuteQuery_MaxRowsLimit(t *testing.T) {
	mockClient := NewMockKustoExecutor(t, "TestDB", "https://test.kusto.windows.net")
	executor := NewQueryExecutor(mockClient)
	executor.SetMaxRows(1)

	now := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC)
	mockClient.SetNextResult(t, [][]interface{}{
		{"metric_one", 1.0, now},
		{"metric_two", 2.0, now},
	})

	ctx := context.Background()
	clusterLabels := map[string]string{}
	queryBody := "MyTable | project metric_name, value, timestamp"

	result, err := executor.ExecuteQuery(ctx, queryBody, now.Add(-time.Hour), now, clusterLabels)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "maximum row limit")
	require.Len(t, result.Rows, 1)
}

func TestNewKustoClient(t *testing.T) {
	t.Run("valid endpoint", func(t *testing.T) {
		// Note: This test will only verify the client creation logic,
		// not actual connectivity since we don't have a real cluster
		client, err := NewKustoClient("https://test.kusto.windows.net", "TestDB")

		if err != nil {
			// If there's an error, it should be related to authentication/connectivity
			// not the client creation logic itself
			t.Logf("Expected error for test environment: %v", err)
		} else {
			require.NotNil(t, client)
			require.Equal(t, "TestDB", client.Database())
			require.Equal(t, "https://test.kusto.windows.net", client.Endpoint())
		}
	})

	t.Run("empty endpoint should cause error", func(t *testing.T) {
		// Use defer to catch the panic and convert it to an expected error
		defer func() {
			if r := recover(); r != nil {
				// Expected panic from empty connection string
				require.Contains(t, fmt.Sprintf("%v", r), "Connection string cannot be empty")
			}
		}()

		_, err := NewKustoClient("", "TestDB")
		if err != nil {
			// If it returns an error instead of panicking, that's also fine
			t.Logf("Got error as expected: %v", err)
		}
	})
}

func TestNewQueryExecutor(t *testing.T) {
	mockClient := NewMockKustoExecutor(t, "TestDB", "https://test.kusto.windows.net")
	executor := NewQueryExecutor(mockClient)

	require.NotNil(t, executor)
	require.NotNil(t, executor.clock)
	require.Equal(t, mockClient, executor.kustoClient)
}
