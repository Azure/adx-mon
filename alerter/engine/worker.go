package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/queue"
	"github.com/Azure/adx-mon/alerter/rules"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	kerrors "github.com/Azure/azure-kusto-go/azkustodata/errors"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	maxQueryTime                       = 5 * time.Minute
	queryErrorNotificationReserve      = 30 * time.Second
	remoteEntityResolutionMaxAttempts  = 2
	remoteEntityResolutionRetryDelay   = 5 * time.Second
	remoteEntityResolutionMinQueryTime = 30 * time.Second

	evaluationOutcomeSuccess               = metrics.AlertRuleEvaluationOutcomeSuccess
	evaluationOutcomeSetupError            = metrics.AlertRuleEvaluationOutcomeSetupError
	evaluationOutcomeUserError             = metrics.AlertRuleEvaluationOutcomeUserError
	evaluationOutcomeServiceError          = metrics.AlertRuleEvaluationOutcomeServiceError
	evaluationOutcomeNotificationThrottled = metrics.AlertRuleEvaluationOutcomeNotificationThrottled
)

type worker struct {
	mu     sync.Mutex
	cancel context.CancelFunc

	wg          sync.WaitGroup
	rule        *rules.Rule
	region      string
	kustoClient Client
	alertAddr   string
	alertCli    interface {
		Create(ctx context.Context, endpoint string, alert alert.Alert) error
	}
	handlerFn    func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error
	querySlots   chan struct{}
	ctrlCli      client.Client
	queryTime    time.Duration
	retryDelay   time.Duration
	minRetryTime time.Duration
	clock        clock.Clock

	// criteria/expression evaluation cached at construction
	matchAllowed bool
	matchErr     error
}

// WorkerConfig groups parameters for constructing a worker.
type WorkerConfig struct {
	Rule                 *rules.Rule
	Region               string
	Tags                 map[string]string
	KustoClient          Client
	MaxConcurrentQueries int
	AlertClient          interface {
		Create(ctx context.Context, endpoint string, alert alert.Alert) error
	}
	AlertAddr        string
	HandlerFn        func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error
	CtrlClient       client.Client
	sharedQuerySlots chan struct{}
	Clock            clock.Clock
}

// NewWorker creates a worker and performs one-time match evaluation.
func NewWorker(cfg *WorkerConfig) *worker {
	if cfg == nil || cfg.Rule == nil {
		return nil
	}

	querySlots := cfg.sharedQuerySlots
	if querySlots == nil {
		querySlots = queue.New(cfg.MaxConcurrentQueries)
	}
	workerClock := cfg.Clock
	if workerClock == nil {
		workerClock = clock.RealClock{}
	}
	w := &worker{
		rule:         cfg.Rule,
		region:       cfg.Region,
		kustoClient:  cfg.KustoClient,
		alertCli:     cfg.AlertClient,
		alertAddr:    cfg.AlertAddr,
		handlerFn:    cfg.HandlerFn,
		querySlots:   querySlots,
		ctrlCli:      cfg.CtrlClient,
		queryTime:    maxQueryTime,
		retryDelay:   remoteEntityResolutionRetryDelay,
		minRetryTime: remoteEntityResolutionMinQueryTime,
		clock:        workerClock,
	}
	allowed, err := cfg.Rule.Matches(cfg.Tags)
	w.matchAllowed = allowed
	w.matchErr = err
	if err != nil {
		logger.Errorf("Worker initialization match error for %s/%s: %v", cfg.Rule.Namespace, cfg.Rule.Name, err)
	} else if !allowed {
		logger.Infof("Worker %s/%s disabled (criteria/expression not matched) at initialization", cfg.Rule.Namespace, cfg.Rule.Name)
	}
	return w
}

