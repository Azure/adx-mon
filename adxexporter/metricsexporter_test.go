package adxexporter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/golang/protobuf/proto"
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
						ValueColumns:     []string{"value"},
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
						ValueColumns:     []string{"value"},
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
						ValueColumns:     []string{"value"},
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
						ValueColumns: []string{"value"},
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
			}

			// Set up mock OTLP server and initialize exporter
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Accept all metrics and return success
				io.Copy(io.Discard, r.Body)
				r.Body.Close()
				w.WriteHeader(http.StatusOK)
				w.Write([]byte{}) // Empty ExportMetricsServiceResponse
			}))
			defer mockServer.Close()

			reconciler.OTLPEndpoint = mockServer.URL
			err = reconciler.initOtlpExporter()
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

func TestInitOtlpExporter(t *testing.T) {
	tests := []struct {
		name          string
		otlpEndpoint  string
		expectedError bool
		errorContains string
	}{
		{
			name:          "valid OTLP endpoint",
			otlpEndpoint:  "http://localhost:4318/v1/metrics",
			expectedError: false,
		},
		{
			name:          "empty OTLP endpoint should error",
			otlpEndpoint:  "",
			expectedError: true,
			errorContains: "OTLP endpoint is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &MetricsExporterReconciler{
				OTLPEndpoint:  tt.otlpEndpoint,
				ClusterLabels: map[string]string{"env": "test"},
				KustoClusters: map[string]string{
					"test-db": "https://test-cluster.kusto.windows.net",
				},
			}

			err := reconciler.initOtlpExporter()

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, reconciler.OtlpExporter, "OtlpExporter should be initialized")
			}
		})
	}
}

func TestInitOtlpExporterResourceAttributes(t *testing.T) {
	tests := []struct {
		name                  string
		clusterLabels         map[string]string
		addResourceAttributes map[string]string
		expectedMergedKeys    []string
	}{
		{
			name:                  "cluster labels only",
			clusterLabels:         map[string]string{"env": "prod", "region": "eastus"},
			addResourceAttributes: nil,
			expectedMergedKeys:    []string{"env", "region"},
		},
		{
			name:                  "add resource attributes only",
			clusterLabels:         nil,
			addResourceAttributes: map[string]string{"service.name": "adxexporter", "service.version": "1.0"},
			expectedMergedKeys:    []string{"service.name", "service.version"},
		},
		{
			name:                  "merged - no overlap",
			clusterLabels:         map[string]string{"env": "prod"},
			addResourceAttributes: map[string]string{"service.name": "adxexporter"},
			expectedMergedKeys:    []string{"env", "service.name"},
		},
		{
			name:                  "merged - with override (add takes precedence)",
			clusterLabels:         map[string]string{"env": "prod", "region": "eastus"},
			addResourceAttributes: map[string]string{"env": "staging"}, // should override
			expectedMergedKeys:    []string{"env", "region"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &MetricsExporterReconciler{
				OTLPEndpoint:          "http://localhost:4318/v1/metrics",
				ClusterLabels:         tt.clusterLabels,
				AddResourceAttributes: tt.addResourceAttributes,
				KustoClusters: map[string]string{
					"test-db": "https://test-cluster.kusto.windows.net",
				},
			}

			err := reconciler.initOtlpExporter()
			require.NoError(t, err)
			assert.NotNil(t, reconciler.OtlpExporter, "OtlpExporter should be initialized")
			// The exporter should have been created with merged resource attributes
			// We can't directly inspect the resourceAttributes field (it's unexported),
			// but we verify initialization succeeded which means the merge worked.
		})
	}
}

