package alerter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/alerter/rules"
	azerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/stretchr/testify/require"
)

func TestGenerateBacktestWindows(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		end      time.Time
		interval time.Duration
		want     []BacktestWindow
	}{
		{
			name:     "exact multiple",
			end:      start.Add(10 * time.Minute),
			interval: 5 * time.Minute,
			want: []BacktestWindow{
				{Index: 0, Start: start, End: start.Add(5 * time.Minute)},
				{Index: 1, Start: start.Add(5 * time.Minute), End: start.Add(10 * time.Minute)},
			},
		},
		{
			name:     "final partial",
			end:      start.Add(12 * time.Minute),
			interval: 5 * time.Minute,
			want: []BacktestWindow{
				{Index: 0, Start: start, End: start.Add(5 * time.Minute)},
				{Index: 1, Start: start.Add(5 * time.Minute), End: start.Add(10 * time.Minute)},
				{Index: 2, Start: start.Add(10 * time.Minute), End: start.Add(12 * time.Minute)},
			},
		},
		{
			name:     "shorter than interval",
			end:      start.Add(2 * time.Minute),
			interval: 5 * time.Minute,
			want: []BacktestWindow{
				{Index: 0, Start: start, End: start.Add(2 * time.Minute)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := validBacktestOptions(start, tt.end)
			got, err := GenerateBacktestWindows(opts, tt.interval)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGenerateBacktestWindows_UTCAndDeterministicOrder(t *testing.T) {
	location := time.FixedZone("test-offset", -7*60*60)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, location)
	opts := validBacktestOptions(start, start.Add(12*time.Minute))

	first, err := GenerateBacktestWindows(opts, 5*time.Minute)
	require.NoError(t, err)
	second, err := GenerateBacktestWindows(opts, 5*time.Minute)
	require.NoError(t, err)
	require.Equal(t, first, second)

	for index, window := range first {
		require.Equal(t, index, window.Index)
		require.Equal(t, time.UTC, window.Start.Location())
		require.Equal(t, time.UTC, window.End.Location())
		if index > 0 {
			require.Equal(t, first[index-1].End, window.Start)
		}
	}
}

func TestGenerateBacktestWindows_InvalidInterval(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	opts := validBacktestOptions(start, start.Add(time.Hour))

	for _, interval := range []time.Duration{0, -time.Minute} {
		_, err := GenerateBacktestWindows(opts, interval)
		require.ErrorContains(t, err, "interval")
	}
}

func TestValidateBacktestOptions(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		modify func(*BacktestOptions)
		field  string
	}{
		{name: "equal range", modify: func(opts *BacktestOptions) { opts.End = opts.Start }, field: "start"},
		{name: "reversed range", modify: func(opts *BacktestOptions) { opts.End = opts.Start.Add(-time.Second) }, field: "start"},
		{name: "zero concurrency", modify: func(opts *BacktestOptions) { opts.Concurrency = 0 }, field: "concurrency"},
		{name: "negative concurrency", modify: func(opts *BacktestOptions) { opts.Concurrency = -1 }, field: "concurrency"},
		{name: "zero result limit", modify: func(opts *BacktestOptions) { opts.MaxResultsPerWindow = 0 }, field: "max results"},
		{name: "negative result limit", modify: func(opts *BacktestOptions) { opts.MaxResultsPerWindow = -1 }, field: "max results"},
		{name: "zero query timeout", modify: func(opts *BacktestOptions) { opts.QueryTimeout = 0 }, field: "query timeout"},
		{name: "negative query timeout", modify: func(opts *BacktestOptions) { opts.QueryTimeout = -time.Second }, field: "query timeout"},
		{name: "zero max windows", modify: func(opts *BacktestOptions) { opts.MaxWindows = 0 }, field: "max windows"},
		{name: "negative max windows", modify: func(opts *BacktestOptions) { opts.MaxWindows = -1 }, field: "max windows"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := validBacktestOptions(start, start.Add(time.Hour))
			tt.modify(&opts)
			require.ErrorContains(t, ValidateBacktestOptions(opts), tt.field)
		})
	}
}

func TestGenerateBacktestWindows_MaxWindows(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	opts := validBacktestOptions(start, start.Add(11*time.Minute))
	opts.MaxWindows = 2

	windows, err := GenerateBacktestWindows(opts, 5*time.Minute)
	require.ErrorContains(t, err, "maximum of 2 windows")
	require.Nil(t, windows)
}

func TestBacktestSummaryAndSort(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	results := []BacktestWindowResult{
		{BacktestWindow: BacktestWindow{Index: 2, Start: start.Add(10 * time.Minute), End: start.Add(15 * time.Minute)}, Status: BacktestWindowStatusError},
		{BacktestWindow: BacktestWindow{Index: 0, Start: start, End: start.Add(5 * time.Minute)}, Status: BacktestWindowStatusClear},
		{BacktestWindow: BacktestWindow{Index: 1, Start: start.Add(5 * time.Minute), End: start.Add(10 * time.Minute)}, Status: BacktestWindowStatusFiring, ResultsRetained: 2},
		{BacktestWindow: BacktestWindow{Index: 3, Start: start.Add(15 * time.Minute), End: start.Add(20 * time.Minute)}, Status: BacktestWindowStatusLimitExceeded, ResultsRetained: 1},
		{BacktestWindow: BacktestWindow{Index: 4, Start: start.Add(20 * time.Minute), End: start.Add(25 * time.Minute)}, Status: BacktestWindowStatusCancelled},
	}

	SortBacktestWindowResults(results)
	require.Equal(t, []int{0, 1, 2, 3, 4}, []int{results[0].Index, results[1].Index, results[2].Index, results[3].Index, results[4].Index})
	require.Equal(t, BacktestSummary{
		TotalWindows:         5,
		ClearWindows:         1,
		FiringWindows:        1,
		ErrorWindows:         1,
		LimitExceededWindows: 1,
		CancelledWindows:     1,
		Alerts:               3,
	}, SummarizeBacktestWindowResults(results))
}

func TestBacktestReportJSONHasNoQueryOrCredentialFields(t *testing.T) {
	report := BacktestReport{
		Version:     BacktestReportVersion,
		GeneratedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		Range: BacktestRange{
			Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		},
		Context: BacktestContext{
			RuleFile:            "rule.yaml",
			Database:            "Metrics",
			KustoEndpoints:      map[string]string{"Metrics": "https://example.kusto.windows.net"},
			Region:              "eastus",
			Cloud:               "public",
			Tags:                map[string]string{"environment": "production"},
			Authentication:      BacktestAuthenticationManagedIdentity,
			Concurrency:         4,
			QueryTimeout:        "5m0s",
			MaxResultsPerWindow: 25,
			MaxWindows:          1000,
		},
		Rule: BacktestRuleResult{
			Namespace: "namespace",
			Name:      "rule",
			Outcome:   BacktestRuleOutcomeCompleted,
			Windows: []BacktestWindowResult{{
				BacktestWindow:  BacktestWindow{Index: 0, Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 7, 1, 0, 5, 0, 0, time.UTC)},
				QueryDuration:   "500ms",
				Status:          BacktestWindowStatusFiring,
				ResultLimit:     25,
				ResultsRetained: 1,
				Alerts: []BacktestAlert{{
					Title:         "title",
					Summary:       "original summary",
					Description:   "description",
					Severity:      2,
					Destination:   "destination",
					Source:        "namespace/rule",
					CorrelationID: "namespace/rule://entity",
					CustomFields:  map[string]string{"entity": "value"},
				}},
			}},
		},
	}

	data, err := json.Marshal(report)
	require.NoError(t, err)

	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	keys := collectJSONKeys(document)
	for _, forbidden := range []string{
		"renderedQuery",
		"query",
		"selectedEndpoint",
		"resolvedEndpoint",
		"token",
		"credential",
		"credentials",
		"managedIdentityClientId",
		"msiId",
		"authorization",
	} {
		require.NotContains(t, keys, forbidden)
	}
	require.EqualValues(t, BacktestReportVersion, document["version"])
}

