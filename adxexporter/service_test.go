package adxexporter

import (
	"context"
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
			result := reconciler.shouldProcessMetricsExporter(tt.metricsExporter)
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
			}

			err := reconciler.exposeMetrics()

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