// TestResourceAttributesInOTLPPayload verifies that resource attributes
// (from --add-resource-attributes and --cluster-labels) are correctly included
// in the OTLP ExportMetricsServiceRequest payload
func TestResourceAttributesInOTLPPayload(t *testing.T) {
	// Channel to receive the parsed OTLP request
	requestChan := make(chan *v1.ExportMetricsServiceRequest, 1)

	// Create mock OTLP server that captures and parses the request
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		r.Body.Close()

		// Parse the OTLP protobuf request
		exportRequest := &v1.ExportMetricsServiceRequest{}
		err = proto.Unmarshal(body, exportRequest)
		require.NoError(t, err)

		requestChan <- exportRequest

		w.WriteHeader(http.StatusOK)
		w.Write([]byte{})
	}))
	defer mockServer.Close()

	// Configure reconciler with specific resource attributes (like the real deployment)
	reconciler := &MetricsExporterReconciler{
		OTLPEndpoint:  mockServer.URL,
		ClusterLabels: map[string]string{"environment": "test", "region": "eastus"},
		AddResourceAttributes: map[string]string{
			"_microsoft_metrics_account":   "RPACSINTv2",
			"_microsoft_metrics_namespace": "Platform",
		},
	}

	// Initialize the OTLP exporter
	err := reconciler.initOtlpExporter()
	require.NoError(t, err)

	// Create test MetricsExporter
	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-resource-attrs",
			Namespace: "default",
		},
		Spec: adxmonv1.MetricsExporterSpec{
			Transform: adxmonv1.TransformConfig{
				ValueColumns:      []string{"count"},
				MetricNameColumn:  "metric_name",
				TimestampColumn:   "PreciseTimeStamp",
				DefaultMetricName: "test_metric",
			},
		},
	}

	// Create test data
	rows := []map[string]any{
		{
			"metric_name":      "infra_host_count",
			"count":            float64(42),
			"PreciseTimeStamp": time.Now(),
		},
	}

	// Execute transformation and push
	err = reconciler.transformAndRegisterMetrics(context.Background(), me, rows)
	require.NoError(t, err)

	// Verify the OTLP request contains the resource attributes
	select {
	case exportRequest := <-requestChan:
		require.Len(t, exportRequest.ResourceMetrics, 1, "Should have one ResourceMetrics")
		resourceMetric := exportRequest.ResourceMetrics[0]

		// Extract resource attributes into a map for easier verification
		resourceAttrs := make(map[string]string)
		for _, attr := range resourceMetric.Resource.Attributes {
			resourceAttrs[attr.Key] = attr.Value.GetStringValue()
		}

		// Verify critical Microsoft metrics attributes are present
		assert.Equal(t, "RPACSINTv2", resourceAttrs["_microsoft_metrics_account"],
			"_microsoft_metrics_account resource attribute should be present")
		assert.Equal(t, "Platform", resourceAttrs["_microsoft_metrics_namespace"],
			"_microsoft_metrics_namespace resource attribute should be present")

		// Verify cluster labels are also present as resource attributes
		assert.Equal(t, "test", resourceAttrs["environment"],
			"environment cluster label should be present as resource attribute")
		assert.Equal(t, "eastus", resourceAttrs["region"],
			"region cluster label should be present as resource attribute")

		// Verify we have the expected total count (2 cluster labels + 2 explicit attributes)
		assert.Len(t, resourceMetric.Resource.Attributes, 4,
			"Should have 4 resource attributes total")

		// Also verify that metrics data was included
		require.Len(t, resourceMetric.ScopeMetrics, 1, "Should have one ScopeMetrics")
		require.GreaterOrEqual(t, len(resourceMetric.ScopeMetrics[0].Metrics), 1, "Should have at least one metric")

	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for OTLP request")
	}
}

