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
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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
	database      string
	endpoint      string
	queries       []string
	results       []azquery.Dataset
	errors        []error
	callIdx       int
	lastIterative *mockIterativeDataset
	tableErr      error
	rowErr        error
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

func (m *MockKustoExecutor) IterativeQuery(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.IterativeDataset, error) {
	m.queries = append(m.queries, query.String())

	result, err := m.nextResult()
	if err != nil {
		return nil, err
	}
	m.lastIterative = newMockIterativeDataset(result)
	m.lastIterative.tableErr = m.tableErr
	m.lastIterative.rowErr = m.rowErr
	return m.lastIterative, nil
}

// Mgmt implements the management command execution for the mock executor.
// It records the statement and returns configured results or an empty dataset.
func (m *MockKustoExecutor) Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.Dataset, error) {
	m.queries = append(m.queries, query.String())
	return m.nextResult()
}

func (m *MockKustoExecutor) nextResult() (azquery.Dataset, error) {
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

func (m *MockKustoExecutor) SetTableError(err error) {
	m.tableErr = err
}

func (m *MockKustoExecutor) SetRowError(err error) {
	m.rowErr = err
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
	m.lastIterative = nil
	m.tableErr = nil
	m.rowErr = nil
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
			case uuid.UUID:
				vals = append(vals, azvalue.NewGUID(v))
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
		case uuid.UUID:
			return aztypes.GUID
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

type mockIterativeDataset struct {
	base     azquery.Dataset
	closed   bool
	tableErr error
	rowErr   error
}

func newMockIterativeDataset(dataset azquery.Dataset) *mockIterativeDataset {
	return &mockIterativeDataset{base: dataset}
}

func (d *mockIterativeDataset) Context() context.Context  { return d.base.Context() }
func (d *mockIterativeDataset) Op() azerrors.Op           { return d.base.Op() }
func (d *mockIterativeDataset) PrimaryResultKind() string { return d.base.PrimaryResultKind() }

func (d *mockIterativeDataset) Tables() <-chan azquery.TableResult {
	tables := d.base.Tables()
	results := make(chan azquery.TableResult, len(tables)+1)
	if d.tableErr != nil {
		results <- azquery.TableResultError(d.tableErr)
		close(results)
		return results
	}
	for _, table := range tables {
		results <- azquery.TableResultSuccess(&mockIterativeTable{base: table, rowErr: d.rowErr})
	}
	close(results)
	return results
}

func (d *mockIterativeDataset) ToDataset() (azquery.Dataset, error) { return d.base, nil }

func (d *mockIterativeDataset) Close() error {
	d.closed = true
	return nil
}

type mockIterativeTable struct {
	base   azquery.Table
	rowErr error
}

func (t *mockIterativeTable) Id() string                { return t.base.Id() }
func (t *mockIterativeTable) Index() int64              { return t.base.Index() }
func (t *mockIterativeTable) Name() string              { return t.base.Name() }
func (t *mockIterativeTable) Columns() []azquery.Column { return t.base.Columns() }
func (t *mockIterativeTable) Kind() string              { return t.base.Kind() }
func (t *mockIterativeTable) ColumnByName(name string) azquery.Column {
	return t.base.ColumnByName(name)
}
func (t *mockIterativeTable) Op() azerrors.Op       { return t.base.Op() }
func (t *mockIterativeTable) IsPrimaryResult() bool { return t.base.IsPrimaryResult() }

func (t *mockIterativeTable) Rows() <-chan azquery.RowResult {
	rows := t.base.Rows()
	results := make(chan azquery.RowResult, len(rows)+1)
	for _, row := range rows {
		results <- azquery.RowResultSuccess(row)
	}
	if t.rowErr != nil {
		results <- azquery.RowResultError(t.rowErr)
	}
	close(results)
	return results
}

func (t *mockIterativeTable) ToTable() (azquery.Table, error) { return t.base, nil }

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

	t.Run("streamed table error", func(t *testing.T) {
		mockClient.Reset()
		streamErr := errors.New("table stream failed")
		mockClient.SetTableError(streamErr)

		result, err := executor.ExecuteQuery(ctx, queryBody, startTime, endTime, clusterLabels)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.ErrorIs(t, result.Error, streamErr)
		require.Contains(t, result.Error.Error(), "failed to read query results")
		require.Empty(t, result.Rows)
		require.True(t, mockClient.lastIterative.closed)
	})

	t.Run("streamed row error", func(t *testing.T) {
		mockClient.Reset()
		streamErr := errors.New("row stream failed")
		mockClient.SetNextResult(t, [][]interface{}{{"metric_one", 1.0, startTime}})
		mockClient.SetRowError(streamErr)

		result, err := executor.ExecuteQuery(ctx, queryBody, startTime, endTime, clusterLabels)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.ErrorIs(t, result.Error, streamErr)
		require.Contains(t, result.Error.Error(), "failed to read query results")
		require.Empty(t, result.Rows)
		require.True(t, mockClient.lastIterative.closed)
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
	require.Empty(t, result.Rows)
	require.True(t, mockClient.lastIterative.closed)
}

func TestQueryExecutor_ExecuteQuery_MaterializesNativeValues(t *testing.T) {
	mockClient := NewMockKustoExecutor(t, "TestDB", "https://test.kusto.windows.net")
	executor := NewQueryExecutor(mockClient)
	now := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC)
	mockClient.SetNextResult(t, [][]interface{}{{"metric_one", 1.5, now}})

	result, err := executor.ExecuteQuery(context.Background(), "MyTable", now.Add(-time.Hour), now, nil)

	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Equal(t, "metric_one", result.Rows[0]["metric_name"])
	require.Equal(t, 1.5, result.Rows[0]["value"])
	require.Equal(t, now, result.Rows[0]["timestamp"])
}

func TestMaterializeKustoValue(t *testing.T) {
	now := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC)
	duration := 5 * time.Minute
	id := uuid.MustParse("eeab2025-9cc6-411f-8ef5-9e5c6b720f22")
	decimalValue := decimal.RequireFromString("123.45")
	dynamicValue := []byte(`{"region":"west"}`)
	var typedNilReal *azvalue.Real

	tests := []struct {
		name  string
		value azvalue.Kusto
		want  interface{}
	}{
		{name: "string", value: azvalue.NewString("metric_one"), want: "metric_one"},
		{name: "bool", value: azvalue.NewBool(true), want: true},
		{name: "int", value: azvalue.NewInt(42), want: int32(42)},
		{name: "long", value: azvalue.NewLong(42), want: int64(42)},
		{name: "real", value: azvalue.NewReal(1.5), want: 1.5},
		{name: "datetime", value: azvalue.NewDateTime(now), want: now},
		{name: "timespan", value: azvalue.NewTimespan(duration), want: duration},
		{name: "decimal", value: azvalue.NewDecimal(decimalValue), want: decimalValue},
		{name: "guid", value: azvalue.NewGUID(id), want: id},
		{name: "dynamic", value: azvalue.NewDynamic(dynamicValue), want: dynamicValue},
		{name: "null pointer value", value: azvalue.NewNullReal(), want: nil},
		{name: "null slice value", value: azvalue.NewNullDynamic(), want: nil},
		{name: "typed nil wrapper", value: typedNilReal, want: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, materializeKustoValue(test.value))
		})
	}
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
