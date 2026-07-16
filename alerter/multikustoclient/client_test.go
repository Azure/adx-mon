package multikustoclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/alerter/rules"
	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	azerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	azqueryv1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/stretchr/testify/require"
)

type fakeQueryClient struct {
	nextQueryDataset azquery.IterativeDataset
	nextQueryErr     error
	lastQuery        azkustodata.Statement
	lastQueryOptions []azkustodata.QueryOption

	nextMgmtDataset azqueryv1.Dataset
	nextMgmtErr     error
	lastMgmt        azkustodata.Statement
	lastMgmtOptions []azkustodata.QueryOption

	endpoint string
}

func (f *fakeQueryClient) IterativeQuery(ctx context.Context, db string, query azkustodata.Statement, options ...azkustodata.QueryOption) (azquery.IterativeDataset, error) {
	f.lastQuery = query
	f.lastQueryOptions = options
	return f.nextQueryDataset, f.nextQueryErr
}

func (f *fakeQueryClient) Mgmt(ctx context.Context, db string, query azkustodata.Statement, options ...azkustodata.QueryOption) (azqueryv1.Dataset, error) {
	f.lastMgmt = query
	f.lastMgmtOptions = options
	return f.nextMgmtDataset, f.nextMgmtErr
}

func (f *fakeQueryClient) Endpoint() string {
	return f.endpoint
}

