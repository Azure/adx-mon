package v1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSummaryRuleDurationValidation(t *testing.T) {
	tests := []struct {
		name           string
		interval       string
		ingestionDelay *string
		expectValid    bool
	}{
		{
			name:        "valid durations",
			interval:    "5m",
			ingestionDelay: func() *string { s := "30s"; return &s }(),
			expectValid: true,
		},
		{
			name:        "valid complex duration",
			interval:    "1h30m45s",
			ingestionDelay: func() *string { s := "2m"; return &s }(),
			expectValid: true,
		},
		{
			name:        "valid decimal duration",
			interval:    "1.5h",
			ingestionDelay: func() *string { s := ".5h"; return &s }(),
			expectValid: true,
		},
		{
			name:        "invalid interval",
			interval:    "something-that-is-not-a-valid-go-duration",
			ingestionDelay: func() *string { s := "30s"; return &s }(),
			expectValid: false,
		},
		{
			name:        "invalid ingestion delay",
			interval:    "5m",
			ingestionDelay: func() *string { s := "invalid-duration"; return &s }(),
			expectValid: false,
		},
		{
			name:        "no time unit",
			interval:    "5",
			ingestionDelay: nil,
			expectValid: false,
		},
		{
			name:        "empty duration",
			interval:    "",
			ingestionDelay: nil,
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test parsing interval duration
			intervalDuration, err := time.ParseDuration(tt.interval)
			intervalValid := err == nil
			
			// Test parsing ingestion delay duration if provided
			ingestionDelayValid := true
			if tt.ingestionDelay != nil {
				_, err := time.ParseDuration(*tt.ingestionDelay)
				ingestionDelayValid = err == nil
			}
			
			overallValid := intervalValid && ingestionDelayValid
			
			if overallValid != tt.expectValid {
				t.Errorf("Expected valid=%v, got valid=%v (interval valid=%v, ingestionDelay valid=%v)", 
					tt.expectValid, overallValid, intervalValid, ingestionDelayValid)
			}
			
			// If valid, test that we can create a SummaryRule with these durations
			if tt.expectValid {
				var ingestionDelay *metav1.Duration
				if tt.ingestionDelay != nil {
					ingestionDelayDuration, _ := time.ParseDuration(*tt.ingestionDelay)
					ingestionDelay = &metav1.Duration{Duration: ingestionDelayDuration}
				}
				
				rule := &SummaryRule{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rule",
						Namespace: "default",
					},
					Spec: SummaryRuleSpec{
						Database:       "test-db",
						Table:         "test-table",
						Body:          "TestTable | summarize count()",
						Interval:      metav1.Duration{Duration: intervalDuration},
						IngestionDelay: ingestionDelay,
					},
				}
				
				// Verify the rule can compute execution windows without panicking
				startTime, endTime := rule.NextExecutionWindow(nil)
				if startTime.IsZero() || endTime.IsZero() {
					t.Errorf("NextExecutionWindow returned zero times")
				}
			}
		})
	}
}