func (e *worker) Run(ctx context.Context) {
	e.wg.Add(1)

	e.mu.Lock()
	ctx, e.cancel = context.WithCancel(ctx)
	e.mu.Unlock()

	// Best-effort: update criteria condition reflecting cached match evaluation
	e.updateAlertRuleCriteriaCondition(ctx)

	go func() {
		defer e.wg.Done()

		// Calculate the next execution time based on last execution
		nextQueryTime := e.calculateNextQueryTime()
		scheduledQueryTime := nextQueryTime
		if e.rule.LastQueryTime.IsZero() {
			// calculateNextQueryTime returns a slightly-past timestamp only to
			// trigger immediate execution. Use the actual desired execution
			// time as the end of the first query window.
			scheduledQueryTime = e.clock.Now()
		}

		logger.Infof("Creating query executor for %s/%s in %s executing every %s, next execution at %s",
			e.rule.Namespace, e.rule.Name, e.rule.Database, e.rule.Interval.String(), nextQueryTime.Format(time.RFC3339))

		timer := e.clock.NewTimer(nextQueryTime.Sub(e.clock.Now()))
		defer timer.Stop()

		var (
			retry          *queryRetryState
			normalSchedule bool
		)
		for {
			select {
			case <-ctx.Done():
				if retry != nil {
					e.finishAbortedQuery(retry)
				}
				return
			case <-timer.C():
				// Once the initial evaluation has completed, keep the normal
				// schedule anchored to absolute deadlines like time.Ticker does.
				// A retry temporarily replaces the timer but does not move the
				// next normal execution.
				if retry == nil && normalSchedule {
					scheduledQueryTime = nextQueryTime
					nextQueryTime = e.advanceQuerySchedule(nextQueryTime, e.clock.Now())
				}

				result := e.executeQueryAttempt(ctx, retry, scheduledQueryTime)
				if result.aborted {
					e.finishAbortedQuery(result.retry)
					return
				}
				if result.retryable {
					retry = result.retry
					timer.Reset(e.retryDelay)
					continue
				}

				e.handleQueryResult(ctx, result)
				retry = nil
				if !normalSchedule {
					// The original implementation created its ticker after the
					// initial query returned. Anchor the recurring schedule at
					// the end of that initial evaluation, including any retry.
					nextQueryTime = e.clock.Now().Add(e.rule.Interval)
					scheduledQueryTime = nextQueryTime
					normalSchedule = true
				}
				timer.Reset(nextQueryTime.Sub(e.clock.Now()))
			}
		}
	}()
}

// calculateNextQueryTime determines when the next execution should occur
// based on the last execution time from the AlertRule status
func (e *worker) calculateNextQueryTime() time.Time {
	// If no last query time, this is the first execution
	if e.rule.LastQueryTime.IsZero() {
		return e.clock.Now().Add(-time.Second) // Immediate execution
	}

	// Calculate next execution time based on last execution + interval
	lastQueryTime := e.rule.LastQueryTime
	nextQueryTime := lastQueryTime.Add(e.rule.Interval)

	return nextQueryTime
}

// advanceQuerySchedule advances the normal schedule by one interval and skips any
// additional deadlines missed while the worker was busy. This mirrors a
// time.Ticker's behavior of delivering at most one pending tick rather than
// accumulating a backlog.
func (e *worker) advanceQuerySchedule(next time.Time, now time.Time) time.Time {
	next = next.Add(e.rule.Interval)
	for !next.After(now) {
		next = next.Add(e.rule.Interval)
	}
	return next
}

func (e *worker) ExecuteQuery(ctx context.Context) {
	result := e.executeQueryAttempt(ctx, nil, time.Time{})
	if result.aborted {
		e.finishAbortedQuery(result.retry)
		return
	}
	if result.skipped {
		return
	}
	// RunOnce is used by linting and has no scheduler. Treat a transient
	// failure as terminal for this one-shot execution rather than waiting.
	if result.retryable {
		result.err = result.retry.initialErr
		result.retryable = false
	}
	e.handleQueryResult(ctx, result)
}

func (e *worker) finishAbortedQuery(retry *queryRetryState) {
	if retry == nil {
		return
	}
	retry.evaluation.outcome = evaluationOutcomeServiceError
	retry.evaluation.finish()
}

type queryRetryState struct {
	evaluation             *alertRuleEvaluation
	queryContext           *QueryContext
	deadline               time.Time
	attempt                int
	initialErr             error
	notificationsThrottled bool
	throttledAlerts        ThrottledNotificationsError
}

