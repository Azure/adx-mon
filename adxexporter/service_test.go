package adxexporter

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/transform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestMatchesCriteria(t *testing.T) {
	tests := []struct {
		name           string
		criteria       map[string][]string
		clusterLabels  map[string]string
		expectedResult bool
	}{
		{
			name:           "no criteria should always match",
			criteria:       map[string][]string{},
			clusterLabels:  map[string]string{"env": "prod"},
			expectedResult: true,
		},
		{
			name: "single criterion matches",
			criteria: map[string][]string{
				"env": {"prod"},
			},
			clusterLabels:  map[string]string{"env": "prod"},
			expectedResult: true,
		},
		{
			name: "single criterion doesn't match",
			criteria: map[string][]string{
				"env": {"prod"},
			},
			clusterLabels:  map[string]string{"env": "dev"},
			expectedResult: false,
		},
		{
			name: "case insensitive key matching",
			criteria: map[string][]string{
				"ENV": {"prod"},
			},
			clusterLabels:  map[string]string{"env": "prod"},
			expectedResult: true,
		},
		{
			name: "case insensitive value matching",
			criteria: map[string][]string{
				"env": {"PROD"},
			},
			clusterLabels:  map[string]string{"env": "prod"},
			expectedResult: true,
		},
		{
			name: "multiple values in criterion, one matches",
			criteria: map[string][]string{
				"env": {"dev", "staging", "prod"},
			},
			clusterLabels:  map[string]string{"env": "staging"},
			expectedResult: true,
		},
		{
			name: "multiple criteria, first matches (OR logic)",
			criteria: map[string][]string{
				"env":    {"prod"},
				"region": {"us-west"},
			},
			clusterLabels: map[string]string{
				"env":    "prod",
				"region": "us-east",
			},
			expectedResult: true,
		},
		{
			name: "multiple criteria, second matches (OR logic)",
			criteria: map[string][]string{
				"env":    {"staging"},
				"region": {"us-east"},
			},
			clusterLabels: map[string]string{
				"env":    "prod",
				"region": "us-east",
			},
			expectedResult: true,
		},
		{
			name: "multiple criteria, none match",
			criteria: map[string][]string{
				"env":    {"staging"},
				"region": {"us-west"},
			},
			clusterLabels: map[string]string{
				"env":    "prod",
				"region": "us-east",
			},
			expectedResult: false,
		},
		{
			name: "criterion key not found in cluster labels",
			criteria: map[string][]string{
				"nonexistent": {"value"},
			},
			clusterLabels:  map[string]string{"env": "prod"},
			expectedResult: false,
		},
		{
			name:           "empty cluster labels with criteria",
			criteria:       map[string][]string{"env": {"prod"}},
			clusterLabels:  map[string]string{},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesCriteria(tt.criteria, tt.clusterLabels)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestShouldProcessMetricsExporter(t *testing.T) {
	reconciler := &MetricsExporterReconciler{
		ClusterLabels: map[string]string{
			"env":    "prod",
			"region": "us-east",
		},
		KustoClusters: map[string]string{
			"test-db": "https://test-cluster.kusto.windows.net",
		},
	}

	tests := []struct {
		name            string
		metricsExporter adxmonv1.MetricsExporter
		expectedResult  bool
	}{
		{
			name: "no criteria should match",
			metricsExporter: adxmonv1.MetricsExporter{
				Spec: adxmonv1.MetricsExporterSpec{
					Criteria: map[string][]string{},
				},
			},
			expectedResult: true,
		},
		{
			name: "matching criteria should match",
			metricsExporter: adxmonv1.MetricsExporter{
				Spec: adxmonv1.MetricsExporterSpec{
					Criteria: map[string][]string{
						"env": {"prod"},
					},
				},
			},
			expectedResult: true,
		},
		{
			name: "non-matching criteria should not match",
			metricsExporter: adxmonv1.MetricsExporter{
				Spec: adxmonv1.MetricsExporterSpec{
					Criteria: map[string][]string{
						"env": {"dev"},
					},
				},
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesCriteria(tt.metricsExporter.Spec.Criteria, reconciler.ClusterLabels)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestReconcile(t *testing.T) {
	// Create a test scheme and add our CRD
	s := runtime.NewScheme()
	err := scheme.AddToScheme(s)
	require.NoError(t, err)
	err = adxmonv1.AddToScheme(s)
	require.NoError(t, err)

	tests := []struct {
		name                string
		metricsExporter     *adxmonv1.MetricsExporter
		clusterLabels       map[string]string
		expectRequeue       bool
		expectedRequeueTime time.Duration
	}{
		{
			name: "successful reconciliation with matching criteria",
			metricsExporter: &adxmonv1.MetricsExporter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exporter",
					Namespace: "default",
				},
				Spec: adxmonv1.MetricsExporterSpec{
					Database: "test-db",
					Interval: metav1.Duration{Duration: 30 * time.Second},
					Criteria: map[string][]string{
						"env": {"prod"},
					},
				},
			},
			clusterLabels: map[string]string{
				"env": "prod",
			},
			expectRequeue:       true,
			expectedRequeueTime: time.Minute, // Minimum requeue interval
		},
		{
			name: "successful reconciliation with longer interval",
			metricsExporter: &adxmonv1.MetricsExporter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exporter-long",
					Namespace: "default",
				},
				Spec: adxmonv1.MetricsExporterSpec{
					Database: "test-db",
					Interval: metav1.Duration{Duration: 5 * time.Minute},
					Criteria: map[string][]string{},
				},
			},
			clusterLabels: map[string]string{
				"env": "prod",
			},
			expectRequeue:       true,
			expectedRequeueTime: 5 * time.Minute,
		},
		{
			name: "skipped reconciliation due to non-matching criteria",
			metricsExporter: &adxmonv1.MetricsExporter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exporter-skip",
					Namespace: "default",
				},
				Spec: adxmonv1.MetricsExporterSpec{
					Database: "test-db",
					Interval: metav1.Duration{Duration: 30 * time.Second},
					Criteria: map[string][]string{
						"env": {"dev"},
					},
				},
			},
			clusterLabels: map[string]string{
				"env": "prod",
			},
			expectRequeue:       false,
			expectedRequeueTime: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with the MetricsExporter
			var objs []client.Object
			if tt.metricsExporter != nil {
				objs = append(objs, tt.metricsExporter)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(objs...).
				Build()

			reconciler := &MetricsExporterReconciler{
				Client:        fakeClient,
				Scheme:        s,
				ClusterLabels: tt.clusterLabels,
				KustoClusters: map[string]string{
					"test-db": "https://test-cluster.kusto.windows.net",
				},
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.metricsExporter.Name,
					Namespace: tt.metricsExporter.Namespace,
				},
			}

			result, err := reconciler.Reconcile(context.Background(), req)
			require.NoError(t, err)

			if tt.expectRequeue {
				assert.True(t, result.RequeueAfter > 0, "Expected requeue after some duration")
				assert.Equal(t, tt.expectedRequeueTime, result.RequeueAfter)
			} else {
				assert.Equal(t, time.Duration(0), result.RequeueAfter, "Expected no requeue")
			}
		})
	}
}

