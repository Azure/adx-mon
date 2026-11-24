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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	queryErrorNotificationTimeout = 30 * time.Second

	evaluationOutcomeSuccess               = metrics.AlertRuleEvaluationOutcomeSuccess
	evaluationOutcomeSetupError            = metrics.AlertRuleEvaluationOutcomeSetupError
	evaluationOutcomeUserError             = metrics.AlertRuleEvaluationOutcomeUserError
	evaluationOutcomeServiceError          = metrics.AlertRuleEvaluationOutcomeServiceError
	evaluationOutcomeCancelled             = metrics.AlertRuleEvaluationOutcomeCancelled
	evaluationOutcomeNotificationThrottled = metrics.AlertRuleEvaluationOutcomeNotificationThrottled
)

func (e *worker) handleQueryResult(ctx context.Context, result queryAttemptResult) {
	if err := ctx.Err(); err != nil {
		result.err = err
		e.finishAbortedQuery(result)
		return
	}
	if result.retry == nil {
		return
	}
	evaluation := result.retry.evaluation
	evalCli, isEvalCli := e.alertCli.(EvaluationAlertCli)
	sealEval := func(forceThrottled bool, extra []alert.SuppressedAlert, extraTotal int, incomplete bool) {
		if isEvalCli && result.retry.evalID != 0 {
			evalCli.SealEvaluation(result.retry.evalID, forceThrottled, extra, extraTotal, incomplete)
			result.retry.evalID = 0
		}
	}
	defer func() {
		sealEval(false, nil, 0, true)
		// Reporting uses the lifecycle context, not the expired query budget.
		// Shutdown during delivery must not be recorded as notification failure.
		if err := ctx.Err(); err != nil {
			result.err = err
			e.finishAbortedQuery(result)
			return
		}
		if result.retry.evaluationCancel != nil {
			result.retry.evaluationCancel()
		}
		evaluation.finish()
	}()

	err := result.err
	if result.setupError {
		evaluation.outcome = evaluationOutcomeSetupError
		logger.Errorf("Failed to wrap query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)
		e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Failed to wrap query: %v", err))
		return
	}
	if err == nil && !result.retry.notificationsThrottled {
		sealEval(false, nil, 0, false)
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
		if overflow, ok := errors.AsType[*ThrottledNotificationsError](err); ok {
			result.retry.throttledAlerts.merge(overflow)
		}
		incomplete := err != nil && !errors.Is(err, alert.ErrTooManyRequests)
		if incomplete {
			logger.Errorf("Failed to collect throttled notifications for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
		}
		if isEvalCli && result.retry.evalID != 0 {
			extra := make([]alert.SuppressedAlert, 0, len(result.retry.throttledAlerts.Notifications))
			for _, n := range result.retry.throttledAlerts.Notifications {
				extra = append(extra, alert.SuppressedAlert{
					Title:    n.Title,
					Severity: clampInt64ToInt(n.Severity),
				})
			}
			if ctx.Err() != nil {
				return
			}
			sealEval(true, extra, result.retry.throttledAlerts.Total, incomplete)
			e.updateAlertRuleStatus(ctx, evaluation, "Throttled", "Too many notifications sent")
			return
		}
		summary := throttledNotificationSummary(&result.retry.throttledAlerts)
		if incomplete {
			summary += "<br/>The query results could not be fully processed; this table may be incomplete."
		}
		linkedSummary, linkErr := KustoQueryLinks(summary, result.retry.queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
		if linkErr != nil {
			logger.Errorf("Failed to create query links for throttled notification for %s/%s: %s", e.rule.Namespace, e.rule.Name, linkErr)
		} else {
			summary = linkedSummary
		}
		if ctx.Err() != nil {
			return
		}
		metaDest, metaDests := e.rule.ResolvedMetaDestinations()
		err := e.alertCli.Create(alert.WithRuleMeta(ctx, e.ruleMeta()), e.alertAddr, alert.Alert{
			Destination:   metaDest,
			Destinations:  metaDests,
			Title:         fmt.Sprintf("Alert %s/%s has too many notifications in %s", e.rule.Namespace, e.rule.Name, e.region),
			Summary:       summary,
			Severity:      3,
			Source:        fmt.Sprintf("notification-failure/%s/%s", e.rule.Namespace, e.rule.Name),
			CorrelationID: fmt.Sprintf("notification-failure/%s/%s", e.rule.Namespace, e.rule.Name),
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Errorf("Failed to send alert for throttled notification for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
		}
		e.updateAlertRuleStatus(ctx, evaluation, "Throttled", "Too many notifications sent")
		return
	}

	if err != nil {
		// This failed because the query failed.
		logger.Errorf("Failed to execute query=%s/%s on %s/%s: %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database, err)
		if ctx.Err() != nil {
			return
		}

		retryExhausted := isRetriableError(err)
		if !retryExhausted && !isUserError(err) {
			evaluation.outcome = evaluationOutcomeServiceError
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query execution failed: %v", err))
			return
		}
		if retryExhausted {
			evaluation.outcome = evaluationOutcomeServiceError
		} else {
			evaluation.outcome = evaluationOutcomeUserError
		}

		// Store the original query error before it gets overwritten
		originalQueryErr := err

		notificationCtx, cancel := e.notificationContext(ctx)
		defer cancel()

		summary, err := KustoQueryLinks(fmt.Sprintf("This query is failing to execute:<br/><br/><pre>%s</pre><br/><br/>", originalQueryErr.Error()), result.retry.queryContext.Query, e.kustoClient.Endpoint(e.rule.Database), e.rule.Database)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if retryExhausted {
				metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
			}
			logger.Errorf("Failed to send failure alert for %s/%s: %s", e.rule.Namespace, e.rule.Name, err)
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed and unable to create failure alert: %v", originalQueryErr))
			return
		}

		endpointBaseName, _ := strings.CutPrefix(e.kustoClient.Endpoint(e.rule.Database), "https://")
		metaDest, metaDests := e.rule.ResolvedMetaDestinations()
		failureAlert := alert.Alert{
			Destination:   metaDest,
			Destinations:  metaDests,
			Title:         fmt.Sprintf("Alert %s/%s has query errors on %s", e.rule.Namespace, e.rule.Name, e.kustoClient.Endpoint(e.rule.Database)),
			Summary:       summary,
			Severity:      3,
			Source:        fmt.Sprintf("%s/%s", e.rule.Namespace, e.rule.Name),
			CorrelationID: fmt.Sprintf("alert-failure/%s/%s/%s", endpointBaseName, e.rule.Namespace, e.rule.Name),
		}
		if ctx.Err() != nil {
			return
		}
		err = e.alertCli.Create(alert.WithRuleMeta(notificationCtx, e.ruleMeta()), e.alertAddr, failureAlert)

		if ctx.Err() != nil {
			return
		}
		if retryExhausted {
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
		}
		if err != nil {
			logger.Errorf("Failed to send failure alert for %s/%s/%s: %s", endpointBaseName, e.rule.Namespace, e.rule.Name, err)
			// Only set the notification as failed if we are not able to send a failure alert directly.
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed and unable to send failure alert: %v", originalQueryErr))
			return
		} else if !isEvalCli {
			metrics.NotificationUnhealthy.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(0)
		}
		if retryExhausted {
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed after retry: %v", originalQueryErr))
		} else {
			// Query failed due to user error, so return the query to healthy.
			metrics.QueryHealth.WithLabelValues(e.rule.Namespace, e.rule.Name).Set(1)
			e.updateAlertRuleStatus(ctx, evaluation, "Error", fmt.Sprintf("Query failed with user error: %v", originalQueryErr))
		}
		return
	}
}

func (e *worker) finishAbortedQuery(result queryAttemptResult) {
	retry := result.retry
	if retry == nil {
		return
	}
	if evalCli, ok := e.alertCli.(EvaluationAlertCli); ok && retry.evalID != 0 {
		evalCli.SealEvaluation(retry.evalID, false, nil, 0, true)
		retry.evalID = 0
	}
	retry.evaluation.outcome = evaluationOutcomeServiceError
	if errors.Is(result.err, context.Canceled) {
		retry.evaluation.outcome = evaluationOutcomeCancelled
	}
	if retry.evaluationCancel != nil {
		retry.evaluationCancel()
	}
	retry.evaluation.finish()
}

func (e *worker) notificationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, queryErrorNotificationTimeout)
}

func (e *worker) ruleMeta() alert.RuleMeta {
	return alert.RuleMeta{
		UID:        e.rule.UID,
		Namespace:  e.rule.Namespace,
		Name:       e.rule.Name,
		Generation: e.rule.Generation,
	}
}

func (e *worker) sanitizeStatusMessage(msg string) string {
	if msg == "" {
		return ""
	}
	if reporter, ok := e.alertCli.(AlertStatusReporter); ok {
		return reporter.SanitizeMessage(msg)
	}
	if e.alertAddr != "" {
		msg = alert.SanitizeText(msg, e.alertAddr)
	}
	return alert.CapStatusMessage(msg, alert.MaxStatusMessageLength)
}

// updateAlertRuleStatus is the single writer for AlertRule.Status, persisting evaluation metrics
// alongside Criteria, DestinationMatched, and NotificationDelivery conditions with 512-byte
// sanitized messages containing zero raw webhook URLs.
func (e *worker) updateAlertRuleStatus(ctx context.Context, evaluation *alertRuleEvaluation, status, message string) {
	// Skip status update if we don't have a Kubernetes client or if this worker did not match criteria.
	if e.ctrlCli == nil || ctx.Err() != nil || e.matchErr != nil || !e.matchAllowed {
		return
	}

	updateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	sanitizedMessage := e.sanitizeStatusMessage(message)
	executionTimeMeta := metav1.NewTime(evaluation.executionTime)
	var lastAlertTime metav1.Time

	var err error
	for range 3 {
		alertRule := &alertrulev1.AlertRule{}
		if err = e.ctrlCli.Get(updateCtx, types.NamespacedName{
			Namespace: e.rule.Namespace,
			Name:      e.rule.Name,
		}, alertRule); err != nil {
			break
		}

		if e.rule.UID != "" && alertRule.GetUID() != "" && string(alertRule.GetUID()) != e.rule.UID {
			logger.Infof("Skipping status update for AlertRule %s/%s: UID changed from %s to %s",
				e.rule.Namespace, e.rule.Name, e.rule.UID, alertRule.GetUID())
			return
		}
		if (e.rule.Generation != 0 || alertRule.GetGeneration() != 0) && alertRule.GetGeneration() != e.rule.Generation {
			logger.Infof("Skipping status update for AlertRule %s/%s: generation changed from %d to %d",
				e.rule.Namespace, e.rule.Name, e.rule.Generation, alertRule.GetGeneration())
			return
		}

		alertRule.Status.LastQueryTime = executionTimeMeta
		if evaluation.alertsGenerated > 0 {
			alertRule.Status.LastAlertTime = executionTimeMeta
		}
		alertRule.Status.LastEvaluationDurationMilliseconds = evaluation.elapsed().Milliseconds()
		alertRule.Status.LastRowsReturned = int64(evaluation.rows)
		alertRule.Status.LastAlertsGenerated = int64(evaluation.alertsGenerated)
		alertRule.Status.Status = status
		alertRule.Status.Message = sanitizedMessage

		observedGen := alertRule.GetGeneration()
		nowMeta := metav1.NewTime(e.clock.Now())

		meta.SetStatusCondition(&alertRule.Status.Conditions, metav1.Condition{
			Type:               alertrulev1.ConditionCriteria,
			Status:             metav1.ConditionTrue,
			Reason:             alertrulev1.ReasonCriteriaMatched,
			Message:            "criteria/expression matched",
			ObservedGeneration: observedGen,
			LastTransitionTime: nowMeta,
		})

		if reporter, ok := e.alertCli.(AlertStatusReporter); ok {
			destReport := reporter.EvaluateDestinationMatch(e.rule.CandidateDestinations())
			meta.SetStatusCondition(&alertRule.Status.Conditions, metav1.Condition{
				Type:               alertrulev1.ConditionDestinationMatched,
				Status:             destReport.Status,
				Reason:             destReport.Reason,
				Message:            e.sanitizeStatusMessage(destReport.Message),
				ObservedGeneration: observedGen,
				LastTransitionTime: nowMeta,
			})

			ruleUID := string(alertRule.GetUID())
			if ruleUID == "" {
				ruleUID = e.rule.UID
			}
			ruleGen := observedGen
			if ruleGen == 0 && e.rule.Generation != 0 {
				ruleGen = e.rule.Generation
			}
			delivReport := reporter.EvaluateNotificationDelivery(alert.RuleMeta{
				UID:        ruleUID,
				Namespace:  e.rule.Namespace,
				Name:       e.rule.Name,
				Generation: ruleGen,
			})
			meta.SetStatusCondition(&alertRule.Status.Conditions, metav1.Condition{
				Type:               alertrulev1.ConditionNotificationDelivery,
				Status:             delivReport.Status,
				Reason:             delivReport.Reason,
				Message:            e.sanitizeStatusMessage(delivReport.Message),
				ObservedGeneration: observedGen,
				LastTransitionTime: nowMeta,
			})
		}

		lastAlertTime = alertRule.Status.LastAlertTime
		err = e.ctrlCli.Status().Update(updateCtx, alertRule)
		if !apierrors.IsConflict(err) {
			break
		}
	}
	if err != nil {
		logger.Errorf("Failed to update AlertRule %s/%s status: %v", e.rule.Namespace, e.rule.Name, err)
		return
	}

	logger.Debugf("Updated AlertRule %s/%s status: LastQueryTime=%v, LastAlertTime=%v, Status=%s",
		e.rule.Namespace, e.rule.Name, evaluation.executionTime, lastAlertTime, status)
}
