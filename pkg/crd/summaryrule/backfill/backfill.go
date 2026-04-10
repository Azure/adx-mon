// Package backfill provides shared logic for processing SummaryRule historical
// backfill requests. Both the Ingestor and ADX Exporter use this package to
// avoid duplicating the window generation, concurrency gating, and progress
// tracking code.
package backfill

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
)

// SubmitFunc is the abstraction that lets both the Ingestor and ADX Exporter
// plug in their own Kusto submission paths.  It receives RFC3339Nano-formatted
// start and end times (inclusive end, already adjusted by -OneTick) and returns
// the Kusto async operation ID or an error.
type SubmitFunc func(ctx context.Context, rule v1.SummaryRule, startTime, endTime string) (operationID string, err error)

// GetOperationStatusFunc checks the status of a Kusto async operation.
// It returns (state, shouldRetry, error). state is one of
// "Completed", "Failed", "InProgress", etc.
type GetOperationStatusFunc func(ctx context.Context, database, operationID string) (state string, shouldRetry bool, err error)

// DefaultMaxInFlight is the default number of concurrent backfill async
// operations when spec.backfill.maxInFlight is zero or unset.
const DefaultMaxInFlight = 1

// MaxActiveOperations caps the ActiveOperations slice to prevent unbounded
// status growth even when users set very large maxInFlight values.
const MaxActiveOperations = 20

// Process is the main entry point called by both the Ingestor and ADX Exporter
// on every reconcile/run cycle. It is safe to call repeatedly; it is a no-op
// when there is no backfill spec or the backfill has already completed.
//
// The function mutates rule.Status.Backfill in memory. The caller is
// responsible for persisting the status update.
func Process(
	ctx context.Context,
	rule *v1.SummaryRule,
	clk clock.Clock,
	submit SubmitFunc,
	getStatus GetOperationStatusFunc,
) {
	if rule == nil {
		return
	}
	if clk == nil {
		clk = clock.RealClock{}
	}
	defer syncBackfillCondition(rule)

	if rule.Spec.Backfill == nil {
		clearBackfillStatus(rule)
		return
	}

	spec := rule.Spec.Backfill
	if msg, ok := validateSpec(rule); !ok {
		setBackfillFailed(rule, spec.RequestID, rule.GetGeneration(), msg, clk)
		return
	}

	status := rule.Status.Backfill

	// New or changed request?
	if status == nil || status.RequestID != spec.RequestID {
		status = initBackfillStatus(spec, rule.GetGeneration())
		rule.Status.Backfill = status
	}

	// Terminal states are sticky until spec changes.
	if status.Phase == v1.BackfillPhaseCompleted || status.Phase == v1.BackfillPhaseFailed {
		return
	}

	// Generation pin: if the spec was edited mid-backfill, fail the request.
	if status.ObservedGeneration != rule.GetGeneration() {
		setBackfillFailed(rule, spec.RequestID, status.ObservedGeneration,
			fmt.Sprintf("SummaryRule spec changed during backfill (generation %d → %d); submit a new requestId to retry",
				status.ObservedGeneration, rule.GetGeneration()), clk)
		return
	}

	// Advance phase from Pending → Running on first cycle.
	if status.Phase == v1.BackfillPhasePending {
		status.Phase = v1.BackfillPhaseRunning
	}

	// 1. Poll active operations.
	if !pollActiveOperations(ctx, rule, getStatus, clk) {
		return
	}

	// 2. Submit new windows up to maxInFlight.
	submitNewWindows(ctx, rule, submit)

	// 3. Check for completion.
	checkCompletion(rule)
}

// initBackfillStatus creates a fresh BackfillStatus for a new request.
func initBackfillStatus(spec *v1.BackfillSpec, generation int64) *v1.BackfillStatus {
	return &v1.BackfillStatus{
		RequestID:          spec.RequestID,
		Phase:              v1.BackfillPhasePending,
		ObservedGeneration: generation,
		NextWindowStart:    spec.StartTime,
	}
}

// clearBackfillStatus clears non-terminal backfill status when the spec is
// removed. Terminal status is retained for observability even after spec
// removal, and will be replaced by the next backfill request.
func clearBackfillStatus(rule *v1.SummaryRule) {
	if rule == nil {
		return
	}
	if rule.Status.Backfill == nil {
		return
	}
	// Keep terminal statuses for observability; clear everything else.
	if rule.Status.Backfill.Phase != v1.BackfillPhaseCompleted &&
		rule.Status.Backfill.Phase != v1.BackfillPhaseFailed {
		rule.Status.Backfill = nil
	}
}

// effectiveMaxInFlight returns the bounded maxInFlight value.
func effectiveMaxInFlight(spec *v1.BackfillSpec) int {
	m := spec.MaxInFlight
	if m <= 0 {
		m = DefaultMaxInFlight
	}
	if m > MaxActiveOperations {
		m = MaxActiveOperations
	}
	return m
}

