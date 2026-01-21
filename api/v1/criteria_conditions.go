package v1

import (
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionedResource is a ConditionedObject that also exposes generation tracking for status updates.
// +k8s:deepcopy-gen=false
type ConditionedResource interface {
	ConditionedObject
	GetGeneration() int64
}

// FormatClusterLabels returns a stable, human-readable summary of cluster labels for status messages.
// Keys are sorted to make comparisons deterministic.
func FormatClusterLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, labels[k]))
	}
	return strings.Join(parts, ", ")
}

// NewCriteriaCondition constructs a metav1.Condition encapsulating the outcome of criteria expression evaluation.
// The condition uses the shared Reason constants defined in conditions.go for consistent signaling across CRDs.
func NewCriteriaCondition(conditionType string, generation int64, matched bool, expression string, evaluationErr error, clusterLabels map[string]string) metav1.Condition {
	labelSummary := FormatClusterLabels(clusterLabels)

	condition := metav1.Condition{
		Type:               conditionType,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}

	switch {
	case expression == "":
		condition.Status = metav1.ConditionTrue
		condition.Reason = ReasonCriteriaMatched
		condition.Message = "No criteria expression configured; defaulting to match"
	case evaluationErr != nil:
		condition.Status = metav1.ConditionFalse
		condition.Reason = ReasonCriteriaExpressionError
		condition.Message = fmt.Sprintf("Criteria expression %q failed: %v (cluster labels: %s)", expression, evaluationErr, labelSummary)
	case matched:
		condition.Status = metav1.ConditionTrue
		condition.Reason = ReasonCriteriaMatched
		condition.Message = fmt.Sprintf("Criteria expression %q matched cluster labels: %s", expression, labelSummary)
	default:
		condition.Status = metav1.ConditionFalse
		condition.Reason = ReasonCriteriaNotMatched
		condition.Message = fmt.Sprintf("Criteria expression %q evaluated to false for cluster labels: %s", expression, labelSummary)
	}

	return condition
}

// NewOwnerCondition constructs a metav1.Condition for owner-type signaling (execution success/failure) with sensible defaults.
func NewOwnerCondition(conditionType string, generation int64, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Message:            message,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}

	if reason != "" {
		condition.Reason = reason
	} else {
		condition.Reason = defaultOwnerReason(status)
	}

	return condition
}

// SetOwnerCondition applies an owner-style condition to the provided resource using the supplied parameters.
func SetOwnerCondition(resource ConditionedResource, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := NewOwnerCondition(conditionType, resource.GetGeneration(), status, reason, message)
	resource.SetCondition(condition)
}

func defaultOwnerReason(status metav1.ConditionStatus) string {
	switch status {
	case metav1.ConditionTrue:
		return ConditionCompleted
	case metav1.ConditionFalse:
		return ConditionFailed
	default:
		return ReasonProgressing
	}
}
