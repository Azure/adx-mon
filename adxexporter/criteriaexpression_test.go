package adxexporter

import (
	"context"
	"fmt"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestMetricsExporter_CriteriaExpressionProcessing verifies combined semantics of Criteria (OR) and CriteriaExpression (CEL) gating
// for MetricsExporter reconciliation, mirroring SummaryRule tests.
func TestMetricsExporter_CriteriaExpressionProcessing(t *testing.T) {
	// Prepare scheme with our CRD
	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s))
	require.NoError(t, adxmonv1.AddToScheme(s))

	clusterLabels := map[string]string{"region": "eastus", "environment": "prod"}

	makeME := func(spec adxmonv1.MetricsExporterSpec, name string) *adxmonv1.MetricsExporter {
		// Ensure required minimal fields
		if spec.Interval.Duration == 0 {
			spec.Interval = metav1.Duration{Duration: time.Minute}
		}
		if spec.Database == "" {
			spec.Database = "test-db"
		}
		if spec.Body == "" {
			spec.Body = "MyTable | project metric_name='test_metric', value=1.0, timestamp=now()"
		}
		if len(spec.Transform.ValueColumns) == 0 {
			spec.Transform.ValueColumns = []string{"value"}
		}
		if spec.Transform.TimestampColumn == "" {
			spec.Transform.TimestampColumn = "timestamp"
		}
		if spec.Transform.MetricNameColumn == "" && spec.Transform.DefaultMetricName == "" {
			spec.Transform.MetricNameColumn = "metric_name"
		}
		return &adxmonv1.MetricsExporter{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       spec,
		}
	}

	tests := []struct {
		name            string
		spec            adxmonv1.MetricsExporterSpec
		clusterLabels   map[string]string // optional override of default cluster labels
		expectExecuted  bool
		expectQueryRows bool // whether we configure a mock row result
	}{
		{
			name:           "no criteria or expression => executes",
			spec:           adxmonv1.MetricsExporterSpec{},
			expectExecuted: true,
		},
		{
			name:           "criteria match only",
			spec:           adxmonv1.MetricsExporterSpec{Criteria: map[string][]string{"region": {"eastus"}}},
			expectExecuted: true,
		},
		{
			name:           "criteriaExpression match only",
			spec:           adxmonv1.MetricsExporterSpec{CriteriaExpression: "region == 'eastus' && environment == 'prod'"},
			expectExecuted: true,
		},
		{
			name:           "both match",
			spec:           adxmonv1.MetricsExporterSpec{Criteria: map[string][]string{"region": {"eastus"}}, CriteriaExpression: "environment == 'prod'"},
			expectExecuted: true,
		},
		{
			name:           "criteria match expression false",
			spec:           adxmonv1.MetricsExporterSpec{Criteria: map[string][]string{"region": {"eastus"}}, CriteriaExpression: "environment == 'dev'"},
			expectExecuted: false,
		},
		{
			name:           "criteria no match expression true",
			spec:           adxmonv1.MetricsExporterSpec{Criteria: map[string][]string{"region": {"westus"}}, CriteriaExpression: "environment == 'prod'"},
			expectExecuted: false,
		},
		{
			name:           "invalid expression treated as skip",
			spec:           adxmonv1.MetricsExporterSpec{CriteriaExpression: "region =="},
			expectExecuted: false,
		},
		// Casing behaviors:
		{
			name:           "criteria map insensitive - upper label lower criteria key",
			spec:           adxmonv1.MetricsExporterSpec{Criteria: map[string][]string{"region": {"eastus"}}},
			clusterLabels:  map[string]string{"REGION": "EASTUS"},
			expectExecuted: true,
		},
		{
			name: "criteriaExpression sensitive - mismatched case identifiers",
			spec: adxmonv1.MetricsExporterSpec{CriteriaExpression: "REGION == 'eastus'"},
			// clusterLabels default (lowercase) -> REGION undefined
			expectExecuted: false,
		},
		{
			name:           "criteriaExpression sensitive - matching case identifiers",
			spec:           adxmonv1.MetricsExporterSpec{CriteriaExpression: "Region == 'eastus'"},
			clusterLabels:  map[string]string{"Region": "eastus"},
			expectExecuted: true,
		},
		{
			name:           "criteriaExpression sensitive - matching case values",
			spec:           adxmonv1.MetricsExporterSpec{CriteriaExpression: "Region == 'Eastus'"},
			clusterLabels:  map[string]string{"Region": "Eastus"},
			expectExecuted: true,
		},
		{
			name:           "criteriaExpression sensitive - not matching case values",
			spec:           adxmonv1.MetricsExporterSpec{CriteriaExpression: "Region == 'Eastus'"},
			clusterLabels:  map[string]string{"Region": "eastus"},
			expectExecuted: false,
		},
		{
			name:           "criteriaExpression value case sensitive mismatch",
			spec:           adxmonv1.MetricsExporterSpec{CriteriaExpression: "region == 'eastus'"},
			clusterLabels:  map[string]string{"region": "EASTUS"},
			expectExecuted: false,
		},
		{
			name:           "combined - criteria matches expression identifier case mismatch causes skip",
			spec:           adxmonv1.MetricsExporterSpec{Criteria: map[string][]string{"region": {"eastus"}}, CriteriaExpression: "REGION == 'eastus'"},
			clusterLabels:  map[string]string{"region": "eastus"},
			expectExecuted: false,
		},
		{
			name: "combined - both match with matching identifier cases",
			spec: adxmonv1.MetricsExporterSpec{Criteria: map[string][]string{"Region": {"EASTUS"}}, CriteriaExpression: "environment == 'prod' && region == 'eastus'"},
			// default labels suffice
			expectExecuted: true,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			me := makeME(tt.spec, fmt.Sprintf("me-%d", i))
			labels := clusterLabels
			if tt.clusterLabels != nil {
				labels = tt.clusterLabels
			}
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(me).Build()
			mockKusto := NewMockKustoExecutor(t, "test-db", "https://test-cluster.kusto.windows.net")
			if tt.expectExecuted {
				mockKusto.SetNextResult(t, [][]interface{}{{"test_metric", 1.0, time.Now()}})
			}
			reconciler := &MetricsExporterReconciler{
				Client:                fakeClient,
				Scheme:                s,
				ClusterLabels:         labels,
				KustoClusters:         map[string]string{"test-db": "https://test-cluster.kusto.windows.net"},
				QueryExecutors:        map[string]*QueryExecutor{"test-db": NewQueryExecutor(mockKusto)},
				EnableMetricsEndpoint: false,
			}
			require.NoError(t, reconciler.exposeMetricsServer())
			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: me.Name, Namespace: me.Namespace}}
			res, err := reconciler.Reconcile(context.Background(), req)
			require.NoError(t, err)
			queries := mockKusto.GetQueries()
			if tt.expectExecuted {
				assert.Len(t, queries, 1, "expected a query to run")
				assert.NotZero(t, res.RequeueAfter, "expected requeue when executed")
			} else {
				assert.Len(t, queries, 0, "expected no query to run")
				assert.Zero(t, res.RequeueAfter, "expected no requeue when skipped")
			}
		})
	}
}
