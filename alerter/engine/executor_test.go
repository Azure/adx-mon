package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	azerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestExecutor_Handler_MissingTitle(t *testing.T) {
	e := Executor{
		alertCli: &fakeAlertClient{},
	}

	rule := &rules.Rule{}
	qc := &QueryContext{
		Rule: rule,
	}

	row := testRow(
		azquery.Columns{
			testColumn(0, "Severity", aztypes.Long),
			testColumn(1, "Summary", aztypes.String),
			testColumn(2, "CorrelationId", aztypes.String),
		},
		azvalue.Values{
			azvalue.NewLong(1),
			azvalue.NewString("Fake Alert Summary"),
			azvalue.NewString("Fake CorrelationId"),
		},
	)
	err := e.HandlerFn(context.Background(), "http://endpoint", qc, row)
	require.ErrorContains(t, err, "title must be between 1 and 512 chars")
	require.True(t, isUserError(err))
}

func TestNewExecutor_UsesConfiguredConcurrency(t *testing.T) {
	e := NewExecutor(ExecutorOpts{
		Concurrency: 12,
	})

	require.NotNil(t, e.querySlots)
	require.Equal(t, 12, cap(e.querySlots))
}

func TestNewExecutor_DefaultsConcurrency(t *testing.T) {
	e := NewExecutor(ExecutorOpts{})

	require.NotNil(t, e.querySlots)
	require.Equal(t, queue.DefaultConcurrency, cap(e.querySlots))
}

func TestExecutor_newWorker_UsesExecutorQueue(t *testing.T) {
	slots := make(chan struct{}, 7)
	e := &Executor{
		region:     "eastus",
		querySlots: slots,
	}

	w := e.newWorker(&rules.Rule{Namespace: "ns", Name: "rule"})

	require.NotNil(t, w)
	require.Equal(t, slots, w.querySlots)
}

func TestExecutor_Handler_Severity(t *testing.T) {

	for _, tt := range []struct {
		desc     string
		columns  azquery.Columns
		values   azvalue.Values
		err      string
		severity int
	}{
		{
			desc:    "missing severity",
			columns: azquery.Columns{testColumn(0, "Title", aztypes.String)},
			values:  azvalue.Values{azvalue.NewString("Title")},
			err:     "severity must be specified",
		},
		{
			desc:    "severity not convertable to a number",
			columns: azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.String)},
			values:  azvalue.Values{azvalue.NewString("Title"), azvalue.NewString("not a number")},
			err:     "failed to convert severity to int",
		},
		{
			desc:     "severity as long",
			columns:  azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long)},
			values:   azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1)},
			err:      "",
			severity: 1,
		},
		{
			desc:     "severity as string",
			columns:  azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.String)},
			values:   azvalue.Values{azvalue.NewString("Title"), azvalue.NewString("10")},
			err:      "",
			severity: 10,
		},
		{
			desc:     "severity as real",
			columns:  azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Real)},
			values:   azvalue.Values{azvalue.NewString("Title"), azvalue.NewReal(10.1)},
			err:      "",
			severity: 10,
		},
		{
			desc:     "severity as int",
			columns:  azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Int)},
			values:   azvalue.Values{azvalue.NewString("Title"), azvalue.NewInt(10)},
			err:      "",
			severity: 10,
		},
		{
			desc:     "severity as decimal",
			columns:  azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Decimal)},
			values:   azvalue.Values{azvalue.NewString("Title"), azvalue.NewDecimal(decimal.RequireFromString("10.1"))},
			err:      "",
			severity: 10,
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			row := testRow(tt.columns, tt.values)

			client := &fakeAlertClient{}
			e := Executor{
				alertCli: client,
			}

			rule := &rules.Rule{}
			qc := &QueryContext{
				Rule: rule,
			}

			err := e.HandlerFn(context.Background(), "http://endpoint", qc, row)
			if tt.err == "" {
				require.NoError(t, err)
				require.Equal(t, tt.severity, client.alert.Severity)
			} else {
				require.ErrorContains(t, err, tt.err)
				require.True(t, isUserError(err))
			}
		})
	}
}

func TestExecutor_asInt64_Errors(t *testing.T) {
	for _, tt := range []struct {
		desc  string
		value azvalue.Kusto
		err   string
	}{
		{
			desc:  "nil value",
			value: nil,
			err:   "failed to convert severity to int: <nil>",
		},
		{
			desc:  "invalid string",
			value: azvalue.NewString("not a number"),
			err:   "failed to convert severity to int: strconv.ParseInt",
		},
		{
			desc:  "null long",
			value: azvalue.NewNullLong(),
			err:   "failed to convert severity to int:",
		},
		{
			desc:  "null real",
			value: azvalue.NewNullReal(),
			err:   "failed to convert severity to int:",
		},
		{
			desc:  "null int",
			value: azvalue.NewNullInt(),
			err:   "failed to convert severity to int:",
		},
		{
			desc:  "null decimal",
			value: azvalue.NewNullDecimal(),
			err:   "failed to convert severity to int:",
		},
		{
			desc:  "unsupported type",
			value: unsupportedKustoValue{},
			err:   "failed to convert severity to int:",
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := asInt64(tt.value)
			require.ErrorContains(t, err, tt.err)
		})
	}
}

