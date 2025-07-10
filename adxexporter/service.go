package adxexporter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/transform"
	"go.opentelemetry.io/otel/attribute"
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
	KustoClusters         map[string]string // database name -> endpoint URL
	OTLPEndpoint          string
	EnableMetricsEndpoint bool
	MetricsPort           string
	MetricsPath           string

	// Query execution components
	QueryExecutors map[string]*QueryExecutor // keyed by database name
	Clock          clock.Clock

	// Metrics server components
	Meter         metric.Meter
	metricsServer *http.Server

	// Synchronization for shared state
	mu sync.RWMutex // Protects QueryExecutors map
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
	if !matchesCriteria(metricsExporter.Spec.Criteria, r.ClusterLabels) {
		if logger.IsDebug() {
			logger.Debugf("Skipping MetricsExporter %s/%s - criteria does not match cluster labels",
				req.Namespace, req.Name)
		}
		return ctrl.Result{}, nil
	}

	logger.Infof("Processing MetricsExporter %s/%s with database: %s, interval: %s",
		req.Namespace, req.Name, metricsExporter.Spec.Database, metricsExporter.Spec.Interval.Duration)

	// Execute KQL query if it's time
	execErr := r.executeMetricsExporter(ctx, &metricsExporter)

	// Always update status, whether success or failure
	if statusErr := r.updateStatus(ctx, &metricsExporter, execErr); statusErr != nil {
		logger.Errorf("Failed to update status for MetricsExporter %s/%s: %v", req.Namespace, req.Name, statusErr)
		// Don't return error here - we want to continue processing
	}

	// Requeue for next interval (for continuous processing)
	requeueAfter := max(metricsExporter.Spec.Interval.Duration, time.Minute)

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *MetricsExporterReconciler) exposeMetricsServer() error {
	if !r.EnableMetricsEndpoint {
		r.Meter = noop.NewMeterProvider().Meter("noop")
		return nil
	}

	exporter, err := prometheus.New(
		// Adds a namespace prefix to all metrics
		prometheus.WithNamespace("adxexporter"),
		// Disables the long otel specific scope string since we're only exposing through metrics
		prometheus.WithoutScopeInfo(),
	)
	if err != nil {
		return fmt.Errorf("failed to create metrics exporter: %w", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	r.Meter = provider.Meter("adxexporter")

	return nil
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
	if err := r.exposeMetricsServer(); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.MetricsExporter{}).
		Complete(r)
}

// updateStatus updates the MetricsExporter status with proper Kusto error parsing
func (r *MetricsExporterReconciler) updateStatus(ctx context.Context, me *adxmonv1.MetricsExporter, err error) error {
	condition := metav1.Condition{
		Type:               adxmonv1.MetricsExporterOwner,
		Status:             metav1.ConditionTrue,
		Reason:             "ExecutionSuccessful",
		Message:            "Query executed successfully",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: me.GetGeneration(),
	}

	if err != nil {
		logger.Errorf("Failed to execute MetricsExporter %s/%s: %v", me.GetNamespace(), me.GetName(), err)
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ExecutionFailed"
		condition.Message = kustoutil.ParseError(err)
	}

	me.SetCondition(condition)

	if statusErr := r.Status().Update(ctx, me); statusErr != nil {
		return fmt.Errorf("failed to update status: %w", statusErr)
	}

	return nil
}

// executeMetricsExporter handles the execution logic for a MetricsExporter
func (r *MetricsExporterReconciler) executeMetricsExporter(ctx context.Context, me *adxmonv1.MetricsExporter) error {
	// Get or create query executor for this database
	executor, err := r.getQueryExecutor(me.Spec.Database)
	if err != nil {
		return fmt.Errorf("failed to get query executor for database %s: %w", me.Spec.Database, err)
	}

	// Set the clock on the CRD for testing
	var clk clock.Clock = r.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}

	// Check if it's time to execute using the CRD method
	if !me.ShouldExecuteQuery(clk) {
		if logger.IsDebug() {
			logger.Debugf("Not time to execute MetricsExporter %s/%s yet", me.Namespace, me.Name)
		}
		return nil
	}

	// Calculate the execution window using the CRD method
	startTime, endTime := me.NextExecutionWindow(clk)

	logger.Infof("Executing MetricsExporter %s/%s for window %s to %s",
		me.Namespace, me.Name, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	// Execute the KQL query
	tCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, err := executor.ExecuteQuery(tCtx, me.Spec.Body, startTime, endTime, r.ClusterLabels)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	if result.Error != nil {
		return fmt.Errorf("query execution failed: %w", result.Error)
	}

	logger.Infof("Query executed successfully in %v, returned %d rows",
		result.Duration, len(result.Rows))

	// Transform results to metrics and expose them
	if err := r.transformAndRegisterMetrics(ctx, me, result.Rows); err != nil {
		return fmt.Errorf("failed to transform and register metrics: %w", err)
	}

	// Update the last execution time using the CRD method
	me.SetLastExecutionTime(endTime)

	return nil
}

// transformAndRegisterMetrics converts KQL query results to metrics and registers them
func (r *MetricsExporterReconciler) transformAndRegisterMetrics(ctx context.Context, me *adxmonv1.MetricsExporter, rows []map[string]any) error {
	if len(rows) == 0 {
		if logger.IsDebug() {
			logger.Debugf("No rows returned from query for MetricsExporter %s/%s", me.Namespace, me.Name)
		}
		return nil
	}

	// Create transformer with the MetricsExporter's transform configuration
	transformer := transform.NewKustoToMetricsTransformer(
		transform.TransformConfig{
			MetricNameColumn:  me.Spec.Transform.MetricNameColumn,
			ValueColumn:       me.Spec.Transform.ValueColumn,
			TimestampColumn:   me.Spec.Transform.TimestampColumn,
			LabelColumns:      me.Spec.Transform.LabelColumns,
			DefaultMetricName: me.Spec.Transform.DefaultMetricName,
		},
		r.Meter,
	)

	// Validate the transform configuration against the query results
	if err := transformer.Validate(rows); err != nil {
		return fmt.Errorf("transform validation failed: %w", err)
	}

	// Transform the rows to metric data
	metrics, err := transformer.Transform(rows)
	if err != nil {
		return fmt.Errorf("failed to transform rows to metrics: %w", err)
	}

	if err := r.registerMetrics(ctx, metrics); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	logger.Infof("Successfully transformed and registered %d metrics for MetricsExporter %s/%s",
		len(metrics), me.Namespace, me.Name)

	return nil
}

func (r *MetricsExporterReconciler) registerMetrics(ctx context.Context, metrics []transform.MetricData) error {
	// Group metrics by name for efficient registration
	metricsByName := make(map[string][]transform.MetricData)
	for _, metric := range metrics {
		metricsByName[metric.Name] = append(metricsByName[metric.Name], metric)
	}

	// Register each unique metric name as a gauge
	for metricName, metricData := range metricsByName {
		gauge, err := r.Meter.Float64Gauge(metricName)
		if err != nil {
			return fmt.Errorf("failed to create gauge for metric '%s': %w", metricName, err)
		}

		// Record all data points for this metric
		for _, data := range metricData {
			// Convert labels to OpenTelemetry attributes
			attrs := make([]attribute.KeyValue, 0, len(data.Labels))
			for key, value := range data.Labels {
				attrs = append(attrs, attribute.String(key, value))
			}

			// Record the metric value
			gauge.Record(ctx, data.Value, metric.WithAttributes(attrs...))
		}
	}

	return nil
}

// getQueryExecutor gets or creates a QueryExecutor for the specified database
func (r *MetricsExporterReconciler) getQueryExecutor(database string) (*QueryExecutor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.QueryExecutors == nil {
		r.QueryExecutors = make(map[string]*QueryExecutor)
	}

	// Check if executor already exists (read lock)
	if executor, exists := r.QueryExecutors[database]; exists {
		return executor, nil
	}

	// Get the endpoint for this database from KustoClusters
	endpoint, exists := r.KustoClusters[database]
	if !exists {
		return nil, fmt.Errorf("no kusto endpoint configured for database %s", database)
	}

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