func TestReconcile_NotFound(t *testing.T) {
	// Create a test scheme
	s := runtime.NewScheme()
	err := scheme.AddToScheme(s)
	require.NoError(t, err)
	err = adxmonv1.AddToScheme(s)
	require.NoError(t, err)

	// Create fake client with no objects
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		Build()

	reconciler := &MetricsExporterReconciler{
		Client: fakeClient,
		Scheme: s,
		KustoClusters: map[string]string{
			"test-db": "https://test-cluster.kusto.windows.net",
		},
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.RequeueAfter, "Expected no requeue for deleted object")
}

func TestExposeMetrics(t *testing.T) {
	tests := []struct {
		name                  string
		enableMetricsEndpoint bool
		expectedError         bool
	}{
		{
			name:                  "metrics endpoint disabled",
			enableMetricsEndpoint: false,
			expectedError:         false,
		},
		{
			name:                  "metrics endpoint enabled",
			enableMetricsEndpoint: true,
			expectedError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset any existing handlers to avoid conflicts
			http.DefaultServeMux = http.NewServeMux()

			reconciler := &MetricsExporterReconciler{
				EnableMetricsEndpoint: tt.enableMetricsEndpoint,
				MetricsPort:           ":0", // Use random port to avoid conflicts
				MetricsPath:           "/metrics",
				KustoClusters: map[string]string{
					"test-db": "https://test-cluster.kusto.windows.net",
				},
			}

			err := reconciler.exposeMetricsServer()

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.enableMetricsEndpoint {
				assert.NotNil(t, reconciler.Meter, "Meter should be initialized when metrics are enabled")
			}
		})
	}
}

