package adxexporter

import (
	"context"
	"fmt"
	"io"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	crdownership "github.com/Azure/adx-mon/pkg/crd/summaryrule"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
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
)

// SummaryRuleReconciler will reconcile SummaryRule objects.
type SummaryRuleReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Configuration
	ClusterLabels map[string]string
	KustoClusters map[string]string // database name -> endpoint URL

	KustoExecutors map[string]KustoExecutor // per-database Kusto clients supporting Query and Mgmt
	Clock          clock.Clock
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

	// Database gating: skip if this controller doesn't manage the rule's database
	if _, ok := r.KustoExecutors[rule.Spec.Database]; !ok {
		return ctrl.Result{}, nil
	}

	// Criteria gating
	if !matchesCriteria(rule.Spec.Criteria, r.ClusterLabels) {
		return ctrl.Result{}, nil
	}

	// Ownership gating and safe adoption via shared helpers
	if !crdownership.IsOwnedBy(&rule, adxmonv1.SummaryRuleOwnerADXExporter) {
		if crdownership.WantsOwner(&rule, adxmonv1.SummaryRuleOwnerADXExporter) {
			if ok, _ := crdownership.SafeToAdopt(&rule, r.Clock); ok {
				if err := crdownership.PatchClaim(ctx, r.Client, &rule, adxmonv1.SummaryRuleOwnerADXExporter); err != nil {
					logger.Warnf("Failed to patch ownership for %s/%s: %v", rule.Namespace, rule.Name, err)
				} else {
					return ctrl.Result{RequeueAfter: adoptRequeue}, nil
				}
			}
		}
		return ctrl.Result{}, nil
	}

	// Minimal submission path: compute window and submit async if it's time
	if rule.ShouldSubmitRule(r.Clock) {
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
	rule.BackfillAsyncOperations(r.Clock)

	// Track/advance outstanding async operations (including backlog windows)
	r.trackAsyncOperations(ctx, &rule)

	// Persist any changes made to async operation conditions or timestamps
	if err := r.Status().Update(ctx, &rule); err != nil {
		// Not fatal; next reconcile will retry
		logger.Warnf("Failed to persist SummaryRule async status %s/%s: %v", rule.Namespace, rule.Name, err)
	}

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
func nextRequeue(rule *adxmonv1.SummaryRule) time.Duration {
	base := rule.Spec.Interval.Duration
	if len(rule.GetAsyncOperations()) > 0 {
		poll := adxmonv1.SummaryRuleAsyncOperationPollInterval
		if poll > 0 && (base <= 0 || poll < base) {
			base = poll
		}
	}
	if base <= 0 {
		base = time.Minute
	}
	return base
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
	stmt := kql.New(".set-or-append async ").AddUnsafe(rule.Spec.Table).AddLiteral(" <| ").AddUnsafe(body)

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
func (r *SummaryRuleReconciler) trackAsyncOperations(ctx context.Context, rule *adxmonv1.SummaryRule) {
	ops := rule.GetAsyncOperations()
	for _, op := range ops {
		// Backlog: operation without OperationId indicates failed submission earlier
		if op.OperationId == "" {
			r.processBacklogOperation(ctx, rule, op)
			continue
		}

		status, err := r.getOperation(ctx, rule.Spec.Database, op.OperationId)
		if err != nil {
			logger.Errorf("Failed to query operation %s: %v", op.OperationId, err)
			continue
		}
		if status == nil {
			// Operation not found; could be very old or already pruned server-side
			if logger.IsDebug() {
				logger.Debugf("Async operation %s not found in .show operations", op.OperationId)
			}
			continue
		}

		// Completed (Succeeded or Failed) — remove entry and update status on failure
		if status.State == stateCompleted || status.State == stateFailed {
			r.handleCompletedOperation(ctx, rule, *status)
			continue
		} else if status.ShouldRetry != 0 {
			// ShouldRetry indicates resubmission is recommended
			r.handleRetryOperation(ctx, rule, op, *status)
		}
	}
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
	start, errStart := time.Parse(time.RFC3339Nano, operation.StartTime)
	end, errEnd := time.Parse(time.RFC3339Nano, operation.EndTime)
	if errStart != nil || errEnd != nil {
		logger.Warnf("Failed to parse backlog operation window: start=%s end=%s", operation.StartTime, operation.EndTime)
		return
	}

	if opId, err := r.submitRule(ctx, *rule, start, end); err == nil {
		operation.OperationId = opId

		// Restore original (exclusive) window end by adding back OneTick to the inclusive end we stored
		originalWindowEnd := end.Add(kustoutil.OneTick)
		current := rule.GetLastExecutionTime()
		if current == nil {
			t := time.Time{}
			current = &t
		}
		if originalWindowEnd.After(*current) {
			rule.SetLastExecutionTime(originalWindowEnd)
		}

		// Track updated operation with new id
		rule.SetAsyncOperation(operation)
	}
}