func TestRunBacktest_ClearAndFiringWithExactWindows(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events | take 1", "", ""))
	var windowsMu sync.Mutex
	var windows []BacktestWindow
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, qc *engine.QueryContext, fn engineRowFn) (error, int) {
		windowsMu.Lock()
		windows = append(windows, BacktestWindow{Start: qc.StartTime, End: qc.EndTime})
		windowsMu.Unlock()
		if qc.StartTime.Equal(start.Add(5 * time.Minute)) {
			if err := fn(ctx, "https://selected.invalid", qc, backtestAlertRow("firing")); err != nil {
				return err, 0
			}
			return nil, 1
		}
		return nil, 0
	}}
	opts := backtestAlerterOptions()
	backtestOpts := validBacktestOptions(start, start.Add(12*time.Minute))

	report, err := runBacktest(context.Background(), opts, rulePath, backtestOpts, fakeBacktestFactory(client))
	require.NoError(t, err)
	require.Equal(t, BacktestRuleOutcomeCompleted, report.Rule.Outcome)
	require.Equal(t, BacktestSummary{TotalWindows: 3, ClearWindows: 2, FiringWindows: 1, Alerts: 1}, report.Summary)
	require.Equal(t, []BacktestWindowStatus{BacktestWindowStatusClear, BacktestWindowStatusFiring, BacktestWindowStatusClear}, backtestStatuses(report))
	require.Equal(t, BacktestAlert{
		Title:         "firing",
		Summary:       "original summary",
		Description:   "description",
		Severity:      2,
		Destination:   "destination",
		Source:        "namespace/rule",
		CorrelationID: "namespace/rule://entity",
		CustomFields:  map[string]string{"Custom": "value"},
	}, report.Rule.Windows[1].Alerts[0])
	require.ElementsMatch(t, []BacktestWindow{
		{Start: start, End: start.Add(5 * time.Minute)},
		{Start: start.Add(5 * time.Minute), End: start.Add(10 * time.Minute)},
		{Start: start.Add(10 * time.Minute), End: start.Add(12 * time.Minute)},
	}, windows)
}