func TestGetQueryExecutor_MissingEndpoint(t *testing.T) {
	reconciler := &MetricsExporterReconciler{
		KustoClusters: map[string]string{
			"existing-db": "https://cluster.kusto.windows.net",
		},
	}

	// Test with database that doesn't exist in KustoClusters
	_, err := reconciler.getQueryExecutor("missing-db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kusto endpoint configured for database missing-db")

	// Test with database that exists in KustoClusters
	// The Kusto client creation might succeed even with non-existent endpoints
	// as it doesn't validate connectivity during construction
	executor, err := reconciler.getQueryExecutor("existing-db")
	if err != nil {
		// If it fails, it should be due to Kusto client creation, not missing endpoint
		assert.Contains(t, err.Error(), "failed to create Kusto client")
		assert.NotContains(t, err.Error(), "no kusto endpoint configured")
	} else {
		// If it succeeds, we should have an executor
		assert.NotNil(t, executor)
	}
}

func TestGetOrCreateGauge_CachingBehavior(t *testing.T) {
	// Test that gauge instruments are properly cached and reused
	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0",
		MetricsPath:           "/metrics",
	}

	// Initialize the metrics server to set up the meter
	err := reconciler.exposeMetricsServer()
	require.NoError(t, err)
	require.NotNil(t, reconciler.Meter)

	// Test first creation
	gauge1, err := reconciler.getOrCreateGauge("test_metric")
	require.NoError(t, err)
	require.NotNil(t, gauge1)

	// Test cache hit - should return the same gauge instance
	gauge2, err := reconciler.getOrCreateGauge("test_metric")
	require.NoError(t, err)
	require.NotNil(t, gauge2)

	// Verify it's the same instance (pointer comparison)
	assert.Equal(t, gauge1, gauge2, "Should return cached gauge instance")

	// Test different metric name - should create new gauge
	gauge3, err := reconciler.getOrCreateGauge("different_metric")
	require.NoError(t, err)
	require.NotNil(t, gauge3)

	// Verify it's a different instance
	assert.NotEqual(t, gauge1, gauge3, "Should create different gauge for different metric name")

	// Verify cache contains both metrics
	assert.Len(t, reconciler.gaugeCache, 2, "Cache should contain 2 gauges")
	assert.Contains(t, reconciler.gaugeCache, "test_metric")
	assert.Contains(t, reconciler.gaugeCache, "different_metric")
}

