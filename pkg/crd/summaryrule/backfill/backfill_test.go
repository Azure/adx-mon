package backfill

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clocktesting "k8s.io/utils/clock/testing"
)

// --- helpers ---

func fakeClockAt(t time.Time) *clocktesting.FakeClock {
	return clocktesting.NewFakeClock(t)
}

func baseTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func newRule(start, end time.Time, interval time.Duration) *v1.SummaryRule {
	return &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-rule",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1.SummaryRuleSpec{
			Database: "testdb",
			Table:    "TestTable",
			Interval: metav1.Duration{Duration: interval},
			Body:     "T | where Timestamp between (_startTime .. _endTime)",
			Backfill: &v1.BackfillSpec{
				RequestID: "backfill-1",
				StartTime: metav1.Time{Time: start},
				EndTime:   metav1.Time{Time: end},
			},
		},
	}
}

// succeedSubmit returns a SubmitFunc that always succeeds with sequential operation IDs.
func succeedSubmit() (SubmitFunc, *[]string) {
	var calls []string
	counter := 0
	fn := func(ctx context.Context, rule v1.SummaryRule, start, end string) (string, error) {
		counter++
		id := fmt.Sprintf("op-%d", counter)
		calls = append(calls, fmt.Sprintf("%s→%s", start, end))
		return id, nil
	}
	return fn, &calls
}

// failSubmit returns a SubmitFunc that always fails.
func failSubmit() SubmitFunc {
	return func(ctx context.Context, rule v1.SummaryRule, start, end string) (string, error) {
		return "", fmt.Errorf("kusto unavailable")
	}
}

// nthCallFails returns a SubmitFunc that fails on the nth call (1-indexed).
func nthCallFails(n int) (SubmitFunc, *int) {
	counter := 0
	fn := func(ctx context.Context, rule v1.SummaryRule, start, end string) (string, error) {
		counter++
		if counter == n {
			return "", fmt.Errorf("transient error")
		}
		return fmt.Sprintf("op-%d", counter), nil
	}
	return fn, &counter
}

// completedStatus returns a GetOperationStatusFunc where all ops are Completed.
func completedStatus() GetOperationStatusFunc {
	return func(ctx context.Context, db, opID string) (string, bool, error) {
		return "Completed", false, nil
	}
}

// inProgressStatus returns a GetOperationStatusFunc where all ops are InProgress.
func inProgressStatus() GetOperationStatusFunc {
	return func(ctx context.Context, db, opID string) (string, bool, error) {
		return "InProgress", false, nil
	}
}

// failedStatus returns a GetOperationStatusFunc where all ops are Failed.
func failedStatus() GetOperationStatusFunc {
	return func(ctx context.Context, db, opID string) (string, bool, error) {
		return "Failed", false, nil
	}
}

// retryableStatus returns a GetOperationStatusFunc where all ops are marked retryable.
func retryableStatus() GetOperationStatusFunc {
	return func(ctx context.Context, db, opID string) (string, bool, error) {
		return "InProgress", true, nil
	}
}

// statusByID maps operation IDs to states for selective status responses.
func statusByID(m map[string]string) GetOperationStatusFunc {
	return func(ctx context.Context, db, opID string) (string, bool, error) {
		state, ok := m[opID]
		if !ok {
			return "InProgress", false, nil
		}
		return state, false, nil
	}
}

// --- tests ---

func TestProcess_NoBackfillSpec(t *testing.T) {
	rule := &v1.SummaryRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "ns", Generation: 1},
		Spec:       v1.SummaryRuleSpec{Database: "db", Table: "T", Interval: metav1.Duration{Duration: time.Hour}, Body: "T"},
	}
	clk := fakeClockAt(baseTime())
	submit, _ := succeedSubmit()

	Process(context.Background(), rule, clk, submit, completedStatus())

	require.Nil(t, rule.Status.Backfill)
}

