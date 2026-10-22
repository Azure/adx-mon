package adxexporter

import (
	"context"
	"fmt"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/collector/export"
	"github.com/Azure/adx-mon/pkg/kustoutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/transform"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	AddResourceAttributes map[string]string // Additional OTLP resource attributes (merged with ClusterLabels)
	MetricNamePrefix      string            // Global prefix prepended to all metric names (combined with CRD prefix)
	EnableMetricsEndpoint bool              // Deprecated: kept for backward compatibility, not used with OTLP push
	MetricsPort           string            // Deprecated: kept for backward compatibility
	MetricsPath           string            // Deprecated: kept for backward compatibility

	// Query execution components
	QueryExecutors map[string]*QueryExecutor // keyed by database name
	Clock          clock.Clock

	// OTLP push client for metrics delivery
	OtlpExporter *export.PromToOtlpExporter
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

	proceed, reason, message, exprErr := EvaluateExecutionCriteria(metricsExporter.Spec.Criteria, metricsExporter.Spec.CriteriaExpression, r.ClusterLabels)
	if exprErr != nil {
		// This error means the criteria expression was invalid (parse/type/eval error).
		// We'll treat this as a non-match to prevent execution, but log the error for visibility.
		logger.Errorf("Criteria expression error for MetricsExporter %s/%s: %v", req.Namespace, req.Name, exprErr)
		return ctrl.Result{}, nil
	}
	condStatus := metav1.ConditionFalse
	if proceed {
		condStatus = metav1.ConditionTrue
	}
	cond := metav1.Condition{Type: adxmonv1.ConditionCriteria, Status: condStatus, Reason: reason, Message: message, ObservedGeneration: metricsExporter.GetGeneration(), LastTransitionTime: metav1.Now()}
	condChanged := meta.SetStatusCondition(&metricsExporter.Status.Conditions, cond)
	if !proceed {
		if condChanged {
			_ = r.Status().Update(ctx, &metricsExporter) // best-effort persist when skipping
		}
		return ctrl.Result{}, nil
	}

	logger.Infof("Processing MetricsExporter %s/%s with database: %s, interval: %s",
		req.Namespace, req.Name, metricsExporter.Spec.Database, metricsExporter.Spec.Interval.Duration)

	// Execute KQL query if it's time
	executed, execErr := r.executeMetricsExporter(ctx, &metricsExporter)

	// Update status only when there was an actual execution attempt.
	if executed {
		if statusErr := r.updateStatus(ctx, &metricsExporter, execErr); statusErr != nil {
			logger.Errorf("Failed to update status for MetricsExporter %s/%s: %v", req.Namespace, req.Name, statusErr)
			// Don't return error here - we want to continue processing
		}
	}

	// Requeue for next interval (for continuous processing)
	requeueAfter := max(metricsExporter.Spec.Interval.Duration, time.Minute)

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *MetricsExporterReconciler) initOtlpExporter() error {
	if r.OTLPEndpoint == "" {
		return fmt.Errorf("OTLP endpoint is required: specify --otlp-endpoint")
	}

	// Create resource attributes by merging cluster labels with explicit attributes.
	// ClusterLabels provide a base set of resource attributes for identification.
	// AddResourceAttributes allows explicit overrides or additional attributes.
	resourceAttrs := make(map[string]string, len(r.ClusterLabels)+len(r.AddResourceAttributes))
	for k, v := range r.ClusterLabels {
		resourceAttrs[k] = v
	}
	// Explicit attributes take precedence over cluster labels
	for k, v := range r.AddResourceAttributes {
		resourceAttrs[k] = v
	}

	// Create a pass-through transformer (no filtering - KQL already selected the data)
	transformer := &transform.RequestTransformer{
		DefaultDropMetrics: false,
	}

	opts := export.PromToOtlpExporterOpts{
		Transformer:           transformer,
		Destination:           r.OTLPEndpoint,
		AddResourceAttributes: resourceAttrs,
	}

	r.OtlpExporter = export.NewPromToOtlpExporter(opts)
	return nil
}

