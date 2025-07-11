package v1

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"sigs.k8s.io/yaml"
)

// FakeClock implements clock.Clock for testing
type FakeClock struct {
	time time.Time
}

func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{time: t}
}

func (f *FakeClock) Now() time.Time {
	return f.time
}

func (f *FakeClock) Since(ts time.Time) time.Duration {
	return f.time.Sub(ts)
}

func (f *FakeClock) Until(ts time.Time) time.Duration {
	return ts.Sub(f.time)
}

func (f *FakeClock) NewTimer(d time.Duration) clock.Timer {
	return clock.RealClock{}.NewTimer(d)
}

func (f *FakeClock) NewTicker(d time.Duration) clock.Ticker {
	return clock.RealClock{}.NewTicker(d)
}

func (f *FakeClock) Sleep(d time.Duration) {
	f.time = f.time.Add(d)
}

func (f *FakeClock) After(d time.Duration) <-chan time.Time {
	return clock.RealClock{}.After(d)
}

func (f *FakeClock) Tick(d time.Duration) <-chan time.Time {
	return clock.RealClock{}.Tick(d)
}

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
	// Use fixed time for deterministic tests
	fixedTime := time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC)
	fakeClock := NewFakeClock(fixedTime)

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
		require.True(t, rule.ShouldSubmitRule(fakeClock))
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
			LastTransitionTime: metav1.Time{Time: fixedTime.Add(-2 * time.Hour)},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		require.True(t, rule.ShouldSubmitRule(fakeClock))
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
			LastTransitionTime: metav1.Time{Time: fixedTime.Add(-30 * time.Minute)},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		require.False(t, rule.ShouldSubmitRule(fakeClock))
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
			LastTransitionTime: metav1.Time{Time: fixedTime},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set last execution time to more than interval ago
		lastExecution := fixedTime.Add(-2 * time.Hour)
		rule.SetLastExecutionTime(lastExecution)

		require.True(t, rule.ShouldSubmitRule(fakeClock))
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
			LastTransitionTime: metav1.Time{Time: fixedTime},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set last execution time to less than interval ago
		lastExecution := fixedTime.Add(-30 * time.Minute)
		rule.SetLastExecutionTime(lastExecution)

		require.False(t, rule.ShouldSubmitRule(fakeClock))
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
			LastTransitionTime: metav1.Time{Time: fixedTime},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set recent last execution time
		lastExecution := fixedTime.Add(-10 * time.Minute)
		rule.SetLastExecutionTime(lastExecution)

		// Now update the generation to simulate rule update
		rule.ObjectMeta.Generation = 2

		require.True(t, rule.ShouldSubmitRule(fakeClock))
	})

	t.Run("should submit when rule is being deleted", func(t *testing.T) {
		deletionTime := metav1.Time{Time: fixedTime}
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
			LastTransitionTime: metav1.Time{Time: fixedTime},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set recent last execution time
		lastExecution := fixedTime.Add(-10 * time.Minute)
		rule.SetLastExecutionTime(lastExecution)

		require.True(t, rule.ShouldSubmitRule(fakeClock))
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
				lastExecution:  fixedTime.Add(-6 * time.Minute),
				expectedSubmit: true,
			},
			{
				name:           "5 minute interval - should not submit",
				interval:       5 * time.Minute,
				lastExecution:  fixedTime.Add(-3 * time.Minute),
				expectedSubmit: false,
			},
			{
				name:           "24 hour interval - should submit",
				interval:       24 * time.Hour,
				lastExecution:  fixedTime.Add(-25 * time.Hour),
				expectedSubmit: true,
			},
			{
				name:           "24 hour interval - should not submit",
				interval:       24 * time.Hour,
				lastExecution:  fixedTime.Add(-23 * time.Hour),
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
					LastTransitionTime: metav1.Time{Time: fixedTime},
					ObservedGeneration: 1,
				}
				rule.SetCondition(condition)

				// Set last execution time
				rule.SetLastExecutionTime(tc.lastExecution)

				require.Equal(t, tc.expectedSubmit, rule.ShouldSubmitRule(fakeClock))
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
			LastTransitionTime: metav1.Time{Time: fixedTime},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set last execution time to exactly interval ago
		lastExecution := fixedTime.Add(-time.Hour)
		rule.SetLastExecutionTime(lastExecution)

		require.True(t, rule.ShouldSubmitRule(fakeClock))
	})

	t.Run("multiple conditions should prioritize execution triggers", func(t *testing.T) {
		deletionTime := metav1.Time{Time: fixedTime}
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
			LastTransitionTime: metav1.Time{Time: fixedTime},
			ObservedGeneration: 1,
		}
		rule.SetCondition(condition)

		// Set very recent last execution time (normally would prevent submission)
		lastExecution := fixedTime.Add(-1 * time.Minute)
		rule.SetLastExecutionTime(lastExecution)

		// Update generation to simulate rule update
		rule.ObjectMeta.Generation = 2

		// Should still submit because of deletion and generation change
		require.True(t, rule.ShouldSubmitRule(fakeClock))
	})
}