func TestProcess_InvalidRange(t *testing.T) {
	start := baseTime()
	end := start.Add(-time.Hour) // end before start
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start)
	submit, _ := succeedSubmit()

	Process(context.Background(), rule, clk, submit, completedStatus())

	require.NotNil(t, rule.Status.Backfill)
	require.Equal(t, v1.BackfillPhaseFailed, rule.Status.Backfill.Phase)
	require.Contains(t, rule.Status.Backfill.Message, "EndTime must be after StartTime")
}

func TestProcess_InvalidSpecFailsAndClearsStaleProgress(t *testing.T) {
	start := baseTime()
	end := start.Add(2 * time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Status.Backfill = &v1.BackfillStatus{
		RequestID:          "old-request",
		Phase:              v1.BackfillPhaseRunning,
		ObservedGeneration: 1,
		NextWindowStart:    metav1.NewTime(start.Add(time.Hour)),
		SubmittedWindows:   3,
		CompletedWindows:   1,
	}
	rule.Spec.Backfill.RequestID = ""

	clk := fakeClockAt(start)
	submit, _ := succeedSubmit()

	Process(context.Background(), rule, clk, submit, completedStatus())

	require.NotNil(t, rule.Status.Backfill)
	require.Equal(t, v1.BackfillPhaseFailed, rule.Status.Backfill.Phase)
	require.Contains(t, rule.Status.Backfill.Message, "RequestID must be set")
	require.False(t, rule.Status.Backfill.NextWindowStart.IsZero())

	cond := meta.FindStatusCondition(rule.Status.Conditions, v1.ConditionBackfill)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(v1.BackfillPhaseFailed), cond.Reason)
}

func TestProcess_SingleWindow(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour) // exactly one window
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, calls := succeedSubmit()

	// First cycle: submit the window.
	Process(context.Background(), rule, clk, submit, completedStatus())

	require.NotNil(t, rule.Status.Backfill)
	require.Equal(t, v1.BackfillPhaseRunning, rule.Status.Backfill.Phase)
	require.Len(t, *calls, 1)
	require.Equal(t, 1, rule.Status.Backfill.SubmittedWindows)
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)
	require.Equal(t, "op-1", rule.Status.Backfill.ActiveOperations[0].OperationID)

	// Verify window boundaries.
	expectedStart := start.Format(time.RFC3339Nano)
	expectedEnd := end.Add(-kustoutil.OneTick).UTC().Format(time.RFC3339Nano)
	require.Equal(t, expectedStart, rule.Status.Backfill.ActiveOperations[0].StartTime)
	require.Equal(t, expectedEnd, rule.Status.Backfill.ActiveOperations[0].EndTime)

	// Second cycle: poll completes the op, then checkCompletion fires.
	Process(context.Background(), rule, clk, submit, completedStatus())

	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	require.Equal(t, 1, rule.Status.Backfill.CompletedWindows)
	require.Equal(t, 0, rule.Status.Backfill.RetriedWindows)
}

func TestProcess_MultipleWindows_DefaultMaxInFlight(t *testing.T) {
	start := baseTime()
	end := start.Add(3 * time.Hour) // 3 windows
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(5 * time.Hour))
	submit, _ := succeedSubmit()

	// Default maxInFlight = 1: only one window per cycle.
	// Cycle 1: submit window 0.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)
	require.Equal(t, 1, rule.Status.Backfill.SubmittedWindows)

	// Cycle 2: poll completes window 0, submit window 1.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, 1, rule.Status.Backfill.CompletedWindows)
	require.Equal(t, 2, rule.Status.Backfill.SubmittedWindows)

	// Cycle 3: poll completes window 1, submit window 2.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, 2, rule.Status.Backfill.CompletedWindows)
	require.Equal(t, 3, rule.Status.Backfill.SubmittedWindows)

	// Cycle 4: poll completes window 2, all done.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	require.Equal(t, 3, rule.Status.Backfill.CompletedWindows)
}