func inFlightCount(status *v1.BackfillStatus) int {
	if status == nil {
		return 0
	}

	count := 0
	for _, op := range status.ActiveOperations {
		if op.OperationID != "" {
			count++
		}
	}
	return count
}

// pollActiveOperations checks each in-flight operation and handles completion.
// Retryable operations are re-queued as backlog entries. Non-retryable failures
// fail the backfill request so the user can fix the underlying issue.
func pollActiveOperations(ctx context.Context, rule *v1.SummaryRule, getStatus GetOperationStatusFunc, clk clock.Clock) bool {
	status := rule.Status.Backfill
	if status == nil || len(status.ActiveOperations) == 0 {
		return true
	}

	remaining := make([]v1.BackfillOperation, 0, len(status.ActiveOperations))
	for _, op := range status.ActiveOperations {
		if op.OperationID == "" {
			// Backlog entry (submission failed previously) — keep for retry in submitNewWindows.
			remaining = append(remaining, op)
			continue
		}

		operationID := op.OperationID
		state, shouldRetry, err := getStatus(ctx, rule.Spec.Database, operationID)
		if err != nil {
			logger.Errorf("backfill: failed to poll operation %s: %v", operationID, err)
			remaining = append(remaining, op)
			continue
		}
		if shouldRetry {
			status.RetriedWindows++
			op.OperationID = ""
			remaining = append(remaining, op)
			logger.Warnf("backfill: operation %s marked retryable for %s [%s, %s], re-queued for retry",
				operationID, rule.Name, op.StartTime, op.EndTime)
			continue
		}

		switch state {
		case "Completed":
			status.CompletedWindows++
			logger.Infof("backfill: operation %s completed for %s [%s, %s]",
				operationID, rule.Name, op.StartTime, op.EndTime)
		case "Failed":
			setBackfillFailed(rule, status.RequestID, status.ObservedGeneration,
				fmt.Sprintf("Backfill window [%s, %s] failed and is not retryable (operation %s)",
					op.StartTime, op.EndTime, operationID), clk)
			return false
		default:
			// Still in progress.
			remaining = append(remaining, op)
		}
	}
	status.ActiveOperations = remaining
	return true
}

// submitNewWindows generates and submits interval-sized windows from the
// cursor (NextWindowStart) until we hit maxInFlight or exhaust the range.
func submitNewWindows(ctx context.Context, rule *v1.SummaryRule, submit SubmitFunc) {
	spec := rule.Spec.Backfill
	status := rule.Status.Backfill
	if spec == nil || status == nil {
		return
	}

	maxInFlight := effectiveMaxInFlight(spec)
	interval := rule.Spec.Interval.Duration

	// First, retry any backlog entries (empty OperationID), respecting maxInFlight.
	for i := range status.ActiveOperations {
		if inFlightCount(status) >= maxInFlight {
			break
		}
		op := &status.ActiveOperations[i]
		if op.OperationID != "" {
			continue
		}
		opID, err := submit(ctx, *rule, op.StartTime, op.EndTime)
		if err != nil {
			logger.Warnf("backfill: retry submission failed for %s [%s, %s]: %v",
				rule.Name, op.StartTime, op.EndTime, err)
			continue
		}
		op.OperationID = opID
		status.SubmittedWindows++
	}

	// Then generate new windows from the cursor.
	for inFlightCount(status) < maxInFlight {
		windowStart := status.NextWindowStart.Time.UTC()
		windowEnd := windowStart.Add(interval)

		if !windowEnd.Before(spec.EndTime.Time) && !windowEnd.Equal(spec.EndTime.Time) {
			// Window exceeds the backfill range — we're done generating.
			break
		}

		// Inclusive end for Kusto query (same convention as regular SummaryRule execution).
		queryEnd := windowEnd.Add(-kustoutil.OneTick)

		startStr := windowStart.Format(time.RFC3339Nano)
		endStr := queryEnd.UTC().Format(time.RFC3339Nano)

		// Guard against overlapping windows: skip if this window is already tracked.
		if windowAlreadyTracked(status, startStr) {
			status.NextWindowStart = metav1.Time{Time: windowEnd}
			continue
		}

		bop := v1.BackfillOperation{
			StartTime: startStr,
			EndTime:   endStr,
		}

		opID, err := submit(ctx, *rule, startStr, endStr)
		if err != nil {
			logger.Warnf("backfill: submission failed for %s [%s, %s]: %v",
				rule.Name, startStr, endStr, err)
			// Store as backlog (empty OperationID) so we can retry next cycle.
			status.ActiveOperations = append(status.ActiveOperations, bop)
		} else {
			bop.OperationID = opID
			status.ActiveOperations = append(status.ActiveOperations, bop)
			status.SubmittedWindows++
		}

		// Always advance the cursor so we don't re-generate the same window.
		status.NextWindowStart = metav1.Time{Time: windowEnd}
	}
}