func TestNextExecutionWindow(t *testing.T) {
	// Use fixed time for deterministic tests
	fixedTime := time.Date(2025, 6, 23, 12, 1, 2, 3, time.UTC)
	fakeClock := NewFakeClock(fixedTime)

	t.Run("first execution - no last execution time", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
		}

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		// First execution should go back one interval from current time (aligned to interval)
		expectedEndTime := fixedTime.UTC().Truncate(rule.Spec.Interval.Duration)
		expectedStartTime := expectedEndTime.Add(-time.Hour)

		require.Equal(t, expectedStartTime, startTime)
		require.Equal(t, expectedEndTime, endTime)
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
		lastExecution := fixedTime.Add(-2 * time.Hour).UTC().Truncate(rule.Spec.Interval.Duration)
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

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
		futureStart := fixedTime.Add(3 * time.Hour).UTC()
		rule.SetLastExecutionTime(futureStart)

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		// Should start from the future time (no truncation for exact time continuity)
		require.Equal(t, futureStart, startTime)
		currentTimeMinute := fixedTime.UTC().Truncate(time.Minute)

		// End time should be capped to current time since start time is in the future
		require.Equal(t, currentTimeMinute, endTime)
	})

	t.Run("should correct a wrongly set last execution time", func(t *testing.T) {
		// fixedTime := time.Date(2025, 6, 23, 12, 1, 2, 3, time.UTC)
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Hour},
			},
			Status: SummaryRuleStatus{
				Conditions: []metav1.Condition{
					{
						Type:               SummaryRuleLastSuccessfulExecution,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Time{Time: time.Date(2025, 6, 23, 8, 20, 0, 0, time.UTC)},
						ObservedGeneration: 1,
						Message:            time.Date(2025, 6, 23, 8, 20, 0, 0, time.UTC).Format(time.RFC3339Nano),
					},
				},
			},
		}

		startTime, endTime := rule.NextExecutionWindow(fakeClock)
		// Should preserve the exact execution time for continuity (no truncation)
		expectedStartTime := time.Date(2025, 6, 23, 8, 20, 0, 0, time.UTC)
		expectedEndTime := time.Date(2025, 6, 23, 9, 20, 0, 0, time.UTC)

		require.Equal(t, expectedStartTime, startTime)
		require.Equal(t, expectedEndTime, endTime)
		require.Equal(t, time.Hour, endTime.Sub(startTime))
	})

	t.Run("handles different interval durations", func(t *testing.T) {
		// fixedTime := time.Date(2025, 6, 23, 12, 1, 2, 3, time.UTC)
		testCases := []struct {
			name          string
			interval      time.Duration
			expectedStart time.Time
			expectedEnd   time.Time
		}{
			{
				name:     "5 minutes",
				interval: 5 * time.Minute,
				// 2025-06-23 12:01:02.003 − 5 m → 11:56:02.003 → truncate to 11:55:00
				expectedStart: time.Date(2025, 6, 23, 11, 55, 0, 0, time.UTC),
				expectedEnd:   time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC),
			},
			{
				name:     "15 minutes",
				interval: 15 * time.Minute,
				// 12:01:02.003 − 15 m → 11:46:02.003 → truncate to 11:45:00
				expectedStart: time.Date(2025, 6, 23, 11, 45, 0, 0, time.UTC),
				expectedEnd:   time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC),
			},
			{
				name:     "30 minutes",
				interval: 30 * time.Minute,
				// 12:01:02.003 − 30 m → 11:31:02.003 → truncate to 11:30:00
				expectedStart: time.Date(2025, 6, 23, 11, 30, 0, 0, time.UTC),
				expectedEnd:   time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC),
			},
			{
				name:     "1 hour",
				interval: time.Hour,
				// 12:01:02.003 − 1 h → 11:01:02.003 → truncate to 11:00:00
				expectedStart: time.Date(2025, 6, 23, 11, 0, 0, 0, time.UTC),
				expectedEnd:   time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC),
			},
			{
				name:     "6 hours",
				interval: 6 * time.Hour,
				// 12:01:02.003 − 6 h → 06:01:02.003 → truncate to 06:00:00
				expectedStart: time.Date(2025, 6, 23, 6, 0, 0, 0, time.UTC),
				expectedEnd:   time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC),
			},
			{
				name:     "24 hours",
				interval: 24 * time.Hour,
				// 12:01:02.003 − 24 h → 2025-06-22 12:01:02.003 → truncate to 2025-06-22 00:00:00
				expectedStart: time.Date(2025, 6, 22, 0, 0, 0, 0, time.UTC),
				expectedEnd:   time.Date(2025, 6, 23, 0, 0, 0, 0, time.UTC),
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

				// Test first execution
				startTime, endTime := rule.NextExecutionWindow(fakeClock)
				require.Equal(t, tc.expectedStart, startTime)
				require.Equal(t, tc.expectedEnd, endTime)

				require.Equal(t, tc.interval, endTime.Sub(startTime))
				// Set last execution time to the expected start time
				rule.SetLastExecutionTime(tc.expectedStart)
				// Test subsequent execution
				startTime, endTime = rule.NextExecutionWindow(fakeClock)
				require.Equal(t, tc.expectedStart, startTime)
				require.Equal(t, tc.expectedEnd, endTime)
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

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

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

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

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
		pastTime := fixedTime.Add(-4 * time.Hour).UTC().Truncate(rule.Spec.Interval.Duration)
		rule.SetLastExecutionTime(pastTime)

		// First execution window
		startTime1, endTime1 := rule.NextExecutionWindow(fakeClock)
		require.Equal(t, pastTime, startTime1)
		require.Equal(t, time.Hour, endTime1.Sub(startTime1))

		// Simulate completing the first execution
		rule.SetLastExecutionTime(endTime1)

		// Second execution
		startTime2, endTime2 := rule.NextExecutionWindow(fakeClock)

		// Second execution should start where first ended
		require.Equal(t, endTime1, startTime2)
		require.Equal(t, time.Hour, endTime2.Sub(startTime2))

		// Simulate completing the second execution
		rule.SetLastExecutionTime(endTime2)

		// Third execution - this should still be in the past
		startTime3, endTime3 := rule.NextExecutionWindow(fakeClock)

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

		startTime, endTime := rule.NextExecutionWindow(fakeClock)
		require.Equal(t, 30*time.Second, endTime.Sub(startTime))

		// Set last execution time far enough back
		lastExecution := fixedTime.Add(-2 * time.Minute).UTC()
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime = rule.NextExecutionWindow(fakeClock)
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

		startTime, endTime := rule.NextExecutionWindow(fakeClock)
		require.Equal(t, 7*24*time.Hour, endTime.Sub(startTime))

		// Set last execution time far enough back
		lastExecution := fixedTime.Add(-8 * 24 * time.Hour).UTC() // 8 days ago
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime = rule.NextExecutionWindow(fakeClock)
		require.Equal(t, lastExecution, startTime)
		require.Equal(t, 7*24*time.Hour, endTime.Sub(startTime))
	})

	t.Run("edge case - last execution exactly at current time", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval: metav1.Duration{Duration: time.Minute},
			},
		}

		// Set last execution to current time
		currentTime := fixedTime.UTC().Truncate(time.Minute)
		rule.SetLastExecutionTime(currentTime)

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		require.Equal(t, currentTime, startTime)
		// End time should be capped to current time since it would be in the future
		require.Equal(t, currentTime, endTime)
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
		lastExecution := fixedTime.Add(-30 * time.Minute).UTC() // 30 minutes ago
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		require.Equal(t, lastExecution, startTime)
		// End time should be capped to current time (truncated to minute)
		expectedMaxEndTime := fixedTime.UTC().Truncate(time.Minute)
		require.Equal(t, expectedMaxEndTime, endTime)
		require.True(t, endTime.After(startTime))
	})
}