func TestProcess_MaxInFlight3(t *testing.T) {
	start := baseTime()
	end := start.Add(5 * time.Hour) // 5 windows
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.MaxInFlight = 3
	clk := fakeClockAt(start.Add(10 * time.Hour))
	submit, _ := succeedSubmit()

	// Cycle 1: submit 3 windows.
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.Len(t, rule.Status.Backfill.ActiveOperations, 3)
	require.Equal(t, 3, rule.Status.Backfill.SubmittedWindows)

	// Cycle 2: all still in progress, no new submissions.
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.Len(t, rule.Status.Backfill.ActiveOperations, 3)
	require.Equal(t, 3, rule.Status.Backfill.SubmittedWindows) // unchanged

	// Cycle 3: all complete, submit remaining 2.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, 3, rule.Status.Backfill.CompletedWindows)
	require.Len(t, rule.Status.Backfill.ActiveOperations, 2)
	require.Equal(t, 5, rule.Status.Backfill.SubmittedWindows)

	// Cycle 4: complete remaining 2.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	require.Equal(t, 5, rule.Status.Backfill.CompletedWindows)
}

func TestProcess_SubmissionFailure_CreatesBacklog(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(2 * time.Hour))

	// Cycle 1: submission fails — should create backlog entry.
	Process(context.Background(), rule, clk, failSubmit(), completedStatus())
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)
	require.Equal(t, "", rule.Status.Backfill.ActiveOperations[0].OperationID) // backlog
	require.Equal(t, 0, rule.Status.Backfill.SubmittedWindows)                 // not counted

	// Cycle 2: retry succeeds.
	submit, _ := succeedSubmit()
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, 1, rule.Status.Backfill.SubmittedWindows)
	require.Equal(t, "op-1", rule.Status.Backfill.ActiveOperations[0].OperationID)

	// Cycle 3: poll completes.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
}

func TestProcess_FailedOperation_RetriedNotSkipped(t *testing.T) {
	start := baseTime()
	end := start.Add(2 * time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(5 * time.Hour))
	submit, _ := succeedSubmit()

	// Cycle 1: submit window 0.
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)

	// Cycle 2: window 0 fails → re-queued as backlog AND immediately retried
	// in the same cycle (since submitNewWindows retries backlog entries).
	Process(context.Background(), rule, clk, submit, failedStatus())
	require.Equal(t, 1, rule.Status.Backfill.RetriedWindows)
	require.Equal(t, 0, rule.Status.Backfill.CompletedWindows)
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)
	// The backlog entry was retried in the same cycle, so it should have a new OperationID.
	require.NotEmpty(t, rule.Status.Backfill.ActiveOperations[0].OperationID)
	// Verify it's still the same window (not skipped to window 1).
	require.Equal(t, start.Format(time.RFC3339Nano),
		rule.Status.Backfill.ActiveOperations[0].StartTime,
		"failed window should be retried with same start time, not skipped")

	// Cycle 3: retried window 0 completes → submit window 1.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, 1, rule.Status.Backfill.CompletedWindows)
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)

	// Cycle 4: window 1 completes → all done.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	require.Equal(t, 2, rule.Status.Backfill.CompletedWindows)
	require.Equal(t, 1, rule.Status.Backfill.RetriedWindows)
}

func TestProcess_ShouldRetryRequeuesWindow(t *testing.T) {
	start := baseTime()
	end := start.Add(2 * time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(5 * time.Hour))
	submit, _ := succeedSubmit()

	Process(context.Background(), rule, clk, submit, inProgressStatus())
	firstOperationID := rule.Status.Backfill.ActiveOperations[0].OperationID
	require.NotEmpty(t, firstOperationID)

	Process(context.Background(), rule, clk, submit, retryableStatus())

	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)
	require.Equal(t, 1, rule.Status.Backfill.RetriedWindows)
	require.Equal(t, start.Format(time.RFC3339Nano), rule.Status.Backfill.ActiveOperations[0].StartTime)
	require.NotEmpty(t, rule.Status.Backfill.ActiveOperations[0].OperationID)
	require.NotEqual(t, firstOperationID, rule.Status.Backfill.ActiveOperations[0].OperationID)
}

