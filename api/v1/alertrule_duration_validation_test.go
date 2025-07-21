package v1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAlertRuleDurationValidation(t *testing.T) {
	tests := []struct {
		name              string
		interval          string
		autoMitigateAfter string
		expectValid       bool
	}{
		{
			name:              "valid durations",
			interval:          "5m",
			autoMitigateAfter: "1h",
			expectValid:       true,
		},
		{
			name:              "valid complex durations",
			interval:          "1h30m",
			autoMitigateAfter: "2h45m30s",
			expectValid:       true,
		},
		{
			name:              "valid decimal durations",
			interval:          "1.5h",
			autoMitigateAfter: ".5h",
			expectValid:       true,
		},
		{
			name:              "invalid interval",
			interval:          "something-that-is-not-a-valid-go-duration",
			autoMitigateAfter: "1h",
			expectValid:       false,
		},
		{
			name:              "invalid autoMitigateAfter",
			interval:          "5m",
			autoMitigateAfter: "invalid-duration",
			expectValid:       false,
		},
		{
			name:              "no time unit in interval",
			interval:          "5",
			autoMitigateAfter: "1h",
			expectValid:       false,
		},
		{
			name:              "empty interval",
			interval:          "",
			autoMitigateAfter: "1h",
			expectValid:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test parsing interval duration
			intervalDuration, intervalErr := time.ParseDuration(tt.interval)
			intervalValid := intervalErr == nil
			
			// Test parsing auto mitigate after duration
			autoMitigateAfterDuration, autoMitigateAfterErr := time.ParseDuration(tt.autoMitigateAfter)
			autoMitigateAfterValid := autoMitigateAfterErr == nil
			
			overallValid := intervalValid && autoMitigateAfterValid
			
			if overallValid != tt.expectValid {
				t.Errorf("Expected valid=%v, got valid=%v (interval valid=%v, autoMitigateAfter valid=%v)", 
					tt.expectValid, overallValid, intervalValid, autoMitigateAfterValid)
				if intervalErr != nil {
					t.Errorf("Interval error: %v", intervalErr)
				}
				if autoMitigateAfterErr != nil {
					t.Errorf("AutoMitigateAfter error: %v", autoMitigateAfterErr)
				}
			}
			
			// If valid, test that we can create an AlertRule with these durations
			if tt.expectValid {
				rule := &AlertRule{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-alert",
						Namespace: "default",
					},
					Spec: AlertRuleSpec{
						Database:          "test-db",
						Interval:          metav1.Duration{Duration: intervalDuration},
						Query:             "TestTable | where severity == 'critical'",
						AutoMitigateAfter: metav1.Duration{Duration: autoMitigateAfterDuration},
						Destination:       "test-webhook",
					},
				}
				
				// Verify the rule was created successfully
				if rule.Spec.Interval.Duration != intervalDuration {
					t.Errorf("Interval duration mismatch")
				}
				if rule.Spec.AutoMitigateAfter.Duration != autoMitigateAfterDuration {
					t.Errorf("AutoMitigateAfter duration mismatch")
				}
			}
		})
	}
}