// windowAlreadyTracked returns true if an active operation with the given
// startTime already exists. This prevents overlapping window submissions.
func windowAlreadyTracked(status *v1.BackfillStatus, startTime string) bool {
	for _, op := range status.ActiveOperations {
		if op.StartTime == startTime {
			return true
		}
	}
	return false
}

// checkCompletion moves the backfill to Completed when there are no more
// windows to generate and no active operations remaining.
func checkCompletion(rule *v1.SummaryRule) {
	spec := rule.Spec.Backfill
	status := rule.Status.Backfill
	if spec == nil || status == nil {
		return
	}

	allWindowsGenerated := !status.NextWindowStart.Time.Before(spec.EndTime.Time)

	// Also check if the next potential window would exceed the range — this
	// handles the sub-interval case where NextWindowStart hasn't advanced.
	if !allWindowsGenerated {
		nextWindowEnd := status.NextWindowStart.Time.Add(rule.Spec.Interval.Duration)
		if nextWindowEnd.After(spec.EndTime.Time) && !nextWindowEnd.Equal(spec.EndTime.Time) {
			allWindowsGenerated = true
		}
	}

	noActiveOps := len(status.ActiveOperations) == 0

	if allWindowsGenerated && noActiveOps {
		status.Phase = v1.BackfillPhaseCompleted
		status.Message = fmt.Sprintf("Backfill completed: %d windows submitted, %d completed, %d retries",
			status.SubmittedWindows, status.CompletedWindows, status.RetriedWindows)
	}
}

func validateSpec(rule *v1.SummaryRule) (string, bool) {
	spec := rule.Spec.Backfill
	switch {
	case spec == nil:
		return "Backfill spec is missing", false
	case spec.RequestID == "":
		return "RequestID must be set", false
	case spec.StartTime.IsZero():
		return "StartTime must be set", false
	case spec.EndTime.IsZero():
		return "EndTime must be set", false
	case !spec.EndTime.Time.After(spec.StartTime.Time):
		return "EndTime must be after StartTime", false
	case rule.Spec.Interval.Duration <= 0:
		return "Interval must be positive", false
	case spec.EndTime.Time.Sub(spec.StartTime.Time) < rule.Spec.Interval.Duration:
		return fmt.Sprintf("Backfill range must cover at least one full interval of %s", rule.Spec.Interval.Duration), false
	case spec.EndTime.Time.Sub(spec.StartTime.Time)%rule.Spec.Interval.Duration != 0:
		return fmt.Sprintf("Backfill range duration must be an exact multiple of interval %s", rule.Spec.Interval.Duration), false
	default:
		return "", true
	}
}

func failedNextWindowStart(rule *v1.SummaryRule, clk clock.Clock) metav1.Time {
	if rule != nil && rule.Status.Backfill != nil && !rule.Status.Backfill.NextWindowStart.IsZero() {
		return rule.Status.Backfill.NextWindowStart
	}
	if rule != nil && rule.Spec.Backfill != nil && !rule.Spec.Backfill.StartTime.IsZero() {
		return rule.Spec.Backfill.StartTime
	}
	return metav1.NewTime(clk.Now().UTC())
}

func syncBackfillCondition(rule *v1.SummaryRule) {
	if rule == nil {
		return
	}
	if rule.Status.Backfill == nil {
		meta.RemoveStatusCondition(&rule.Status.Conditions, v1.ConditionBackfill)
		return
	}

	status := metav1.ConditionUnknown
	switch rule.Status.Backfill.Phase {
	case v1.BackfillPhaseCompleted:
		status = metav1.ConditionTrue
	case v1.BackfillPhaseFailed:
		status = metav1.ConditionFalse
	}

	message := rule.Status.Backfill.Message
	if message == "" {
		message = fmt.Sprintf("Backfill %s", rule.Status.Backfill.Phase)
	}

	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type:               v1.ConditionBackfill,
		Status:             status,
		Reason:             string(rule.Status.Backfill.Phase),
		Message:            message,
		ObservedGeneration: rule.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	})
}

// setBackfillFailed sets the backfill status to Failed with a message.
func setBackfillFailed(rule *v1.SummaryRule, requestID string, generation int64, message string, clk clock.Clock) {
	if rule == nil {
		return
	}
	status := &v1.BackfillStatus{}
	if rule.Status.Backfill != nil {
		status = rule.Status.Backfill.DeepCopy()
	}
	status.RequestID = requestID
	status.Phase = v1.BackfillPhaseFailed
	status.ObservedGeneration = generation
	status.NextWindowStart = failedNextWindowStart(rule, clk)
	status.ActiveOperations = nil
	status.Message = message
	rule.Status.Backfill = status
}
