package v1

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestSummaryRulesSpecFromYAML(t *testing.T) {
	yamlStr := `apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: HourlyAvg
  namespace: adx-mon
spec:
  database: Metrics
  body: |
    SomeMetric
    | where Timestamp between (_startTime .. _endtime)
    | summarize avg(Value) by bin(Timestamp, 1h)
  table: SomeMetricHourlyAvg
  interval: 1h`

	var sr SummaryRule
	err := yaml.Unmarshal([]byte(yamlStr), &sr)
	require.NoError(t, err)
	require.Equal(t, "Metrics", sr.Spec.Database)
	require.Equal(t, "HourlyAvg", sr.GetName())
	require.Equal(t, "adx-mon", sr.GetNamespace())
	require.Equal(t, "SomeMetricHourlyAvg", sr.Spec.Table)
	require.Equal(t, metav1.Duration{Duration: time.Hour}, sr.Spec.Interval)
	require.Empty(t, sr.Spec.Criteria) // No criteria specified
}

func TestSummaryRulesSpecFromYAMLWithCriteria(t *testing.T) {
	yamlStr := `apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: HourlyAvg
  namespace: adx-mon
spec:
  database: Metrics
  body: |
    SomeMetric
    | where Timestamp between (_startTime .. _endtime)
    | summarize avg(Value) by bin(Timestamp, 1h)
  table: SomeMetricHourlyAvg
  interval: 1h
  criteria:
    region:
      - eastus
      - westus
    environment:
      - production`

	var sr SummaryRule
	err := yaml.Unmarshal([]byte(yamlStr), &sr)
	require.NoError(t, err)
	require.Equal(t, "Metrics", sr.Spec.Database)
	require.Equal(t, "HourlyAvg", sr.GetName())
	require.Equal(t, "adx-mon", sr.GetNamespace())
	require.Equal(t, "SomeMetricHourlyAvg", sr.Spec.Table)
	require.Equal(t, metav1.Duration{Duration: time.Hour}, sr.Spec.Interval)

	// Check criteria
	require.NotEmpty(t, sr.Spec.Criteria)
	require.Contains(t, sr.Spec.Criteria, "region")
	require.Contains(t, sr.Spec.Criteria, "environment")
	require.Equal(t, []string{"eastus", "westus"}, sr.Spec.Criteria["region"])
	require.Equal(t, []string{"production"}, sr.Spec.Criteria["environment"])
}

func TestAsyncOperations(t *testing.T) {
	var sr SummaryRule

	// We expect the initial AsyncOperation to be empty
	ops := sr.GetAsyncOperations()
	require.Equal(t, 0, len(ops))

	// Create an AsyncOperation, get it, and validate its contents
	sr.SetAsyncOperation(AsyncOperation{OperationId: "a"})
	ops = sr.GetAsyncOperations()
	require.Equal(t, 1, len(ops))
	require.Equal(t, "a", ops[0].OperationId)
	c := meta.FindStatusCondition(sr.Status.Conditions, SummaryRuleOperationIdOwner)
	require.NotNil(t, c)
	require.Equal(t, metav1.ConditionUnknown, c.Status)
	require.Equal(t, SummaryRuleOperationIdOwner, c.Type)
	require.NotZero(t, c.LastTransitionTime)

	// Create a second AsyncOperation, get it, and validate its contents
	sr.SetAsyncOperation(AsyncOperation{OperationId: "b"})
	ops = sr.GetAsyncOperations()
	require.Equal(t, 2, len(ops))
	require.Equal(t, "b", ops[1].OperationId)
	c = meta.FindStatusCondition(sr.Status.Conditions, SummaryRuleOperationIdOwner)
	require.NotNil(t, c)
	require.Equal(t, metav1.ConditionUnknown, c.Status)
	require.Equal(t, SummaryRuleOperationIdOwner, c.Type)
	require.NotZero(t, c.LastTransitionTime)

	// Remove the first AsyncOperation, get it, and validate its contents
	sr.RemoveAsyncOperation("a")
	ops = sr.GetAsyncOperations()
	require.Equal(t, 1, len(ops))
	require.Equal(t, "b", ops[0].OperationId)
	c = meta.FindStatusCondition(sr.Status.Conditions, SummaryRuleOperationIdOwner)
	require.NotNil(t, c)
	require.Equal(t, metav1.ConditionUnknown, c.Status)
	require.Equal(t, SummaryRuleOperationIdOwner, c.Type)
	require.NotZero(t, c.LastTransitionTime)

	// Remove the second AsyncOperation, get it, and validate its contents
	sr.RemoveAsyncOperation("b")
	ops = sr.GetAsyncOperations()
	require.Equal(t, 0, len(ops))
	c = meta.FindStatusCondition(sr.Status.Conditions, SummaryRuleOperationIdOwner)
	require.NotNil(t, c)
	require.Equal(t, metav1.ConditionTrue, c.Status)
	require.Equal(t, SummaryRuleOperationIdOwner, c.Type)
	require.NotZero(t, c.LastTransitionTime)

	// Create a thousand AsyncOperations, get them, ensure the array is of size 200, which is our max
	for i := 0; i < 1000; i++ {
		sr.SetAsyncOperation(AsyncOperation{OperationId: strconv.Itoa(i)})
	}
	ops = sr.GetAsyncOperations()
	require.Equal(t, 200, len(ops))
}

