package multikustoclient

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	"github.com/stretchr/testify/require"
)

type fakeQueryClient struct {
	nextQueryIter *kusto.RowIterator
	nextQueryErr  error

	nextMgmtIter *kusto.RowIterator
	nextMgmtErr  error

	endpoint string
}

func (f *fakeQueryClient) Query(ctx context.Context, db string, query kusto.Statement, options ...kusto.QueryOption) (*kusto.RowIterator, error) {
	return f.nextQueryIter, f.nextQueryErr
}

func (f *fakeQueryClient) Mgmt(ctx context.Context, db string, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	return f.nextMgmtIter, f.nextMgmtErr
}

func (f *fakeQueryClient) Endpoint() string {
	return f.endpoint
}

func TestQuery(t *testing.T) {
	maxNotifications := 5

	type testcase struct {
		name         string
		rows         *kusto.MockRows
		rule         *rules.Rule
		queryErr     error
		callbackErr  error
		expectedSent int
		expectError  bool
	}

	testcases := []testcase{
		{
			name: "Query with no rows",
			rows: newRows(t, []string{}),
			rule: &rules.Rule{
				Database: "dbOne",
			},
			queryErr:     nil,
			callbackErr:  nil,
			expectedSent: 0,
			expectError:  false,
		},
		{
			name: "Two rows",
			rows: newRows(t, []string{
				"rowOne",
				"rowTwo",
			}),
			rule: &rules.Rule{
				Database: "dbOne",
			},
			queryErr:     nil,
			callbackErr:  nil,
			expectedSent: 2,
			expectError:  false,
		},
		{
			name: "Max notifications",
			rows: newRows(t, []string{
				"rowOne",
				"rowTwo",
				"rowThree",
				"rowFour",
				"rowFive",
			}),
			rule: &rules.Rule{
				Database: "dbOne",
			},
			queryErr:     nil,
			callbackErr:  nil,
			expectedSent: 5,
			expectError:  false,
		},
		{
			name: "Over max notifications",
			rows: newRows(t, []string{
				"rowOne",
				"rowTwo",
				"rowThree",
				"rowFour",
				"rowFive",
				"rowSix",
			}),
			rule: &rules.Rule{
				Database: "dbOne",
			},
			queryErr:     nil,
			callbackErr:  nil,
			expectedSent: 5, // first 5 sent, then error
			expectError:  true,
		},
		{
			name: "Unknown db",
			rows: newRows(t, []string{}),
			rule: &rules.Rule{
				Database: "dbUnknown",
			},
			queryErr:     nil,
			callbackErr:  nil,
			expectedSent: 0,
			expectError:  true,
		},
		{
			name: "Client query error",
			rows: newRows(t, []string{}),
			rule: &rules.Rule{
				Database: "dbOne",
			},
			queryErr:     errors.New("query error"),
			callbackErr:  nil,
			expectedSent: 0,
			expectError:  true,
		},
		{
			name: "Callback error",
			rows: newRows(t, []string{
				"rowOne",
				"rowTwo",
			}),
			rule: &rules.Rule{
				Database: "dbOne",
			},
			queryErr:     nil,
			callbackErr:  errors.New("callback error"),
			expectedSent: 1, // still attempts to send first, bails out
			expectError:  true,
		},
		{
			name: "Client mgmt query error",
			rows: newRows(t, []string{}),
			rule: &rules.Rule{
				Database:    "dbOne",
				IsMgmtQuery: true,
			},
			queryErr:     errors.New("query error"),
			callbackErr:  nil,
			expectedSent: 0,
			expectError:  true,
		},
		{
			name: "Query with no rows mgmt query",
			rows: newRows(t, []string{}),
			rule: &rules.Rule{
				Database:    "dbOne",
				IsMgmtQuery: true,
			},
			queryErr:     nil,
			callbackErr:  nil,
			expectedSent: 0,
			expectError:  false,
		},
		{
			name: "Two rows mgmt query",
			rows: newRows(t, []string{
				"rowOne",
				"rowTwo",
			}),
			rule: &rules.Rule{
				Database:    "dbOne",
				IsMgmtQuery: true,
			},
			queryErr:     nil,
			callbackErr:  nil,
			expectedSent: 2,
			expectError:  false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			rowIterator := &kusto.RowIterator{}
			err := rowIterator.Mock(tc.rows)
			require.NoError(t, err)

			var client QueryClient
			if tc.rule.IsMgmtQuery {
				client = &fakeQueryClient{
					nextMgmtIter: rowIterator,
					nextMgmtErr:  tc.queryErr,
					endpoint:     "endpointOne",
				}
			} else {
				client = &fakeQueryClient{
					nextQueryIter: rowIterator,
					nextQueryErr:  tc.queryErr,
					endpoint:      "endpointOne",
				}
			}

			multiKustoClient := multiKustoClient{
				clients: map[string]QueryClient{
					"dbOne": client,
				},
				maxNotifications: maxNotifications,
			}

			ctx := context.Background()
			queryContext := &engine.QueryContext{
				Rule: tc.rule,
				Stmt: kusto.NewStmt(``, kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd("query"),
			}

			callbackCounter := 0
			callback := func(context.Context, string, *engine.QueryContext, *table.Row) error {
				callbackCounter++
				return tc.callbackErr
			}

			err, _ = multiKustoClient.Query(ctx, queryContext, callback)

			require.Equal(t, tc.expectedSent, callbackCounter)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func newRows(t *testing.T, values []string) *kusto.MockRows {
	t.Helper()

	rows, err := kusto.NewMockRows(table.Columns{
		{Name: "columnOne", Type: types.String},
	})
	require.NoError(t, err)
	for _, val := range values {
		err = rows.Row(value.Values{value.String{Value: val, Valid: true}})
		require.NoError(t, err)
	}
	return rows
}
