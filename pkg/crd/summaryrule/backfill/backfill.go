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
	defer syncBackfillCondition(rule)

	if rule.Spec.Backfill == nil {
		clearBackfillStatus(rule)
		return
	}

	spec := rule.Spec.Backfill
	if msg, ok := validateSpec(spec); !ok {
		setBackfillFailed(rule, spec.RequestID, rule.GetGeneration(), msg)
		return
	}
	if !spec.EndTime.Time.After(spec.StartTime.Time) {
		setBackfillFailed(rule, spec.RequestID, rule.GetGeneration(), "EndTime must be after StartTime")
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
				status.ObservedGeneration, rule.GetGeneration()))
		return
	}

	// Advance phase from Pending → Running on first cycle.
	if status.Phase == v1.BackfillPhasePending {
		status.Phase = v1.BackfillPhaseRunning
	}

	// 1. Poll active operations.
	pollActiveOperations(ctx, rule, getStatus)

	// 2. Submit new windows up to maxInFlight.
	submitNewWindows(ctx, rule, clk, submit)

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

// clearBackfillStatus clears the backfill status when the spec is removed,
// unless the status records a completed or failed backfill (which we keep
// for observability until the user explicitly removes the spec).
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

// pollActiveOperations checks each in-flight operation and handles completion.
// Failed operations are re-queued as backlog entries (OperationID cleared) so
// they will be retried — this guarantees no intervals are permanently skipped.
func pollActiveOperations(ctx context.Context, rule *v1.SummaryRule, getStatus GetOperationStatusFunc) {
	status := rule.Status.Backfill
	if status == nil || len(status.ActiveOperations) == 0 {
		return
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
			logger.Warnf("backfill: operation %s marked retryable for %s [%s, %s), re-queued for retry",
				operationID, rule.Name, op.StartTime, op.EndTime)
			continue
		}

		switch state {
		case "Completed":
			status.CompletedWindows++
			logger.Infof("backfill: operation %s completed for %s [%s, %s)",
				operationID, rule.Name, op.StartTime, op.EndTime)
		case "Failed":
			// Re-queue as backlog so the window is retried — no interval is skipped.
			status.RetriedWindows++
			op.OperationID = ""
			remaining = append(remaining, op)
			logger.Warnf("backfill: operation failed for %s [%s, %s), re-queued for retry",
				rule.Name, op.StartTime, op.EndTime)
		default:
			// Still in progress.
			remaining = append(remaining, op)
		}
	}
	status.ActiveOperations = remaining
}

// submitNewWindows generates and submits interval-sized windows from the
// cursor (NextWindowStart) until we hit maxInFlight or exhaust the range.
func submitNewWindows(ctx context.Context, rule *v1.SummaryRule, clk clock.Clock, submit SubmitFunc) {
	spec := rule.Spec.Backfill
	status := rule.Status.Backfill
	if spec == nil || status == nil {
		return
	}

	maxInFlight := effectiveMaxInFlight(spec)
	interval := rule.Spec.Interval.Duration

	// First, retry any backlog entries (empty OperationID).
	for i := range status.ActiveOperations {
		op := &status.ActiveOperations[i]
		if op.OperationID != "" {
			continue
		}
		opID, err := submit(ctx, *rule, op.StartTime, op.EndTime)
		if err != nil {
			logger.Warnf("backfill: retry submission failed for %s [%s, %s): %v",
				rule.Name, op.StartTime, op.EndTime, err)
			continue
		}
		op.OperationID = opID
		status.SubmittedWindows++
	}

	// Then generate new windows from the cursor.
	for len(status.ActiveOperations) < maxInFlight {
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
			logger.Warnf("backfill: submission failed for %s [%s, %s): %v",
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

func validateSpec(spec *v1.BackfillSpec) (string, bool) {
	switch {
	case spec == nil:
		return "Backfill spec is missing", false
	case spec.RequestID == "":
		return "RequestID must be set", false
	case spec.StartTime.IsZero():
		return "StartTime must be set", false
	case spec.EndTime.IsZero():
		return "EndTime must be set", false
	default:
		return "", true
	}
}

func failedNextWindowStart(rule *v1.SummaryRule) metav1.Time {
	if rule != nil && rule.Status.Backfill != nil && !rule.Status.Backfill.NextWindowStart.IsZero() {
		return rule.Status.Backfill.NextWindowStart
	}
	if rule != nil && rule.Spec.Backfill != nil && !rule.Spec.Backfill.StartTime.IsZero() {
		return rule.Spec.Backfill.StartTime
	}
	return metav1.NewTime(time.Now().UTC())
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
func setBackfillFailed(rule *v1.SummaryRule, requestID string, generation int64, message string) {
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
	status.NextWindowStart = failedNextWindowStart(rule)
	status.Message = message
	rule.Status.Backfill = status
}
