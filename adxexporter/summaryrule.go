package adxexporter

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	crdownership "github.com/Azure/adx-mon/pkg/crd/summaryrule"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Exporter-local constants to avoid magic values scattered in the reconciler.
const (
	adoptRequeue  = 5 * time.Second
	submitTimeout = 5 * time.Minute
	pollTimeout   = 2 * time.Minute

	stateCompleted = "Completed"
	stateFailed    = "Failed"

	autoSuspendMinDuration        = 15 * time.Minute
	autoSuspendIntervalMultiplier = 3
	defaultSuspendedMessage       = "SummaryRule execution is suspended"
)

var throttleErrorIndicators = []string{
	"e_query_throttled",
	"query throttled",
	"too many requests",
	"request is throttled",
	"rate limit",
	"status code 429",
	"http status code 429",
}

// SummaryRuleReconciler will reconcile SummaryRule objects.
type SummaryRuleReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Configuration
	ClusterLabels map[string]string
	KustoClusters map[string]string // database name -> endpoint URL

	KustoExecutors map[string]KustoExecutor // per-database Kusto clients supporting Query and Mgmt
	Clock          clock.Clock

	AutoSuspendEvaluator func(*adxmonv1.SummaryRule) bool
}

// Reconcile processes a single SummaryRule; placeholder implementation for now.
func (r *SummaryRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the SummaryRule instance
	var rule adxmonv1.SummaryRule
	if err := r.Get(ctx, req.NamespacedName, &rule); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Deleted, nothing to do
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Gate by database, criteria (map + expression), and adoption
	if !r.isDatabaseManaged(&rule) {
		return ctrl.Result{}, nil
	}

	proceed, reason, message, exprErr := EvaluateExecutionCriteria(rule.Spec.Criteria, rule.Spec.CriteriaExpression, r.ClusterLabels)
	if exprErr != nil {
		// This error means the criteria expression was invalid (parse/type/eval error).
		// We'll treat this as a non-match to prevent execution, but log the error for visibility.
		logger.Errorf("Criteria expression error for SummaryRule %s/%s: %v", req.Namespace, req.Name, exprErr)
		return ctrl.Result{}, nil
	}
	condStatus := metav1.ConditionFalse
	if proceed {
		condStatus = metav1.ConditionTrue
	}
	cond := metav1.Condition{Type: adxmonv1.ConditionCriteria, Status: condStatus, Reason: reason, Message: message, ObservedGeneration: rule.GetGeneration(), LastTransitionTime: metav1.Now()}
	if meta.SetStatusCondition(&rule.Status.Conditions, cond) {
		_ = r.Status().Update(ctx, &rule) // best-effort update
	}
	if !proceed {
		return ctrl.Result{}, nil
	}

	suspended, suspendMsg := r.evaluateSuspension(&rule)
	if suspended {
		r.markSuspendedStatus(&rule, suspendMsg)
	} else {
		if requeue, handled := r.adoptIfDesired(ctx, &rule); handled {
			return requeue, nil
		}
	}

	// Minimal submission path: compute window and submit async if it's time
	if !suspended && rule.ShouldSubmitRule(r.Clock) {
		windowStart, windowEnd := rule.NextExecutionWindow(r.Clock)

		// Use inclusive end for the query by subtracting OneTick (100ns) to avoid boundary issues
		queryEnd := windowEnd.Add(-kustoutil.OneTick)

		// Submit rule asynchronously
		opID, err := r.submitRule(ctx, rule, windowStart, queryEnd)

		// Always set async operation entry with the submitted (or attempted) window
		asyncOp := adxmonv1.AsyncOperation{
			OperationId: opID,
			StartTime:   windowStart.UTC().Format(time.RFC3339Nano),
			EndTime:     queryEnd.UTC().Format(time.RFC3339Nano),
		}
		rule.SetAsyncOperation(asyncOp)

		// Advance last execution time to the window end (original, exclusive end)
		rule.SetLastExecutionTime(windowEnd)

		// Update in-memory condition reflecting submission result; persist once at end
		r.updateSummaryRuleStatus(&rule, err)
	}

	// Backfill any missing async operations based on last successful execution time
	if !suspended {
		rule.BackfillAsyncOperations(r.Clock)
	}

	// Track/advance outstanding async operations (including backlog windows)
	r.trackAsyncOperations(ctx, &rule, suspended)

	// Persist any changes made to async operation conditions or timestamps
	if err := r.Status().Update(ctx, &rule); err != nil {
		// Not fatal; next reconcile will retry
		logger.Warnf("Failed to persist SummaryRule async status %s/%s: %v", rule.Namespace, rule.Name, err)
	}

	// Emit health / status log (best-effort, after status update to reflect latest view)
	r.emitHealthLog(&rule, suspended)

	// Requeue based on rule interval and async polling needs
	return ctrl.Result{RequeueAfter: nextRequeue(&rule)}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *SummaryRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize defaults
	if r.Clock == nil {
		r.Clock = clock.RealClock{}
	}
	if r.KustoExecutors == nil {
		r.KustoExecutors = make(map[string]KustoExecutor)
	}

	// Prepare per-database Kusto executors
	if err := r.initializeQueryExecutors(); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.SummaryRule{}).
		Complete(r)
}

