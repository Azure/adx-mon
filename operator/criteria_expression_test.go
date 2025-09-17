package operator

import (
	"testing"

	"github.com/Azure/adx-mon/pkg/celutil"
)

// Test that resources with empty CriteriaExpression reconcile (helper returns empty map currently)
func TestADXClusterCriteriaExpression(t *testing.T) {
	// Empty expression => true
	ok, err := celutil.EvaluateCriteriaExpression(map[string]string{"region": "eastus"}, "")
	if err != nil || !ok {
		t.Fatalf("empty expression should be true, got ok=%v err=%v", ok, err)
	}

	// Simple true
	ok, err = celutil.EvaluateCriteriaExpression(map[string]string{"region": "eastus"}, "region == 'eastus'")
	if err != nil || !ok {
		t.Fatalf("expected true expression ok, got ok=%v err=%v", ok, err)
	}

	// Simple false
	ok, err = celutil.EvaluateCriteriaExpression(map[string]string{"region": "westus"}, "region == 'eastus'")
	if err != nil || ok {
		t.Fatalf("expected false expression ok=false, got ok=%v err=%v", ok, err)
	}
}
