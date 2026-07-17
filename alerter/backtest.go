package alerter

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/alerter/rules"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
)

const BacktestReportVersion = 1

// ErrBacktestFailed identifies invalid, partial, and cancelled backtests.
var ErrBacktestFailed = errors.New("backtest failed")

type backtestError struct {
	message string
	causes  []error
}

func (e *backtestError) Error() string   { return e.message }
func (e *backtestError) Unwrap() []error { return e.causes }

func newBacktestError(message string, causes ...error) error {
	nonNilCauses := make([]error, 0, len(causes))
	for _, cause := range causes {
		if cause != nil {
			nonNilCauses = append(nonNilCauses, cause)
		}
	}
	return &backtestError{message: message, causes: nonNilCauses}
}

type backtestClientFactory func(opts *AlerterOpts, maxResults int) (engine.Client, error)

type BacktestRuleOutcome string

const (
	BacktestRuleOutcomeCompleted BacktestRuleOutcome = "completed"
	BacktestRuleOutcomeSkipped   BacktestRuleOutcome = "skipped"
	BacktestRuleOutcomeInvalid   BacktestRuleOutcome = "invalid"
	BacktestRuleOutcomePartial   BacktestRuleOutcome = "partial"
)

type BacktestWindowStatus string

const (
	BacktestWindowStatusClear         BacktestWindowStatus = "clear"
	BacktestWindowStatusFiring        BacktestWindowStatus = "firing"
	BacktestWindowStatusError         BacktestWindowStatus = "error"
	BacktestWindowStatusLimitExceeded BacktestWindowStatus = "limit-exceeded"
	BacktestWindowStatusCancelled     BacktestWindowStatus = "cancelled"
)

type BacktestAuthenticationMode string

const (
	BacktestAuthenticationToken             BacktestAuthenticationMode = "token"
	BacktestAuthenticationManagedIdentity   BacktestAuthenticationMode = "managed-identity"
	BacktestAuthenticationDefaultCredential BacktestAuthenticationMode = "default-credential"
)

// BacktestOptions controls the range and safety bounds of a backtest.
type BacktestOptions struct {
	Start               time.Time
	End                 time.Time
	Concurrency         int
	MaxResultsPerWindow int
	QueryTimeout        time.Duration
	MaxWindows          int
}

type BacktestReport struct {
	Version     int                `json:"version"`
	GeneratedAt time.Time          `json:"generatedAt"`
	Range       BacktestRange      `json:"range"`
	Context     BacktestContext    `json:"context"`
	Summary     BacktestSummary    `json:"summary"`
	Rule        BacktestRuleResult `json:"rule"`
}

type BacktestRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// BacktestContext contains reproducible execution settings but no credential values.
type BacktestContext struct {
	RuleFile            string                     `json:"ruleFile"`
	Database            string                     `json:"database"`
	KustoEndpoints      map[string]string          `json:"kustoEndpoints"`
	Region              string                     `json:"region"`
	Cloud               string                     `json:"cloud"`
	Tags                map[string]string          `json:"tags"`
	Authentication      BacktestAuthenticationMode `json:"authentication"`
	Concurrency         int                        `json:"concurrency"`
	QueryTimeout        string                     `json:"queryTimeout"`
	MaxResultsPerWindow int                        `json:"maxResultsPerWindow"`
	MaxWindows          int                        `json:"maxWindows"`
}

type BacktestSummary struct {
	TotalWindows         int `json:"totalWindows"`
	ClearWindows         int `json:"clearWindows"`
	FiringWindows        int `json:"firingWindows"`
	ErrorWindows         int `json:"errorWindows"`
	LimitExceededWindows int `json:"limitExceededWindows"`
	CancelledWindows     int `json:"cancelledWindows"`
	Alerts               int `json:"alerts"`
}

type BacktestRuleResult struct {
	Namespace string                 `json:"namespace"`
	Name      string                 `json:"name"`
	Outcome   BacktestRuleOutcome    `json:"outcome"`
	Windows   []BacktestWindowResult `json:"windows"`
	Error     string                 `json:"error,omitempty"`
}

