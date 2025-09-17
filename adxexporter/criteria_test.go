package adxexporter

import (
	"testing"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
)

// TestEvaluateExecutionCriteria covers the semantics described in EvaluateExecutionCriteria documentation.
func TestEvaluateExecutionCriteria(t *testing.T) {
	clusterLabels := map[string]string{"region": "eastus", "environment": "prod", "cloud": "public"}
	// Valid reasons set (from api/v1/conditions.go) for conformance
	validReasons := map[string]struct{}{
		adxmonv1.ReasonCriteriaMatched:         {},
		adxmonv1.ReasonCriteriaNotMatched:      {},
		adxmonv1.ReasonCriteriaExpressionError: {},
	}

	tests := []struct {
		name        string
		criteria    map[string][]string
		expr        string
		labels      map[string]string
		proceed     bool
		reason      string
		msgContains string
		expectErr   bool
	}{
		{
			name:        "neither criteria nor expression => allow",
			criteria:    nil,
			expr:        "",
			labels:      clusterLabels,
			proceed:     true,
			reason:      adxmonv1.ReasonCriteriaMatched,
			msgContains: "no criteria",
		},
		{
			name:        "map only match single key",
			criteria:    map[string][]string{"region": {"eastus"}},
			expr:        "",
			labels:      clusterLabels,
			proceed:     true,
			reason:      adxmonv1.ReasonCriteriaMatched,
			msgContains: "criteria map matched",
		},
		{
			name:        "map only match OR semantics other key",
			criteria:    map[string][]string{"region": {"centralus"}, "cloud": {"public"}}, // region doesn't match, cloud does ⇒ overall match
			expr:        "",
			labels:      clusterLabels,
			proceed:     true,
			reason:      adxmonv1.ReasonCriteriaMatched,
			msgContains: "criteria map matched",
		},
		{
			name:        "map only no match",
			criteria:    map[string][]string{"region": {"westus"}},
			expr:        "",
			labels:      clusterLabels,
			proceed:     false,
			reason:      adxmonv1.ReasonCriteriaNotMatched,
			msgContains: "criteria map did not match",
		},
		{
			name:        "map only case-insensitive key and value",
			criteria:    map[string][]string{"ReGiOn": {"EasTus"}},
			expr:        "",
			labels:      map[string]string{"REGION": "EASTUS"}, // ensure value and key folding works
			proceed:     true,
			reason:      adxmonv1.ReasonCriteriaMatched,
			msgContains: "criteria map matched",
		},
		{
			name:        "expr only true",
			expr:        "region == 'eastus' && environment == 'prod'",
			labels:      clusterLabels,
			proceed:     true,
			reason:      adxmonv1.ReasonCriteriaMatched,
			msgContains: "criteria expression true",
		},
		{
			name:        "expr only false",
			expr:        "region == 'westus'",
			labels:      clusterLabels,
			proceed:     false,
			reason:      adxmonv1.ReasonCriteriaNotMatched,
			msgContains: "criteria expression evaluated to false",
		},
		{
			name:        "expr only invalid syntax",
			expr:        "region ==",
			labels:      clusterLabels,
			proceed:     false,
			reason:      adxmonv1.ReasonCriteriaExpressionError,
			msgContains: "criteria expression error",
			expectErr:   true,
		},
		{
			name:        "expr only unknown identifier",
			expr:        "region == 'eastus' && missingKey == 'x'",
			labels:      clusterLabels, // missingKey not present => CEL typecheck error
			proceed:     false,
			reason:      adxmonv1.ReasonCriteriaExpressionError,
			msgContains: "criteria expression error",
			expectErr:   true,
		},
		{
			name:        "both match",
			criteria:    map[string][]string{"region": {"eastus"}},
			expr:        "environment == 'prod'",
			labels:      clusterLabels,
			proceed:     true,
			reason:      adxmonv1.ReasonCriteriaMatched,
			msgContains: "criteria map matched and expression true",
		},
		{
			name:        "both map match expression false",
			criteria:    map[string][]string{"region": {"eastus"}},
			expr:        "environment == 'dev'",
			labels:      clusterLabels,
			proceed:     false,
			reason:      adxmonv1.ReasonCriteriaNotMatched,
			msgContains: "criteria expression evaluated to false",
		},
		{
			name:        "both map no match expression true",
			criteria:    map[string][]string{"region": {"westus"}},
			expr:        "environment == 'prod'",
			labels:      clusterLabels,
			proceed:     false,
			reason:      adxmonv1.ReasonCriteriaNotMatched,
			msgContains: "criteria map did not match",
		},
		{
			name:        "both neither match",
			criteria:    map[string][]string{"region": {"westus"}},
			expr:        "environment == 'dev'",
			labels:      clusterLabels,
			proceed:     false,
			reason:      adxmonv1.ReasonCriteriaNotMatched,
			msgContains: "criteria map did not match AND expression false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proceed, reason, msg, err := EvaluateExecutionCriteria(tt.criteria, tt.expr, tt.labels)
			if tt.expectErr {
				require.Error(t, err, "expected an error")
			} else {
				require.NoError(t, err, "unexpected error")
			}
			// Base expectations from table
			require.Equal(t, tt.proceed, proceed, "proceed mismatch")
			require.Equal(t, tt.reason, reason, "reason mismatch")
			require.Contains(t, msg, tt.msgContains, "message mismatch: %s", msg)
			// Conformance checks
			if proceed {
				require.Equal(t, adxmonv1.ReasonCriteriaMatched, reason, "proceed true must have matched reason")
			} else if reason == adxmonv1.ReasonCriteriaMatched {
				t.Fatalf("proceed false but reason is matched")
			}
			if reason == adxmonv1.ReasonCriteriaExpressionError {
				require.Error(t, err, "expected error for expression error reason")
			}
			if _, ok := validReasons[reason]; !ok {
				t.Fatalf("reason %s not in allowed set", reason)
			}
		})
	}
}