// SetupWithManager sets up the service with the Manager
func (r *MetricsExporterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.initOtlpExporter(); err != nil {
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

	// Mirror terminal success/failure via shared Completed/Failed conditions using meta.SetStatusCondition.
	if err == nil {
		// Completed=True, Failed=False
		meta.SetStatusCondition(&me.Status.Conditions, metav1.Condition{
			Type:               adxmonv1.ConditionCompleted,
			Status:             metav1.ConditionTrue,
			Reason:             "ExecutionSuccessful",
			Message:            "Most recent execution succeeded",
			ObservedGeneration: me.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
		meta.SetStatusCondition(&me.Status.Conditions, metav1.Condition{
			Type:               adxmonv1.ConditionFailed,
			Status:             metav1.ConditionFalse,
			Reason:             "ExecutionSuccessful",
			Message:            "Most recent execution succeeded",
			ObservedGeneration: me.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
	} else {
		// Failed=True, Completed=False
		meta.SetStatusCondition(&me.Status.Conditions, metav1.Condition{
			Type:               adxmonv1.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "ExecutionFailed",
			Message:            condition.Message,
			ObservedGeneration: me.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
		meta.SetStatusCondition(&me.Status.Conditions, metav1.Condition{
			Type:               adxmonv1.ConditionCompleted,
			Status:             metav1.ConditionFalse,
			Reason:             "ExecutionFailed",
			Message:            condition.Message,
			ObservedGeneration: me.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		})
	}

	if statusErr := r.Status().Update(ctx, me); statusErr != nil {
		return fmt.Errorf("failed to update status: %w", statusErr)
	}

	return nil
}

// executeMetricsExporter handles the execution logic for a MetricsExporter
// executeMetricsExporter runs the exporter if due. It returns (executed, err)
// where executed indicates whether a query attempt occurred.
func (r *MetricsExporterReconciler) executeMetricsExporter(ctx context.Context, me *adxmonv1.MetricsExporter) (bool, error) {
	// Get query executor for this database
	executor, exists := r.QueryExecutors[me.Spec.Database]
	if !exists {
		return false, fmt.Errorf("no query executor configured for database %s", me.Spec.Database)
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
		return false, nil
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
		return true, fmt.Errorf("failed to execute query: %w", err)
	}

	if result.Error != nil {
		return true, fmt.Errorf("query execution failed: %w", result.Error)
	}

	logger.Infof("Query executed successfully in %v, returned %d rows",
		result.Duration, len(result.Rows))

	// Transform results to metrics and expose them
	if err := r.transformAndRegisterMetrics(ctx, me, result.Rows); err != nil {
		return true, fmt.Errorf("failed to transform and register metrics: %w", err)
	}

	// Update the last execution time using the CRD method
	me.SetLastExecutionTime(endTime)

	return true, nil
}

// transformAndRegisterMetrics converts KQL query results to metrics and pushes them via OTLP
func (r *MetricsExporterReconciler) transformAndRegisterMetrics(ctx context.Context, me *adxmonv1.MetricsExporter, rows []map[string]any) error {
	if len(rows) == 0 {
		if logger.IsDebug() {
			logger.Debugf("No rows returned from query for MetricsExporter %s/%s", me.Namespace, me.Name)
		}
		return nil
	}

	// Build the effective metric name prefix by combining CLI and CRD prefixes:
	// - CLI prefix (--metric-name-prefix) is always prepended first if set
	// - CRD prefix (transform.metricNamePrefix) is appended after CLI prefix if set
	// This ensures operators can enforce a global prefix (e.g., for allow-lists)
	// while teams can still add their own sub-prefixes via CRD.
	var prefixParts []string
	if r.MetricNamePrefix != "" {
		prefixParts = append(prefixParts, r.MetricNamePrefix)
	}
	if me.Spec.Transform.MetricNamePrefix != "" {
		prefixParts = append(prefixParts, me.Spec.Transform.MetricNamePrefix)
	}
	effectivePrefix := strings.Join(prefixParts, "_")

	// Create transformer with the MetricsExporter's transform configuration
	// Note: meter parameter is nil since we're using OTLP push instead of OTel SDK registration
	transformer := transform.NewKustoToMetricsTransformer(
		transform.TransformConfig{
			MetricNameColumn:  me.Spec.Transform.MetricNameColumn,
			MetricNamePrefix:  effectivePrefix,
			ValueColumns:      me.Spec.Transform.ValueColumns,
			TimestampColumn:   me.Spec.Transform.TimestampColumn,
			LabelColumns:      me.Spec.Transform.LabelColumns,
			DefaultMetricName: me.Spec.Transform.DefaultMetricName,
		},
		nil, // meter not needed for OTLP push path
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

	if err := r.pushMetrics(ctx, metrics); err != nil {
		return fmt.Errorf("failed to push metrics: %w", err)
	}

	logger.Infof("Successfully transformed and pushed %d metrics for MetricsExporter %s/%s",
		len(metrics), me.Namespace, me.Name)

	return nil
}

// pushMetrics converts MetricData to WriteRequest and sends via OTLP
func (r *MetricsExporterReconciler) pushMetrics(ctx context.Context, metrics []transform.MetricData) error {
	if len(metrics) == 0 {
		return nil
	}

	// Convert to WriteRequest using pooled objects for efficiency
	wr := transform.ToWriteRequest(metrics)
	defer func() {
		// Return pooled objects
		wr.Reset()
		prompb.WriteRequestPool.Put(wr)
	}()

	// Push via OTLP exporter
	return r.OtlpExporter.Write(ctx, wr)
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