func TestProcess_RequestIDResume(t *testing.T) {
	start := baseTime()
	end := start.Add(3 * time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(5 * time.Hour))
	submit, _ := succeedSubmit()

	// Cycle 1: submit window 0.
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.Equal(t, 1, rule.Status.Backfill.SubmittedWindows)

	// Simulate controller restart: same requestID should resume, not restart.
	savedStatus := rule.Status.Backfill.DeepCopy()
	rule.Status.Backfill = savedStatus

	Process(context.Background(), rule, clk, submit, completedStatus())
	// Should complete window 0 and submit window 1 (not restart from 0).
	require.Equal(t, 1, rule.Status.Backfill.CompletedWindows)
	require.Equal(t, 2, rule.Status.Backfill.SubmittedWindows)
}

func TestProcess_NewRequestID_Restarts(t *testing.T) {
	start := baseTime()
	end := start.Add(2 * time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(5 * time.Hour))
	submit, _ := succeedSubmit()

	// Start backfill with request "backfill-1".
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.Equal(t, 1, rule.Status.Backfill.SubmittedWindows)

	// Change requestID → should reset.
	rule.Spec.Backfill.RequestID = "backfill-2"
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, "backfill-2", rule.Status.Backfill.RequestID)
	require.Equal(t, 1, rule.Status.Backfill.SubmittedWindows) // reset + 1 new
}

func TestProcess_GenerationChange_FailsBackfill(t *testing.T) {
	start := baseTime()
	end := start.Add(2 * time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(5 * time.Hour))
	submit, _ := succeedSubmit()

	// Start backfill at generation 1.
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.Equal(t, v1.BackfillPhaseRunning, rule.Status.Backfill.Phase)

	// Simulate spec update (generation bump).
	rule.ObjectMeta.Generation = 2
	Process(context.Background(), rule, clk, submit, completedStatus())

	require.Equal(t, v1.BackfillPhaseFailed, rule.Status.Backfill.Phase)
	require.Contains(t, rule.Status.Backfill.Message, "spec changed")
}

func TestProcess_CompletedIsTerminal(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, calls := succeedSubmit()

	// Drive to completion.
	Process(context.Background(), rule, clk, submit, completedStatus())
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	callCount := len(*calls)

	// Additional cycles should be no-ops.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	require.Equal(t, callCount, len(*calls)) // no additional submissions
}

func TestProcess_FailedIsTerminal(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.EndTime = metav1.Time{Time: start.Add(-time.Hour)} // invalid
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, calls := succeedSubmit()

	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseFailed, rule.Status.Backfill.Phase)

	// Additional cycles should be no-ops.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseFailed, rule.Status.Backfill.Phase)
	require.Empty(t, *calls) // no submissions attempted
}

func TestProcess_ClearsStatusWhenSpecRemoved(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, _ := succeedSubmit()

	// Start a backfill (Running phase).
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.NotNil(t, rule.Status.Backfill)
	require.Equal(t, v1.BackfillPhaseRunning, rule.Status.Backfill.Phase)

	// Remove backfill spec — status should be cleared for non-terminal phases.
	rule.Spec.Backfill = nil
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Nil(t, rule.Status.Backfill)
	require.Nil(t, meta.FindStatusCondition(rule.Status.Conditions, v1.ConditionBackfill))
}

func TestProcess_KeepsTerminalStatusWhenSpecRemoved(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, _ := succeedSubmit()

	// Drive to completion.
	Process(context.Background(), rule, clk, submit, completedStatus())
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)

	// Remove spec — completed status should be preserved.
	rule.Spec.Backfill = nil
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.NotNil(t, rule.Status.Backfill)
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	cond := meta.FindStatusCondition(rule.Status.Conditions, v1.ConditionBackfill)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
}