// nextRequeue decides the next reconcile interval:
// - Base: rule interval
// - If there are inflight/backlog async operations, prefer SummaryRuleAsyncOperationPollInterval when shorter
// - Fallback minimum of 1 minute to avoid hot loops on misconfiguration
// asyncPollInterval decides whether to use the async operation poll cadence.
// Returns (pollInterval, ok) where ok indicates we should override the base interval.
func asyncPollInterval(rule *adxmonv1.SummaryRule) (time.Duration, bool) {
	if len(rule.GetAsyncOperations()) == 0 {
		return 0, false
	}
	poll := adxmonv1.SummaryRuleAsyncOperationPollInterval
	if poll <= 0 {
		return 0, false
	}
	if base := rule.Spec.Interval.Duration; base > 0 && base <= poll { // base already sooner or equal
		return 0, false
	}
	return poll, true
}

func nextRequeue(rule *adxmonv1.SummaryRule) time.Duration {
	if poll, ok := asyncPollInterval(rule); ok {
		return poll
	}
	base := rule.Spec.Interval.Duration
	if base <= 0 {
		return time.Minute
	}
	return base
}

// isDatabaseManaged returns true if this reconciler has an executor for the rule's database
func (r *SummaryRuleReconciler) isDatabaseManaged(rule *adxmonv1.SummaryRule) bool {
	_, ok := r.KustoExecutors[rule.Spec.Database]
	return ok
}

func (r *SummaryRuleReconciler) evaluateSuspension(rule *adxmonv1.SummaryRule) (bool, string) {
	if r.AutoSuspendEvaluator != nil {
		if r.AutoSuspendEvaluator(rule) {
			return true, ""
		}
		return false, ""
	}
	return r.shouldAutoSuspend(rule)
}

func (r *SummaryRuleReconciler) shouldAutoSuspend(rule *adxmonv1.SummaryRule) (bool, string) {
	clk := r.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}

	threshold := autoSuspendThresholdForRule(rule)
	if threshold <= 0 {
		return false, ""
	}

	failed := meta.FindStatusCondition(rule.Status.Conditions, adxmonv1.ConditionFailed)
	if failed == nil || failed.Status != metav1.ConditionTrue {
		return false, ""
	}
	if failed.ObservedGeneration != rule.GetGeneration() {
		return false, ""
	}
	if !isThrottleFailure(failed) {
		return false, ""
	}
	if failed.LastTransitionTime.IsZero() {
		return false, ""
	}

	completed := meta.FindStatusCondition(rule.Status.Conditions, adxmonv1.ConditionCompleted)
	if completed != nil && completed.Status == metav1.ConditionTrue {
		if completed.LastTransitionTime.After(failed.LastTransitionTime.Time) {
			return false, ""
		}
	}

	elapsed := clk.Now().Sub(failed.LastTransitionTime.Time)
	if elapsed < threshold {
		return false, ""
	}

	message := fmt.Sprintf("%s after sustained throttling (elapsed=%s, threshold=%s)", defaultSuspendedMessage, formatDuration(elapsed), formatDuration(threshold))
	return true, message
}

