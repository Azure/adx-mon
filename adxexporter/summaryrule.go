package adxexporter

import (
	"context"
	"fmt"
	"io"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
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

// SummaryRuleReconciler will reconcile SummaryRule objects.
// This is a skeleton; behavior will be implemented in follow-up commits.
type SummaryRuleReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Configuration
	ClusterLabels map[string]string
	KustoClusters map[string]string // database name -> endpoint URL

	// Query/Mgmt execution components (initialized later)
	QueryExecutors map[string]*QueryExecutor // keyed by database name (may be used later)
	KustoExecutors map[string]KustoExecutor  // per-database Kusto clients supporting Query and Mgmt
	Clock          clock.Clock
}

// Reconcile processes a single SummaryRule; placeholder implementation for now.
func (r *SummaryRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if logger.IsDebug() {
		logger.Debugf("SummaryRule reconcile start %s/%s", req.Namespace, req.Name)
	}

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
		if logger.IsDebug() {
			logger.Debugf("Skipping SummaryRule %s/%s - no executor for database %q", req.Namespace, req.Name, rule.Spec.Database)
		}
		return ctrl.Result{}, nil
	}

	// Criteria gating
	if !matchesCriteria(rule.Spec.Criteria, r.ClusterLabels) {
		if logger.IsDebug() {
			logger.Debugf("Skipping SummaryRule %s/%s - criteria did not match cluster labels", req.Namespace, req.Name)
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

		// Update status reflecting submission result
		if uerr := r.updateSummaryRuleStatus(ctx, &rule, err); uerr != nil {
			// Log but continue; controller will retry on next reconcile
			logger.Errorf("Failed to update SummaryRule status %s/%s: %v", rule.Namespace, rule.Name, uerr)
		}
	}

	// Requeue based on rule interval to drive periodic submissions
	requeueAfter := rule.Spec.Interval.Duration
	if requeueAfter <= 0 {
		requeueAfter = time.Minute
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *SummaryRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize defaults
	if r.Clock == nil {
		r.Clock = clock.RealClock{}
	}
	if r.QueryExecutors == nil {
		r.QueryExecutors = make(map[string]*QueryExecutor)
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

// initializeQueryExecutors creates per-database executors for SummaryRule processing.
// No-op placeholder for now to keep this commit minimal.
func (r *SummaryRuleReconciler) initializeQueryExecutors() error {
	for database, endpoint := range r.KustoClusters {
		// Reuse the shared Kusto client capable of both Query and Mgmt
		kustoClient, err := NewKustoClient(endpoint, database)
		if err != nil {
			return err
		}
		r.KustoExecutors[database] = kustoClient

		// Optionally keep a QueryExecutor if needed later
		if _, ok := r.QueryExecutors[database]; !ok {
			r.QueryExecutors[database] = NewQueryExecutor(kustoClient)
			if r.Clock != nil {
				r.QueryExecutors[database].SetClock(r.Clock)
			}
		}
	}
	return nil
}

// Run starts any background tasks needed by the reconciler; placeholder for now.
func (r *SummaryRuleReconciler) Run(ctx context.Context) error {
	// Intentionally empty; async operation polling and other routines will be added later.
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
	tCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	res, err := exec.Mgmt(tCtx, stmt)
	if err != nil {
		return "", fmt.Errorf("failed to execute summary rule %s.%s: %w", rule.Spec.Database, rule.Name, err)
	}

	return operationIDFromResult(res)
}

// updateSummaryRuleStatus sets the primary condition with friendly Kusto error parsing
func (r *SummaryRuleReconciler) updateSummaryRuleStatus(ctx context.Context, rule *adxmonv1.SummaryRule, err error) error {
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
	return r.Status().Update(ctx, rule)
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
