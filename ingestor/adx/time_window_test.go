package adx

import (
	"context"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestTimeWindowCalculation(t *testing.T) {
	t.Run("first execution calculates correct time window", func(t *testing.T) {
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

		// Create task
		task := &SummaryRuleTask{
			store:    mockHandler,
			kustoCli: mockExecutor,
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

		// Verify the window duration is exactly the interval
		windowDuration := endTime.Sub(startTime)
		require.Equal(t, rule.Spec.Interval.Duration, windowDuration,
			"Window duration should match the configured interval")

		// Verify the window ends at or before current time
		now := time.Now().UTC()
		require.True(t, endTime.Before(now) || endTime.Equal(now.Truncate(time.Minute)),
			"Window should not extend into the future")
	})

	t.Run("subsequent execution uses last successful end time as start time", func(t *testing.T) {
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

		// Set a last successful execution time
		lastSuccessfulEndTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
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

		// Create task
		task := &SummaryRuleTask{
			store:    mockHandler,
			kustoCli: mockExecutor,
		}

		// Mock GetOperations to return empty
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

		// Verify the time windows
		require.NotEmpty(t, capturedStartTime)
		require.NotEmpty(t, capturedEndTime)

		// Parse the times
		startTime, err := time.Parse(time.RFC3339Nano, capturedStartTime)
		require.NoError(t, err)
		endTime, err := time.Parse(time.RFC3339Nano, capturedEndTime)
		require.NoError(t, err)

		// Verify start time matches the last successful end time
		require.True(t, startTime.Equal(lastSuccessfulEndTime),
			"Start time should equal the last successful execution end time")

		// Verify the window duration is exactly the interval
		windowDuration := endTime.Sub(startTime)
		require.Equal(t, rule.Spec.Interval.Duration, windowDuration,
			"Window duration should match the configured interval")
	})

	t.Run("execution time is updated when shouldSubmitRule is true", func(t *testing.T) {
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
		rule.SetCondition(metav1.Condition{
			LastTransitionTime: metav1.Time{Time: time.Now().Add(-2 * time.Hour)},
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

		// Create task
		task := &SummaryRuleTask{
			store:    mockHandler,
			kustoCli: mockExecutor,
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

		// Parse the submitted window end time and verify it matches
		expectedEndTime, err := time.Parse(time.RFC3339Nano, submittedWindowEndTime)
		require.NoError(t, err)
		require.True(t, lastExecution.Equal(expectedEndTime),
			"Last execution time should match the submitted window end time")

		// Verify an async operation was created
		asyncOps := updatedRule.GetAsyncOperations()
		require.Len(t, asyncOps, 1, "Should have one async operation after submission")
		require.Equal(t, "test-operation-id", asyncOps[0].OperationId, "Should have the correct operation ID")
	})

	t.Run("prevents gaps and overlaps in time windows", func(t *testing.T) {
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

		// Simulate first execution completed successfully
		firstWindowStart := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
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

		// Create task
		task := &SummaryRuleTask{
			store:    mockHandler,
			kustoCli: mockExecutor,
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

		// Verify correct duration
		duration := end.Sub(start)
		require.Equal(t, 30*time.Minute, duration,
			"Window duration should match the configured interval")

		// Verify no overlap by checking the windows are contiguous
		require.True(t, start.Equal(firstWindowEnd),
			"Windows should be contiguous with no overlap")
	})
}
