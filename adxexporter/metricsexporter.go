package adxexporter

import (
	"context"
	"fmt"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/transform"
	"github.com/google/cel-go/cel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
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
	MetricsPort           string // Used for controller-runtime metrics server configuration
	MetricsPath           string // For documentation/consistency (controller-runtime uses /metrics)

	// Query execution components
	QueryExecutors map[string]*QueryExecutor // keyed by database name
	Clock          clock.Clock

	// Metrics components
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
	if !matchesCriteria(metricsExporter.Spec.Criteria, r.ClusterLabels) {
		if logger.IsDebug() {
			logger.Debugf("Skipping MetricsExporter %s/%s - criteria does not match cluster labels",
				req.Namespace, req.Name)
		}
		return ctrl.Result{}, nil
	}

	// Evaluate optional criteriaExpression (must be true if provided)
	if expr := metricsExporter.Spec.CriteriaExpression; expr != "" {
		var decls []cel.EnvOption
		activation := map[string]interface{}{}
		for k, v := range r.ClusterLabels {
			lk := strings.ToLower(k)
			decls = append(decls, cel.Variable(lk, cel.StringType))
			activation[lk] = strings.ToLower(v)
		}
		env, err := cel.NewEnv(decls...)
		if err != nil {
			logger.Errorf("Skipping MetricsExporter %s/%s due to CEL env error: %v", req.Namespace, req.Name, err)
			return ctrl.Result{}, nil
		}
		ast, iss := env.Parse(expr)
		if iss.Err() != nil {
			logger.Errorf("Skipping MetricsExporter %s/%s due to CEL parse error: %v", req.Namespace, req.Name, iss.Err())
			return ctrl.Result{}, nil
		}
		checked, iss2 := env.Check(ast)
		if iss2.Err() != nil {
			logger.Errorf("Skipping MetricsExporter %s/%s due to CEL typecheck error: %v", req.Namespace, req.Name, iss2.Err())
			return ctrl.Result{}, nil
		}
		prog, err := env.Program(checked)
		if err != nil {
			logger.Errorf("Skipping MetricsExporter %s/%s due to CEL program build error: %v", req.Namespace, req.Name, err)
			return ctrl.Result{}, nil
		}
		out, _, err := prog.Eval(activation)
		if err != nil {
			logger.Errorf("Skipping MetricsExporter %s/%s due to CEL eval error: %v", req.Namespace, req.Name, err)
			return ctrl.Result{}, nil
		}
		b, ok := out.Value().(bool)
		if !ok || !b {
			if logger.IsDebug() {
				logger.Debugf("Skipping MetricsExporter %s/%s because criteriaExpression evaluated to false", req.Namespace, req.Name)
			}
			return ctrl.Result{}, nil
		}
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

	// Register with controller-runtime's shared metrics registry, replacing the default registry
	exporter, err := prometheus.New(
		prometheus.WithRegisterer(crmetrics.Registry),
		// Adds a namespace prefix to all metrics
		prometheus.WithNamespace("adxexporter"),
		// Disables the long otel specific scope string since we're only exposing through metrics
		prometheus.WithoutScopeInfo(),
	)
	if err != nil {
		return fmt.Errorf("failed to create metrics exporter: %w", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	r.Meter = otel.GetMeterProvider().Meter("adxexporter")

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

	// Initialize QueryExecutors for all configured databases
	if err := r.initializeQueryExecutors(); err != nil {
		return fmt.Errorf("failed to initialize query executors: %w", err)
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
	// Get query executor for this database
	executor, exists := r.QueryExecutors[me.Spec.Database]
	if !exists {
		return fmt.Errorf("no query executor configured for database %s", me.Spec.Database)
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
			MetricNamePrefix:  me.Spec.Transform.MetricNamePrefix,
			ValueColumns:      me.Spec.Transform.ValueColumns,
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

// initializeQueryExecutors creates QueryExecutors for all configured databases
func (r *MetricsExporterReconciler) initializeQueryExecutors() error {
	r.QueryExecutors = make(map[string]*QueryExecutor)

	for database, endpoint := range r.KustoClusters {
		kustoClient, err := NewKustoClient(endpoint, database)
		if err != nil {
			return fmt.Errorf("failed to create Kusto client for database %s: %w", database, err)
		}

		executor := NewQueryExecutor(kustoClient)
		if r.Clock != nil {
			executor.SetClock(r.Clock)
		}

		r.QueryExecutors[database] = executor
	}

	return nil
}