func TestBacklog(t *testing.T) {
	var sr SummaryRule

	// We expect the initial AsyncOperation to be empty
	ops := sr.GetAsyncOperations()
	require.Equal(t, 0, len(ops))

	// Create a backlog AsyncOperation, which means it has no operation-id
	sr.SetAsyncOperation(AsyncOperation{StartTime: "2025-05-22T19:20:00Z", EndTime: "2025-05-22T19:30:00Z"})
	ops = sr.GetAsyncOperations()
	require.Equal(t, 1, len(ops))

	// Add another backlog
	sr.SetAsyncOperation(AsyncOperation{StartTime: "2025-05-22T19:40:00Z", EndTime: "2025-05-22T19:50:00Z"})
	ops = sr.GetAsyncOperations()
	require.Equal(t, 2, len(ops))

	// Now simulate the operation was able to be submitted, so the operation-id is now set
	sr.SetAsyncOperation(AsyncOperation{OperationId: "1", StartTime: "2025-05-22T19:20:00Z", EndTime: "2025-05-22T19:30:00Z"})
	ops = sr.GetAsyncOperations()
	require.Equal(t, 2, len(ops)) // should just update the existing operation
	for _, op := range ops {
		if op.StartTime == "2025-05-22T19:20:00Z" && op.EndTime == "2025-05-22T19:30:00Z" {
			require.Equal(t, "1", op.OperationId) // should have the operation-id set now
		}
	}
}

func TestShouldSubmitRule(t *testing.T) {
	// Use current time but truncate to seconds for deterministic tests
	now := time.Now().Truncate(time.Second)

	t.Run("should submit on first execution - no condition", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// No condition set, should submit for first execution
		require.True(t, rule.ShouldSubmitRule())
	})

	t.Run("should submit on first execution - condition with old timestamp", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set a condition with old timestamp (more than interval ago)
		condition := metav1.Condition{
			Type:               SummaryRuleOwner,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: now.Add(-2 * time.Hour)},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		require.True(t, rule.ShouldSubmitRule())
	})

	t.Run("should not submit - recent condition, no last execution time", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set a condition with recent timestamp (less than interval ago)
		condition := metav1.Condition{
			Type:               SummaryRuleOwner,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: now.Add(-30 * time.Minute)},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		require.False(t, rule.ShouldSubmitRule())
	})

	t.Run("should submit when interval has elapsed since last execution", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set condition
		condition := metav1.Condition{
			Type:               SummaryRuleOwner,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: now},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set last execution time to more than interval ago
		lastExecution := now.Add(-2 * time.Hour)
		rule.SetLastExecutionTime(lastExecution)

		require.True(t, rule.ShouldSubmitRule())
	})

	t.Run("should not submit when interval has not elapsed since last execution", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set condition
		condition := metav1.Condition{
			Type:               SummaryRuleOwner,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: now},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set last execution time to less than interval ago
		lastExecution := now.Add(-30 * time.Minute)
		rule.SetLastExecutionTime(lastExecution)

		require.False(t, rule.ShouldSubmitRule())
	})

	t.Run("should submit when rule has been updated (new generation)", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1, // Start with generation 1
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set condition with generation 1
		condition := metav1.Condition{
			Type:               SummaryRuleOwner,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: now},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set recent last execution time
		lastExecution := now.Add(-10 * time.Minute)
		rule.SetLastExecutionTime(lastExecution)

		// Now update the generation to simulate rule update
		rule.ObjectMeta.Generation = 2

		require.True(t, rule.ShouldSubmitRule())
	})

	t.Run("should submit when rule is being deleted", func(t *testing.T) {
		deletionTime := metav1.Time{Time: now}
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation:        1,
				DeletionTimestamp: &deletionTime,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set condition
		condition := metav1.Condition{
			Type:               SummaryRuleOwner,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: now},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set recent last execution time
		lastExecution := now.Add(-10 * time.Minute)
		rule.SetLastExecutionTime(lastExecution)

		require.True(t, rule.ShouldSubmitRule())
	})

	t.Run("should handle different interval durations", func(t *testing.T) {
		testCases := []struct {
			name           string
			interval       time.Duration
			lastExecution  time.Time
			expectedSubmit bool
		}{
			{
				name:           "5 minute interval - should submit",
				interval:       5 * time.Minute,
				lastExecution:  now.Add(-6 * time.Minute),
				expectedSubmit: true,
			},
			{
				name:           "5 minute interval - should not submit",
				interval:       5 * time.Minute,
				lastExecution:  now.Add(-3 * time.Minute),
				expectedSubmit: false,
			},
			{
				name:           "24 hour interval - should submit",
				interval:       24 * time.Hour,
				lastExecution:  now.Add(-25 * time.Hour),
				expectedSubmit: true,
			},
			{
				name:           "24 hour interval - should not submit",
				interval:       24 * time.Hour,
				lastExecution:  now.Add(-23 * time.Hour),
				expectedSubmit: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				rule := &SummaryRule{
					ObjectMeta: metav1.ObjectMeta{
						Generation: 1,
					},
					Spec: SummaryRuleSpec{
						Interval: metav1.Duration{Duration: tc.interval},
					},
				}

				// Set condition
				condition := metav1.Condition{
					Type:               SummaryRuleOwner,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: now},
					ObservedGeneration: 1,
				}
				rule.SetCondition(condition)

				// Set last execution time
				rule.SetLastExecutionTime(tc.lastExecution)

				require.Equal(t, tc.expectedSubmit, rule.ShouldSubmitRule())
			})
		}
	})

	t.Run("edge case - exactly at interval boundary", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set condition
		condition := metav1.Condition{
			Type:               SummaryRuleOwner,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: now},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set last execution time to exactly interval ago
		lastExecution := now.Add(-time.Hour)
		rule.SetLastExecutionTime(lastExecution)

		require.True(t, rule.ShouldSubmitRule())
	})

	t.Run("multiple conditions should prioritize execution triggers", func(t *testing.T) {
		deletionTime := metav1.Time{Time: now}
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation:        1,             // Start with generation 1
				DeletionTimestamp: &deletionTime, // Being deleted
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set condition with generation 1
		condition := metav1.Condition{
			Type:               SummaryRuleOwner,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: now},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set very recent last execution time (normally would prevent submission)
		lastExecution := now.Add(-1 * time.Minute)
		rule.SetLastExecutionTime(lastExecution)

		// Update generation to simulate rule update
		rule.ObjectMeta.Generation = 2

		// Should still submit because of deletion and generation change
		require.True(t, rule.ShouldSubmitRule())
	})
}

