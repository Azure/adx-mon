package adxexporter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MetricsExporterReconciler reconciles MetricsExporter objects
type MetricsExporterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Configuration
	ClusterLabels         map[string]string
	OTLPEndpoint          string
	EnableMetricsEndpoint bool
	MetricsPort           string
	MetricsPath           string

	// Query execution components
	QueryExecutors map[string]*QueryExecutor // keyed by database name
	Clock          clock.Clock

	Meter metric.Meter
}

// Reconcile handles MetricsExporter reconciliation
func (r *MetricsExporterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the MetricsExporter instance
	var metricsExporter adxmonv1.MetricsExporter
	if err := r.Get(ctx, req.NamespacedName, &metricsExporter); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// MetricsExporter was deleted, nothing to do
			if logger.IsDebug() {
				logger.Debugf("MetricsExporter %s/%s not found, likely deleted", req.Namespace, req.Name)
			}
			return ctrl.Result{}, nil
		}
		logger.Errorf("Failed to get MetricsExporter %s/%s: %v", req.Namespace, req.Name, err)
		return ctrl.Result{}, err
	}

	// Check if this MetricsExporter should be processed by this instance
	if !r.shouldProcessMetricsExporter(metricsExporter) {
		if logger.IsDebug() {
			logger.Debugf("Skipping MetricsExporter %s/%s - criteria does not match cluster labels",
				req.Namespace, req.Name)
		}
		return ctrl.Result{}, nil
	}

	logger.Infof("Processing MetricsExporter %s/%s with database: %s, interval: %s",
		req.Namespace, req.Name, metricsExporter.Spec.Database, metricsExporter.Spec.Interval.Duration)

	// Execute KQL query if it's time
	if err := r.executeMetricsExporter(ctx, &metricsExporter); err != nil {
		logger.Errorf("Failed to execute MetricsExporter %s/%s: %v", req.Namespace, req.Name, err)
		// Continue with requeue even on error to retry later
	}

	// Requeue for next interval (for continuous processing)
	requeueAfter := metricsExporter.Spec.Interval.Duration
	if requeueAfter < time.Minute {
		requeueAfter = time.Minute // Minimum requeue interval
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *MetricsExporterReconciler) exposeMetrics() error {
	if !r.EnableMetricsEndpoint {
		r.Meter = noop.NewMeterProvider().Meter("noop")
		return nil
	}

	exporter, err := prometheus.New(
		// Adds a namespace prefix to all metrics
		prometheus.WithNamespace("adxexporter"),
		// Disables the long otel specific scope string since we're only exposing through prometheus
		prometheus.WithoutScopeInfo(),
	)
	if err != nil {
		return fmt.Errorf("failed to create Prometheus exporter: %w", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	r.Meter = provider.Meter("adxexporter")

	metricsServer := &http.Server{Addr: r.MetricsPort}
	http.Handle(r.MetricsPath, promhttp.Handler())

	go func() {
		err := metricsServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			logger.Errorf("Metrics server failed: %v", err)
			// Optionally, shut down the application gracefully if needed
			return
		}
	}()

	return nil
}

// shouldProcessMetricsExporter determines if this instance should process the given MetricsExporter
// based on criteria matching against cluster labels
func (r *MetricsExporterReconciler) shouldProcessMetricsExporter(me adxmonv1.MetricsExporter) bool {
	return matchesCriteria(me.Spec.Criteria, r.ClusterLabels)
}

// matchesCriteria checks if the given criteria matches any of the cluster labels.
// Uses the same logic as SummaryRule for consistency
func matchesCriteria(criteria map[string][]string, clusterLabels map[string]string) bool {
	// If no criteria are specified, always match
	if len(criteria) == 0 {
		return true
	}

	// Check if any criterion matches
	for k, v := range criteria {
		lowerKey := strings.ToLower(k)
		// Look for matching cluster label (case-insensitive key matching)
		for labelKey, labelValue := range clusterLabels {
			if strings.ToLower(labelKey) == lowerKey {
				for _, value := range v {
					if strings.EqualFold(labelValue, value) {
						return true // Found a match, return immediately
					}
				}
				break // We found the key, no need to check other label keys
			}
		}
	}

	return false // No criteria matched
}

// SetupWithManager sets up the service with the Manager
func (r *MetricsExporterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.exposeMetrics(); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.MetricsExporter{}).
		Complete(r)
}