func TestParseAlertResult(t *testing.T) {
	qc := &QueryContext{
		Rule: &rules.Rule{
			Namespace: "rulesns",
			Name:      "rulename",
			Database:  "database",
			Query:     "source | where Secret == true",
		},
		Query: "rendered query that must remain delivery-only",
	}
	row := testRow(
		azquery.Columns{
			testColumn(0, "TITLE", aztypes.String),
			testColumn(1, "description", aztypes.String),
			testColumn(2, "SeVeRiTy", aztypes.String),
			testColumn(3, "RECIPIENT", aztypes.String),
			testColumn(4, "Summary", aztypes.String),
			testColumn(5, "correlationID", aztypes.String),
			testColumn(6, "CustomField", aztypes.String),
		},
		azvalue.Values{
			azvalue.NewString("Alert title"),
			azvalue.NewString("Alert description"),
			azvalue.NewString("3"),
			azvalue.NewString("fallback destination"),
			azvalue.NewString("Original summary"),
			azvalue.NewString("entity"),
			azvalue.NewString("custom value"),
		},
	)

	got, err := ParseAlertResult(qc, row)
	require.NoError(t, err)
	require.Equal(t, AlertResult{
		Destination:   "fallback destination",
		Title:         "Alert title",
		Summary:       "Original summary",
		Description:   "Alert description",
		Severity:      3,
		Source:        "rulesns/rulename",
		CorrelationID: "rulesns/rulename://entity",
		CustomFields:  map[string]string{"CustomField": "custom value"},
	}, got)

	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), qc.Query)
	require.NotContains(t, string(encoded), "query=")
}

func TestParseAlertResult_RuleDestinationTakesPrecedence(t *testing.T) {
	qc := &QueryContext{Rule: &rules.Rule{Destination: "rule destination"}}
	row := testRow(
		azquery.Columns{
			testColumn(0, "Title", aztypes.String),
			testColumn(1, "Severity", aztypes.Long),
			testColumn(2, "Recipient", aztypes.String),
		},
		azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewString("recipient")},
	)

	got, err := ParseAlertResult(qc, row)
	require.NoError(t, err)
	require.Equal(t, "rule destination", got.Destination)
}

func TestParseAlertResult_Validation(t *testing.T) {
	tests := []struct {
		name    string
		columns azquery.Columns
		values  azvalue.Values
		wantErr string
	}{
		{
			name:    "missing title",
			columns: azquery.Columns{testColumn(0, "Severity", aztypes.Long)},
			values:  azvalue.Values{azvalue.NewLong(1)},
			wantErr: "title must be between 1 and 512 chars",
		},
		{
			name:    "missing severity",
			columns: azquery.Columns{testColumn(0, "Title", aztypes.String)},
			values:  azvalue.Values{azvalue.NewString("Title")},
			wantErr: "severity must be specified",
		},
		{
			name: "duplicate reserved field",
			columns: azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
				testColumn(2, "severity", aztypes.Long),
			},
			values:  azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewLong(2)},
			wantErr: `query results include multiple columns for reserved alert field "Severity": Severity, severity`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAlertResult(&QueryContext{Rule: &rules.Rule{}}, testRow(tt.columns, tt.values))
			require.ErrorContains(t, err, tt.wantErr)
			require.True(t, isUserError(err))
		})
	}
}