func (r *SummaryRuleReconciler) markSuspendedStatus(rule *adxmonv1.SummaryRule, message string) {
	if message == "" {
		message = defaultSuspendedMessage
	}
	condition := metav1.Condition{
		Type:               adxmonv1.SummaryRuleOwner,
		Status:             metav1.ConditionFalse,
		Reason:             "Suspended",
		Message:            message,
		ObservedGeneration: rule.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}
	existing := meta.FindStatusCondition(rule.Status.Conditions, adxmonv1.SummaryRuleOwner)
	if existing != nil && existing.Status == condition.Status && existing.Reason == condition.Reason && existing.Message == condition.Message {
		return
	}
	meta.SetStatusCondition(&rule.Status.Conditions, condition)
}

func autoSuspendThresholdForRule(rule *adxmonv1.SummaryRule) time.Duration {
	interval := rule.Spec.Interval.Duration
	if interval <= 0 {
		return autoSuspendMinDuration
	}
	base := time.Duration(autoSuspendIntervalMultiplier) * interval
	if base < autoSuspendMinDuration {
		return autoSuspendMinDuration
	}
	return base
}

func isThrottleFailure(cond *metav1.Condition) bool {
	if cond == nil {
		return false
	}
	if cond.Status != metav1.ConditionTrue {
		return false
	}
	if cond.Message == "" {
		return false
	}
	lower := strings.ToLower(cond.Message)
	for _, indicator := range throttleErrorIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	// Prefer second precision to avoid overly long fractional strings.
	if d < time.Second {
		return d.String()
	}
	return d.Truncate(time.Second).String()
}

// (criteria match handled inline for clarity)

// adoptIfDesired tries to claim ownership if requested and safe. It returns (result, handled)
// where handled indicates the reconcile should return immediately with the given result.
func (r *SummaryRuleReconciler) adoptIfDesired(ctx context.Context, rule *adxmonv1.SummaryRule) (ctrl.Result, bool) {
	if crdownership.IsOwnedBy(rule, adxmonv1.SummaryRuleOwnerADXExporter) {
		return ctrl.Result{}, false
	}

	// Auto-adoption policy: if rule is currently (explicitly or implicitly) owned by the ingestor
	// (missing annotation or annotation=ingestor) we attempt to adopt it even without a desired-owner
	// annotation. This enables seamless migration when the exporter is introduced.
	wantsExporter := crdownership.WantsOwner(rule, adxmonv1.SummaryRuleOwnerADXExporter)
	ingestorOwned := crdownership.IsOwnedBy(rule, adxmonv1.SummaryRuleOwnerIngestor)
	if wantsExporter || ingestorOwned {
		if ok, reason := crdownership.SafeToAdopt(rule, r.Clock); ok {
			if err := crdownership.PatchClaim(ctx, r.Client, rule, adxmonv1.SummaryRuleOwnerADXExporter); err != nil {
				logger.Logger().Error("Failed to claim ownership",
					"crd_name", fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
					"event", "ownership_adopt",
					"status", "FAILURE",
					"auto", ingestorOwned && !wantsExporter,
					"error", err.Error(),
				)
				// Requeue to handle potential transient API errors (network, apiserver disruption, etc.).
				return ctrl.Result{RequeueAfter: adoptRequeue}, true
			} else {
				logger.Logger().Info("Adopted SummaryRule",
					"crd_name", fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
					"event", "ownership_adopt",
					"status", "SUCCESS",
					"safe_reason", reason,
					"desired_owner", adxmonv1.SummaryRuleOwnerADXExporter,
					"auto", ingestorOwned && !wantsExporter,
				)
				return ctrl.Result{RequeueAfter: adoptRequeue}, true
			}
		} else {
			// Not yet safe; requeue so we can attempt adoption soon.
			return ctrl.Result{RequeueAfter: adoptRequeue}, true
		}
	}

	// Unknown/unsupported owner annotation: skip processing (fail closed)
	return ctrl.Result{}, true
}