func TestSummaryRuleIngestionDelay(t *testing.T) {
	fixedTime := time.Date(2025, 6, 23, 12, 1, 2, 3000000, time.UTC)
	fakeClock := NewFakeClock(fixedTime)

	t.Run("YAML parsing with ingestion delay", func(t *testing.T) {
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
  ingestionDelay: 5m`

		var sr SummaryRule
		err := yaml.Unmarshal([]byte(yamlStr), &sr)
		require.NoError(t, err)
		require.Equal(t, "Metrics", sr.Spec.Database)
		require.Equal(t, "HourlyAvg", sr.GetName())
		require.Equal(t, "adx-mon", sr.GetNamespace())
		require.Equal(t, "SomeMetricHourlyAvg", sr.Spec.Table)
		require.Equal(t, metav1.Duration{Duration: time.Hour}, sr.Spec.Interval)
		require.NotNil(t, sr.Spec.IngestionDelay)
		require.Equal(t, metav1.Duration{Duration: 5 * time.Minute}, *sr.Spec.IngestionDelay)
	})

	t.Run("YAML parsing without ingestion delay", func(t *testing.T) {
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
		require.Nil(t, sr.Spec.IngestionDelay)
	})

	t.Run("NextExecutionWindow with ingestion delay - first execution", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval:       metav1.Duration{Duration: time.Hour},
				IngestionDelay: &metav1.Duration{Duration: 10 * time.Minute},
			},
		}

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		// Expected: current time (12:01:02) minus 10min = 11:51:02, truncated to hour = 11:00:00
		// End time: 11:00:00
		// Start time: 10:00:00
		expectedStart := time.Date(2025, 6, 23, 10, 0, 0, 0, time.UTC)
		expectedEnd := time.Date(2025, 6, 23, 11, 0, 0, 0, time.UTC)

		require.Equal(t, expectedStart, startTime)
		require.Equal(t, expectedEnd, endTime)
		require.Equal(t, time.Hour, endTime.Sub(startTime))
	})

	t.Run("NextExecutionWindow with ingestion delay - subsequent execution", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval:       metav1.Duration{Duration: time.Hour},
				IngestionDelay: &metav1.Duration{Duration: 15 * time.Minute},
			},
		}

		// Set last execution time
		lastExecution := time.Date(2025, 6, 23, 11, 0, 0, 0, time.UTC)
		rule.SetLastExecutionTime(lastExecution)

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		// lastExecution - 15min = 10:45:00 (no truncation for exact time continuity)
		// End time: 11:45:00 (start + interval)
		expectedStart := time.Date(2025, 6, 23, 10, 45, 0, 0, time.UTC)
		expectedEnd := time.Date(2025, 6, 23, 11, 45, 0, 0, time.UTC)

		require.Equal(t, expectedStart, startTime)
		require.Equal(t, expectedEnd, endTime)
		require.Equal(t, time.Hour, endTime.Sub(startTime))
	})

	t.Run("NextExecutionWindow with zero ingestion delay", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval:       metav1.Duration{Duration: time.Hour},
				IngestionDelay: &metav1.Duration{Duration: 0},
			},
		}

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		// Should behave the same as no ingestion delay
		expectedStart := time.Date(2025, 6, 23, 11, 0, 0, 0, time.UTC)
		expectedEnd := time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC)

		require.Equal(t, expectedStart, startTime)
		require.Equal(t, expectedEnd, endTime)
	})

	t.Run("NextExecutionWindow with very large ingestion delay", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval:       metav1.Duration{Duration: time.Hour},
				IngestionDelay: &metav1.Duration{Duration: 2 * time.Hour},
			},
		}

		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		// 12:01:02 - 2h = 10:01:02, truncated to hour = 10:00:00
		// End time: 10:00:00
		// Start time: 9:00:00
		expectedStart := time.Date(2025, 6, 23, 9, 0, 0, 0, time.UTC)
		expectedEnd := time.Date(2025, 6, 23, 10, 0, 0, 0, time.UTC)

		require.Equal(t, expectedStart, startTime)
		require.Equal(t, expectedEnd, endTime)
	})

	t.Run("NextExecutionWindow with ingestion delay and future window capping", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval:       metav1.Duration{Duration: time.Hour},
				IngestionDelay: &metav1.Duration{Duration: 30 * time.Minute},
			},
		}

		// Set last execution time that would create a future window
		lastExecution := fixedTime.Add(-30 * time.Minute).UTC() // 30 minutes ago
		rule.SetLastExecutionTime(lastExecution)

		// lastExecution - 30min = 11:01:02.003 (no truncation for exact time continuity)
		// End time: 12:01:02.003 (capped to now - delay = 11:31:02.003, truncated to minute = 11:31:00)
		startTime, endTime := rule.NextExecutionWindow(fakeClock)
		expectedStart := time.Date(2025, 6, 23, 11, 1, 2, 3000000, time.UTC)
		expectedEnd := time.Date(2025, 6, 23, 11, 31, 0, 0, time.UTC)

		require.Equal(t, expectedStart, startTime)
		require.Equal(t, expectedEnd, endTime)
		require.True(t, endTime.Before(fixedTime))
	})

	t.Run("NextExecutionWindow with different interval and ingestion delay combinations", func(t *testing.T) {
		testCases := []struct {
			name           string
			interval       time.Duration
			ingestionDelay time.Duration
			expectedStart  time.Time
			expectedEnd    time.Time
		}{
			{
				name:           "30min interval with 5min delay",
				interval:       30 * time.Minute,
				ingestionDelay: 5 * time.Minute,
				expectedStart:  time.Date(2025, 6, 23, 11, 0, 0, 0, time.UTC),
				expectedEnd:    time.Date(2025, 6, 23, 11, 30, 0, 0, time.UTC),
			},
			{
				name:           "2h interval with 15min delay",
				interval:       2 * time.Hour,
				ingestionDelay: 15 * time.Minute,
				expectedStart:  time.Date(2025, 6, 23, 8, 0, 0, 0, time.UTC),
				expectedEnd:    time.Date(2025, 6, 23, 10, 0, 0, 0, time.UTC),
			},
			{
				name:           "1day interval with 1h delay",
				interval:       24 * time.Hour,
				ingestionDelay: time.Hour,
				expectedStart:  time.Date(2025, 6, 22, 0, 0, 0, 0, time.UTC),
				expectedEnd:    time.Date(2025, 6, 23, 0, 0, 0, 0, time.UTC),
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				rule := &SummaryRule{
					ObjectMeta: metav1.ObjectMeta{
						Generation: 1,
					},
					Spec: SummaryRuleSpec{
						Interval:       metav1.Duration{Duration: tc.interval},
						IngestionDelay: &metav1.Duration{Duration: tc.ingestionDelay},
					},
				}

				startTime, endTime := rule.NextExecutionWindow(fakeClock)
				t.Logf("TestCase: %s", tc.name)
				t.Logf("Actual start: %v", startTime)
				t.Logf("Actual end: %v", endTime)
				t.Logf("Expected start: %v", tc.expectedStart)
				t.Logf("Expected end: %v", tc.expectedEnd)
				require.Equal(t, tc.expectedStart, startTime)
				require.Equal(t, tc.expectedEnd, endTime)
				require.Equal(t, tc.interval, endTime.Sub(startTime))
			})
		}
	})

	// Status conditions test: last execution time is 12:00:00, delay is 10min, so window starts at 11:00:00, ends at 12:00:00
	// But if now - delay is before window end, it will be capped
	// 12:01:02 - 10min = 11:51:02, truncated to minute = 11:51:00
	t.Run("Status conditions unchanged by ingestion delay", func(t *testing.T) {
		rule := &SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
			},
			Spec: SummaryRuleSpec{
				Interval:       metav1.Duration{Duration: time.Hour},
				IngestionDelay: &metav1.Duration{Duration: 10 * time.Minute},
			},
		}

		// Set a condition before applying ingestion delay
		originalTime := time.Date(2025, 6, 23, 12, 0, 0, 0, time.UTC)
		rule.SetLastExecutionTime(originalTime)

		// Verify the condition is set correctly
		retrievedTime := rule.GetLastExecutionTime()
		require.NotNil(t, retrievedTime)
		require.Equal(t, originalTime, *retrievedTime)

		// Calculate next execution window (this should not affect the stored condition)
		startTime, endTime := rule.NextExecutionWindow(fakeClock)

		// Debug output
		t.Logf("Original time: %v", originalTime)
		t.Logf("Calculated start time: %v", startTime)
		t.Logf("Calculated end time: %v", endTime)
		t.Logf("Current time (fake clock): %v", fakeClock.Now())

		// The stored condition should remain unchanged
		retrievedTime = rule.GetLastExecutionTime()
		require.NotNil(t, retrievedTime)
		require.Equal(t, originalTime, *retrievedTime)

		// Start time: 11:50:00 (originalTime - delay, no truncation), end time: 12:50:00, but capped to 11:51:00
		expectedStart := time.Date(2025, 6, 23, 11, 50, 0, 0, time.UTC)
		expectedEnd := time.Date(2025, 6, 23, 11, 51, 0, 0, time.UTC)
		require.Equal(t, expectedStart, startTime)
		require.Equal(t, expectedEnd, endTime)
	})
}