func TestRunBacktest_QueryAndConversionErrorsAreIsolated(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", ""))
	queryFailure := errors.New("isolated query failure")
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, qc *engine.QueryContext, fn engineRowFn) (error, int) {
		switch qc.StartTime {
		case start:
			return queryFailure, 0
		case start.Add(5 * time.Minute):
			if err := fn(ctx, "endpoint", qc, backtestInvalidAlertRow()); err != nil {
				return err, 0
			}
		}
		return nil, 0
	}}

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, validBacktestOptions(start, start.Add(15*time.Minute)), fakeBacktestFactory(client))
	require.ErrorIs(t, err, ErrBacktestFailed)
	require.ErrorIs(t, err, queryFailure)
	require.Equal(t, BacktestRuleOutcomePartial, report.Rule.Outcome)
	require.Equal(t, []BacktestWindowStatus{BacktestWindowStatusError, BacktestWindowStatusError, BacktestWindowStatusClear}, backtestStatuses(report))
	var validationErr *engine.NotificationValidationError
	require.ErrorAs(t, err, &validationErr)
	require.NotContains(t, err.Error(), "severity must be specified")
	require.Equal(t, "invalid alert result: severity must be specified", report.Rule.Windows[1].Error)
	require.Equal(t, 3, client.callCount())
}

func TestRunBacktest_QueryErrorsAreSafeInReport(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events | where Secret == 'rendered-query-secret'", "", ""))
	original := errors.New("original query error")
	client := &fakeBacktestClient{queryFn: func(_ context.Context, qc *engine.QueryContext, _ engineRowFn) (error, int) {
		return fmt.Errorf("Authorization: Bearer token-secret; MSI msi-secret; DefaultAzureCredential diagnostics; https://selected.invalid/DB?query=secret-link; query=%s: %w", qc.Query, original), 0
	}}

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, validBacktestOptions(start, start.Add(5*time.Minute)), fakeBacktestFactory(client))
	require.ErrorIs(t, err, original)
	assertBacktestErrorExcludes(t, err, "rendered-query-secret", "token-secret", "msi-secret", "Authorization", "DefaultAzureCredential", "selected.invalid", "secret-link")
	require.Equal(t, "query execution failed", report.Rule.Windows[0].Error)
	assertBacktestReportExcludes(t, report, "rendered-query-secret", "token-secret", "msi-secret", "Authorization", "DefaultAzureCredential", "selected.invalid", "secret-link")
}

func TestRunBacktest_ClientConstructionErrorsAreSafeInReport(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", ""))
	original := errors.New("original auth error")
	factoryErr := fmt.Errorf("DefaultAzureCredential diagnostics token=token-secret msi=msi-secret Authorization header: %w", original)

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, validBacktestOptions(start, start.Add(5*time.Minute)), func(*AlerterOpts, int) (engine.Client, error) {
		return nil, factoryErr
	})
	require.ErrorIs(t, err, original)
	assertBacktestErrorExcludes(t, err, "token-secret", "msi-secret", "Authorization", "DefaultAzureCredential")
	require.Equal(t, "failed to construct query client", report.Rule.Windows[0].Error)
	assertBacktestReportExcludes(t, report, "token-secret", "msi-secret", "Authorization", "DefaultAzureCredential")
}

func TestRunBacktest_ResultLimitExceededRetainsAlerts(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", ""))
	limitErr := &engine.ResultLimitExceededError{Limit: 2, RowsProcessed: 4}
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, qc *engine.QueryContext, fn engineRowFn) (error, int) {
		for _, title := range []string{"first", "second"} {
			if err := fn(ctx, "endpoint", qc, backtestAlertRow(title)); err != nil {
				return err, 0
			}
		}
		return limitErr, 0
	}}
	backtestOpts := validBacktestOptions(start, start.Add(5*time.Minute))
	backtestOpts.MaxResultsPerWindow = 2

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, backtestOpts, fakeBacktestFactory(client))
	var got *engine.ResultLimitExceededError
	require.ErrorAs(t, err, &got)
	require.Same(t, limitErr, got)
	require.ErrorIs(t, err, ErrBacktestFailed)
	require.Equal(t, BacktestRuleOutcomePartial, report.Rule.Outcome)
	require.Equal(t, BacktestSummary{TotalWindows: 1, LimitExceededWindows: 1, Alerts: 2}, report.Summary)
	require.ErrorContains(t, errors.New(report.Rule.Error), "1 limit-exceeded windows")

	window := report.Rule.Windows[0]
	require.Equal(t, BacktestWindowStatusLimitExceeded, window.Status)
	require.True(t, window.ResultLimitExceeded)
	require.Equal(t, 2, window.ResultLimit)
	require.Equal(t, 2, window.ResultsRetained)
	require.Len(t, window.Alerts, 2)
	require.Equal(t, []string{"first", "second"}, []string{window.Alerts[0].Title, window.Alerts[1].Title})
	require.Equal(t, limitErr.Error(), window.Error)
}