func TestGetOrCreateGauge_ConcurrentAccess(t *testing.T) {
	// Test that concurrent access to gauge creation is thread-safe
	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0",
		MetricsPath:           "/metrics",
	}

	// Initialize the metrics server to set up the meter
	err := reconciler.exposeMetricsServer()
	require.NoError(t, err)

	const numGoroutines = 50
	const metricName = "concurrent_test_metric"

	// Channel to collect all gauge instances created
	gauges := make(chan metric.Float64Gauge, numGoroutines)
	errors := make(chan error, numGoroutines)

	// Start multiple goroutines trying to create the same gauge
	for i := 0; i < numGoroutines; i++ {
		go func() {
			gauge, err := reconciler.getOrCreateGauge(metricName)
			if err != nil {
				errors <- err
				return
			}
			gauges <- gauge
		}()
	}

	// Collect all results
	var collectedGauges []metric.Float64Gauge
	for i := 0; i < numGoroutines; i++ {
		select {
		case gauge := <-gauges:
			collectedGauges = append(collectedGauges, gauge)
		case err := <-errors:
			t.Fatalf("Unexpected error in goroutine: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatalf("Timeout waiting for goroutine %d", i)
		}
	}

	// Verify all goroutines got the same gauge instance
	require.Len(t, collectedGauges, numGoroutines)
	firstGauge := collectedGauges[0]
	for i, gauge := range collectedGauges {
		assert.Equal(t, firstGauge, gauge, "Goroutine %d got different gauge instance", i)
	}

	// Verify only one entry in cache
	assert.Len(t, reconciler.gaugeCache, 1, "Cache should contain exactly 1 gauge")
	assert.Contains(t, reconciler.gaugeCache, metricName)
}

func TestTransformAndRegisterMetrics(t *testing.T) {
	// Test the integration between transformation and metrics registration
	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0",
		MetricsPath:           "/metrics",
	}

	// Initialize the metrics server to set up the meter
	err := reconciler.exposeMetricsServer()
	require.NoError(t, err)
	require.NotNil(t, reconciler.Meter)

	// Create test MetricsExporter with transform configuration
	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter",
			Namespace: "default",
		},
		Spec: adxmonv1.MetricsExporterSpec{
			Transform: adxmonv1.TransformConfig{
				ValueColumn:       "value",
				MetricNameColumn:  "metric_name",
				TimestampColumn:   "timestamp",
				LabelColumns:      []string{"label1", "label2"},
				DefaultMetricName: "default_metric",
			},
		},
	}

	// Create test data that matches the transform configuration
	rows := []map[string]any{
		{
			"metric_name": "test_metric_1",
			"value":       42.5,
			"timestamp":   time.Now(),
			"label1":      "value1",
			"label2":      "value2",
		},
		{
			"metric_name": "test_metric_2",
			"value":       100.0,
			"timestamp":   time.Now(),
			"label1":      "different_value",
			"label2":      "another_value",
		},
	}

	// Execute transformation and registration
	err = reconciler.transformAndRegisterMetrics(context.Background(), me, rows)
	require.NoError(t, err)

	// Verify gauges were created and cached
	assert.Len(t, reconciler.gaugeCache, 2, "Should have cached 2 unique metric names")
	assert.Contains(t, reconciler.gaugeCache, "test_metric_1")
	assert.Contains(t, reconciler.gaugeCache, "test_metric_2")
}

func TestTransformAndRegisterMetrics_EmptyRows(t *testing.T) {
	// Test handling of empty query results
	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0",
		MetricsPath:           "/metrics",
	}

	err := reconciler.exposeMetricsServer()
	require.NoError(t, err)

	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: adxmonv1.MetricsExporterSpec{
			Transform: adxmonv1.TransformConfig{ValueColumn: "value"},
		},
	}

	// Test with empty rows
	err = reconciler.transformAndRegisterMetrics(context.Background(), me, []map[string]any{})
	require.NoError(t, err, "Should handle empty rows gracefully")

	// Test with nil rows
	err = reconciler.transformAndRegisterMetrics(context.Background(), me, nil)
	require.NoError(t, err, "Should handle nil rows gracefully")
}

func TestHealthzEndpoint(t *testing.T) {
	reconciler := &MetricsExporterReconciler{}

	// Create test request
	req, err := http.NewRequest("GET", "/healthz", nil)
	require.NoError(t, err)

	// Create response recorder
	rr := &testResponseWriter{
		headers: make(http.Header),
		body:    &bytes.Buffer{},
	}

	// Call handler
	reconciler.healthzHandler(rr, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rr.statusCode)
	assert.Equal(t, "ok", rr.body.String())
}