func TestProcess_WindowBoundaries(t *testing.T) {
	start := baseTime()
	end := start.Add(2 * time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.MaxInFlight = 10 // allow all windows at once
	clk := fakeClockAt(start.Add(5 * time.Hour))

	var submittedWindows []struct{ start, end string }
	submit := func(ctx context.Context, r v1.SummaryRule, s, e string) (string, error) {
		submittedWindows = append(submittedWindows, struct{ start, end string }{s, e})
		return fmt.Sprintf("op-%d", len(submittedWindows)), nil
	}

	Process(context.Background(), rule, clk, submit, inProgressStatus())

	require.Len(t, submittedWindows, 2)

	// Window 0: [00:00, 01:00 - OneTick)
	require.Equal(t, start.Format(time.RFC3339Nano), submittedWindows[0].start)
	require.Equal(t, start.Add(time.Hour).Add(-kustoutil.OneTick).UTC().Format(time.RFC3339Nano), submittedWindows[0].end)

	// Window 1: [01:00, 02:00 - OneTick)
	require.Equal(t, start.Add(time.Hour).Format(time.RFC3339Nano), submittedWindows[1].start)
	require.Equal(t, start.Add(2*time.Hour).Add(-kustoutil.OneTick).UTC().Format(time.RFC3339Nano), submittedWindows[1].end)
}

func TestProcess_MaxInFlightCapped(t *testing.T) {
	start := baseTime()
	end := start.Add(100 * time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.MaxInFlight = 999 // should be capped to MaxActiveOperations
	clk := fakeClockAt(start.Add(200 * time.Hour))
	submit, _ := succeedSubmit()

	Process(context.Background(), rule, clk, submit, inProgressStatus())

	require.Len(t, rule.Status.Backfill.ActiveOperations, MaxActiveOperations)
}

func TestProcess_MixedOperationStates(t *testing.T) {
	start := baseTime()
	end := start.Add(3 * time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.MaxInFlight = 3
	clk := fakeClockAt(start.Add(5 * time.Hour))
	submit, _ := succeedSubmit()

	// Cycle 1: submit all 3 windows.
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.Len(t, rule.Status.Backfill.ActiveOperations, 3)

	// Cycle 2: op-1 completes, op-2 fails (re-queued), op-3 in progress.
	statusMap := statusByID(map[string]string{
		"op-1": "Completed",
		"op-2": "Failed",
		"op-3": "InProgress",
	})
	Process(context.Background(), rule, clk, submit, statusMap)

	require.Equal(t, 1, rule.Status.Backfill.CompletedWindows)
	require.Equal(t, 1, rule.Status.Backfill.RetriedWindows)
	// op-2 re-queued as backlog + op-3 still in progress = 2 active ops.
	// But the backlog retry in submitNewWindows will re-submit op-2, so it
	// should now have an OperationID again.
	require.Len(t, rule.Status.Backfill.ActiveOperations, 2)
}

func TestProcess_EmptyRequestID(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.RequestID = ""
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, calls := succeedSubmit()

	Process(context.Background(), rule, clk, submit, completedStatus())

	require.NotNil(t, rule.Status.Backfill)
	require.Equal(t, v1.BackfillPhaseFailed, rule.Status.Backfill.Phase)
	require.Contains(t, rule.Status.Backfill.Message, "RequestID must be set")
	require.False(t, rule.Status.Backfill.NextWindowStart.IsZero())
	require.Empty(t, *calls)
}

func TestProcess_SubIntervalRange(t *testing.T) {
	// EndTime is less than one full interval from StartTime — no windows fit.
	start := baseTime()
	end := start.Add(30 * time.Minute)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, calls := succeedSubmit()

	Process(context.Background(), rule, clk, submit, completedStatus())

	// No windows submitted — should immediately complete.
	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	require.Equal(t, 0, rule.Status.Backfill.SubmittedWindows)
	require.Empty(t, *calls)
}

func TestProcess_LargeRange_BatchesOverCycles(t *testing.T) {
	start := baseTime()
	end := start.Add(24 * time.Hour) // 24 windows
	rule := newRule(start, end, time.Hour)
	// Default maxInFlight=1, so should take 24 cycles to complete.
	clk := fakeClockAt(start.Add(48 * time.Hour))
	submit, _ := succeedSubmit()

	for i := 0; i < 50; i++ {
		Process(context.Background(), rule, clk, submit, completedStatus())
		if rule.Status.Backfill.Phase == v1.BackfillPhaseCompleted {
			break
		}
	}

	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	require.Equal(t, 24, rule.Status.Backfill.CompletedWindows)
	require.Equal(t, 24, rule.Status.Backfill.SubmittedWindows)
}

func TestProcess_NilRule(t *testing.T) {
	// Should not panic.
	Process(context.Background(), nil, clocktesting.NewFakeClock(baseTime()), failSubmit(), completedStatus())
}

func TestEffectiveMaxInFlight(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero defaults to 1", 0, DefaultMaxInFlight},
		{"negative defaults to 1", -5, DefaultMaxInFlight},
		{"normal value", 3, 3},
		{"at cap", MaxActiveOperations, MaxActiveOperations},
		{"over cap", MaxActiveOperations + 10, MaxActiveOperations},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &v1.BackfillSpec{MaxInFlight: tt.input}
			require.Equal(t, tt.expected, effectiveMaxInFlight(spec))
		})
	}
}