// executeMetricsExporter handles the execution logic for a MetricsExporter
func (r *MetricsExporterReconciler) executeMetricsExporter(ctx context.Context, me *adxmonv1.MetricsExporter) error {
	// Get or create query executor for this database
	executor, err := r.getQueryExecutor(me.Spec.Database)
	if err != nil {
		return fmt.Errorf("failed to get query executor for database %s: %w", me.Spec.Database, err)
	}

	// Get the last execution time from the CRD status
	lastExecutionTime := r.getLastExecutionTime(me)

	// Check if it's time to execute
	if !executor.ShouldExecuteQuery(
		lastExecutionTime,
		me.Spec.Interval.Duration,
		r.getLastTransitionTime(me),
		me.GetGeneration(),
		r.getObservedGeneration(me),
	) {
		if logger.IsDebug() {
			logger.Debugf("Not time to execute MetricsExporter %s/%s yet", me.Namespace, me.Name)
		}
		return nil
	}

	// Calculate the execution window
	startTime, endTime := executor.CalculateNextExecutionWindow(lastExecutionTime, me.Spec.Interval.Duration)

	logger.Infof("Executing MetricsExporter %s/%s for window %s to %s",
		me.Namespace, me.Name, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	// Execute the KQL query
	result, err := executor.ExecuteQuery(ctx, me.Spec.Body, startTime, endTime, r.ClusterLabels)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	if result.Error != nil {
		return fmt.Errorf("query execution failed: %w", result.Error)
	}

	logger.Infof("Query executed successfully in %v, returned %d rows",
		result.Duration, len(result.Rows))

	// TODO: Transform results to metrics and expose them (Task 4-5)
	// For now, we just log the successful execution

	// Update the last execution time
	r.setLastExecutionTime(me, endTime)

	return nil
}

// getQueryExecutor gets or creates a QueryExecutor for the specified database
func (r *MetricsExporterReconciler) getQueryExecutor(database string) (*QueryExecutor, error) {
	if r.QueryExecutors == nil {
		r.QueryExecutors = make(map[string]*QueryExecutor)
	}

	if executor, exists := r.QueryExecutors[database]; exists {
		return executor, nil
	}

	// Create a new Kusto client for this database
	// TODO: Make endpoint configurable - for now use a placeholder
	endpoint := "https://your-cluster.kusto.windows.net" // This should be configurable
	kustoClient, err := NewKustoClient(endpoint, database)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kusto client: %w", err)
	}

	executor := NewQueryExecutor(kustoClient)
	if r.Clock != nil {
		executor.SetClock(r.Clock)
	}

	r.QueryExecutors[database] = executor
	return executor, nil
}

// getLastExecutionTime extracts the last execution time from MetricsExporter status
func (r *MetricsExporterReconciler) getLastExecutionTime(me *adxmonv1.MetricsExporter) *time.Time {
	// Look for a condition that tracks last execution time
	// This follows the pattern used in SummaryRule
	for _, condition := range me.Status.Conditions {
		if condition.Type == "LastSuccessfulExecution" && condition.Message != "" {
			if t, err := time.Parse(time.RFC3339Nano, condition.Message); err == nil {
				return &t
			}
		}
	}
	return nil
}

// setLastExecutionTime updates the last execution time in MetricsExporter status
func (r *MetricsExporterReconciler) setLastExecutionTime(me *adxmonv1.MetricsExporter, t time.Time) {
	// This would need to be implemented to update the CRD status
	// For now, we'll leave this as a placeholder since we need to
	// implement the status update mechanism
	logger.Debugf("Would set last execution time to %s for MetricsExporter %s/%s",
		t.Format(time.RFC3339), me.Namespace, me.Name)
}

// getLastTransitionTime gets the last transition time for timing calculations
func (r *MetricsExporterReconciler) getLastTransitionTime(me *adxmonv1.MetricsExporter) time.Time {
	// Find the most recent condition transition time
	var latest time.Time
	for _, condition := range me.Status.Conditions {
		if condition.LastTransitionTime.Time.After(latest) {
			latest = condition.LastTransitionTime.Time
		}
	}

	// If no conditions exist, use creation time
	if latest.IsZero() {
		latest = me.CreationTimestamp.Time
	}

	return latest
}

// getObservedGeneration gets the observed generation from status
func (r *MetricsExporterReconciler) getObservedGeneration(me *adxmonv1.MetricsExporter) int64 {
	// Look for a condition with observed generation
	for _, condition := range me.Status.Conditions {
		if condition.ObservedGeneration > 0 {
			return condition.ObservedGeneration
		}
	}
	return 0
}