type queryAttemptResult struct {
	retry      *queryRetryState
	err        error
	retryable  bool
	setupError bool
	aborted    bool
	skipped    bool
}

func (e *worker) executeQueryAttempt(ctx context.Context, retry *queryRetryState, scheduledQueryTime time.Time) queryAttemptResult {
	// Use cached match decision
	if e.matchErr != nil {
		logger.Errorf("Skipping %s/%s due to cached criteria evaluation error: %v", e.rule.Namespace, e.rule.Name, e.matchErr)
		return queryAttemptResult{skipped: true}
	}
	if !e.matchAllowed {
		logger.Infof("Skipping %s/%s due to cached criteria evaluation", e.rule.Namespace, e.rule.Name)
		return queryAttemptResult{skipped: true}
	}

	if err := ctx.Err(); err != nil {
		return queryAttemptResult{aborted: true}
	}

	if retry == nil {
		if scheduledQueryTime.IsZero() {
			scheduledQueryTime = e.clock.Now()
		}
		evaluation := newAlertRuleEvaluationAt(e.rule, scheduledQueryTime, e.clock)
		retry = &queryRetryState{
			evaluation: evaluation,
			attempt:    1,
		}
	}

	// Try to acquire a worker slot while still honoring shutdown.
	select {
	case e.querySlots <- struct{}{}:
	case <-ctx.Done():
		return queryAttemptResult{retry: retry, aborted: true}
	}

	// Release the worker slot.
	defer func() { <-e.querySlots }()

	if retry.deadline.IsZero() {
		retry.deadline = e.retryDeadline(ctx)
	}
	if retry.queryContext == nil {
		var err error
		retry.queryContext, err = NewQueryContext(e.rule, retry.evaluation.executionTime, e.region)
		if err != nil {
			return queryAttemptResult{retry: retry, err: err, setupError: true}
		}
	}

	queryCtx, cancel := e.queryAttemptContext(ctx, retry.deadline)
	defer cancel()

	logger.Infof("Executing %s/%s on %s/%s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)

	// Create a wrapper handler that tracks alerts generated and retains rows
	// after notification throttling so the summary can identify affected alerts.
	wrappedHandler := func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error {
		if !retry.notificationsThrottled {
			err := e.handlerFn(ctx, endpoint, qc, row)
			if err == nil {
				retry.evaluation.alertsGenerated++
				return nil
			}
			if !errors.Is(err, alert.ErrTooManyRequests) {
				return err
			}
			retry.notificationsThrottled = true
		}
		result, err := ParseAlertResult(qc, row)
		if err != nil {
			return err
		}
		retry.throttledAlerts.Add(result)
		return nil
	}

	var err error
	err, retry.evaluation.rows = e.kustoClient.Query(queryCtx, retry.queryContext, wrappedHandler)
	if ctx.Err() != nil {
		return queryAttemptResult{retry: retry, aborted: true}
	}
	if err == nil {
		return queryAttemptResult{retry: retry}
	}

	if isTransientRemoteEntityResolutionError(err) {
		if retry.initialErr == nil {
			retry.initialErr = err
		}
		if retry.attempt < remoteEntityResolutionMaxAttempts && e.hasRetryBudget(retry.deadline) {
			retry.attempt++
			logger.Warnf("Query %s/%s failed with a transient remote entity resolution error; scheduling retry attempt %d/%d after %s", e.rule.Namespace, e.rule.Name, retry.attempt, remoteEntityResolutionMaxAttempts, e.retryDelay)
			return queryAttemptResult{retry: retry, retryable: true}
		}
		return queryAttemptResult{
			retry: retry,
			err: &remoteEntityResolutionRetryError{
				initialErr: retry.initialErr,
				retryErr:   err,
			},
		}
	}

	return queryAttemptResult{retry: retry, err: err}
}

func (e *worker) retryDeadline(ctx context.Context) time.Time {
	now := e.clock.Now()
	deadline := now.Add(e.queryTime)
	if parentDeadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(parentDeadline); remaining < e.queryTime {
			return now.Add(remaining)
		}
	}
	return deadline
}