// initializeQueryExecutors creates per-database executors for SummaryRule processing.
func (r *SummaryRuleReconciler) initializeQueryExecutors() error {
	for database, endpoint := range r.KustoClusters {
		// Reuse the shared Kusto client capable of both Query and Mgmt
		kustoClient, err := NewKustoClient(endpoint, database)
		if err != nil {
			return err
		}
		r.KustoExecutors[database] = kustoClient
	}
	return nil
}

// submitRule executes the SummaryRule body using an async .set-or-append into the target table
// with _startTime/_endTime substitutions and returns the Kusto operation ID.
func (r *SummaryRuleReconciler) submitRule(ctx context.Context, rule adxmonv1.SummaryRule, start, end time.Time) (string, error) {
	exec := r.KustoExecutors[rule.Spec.Database]
	if exec == nil {
		return "", fmt.Errorf("no Kusto executor for database %s", rule.Spec.Database)
	}

	// Build query with substitutions
	body := kustoutil.ApplySubstitutions(rule.Spec.Body, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano), r.ClusterLabels)

	// Execute asynchronously: .set-or-append async <table> <| <body>
	stmt := kql.New(".set-or-append async ").AddUnsafe(rule.Spec.Table).AddLiteral(" with (extend_schema=true) <| ").AddUnsafe(body)

	// Apply a safety timeout to prevent hanging calls
	tCtx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()

	res, err := exec.Mgmt(tCtx, stmt)
	if err != nil {
		return "", fmt.Errorf("failed to execute summary rule %s.%s: %w", rule.Spec.Database, rule.Name, err)
	}

	return operationIDFromResult(res)
}

// updateSummaryRuleStatus sets the primary condition with friendly Kusto error parsing
func (r *SummaryRuleReconciler) updateSummaryRuleStatus(rule *adxmonv1.SummaryRule, err error) {
	condition := metav1.Condition{
		Type:               adxmonv1.SummaryRuleOwner,
		Status:             metav1.ConditionTrue,
		Reason:             "ExecutionSuccessful",
		Message:            "Rule submitted successfully",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: rule.GetGeneration(),
	}
	if err != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ExecutionFailed"
		condition.Message = kustoutil.ParseError(err)
	}
	rule.SetCondition(condition)

	// Add/Update Completed and Failed shared conditions for last submission attempt using meta.SetStatusCondition
	if err == nil {
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               adxmonv1.ConditionCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             "ExecutionSuccessful",
			Message:            "Most recent submission succeeded",
			ObservedGeneration: rule.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               adxmonv1.ConditionFailed,
			Status:             metav1.ConditionFalse,
			Reason:             "ExecutionSuccessful",
			Message:            "Most recent submission succeeded",
			ObservedGeneration: rule.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
	} else {
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               adxmonv1.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "ExecutionFailed",
			Message:            condition.Message,
			ObservedGeneration: rule.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               adxmonv1.ConditionCompleted,
			Status:             metav1.ConditionFalse,
			Reason:             "ExecutionFailed",
			Message:            condition.Message,
			ObservedGeneration: rule.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
	}
}

// operationIDFromResult extracts a single string cell (operation id) from the RowIterator
func operationIDFromResult(iter *kusto.RowIterator) (string, error) {
	defer iter.Stop()
	for {
		row, errInline, errFinal := iter.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return "", fmt.Errorf("failed to retrieve operation ID: %v", errFinal)
		}
		if len(row.Values) != 1 {
			return "", fmt.Errorf("unexpected number of values in row: %d", len(row.Values))
		}
		return row.Values[0].String(), nil
	}
	return "", nil
}

// AsyncOperationStatus mirrors the schema returned by .show operations
type AsyncOperationStatus struct {
	OperationId   string    `kusto:"OperationId"`
	LastUpdatedOn time.Time `kusto:"LastUpdatedOn"`
	State         string    `kusto:"State"`
	ShouldRetry   float64   `kusto:"ShouldRetry"`
	Status        string    `kusto:"Status"`
}