func TestQuery(t *testing.T) {
	resultLimit := 5

	type testcase struct {
		name              string
		rows              []string
		rowErr            error
		rule              *rules.Rule
		queryErr          error
		callbackErr       error
		callbackErrorAt   int
		expectedCallbacks int
		expectedConsumed  int
		expectedReturned  int
		expectError       bool
		expectLimit       bool
		expectedRows      int
		expectCallbackErr bool
	}

	testcases := []testcase{
		{
			name:              "Query with no rows",
			rows:              []string{},
			rule:              &rules.Rule{Database: "dbOne"},
			expectedCallbacks: 0,
			expectedConsumed:  0,
		},
		{
			name:              "Two rows",
			rows:              []string{"rowOne", "rowTwo"},
			rule:              &rules.Rule{Database: "dbOne"},
			expectedCallbacks: 2,
			expectedConsumed:  2,
			expectedReturned:  2,
		},
		{
			name:              "Exact result limit",
			rows:              []string{"rowOne", "rowTwo", "rowThree", "rowFour", "rowFive"},
			rule:              &rules.Rule{Database: "dbOne"},
			expectedCallbacks: 5,
			expectedConsumed:  5,
			expectedReturned:  5,
		},
		{
			name:              "Over result limit sends retained rows after fully draining",
			rows:              []string{"rowOne", "rowTwo", "rowThree", "rowFour", "rowFive", "rowSix"},
			rule:              &rules.Rule{Database: "dbOne"},
			expectedCallbacks: 5,
			expectedConsumed:  6,
			expectError:       true,
			expectLimit:       true,
			expectedRows:      6,
		},
		{
			name:        "Unknown db",
			rows:        []string{},
			rule:        &rules.Rule{Database: "dbUnknown"},
			expectError: true,
		},
		{
			name:        "Client query error",
			rows:        []string{},
			rule:        &rules.Rule{Database: "dbOne"},
			queryErr:    errors.New("query error"),
			expectError: true,
		},
		{
			name:              "Callback error",
			rows:              []string{"rowOne", "rowTwo"},
			rule:              &rules.Rule{Database: "dbOne"},
			callbackErr:       errors.New("callback error"),
			callbackErrorAt:   1,
			expectedCallbacks: 1,
			expectedConsumed:  2,
			expectError:       true,
			expectCallbackErr: true,
		},
		{
			name:              "Callback error preserves exceeded result limit",
			rows:              []string{"rowOne", "rowTwo", "rowThree", "rowFour", "rowFive", "rowSix", "rowSeven"},
			rule:              &rules.Rule{Database: "dbOne"},
			callbackErr:       errors.New("callback error"),
			callbackErrorAt:   3,
			expectedCallbacks: 3,
			expectedConsumed:  7,
			expectError:       true,
			expectLimit:       true,
			expectedRows:      7,
			expectCallbackErr: true,
		},
		{
			name:             "Iterator error after exceeding result limit takes precedence",
			rows:             []string{"rowOne", "rowTwo", "rowThree", "rowFour", "rowFive", "rowSix"},
			rowErr:           errors.New("iterator error after result limit"),
			rule:             &rules.Rule{Database: "dbOne"},
			expectedConsumed: 6,
			expectError:      true,
		},
		{
			name:        "Client mgmt query error",
			rows:        []string{},
			rule:        &rules.Rule{Database: "dbOne", IsMgmtQuery: true},
			queryErr:    errors.New("query error"),
			expectError: true,
		},
		{
			name:             "Query with no rows mgmt query",
			rows:             []string{},
			rule:             &rules.Rule{Database: "dbOne", IsMgmtQuery: true},
			expectedConsumed: 0,
		},
		{
			name:              "Two rows mgmt query",
			rows:              []string{"rowOne", "rowTwo"},
			rule:              &rules.Rule{Database: "dbOne", IsMgmtQuery: true},
			expectedCallbacks: 2,
			expectedConsumed:  0,
			expectedReturned:  2,
		},
		{
			name:              "Over result limit management query sends retained rows",
			rows:              []string{"rowOne", "rowTwo", "rowThree", "rowFour", "rowFive", "rowSix", "rowSeven"},
			rule:              &rules.Rule{Database: "dbOne", IsMgmtQuery: true},
			expectedCallbacks: 5,
			expectedConsumed:  0,
			expectError:       true,
			expectLimit:       true,
			expectedRows:      7,
		},
		{
			name:             "Error after first row - no rows should be sent",
			rows:             []string{"rowOne"},
			rowErr:           errors.New("iterator error after first row"),
			rule:             &rules.Rule{Database: "dbOne"},
			expectedConsumed: 1,
			expectError:      true,
		},
		{
			name:             "Error after third row - no rows should be sent",
			rows:             []string{"rowOne", "rowTwo", "rowThree"},
			rowErr:           errors.New("iterator error after third row"),
			rule:             &rules.Rule{Database: "dbOne"},
			expectedConsumed: 3,
			expectError:      true,
		},
		{
			name:        "Error on first row - no rows should be sent",
			rowErr:      errors.New("iterator error immediately"),
			rule:        &rules.Rule{Database: "dbOne"},
			expectError: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			iterative := newFakeIterativeDataset(tc.rows, tc.rowErr)
			client := &fakeQueryClient{endpoint: "endpointOne"}
			if tc.rule.IsMgmtQuery {
				client.nextMgmtDataset = newFakeDataset(tc.rows)
				client.nextMgmtErr = tc.queryErr
			} else {
				client.nextQueryDataset = iterative
				client.nextQueryErr = tc.queryErr
			}

			multiKustoClient := multiKustoClient{
				clients: map[string]QueryClient{
					"dbOne": client,
				},
				resultLimit: resultLimit,
			}

			ctx := context.Background()
			queryContext, err := engine.NewQueryContext(tc.rule, time.Date(2023, 04, 10, 0, 0, 0, 0, time.UTC), "eastus")
			require.NoError(t, err)

			callbackCounter := 0
			successfulCallbacks := 0
			callback := func(context.Context, string, *engine.QueryContext, azquery.Row) error {
				callbackCounter++
				if callbackCounter == tc.callbackErrorAt {
					return tc.callbackErr
				}
				successfulCallbacks++
				return nil
			}

			err, rowsReturned := multiKustoClient.Query(ctx, queryContext, callback)

			require.Equal(t, tc.expectedCallbacks, callbackCounter)
			require.Equal(t, tc.expectedReturned, rowsReturned)
			if tc.rule.IsMgmtQuery {
				require.False(t, iterative.closed)
			} else if tc.queryErr == nil && tc.rule.Database == "dbOne" {
				require.True(t, iterative.closed)
				require.Equal(t, tc.expectedConsumed, iterative.table.consumed)
			}
			if tc.expectLimit {
				var limitErr *engine.ResultLimitExceededError
				require.ErrorAs(t, err, &limitErr)
				require.Equal(t, resultLimit, limitErr.Limit)
				require.Equal(t, tc.expectedRows, limitErr.RowsProcessed)
				require.NotErrorIs(t, err, alert.ErrTooManyRequests)
			}
			if tc.expectCallbackErr {
				require.ErrorIs(t, err, tc.callbackErr)
				require.Equal(t, tc.expectedCallbacks-1, successfulCallbacks)
			}
			if tc.rowErr != nil {
				require.ErrorIs(t, err, tc.rowErr)
				var limitErr *engine.ResultLimitExceededError
				require.False(t, errors.As(err, &limitErr))
			}
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFindCaseInsensitiveMatch(t *testing.T) {
	client := multiKustoClient{
		clients: map[string]QueryClient{
			"Cluster_State": &fakeQueryClient{endpoint: "endpoint"},
			"Metrics":       &fakeQueryClient{endpoint: "endpoint"},
		},
	}

	match := client.FindCaseInsensitiveMatch("cluster_state")
	require.Equal(t, "Cluster_State", match)

	match = client.FindCaseInsensitiveMatch("CLUSTER_STATE")
	require.Equal(t, "Cluster_State", match)

	match = client.FindCaseInsensitiveMatch("unknown")
	require.Empty(t, match)

	match = client.FindCaseInsensitiveMatch("metrics")
	require.Equal(t, "Metrics", match)
}

func TestQuery_UsesConstructedWrappedQuery(t *testing.T) {
	client := &fakeQueryClient{endpoint: "endpoint", nextQueryDataset: newFakeIterativeDataset(nil, nil)}
	multiKustoClient := multiKustoClient{
		clients:     map[string]QueryClient{"dbOne": client},
		resultLimit: 5,
	}
	queryContext, err := engine.NewQueryContext(&rules.Rule{
		Database: "dbOne",
		Interval: time.Hour,
		Query:    "Table | where Region == _region",
	}, time.Date(2023, 04, 10, 0, 0, 0, 0, time.UTC), "eastus")
	require.NoError(t, err)

	err, _ = multiKustoClient.Query(context.Background(), queryContext, func(context.Context, string, *engine.QueryContext, azquery.Row) error {
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, "\nlet _startTime = _adxmonStartTime;\nlet _endTime = _adxmonEndTime;\nlet _region = _adxmonRegion;\nTable | where Region == _region\n", client.lastQuery.String())
	require.Len(t, client.lastQueryOptions, 1)
}

func TestQuery_UnknownDB_EnhancedError(t *testing.T) {
	client := multiKustoClient{
		clients: map[string]QueryClient{
			"Cluster_State": &fakeQueryClient{endpoint: "endpoint"},
			"Metrics":       &fakeQueryClient{endpoint: "endpoint"},
		},
		availableDatabases: []string{"Cluster_State", "Metrics"},
		resultLimit:        5,
	}

	ctx := context.Background()
	queryContext, err := engine.NewQueryContext(&rules.Rule{Database: "cluster_state"}, time.Now(), "eastus")
	require.NoError(t, err)

	err, _ = client.Query(ctx, queryContext, func(context.Context, string, *engine.QueryContext, azquery.Row) error {
		return nil
	})

	require.Error(t, err)

	var unknownDBErr *engine.UnknownDBError
	require.ErrorAs(t, err, &unknownDBErr)
	require.Equal(t, "cluster_state", unknownDBErr.DB)
	require.Equal(t, "Cluster_State", unknownDBErr.CaseInsensitiveMatch)
	require.Equal(t, []string{"Cluster_State", "Metrics"}, unknownDBErr.AvailableDatabases)

	errMsg := err.Error()
	require.Contains(t, errMsg, `did you mean "Cluster_State"?`)
	require.Contains(t, errMsg, "--kusto-endpoint")
}

type fakeDataset struct {
	azquery.BaseDataset
	tables []azquery.Table
}

func newFakeDataset(values []string) azqueryv1.Dataset {
	base := azquery.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	table := newFakeTable(base, values)
	return &fakeV1Dataset{fakeDataset: fakeDataset{BaseDataset: base, tables: []azquery.Table{table}}}
}

func (d *fakeDataset) Tables() []azquery.Table { return d.tables }

type fakeV1Dataset struct{ fakeDataset }

func (d *fakeV1Dataset) Index() []azqueryv1.TableIndexRow  { return nil }
func (d *fakeV1Dataset) Status() []azqueryv1.QueryStatus   { return nil }
func (d *fakeV1Dataset) Info() []azqueryv1.QueryProperties { return nil }

type fakeIterativeDataset struct {
	azquery.BaseDataset
	table  *fakeIterativeTable
	closed bool
}

func newFakeIterativeDataset(values []string, rowErr error) *fakeIterativeDataset {
	base := azquery.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	return &fakeIterativeDataset{BaseDataset: base, table: newFakeIterativeTable(base, values, rowErr)}
}

func (d *fakeIterativeDataset) Tables() <-chan azquery.TableResult {
	out := make(chan azquery.TableResult, 1)
	out <- azquery.TableResultSuccess(d.table)
	close(out)
	return out
}

func (d *fakeIterativeDataset) ToDataset() (azquery.Dataset, error) { return nil, nil }

func (d *fakeIterativeDataset) Close() error {
	d.closed = true
	return nil
}

type fakeIterativeTable struct {
	azquery.BaseTable
	rows     []azquery.Row
	rowErr   error
	consumed int
}

func newFakeIterativeTable(base azquery.BaseDataset, values []string, rowErr error) *fakeIterativeTable {
	table := &fakeIterativeTable{}
	table.BaseTable = azquery.NewBaseTable(base, 0, "", "QueryResult", "QueryResult", []azquery.Column{azquery.NewColumn(0, "columnOne", aztypes.String)})
	for i, val := range values {
		table.rows = append(table.rows, azquery.NewRow(table, i, azvalue.Values{azvalue.NewString(val)}))
	}
	table.rowErr = rowErr
	return table
}

func (t *fakeIterativeTable) Rows() <-chan azquery.RowResult {
	out := make(chan azquery.RowResult)
	go func() {
		defer close(out)
		for _, row := range t.rows {
			t.consumed++
			out <- azquery.RowResultSuccess(row)
		}
		if t.rowErr != nil {
			out <- azquery.RowResultError(t.rowErr)
		}
	}()
	return out
}

func (t *fakeIterativeTable) ToTable() (azquery.Table, error) {
	return azquery.NewTable(t.BaseTable, t.rows), nil
}

func newFakeTable(base azquery.BaseDataset, values []string) azquery.Table {
	baseTable, ok := base.(azquery.BaseTable)
	if !ok {
		baseTable = azquery.NewBaseTable(base, 0, "", "QueryResult", "QueryResult", []azquery.Column{azquery.NewColumn(0, "columnOne", aztypes.String)})
	}
	rows := make([]azquery.Row, 0, len(values))
	for i, val := range values {
		rows = append(rows, azquery.NewRow(baseTable, i, azvalue.Values{azvalue.NewString(val)}))
	}
	return azquery.NewTable(baseTable, rows)
}