func TestProcess_PartialSubmissionFailure(t *testing.T) {
	start := baseTime()
	end := start.Add(3 * time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.MaxInFlight = 3
	clk := fakeClockAt(start.Add(5 * time.Hour))

	// Second call fails.
	submit, _ := nthCallFails(2)

	Process(context.Background(), rule, clk, submit, inProgressStatus())

	ops := rule.Status.Backfill.ActiveOperations
	require.Len(t, ops, 3)
	require.Equal(t, "op-1", ops[0].OperationID)
	require.Equal(t, "", ops[1].OperationID) // backlog
	require.Equal(t, "op-3", ops[2].OperationID)
	require.Equal(t, 2, rule.Status.Backfill.SubmittedWindows) // only successful counted
}

func TestProcess_DoesNotTouchLastExecutionTime(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, _ := succeedSubmit()

	// Set a LastExecutionTime to verify it's not modified.
	rule.SetLastExecutionTime(start.Add(-24 * time.Hour))
	original := rule.GetLastExecutionTime()

	Process(context.Background(), rule, clk, submit, completedStatus())
	Process(context.Background(), rule, clk, submit, completedStatus())

	after := rule.GetLastExecutionTime()
	require.Equal(t, original, after, "backfill should not modify LastExecutionTime")
}

func TestProcess_ContiguousWindows_NoGaps_NoOverlaps(t *testing.T) {
	start := baseTime()
	end := start.Add(10 * time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.MaxInFlight = 5
	clk := fakeClockAt(start.Add(20 * time.Hour))

	// Capture all submitted windows in order.
	var windows []struct{ start, end string }
	submit := func(ctx context.Context, r v1.SummaryRule, s, e string) (string, error) {
		windows = append(windows, struct{ start, end string }{s, e})
		return fmt.Sprintf("op-%d", len(windows)), nil
	}

	// Run to completion.
	for i := 0; i < 50; i++ {
		Process(context.Background(), rule, clk, submit, completedStatus())
		if rule.Status.Backfill.Phase == v1.BackfillPhaseCompleted {
			break
		}
	}

	require.Equal(t, v1.BackfillPhaseCompleted, rule.Status.Backfill.Phase)
	require.Equal(t, 10, len(windows), "expected exactly 10 windows for a 10-hour range with 1h intervals")

	// Parse all windows and verify contiguous, non-overlapping, no gaps.
	for i, w := range windows {
		wStart, err := time.Parse(time.RFC3339Nano, w.start)
		require.NoError(t, err, "window %d start parse", i)
		wEnd, err := time.Parse(time.RFC3339Nano, w.end)
		require.NoError(t, err, "window %d end parse", i)

		// Each window should be exactly (interval - OneTick) wide.
		expectedEnd := wStart.Add(time.Hour).Add(-kustoutil.OneTick)
		require.Equal(t, expectedEnd, wEnd, "window %d should span exactly one interval (minus OneTick)", i)

		// Expected start is base + i*interval.
		expectedStart := start.Add(time.Duration(i) * time.Hour)
		require.Equal(t, expectedStart, wStart, "window %d should start at expected offset", i)

		// Verify no overlap with previous window.
		if i > 0 {
			prevEnd, _ := time.Parse(time.RFC3339Nano, windows[i-1].end)
			require.True(t, wStart.After(prevEnd),
				"window %d starts at %s but previous window ends at %s — overlap detected",
				i, wStart, prevEnd)
		}

		// Verify no gap from previous window.
		if i > 0 {
			prevEnd, _ := time.Parse(time.RFC3339Nano, windows[i-1].end)
			// Previous inclusive end + OneTick = this window's start (contiguous).
			require.Equal(t, prevEnd.Add(kustoutil.OneTick), wStart,
				"gap detected between window %d and %d", i-1, i)
		}
	}
}

func TestProcess_FailedOpsBlockProgress_NoSkip(t *testing.T) {
	// With maxInFlight=1, a failed operation should be retried in the same
	// window — no window is ever skipped.
	start := baseTime()
	end := start.Add(3 * time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(10 * time.Hour))

	var submittedStarts []string
	callCount := 0
	submit := func(ctx context.Context, r v1.SummaryRule, s, e string) (string, error) {
		callCount++
		submittedStarts = append(submittedStarts, s)
		return fmt.Sprintf("op-%d", callCount), nil
	}

	// Cycle 1: submit window 0.
	Process(context.Background(), rule, clk, submit, inProgressStatus())
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)

	// Cycle 2: window 0 fails → re-queued and immediately retried in same cycle.
	Process(context.Background(), rule, clk, submit, failedStatus())
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)
	require.NotEmpty(t, rule.Status.Backfill.ActiveOperations[0].OperationID)
	// Verify window 0's start time is still the same (not skipped to window 1).
	require.Equal(t, start.Format(time.RFC3339Nano),
		rule.Status.Backfill.ActiveOperations[0].StartTime,
		"failed window should be retried with same start time, not skipped")

	// Cycle 3: retried window 0 completes → submit window 1.
	Process(context.Background(), rule, clk, submit, completedStatus())
	require.Equal(t, 1, rule.Status.Backfill.CompletedWindows)
	require.Len(t, rule.Status.Backfill.ActiveOperations, 1)
	require.Equal(t, start.Add(time.Hour).Format(time.RFC3339Nano),
		rule.Status.Backfill.ActiveOperations[0].StartTime,
		"after window 0 completes, window 1 should be submitted")
}