// getOperation queries Kusto for a specific async operation by ID, returning latest status or nil if not found
func (r *SummaryRuleReconciler) getOperation(ctx context.Context, database string, operationId string) (*AsyncOperationStatus, error) {
	exec := r.KustoExecutors[database]
	if exec == nil {
		return nil, fmt.Errorf("no Kusto executor for database %s", database)
	}

	// Management commands do not support parameters; build safe literal with AddString
	stmt := kql.New(`.show operations`).
		AddLiteral(" | where OperationId == ").AddString(operationId).
		AddLiteral(" | summarize arg_max(LastUpdatedOn, OperationId, State, ShouldRetry, Status)").
		AddLiteral(" | project LastUpdatedOn, OperationId = tostring(OperationId), State, ShouldRetry = todouble(ShouldRetry), Status")

	// Apply a shorter timeout for status polling
	tCtx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	rows, err := exec.Mgmt(tCtx, stmt)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve operation %s: %w", operationId, err)
	}
	defer rows.Stop()

	for {
		row, errInline, errFinal := rows.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return nil, fmt.Errorf("failed to retrieve operation %s: %v", operationId, errFinal)
		}

		var status AsyncOperationStatus
		if err := row.ToStruct(&status); err != nil {
			return nil, fmt.Errorf("failed to parse operation %s: %v", operationId, err)
		}
		if status.State != "" {
			return &status, nil
		}
	}
	return nil, nil
}

// trackAsyncOperations iterates current async ops and processes completed/retry/backlog entries
func (r *SummaryRuleReconciler) trackAsyncOperations(ctx context.Context, rule *adxmonv1.SummaryRule, suspended bool) {
	// Reviewer note addressed: this method previously handled backlog lookup, status polling,
	// completion, and retry logic inline. It is now a simple iterator delegating responsibilities
	// to focused helpers for readability & testability.
	for _, op := range rule.GetAsyncOperations() {
		r.processAsyncOperation(ctx, rule, op, suspended)
	}
}

// processAsyncOperation routes an async operation to the correct handler based on its state.
func (r *SummaryRuleReconciler) processAsyncOperation(ctx context.Context, rule *adxmonv1.SummaryRule, op adxmonv1.AsyncOperation, suspended bool) {
	if suspended && op.OperationId == "" {
		return
	}
	if op.OperationId == "" { // backlog entry (submission previously failed)
		r.processBacklogOperation(ctx, rule, op)
		return
	}

	status := r.fetchOperationStatus(ctx, rule, op)
	if status == nil { // error already logged or not found
		return
	}

	switch {
	case status.State == stateCompleted || status.State == stateFailed:
		r.handleCompletedOperation(ctx, rule, *status)
	case status.ShouldRetry != 0:
		if suspended {
			logger.Infof("Skipping retry for async operation %s on suspended rule %s.%s", status.OperationId, rule.Spec.Database, rule.Name)
			return
		}
		r.handleRetryOperation(ctx, rule, op, *status)
	}
}

// fetchOperationStatus gets the status for an operation, logging errors consistently.
func (r *SummaryRuleReconciler) fetchOperationStatus(ctx context.Context, rule *adxmonv1.SummaryRule, op adxmonv1.AsyncOperation) *AsyncOperationStatus {
	status, err := r.getOperation(ctx, rule.Spec.Database, op.OperationId)
	if err != nil {
		logger.Errorf("Failed to query operation %s: %v", op.OperationId, err)
		return nil
	}
	if status == nil && logger.IsDebug() {
		logger.Debugf("Async operation %s not found in .show operations", op.OperationId)
	}
	return status
}

// handleRetryOperation resubmits a window and swaps the tracked OperationId if a new one is created
func (r *SummaryRuleReconciler) handleRetryOperation(ctx context.Context, rule *adxmonv1.SummaryRule, operation adxmonv1.AsyncOperation, status AsyncOperationStatus) {
	logger.Infof("Async operation %s for rule %s.%s is marked for retry, retrying submission", status.OperationId, rule.Spec.Database, rule.Name)

	// Parse window times from operation
	start, errStart := time.Parse(time.RFC3339Nano, operation.StartTime)
	end, errEnd := time.Parse(time.RFC3339Nano, operation.EndTime)
	if errStart != nil || errEnd != nil {
		logger.Warnf("Failed to parse operation window for retry: start=%s end=%s", operation.StartTime, operation.EndTime)
		return
	}

	if newId, err := r.submitRule(ctx, *rule, start, end); err == nil && newId != operation.OperationId {
		// Remove old operation and track new one with same window
		rule.RemoveAsyncOperation(operation.OperationId)
		operation.OperationId = newId
		rule.SetAsyncOperation(operation)
	}
}