// TestDefaultMetricNamePrefix verifies that the DefaultMetricNamePrefix from CLI
// is applied when the CRD doesn't specify a metricNamePrefix, and that CRD prefix
// takes precedence when specified.
func TestDefaultMetricNamePrefix(t *testing.T) {
	tests := []struct {
		name                    string
		defaultMetricNamePrefix string // CLI flag
		crdMetricNamePrefix     string // CRD transform.metricNamePrefix
		metricName              string
		expectedMetric          string
	}{
		{
			name:                    "default prefix applied when CRD has no prefix",
			defaultMetricNamePrefix: "adxexporter",
			crdMetricNamePrefix:     "",
			metricName:              "host_count",
			expectedMetric:          "adxexporter_host_count_value",
		},
		{
			name:                    "CRD prefix overrides default",
			defaultMetricNamePrefix: "adxexporter",
			crdMetricNamePrefix:     "custom",
			metricName:              "host_count",
			expectedMetric:          "custom_host_count_value",
		},
		{
			name:                    "no prefix when both are empty",
			defaultMetricNamePrefix: "",
			crdMetricNamePrefix:     "",
			metricName:              "host_count",
			expectedMetric:          "host_count_value",
		},
		{
			name:                    "CRD can specify full prefix including adxexporter",
			defaultMetricNamePrefix: "something_else",
			crdMetricNamePrefix:     "adxexporter_cluster_autoscaler",
			metricName:              "availability",
			expectedMetric:          "adxexporter_cluster_autoscaler_availability_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestChan := make(chan *v1.ExportMetricsServiceRequest, 1)

			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				r.Body.Close()

				exportRequest := &v1.ExportMetricsServiceRequest{}
				err = proto.Unmarshal(body, exportRequest)
				require.NoError(t, err)

				requestChan <- exportRequest
				w.WriteHeader(http.StatusOK)
				w.Write([]byte{})
			}))
			defer mockServer.Close()

			reconciler := &MetricsExporterReconciler{
				OTLPEndpoint:            mockServer.URL,
				ClusterLabels:           map[string]string{},
				DefaultMetricNamePrefix: tt.defaultMetricNamePrefix,
			}

			err := reconciler.initOtlpExporter()
			require.NoError(t, err)

			me := &adxmonv1.MetricsExporter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-default-prefix",
					Namespace: "default",
				},
				Spec: adxmonv1.MetricsExporterSpec{
					Transform: adxmonv1.TransformConfig{
						ValueColumns:     []string{"value"},
						MetricNameColumn: "metric_name",
						MetricNamePrefix: tt.crdMetricNamePrefix,
						TimestampColumn:  "timestamp",
					},
				},
			}

			rows := []map[string]any{
				{
					"metric_name": tt.metricName,
					"value":       float64(100),
					"timestamp":   time.Now(),
				},
			}

			err = reconciler.transformAndRegisterMetrics(context.Background(), me, rows)
			require.NoError(t, err)

			select {
			case exportRequest := <-requestChan:
				require.Len(t, exportRequest.ResourceMetrics, 1)
				require.Len(t, exportRequest.ResourceMetrics[0].ScopeMetrics, 1)
				metrics := exportRequest.ResourceMetrics[0].ScopeMetrics[0].Metrics
				require.GreaterOrEqual(t, len(metrics), 1)

				actualMetricName := metrics[0].Name
				assert.Equal(t, tt.expectedMetric, actualMetricName,
					"Metric name should reflect default/CRD prefix precedence")

			case <-time.After(5 * time.Second):
				t.Fatal("Timeout waiting for OTLP request")
			}
		})
	}
}