func TestRunBacktest_ResultLimitAndInvalidRowPreserveBothErrors(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", ""))
	limitErr := &engine.ResultLimitExceededError{Limit: 3, RowsProcessed: 5}
	var conversionErr error
	callbackCount := 0
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, qc *engine.QueryContext, fn engineRowFn) (error, int) {
		for _, row := range []azquery.Row{backtestAlertRow("first"), backtestInvalidAlertRow(), backtestAlertRow("not-called")} {
			callbackCount++
			if err := fn(ctx, "endpoint", qc, row); err != nil {
				conversionErr = err
				return errors.Join(err, limitErr), 0
			}
		}
		return limitErr, 0
	}}
	backtestOpts := validBacktestOptions(start, start.Add(5*time.Minute))
	backtestOpts.MaxResultsPerWindow = 3

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, backtestOpts, fakeBacktestFactory(client))
	var gotLimit *engine.ResultLimitExceededError
	var gotValidation *engine.NotificationValidationError
	require.ErrorAs(t, err, &gotLimit)
	require.Same(t, limitErr, gotLimit)
	require.ErrorAs(t, err, &gotValidation)
	require.ErrorIs(t, err, conversionErr)
	require.ErrorIs(t, err, ErrBacktestFailed)
	require.Equal(t, 2, callbackCount)
	require.Equal(t, BacktestRuleOutcomePartial, report.Rule.Outcome)
	require.Equal(t, BacktestSummary{TotalWindows: 1, ErrorWindows: 1, Alerts: 1}, report.Summary)

	window := report.Rule.Windows[0]
	require.Equal(t, BacktestWindowStatusError, window.Status)
	require.True(t, window.ResultLimitExceeded)
	require.Equal(t, 3, window.ResultLimit)
	require.Equal(t, 1, window.ResultsRetained)
	require.Equal(t, []string{"first"}, []string{window.Alerts[0].Title})
	require.Equal(t, "invalid alert result: severity must be specified", window.Error)
}

func TestRunBacktest_CriteriaHandlingDoesNotConstructClient(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		criteria    string
		expression  string
		wantOutcome BacktestRuleOutcome
		wantError   bool
	}{
		{name: "mismatch", criteria: "environment: [production]", wantOutcome: BacktestRuleOutcomeSkipped},
		{name: "expression error", expression: "missing == 'value'", wantOutcome: BacktestRuleOutcomeInvalid, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events", tt.criteria, tt.expression))
			factoryCalls := 0
			report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, validBacktestOptions(start, start.Add(5*time.Minute)), func(*AlerterOpts, int) (engine.Client, error) {
				factoryCalls++
				return nil, errors.New("must not construct")
			})
			if tt.wantError {
				require.ErrorIs(t, err, ErrBacktestFailed)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantOutcome, report.Rule.Outcome)
			require.Zero(t, factoryCalls)
		})
	}
}

func TestRunBacktest_InvalidInvocationCannotBeSkippedByCriteria(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		rule       string
		modifyOpts func(*BacktestOptions)
		wantError  string
	}{
		{name: "invalid options", rule: backtestRuleYAML("5m", "Events", "environment: [production]", ""), modifyOpts: func(opts *BacktestOptions) { opts.Concurrency = 0 }, wantError: "concurrency"},
		{name: "invalid interval", rule: backtestRuleYAML("", "Events", "environment: [production]", ""), wantError: "interval"},
		{name: "too many windows", rule: backtestRuleYAML("5m", "Events", "environment: [production]", ""), modifyOpts: func(opts *BacktestOptions) { opts.MaxWindows = 1 }, wantError: "maximum of 1"},
		{name: "management query", rule: backtestRuleYAML("5m", "  .show tables", "environment: [production]", ""), wantError: "management"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backtestOpts := validBacktestOptions(start, start.Add(10*time.Minute))
			if tt.modifyOpts != nil {
				tt.modifyOpts(&backtestOpts)
			}
			report, err := runBacktest(context.Background(), backtestAlerterOptions(), writeBacktestRule(t, tt.rule), backtestOpts, fakeBacktestFactory(&fakeBacktestClient{}))
			require.ErrorContains(t, err, tt.wantError)
			require.Equal(t, BacktestRuleOutcomeInvalid, report.Rule.Outcome)
		})
	}
}

func TestRunBacktest_ValidationBeforeClientConstruction(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		rule       string
		path       func(*testing.T, string) string
		modifyOpts func(*BacktestOptions)
		wantError  string
	}{
		{name: "directory", path: func(t *testing.T, _ string) string { return t.TempDir() }, wantError: "regular file"},
		{name: "no rules", rule: "kind: ConfigMap\nmetadata:\n  name: ignored\n", wantError: "found 0"},
		{name: "multiple rules", rule: backtestRuleYAML("5m", "Events", "", "") + "\n---\n" + backtestRuleYAMLWithName("second", "5m", "Events"), wantError: "found 2"},
		{name: "invalid options", rule: backtestRuleYAML("5m", "Events", "", ""), modifyOpts: func(opts *BacktestOptions) { opts.Concurrency = 0 }, wantError: "concurrency"},
		{name: "zero interval", rule: backtestRuleYAML("", "Events", "", ""), wantError: "interval"},
		{name: "max windows", rule: backtestRuleYAML("5m", "Events", "", ""), modifyOpts: func(opts *BacktestOptions) { opts.MaxWindows = 1 }, wantError: "maximum of 1"},
		{name: "management query", rule: backtestRuleYAML("5m", ".show tables", "", ""), wantError: "management"},
		{name: "whitespace management query", rule: backtestRuleYAML("5m", "   .show tables", "", ""), wantError: "management"},
		{name: "missing database", rule: backtestRuleWithoutRequiredField("database"), wantError: "database"},
		{name: "missing query", rule: backtestRuleWithoutRequiredField("query"), wantError: "query"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := ""
			if tt.path != nil {
				path = tt.path(t, tt.rule)
			} else {
				path = writeBacktestRule(t, tt.rule)
			}
			backtestOpts := validBacktestOptions(start, start.Add(10*time.Minute))
			if tt.modifyOpts != nil {
				tt.modifyOpts(&backtestOpts)
			}
			factoryCalls := 0
			report, err := runBacktest(context.Background(), backtestAlerterOptions(), path, backtestOpts, func(*AlerterOpts, int) (engine.Client, error) {
				factoryCalls++
				return nil, nil
			})
			require.ErrorContains(t, err, tt.wantError)
			require.Equal(t, BacktestRuleOutcomeInvalid, report.Rule.Outcome)
			require.Zero(t, factoryCalls)
		})
	}
}

