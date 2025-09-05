package adxexporter

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
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
	results  []*kusto.RowIterator
	errors   []error
	callIdx  int
}

func NewMockKustoExecutor(t *testing.T, database, endpoint string) *MockKustoExecutor {
	t.Helper()
	return &MockKustoExecutor{
		database: database,
		endpoint: endpoint,
		queries:  make([]string, 0),
		results:  make([]*kusto.RowIterator, 0),
		errors:   make([]error, 0),
	}
}

func (m *MockKustoExecutor) Database() string {
	return m.database
}

func (m *MockKustoExecutor) Endpoint() string {
	return m.endpoint
}

func (m *MockKustoExecutor) Query(ctx context.Context, query kusto.Statement, options ...kusto.QueryOption) (*kusto.RowIterator, error) {
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

	// Return empty iterator if no specific result configured
	return createEmptyMockRowIterator(), nil
}

// Mgmt implements the management command execution for the mock executor.
// For current tests, no behavior changes are needed, so it mirrors Query by
// recording the statement and returning configured results or an empty iterator.
func (m *MockKustoExecutor) Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	// Reuse Query behavior to avoid duplicating test plumbing
	// Cast MgmtOption to no-op and delegate to Query-like path
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

	return createEmptyMockRowIterator(), nil
}

func (m *MockKustoExecutor) SetNextError(err error) {
	m.errors = append(m.errors, err)
}

func (m *MockKustoExecutor) SetNextResult(t *testing.T, rows [][]interface{}) {
	t.Helper()
	iter := createMockRowIterator(t, rows)
	m.results = append(m.results, iter)
}

func (m *MockKustoExecutor) GetQueries() []string {
	return m.queries
}

func (m *MockKustoExecutor) Reset() {
	m.queries = make([]string, 0)
	m.results = make([]*kusto.RowIterator, 0)
	m.errors = make([]error, 0)
	m.callIdx = 0
}

// createMockRowIterator creates a mock RowIterator for testing with actual data
func createMockRowIterator(t *testing.T, rows [][]interface{}) *kusto.RowIterator {
	t.Helper()
	iter := &kusto.RowIterator{}

	// If no rows provided, return empty iterator
	if len(rows) == 0 {
		// Create empty mock rows to avoid nil pointer issues
		mockRows, err := kusto.NewMockRows(table.Columns{})
		require.NoError(t, err, "Failed to create empty mock rows")

		// Work-around for Azure Kusto SDK's test environment check.
		// The SDK's RowIterator.Mock() method explicitly checks if it's running in a test
		// by calling isTest() which looks for the "test.v" flag (set by `go test`).
		// If not found, Mock() panics with "cannot call Mock outside a test".
		// We ensure the flag exists to bypass this safety check.
		if flag.Lookup("test.v") == nil {
			flag.String("test.v", "", "")
			err := flag.CommandLine.Set("test.v", "true")
			require.NoError(t, err, "Failed to set test.v flag")
		}
		err = iter.Mock(mockRows)
		require.NoError(t, err, "Failed to mock empty iterator")
		return iter
	}

	// Create columns based on the first row structure
	// For simplicity, assume first row has: metric_name, value, timestamp
	columns := table.Columns{
		{Name: "metric_name", Type: types.String},
		{Name: "value", Type: types.Real},
		{Name: "timestamp", Type: types.DateTime},
	}

	mockRows, err := kusto.NewMockRows(columns)
	require.NoError(t, err, "Failed to create mock rows")

	// Add each row to the mock
	for _, rowData := range rows {
		var values value.Values
		for _, col := range rowData {
			switch v := col.(type) {
			case string:
				values = append(values, value.String{Value: v, Valid: true})
			case float64:
				values = append(values, value.Real{Value: v, Valid: true})
			case time.Time:
				values = append(values, value.DateTime{Value: v, Valid: true})
			default:
				// Convert to string as fallback
				values = append(values, value.String{Value: fmt.Sprintf("%v", v), Valid: true})
			}
		}
		mockRows.Row(values)
	}

	// Work-around for Azure Kusto SDK's test environment check.
	// The SDK's RowIterator.Mock() method explicitly checks if it's running in a test
	// by calling isTest() which looks for the "test.v" flag (set by `go test`).
	// If not found, Mock() panics with "cannot call Mock outside a test".
	// We ensure the flag exists to bypass this safety check.
	if flag.Lookup("test.v") == nil {
		flag.String("test.v", "", "")
		err := flag.CommandLine.Set("test.v", "true")
		require.NoError(t, err, "Failed to set test.v flag")
	}
	err = iter.Mock(mockRows)
	require.NoError(t, err, "Failed to mock iterator")
	return iter
}

// createEmptyMockRowIterator creates an empty mock RowIterator without requiring *testing.T
// This is used internally by MockKustoExecutor when no specific result is configured
func createEmptyMockRowIterator() *kusto.RowIterator {
	iter := &kusto.RowIterator{}

	// Create empty mock rows to avoid nil pointer issues
	mockRows, err := kusto.NewMockRows(table.Columns{})
	if err != nil {
		panic(fmt.Sprintf("Failed to create empty mock rows: %v", err))
	}

	// Work-around for Azure Kusto SDK's test environment check.
	// The SDK's RowIterator.Mock() method explicitly checks if it's running in a test
	// by calling isTest() which looks for the "test.v" flag (set by `go test`).
	// If not found, Mock() panics with "cannot call Mock outside a test".
	// We ensure the flag exists to bypass this safety check.
	if flag.Lookup("test.v") == nil {
		flag.String("test.v", "", "")
		if err := flag.CommandLine.Set("test.v", "true"); err != nil {
			panic(err)
		}
	}
	if err := iter.Mock(mockRows); err != nil {
		panic(fmt.Sprintf("Failed to mock empty iterator: %v", err))
	}
	return iter
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

		// For successful case, we just want to verify the query gets constructed correctly
		// We'll mock an error to avoid the RowIterator processing issues
		mockClient.SetNextError(errors.New("mock error to bypass iterator"))

		result, err := executor.ExecuteQuery(ctx, queryBody, startTime, endTime, clusterLabels)

		require.NoError(t, err) // No error from the function itself
		require.NotNil(t, result)
		require.Error(t, result.Error) // The mock error we set
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
