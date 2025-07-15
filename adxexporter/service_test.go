package adxexporter

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		setupMockResults    func(t *testing.T, mock *MockKustoExecutor) // Setup function for mock responses
		expectedQueryCount  int                                         // Expected number of queries executed
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
					Body:     "MyTable | project metric_name='test_metric', value=42.5, timestamp=now()",
					Transform: adxmonv1.TransformConfig{
						MetricNameColumn: "metric_name",
						ValueColumn:      "value",
						TimestampColumn:  "timestamp",
					},
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
			setupMockResults: func(t *testing.T, mock *MockKustoExecutor) {
				// Return successful query results
				mock.SetNextResult(t, [][]interface{}{
					{"test_metric", 42.5, time.Now()},
				})
			},
			expectedQueryCount: 1,
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
					Body:     "MyTable | project metric_name='long_interval_metric', value=100.0",
					Transform: adxmonv1.TransformConfig{
						MetricNameColumn: "metric_name",
						ValueColumn:      "value",
						TimestampColumn:  "timestamp",
					},
					Criteria: map[string][]string{},
				},
			},
			clusterLabels: map[string]string{
				"env": "prod",
			},
			expectRequeue:       true,
			expectedRequeueTime: 5 * time.Minute,
			setupMockResults: func(t *testing.T, mock *MockKustoExecutor) {
				// Return successful query results
				mock.SetNextResult(t, [][]interface{}{
					{"long_interval_metric", 100.0, time.Now()},
				})
			},
			expectedQueryCount: 1,
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
					Body:     "MyTable | project metric_name='skipped_metric', value=0",
					Transform: adxmonv1.TransformConfig{
						MetricNameColumn: "metric_name",
						ValueColumn:      "value",
					},
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
			setupMockResults: func(t *testing.T, mock *MockKustoExecutor) {
				// No mock setup needed since query shouldn't execute
			},
			expectedQueryCount: 0, // No query should be executed due to criteria mismatch
		},
		{
			name: "query execution failure",
			metricsExporter: &adxmonv1.MetricsExporter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-exporter-error",
					Namespace: "default",
				},
				Spec: adxmonv1.MetricsExporterSpec{
					Database: "test-db",
					Interval: metav1.Duration{Duration: 30 * time.Second},
					Body:     "InvalidTable | project invalid_query",
					Transform: adxmonv1.TransformConfig{
						ValueColumn: "value",
					},
					Criteria: map[string][]string{},
				},
			},
			clusterLabels: map[string]string{
				"env": "prod",
			},
			expectRequeue:       true,
			expectedRequeueTime: time.Minute, // Still requeue even on error
			setupMockResults: func(t *testing.T, mock *MockKustoExecutor) {
				// Set up mock to return an error
				mock.SetNextError(fmt.Errorf("table 'InvalidTable' not found"))
			},
			expectedQueryCount: 1, // Query should be attempted even if it fails
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

			// Create mock Kusto client and set up expected results
			mockKustoClient := NewMockKustoExecutor(t, "test-db", "https://test-cluster.kusto.windows.net")
			if tt.setupMockResults != nil {
				tt.setupMockResults(t, mockKustoClient)
			}

			// Create reconciler with mock QueryExecutors
			reconciler := &MetricsExporterReconciler{
				Client:        fakeClient,
				Scheme:        s,
				ClusterLabels: tt.clusterLabels,
				KustoClusters: map[string]string{
					"test-db": "https://test-cluster.kusto.windows.net",
				},
				QueryExecutors: map[string]*QueryExecutor{
					"test-db": NewQueryExecutor(mockKustoClient),
				},
				EnableMetricsEndpoint: false, // Disable metrics to avoid initialization complexity
			}

			// Initialize the meter (this sets up r.Meter)
			err := reconciler.exposeMetricsServer()
			require.NoError(t, err)

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

			// Verify query execution
			queries := mockKustoClient.GetQueries()
			assert.Len(t, queries, tt.expectedQueryCount, "Unexpected number of queries executed")

			if tt.expectedQueryCount > 0 {
				// Verify that the query contains expected elements
				query := queries[0]
				assert.Contains(t, query, tt.metricsExporter.Spec.Body, "Query should contain the body from MetricsExporter spec")
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

func TestInitializeQueryExecutors(t *testing.T) {
	reconciler := &MetricsExporterReconciler{
		KustoClusters: map[string]string{
			"existing-db": "https://cluster.kusto.windows.net",
		},
	}

	// Test successful initialization
	err := reconciler.initializeQueryExecutors()
	if err != nil {
		// If it fails, it should be due to Kusto client creation, not missing endpoint
		assert.Contains(t, err.Error(), "failed to create Kusto client")
	} else {
		// If it succeeds, we should have executors for all databases
		assert.NotNil(t, reconciler.QueryExecutors)
		assert.Len(t, reconciler.QueryExecutors, 1)
		assert.Contains(t, reconciler.QueryExecutors, "existing-db")
	}

	// Test direct access behavior - missing database
	reconciler.QueryExecutors = map[string]*QueryExecutor{}
	executor, exists := reconciler.QueryExecutors["missing-db"]
	assert.False(t, exists)
	assert.Nil(t, executor)
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
}
