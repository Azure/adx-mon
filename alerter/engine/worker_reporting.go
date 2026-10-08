package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	evaluationOutcomeSuccess               = metrics.AlertRuleEvaluationOutcomeSuccess
	evaluationOutcomeSetupError            = metrics.AlertRuleEvaluationOutcomeSetupError
	evaluationOutcomeUserError             = metrics.AlertRuleEvaluationOutcomeUserError
	evaluationOutcomeServiceError          = metrics.AlertRuleEvaluationOutcomeServiceError
	evaluationOutcomeNotificationThrottled = metrics.AlertRuleEvaluationOutcomeNotificationThrottled
)

func (e *worker) handleQueryResult(ctx context.Context, result queryAttemptResult) {
	if result.retry == nil {
		return
	}
	evaluation := result.retry.evaluation
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
		if !isUserError(err) {
			evaluation.outcome = evaluationOutcomeServiceError
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query execution failed: %v", err))
			return
		}
		evaluation.outcome = evaluationOutcomeUserError

		// Store the original query error before it gets overwritten
		originalQueryErr := err

		summary, err := KustoQueryLinks(fmt.Sprintf("This query is failing to execute:<br/><br/><pre>%s</pre><br/><br/>", originalQueryErr.Error()), result.retry.queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
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