func TestRunBacktest_UnknownDatabaseErrorRemainsInformative(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", ""))
	unknownDB := &engine.UnknownDBError{DB: "DB", AvailableDatabases: []string{"db", "Metrics"}, CaseInsensitiveMatch: "db"}
	client := &fakeBacktestClient{queryFn: func(context.Context, *engine.QueryContext, engineRowFn) (error, int) {
		return unknownDB, 0
	}}

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, validBacktestOptions(start, start.Add(5*time.Minute)), fakeBacktestFactory(client))
	var got *engine.UnknownDBError
	require.ErrorAs(t, err, &got)
	require.Same(t, unknownDB, got)
	require.Contains(t, report.Rule.Windows[0].Error, `did you mean "db"`)
	require.Contains(t, report.Rule.Windows[0].Error, "configured databases")
}

func TestRunBacktest_ConcurrencyBoundAndDeterministicSorting(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("1m", "Events", "", ""))
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	var active atomic.Int32
	var maximum atomic.Int32
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, qc *engine.QueryContext, fn engineRowFn) (error, int) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		// Reverse completion order after the bound is observed.
		time.Sleep(time.Duration(4-qc.StartTime.Sub(start)/time.Minute) * time.Millisecond)
		return nil, 0
	}}
	backtestOpts := validBacktestOptions(start, start.Add(4*time.Minute))
	backtestOpts.Concurrency = 2
	type response struct {
		report *BacktestReport
		err    error
	}
	done := make(chan response, 1)
	go func() {
		report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, backtestOpts, fakeBacktestFactory(client))
		done <- response{report: report, err: err}
	}()

	<-started
	<-started
	require.EqualValues(t, 2, active.Load())
	close(release)
	result := <-done
	require.NoError(t, result.err)
	require.EqualValues(t, 2, maximum.Load())
	require.Equal(t, []int{0, 1, 2, 3}, backtestIndexes(result.report))
}

func TestRunBacktest_CancellationStopsSchedulingAndCancelsInflight(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("1m", "Events", "", ""))
	started := make(chan struct{}, 2)
	inflightCancelled := make(chan struct{}, 2)
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, _ *engine.QueryContext, _ engineRowFn) (error, int) {
		started <- struct{}{}
		<-ctx.Done()
		inflightCancelled <- struct{}{}
		return ctx.Err(), 0
	}}
	ctx, cancel := context.WithCancel(context.Background())
	backtestOpts := validBacktestOptions(start, start.Add(6*time.Minute))
	backtestOpts.Concurrency = 2
	done := make(chan struct {
		report *BacktestReport
		err    error
	}, 1)
	go func() {
		report, err := runBacktest(ctx, backtestAlerterOptions(), rulePath, backtestOpts, fakeBacktestFactory(client))
		done <- struct {
			report *BacktestReport
			err    error
		}{report, err}
	}()
	<-started
	<-started
	cancel()
	result := <-done

	require.ErrorIs(t, result.err, context.Canceled)
	require.Equal(t, BacktestRuleOutcomePartial, result.report.Rule.Outcome)
	require.Equal(t, 6, result.report.Summary.CancelledWindows)
	require.Equal(t, 6, result.report.Summary.TotalWindows)
	require.Equal(t, 2, client.callCount())
	<-inflightCancelled
	<-inflightCancelled
	for _, window := range result.report.Rule.Windows {
		require.Equal(t, BacktestWindowStatusCancelled, window.Status)
		require.ErrorContains(t, errors.New(window.Error), context.Canceled.Error())
	}
}

func TestRunBacktest_QueryTimeout(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", ""))
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, _ *engine.QueryContext, _ engineRowFn) (error, int) {
		<-ctx.Done()
		return ctx.Err(), 0
	}}
	backtestOpts := validBacktestOptions(start, start.Add(5*time.Minute))
	backtestOpts.QueryTimeout = time.Millisecond

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, backtestOpts, fakeBacktestFactory(client))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, BacktestWindowStatusError, report.Rule.Windows[0].Status)
}

func TestRunBacktest_QueryReturnsNilAfterDeadline(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, _ *engine.QueryContext, _ engineRowFn) (error, int) {
		<-ctx.Done()
		return nil, 0
	}}
	backtestOpts := validBacktestOptions(start, start.Add(5*time.Minute))
	backtestOpts.QueryTimeout = time.Millisecond

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", "")), backtestOpts, fakeBacktestFactory(client))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, BacktestRuleOutcomePartial, report.Rule.Outcome)
	require.Equal(t, BacktestWindowStatusError, report.Rule.Windows[0].Status)
	require.Equal(t, context.DeadlineExceeded.Error(), report.Rule.Windows[0].Error)
}

