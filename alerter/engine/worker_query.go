package engine

import (
	"context"
	"errors"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/pkg/logger"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
)

const (
	maxQueryTime      = 5 * time.Minute
	maxRetryAttempts  = 2
	defaultRetryDelay = 5 * time.Second
)

func (e *worker) ExecuteQuery(ctx context.Context) {
	result := e.executeQueryAttempt(ctx, nil)
	if result.aborted {
		e.finishAbortedQuery(result)
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

// executeScheduledQuery runs an evaluation and, when appropriate, its one
// retry. The evaluation context is deliberately created by the first attempt
// after it acquires a query slot. It remains alive across the retry delay and
// the second slot wait, so the query budget is shared by the whole evaluation.
func (e *worker) executeScheduledQuery(ctx context.Context) queryAttemptResult {
	result := e.executeQueryAttempt(ctx, nil)
	if result.aborted || !result.retryable {
		return result
	}

	retry := result.retry
	timer := e.clock.NewTimer(e.retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-retry.evaluationContext.Done():
	case <-timer.C():
	}
	if result, stopped := queryContextStopped(ctx, retry); stopped {
		return result
	}

	return e.executeQueryAttempt(ctx, retry)
}

type queryRetryState struct {
	evaluation             *alertRuleEvaluation
	queryContext           *QueryContext
	evaluationContext      context.Context
	evaluationCancel       context.CancelFunc
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

// queryContextStopped reports whether lifecycle termination or the shared
// evaluation budget prevents further query execution, with lifecycle termination
// taking precedence. Only lifecycle termination aborts reporting; an SDK error
// wrapping context.Canceled alone is not evidence of worker shutdown.
func queryContextStopped(ctx context.Context, retry *queryRetryState) (queryAttemptResult, bool) {
	if err := ctx.Err(); err != nil {
		return queryAttemptResult{retry: retry, err: err, aborted: true}, true
	}
	if retry == nil || retry.evaluationContext == nil {
		return queryAttemptResult{}, false
	}
	if err := retry.evaluationContext.Err(); err != nil {
		if retry.initialErr != nil {
			return retriableFailure(retry, err), true
		}
		return queryAttemptResult{retry: retry, err: err}, true
	}
	return queryAttemptResult{}, false
}

func (e *worker) executeQueryAttempt(ctx context.Context, retry *queryRetryState) queryAttemptResult {
	if result, stopped := queryContextStopped(ctx, retry); stopped {
		return result
	}

	// Use cached match decision
	if e.matchErr != nil {
		logger.Errorf("Skipping %s/%s due to cached criteria evaluation error: %v", e.rule.Namespace, e.rule.Name, e.matchErr)
		return queryAttemptResult{skipped: true}
	}
	if !e.matchAllowed {
		logger.Infof("Skipping %s/%s due to cached criteria evaluation", e.rule.Namespace, e.rule.Name)
		return queryAttemptResult{skipped: true}
	}

	// The first slot wait is outside the query budget. A retry's slot wait is
	// inside the shared evaluation context.
	slotContext := ctx
	if retry != nil {
		slotContext = retry.evaluationContext
	}
	select {
	case e.querySlots <- struct{}{}:
	case <-slotContext.Done():
		result, _ := queryContextStopped(ctx, retry)
		return result
	}
	defer func() { <-e.querySlots }()
	// Both select cases can be ready. Do not start an evaluation or query if
	// termination raced with acquiring the slot.
	if result, stopped := queryContextStopped(ctx, retry); stopped {
		return result
	}

	if retry == nil {
		executionTime := e.clock.Now()
		evaluation := newAlertRuleEvaluationAt(e.rule, executionTime, e.clock)
		retry = &queryRetryState{
			evaluation: evaluation,
			attempt:    1,
		}
	}

	// The first query slot has now been acquired, so waiting for it did not
	// consume the evaluation's query budget.
	if retry.evaluationContext == nil {
		retry.evaluationContext, retry.evaluationCancel = context.WithTimeout(ctx, e.queryTime)
	}

	if retry.queryContext == nil {
		var err error
		retry.queryContext, err = NewQueryContext(e.rule, retry.evaluation.executionTime, e.region)
		if err != nil {
			return queryAttemptResult{retry: retry, err: err, setupError: true}
		}
	}
	if result, stopped := queryContextStopped(ctx, retry); stopped {
		return result
	}

	queryCtx := retry.evaluationContext
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
	if result, stopped := queryContextStopped(ctx, retry); stopped {
		return result
	}
	err, retry.evaluation.rows = e.kustoClient.Query(queryCtx, retry.queryContext, wrappedHandler)
	transient := isTransientFailedRequest(err)
	if transient && retry.initialErr == nil {
		retry.initialErr = err
	}
	if result, stopped := queryContextStopped(ctx, retry); stopped {
		return result
	}
	if err == nil {
		return queryAttemptResult{retry: retry}
	}

	if transient {
		if retry.attempt < maxRetryAttempts {
			retry.attempt++
			logger.Warnf("Query %s/%s failed with a transient request error; scheduling retry attempt %d/%d after %s", e.rule.Namespace, e.rule.Name, retry.attempt, maxRetryAttempts, e.retryDelay)
			return queryAttemptResult{retry: retry, retryable: true}
		}
		return retriableFailure(retry, err)
	}

	return queryAttemptResult{retry: retry, err: err}
}

func retriableFailure(retry *queryRetryState, err error) queryAttemptResult {
	return queryAttemptResult{
		retry: retry,
		err: &retriableError{
			initialErr: retry.initialErr,
			retryErr:   err,
		},
	}
}
