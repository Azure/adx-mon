package engine

import (
	"context"
	"errors"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/pkg/logger"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
)

const maxQueryTime = 5 * time.Minute

func (e *worker) ExecuteQuery(ctx context.Context) {
	// Use cached match decision
	if e.matchErr != nil {
		logger.Errorf("Skipping %s/%s due to cached criteria evaluation error: %v", e.rule.Namespace, e.rule.Name, e.matchErr)
		return
	}
	if !e.matchAllowed {
		logger.Infof("Skipping %s/%s due to cached criteria evaluation", e.rule.Namespace, e.rule.Name)
		return
	}

	if err := ctx.Err(); err != nil {
		return
	}

	// Try to acquire a worker slot while still honoring shutdown.
	select {
	case e.querySlots <- struct{}{}:
	case <-ctx.Done():
		return
	}

	// Release the worker slot after reporting completes.
	defer func() { <-e.querySlots }()

	ctx, cancel := context.WithTimeout(ctx, maxQueryTime)
	defer cancel()

	result := e.executeQueryAttempt(ctx, nil)
	defer result.retry.evaluation.finish()
	e.handleQueryResult(ctx, result)
}

type queryRetryState struct {
	evaluation             *alertRuleEvaluation
	queryContext           *QueryContext
	notificationsThrottled bool
	throttledAlerts        ThrottledNotificationsError
}

type queryAttemptResult struct {
	retry      *queryRetryState
	err        error
	setupError bool
}

func (e *worker) executeQueryAttempt(ctx context.Context, retry *queryRetryState) queryAttemptResult {
	if retry == nil {
		retry = &queryRetryState{evaluation: newAlertRuleEvaluation(e.rule, e.clock)}
	}

	if retry.queryContext == nil {
		var err error
		retry.queryContext, err = NewQueryContext(e.rule, retry.evaluation.executionTime, e.region)
		if err != nil {
			return queryAttemptResult{retry: retry, err: err, setupError: true}
		}
	}

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

	err, rows := e.kustoClient.Query(ctx, retry.queryContext, wrappedHandler)
	retry.evaluation.rows = rows
	return queryAttemptResult{retry: retry, err: err}
}
