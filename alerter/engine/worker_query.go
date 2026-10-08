package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/metrics"
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

	// Release the worker slot.
	defer func() { <-e.querySlots }()

	ctx, cancel := context.WithTimeout(ctx, maxQueryTime)
	defer cancel()

	evaluation := newAlertRuleEvaluation(e.rule, e.clock)
	defer evaluation.finish()

	queryContext, err := NewQueryContext(e.rule, evaluation.executionTime, e.region)
	if err != nil {
		evaluation.outcome = evaluationOutcomeSetupError
		logger.Errorf("Failed to wrap query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)
		e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Failed to wrap query: %v", err))
		return
	}

	logger.Infof("Executing %s/%s on %s/%s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)

	// Create a wrapper handler that tracks alerts generated
	var notificationsThrottled bool
	var throttledAlerts ThrottledNotificationsError
	wrappedHandler := func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error {
		if !notificationsThrottled {
			err := e.handlerFn(ctx, endpoint, qc, row)
			if err == nil {
				evaluation.alertsGenerated++
				return nil
			}
			if !errors.Is(err, alert.ErrTooManyRequests) {
				return err
			}
			notificationsThrottled = true
		}

		result, err := ParseAlertResult(qc, row)
		if err != nil {
			return err
		}
		throttledAlerts.Add(result)
		return nil
	}

	err, evaluation.rows = e.kustoClient.Query(ctx, queryContext, wrappedHandler)
	if err != nil || notificationsThrottled {
		// This failed because we sent too many notifications.
		if notificationsThrottled || errors.Is(err, alert.ErrTooManyRequests) {
			evaluation.outcome = evaluationOutcomeNotificationThrottled
			var overflow *ThrottledNotificationsError
			if errors.As(err, &overflow) {
				throttledAlerts.merge(overflow)
			}
			summary := throttledNotificationSummary(&throttledAlerts)
			if err != nil && !errors.Is(err, alert.ErrTooManyRequests) {
				logger.Errorf("Failed to collect throttled notifications for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
				summary += "<br/>The query results could not be fully processed; this table may be incomplete."
			}
			linkedSummary, linkErr := KustoQueryLinks(summary, queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
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

		// This failed because the query failed.
		logger.Errorf("Failed to execute query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)

		if !isUserError(err) {
			evaluation.outcome = evaluationOutcomeServiceError
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query execution failed: %v", err))
			return
		}
		evaluation.outcome = evaluationOutcomeUserError

		// Store the original query error before it gets overwritten
		originalQueryErr := err

		summary, err := KustoQueryLinks(fmt.Sprintf("This query is failing to execute:<br/><br/><pre>%s</pre><br/><br/>", originalQueryErr.Error()), queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
		if err != nil {
			logger.Errorf("Failed to send failure alert for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed and unable to create failure alert: %v", originalQueryErr))
			return
		}

		endpointBaseName, _ := strings.CutPrefix(e.kustoClient.Endpoint(e.rule.Database), "https://")
		err = e.alertCli.Create(ctx, e.alertAddr, alert.Alert{
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
		// Query failed due to user error, so return the query to healthy.
		metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
		e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed with user error: %v", originalQueryErr))
		return
	}

	metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
	metrics.QueriesRunTotal.WithLabelValues().Inc()
	logger.Infof("Completed %s/%s in %s", e.rule.Namespace, e.rule.Name, e.clock.Since(evaluation.executionTime))
	logger.Infof("Query for %s/%s completed with %d entries found", e.rule.Namespace, e.rule.Name, evaluation.rows)

	// Update AlertRule status with execution information
	e.updateAlertRuleStatus(ctx, evaluation, "Success", "")
}