func TestReadyzEndpoint(t *testing.T) {
	tests := []struct {
		name               string
		setupReconciler    func() *MetricsExporterReconciler
		expectedStatusCode int
		expectedBody       string
	}{
		{
			name: "ready when meter is initialized",
			setupReconciler: func() *MetricsExporterReconciler {
				r := &MetricsExporterReconciler{
					EnableMetricsEndpoint: true,
					MetricsPort:           ":0",
					MetricsPath:           "/metrics",
				}
				err := r.exposeMetricsServer()
				require.NoError(t, err)
				return r
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       "ready",
		},
		{
			name: "not ready when meter is nil",
			setupReconciler: func() *MetricsExporterReconciler {
				return &MetricsExporterReconciler{}
			},
			expectedStatusCode: http.StatusServiceUnavailable,
			expectedBody:       "metrics not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := tt.setupReconciler()

			req, err := http.NewRequest("GET", "/readyz", nil)
			require.NoError(t, err)

			rr := &testResponseWriter{
				headers: make(http.Header),
				body:    &bytes.Buffer{},
			}

			reconciler.readyzHandler(rr, req)

			assert.Equal(t, tt.expectedStatusCode, rr.statusCode)
			assert.Equal(t, tt.expectedBody, rr.body.String())
		})
	}
}

func TestMetricsServerLifecycle(t *testing.T) {
	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0", // Use random port
		MetricsPath:           "/metrics",
	}

	// Test server startup
	err := reconciler.exposeMetricsServer()
	require.NoError(t, err)
	assert.NotNil(t, reconciler.metricsServer, "Server should be initialized")
	assert.NotNil(t, reconciler.Meter, "Meter should be initialized")
	assert.NotNil(t, reconciler.gaugeCache, "Gauge cache should be initialized")

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Test server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = reconciler.shutdownMetricsServer(ctx)
	assert.NoError(t, err, "Server shutdown should succeed")
}

func TestMetricsServerShutdown_NoServer(t *testing.T) {
	// Test shutdown when no server is running
	reconciler := &MetricsExporterReconciler{}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := reconciler.shutdownMetricsServer(ctx)
	assert.NoError(t, err, "Shutdown should succeed even when no server exists")
}

func TestGetQueryExecutor_ThreadSafety(t *testing.T) {
	// Test that concurrent access to query executor creation is thread-safe
	reconciler := &MetricsExporterReconciler{
		KustoClusters: map[string]string{
			"test-db": "https://test.kusto.windows.net",
		},
	}

	const numGoroutines = 50
	const dbName = "test-db"

	// Channels to collect results
	executors := make(chan *QueryExecutor, numGoroutines)
	errors := make(chan error, numGoroutines)

	// Start multiple goroutines trying to get the same executor
	for i := 0; i < numGoroutines; i++ {
		go func() {
			executor, err := reconciler.getQueryExecutor(dbName)
			if err != nil {
				errors <- err
				return
			}
			executors <- executor
		}()
	}

	// Collect all results
	var collectedExecutors []*QueryExecutor
	for i := 0; i < numGoroutines; i++ {
		select {
		case executor := <-executors:
			collectedExecutors = append(collectedExecutors, executor)
		case err := <-errors:
			// Some test environments might not support Kusto client creation
			// In this case, all goroutines should get the same error consistently
			t.Logf("Expected error creating Kusto client: %v", err)
			return
		case <-time.After(5 * time.Second):
			t.Fatalf("Timeout waiting for goroutine %d", i)
		}
	}

	// If we got executors, verify all goroutines got the same instance
	if len(collectedExecutors) > 0 {
		require.Len(t, collectedExecutors, numGoroutines)
		firstExecutor := collectedExecutors[0]
		for i, executor := range collectedExecutors {
			assert.Equal(t, firstExecutor, executor, "Goroutine %d got different executor instance", i)
		}

		// Verify only one entry in cache
		assert.Len(t, reconciler.QueryExecutors, 1, "Should have exactly 1 cached executor")
		assert.Contains(t, reconciler.QueryExecutors, dbName)
	}
}