func TestProcess_NoOverlap_WithMaxInFlight(t *testing.T) {
	start := baseTime()
	end := start.Add(5 * time.Hour)
	rule := newRule(start, end, time.Hour)
	rule.Spec.Backfill.MaxInFlight = 3
	clk := fakeClockAt(start.Add(10 * time.Hour))

	var windows []string
	submit := func(ctx context.Context, r v1.SummaryRule, s, e string) (string, error) {
		windows = append(windows, s)
		return fmt.Sprintf("op-%d", len(windows)), nil
	}

	// Run multiple cycles to completion.
	for i := 0; i < 30; i++ {
		Process(context.Background(), rule, clk, submit, completedStatus())
		if rule.Status.Backfill.Phase == v1.BackfillPhaseCompleted {
			break
		}
	}

	// Verify no duplicate start times (no overlapping windows).
	seen := make(map[string]int)
	for _, w := range windows {
		seen[w]++
	}
	for start, count := range seen {
		require.Equal(t, 1, count,
			"window starting at %s was submitted %d times — overlap detected", start, count)
	}
	require.Equal(t, 5, len(windows), "expected exactly 5 unique windows")
}

func TestProcess_BackfillConditionLifecycle(t *testing.T) {
	start := baseTime()
	end := start.Add(time.Hour)
	rule := newRule(start, end, time.Hour)
	clk := fakeClockAt(start.Add(2 * time.Hour))
	submit, _ := succeedSubmit()

	Process(context.Background(), rule, clk, submit, inProgressStatus())

	cond := meta.FindStatusCondition(rule.Status.Conditions, v1.ConditionBackfill)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionUnknown, cond.Status)
	require.Equal(t, string(v1.BackfillPhaseRunning), cond.Reason)

	Process(context.Background(), rule, clk, submit, completedStatus())

	cond = meta.FindStatusCondition(rule.Status.Conditions, v1.ConditionBackfill)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, string(v1.BackfillPhaseCompleted), cond.Reason)
}