// handleCompletedOperation removes completed op and updates status on failure
func (r *SummaryRuleReconciler) handleCompletedOperation(ctx context.Context, rule *adxmonv1.SummaryRule, status AsyncOperationStatus) {
	if status.State == stateFailed {
		// Build a concise error message
		var err error
		if status.Status != "" {
			msg := status.Status
			if len(msg) > 200 {
				msg = msg[:200]
			}
			err = fmt.Errorf("async operation %s failed: %s", status.OperationId, msg)
		} else {
			err = fmt.Errorf("async operation %s failed", status.OperationId)
		}
		// Update in-memory condition; persist once at end of reconcile
		r.updateSummaryRuleStatus(rule, err)
	}
	// Remove completed op from tracking
	rule.RemoveAsyncOperation(status.OperationId)
}

// processBacklogOperation attempts to submit a previously failed window and advance timestamp safely
func (r *SummaryRuleReconciler) processBacklogOperation(ctx context.Context, rule *adxmonv1.SummaryRule, operation adxmonv1.AsyncOperation) {
	// Parse the stored window; if invalid we can't safely resubmit.
	start, errStart := time.Parse(time.RFC3339Nano, operation.StartTime)
	end, errEnd := time.Parse(time.RFC3339Nano, operation.EndTime)
	if errStart != nil || errEnd != nil {
		logger.Warnf("Failed to parse backlog operation window: start=%s end=%s", operation.StartTime, operation.EndTime)
		return
	}

	opID, err := r.submitRule(ctx, *rule, start, end)
	if err != nil {
		// Keep backlog entry (OperationId empty) for future attempts
		return
	}
	operation.OperationId = opID
	r.updateLastExecutionTimeIfForward(rule, end)
	rule.SetAsyncOperation(operation)
}

// updateLastExecutionTimeIfForward advances LastExecutionTime if the provided inclusiveEnd corresponds
// to a strictly newer (exclusive) window end, preserving forward-only progression semantics.
func (r *SummaryRuleReconciler) updateLastExecutionTimeIfForward(rule *adxmonv1.SummaryRule, inclusiveEnd time.Time) {
	originalWindowEnd := inclusiveEnd.Add(kustoutil.OneTick) // convert back to exclusive end
	current := rule.GetLastExecutionTime()
	if current == nil || originalWindowEnd.After(*current) {
		rule.SetLastExecutionTime(originalWindowEnd)
	}
}

