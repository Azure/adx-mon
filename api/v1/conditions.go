package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Shared condition type constants (mirroring LogFlowVerifier semantics where applicable) to ensure
// consistent signaling across SummaryRule, MetricsExporter, and Verifier style controllers.
// These complement the per-CRD owner conditions (e.g., SummaryRuleOwner, MetricsExporterOwner) and
// are optional; controllers set them when implementing richer gating / lifecycle semantics.
const (
	// ConditionCriteria reflects evaluation of Spec.Criteria / Spec.CriteriaExpression gating.
	// Status=True => execution permitted (criteria matched); False => execution skipped.
	ConditionCriteria = "Criteria"
	// ConditionCompleted indicates the controller has finished processing successfully (terminal success).
	ConditionCompleted = "Completed"
	// ConditionFailed indicates terminal failure (will not retry automatically without spec change or manual intervention).
	ConditionFailed = "Failed"
)

// Standardized reasons for shared conditions (subset reused from verifier conditions file to avoid duplication).
// Controllers may introduce additional reasons; these are the common baseline.
const (
	ReasonCriteriaNotMatched      = "CriteriaNotMatched"
	ReasonCriteriaExpressionError = "CriteriaExpressionError"
	ReasonCriteriaMatched         = "CriteriaMatched"
)

// ConditionedObject defines interface for objects that can have conditions
// +k8s:deepcopy-gen=false
type ConditionedObject interface {
	GetCondition() *metav1.Condition
	SetCondition(condition metav1.Condition)
}