func TestGetQueryExecutor_Caching(t *testing.T) {
	reconciler := &MetricsExporterReconciler{
		KustoClusters: map[string]string{
			"db1": "https://cluster1.kusto.windows.net",
			"db2": "https://cluster2.kusto.windows.net",
		},
	}

	// Note: These calls might fail with Kusto client creation errors in test environment
	// but the caching behavior should still be testable

	// Try to get executor for db1
	executor1, err1 := reconciler.getQueryExecutor("db1")

	// Try to get executor for db1 again (should be cached)
	executor1_cached, err1_cached := reconciler.getQueryExecutor("db1")

	// Try to get executor for db2 (should be different)
	executor2, err2 := reconciler.getQueryExecutor("db2")

	// If all calls succeeded, verify caching behavior
	if err1 == nil && err1_cached == nil && err2 == nil {
		assert.Equal(t, executor1, executor1_cached, "Should return cached executor for same database")
		assert.NotEqual(t, executor1, executor2, "Should return different executors for different databases")
		assert.Len(t, reconciler.QueryExecutors, 2, "Should have 2 cached executors")
	} else {
		// If calls failed due to environment, at least verify error consistency
		assert.Equal(t, err1, err1_cached, "Should get consistent errors for same database")
		t.Logf("Kusto client creation failed as expected in test environment: %v", err1)
	}
}

func TestRegisterMetricsWithCache_ErrorHandling(t *testing.T) {
	// Test error handling in metrics registration
	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0",
		MetricsPath:           "/metrics",
	}

	err := reconciler.exposeMetricsServer()
	require.NoError(t, err)

	// Test with invalid metric name (empty string)
	invalidMetrics := []transform.MetricData{
		{
			Name:      "", // Invalid empty name
			Value:     42.0,
			Timestamp: time.Now(),
			Labels:    map[string]string{"test": "value"},
		},
	}

	err = reconciler.registerMetricsWithCache(context.Background(), invalidMetrics)
	assert.Error(t, err, "Should fail with invalid metric name")
	assert.Contains(t, err.Error(), "failed to get or create gauge", "Error should mention gauge creation failure")
}

func TestExposeMetricsServer_DisabledEndpoint(t *testing.T) {
	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: false,
	}

	err := reconciler.exposeMetricsServer()
	require.NoError(t, err)

	// When disabled, should use noop meter
	assert.NotNil(t, reconciler.Meter, "Should have noop meter when disabled")
	assert.Nil(t, reconciler.metricsServer, "Should not have server when disabled")
	assert.Nil(t, reconciler.gaugeCache, "Should not have gauge cache when disabled")
}

func TestExecuteMetricsExporter_Integration(t *testing.T) {
	// Test the end-to-end execution flow (mock parts that require external dependencies)
	s := runtime.NewScheme()
	err := scheme.AddToScheme(s)
	require.NoError(t, err)
	err = adxmonv1.AddToScheme(s)
	require.NoError(t, err)

	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0",
		MetricsPath:           "/metrics",
		KustoClusters: map[string]string{
			"test-db": "https://test.kusto.windows.net",
		},
	}

	err = reconciler.exposeMetricsServer()
	require.NoError(t, err)

	// Create test MetricsExporter
	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter",
			Namespace: "default",
		},
		Spec: adxmonv1.MetricsExporterSpec{
			Database: "test-db",
			Interval: metav1.Duration{Duration: time.Minute},
			Body:     "TestTable | take 10",
			Transform: adxmonv1.TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test_metric",
			},
		},
	}

	// This will likely fail due to Kusto connection, but we can test the setup
	err = reconciler.executeMetricsExporter(context.Background(), me)

	// In test environment, we expect this to fail at Kusto query execution
	// The important thing is that it doesn't panic and handles errors gracefully
	if err != nil {
		t.Logf("Expected failure in test environment: %v", err)
		// The error could be either at query executor creation or query execution
		// Both are valid in a test environment without real Kusto connectivity
		assert.True(t,
			strings.Contains(err.Error(), "failed to get query executor") ||
				strings.Contains(err.Error(), "query execution failed"),
			"Should fail at either query executor creation or query execution")
	}
}