// emitHealthLog produces a single structured log line summarizing whether the rule is considered
// "working". A rule is working when: owned (or already adopted) by exporter, database managed,
// criteria matched, no stale async operations, no detected interval gap beyond one interval, and
// (if due) submission succeeded or (if not due) simply waiting.
// This avoids expensive downstream Kusto correlation for basic liveness checks.
func (r *SummaryRuleReconciler) emitHealthLog(rule *adxmonv1.SummaryRule, suspended bool) {
	// Only log for rules the exporter owns or is attempting to adopt; skip others to reduce noise.
	owned := crdownership.IsOwnedBy(rule, adxmonv1.SummaryRuleOwnerADXExporter)
	wantsExporter := crdownership.WantsOwner(rule, adxmonv1.SummaryRuleOwnerADXExporter)
	if !owned && !wantsExporter && !crdownership.IsOwnedBy(rule, adxmonv1.SummaryRuleOwnerIngestor) {
		return
	}

	// Pre-calculate basic properties
	interval := rule.Spec.Interval.Duration
	if interval <= 0 {
		interval = time.Minute
	}
	var ingestionDelay time.Duration
	if rule.Spec.IngestionDelay != nil {
		ingestionDelay = rule.Spec.IngestionDelay.Duration
	}

	lastExec := rule.GetLastExecutionTime()
	now := r.Clock.Now()
	effectiveNow := now.Add(-ingestionDelay)

	asyncOps := rule.GetAsyncOperations()
	var inflight, backlog int
	var oldestInflightStart *time.Time
	for _, op := range asyncOps {
		if op.OperationId == "" {
			backlog++
		} else {
			inflight++
			// Parse start for stale detection
			if op.StartTime != "" {
				if t, err := time.Parse(time.RFC3339Nano, op.StartTime); err == nil {
					if oldestInflightStart == nil || t.Before(*oldestInflightStart) {
						oldestInflightStart = &t
					}
				}
			}
		}
	}

	// Detect stale async (heuristic): oldest inflight started more than max(interval, 3*pollInterval)
	poll := adxmonv1.SummaryRuleAsyncOperationPollInterval
	staleThreshold := interval
	if th := 3 * poll; th > staleThreshold {
		staleThreshold = th
	}
	staleAsync := false
	if oldestInflightStart != nil && now.Sub(*oldestInflightStart) > staleThreshold {
		staleAsync = true
	}

	// Gap detection: after BackfillAsyncOperations, any remaining fully elapsed intervals not represented
	// by either lastExec advancement or backlog windows implies a gap.
	gapDetected := false
	if lastExec != nil && !lastExec.IsZero() {
		elapsed := effectiveNow.Sub(*lastExec)
		if elapsed > interval { // at least one interval boundary has passed
			// Count how many *complete* intervals have elapsed
			completeIntervals := int(elapsed / interval)
			// Windows already accounted for: backlog operations (each backlog op represents one missing window)
			if backlog < completeIntervals {
				// Allow one interval of leeway to reduce false positives (e.g., reconcile delay)
				if completeIntervals-backlog > 1 {
					gapDetected = true
				}
			}
		}
	}

	// Determine due & submitted from conditions: suspended rules are never due
	due := false
	if !suspended {
		due = rule.ShouldSubmitRule(r.Clock)
	}
	submitted := false
	// We approximate submission success by checking if due and we advanced LastSuccessfulExecutionTime within this reconcile.
	if !suspended && due && lastExec != nil {
		// If lastExec within <= interval of now (with delay considered) we consider that just submitted.
		if effectiveNow.Sub(*lastExec) < (interval / 2) { // heuristic; avoids false positive if reconcile jitter small
			submitted = true
		}
	}

	// Working state & reason
	working := true
	degraded := ""
	if suspended {
		working = false
		degraded = "suspended"
	} else {
		switch {
		case !r.isDatabaseManaged(rule):
			working, degraded = false, "unmanaged_database"
		case func() bool {
			ok, _, _, _ := EvaluateExecutionCriteria(rule.Spec.Criteria, rule.Spec.CriteriaExpression, r.ClusterLabels)
			return !ok
		}():
			working, degraded = false, "criteria_mismatch"
		case !owned && wantsExporter:
			working, degraded = false, "ownership_pending"
		case due && !submitted:
			// If it was due but not (apparently) submitted this cycle
			working, degraded = false, "submission_missed"
		case staleAsync:
			working, degraded = false, "async_stale"
		case gapDetected:
			working, degraded = false, "backlog_gap"
		}
	}

	l := logger.Logger()
	// Construct structured fields; keep keys stable
	kv := []interface{}{
		"event", "summaryrule_health",
		"crd_name", fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
		"database", rule.Spec.Database,
		"interval_seconds", int(interval.Seconds()),
	}
	if ingestionDelay > 0 {
		kv = append(kv, "ingestion_delay_seconds", int(ingestionDelay.Seconds()))
	}
	if lastExec != nil {
		kv = append(kv, "last_execution_end", lastExec.UTC().Format(time.RFC3339Nano))
	}
	kv = append(kv,
		"due", due,
		"submitted", submitted,
		"suspended", suspended,
		"async_inflight", inflight,
		"async_backlog", backlog,
		"stale_async", staleAsync,
		"gap_detected", gapDetected,
		"working", working,
	)
	if degraded != "" {
		kv = append(kv, "degraded_reason", degraded)
	}
	l.Info("SummaryRule health", kv...)
}