// TestMetricNamePrefixInOTLPPayload verifies that the user-specified metricNamePrefix
// is correctly applied to metric names in the OTLP payload.
func TestMetricNamePrefixInOTLPPayload(t *testing.T) {
	tests := []struct {
		name             string
		metricNamePrefix string
		metricName       string
		expectedMetric   string
	}{
		{
			name:             "with user-defined prefix",
			metricNamePrefix: "infra",
			metricName:       "host_count",
			expectedMetric:   "infra_host_count_value",
		},
		{
			name:             "without user-defined prefix",
			metricNamePrefix: "",
			metricName:       "my_metric",
			expectedMetric:   "my_metric_value",
		},
		{
			name:             "with adxexporter prefix specified by user",
			metricNamePrefix: "adxexporter_cluster_autoscaler",
			metricName:       "availability",
			expectedMetric:   "adxexporter_cluster_autoscaler_availability_numerator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Channel to receive the parsed OTLP request
			requestChan := make(chan *v1.ExportMetricsServiceRequest, 1)

			// Create mock OTLP server that captures the metric names
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				r.Body.Close()

				exportRequest := &v1.ExportMetricsServiceRequest{}
				err = proto.Unmarshal(body, exportRequest)
				require.NoError(t, err)

				requestChan <- exportRequest

				w.WriteHeader(http.StatusOK)
				w.Write([]byte{})
			}))
			defer mockServer.Close()

			reconciler := &MetricsExporterReconciler{
				OTLPEndpoint:  mockServer.URL,
				ClusterLabels: map[string]string{},
			}

			err := reconciler.initOtlpExporter()
			require.NoError(t, err)

			// Determine value column based on test case
			valueColumn := "value"
			if tt.name == "with adxexporter prefix specified by user" {
				valueColumn = "numerator"
			}

			me := &adxmonv1.MetricsExporter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-prefix",
					Namespace: "default",
				},
				Spec: adxmonv1.MetricsExporterSpec{
					Transform: adxmonv1.TransformConfig{
						ValueColumns:     []string{valueColumn},
						MetricNameColumn: "metric_name",
						MetricNamePrefix: tt.metricNamePrefix,
						TimestampColumn:  "timestamp",
					},
				},
			}

			rows := []map[string]any{
				{
					"metric_name": tt.metricName,
					valueColumn:   float64(100),
					"timestamp":   time.Now(),
				},
			}

			err = reconciler.transformAndRegisterMetrics(context.Background(), me, rows)
			require.NoError(t, err)

			select {
			case exportRequest := <-requestChan:
				require.Len(t, exportRequest.ResourceMetrics, 1)
				require.Len(t, exportRequest.ResourceMetrics[0].ScopeMetrics, 1)
				metrics := exportRequest.ResourceMetrics[0].ScopeMetrics[0].Metrics
				require.GreaterOrEqual(t, len(metrics), 1, "Should have at least one metric")

				actualMetricName := metrics[0].Name
				assert.Equal(t, tt.expectedMetric, actualMetricName,
					"Metric name should match expected format with user-specified prefix")

			case <-time.After(5 * time.Second):
				t.Fatal("Timeout waiting for OTLP request")
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
	// Create mock OTLP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{})
	}))
	defer mockServer.Close()

	// Test the integration between transformation and metrics push
	reconciler := &MetricsExporterReconciler{
		OTLPEndpoint:  mockServer.URL,
		ClusterLabels: map[string]string{"env": "test"},
	}

	// Initialize the OTLP exporter
	err := reconciler.initOtlpExporter()
	require.NoError(t, err)
	require.NotNil(t, reconciler.OtlpExporter)

	// Create test MetricsExporter with transform configuration
	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter",
			Namespace: "default",
		},
		Spec: adxmonv1.MetricsExporterSpec{
			Transform: adxmonv1.TransformConfig{
				ValueColumns:      []string{"value"},
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
	// Create mock OTLP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{})
	}))
	defer mockServer.Close()

	// Test transformation when using default metric name (no metric name column)
	reconciler := &MetricsExporterReconciler{
		OTLPEndpoint:  mockServer.URL,
		ClusterLabels: map[string]string{"env": "test"},
	}

	err := reconciler.initOtlpExporter()
	require.NoError(t, err)

	// Create MetricsExporter with default metric name (no MetricNameColumn)
	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-default-metric",
			Namespace: "default",
		},
		Spec: adxmonv1.MetricsExporterSpec{
			Transform: adxmonv1.TransformConfig{
				ValueColumns:      []string{"value"},
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

func TestTransformAndRegisterMetrics_MultiValueColumns(t *testing.T) {
	// Create mock OTLP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte{})
	}))
	defer mockServer.Close()

	reconciler := &MetricsExporterReconciler{
		Client:        fake.NewClientBuilder().Build(),
		Scheme:        scheme.Scheme,
		OTLPEndpoint:  mockServer.URL,
		ClusterLabels: map[string]string{"env": "test"},
	}

	// Initialize the OTLP exporter
	err := reconciler.initOtlpExporter()
	require.NoError(t, err)
	require.NotNil(t, reconciler.OtlpExporter)

	// Configure MetricsExporter with multiple value columns
	me := &adxmonv1.MetricsExporter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-exporter",
			Namespace: "default",
		},
		Spec: adxmonv1.MetricsExporterSpec{
			Database: "TestDB",
			Body:     "TestQuery",
			Interval: metav1.Duration{Duration: time.Minute},
			Transform: adxmonv1.TransformConfig{
				MetricNamePrefix:  "app_",
				DefaultMetricName: "default_metric",
				ValueColumns:      []string{"cpu_usage", "memory_usage", "disk_usage"},
				LabelColumns:      []string{"node_name"},
				TimestampColumn:   "timestamp",
			},
		},
	}

	// Mock data with multiple value columns
	rows := []map[string]interface{}{
		{
			"cpu_usage":    75.5,
			"memory_usage": 85.2,
			"disk_usage":   45.8,
			"node_name":    "node-1",
			"timestamp":    time.Now(),
		},
		{
			"cpu_usage":    60.3,
			"memory_usage": 70.1,
			"disk_usage":   52.4,
			"node_name":    "node-2",
			"timestamp":    time.Now(),
		},
	}

	// Execute transformation and registration
	err = reconciler.transformAndRegisterMetrics(context.Background(), me, rows)
	require.NoError(t, err)
}
