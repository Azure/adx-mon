package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/metrics"
	azerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"
	clocktesting "k8s.io/utils/clock/testing"
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

func TestExecutor_syncWorkers_SharesExecutorClock(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	e := NewExecutor(ExecutorOpts{
		Clock:       clk,
		Region:      "eastus",
		Concurrency: 2,
		RuleStore:   &fakeRuleStore{rules: []*rules.Rule{{Namespace: "ns", Name: "one", Interval: time.Hour}, {Namespace: "ns", Name: "two", Interval: time.Hour}}},
		KustoClient: &fakeKustoClient{},
	})
	e.syncWorkers(context.Background())
	require.Len(t, e.workers, 2)
	for _, w := range e.workers {
		require.Same(t, clk, w.clock)
	}
}

func TestNewExecutor_DefaultsToRealClock(t *testing.T) {
	e := NewExecutor(ExecutorOpts{})
	require.IsType(t, clock.RealClock{}, e.clock)
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

func TestAsInt64_Errors(t *testing.T) {
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

func TestParseAlertResult_InvalidQueryContext(t *testing.T) {
	tests := []struct {
		name    string
		qc      *QueryContext
		wantErr string
	}{
		{name: "nil context", wantErr: "query context must not be nil"},
		{name: "nil rule", qc: &QueryContext{}, wantErr: "query context rule must not be nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAlertResult(tt.qc, testRow(nil, nil))
			require.EqualError(t, err, tt.wantErr)
		})
	}
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

func TestExecutor_Handler_PropagatesContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"cancelled return", context.Canceled},
		{"nil return", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			qc := &QueryContext{Rule: &rules.Rule{Namespace: "lifecycle", Name: t.Name()}}
			row := testRow(
				azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long)},
				azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1)},
			)
			gauge := metrics.NotificationUnhealthy.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name)
			gauge.Set(NotificationHealthHealthy)
			executor := &Executor{alertCli: &fakeAlerter{createFn: func(deliveryCtx context.Context, _ string, _ alert.Alert) error {
				require.Same(t, ctx, deliveryCtx, "row delivery must inherit the query budget and lifecycle cancellation")
				cancel()
				require.ErrorIs(t, deliveryCtx.Err(), context.Canceled)
				return tc.err
			}}}
			require.ErrorIs(t, executor.HandlerFn(ctx, "https://cluster.kusto.windows.net", qc, row), context.Canceled)
			require.Equal(t, NotificationHealthHealthy, getGaugeValue(t, gauge))
		})
	}
}

func TestExecutor_Handler_CancelsHTTPDelivery(t *testing.T) {
	started := make(chan struct{})
	requestCancelled := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(requestCancelled)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	alertCli, err := alert.NewClient(5 * time.Second)
	require.NoError(t, err)
	executor := &Executor{alertCli: alertCli, alertAddr: server.URL}
	qc := &QueryContext{Rule: &rules.Rule{Namespace: "lifecycle", Name: t.Name()}}
	row := testRow(
		azquery.Columns{testColumn(0, "Title", aztypes.String), testColumn(1, "Severity", aztypes.Long)},
		azvalue.Values{azvalue.NewString("Title"), azvalue.NewLong(1)},
	)
	gauge := metrics.NotificationUnhealthy.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name)
	gauge.Set(NotificationHealthHealthy)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- executor.HandlerFn(ctx, "https://cluster.kusto.windows.net", qc, row) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("HTTP delivery did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("HTTP delivery did not stop on cancellation")
	}
	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("HTTP request did not observe cancellation")
	}
	require.Equal(t, NotificationHealthHealthy, getGaugeValue(t, gauge))
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

func (unsupportedKustoValue) GetValue() any {
	return nil
}

func (unsupportedKustoValue) GetType() aztypes.Column {
	return aztypes.Bool
}

func (unsupportedKustoValue) Unmarshal(any) error {
	return nil
}

func TestExecutor_RunOnce(t *testing.T) {
	ctx := t.Context()
	e := Executor{
		ruleStore: &fakeRuleStore{},
	}

	require.NotPanics(t, func() {
		e.RunOnce(ctx)
	})
	// his is only checkign we don't panic for now. Need to add rules to see we actually get alerts.

}

// TestExecutor_RunOnce_CancelsDerivedContext demonstrates that the context
// RunOnce derives is actually canceled when RunOnce returns. Before the fix,
// RunOnce created `ctx, e.closeFn = context.WithCancel(ctx)` but never called
// cancel, so the derived context (and everything derived from it) lingered
// until the Executor was garbage-collected.
//
// We capture the derived context directly via the runOnceCtx hook (which
// RunOnce invokes with the context it derives, before defer cancel() runs).
// After RunOnce returns, the fix's defer cancel() has canceled the derived
// context. We assert it is canceled. The test's own parent context must remain
// alive, confirming the cancellation came from the executor, not the test.
func TestExecutor_RunOnce_CancelsDerivedContext(t *testing.T) {
	parentCtx, parentCancel := context.WithCancel(t.Context())
	t.Cleanup(parentCancel)

	var mu sync.Mutex
	var derivedCtx context.Context

	e := Executor{
		kustoClient: &fakeKustoClient{},
		ruleStore:   &fakeRuleStore{rules: []*rules.Rule{{Namespace: "ns", Name: "one", Interval: time.Hour}}},
		runOnceCtx: func(ctx context.Context) {
			mu.Lock()
			derivedCtx = ctx
			mu.Unlock()
		},
	}

	e.RunOnce(parentCtx)

	mu.Lock()
	require.NotNil(t, derivedCtx, "RunOnce must have invoked the runOnceCtx hook with the derived context")
	captured := derivedCtx
	mu.Unlock()

	// The test's own parent must still be alive; the cancellation came from the
	// executor's derived context, not from the test.
	require.NoError(t, parentCtx.Err(), "test parent context must remain alive")

	// After RunOnce returns, the fix's defer cancel() must have canceled the
	// derived context. Before the fix it was never canceled.
	require.Eventually(t, func() bool { return captured.Err() != nil },
		2*time.Second, 10*time.Millisecond,
		"RunOnce's derived context was not canceled; it lingers after RunOnce returned")
}