func TestRunBacktest_ParentCancelledImmediatelyBeforeSuccessfulReturn(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeBacktestClient{queryFn: func(context.Context, *engine.QueryContext, engineRowFn) (error, int) {
		cancel()
		return nil, 0
	}}

	report, err := runBacktest(ctx, backtestAlerterOptions(), writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", "")), validBacktestOptions(start, start.Add(5*time.Minute)), fakeBacktestFactory(client))
	require.NoError(t, err)
	require.Equal(t, BacktestRuleOutcomeCompleted, report.Rule.Outcome)
	require.Equal(t, BacktestWindowStatusClear, report.Rule.Windows[0].Status)
}

func TestExecuteBacktestWindow_ContextAndErrorPrecedence(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rule := &rules.Rule{Namespace: "namespace", Name: "rule", Database: "DB", Interval: 5 * time.Minute, Query: "Events"}
	newResult := func() BacktestWindowResult {
		return BacktestWindowResult{
			BacktestWindow: BacktestWindow{Index: 0, Start: start, End: start.Add(5 * time.Minute)},
			ResultLimit:    25,
			Alerts:         []BacktestAlert{},
		}
	}

	t.Run("non-context error wins over simultaneous parent cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		queryErr := errors.New("query failed after cancellation")
		client := &fakeBacktestClient{queryFn: func(context.Context, *engine.QueryContext, engineRowFn) (error, int) {
			cancel()
			return queryErr, 0
		}}

		result, err := executeBacktestWindow(ctx, client, rule, "eastus", validBacktestOptions(start, start.Add(5*time.Minute)), newResult())
		require.ErrorIs(t, err, queryErr)
		require.Equal(t, BacktestWindowStatusError, result.Status)
	})

	t.Run("per-query deadline is an error", func(t *testing.T) {
		client := &fakeBacktestClient{queryFn: func(ctx context.Context, _ *engine.QueryContext, _ engineRowFn) (error, int) {
			<-ctx.Done()
			return ctx.Err(), 0
		}}
		opts := validBacktestOptions(start, start.Add(5*time.Minute))
		opts.QueryTimeout = time.Millisecond

		result, err := executeBacktestWindow(context.Background(), client, rule, "eastus", opts, newResult())
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, BacktestWindowStatusError, result.Status)
	})
}

func TestExecuteBacktestWindow_PreCancelledContextDoesNotQuery(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeBacktestClient{}
	rule := &rules.Rule{Namespace: "namespace", Name: "rule", Database: "DB", Interval: 5 * time.Minute, Query: "Events"}
	result := BacktestWindowResult{
		BacktestWindow: BacktestWindow{Index: 0, Start: start, End: start.Add(5 * time.Minute)},
		ResultLimit:    25,
		Alerts:         []BacktestAlert{},
	}

	result, err := executeBacktestWindow(ctx, client, rule, "eastus", validBacktestOptions(start, start.Add(5*time.Minute)), result)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, BacktestWindowStatusCancelled, result.Status)
	require.Equal(t, context.Canceled.Error(), result.Error)
	require.Zero(t, client.callCount())
}

func TestRunBacktest_OmittedDestinationUsesRecipient(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	rulePath := writeBacktestRule(t, `apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: rule
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: Events
  autoMitigateAfter: 1h
`)
	client := &fakeBacktestClient{queryFn: func(ctx context.Context, qc *engine.QueryContext, fn engineRowFn) (error, int) {
		if err := fn(ctx, "endpoint", qc, backtestAlertRowWithRecipient("fallback-destination")); err != nil {
			return err, 0
		}
		return nil, 1
	}}

	report, err := runBacktest(context.Background(), backtestAlerterOptions(), rulePath, validBacktestOptions(start, start.Add(5*time.Minute)), fakeBacktestFactory(client))
	require.NoError(t, err)
	require.Equal(t, "fallback-destination", report.Rule.Windows[0].Alerts[0].Destination)
}

func TestRunBacktest_ReportContextIsCopiedForAllOutcomes(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		rule      string
		ctx       func() context.Context
		client    *fakeBacktestClient
		want      BacktestRuleOutcome
		wantErr   bool
		authMode  BacktestAuthenticationMode
		configure func(*AlerterOpts)
	}{
		{name: "successful", rule: backtestRuleYAML("5m", "Events", "", ""), ctx: context.Background, client: &fakeBacktestClient{}, want: BacktestRuleOutcomeCompleted, authMode: BacktestAuthenticationDefaultCredential},
		{name: "failed", rule: backtestRuleYAML("5m", "Events", "", ""), ctx: context.Background, client: &fakeBacktestClient{queryFn: func(context.Context, *engine.QueryContext, engineRowFn) (error, int) {
			return errors.New("query failed"), 0
		}}, want: BacktestRuleOutcomePartial, wantErr: true, authMode: BacktestAuthenticationToken, configure: func(opts *AlerterOpts) { opts.KustoToken = "secret-token" }},
		{name: "skipped", rule: backtestRuleYAML("5m", "Events", "environment: [other]", ""), ctx: context.Background, client: &fakeBacktestClient{}, want: BacktestRuleOutcomeSkipped, authMode: BacktestAuthenticationManagedIdentity, configure: func(opts *AlerterOpts) { opts.MSIID = "secret-msi-id" }},
		{name: "cancelled", rule: backtestRuleYAML("5m", "Events", "", ""), ctx: cancelledContext, client: &fakeBacktestClient{}, want: BacktestRuleOutcomePartial, wantErr: true, authMode: BacktestAuthenticationManagedIdentity, configure: func(opts *AlerterOpts) { opts.MSIID = "secret-msi-id"; opts.KustoToken = "secret-token" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rulePath := writeBacktestRule(t, tt.rule)
			opts := backtestAlerterOptions()
			if tt.configure != nil {
				tt.configure(opts)
			}
			report, err := runBacktest(tt.ctx(), opts, rulePath, validBacktestOptions(start, start.Add(5*time.Minute)), fakeBacktestFactory(tt.client))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.want, report.Rule.Outcome)
			require.Equal(t, "DB", report.Context.Database)
			require.Equal(t, "eastus", report.Context.Region)
			require.Equal(t, "public", report.Context.Cloud)
			require.Equal(t, tt.authMode, report.Context.Authentication)
			require.Equal(t, map[string]string{"DB": "https://configured.invalid", "Other": "https://other.invalid"}, report.Context.KustoEndpoints)
			require.Equal(t, map[string]string{"environment": "test", "ring": "stable"}, report.Context.Tags)

			opts.KustoEndpoints["DB"] = "mutated"
			opts.Tags["environment"] = "mutated"
			require.Equal(t, "https://configured.invalid", report.Context.KustoEndpoints["DB"])
			require.Equal(t, "test", report.Context.Tags["environment"])

			data, marshalErr := json.Marshal(report)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(data), "secret-token")
			require.NotContains(t, string(data), "secret-msi-id")
			require.NotContains(t, string(data), "selected.invalid")
			require.NotContains(t, string(data), "Events")
		})
	}
}

