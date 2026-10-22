package summaryrule

import (
	"context"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clocktesting "k8s.io/utils/clock/testing"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func makeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func TestIsOwnedBy_DefaultsToIngestor(t *testing.T) {
	sr := &v1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr"}}
	if !IsOwnedBy(sr, v1.SummaryRuleOwnerIngestor) {
		t.Fatalf("expected default ownership to be ingestor")
	}
	if IsOwnedBy(sr, v1.SummaryRuleOwnerADXExporter) {
		t.Fatalf("did not expect exporter to own by default")
	}
}

func TestIsOwnedBy_UnknownOwner(t *testing.T) {
	sr := &v1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr", Annotations: map[string]string{v1.SummaryRuleOwnerAnnotation: "someone-else"}}}
	if IsOwnedBy(sr, v1.SummaryRuleOwnerIngestor) {
		t.Fatalf("expected false for unknown owner")
	}
	if IsOwnedBy(sr, v1.SummaryRuleOwnerADXExporter) {
		t.Fatalf("expected false for unknown owner")
	}
}

func TestWantsOwner(t *testing.T) {
	sr := &v1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr", Annotations: map[string]string{v1.SummaryRuleDesiredOwnerAnnotation: v1.SummaryRuleOwnerADXExporter}}}
	if !WantsOwner(sr, v1.SummaryRuleOwnerADXExporter) {
		t.Fatalf("expected WantsOwner true for exporter")
	}
	if WantsOwner(sr, v1.SummaryRuleOwnerIngestor) {
		t.Fatalf("expected WantsOwner false for ingestor")
	}
}

func TestSafeToAdopt(t *testing.T) {
	now := time.Date(2025, 8, 13, 12, 0, 0, 0, time.UTC)
	clk := clocktesting.NewFakeClock(now)

	// Case: no ops, not due => safe
	sr := &v1.SummaryRule{Spec: v1.SummaryRuleSpec{Interval: metav1.Duration{Duration: time.Hour}}}
	// Make it "not due": set last execution to 30m ago (< interval)
	sr.SetLastExecutionTime(now.Add(-30 * time.Minute))
	if ok, _ := SafeToAdopt(sr, clk); !ok {
		t.Fatalf("expected safe to adopt when no ops and not due")
	}

	// Case: has async ops => not safe
	sr2 := &v1.SummaryRule{Spec: v1.SummaryRuleSpec{Interval: metav1.Duration{Duration: time.Hour}}}
	sr2.SetAsyncOperation(v1.AsyncOperation{OperationId: "123", StartTime: now.Add(-2 * time.Hour).Format(time.RFC3339Nano), EndTime: now.Add(-time.Hour).Format(time.RFC3339Nano)})
	if ok, _ := SafeToAdopt(sr2, clk); ok {
		t.Fatalf("expected not safe to adopt when ops in progress")
	}

	// Case: due to submit => not safe
	sr3 := &v1.SummaryRule{Spec: v1.SummaryRuleSpec{Interval: metav1.Duration{Duration: time.Hour}}}
	// Force ShouldSubmitRule true by setting last successful time far in the past
	sr3.SetLastExecutionTime(now.Add(-2 * time.Hour))
	if ok, _ := SafeToAdopt(sr3, clk); ok {
		t.Fatalf("expected not safe when within submission window")
	}
}

func TestClaimInMemory(t *testing.T) {
	sr := &v1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr"}}
	ClaimInMemory(sr, v1.SummaryRuleOwnerADXExporter)
	if sr.Annotations[v1.SummaryRuleOwnerAnnotation] != v1.SummaryRuleOwnerADXExporter {
		t.Fatalf("expected owner annotation to be set to exporter")
	}
	if _, ok := sr.Annotations[v1.SummaryRuleDesiredOwnerAnnotation]; ok {
		t.Fatalf("expected desired-owner to be cleared")
	}
}

func TestPatchClaim(t *testing.T) {
	scheme := makeScheme(t)
	sr := &v1.SummaryRule{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "sr", Annotations: map[string]string{}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sr).Build()

	if err := PatchClaim(context.Background(), c, sr, v1.SummaryRuleOwnerADXExporter); err != nil {
		t.Fatalf("patch claim failed: %v", err)
	}

	// Re-fetch to ensure server-side state updated
	got := &v1.SummaryRule{}
	if err := c.Get(context.Background(), ctrlclient.ObjectKeyFromObject(sr), got); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Annotations[v1.SummaryRuleOwnerAnnotation] != v1.SummaryRuleOwnerADXExporter {
		t.Fatalf("expected owner to be exporter after patch")
	}
}