func TestNextExecutionWindow(t *testing.T) {
	// Use current time but truncate to seconds for deterministic tests
	now := time.Now().Truncate(time.Second)

	t.Run("first execution - no last execution time", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		startTime, endTime := rule.NextExecutionWindow()

		// First execution should go back one interval from current time (aligned to minute)
		expectedEndTime := now.UTC().Truncate(time.Minute)
		expectedStartTime := expectedEndTime.Add(-time.Hour)

		// Allow for small time differences due to execution time
		require.WithinDuration(t, expectedStartTime, startTime, 2*time.Minute)
		require.WithinDuration(t, expectedEndTime, endTime, 2*time.Minute)
		require.Equal(t, time.Hour, endTime.Sub(startTime))
	})

	t.Run("subsequent execution - continues from last execution", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set a last execution time that's far enough in the past that adding interval won't exceed current time
		lastExecution := now.Add(-2 * time.Hour).UTC()
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime := rule.NextExecutionWindow()

		// Should start from last execution and go forward one interval
		require.Equal(t, lastExecution, startTime)
		require.Equal(t, lastExecution.Add(time.Hour), endTime)
		require.Equal(t, time.Hour, endTime.Sub(startTime))
	})

	t.Run("prevents future execution windows", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set a last execution time that would create a future window
		futureStart := now.Add(30 * time.Minute).UTC()
		rule.SetLastExecutionTime(futureStart)

		startTime, endTime := rule.NextExecutionWindow()

		// Should start from the future time but cap end time to current time
		require.Equal(t, futureStart, startTime)
		currentTimeMinute := now.UTC().Truncate(time.Minute)

		// End time should be capped to current time since start time is in the future
		require.True(t, endTime.Before(currentTimeMinute.Add(2*time.Minute)) || endTime.Equal(currentTimeMinute))
		// When start time is in the future, end time might be equal to start time if both are capped
		require.True(t, endTime.After(startTime) || endTime.Equal(startTime) || endTime.Equal(currentTimeMinute))
	})

	t.Run("handles different interval durations", func(t *testing.T) {
		testCases := []struct {
			name     string
			interval time.Duration
		}{
			{"5 minutes", 5 * time.Minute},
			{"15 minutes", 15 * time.Minute},
			{"1 hour", time.Hour},
			{"6 hours", 6 * time.Hour},
			{"24 hours", 24 * time.Hour},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				rule := &SummaryRule{
					ObjectMeta: metav1.ObjectMeta{
						Generation: 1,
					},
					Spec: SummaryRuleSpec{
						Interval: metav1.Duration{Duration: tc.interval},
					},
				}

				// Test first execution
				startTime, endTime := rule.NextExecutionWindow()
				require.Equal(t, tc.interval, endTime.Sub(startTime))

				// Test subsequent execution - use a time far enough back
				lastExecution := now.Add(-tc.interval - time.Hour).UTC()
				rule.SetLastExecutionTime(lastExecution)

				startTime, endTime = rule.NextExecutionWindow()
				require.Equal(t, lastExecution, startTime)
				require.Equal(t, tc.interval, endTime.Sub(startTime))
			})
		}
	})

	t.Run("time alignment to minute boundary", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		startTime, endTime := rule.NextExecutionWindow()

		// End time should be aligned to minute boundary (no seconds/nanoseconds)
		require.Zero(t, endTime.Second())
		require.Zero(t, endTime.Nanosecond())

		// Start time should also be aligned (since it's end time minus interval)
		require.Zero(t, startTime.Second())
		require.Zero(t, startTime.Nanosecond())
	})

	t.Run("UTC timezone consistency", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		startTime, endTime := rule.NextExecutionWindow()

		// Both times should be in UTC
		require.Equal(t, time.UTC, startTime.Location())
		require.Equal(t, time.UTC, endTime.Location())
	})
	t.Run("sequential execution windows", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Start with an execution time far enough in the past
		pastTime := now.Add(-4 * time.Hour).UTC()
		rule.SetLastExecutionTime(pastTime)

		// First execution window
		startTime1, endTime1 := rule.NextExecutionWindow()
		require.Equal(t, pastTime, startTime1)
		require.Equal(t, time.Hour, endTime1.Sub(startTime1))

		// Simulate completing the first execution
		rule.SetLastExecutionTime(endTime1)

		// Second execution
		startTime2, endTime2 := rule.NextExecutionWindow()

		// Second execution should start where first ended
		require.Equal(t, endTime1, startTime2)
		require.Equal(t, time.Hour, endTime2.Sub(startTime2))

		// Simulate completing the second execution
		rule.SetLastExecutionTime(endTime2)

		// Third execution - this should still be in the past
		startTime3, endTime3 := rule.NextExecutionWindow()

		// Third execution should start where second ended
		require.Equal(t, endTime2, startTime3)
		require.Equal(t, time.Hour, endTime3.Sub(startTime3))
	})

	t.Run("handles very short intervals", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: 30 * time.Second},
			},
		}

		startTime, endTime := rule.NextExecutionWindow()
		require.Equal(t, 30*time.Second, endTime.Sub(startTime))

		// Set last execution time far enough back
		lastExecution := now.Add(-2 * time.Minute).UTC()
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime = rule.NextExecutionWindow()
		require.Equal(t, lastExecution, startTime)
		require.Equal(t, 30*time.Second, endTime.Sub(startTime))
	})

	t.Run("handles very long intervals", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: 7 * 24 * time.Hour}, // 1 week
			},
		}

		startTime, endTime := rule.NextExecutionWindow()
		require.Equal(t, 7*24*time.Hour, endTime.Sub(startTime))

		// Set last execution time far enough back
		lastExecution := now.Add(-8 * 24 * time.Hour).UTC() // 8 days ago
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime = rule.NextExecutionWindow()
		require.Equal(t, lastExecution, startTime)
		require.Equal(t, 7*24*time.Hour, endTime.Sub(startTime))
	})

	t.Run("edge case - last execution exactly at current time", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		// Set last execution to current time
		currentTime := now.UTC().Truncate(time.Minute)
		rule.SetLastExecutionTime(currentTime)

		startTime, endTime := rule.NextExecutionWindow()

		require.Equal(t, currentTime, startTime)
		// End time should be capped to current time since it would be in the future
		require.True(t, endTime.Equal(currentTime) || endTime.Before(now.Add(2*time.Minute)))
	})

	t.Run("window calculation with past execution that would exceed current time", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: 2 * time.Hour},
			},
		}

		// Set last execution time that when adding interval would exceed current time
		lastExecution := now.Add(-30 * time.Minute).UTC() // 30 minutes ago
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime := rule.NextExecutionWindow()

		require.Equal(t, lastExecution, startTime)
		// End time should be capped to current time (truncated to minute)
		expectedMaxEndTime := now.UTC().Truncate(time.Minute)
		require.True(t, endTime.Before(expectedMaxEndTime.Add(2*time.Minute)) || endTime.Equal(expectedMaxEndTime))
		require.True(t, endTime.After(startTime))
	})
}