func TestRunBacktest_SanitizesReportEndpointsWithoutChangingClientOptions(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	endpoints := map[string]string{
		"UserInfo": "https://client-id:client-secret@cluster.example.test/path-secret",
		"Query":    "https://cluster.example.test/path-token?token=token-secret&sig=signature-secret#fragment-secret",
		"Port":     "https://cluster.example.test:8443/port-path-secret",
		"Opaque":   "cluster-alias token-secret",
	}
	opts := backtestAlerterOptions()
	opts.KustoEndpoints = endpoints
	var received map[string]string

	report, err := runBacktest(context.Background(), opts, writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", "")), validBacktestOptions(start, start.Add(5*time.Minute)), func(got *AlerterOpts, _ int) (engine.Client, error) {
		received = cloneStringMap(got.KustoEndpoints)
		return &fakeBacktestClient{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, endpoints, received)
	require.Equal(t, map[string]string{
		"UserInfo": "https://cluster.example.test",
		"Query":    "https://cluster.example.test",
		"Port":     "https://cluster.example.test:8443",
		"Opaque":   "(non-URL endpoint)",
	}, report.Context.KustoEndpoints)
	assertBacktestReportExcludes(t, report, "client-id", "client-secret", "path-secret", "path-token", "port-path-secret", "token-secret", "signature-secret", "fragment-secret", "selected.invalid")

	opts.KustoEndpoints["UserInfo"] = "mutated"
	require.Equal(t, "https://cluster.example.test", report.Context.KustoEndpoints["UserInfo"])
}

func TestSanitizeBacktestEndpointRejectsMalformedValues(t *testing.T) {
	for _, endpoint := range []string{"", "cluster.example.test/path-token", "://cluster.example.test/path-token", "https:///path-token", "https://"} {
		require.Equal(t, "(non-URL endpoint)", SanitizeBacktestEndpoint(endpoint), endpoint)
	}
}

func TestRunBacktest_EndpointCredentialsStayOutOfReportAndReturnedError(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	opts := backtestAlerterOptions()
	opts.KustoEndpoints = map[string]string{
		"DB": "https://user:userinfo-secret@cluster.example.test?token=query-secret&sig=sig-secret#fragment-secret",
	}

	report, err := runBacktest(context.Background(), opts, writeBacktestRule(t, backtestRuleYAML("5m", "Events", "", "")), validBacktestOptions(start, start.Add(5*time.Minute)), func(got *AlerterOpts, _ int) (engine.Client, error) {
		require.Equal(t, opts.KustoEndpoints, got.KustoEndpoints)
		return nil, errors.New("client construction failed")
	})
	require.ErrorIs(t, err, ErrBacktestFailed)
	require.Equal(t, "https://cluster.example.test", report.Context.KustoEndpoints["DB"])
	assertBacktestReportExcludes(t, report, "userinfo-secret", "query-secret", "sig-secret", "fragment-secret")
	assertBacktestErrorExcludes(t, err, "userinfo-secret", "query-secret", "sig-secret", "fragment-secret")
}

type engineRowFn func(context.Context, string, *engine.QueryContext, azquery.Row) error

type fakeBacktestClient struct {
	mu      sync.Mutex
	calls   int
	queryFn func(context.Context, *engine.QueryContext, engineRowFn) (error, int)
}

func (f *fakeBacktestClient) Endpoint(string) string { return "https://selected.invalid" }

func (f *fakeBacktestClient) Query(ctx context.Context, qc *engine.QueryContext, fn func(context.Context, string, *engine.QueryContext, azquery.Row) error) (error, int) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.queryFn == nil {
		return nil, 0
	}
	return f.queryFn(ctx, qc, fn)
}

func (f *fakeBacktestClient) AvailableDatabases() []string { return []string{"DB"} }

func (f *fakeBacktestClient) FindCaseInsensitiveMatch(string) string { return "" }