func TestParseAlertResult_MalformedRow(t *testing.T) {
	tests := []struct {
		name    string
		columns azquery.Columns
		values  azvalue.Values
		wantErr string
	}{
		{
			name: "extra value",
			columns: azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
			},
			values:  azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewString("extra")},
			wantErr: "query result row has 2 columns and 3 values",
		},
		{
			name: "missing value",
			columns: azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
			},
			values:  azvalue.Values{azvalue.NewString("Title")},
			wantErr: "query result row has 2 columns and 1 values",
		},
		{
			name: "nil non-severity value",
			columns: azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
				testColumn(2, "Summary", aztypes.String),
			},
			values:  azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), nil},
			wantErr: `query result column "Summary" has nil value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAlertResult(&QueryContext{Rule: &rules.Rule{}}, testRow(tt.columns, tt.values))
			require.ErrorContains(t, err, tt.wantErr)
			require.True(t, isUserError(err))
		})
	}
}

func TestExecutor_Handler_DeliversEnrichedAlert(t *testing.T) {
	client := &fakeAlertClient{}
	executor := &Executor{alertCli: client, alertAddr: "http://alert-service"}
	qc := &QueryContext{
		Rule: &rules.Rule{
			Namespace:   "rulesns",
			Name:        "rulename",
			Database:    "database",
			Destination: "rule destination",
		},
		Query: "Table | take 1",
	}
	row := testRow(
		azquery.Columns{
			testColumn(0, "Title", aztypes.String),
			testColumn(1, "Summary", aztypes.String),
			testColumn(2, "Description", aztypes.String),
			testColumn(3, "Severity", aztypes.Long),
			testColumn(4, "CorrelationId", aztypes.String),
			testColumn(5, "Custom", aztypes.String),
		},
		azvalue.Values{
			azvalue.NewString("Title"),
			azvalue.NewString("Original summary"),
			azvalue.NewString("Description"),
			azvalue.NewLong(2),
			azvalue.NewString("entity"),
			azvalue.NewString("value"),
		},
	)

	err := executor.HandlerFn(context.Background(), "https://cluster.kusto.windows.net", qc, row)
	require.NoError(t, err)
	wantSummary, err := KustoQueryLinks("Original summary", qc.Query, "https://cluster.kusto.windows.net", "database")
	require.NoError(t, err)
	require.Equal(t, "http://alert-service/alerts", client.endpoint)
	require.Equal(t, alert.Alert{
		Destination:   "rule destination",
		Title:         "Title",
		Summary:       wantSummary,
		Description:   "Description",
		Severity:      2,
		Source:        "rulesns/rulename",
		CorrelationID: "rulesns/rulename://entity",
		CustomFields:  map[string]string{"Custom": "value"},
	}, client.alert)
	require.Contains(t, client.alert.Summary, qc.Query)
	require.Contains(t, client.alert.Summary, "https://cluster.kusto.windows.net/database?query=")
}

func TestClampInt64ToInt(t *testing.T) {
	for _, tt := range []struct {
		desc string
		in   int64
		want int
	}{
		{
			desc: "within range",
			in:   10,
			want: 10,
		},
		{
			desc: "max int",
			in:   int64(math.MaxInt),
			want: math.MaxInt,
		},
		{
			desc: "min int",
			in:   int64(math.MinInt),
			want: math.MinInt,
		},
		{
			desc: "max int64",
			in:   math.MaxInt64,
			want: math.MaxInt,
		},
		{
			desc: "min int64",
			in:   math.MinInt64,
			want: math.MinInt,
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			require.Equal(t, tt.want, clampInt64ToInt(tt.in))
		})
	}
}

func TestExecutor_Handler_DuplicateReservedColumnsDifferentCase(t *testing.T) {
	row := testRow(
		azquery.Columns{
			testColumn(0, "Title", aztypes.String),
			testColumn(1, "Severity", aztypes.Long),
			testColumn(2, "severity", aztypes.Long),
		},
		azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewLong(2)},
	)
	err := (&Executor{alertCli: &fakeAlertClient{}}).HandlerFn(context.Background(), "http://endpoint", &QueryContext{Rule: &rules.Rule{}}, row)
	require.ErrorContains(t, err, `query results include multiple columns for reserved alert field "Severity": Severity, severity`)
	require.True(t, isUserError(err))
}

func TestExecutor_Handler_DuplicateCustomColumnsDifferentCase(t *testing.T) {
	client := &fakeAlertClient{}
	row := testRow(
		azquery.Columns{
			testColumn(0, "Title", aztypes.String),
			testColumn(1, "Severity", aztypes.Long),
			testColumn(2, "CustomField", aztypes.String),
			testColumn(3, "customfield", aztypes.String),
		},
		azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewString("upper"), azvalue.NewString("lower")},
	)
	err := (&Executor{alertCli: client}).HandlerFn(context.Background(), "http://endpoint", &QueryContext{Rule: &rules.Rule{Destination: "destination"}}, row)
	require.NoError(t, err)
	require.Equal(t, "upper", client.alert.CustomFields["CustomField"])
	require.Equal(t, "lower", client.alert.CustomFields["customfield"])
}

func TestExecutor_Handler_CorrelationId(t *testing.T) {
	ruleNamespace := "rulesns"
	ruleName := "rulename"
	// NOTE: This prefix is used by rule destinations to automitigate rules.
	// Do not change without consideration.
	prefix := fmt.Sprintf("%s/%s://", ruleNamespace, ruleName)

	testcases := []struct {
		desc          string
		columns       azquery.Columns
		values        azvalue.Values
		correlationId string
	}{
		{
			desc:          "normal correlation id",
			columns:       azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long), testColumn(2, "CorrelationId", aztypes.String)},
			values:        azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewString("Fake CorrelationId")},
			correlationId: fmt.Sprintf("%s%s", prefix, "Fake CorrelationId"),
		},
		{
			desc:          "correlation id already has prefix",
			columns:       azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long), testColumn(2, "CorrelationId", aztypes.String)},
			values:        azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewString("rulesns/rulename://Fake Correlation Id")},
			correlationId: fmt.Sprintf("%s%s", prefix, "Fake Correlation Id"),
		},
		{
			desc:          "empty correlation id",
			columns:       azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long), testColumn(2, "CorrelationId", aztypes.String)},
			values:        azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1), azvalue.NewString("")},
			correlationId: "",
		},
		{
			desc:          "no correlation id",
			columns:       azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long)},
			values:        azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1)},
			correlationId: "",
		},
	}

	for _, tt := range testcases {
		t.Run(tt.desc, func(t *testing.T) {
			client := &fakeAlertClient{}
			e := Executor{
				alertCli: client,
			}

			rule := &rules.Rule{
				Name:      ruleName,
				Namespace: ruleNamespace,
			}
			qc := &QueryContext{
				Rule: rule,
			}

			err := e.HandlerFn(context.Background(), "http://endpoint", qc, testRow(tt.columns, tt.values))
			require.NoError(t, err)
			require.Equal(t, tt.correlationId, client.alert.CorrelationID)
		})
	}
}

func testRow(columns azquery.Columns, values azvalue.Values) azquery.Row {
	base := azquery.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	table := azquery.NewBaseTable(base, 0, "", "QueryResult", "QueryResult", columns)
	return azquery.NewRow(table, 0, values)
}

func testColumn(index int, name string, columnType aztypes.Column) azquery.Column {
	return azquery.NewColumn(index, name, columnType)
}

type unsupportedKustoValue struct{}

func (unsupportedKustoValue) String() string {
	panic("String should not be called")
}

func (unsupportedKustoValue) Convert(reflect.Value) error {
	return nil
}

func (unsupportedKustoValue) GetValue() interface{} {
	return nil
}

func (unsupportedKustoValue) GetType() aztypes.Column {
	return aztypes.Bool
}

func (unsupportedKustoValue) Unmarshal(interface{}) error {
	return nil
}

func TestExecutor_RunOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := Executor{
		ruleStore: &fakeRuleStore{},
	}

	require.NotPanics(t, func() {
		e.RunOnce(ctx)
	})
	// his is only checkign we don't panic for now. Need to add rules to see we actually get alerts.

}

func TestExecutor_syncWorkers_Remove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := Executor{
		ruleStore: &fakeRuleStore{},
		workers: map[string]*worker{
			"alert": NewWorker(&WorkerConfig{Rule: &rules.Rule{Name: "alert"}, Region: "eastus", KustoClient: &fakeKustoClient{}, AlertClient: &fakeAlerter{}}),
		},
	}

	require.Equal(t, 1, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 0, len(e.workers))
}

func TestExecutor_syncWorkers_Add(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := Executor{
		closeFn: cancel,
		ruleStore: &fakeRuleStore{
			rules: []*rules.Rule{
				{
					Name:     "alert",
					Interval: 10 * time.Second,
				},
			},
		},
		kustoClient: &fakeKustoClient{},
		workers:     map[string]*worker{},
	}

	require.Equal(t, 0, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
}

func TestExecutor_syncWorkers_NoChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := Executor{
		closeFn: cancel,
		ruleStore: &fakeRuleStore{
			rules: []*rules.Rule{
				{
					Name:     "alert",
					Interval: 10 * time.Second,
				},
			},
		},
		kustoClient: &fakeKustoClient{},
		workers:     map[string]*worker{},
	}

	require.Equal(t, 0, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
}

func TestExecutor_syncWorkers_Changed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeRuleStore{
		rules: []*rules.Rule{
			{
				Name:      "alert",
				Namespace: "foo",
				Interval:  10 * time.Second,
			},
		},
	}
	e := Executor{
		closeFn:     cancel,
		ruleStore:   store,
		kustoClient: &fakeKustoClient{},
		workers:     map[string]*worker{},
	}

	require.Equal(t, 0, len(e.workers))
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
	store.rules[0] = &rules.Rule{
		Version:   "changed",
		Name:      "alert",
		Namespace: "foo",
		Interval:  20 * time.Second,
	}
	e.syncWorkers(ctx)
	require.Equal(t, 1, len(e.workers))
}

type fakeAlertClient struct {
	endpoint string
	alert    alert.Alert
}

func (f *fakeAlertClient) Create(ctx context.Context, endpoint string, alert alert.Alert) error {
	f.endpoint = endpoint
	f.alert = alert
	return nil
}

type fakeRuleStore struct {
	rules []*rules.Rule
}

func (f *fakeRuleStore) Rules() []*rules.Rule {
	return f.rules
}
