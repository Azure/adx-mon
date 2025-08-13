package summaryrule

import (
	"context"

	v1 "github.com/Azure/adx-mon/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsOwnedBy returns true if the SummaryRule is owned by the given owner value.
// Semantics:
// - Missing/empty annotation defaults to ingestor ownership for backward compatibility.
// - Unknown values return false (fail-closed; neither component processes).
func IsOwnedBy(sr *v1.SummaryRule, owner string) bool {
	if sr == nil {
		return false
	}
	ann := sr.GetAnnotations()
	if ann == nil {
		// Default to ingestor
		return owner == v1.SummaryRuleOwnerIngestor
	}
	val := ann[v1.SummaryRuleOwnerAnnotation]
	switch val {
	case "":
		// Default to ingestor when not set
		return owner == v1.SummaryRuleOwnerIngestor
	case v1.SummaryRuleOwnerIngestor, v1.SummaryRuleOwnerADXExporter:
		return val == owner
	default:
		// Unknown value: fail closed
		return false
	}
}

// WantsOwner returns true if the desired-owner annotation matches the owner value.
func WantsOwner(sr *v1.SummaryRule, owner string) bool {
	if sr == nil {
		return false
	}
	ann := sr.GetAnnotations()
	if ann == nil {
		return false
	}
	return ann[v1.SummaryRuleDesiredOwnerAnnotation] == owner
}

// SafeToAdopt returns true when it's safe to adopt ownership:
// - no inflight async operations, and
// - not currently due to submit work according to ShouldSubmitRule.
// It returns an optional reason for logging/debugging.
func SafeToAdopt(sr *v1.SummaryRule, clk clock.Clock) (bool, string) {
	if sr == nil {
		return false, "nil SummaryRule"
	}
	if ops := sr.GetAsyncOperations(); len(ops) > 0 {
		return false, "async operations in progress"
	}
	if sr.ShouldSubmitRule(clk) {
		return false, "within submission window"
	}
	return true, ""
}

// ClaimInMemory sets owner annotation and clears desired-owner on the object instance.
// This only mutates the in-memory object; caller must patch/update to persist.
func ClaimInMemory(sr *v1.SummaryRule, owner string) {
	if sr.Annotations == nil {
		sr.Annotations = map[string]string{}
	}
	sr.Annotations[v1.SummaryRuleOwnerAnnotation] = owner
	delete(sr.Annotations, v1.SummaryRuleDesiredOwnerAnnotation)
}

// PatchClaim patches the object on the API server to claim ownership and clear desired-owner.
// It uses a MergeFrom patch to avoid clobbering other fields.
func PatchClaim(ctx context.Context, c client.Client, sr *v1.SummaryRule, owner string) error {
	if sr == nil {
		return nil
	}
	// Create a copy for the patch base
	base := sr.DeepCopy()
	if sr.Annotations == nil {
		sr.Annotations = map[string]string{}
	}
	sr.Annotations[v1.SummaryRuleOwnerAnnotation] = owner
	delete(sr.Annotations, v1.SummaryRuleDesiredOwnerAnnotation)
	return c.Patch(ctx, sr, client.MergeFrom(base))
}

// SetStatusOwnerCondition is a small helper to set a status condition for owner info, if desired.
// Kept here to avoid duplicating reason typing across components. Optional to use.
func SetStatusOwnerCondition(sr *v1.SummaryRule, status metav1.ConditionStatus, reason, msg string) {
	if sr == nil {
		return
	}
	sr.SetCondition(metav1.Condition{
		Status:  status,
		Reason:  reason,
		Message: msg,
	})
}