func TestExecutor_syncWorkers_Remove(t *testing.T) {
	ctx := t.Context()
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

// TestExecutor_Close_WaitsForWorkerGoroutine proves that Close() blocks until
// the worker goroutine it spawned has drained. The worker's query blocks on a
// test-controlled gate. If Close() waits (correct), the test releases the gate
// from a side goroutine and Close proceeds. If Close() does not wait (the bug),
// Close returns immediately while the worker is still blocked, and the
// "worker finished before Close returned" flag is false.
func TestExecutor_Close_WaitsForWorkerGoroutine(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	queryEntered := make(chan struct{})
	gate := make(chan struct{}) // closed by the test to unblock the worker's query
	queryReturned := make(chan struct{})
	kusto := &fakeKustoClient{queryFn: func(ctx context.Context, _ *QueryContext, _ func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
		close(queryEntered)
		// Block on the gate only. The worker's own ctx cancel from Close()
		// does NOT unblock this query, so the worker goroutine stays in flight
		// until the test explicitly releases the gate. This isolates the
		// "does Close wait?" question from the "does ctx cancel stop the
		// worker?" behavior.
		<-gate
		close(queryReturned)
		return ctx.Err(), 0
	}}

	store := &fakeRuleStore{rules: []*rules.Rule{{
		Namespace: "ns", Name: "rule",
		Database: "db", Query: "Table | take 1",
		Interval: time.Hour, Destination: "dest",
	}}}

	e := NewExecutor(ExecutorOpts{
		Clock:       clk,
		Region:      "eastus",
		Concurrency: 2,
		RuleStore:   store,
		KustoClient: kusto,
		AlertCli:    &fakeAlerter{},
	})
	require.NoError(t, e.Open(context.Background()))

	// Fire the worker's immediate first query so it blocks on the gate.
	waitForWorkerTimer(t, clk)
	clk.Step(0)
	select {
	case <-queryEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start query")
	}

	// Call Close. With the fix, Close blocks until the worker drains. The
	// worker's query is blocked on the gate, so we must release it from a side
	// goroutine for Close to return. With the bug, Close returns immediately
	// without waiting, so the worker is still blocked on the gate.
	closeGate := make(chan struct{})
	go func() {
		// Wait until the test signals, then release the gate.
		<-closeGate
		close(gate)
	}()

	// Run Close in a goroutine; if it waits, it will block until the gate is
	// released. If it doesn't wait (the bug), it returns immediately.
	closeReturned := make(chan error, 1)
	go func() { closeReturned <- e.Close() }()

	// Give Close a moment. If it returned already, the bug is present.
	select {
	case err := <-closeReturned:
		// Close returned without waiting — that is the bug.
		// Release the gate so we don't leak the worker goroutine.
		close(closeGate)
		require.NoError(t, err)
		t.Fatal("Close() returned before the worker goroutine drained (wg is a no-op)")
	case <-time.After(150 * time.Millisecond):
		// Close is still blocked — it is waiting for the worker. Now release
		// the gate so the worker can finish and Close can return.
		close(closeGate)
		select {
		case err := <-closeReturned:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("Close did not return after the worker drained")
		}
	}
}

func TestExecutor_Close_WaitsForPeriodicSync(t *testing.T) {
	store := &countingRuleStore{rules: []*rules.Rule{{
		Namespace: "ns", Name: "rule",
		Database: "db", Query: "Table | take 1",
		Interval: time.Hour, Destination: "dest",
	}}}

	e := NewExecutor(ExecutorOpts{
		Region:      "eastus",
		Concurrency: 2,
		RuleStore:   store,
		KustoClient: &fakeKustoClient{},
		AlertCli:    &fakeAlerter{},
	})
	ticks := make(chan time.Time)
	e.syncTick = ticks
	require.NoError(t, e.Open(context.Background()))
	baseline := store.Count()
	entered := make(chan struct{})
	gate := make(chan struct{})
	store.mu.Lock()
	store.entered = entered
	store.gate = gate
	store.mu.Unlock()

	ticks <- time.Now()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(gate)
		t.Fatal("periodic sync never entered Rules")
	}
	returned := make(chan error, 1)
	go func() { returned <- e.Close() }()
	select {
	case err := <-returned:
		close(gate)
		t.Fatalf("Close returned before periodic sync finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	require.NoError(t, <-returned)
	require.Equal(t, baseline+1, store.Count())
}

type countingRuleStore struct {
	mu      sync.Mutex
	count   int
	rules   []*rules.Rule
	entered chan struct{}
	gate    chan struct{}
}

func (c *countingRuleStore) Rules() []*rules.Rule {
	c.mu.Lock()
	c.count++
	entered, gate := c.entered, c.gate
	c.mu.Unlock()
	if entered != nil {
		close(entered)
		<-gate
	}
	return c.rules
}

func (c *countingRuleStore) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
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
