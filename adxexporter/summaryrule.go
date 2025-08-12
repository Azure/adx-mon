package adxexporter

import (
	"context"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
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

	// No further behavior yet; full submission/tracking to be added in next commits.
	return ctrl.Result{}, nil
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
