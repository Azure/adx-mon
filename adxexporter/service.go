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

	Meter metric.Meter
}

// Reconcile handles MetricsExporter reconciliation
func (r *MetricsExporterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Fetch the MetricsExporter instance
	var metricsExporter adxmonv1.MetricsExporter
	if err := r.Get(ctx, req.NamespacedName, &metricsExporter); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// MetricsExporter was deleted, nothing to do
			logger.Debugf("MetricsExporter %s/%s not found, likely deleted", req.Namespace, req.Name)
			return ctrl.Result{}, nil
		}
		logger.Errorf("Failed to get MetricsExporter %s/%s: %v", req.Namespace, req.Name, err)
		return ctrl.Result{}, err
	}

	// Check if this MetricsExporter should be processed by this instance
	if !r.shouldProcessMetricsExporter(metricsExporter) {
		logger.Debugf("Skipping MetricsExporter %s/%s - criteria does not match cluster labels",
			req.Namespace, req.Name)
		return ctrl.Result{}, nil
	}

	logger.Infof("Processing MetricsExporter %s/%s with database: %s, interval: %s",
		req.Namespace, req.Name, metricsExporter.Spec.Database, metricsExporter.Spec.Interval.Duration)

	// TODO: Implement actual query execution and metrics exposure (Tasks 3-5)
	// For now, we just log that we would process this MetricsExporter

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
					if strings.ToLower(labelValue) == strings.ToLower(value) {
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