func (f *fakeBacktestClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func fakeBacktestFactory(client engine.Client) backtestClientFactory {
	return func(*AlerterOpts, int) (engine.Client, error) { return client, nil }
}

func writeBacktestRule(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rule.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func backtestRuleYAML(interval, query, criteria, expression string) string {
	return backtestRuleYAMLWithNameAndCriteria("rule", interval, query, criteria, expression)
}

func backtestRuleYAMLWithName(name, interval, query string) string {
	return backtestRuleYAMLWithNameAndCriteria(name, interval, query, "", "")
}

func backtestRuleYAMLWithNameAndCriteria(name, interval, query, criteria, expression string) string {
	intervalLine := ""
	if interval != "" {
		intervalLine = fmt.Sprintf("  interval: %s\n", interval)
	}
	criteriaLine := ""
	if criteria != "" {
		criteriaLine = fmt.Sprintf("  criteria:\n    %s\n", criteria)
	}
	expressionLine := ""
	if expression != "" {
		expressionLine = fmt.Sprintf("  criteriaExpression: %q\n", expression)
	}
	return fmt.Sprintf(`apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: %s
  namespace: namespace
spec:
  database: DB
%s  query: |-
    %s
  autoMitigateAfter: 1h
  destination: destination
%s%s`, name, intervalLine, query, criteriaLine, expressionLine)
}

func backtestRuleWithoutRequiredField(field string) string {
	database := "  database: DB\n"
	query := "  query: Events\n"
	if field == "database" {
		database = ""
	}
	if field == "query" {
		query = ""
	}
	return "apiVersion: adx-mon.azure.com/v1\nkind: AlertRule\nmetadata:\n  name: rule\n  namespace: namespace\nspec:\n" + database + "  interval: 5m\n" + query
}

func backtestAlertRow(title string) azquery.Row {
	base := azquery.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	table := azquery.NewBaseTable(base, 0, "", "QueryResult", "QueryResult", []azquery.Column{
		azquery.NewColumn(0, "Title", aztypes.String),
		azquery.NewColumn(1, "Summary", aztypes.String),
		azquery.NewColumn(2, "Description", aztypes.String),
		azquery.NewColumn(3, "Severity", aztypes.Long),
		azquery.NewColumn(4, "CorrelationId", aztypes.String),
		azquery.NewColumn(5, "Custom", aztypes.String),
	})
	return azquery.NewRow(table, 0, azvalue.Values{
		azvalue.NewString(title),
		azvalue.NewString("original summary"),
		azvalue.NewString("description"),
		azvalue.NewLong(2),
		azvalue.NewString("entity"),
		azvalue.NewString("value"),
	})
}

func backtestInvalidAlertRow() azquery.Row {
	base := azquery.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	table := azquery.NewBaseTable(base, 0, "", "QueryResult", "QueryResult", []azquery.Column{
		azquery.NewColumn(0, "Title", aztypes.String),
	})
	return azquery.NewRow(table, 0, azvalue.Values{azvalue.NewString("invalid")})
}

func backtestAlertRowWithRecipient(recipient string) azquery.Row {
	base := azquery.NewBaseDataset(context.Background(), azerrors.OpQuery, "QueryResult")
	table := azquery.NewBaseTable(base, 0, "", "QueryResult", "QueryResult", []azquery.Column{
		azquery.NewColumn(0, "Title", aztypes.String),
		azquery.NewColumn(1, "Severity", aztypes.Long),
		azquery.NewColumn(2, "Recipient", aztypes.String),
	})
	return azquery.NewRow(table, 0, azvalue.Values{
		azvalue.NewString("title"),
		azvalue.NewLong(2),
		azvalue.NewString(recipient),
	})
}

func backtestAlerterOptions() *AlerterOpts {
	return &AlerterOpts{
		KustoEndpoints: map[string]string{"DB": "https://configured.invalid", "Other": "https://other.invalid"},
		Region:         "eastus",
		Cloud:          "public",
		Tags:           map[string]string{"environment": "test", "ring": "stable"},
	}
}

func backtestStatuses(report *BacktestReport) []BacktestWindowStatus {
	statuses := make([]BacktestWindowStatus, len(report.Rule.Windows))
	for i, window := range report.Rule.Windows {
		statuses[i] = window.Status
	}
	return statuses
}

func backtestIndexes(report *BacktestReport) []int {
	indexes := make([]int, len(report.Rule.Windows))
	for i, window := range report.Rule.Windows {
		indexes[i] = window.Index
	}
	return indexes
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func assertBacktestReportExcludes(t *testing.T, report *BacktestReport, values ...string) {
	t.Helper()
	data, err := json.Marshal(report)
	require.NoError(t, err)
	for _, value := range values {
		require.NotContains(t, string(data), value)
	}
}

func assertBacktestErrorExcludes(t *testing.T, err error, values ...string) {
	t.Helper()
	for _, value := range values {
		require.NotContains(t, err.Error(), value)
	}
}

func validBacktestOptions(start, end time.Time) BacktestOptions {
	return BacktestOptions{
		Start:               start,
		End:                 end,
		Concurrency:         4,
		MaxResultsPerWindow: 25,
		QueryTimeout:        5 * time.Minute,
		MaxWindows:          1000,
	}
}

func collectJSONKeys(value any) map[string]struct{} {
	keys := make(map[string]struct{})
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				keys[key] = struct{}{}
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	visit(value)
	return keys
}