type BacktestWindow struct {
	Index int       `json:"index"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type BacktestWindowResult struct {
	BacktestWindow
	QueryDuration       string               `json:"queryDuration"`
	Status              BacktestWindowStatus `json:"status"`
	ResultLimit         int                  `json:"resultLimit"`
	ResultsRetained     int                  `json:"resultsRetained"`
	ResultLimitExceeded bool                 `json:"resultLimitExceeded"`
	Alerts              []BacktestAlert      `json:"alerts"`
	Error               string               `json:"error,omitempty"`
}

// BacktestAlert is the query-safe alert representation stored in reports.
type BacktestAlert struct {
	Title         string            `json:"title"`
	Summary       string            `json:"summary"`
	Description   string            `json:"description"`
	Severity      int64             `json:"severity"`
	Destination   string            `json:"destination"`
	Source        string            `json:"source"`
	CorrelationID string            `json:"correlationId"`
	CustomFields  map[string]string `json:"customFields"`
}

func ValidateBacktestOptions(opts BacktestOptions) error {
	if !opts.Start.Before(opts.End) {
		return fmt.Errorf("backtest start must be before end")
	}
	if opts.Concurrency <= 0 {
		return fmt.Errorf("backtest concurrency must be greater than zero")
	}
	if opts.MaxResultsPerWindow <= 0 {
		return fmt.Errorf("backtest max results per window must be greater than zero")
	}
	if opts.QueryTimeout <= 0 {
		return fmt.Errorf("backtest query timeout must be greater than zero")
	}
	if opts.MaxWindows <= 0 {
		return fmt.Errorf("backtest max windows must be greater than zero")
	}
	return nil
}

// GenerateBacktestWindows divides [Start, End) into interval-sized UTC windows.
func GenerateBacktestWindows(opts BacktestOptions, interval time.Duration) ([]BacktestWindow, error) {
	if err := ValidateBacktestOptions(opts); err != nil {
		return nil, err
	}
	if interval <= 0 {
		return nil, fmt.Errorf("backtest interval must be greater than zero")
	}

	start := opts.Start.UTC()
	end := opts.End.UTC()
	windows := make([]BacktestWindow, 0)
	for windowStart := start; windowStart.Before(end); {
		if len(windows) == opts.MaxWindows {
			return nil, fmt.Errorf("backtest range exceeds maximum of %d windows", opts.MaxWindows)
		}

		windowEnd := windowStart.Add(interval)
		if !windowEnd.After(windowStart) {
			return nil, fmt.Errorf("backtest interval does not advance window start")
		}
		if windowEnd.After(end) {
			windowEnd = end
		}

		windows = append(windows, BacktestWindow{
			Index: len(windows),
			Start: windowStart,
			End:   windowEnd,
		})
		windowStart = windowEnd
	}
	return windows, nil
}

func SortBacktestWindowResults(results []BacktestWindowResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if !results[i].Start.Equal(results[j].Start) {
			return results[i].Start.Before(results[j].Start)
		}
		if !results[i].End.Equal(results[j].End) {
			return results[i].End.Before(results[j].End)
		}
		return results[i].Index < results[j].Index
	})
}

func SummarizeBacktestWindowResults(results []BacktestWindowResult) BacktestSummary {
	summary := BacktestSummary{TotalWindows: len(results)}
	for _, result := range results {
		summary.Alerts += result.ResultsRetained
		switch result.Status {
		case BacktestWindowStatusClear:
			summary.ClearWindows++
		case BacktestWindowStatusFiring:
			summary.FiringWindows++
		case BacktestWindowStatusError:
			summary.ErrorWindows++
		case BacktestWindowStatusLimitExceeded:
			summary.LimitExceededWindows++
		case BacktestWindowStatusCancelled:
			summary.CancelledWindows++
		}
	}
	return summary
}

// Backtest evaluates one local alert rule over an explicit historical range.
func Backtest(ctx context.Context, opts *AlerterOpts, rulePath string, backtestOpts BacktestOptions) (*BacktestReport, error) {
	return runBacktest(ctx, opts, rulePath, backtestOpts, newKustoClient)
}

func runBacktest(ctx context.Context, opts *AlerterOpts, rulePath string, backtestOpts BacktestOptions, newClient backtestClientFactory) (*BacktestReport, error) {
	report := newBacktestReport(opts, rulePath, backtestOpts)
	invalid := func(err error, reportError string) (*BacktestReport, error) {
		report.Rule.Outcome = BacktestRuleOutcomeInvalid
		report.Rule.Error = safeBacktestReportError(err, reportError)
		return report, newBacktestError(ErrBacktestFailed.Error()+": "+report.Rule.Error, ErrBacktestFailed, err)
	}

	if opts == nil {
		err := fmt.Errorf("alerter options must not be nil")
		return invalid(err, err.Error())
	}

	info, err := os.Stat(rulePath)
	if err != nil {
		return invalid(fmt.Errorf("failed to inspect backtest rule file %q: %w", rulePath, err), "failed to inspect backtest rule file")
	}
	if !info.Mode().IsRegular() {
		err := fmt.Errorf("backtest rule path %q must be a regular file", rulePath)
		return invalid(err, err.Error())
	}

	ruleStore, err := rules.FromPath(rulePath, opts.Region)
	if err != nil {
		return invalid(err, "failed to load backtest rule file")
	}
	if len(ruleStore.Rules()) != 1 {
		err := fmt.Errorf("backtest rule file must contain exactly one AlertRule after ignored documents; found %d", len(ruleStore.Rules()))
		return invalid(err, err.Error())
	}

	rule := ruleStore.Rules()[0]
	report.Context.Database = rule.Database
	report.Rule.Namespace = rule.Namespace
	report.Rule.Name = rule.Name
	if strings.TrimSpace(rule.Database) == "" {
		err := fmt.Errorf("backtest rule database must not be empty")
		return invalid(err, err.Error())
	}
	if strings.TrimSpace(rule.Query) == "" {
		err := fmt.Errorf("backtest rule query must not be empty")
		return invalid(err, err.Error())
	}

	if err := ValidateBacktestOptions(backtestOpts); err != nil {
		return invalid(err, err.Error())
	}
	if rule.Interval <= 0 {
		err := fmt.Errorf("backtest interval must be greater than zero")
		return invalid(err, err.Error())
	}
	if isBacktestManagementQuery(rule.Query) {
		err := fmt.Errorf("backtest does not allow management queries")
		return invalid(err, err.Error())
	}

	windows, err := GenerateBacktestWindows(backtestOpts, rule.Interval)
	if err != nil {
		return invalid(err, err.Error())
	}

	matches, err := rule.Matches(opts.Tags)
	if err != nil {
		return invalid(fmt.Errorf("failed to evaluate criteria for rule %s/%s: %w", rule.Namespace, rule.Name, err), "failed to evaluate rule criteria")
	}
	if !matches {
		report.Rule.Outcome = BacktestRuleOutcomeSkipped
		return report, nil
	}
	report.Rule.Windows = makeBacktestWindowResults(windows, backtestOpts.MaxResultsPerWindow)

	if err := ctx.Err(); err != nil {
		causes := markUnexecutedBacktestWindows(report.Rule.Windows, err)
		return completeBacktestReport(report, causes)
	}

	client, err := newClient(opts, backtestOpts.MaxResultsPerWindow)
	if err != nil {
		causes := make([]error, len(report.Rule.Windows))
		for i := range report.Rule.Windows {
			report.Rule.Windows[i].Status = BacktestWindowStatusError
			report.Rule.Windows[i].Error = safeBacktestReportError(err, "failed to construct query client")
			causes[i] = err
		}
		return completeBacktestReport(report, causes)
	}
	if client == nil {
		err := fmt.Errorf("backtest client factory returned a nil client")
		causes := make([]error, len(report.Rule.Windows))
		for i := range report.Rule.Windows {
			report.Rule.Windows[i].Status = BacktestWindowStatusError
			report.Rule.Windows[i].Error = safeBacktestReportError(err, "failed to construct query client")
			causes[i] = err
		}
		return completeBacktestReport(report, causes)
	}

	causes := executeBacktestWindows(ctx, client, rule, opts.Region, backtestOpts, report.Rule.Windows)
	return completeBacktestReport(report, causes)
}

func newBacktestReport(opts *AlerterOpts, rulePath string, backtestOpts BacktestOptions) *BacktestReport {
	report := &BacktestReport{
		Version:     BacktestReportVersion,
		GeneratedAt: time.Now().UTC(),
		Range: BacktestRange{
			Start: backtestOpts.Start.UTC(),
			End:   backtestOpts.End.UTC(),
		},
		Context: BacktestContext{
			RuleFile:            rulePath,
			KustoEndpoints:      map[string]string{},
			Tags:                map[string]string{},
			Authentication:      BacktestAuthenticationDefaultCredential,
			Concurrency:         backtestOpts.Concurrency,
			QueryTimeout:        backtestOpts.QueryTimeout.String(),
			MaxResultsPerWindow: backtestOpts.MaxResultsPerWindow,
			MaxWindows:          backtestOpts.MaxWindows,
		},
		Rule: BacktestRuleResult{
			Outcome: BacktestRuleOutcomeInvalid,
			Windows: []BacktestWindowResult{},
		},
	}
	if opts == nil {
		return report
	}

	report.Context.KustoEndpoints = sanitizeBacktestEndpoints(opts.KustoEndpoints)
	report.Context.Region = opts.Region
	report.Context.Cloud = opts.Cloud
	report.Context.Tags = cloneStringMap(opts.Tags)
	report.Context.Authentication = backtestAuthenticationMode(opts)
	return report
}

// SanitizeBacktestEndpoint returns only the URL origin for safe reporting.
func SanitizeBacktestEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" {
		return "(non-URL endpoint)"
	}
	return parsed.Scheme + "://" + parsed.Host
}

func sanitizeBacktestEndpoints(endpoints map[string]string) map[string]string {
	sanitized := make(map[string]string, len(endpoints))
	for name, endpoint := range endpoints {
		sanitized[name] = SanitizeBacktestEndpoint(endpoint)
	}
	return sanitized
}

func backtestAuthenticationMode(opts *AlerterOpts) BacktestAuthenticationMode {
	if opts.MSIID != "" {
		return BacktestAuthenticationManagedIdentity
	}
	if opts.KustoToken != "" {
		return BacktestAuthenticationToken
	}
	return BacktestAuthenticationDefaultCredential
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func isBacktestManagementQuery(query string) bool {
	return strings.HasPrefix(strings.TrimLeftFunc(query, unicode.IsSpace), ".")
}

func makeBacktestWindowResults(windows []BacktestWindow, resultLimit int) []BacktestWindowResult {
	results := make([]BacktestWindowResult, len(windows))
	for i, window := range windows {
		results[i] = BacktestWindowResult{
			BacktestWindow: window,
			ResultLimit:    resultLimit,
			Alerts:         []BacktestAlert{},
		}
	}
	return results
}

func executeBacktestWindows(ctx context.Context, client engine.Client, rule *rules.Rule, region string, opts BacktestOptions, results []BacktestWindowResult) []error {
	causes := make([]error, len(results))
	next := 0
	var nextMu sync.Mutex
	var wg sync.WaitGroup

	workerCount := min(opts.Concurrency, len(results))
	wg.Add(workerCount)
	for range workerCount {
		go func() {
			defer wg.Done()
			for {
				nextMu.Lock()
				if ctx.Err() != nil || next == len(results) {
					nextMu.Unlock()
					return
				}
				index := next
				next++
				nextMu.Unlock()

				results[index], causes[index] = executeBacktestWindow(ctx, client, rule, region, opts, results[index])
			}
		}()
	}
	wg.Wait()

	if next < len(results) {
		cancelErr := ctx.Err()
		if cancelErr == nil {
			cancelErr = fmt.Errorf("backtest window was not executed")
		}
		for i := next; i < len(results); i++ {
			results[i].Status = BacktestWindowStatusCancelled
			results[i].Error = safeBacktestReportError(cancelErr, "query cancelled")
			causes[i] = cancelErr
		}
	}
	return causes
}

func executeBacktestWindow(ctx context.Context, client engine.Client, rule *rules.Rule, region string, opts BacktestOptions, result BacktestWindowResult) (BacktestWindowResult, error) {
	queryContext, err := engine.NewQueryContextForWindow(rule, result.Start, result.End, region)
	if err != nil {
		result.Status = BacktestWindowStatusError
		result.Error = safeBacktestReportError(err, "failed to construct query")
		return result, err
	}

	queryCtx, cancel := context.WithTimeout(ctx, opts.QueryTimeout)
	if err := queryCtx.Err(); err != nil {
		cancel()
		if ctx.Err() != nil {
			result.Status = BacktestWindowStatusCancelled
			result.Error = safeBacktestReportError(err, "query cancelled")
		} else {
			result.Status = BacktestWindowStatusError
			result.Error = safeBacktestReportError(err, "query execution failed")
		}
		return result, err
	}
	started := time.Now()
	queryErr, _ := client.Query(queryCtx, queryContext, func(_ context.Context, _ string, qc *engine.QueryContext, row azquery.Row) error {
		alertResult, err := engine.ParseAlertResult(qc, row)
		if err != nil {
			return err
		}
		result.Alerts = append(result.Alerts, backtestAlertFromResult(alertResult))
		return nil
	})
	result.QueryDuration = time.Since(started).String()
	queryContextErr := queryCtx.Err()
	parentContextErr := ctx.Err()
	cancel()
	result.ResultsRetained = len(result.Alerts)

	if queryErr != nil && (errors.Is(queryErr, context.Canceled) || errors.Is(queryErr, context.DeadlineExceeded)) && parentContextErr != nil {
		result.Status = BacktestWindowStatusCancelled
		result.Error = safeBacktestReportError(queryErr, "query cancelled")
		return result, queryErr
	}
	if queryErr == nil && errors.Is(queryContextErr, context.DeadlineExceeded) {
		result.Status = BacktestWindowStatusError
		result.Error = safeBacktestReportError(queryContextErr, "query execution failed")
		return result, queryContextErr
	}
	if queryErr != nil {
		var limitErr *engine.ResultLimitExceededError
		if errors.As(queryErr, &limitErr) {
			result.ResultLimitExceeded = true
			result.ResultLimit = limitErr.Limit
		}
		var validationErr *engine.NotificationValidationError
		if errors.As(queryErr, &validationErr) {
			result.Status = BacktestWindowStatusError
			result.Error = safeBacktestReportError(validationErr, "invalid alert result")
			return result, queryErr
		}
		if limitErr != nil {
			result.Status = BacktestWindowStatusLimitExceeded
			result.Error = safeBacktestReportError(limitErr, "query result limit exceeded")
			return result, queryErr
		}
		result.Status = BacktestWindowStatusError
		result.Error = safeBacktestReportError(queryErr, "query execution failed")
		return result, queryErr
	}
	if result.ResultsRetained == 0 {
		result.Status = BacktestWindowStatusClear
	} else {
		result.Status = BacktestWindowStatusFiring
	}
	return result, nil
}

func backtestAlertFromResult(result engine.AlertResult) BacktestAlert {
	return BacktestAlert{
		Title:         result.Title,
		Summary:       result.Summary,
		Description:   result.Description,
		Severity:      result.Severity,
		Destination:   result.Destination,
		Source:        result.Source,
		CorrelationID: result.CorrelationID,
		CustomFields:  cloneStringMap(result.CustomFields),
	}
}

func markUnexecutedBacktestWindows(results []BacktestWindowResult, err error) []error {
	causes := make([]error, len(results))
	for i := range results {
		results[i].Status = BacktestWindowStatusCancelled
		results[i].Error = safeBacktestReportError(err, "query cancelled")
		causes[i] = err
	}
	return causes
}

func safeBacktestReportError(err error, fallback string) string {
	if errors.Is(err, context.Canceled) {
		return context.Canceled.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded.Error()
	}

	var unknownDB *engine.UnknownDBError
	if errors.As(err, &unknownDB) {
		return unknownDB.Error()
	}
	var validationErr *engine.NotificationValidationError
	if errors.As(err, &validationErr) {
		switch validationErr.Error() {
		case "severity must be specified", "title must be between 1 and 512 chars":
			return "invalid alert result: " + validationErr.Error()
		default:
			return "invalid alert result"
		}
	}
	var limitErr *engine.ResultLimitExceededError
	if errors.As(err, &limitErr) {
		return limitErr.Error()
	}
	return fallback
}

func completeBacktestReport(report *BacktestReport, causes []error) (*BacktestReport, error) {
	SortBacktestWindowResults(report.Rule.Windows)
	report.Summary = SummarizeBacktestWindowResults(report.Rule.Windows)
	if report.Summary.ErrorWindows == 0 && report.Summary.LimitExceededWindows == 0 && report.Summary.CancelledWindows == 0 {
		report.Rule.Outcome = BacktestRuleOutcomeCompleted
		return report, nil
	}

	report.Rule.Outcome = BacktestRuleOutcomePartial
	report.Rule.Error = fmt.Sprintf("%d error windows, %d limit-exceeded windows, %d cancelled windows", report.Summary.ErrorWindows, report.Summary.LimitExceededWindows, report.Summary.CancelledWindows)
	aggregate := make([]error, 0, len(causes)+1)
	aggregate = append(aggregate, ErrBacktestFailed)
	for _, cause := range causes {
		if cause != nil {
			aggregate = append(aggregate, cause)
		}
	}
	return report, newBacktestError(ErrBacktestFailed.Error()+": "+report.Rule.Error, aggregate...)
}