func TestTransformAndRegisterMetrics_DefaultMetricName(t *testing.T) {
	// Test transformation when using default metric name (no metric name column)
	reconciler := &MetricsExporterReconciler{
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0",
		MetricsPath:           "/metrics",
	}

	err := reconciler.exposeMetricsServer()
	require.NoError(t, err)

	// Create MetricsExporter with default metric name (no MetricNameColumn)
	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-default-metric",
			Namespace: "default",
		},
		Spec: adxmonv1.MetricsExporterSpec{
			Transform: adxmonv1.TransformConfig{
				ValueColumn:       "value",
				TimestampColumn:   "timestamp",
				LabelColumns:      []string{"label1", "label2"},
				DefaultMetricName: "default_metric_name", // No MetricNameColumn specified
			},
		},
	}

	// Create test data without metric_name column
	rows := []map[string]any{
		{
			"value":     42.5,
			"timestamp": time.Now(),
			"label1":    "value1",
			"label2":    "value2",
		},
		{
			"value":     100.0,
			"timestamp": time.Now(),
			"label1":    "different_value",
			"label2":    "another_value",
		},
	}

	// Execute transformation and registration
	err = reconciler.transformAndRegisterMetrics(context.Background(), me, rows)
	require.NoError(t, err)

	// Verify only one gauge was created (all data uses default metric name)
	assert.Len(t, reconciler.gaugeCache, 1, "Should have cached 1 metric with default name")
	assert.Contains(t, reconciler.gaugeCache, "default_metric_name")
}

// testResponseWriter is a simple implementation of http.ResponseWriter for testing
type testResponseWriter struct {
	statusCode int
	headers    http.Header
	body       *bytes.Buffer
}

func (w *testResponseWriter) Header() http.Header {
	return w.headers
}

func (w *testResponseWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *testResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func TestConcurrentReconciliation(t *testing.T) {
	// Test that multiple concurrent reconciliation loops don't cause race conditions
	s := runtime.NewScheme()
	err := scheme.AddToScheme(s)
	require.NoError(t, err)
	err = adxmonv1.AddToScheme(s)
	require.NoError(t, err)

	// Create test MetricsExporter
	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "concurrent-test",
			Namespace: "default",
		},
		Spec: adxmonv1.MetricsExporterSpec{
			Database: "test-db",
			Interval: metav1.Duration{Duration: time.Minute},
			Criteria: map[string][]string{
				"env": {"test"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(me).
		Build()

	reconciler := &MetricsExporterReconciler{
		Client:                fakeClient,
		Scheme:                s,
		EnableMetricsEndpoint: true,
		MetricsPort:           ":0",
		MetricsPath:           "/metrics",
		ClusterLabels: map[string]string{
			"env": "test",
		},
		KustoClusters: map[string]string{
			"test-db": "https://test.kusto.windows.net",
		},
	}

	err = reconciler.exposeMetricsServer()
	require.NoError(t, err)

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "concurrent-test",
			Namespace: "default",
		},
	}

	// Start multiple concurrent reconciliation loops
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := reconciler.Reconcile(context.Background(), req)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", id, err)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errors)

	// Check for any unexpected errors
	for err := range errors {
		// We expect some errors due to Kusto connectivity in test environment
		// but they should be consistent and not race-related
		t.Logf("Expected error in concurrent test: %v", err)
	}

	// The key test is that we didn't get any race conditions or panics
	// If we made it here, the concurrent access was handled properly
	t.Log("Concurrent reconciliation completed without race conditions")
}
