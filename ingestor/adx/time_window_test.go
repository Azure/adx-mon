package adx

import (
	"context"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func TestTimeWindowCalculation(t *testing.T) {
	t.Run("first execution calculates correct time window", func(t *testing.T) {
		// Use a fixed time for deterministic testing
		fixedTime := time.Date(2025, 6, 17, 8, 30, 0, 0, time.UTC)
		fakeClock := NewFakeClock(fixedTime)

		// Create a rule with 1 hour interval
		rule := &v1.SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-rule",
			},
			Spec: v1.SummaryRuleSpec{
				Database: "testdb",
				Table:    "TestTable",
				Interval: metav1.Duration{Duration: time.Hour},
				Body:     "TestBody",
			},
		}

		// Mock handler that tracks updates
		mockHandler := &mockCRDHandler{
			listResponse: &v1.SummaryRuleList{
				Items: []v1.SummaryRule{*rule},
			},
			updatedObjects: []client.Object{},
		}

		// Mock executor
		mockExecutor := &TestStatementExecutor{
			database: "testdb",
			endpoint: "http://test-endpoint",
		}

		// Create task with fake clock
		task := &SummaryRuleTask{
			store:    mockHandler,
			kustoCli: mockExecutor,
			Clock:    fakeClock,
		}

		// Mock GetOperations to return empty (no ongoing operations)
		task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
			return []AsyncOperationStatus{}, nil
		}

		// Mock SubmitRule to track the time windows
		var capturedStartTime, capturedEndTime string
		task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
			capturedStartTime = startTime
			capturedEndTime = endTime
			return "test-operation-id", nil
		}

		// Run the task
		err := task.Run(context.Background())
		require.NoError(t, err)

		// Verify that a time window was captured
		require.NotEmpty(t, capturedStartTime)
		require.NotEmpty(t, capturedEndTime)

		// Parse the times
		startTime, err := time.Parse(time.RFC3339Nano, capturedStartTime)
		require.NoError(t, err)
		endTime, err := time.Parse(time.RFC3339Nano, capturedEndTime)
		require.NoError(t, err)

		// Verify the window duration is exactly the interval minus 1 tick (100 nanoseconds)
		windowDuration := endTime.Sub(startTime)
		expectedDuration := rule.Spec.Interval.Duration - kustoutil.OneTick
		require.Equal(t, expectedDuration, windowDuration,
			"Window duration should match the configured interval minus 1 tick")

		// Verify the window ends at or before a fixed reference time
		// Using a fixed timestamp well into the future for deterministic testing
		referenceTime := time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC)
		require.True(t, endTime.Before(referenceTime) || endTime.Equal(referenceTime.Truncate(time.Minute)),
			"Window should not extend into the future")
	})

	t.Run("subsequent execution uses last successful end time as start time", func(t *testing.T) {
		// Set a fixed time that's well in the future from the last execution time
		fixedTime := time.Date(2025, 6, 17, 8, 30, 0, 0, time.UTC)
		fakeClock := NewFakeClock(fixedTime)

		// Create a rule with 1 hour interval
		rule := &v1.SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-rule",
			},
			Spec: v1.SummaryRuleSpec{
				Database: "testdb",
				Table:    "TestTable",
				Interval: metav1.Duration{Duration: time.Hour},
				Body:     "TestBody",
			},
		}

		// Set a last successful execution time that's in the past relative to our fake clock
		// Use exactly 1 hour ago to test normal subsequent execution without backfill
		lastSuccessfulEndTime := fixedTime.Add(-1 * time.Hour).UTC().Truncate(rule.Spec.Interval.Duration)
		rule.SetLastExecutionTime(lastSuccessfulEndTime)

		// Add a condition to simulate a previous execution
		rule.SetCondition(metav1.Condition{
			LastTransitionTime: metav1.Time{Time: lastSuccessfulEndTime.Add(-time.Hour)},
			Status:             metav1.ConditionTrue,
		})

		// Mock handler
		mockHandler := &mockCRDHandler{
			listResponse: &v1.SummaryRuleList{
				Items: []v1.SummaryRule{*rule},
			},
			updatedObjects: []client.Object{},
		}

		// Mock executor
		mockExecutor := &TestStatementExecutor{
			database: "testdb",
			endpoint: "http://test-endpoint",
		}

		// Create task with fake clock
		task := &SummaryRuleTask{
			store:    mockHandler,
			kustoCli: mockExecutor,
			Clock:    fakeClock,
		}

		// Mock GetOperations to return empty
		task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
			return []AsyncOperationStatus{}, nil
		}

		// Mock SubmitRule to track the time windows
		var submissionCalls []struct {
			startTime, endTime string
		}
		task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
			submissionCalls = append(submissionCalls, struct {
				startTime, endTime string
			}{startTime, endTime})
			return "test-operation-id", nil
		}

		// Run the task
		err := task.Run(context.Background())
		require.NoError(t, err)

		// Verify that we got exactly one submission
		require.Len(t, submissionCalls, 1, "Expected exactly one rule submission")

		// Parse the times
		startTime, err := time.Parse(time.RFC3339Nano, submissionCalls[0].startTime)
		require.NoError(t, err)
		endTime, err := time.Parse(time.RFC3339Nano, submissionCalls[0].endTime)
		require.NoError(t, err)

		// Verify start time matches the last successful end time
		require.True(t, startTime.Equal(lastSuccessfulEndTime),
			"Start time should equal the last successful execution end time")

		// Verify the window duration is exactly the interval minus 1 tick (100 nanoseconds)
		windowDuration := endTime.Sub(startTime)
		expectedDuration := rule.Spec.Interval.Duration - kustoutil.OneTick
		require.Equal(t, expectedDuration, windowDuration,
			"Window duration should match the configured interval minus 1 tick")
	})

	t.Run("execution time is updated when shouldSubmitRule is true", func(t *testing.T) {
		// Use a fixed time for deterministic testing
		fixedTime := time.Date(2025, 6, 17, 8, 30, 0, 0, time.UTC)
		fakeClock := NewFakeClock(fixedTime)

		// Create a rule with no prior execution history
		rule := &v1.SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-rule",
			},
			Spec: v1.SummaryRuleSpec{
				Database: "testdb",
				Table:    "TestTable",
				Interval: metav1.Duration{Duration: time.Hour},
				Body:     "TestBody",
			},
		}

		// Set a condition with an old LastTransitionTime to allow new submissions
		// Using a time that's far enough in the past relative to our fake clock
		rule.SetCondition(metav1.Condition{
			LastTransitionTime: metav1.Time{Time: fixedTime.Add(-2 * time.Hour)},
			Status:             metav1.ConditionTrue,
		})

		// Mock handler
		mockHandler := &mockCRDHandler{
			listResponse: &v1.SummaryRuleList{
				Items: []v1.SummaryRule{*rule},
			},
			updatedObjects: []client.Object{},
		}

		// Mock executor
		mockExecutor := &TestStatementExecutor{
			database: "testdb",
			endpoint: "http://test-endpoint",
		}

		// Create task with fake clock
		task := &SummaryRuleTask{
			store:    mockHandler,
			kustoCli: mockExecutor,
			Clock:    fakeClock,
		}

		// Mock GetOperations to return empty (no ongoing operations)
		task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
			return []AsyncOperationStatus{}, nil
		}

		// Track the submitted window end time
		var submittedWindowEndTime string
		task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
			submittedWindowEndTime = endTime
			return "test-operation-id", nil
		}

		// Run the task
		err := task.Run(context.Background())
		require.NoError(t, err)

		// Verify that the rule was updated
		require.Len(t, mockHandler.updatedObjects, 1)

		// Get the updated rule
		updatedRule, ok := mockHandler.updatedObjects[0].(*v1.SummaryRule)
		require.True(t, ok)

		// Verify the last execution time was set to the submitted window end time
		lastExecution := updatedRule.GetLastExecutionTime()
		require.NotNil(t, lastExecution)

		// Parse the submitted window end time and verify it matches (accounting for 1 tick adjustment)
		submittedEndTime, err := time.Parse(time.RFC3339Nano, submittedWindowEndTime)
		require.NoError(t, err)
		// The last execution time should be 1 tick (100 nanoseconds) more than the submitted end time
		expectedLastExecution := submittedEndTime.Add(kustoutil.OneTick)
		require.True(t, lastExecution.Equal(expectedLastExecution),
			"Last execution time should be 1 tick more than the submitted window end time")

		// Verify an async operation was created
		asyncOps := updatedRule.GetAsyncOperations()
		require.Len(t, asyncOps, 1, "Should have one async operation after submission")
		require.Equal(t, "test-operation-id", asyncOps[0].OperationId, "Should have the correct operation ID")
	})

	t.Run("prevents gaps and overlaps in time windows", func(t *testing.T) {
		// Use a fixed time for deterministic testing
		fixedTime := time.Date(2025, 6, 17, 8, 30, 0, 0, time.UTC)
		fakeClock := NewFakeClock(fixedTime)

		// Create a rule with 30-minute interval
		rule := &v1.SummaryRule{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-rule",
			},
			Spec: v1.SummaryRuleSpec{
				Database: "testdb",
				Table:    "TestTable",
				Interval: metav1.Duration{Duration: 30 * time.Minute},
				Body:     "TestBody",
			},
		}

		// Simulate first execution completed successfully - should be close to the fake time
		firstWindowStart := time.Date(2025, 6, 17, 7, 30, 0, 0, time.UTC)
		firstWindowEnd := firstWindowStart.Add(30 * time.Minute)
		rule.SetLastExecutionTime(firstWindowEnd)

		// Set condition to allow second execution
		rule.SetCondition(metav1.Condition{
			LastTransitionTime: metav1.Time{Time: firstWindowEnd.Add(-30 * time.Minute)},
			Status:             metav1.ConditionTrue,
		})

		// Mock handler
		mockHandler := &mockCRDHandler{
			listResponse: &v1.SummaryRuleList{
				Items: []v1.SummaryRule{*rule},
			},
			updatedObjects: []client.Object{},
		}

		// Mock executor
		mockExecutor := &TestStatementExecutor{
			database: "testdb",
			endpoint: "http://test-endpoint",
		}

		// Create task with fake clock
		task := &SummaryRuleTask{
			store:    mockHandler,
			kustoCli: mockExecutor,
			Clock:    fakeClock,
		}

		// Mock GetOperations to return empty
		task.GetOperations = func(ctx context.Context) ([]AsyncOperationStatus, error) {
			return []AsyncOperationStatus{}, nil
		}

		// Track multiple executions
		var executions []struct {
			startTime, endTime string
		}
		task.SubmitRule = func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (string, error) {
			executions = append(executions, struct {
				startTime, endTime string
			}{startTime, endTime})
			return "test-operation-id", nil
		}

		// Run the task multiple times to simulate multiple intervals
		err := task.Run(context.Background())
		require.NoError(t, err)

		// Should have exactly one execution (next window)
		require.Len(t, executions, 1)

		// Parse the execution times
		start, err := time.Parse(time.RFC3339Nano, executions[0].startTime)
		require.NoError(t, err)
		end, err := time.Parse(time.RFC3339Nano, executions[0].endTime)
		require.NoError(t, err)

		// Verify no gap: second window starts exactly where first ended
		require.True(t, start.Equal(firstWindowEnd),
			"Second window should start exactly where first window ended (no gap)")

		// Verify correct duration (minus 1 tick)
		duration := end.Sub(start)
		expectedDuration := 30*time.Minute - kustoutil.OneTick
		require.Equal(t, expectedDuration, duration,
			"Window duration should match the configured interval minus 1 tick")

		// Verify no overlap by checking the windows are contiguous
		require.True(t, start.Equal(firstWindowEnd),
			"Windows should be contiguous with no overlap")
	})
}
