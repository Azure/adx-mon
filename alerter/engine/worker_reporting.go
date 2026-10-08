package engine

import (
	"context"
	"fmt"
	"time"

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
		LastTransitionTime: metav1.Now(),
	}
	if meta.SetStatusCondition(&alertRule.Status.Conditions, cond) {
		if err := e.ctrlCli.Status().Update(updateCtx, alertRule); err != nil {
			logger.Errorf("Failed to update AlertRule %s/%s criteria condition: %v", e.rule.Namespace, e.rule.Name, err)
		}
	}
}