func (e *worker) queryAttemptContext(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	remaining := deadline.Sub(e.clock.Now())
	if parentDeadline, ok := ctx.Deadline(); ok {
		if parentRemaining := time.Until(parentDeadline); parentRemaining < remaining {
			remaining = parentRemaining
		}
	}
	return context.WithTimeout(ctx, remaining-queryErrorNotificationReserve)
}

func (e *worker) hasRetryBudget(deadline time.Time) bool {
	return deadline.Sub(e.clock.Now()) >= e.retryDelay+e.minRetryTime+queryErrorNotificationReserve
}

func (e *worker) handleQueryResult(ctx context.Context, result queryAttemptResult) {
	if result.retry == nil {
		return
	}
	evaluation := result.retry.evaluation
	defer evaluation.finish()

	err := result.err
	if result.setupError {
		evaluation.outcome = evaluationOutcomeSetupError
		logger.Errorf("Failed to wrap query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)
		e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Failed to wrap query: %v", err))
		return
	}
	if err == nil && !result.retry.notificationsThrottled {
		metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
		metrics.QueriesRunTotal.WithLabelValues().Inc()
		logger.Infof("Completed %s/%s in %s", e.rule.Namespace, e.rule.Name, e.clock.Since(evaluation.executionTime))
		logger.Infof("Query for %s/%s completed with %d entries found", e.rule.Namespace, e.rule.Name, evaluation.rows)
		e.updateAlertRuleStatus(ctx, evaluation, "Success", "")
		return
	}

	if result.retry.notificationsThrottled || errors.Is(err, alert.ErrTooManyRequests) {
		// This failed because we sent too many notifications.
		evaluation.outcome = evaluationOutcomeNotificationThrottled
		var overflow *ThrottledNotificationsError
		if errors.As(err, &overflow) {
			result.retry.throttledAlerts.merge(overflow)
		}
		summary := throttledNotificationSummary(&result.retry.throttledAlerts)
		if err != nil && !errors.Is(err, alert.ErrTooManyRequests) {
			logger.Errorf("Failed to collect throttled notifications for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			summary += "<br/>The query results could not be fully processed; this table may be incomplete."
		}
		linkedSummary, linkErr := KustoQueryLinks(summary, result.retry.queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
		if linkErr != nil {
			logger.Errorf("Failed to create query links for throttled notification for %s/%s: %s", e.rule.Namespace, e.rule.Name, linkErr)
		} else {
			summary = linkedSummary
		}
		err := e.alertCli.Create(ctx, e.alertAddr, alert.Alert{
			Destination:   e.rule.Destination,
			Title:         fmt.Sprintf("Alert %s/%s has too many notifications in %s", e.rule.Namespace, e.rule.Name, e.region),
			Summary:       summary,
			Severity:      3,
			Source:        fmt.Sprintf("notification-failure/%s/%s", e.rule.Namespace, e.rule.Name),
			CorrelationID: fmt.Sprintf("notification-failure/%s/%s", e.rule.Namespace, e.rule.Name),
		})
		if err != nil {
			logger.Errorf("Failed to send alert for throttled notification for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
		}
		e.updateAlertRuleStatus(ctx, evaluation, "Throttled", "Too many notifications sent")
		return
	}

	if err != nil {
		// This failed because the query failed.
		logger.Errorf("Failed to execute query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)

		retryExhausted := isRemoteEntityResolutionRetryExhausted(err)
		if !retryExhausted && !isUserError(err) {
			evaluation.outcome = evaluationOutcomeServiceError
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query execution failed: %v", err))
			return
		}
		if retryExhausted {
			evaluation.outcome = evaluationOutcomeServiceError
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
		} else {
			evaluation.outcome = evaluationOutcomeUserError
		}

		// Store the original query error before it gets overwritten
		originalQueryErr := err

		notificationCtx, cancel := e.notificationContext(ctx, result.retry.deadline)
		defer cancel()

		summary, err := KustoQueryLinks(fmt.Sprintf("This query is failing to execute:<br/><br/><pre>%s</pre><br/><br/>", originalQueryErr.Error()), result.retry.queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
		if err != nil {
			logger.Errorf("Failed to send failure alert for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed and unable to create failure alert: %v", originalQueryErr))
			return
		}

		endpointBaseName, _ := strings.CutPrefix(e.kustoClient.Endpoint(e.rule.Database), "https://")
		err = e.alertCli.Create(notificationCtx, e.alertAddr, alert.Alert{
			Destination:   e.rule.Destination,
			Title:         fmt.Sprintf("Alert %s/%s has query errors on %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database)),
			Summary:       summary,
			Severity:      3,
			Source:        fmt.Sprintf("%s/%s", e.rule.Namespace, e.rule.Name),
			CorrelationID: fmt.Sprintf("alert-failure/%s/%s/%s", endpointBaseName, e.rule.Namespace, e.rule.Name),
		})

		if err != nil {
			logger.Errorf("Failed to send failure alert for %s/%s/%s: %s", endpointBaseName, e.rule.Namespace, e.rule.Name, err)
			// Only set the notification as failed if we are not able to send a failure alert directly.
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed and unable to send failure alert: %v", originalQueryErr))
			return
		} else {
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
		}
		if retryExhausted {
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed after remote entity resolution retry: %v", originalQueryErr))
		} else {
			// Query failed due to user error, so return the query to healthy.
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed with user error: %v", originalQueryErr))
		}
		return
	}
}

func (e *worker) notificationContext(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	remaining := queryErrorNotificationReserve
	if retryRemaining := deadline.Sub(e.clock.Now()); retryRemaining < remaining {
		remaining = retryRemaining
	}
	if parentDeadline, ok := ctx.Deadline(); ok {
		if parentRemaining := time.Until(parentDeadline); parentRemaining < remaining {
			remaining = parentRemaining
		}
	}
	return context.WithTimeout(ctx, remaining)
}

func isTransientRemoteEntityResolutionError(err error) bool {
	if err == nil {
		return false
	}

	var kerr *kerrors.HttpError
	if !errors.As(err, &kerr) {
		return false
	}

	lowerErr := strings.ToLower(kerr.Error())
	if kerr.StatusCode != http.StatusBadRequest ||
		!strings.Contains(lowerErr, "sem0056") ||
		!strings.Contains(lowerErr, "resolving remote entities") ||
		!strings.Contains(lowerErr, "failed to resolve name or pattern") {
		return false
	}

	return !strings.Contains(lowerErr, "obo token is required for cross-cluster communication") &&
		!strings.Contains(lowerErr, "is not authorized to") &&
		!strings.Contains(lowerErr, "access denied") &&
		!strings.Contains(lowerErr, "is not allowed by the callout policy")
}

type remoteEntityResolutionRetryError struct {
	initialErr error
	retryErr   error
}

func (e *remoteEntityResolutionRetryError) Error() string {
	return fmt.Sprintf("remote entity resolution retry failed: initial error: %v; retry error: %v", e.initialErr, e.retryErr)
}

func (e *remoteEntityResolutionRetryError) Unwrap() error {
	return e.retryErr
}

func isRemoteEntityResolutionRetryExhausted(err error) bool {
	var retryErr *remoteEntityResolutionRetryError
	return errors.As(err, &retryErr)
}

// updateAlertRuleStatus updates the AlertRule status with the execution information
func (e *worker) updateAlertRuleStatus(ctx context.Context, evaluation *alertRuleEvaluation, status, message string) {
	// Skip status update if we don't have a Kubernetes client
	if e.ctrlCli == nil {
		return
	}

	// Create a context for the status update using the passed-in context as parent
	updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Get the current AlertRule
	alertRule := &alertrulev1.AlertRule{}
	err := e.ctrlCli.Get(updateCtx, types.NamespacedName{
		Namespace: e.rule.Namespace,
		Name:      e.rule.Name,
	}, alertRule)
	if err != nil {
		logger.Errorf("Failed to get AlertRule %s/%s for status update: %v", e.rule.Namespace, e.rule.Name, err)
		return
	}

	// Update the status fields using existing fields
	executionTimeMeta := metav1.NewTime(evaluation.executionTime)
	alertRule.Status.LastQueryTime = executionTimeMeta
	if evaluation.alertsGenerated > 0 {
		alertRule.Status.LastAlertTime = executionTimeMeta
	}
	alertRule.Status.LastEvaluationDurationMilliseconds = evaluation.elapsed().Milliseconds()
	alertRule.Status.LastRowsReturned = int64(evaluation.rows)
	alertRule.Status.LastAlertsGenerated = int64(evaluation.alertsGenerated)
	alertRule.Status.Status = status
	alertRule.Status.Message = message

	// Update the AlertRule status
	err = e.ctrlCli.Status().Update(updateCtx, alertRule)
	if err != nil {
		logger.Errorf("Failed to update AlertRule %s/%s status: %v", e.rule.Namespace, e.rule.Name, err)
		return
	}

	logger.Debugf("Updated AlertRule %s/%s status: LastQueryTime=%v, LastAlertTime=%v, Status=%s",
		e.rule.Namespace, e.rule.Name, evaluation.executionTime, alertRule.Status.LastAlertTime, status)
}

// updateAlertRuleCriteriaCondition writes the ConditionCriteria condition based on cached match evaluation.
// It is safe/no-op when ctrlCli is not configured.
func (e *worker) updateAlertRuleCriteriaCondition(ctx context.Context) {
	if e.ctrlCli == nil {
		return
	}

	updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	alertRule := &alertrulev1.AlertRule{}
	if err := e.ctrlCli.Get(updateCtx, types.NamespacedName{Namespace: e.rule.Namespace, Name: e.rule.Name}, alertRule); err != nil {
		logger.Errorf("Failed to get AlertRule %s/%s for criteria condition update: %v", e.rule.Namespace, e.rule.Name, err)
		return
	}

	// Build condition based on evaluation result
	condStatus := metav1.ConditionFalse
	reason := alertrulev1.ReasonCriteriaNotMatched
	message := "criteria map did not match or expression evaluated to false"
	if e.matchErr != nil {
		reason = alertrulev1.ReasonCriteriaExpressionError
		message = fmt.Sprintf("criteria expression error: %v", e.matchErr)
	} else if e.matchAllowed {
		condStatus = metav1.ConditionTrue
		reason = alertrulev1.ReasonCriteriaMatched
		message = "criteria/expression matched"
	}

	cond := metav1.Condition{
		Type:               alertrulev1.ConditionCriteria,
		Status:             condStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: alertRule.GetGeneration(),
		LastTransitionTime: metav1.NewTime(e.clock.Now()),
	}
	if meta.SetStatusCondition(&alertRule.Status.Conditions, cond) {
		if err := e.ctrlCli.Status().Update(updateCtx, alertRule); err != nil {
			logger.Errorf("Failed to update AlertRule %s/%s criteria condition: %v", e.rule.Namespace, e.rule.Name, err)
		}
	}
}

func (e *worker) Close() {
	e.mu.Lock()
	cancelFn := e.cancel
	e.mu.Unlock()
	if cancelFn != nil {
		cancelFn()
	}

	e.wg.Wait()
}

func isUserError(err error) bool {
	if err == nil {
		return false
	}

	// User specified a database in their CRD that adx-mon does not have configured.
	var unknownDB *UnknownDBError
	if errors.As(err, &unknownDB) {
		return true
	}

	// User's query results are missing a required column, or they are the wrong type.
	var validationErr *NotificationValidationError
	if errors.As(err, &validationErr) {
		return true
	}

	// Look to see if a kusto query error is specific to how the query was defined and not due to problems with adx-mon itself.
	var kerr *kerrors.HttpError
	if errors.As(err, &kerr) {
		if kerr.Kind == kerrors.KClientArgs {
			return true
		}
		lowerErr := strings.ToLower(kerr.Error())
		if strings.Contains(lowerErr, "sem0001") || strings.Contains(lowerErr, "semantic error") || strings.Contains(lowerErr, "request is invalid") {
			return true
		}
	}

	return false
}
