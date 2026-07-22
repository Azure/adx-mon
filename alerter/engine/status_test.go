package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/rules"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWorker_StatusUpdate_NoKubernetesClient(t *testing.T) {
	// Test that status updates are gracefully skipped when no Kubernetes client is available
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			// Simulate 1 row being processed
			fn(ctx, "fake.endpoint", qc, testRow(nil, nil))
			return nil, 1
		},
	}

	rule := &rules.Rule{
		Namespace: "test-namespace",
		Name:      "test-rule",
		Database:  "TestDB",
	}

	w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: &fakeAlerter{}, AlertAddr: "http://fake.alert.addr", HandlerFn: func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error {
		return nil
	}})

	// This should not panic or error
	w.ExecuteQuery(context.Background())

	// Verify that 1 alert was tracked even without status update
	require.Equal(t, 1, w.alertsGenerated)
}

func TestWorker_AlertCounting(t *testing.T) {
	// Test that alerts are correctly counted regardless of status updates
	tests := []struct {
		name           string
		handlerSuccess bool
		expectedAlerts int
		numRows        int
	}{
		{
			name:           "All alerts succeed",
			handlerSuccess: true,
			expectedAlerts: 3,
			numRows:        3,
		},
		{
			name:           "Some alerts fail",
			handlerSuccess: false,
			expectedAlerts: 0,
			numRows:        2,
		},
		{
			name:           "No rows returned",
			handlerSuccess: true,
			expectedAlerts: 0,
			numRows:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kcli := &fakeKustoClient{
				queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
					for i := 0; i < tt.numRows; i++ {
						fn(ctx, "fake.endpoint", qc, testRow(nil, nil))
					}
					return nil, tt.numRows
				},
			}

			rule := &rules.Rule{
				Namespace: "test-namespace",
				Name:      "test-rule",
				Database:  "TestDB",
			}

			w := NewWorker(&WorkerConfig{Rule: rule, Region: "eastus", KustoClient: kcli, AlertClient: &fakeAlerter{}, AlertAddr: "http://fake.alert.addr", HandlerFn: func(ctx context.Context, endpoint string, qc *QueryContext, row azquery.Row) error {
				if tt.handlerSuccess {
					return nil // Success - alert generated
				}
				return fmt.Errorf("handler failed") // Failure - no alert generated
			}})

			// Execute the query
			w.ExecuteQuery(context.Background())

			// Verify alert count
			require.Equal(t, tt.expectedAlerts, w.alertsGenerated)
		})
	}
}

func TestWorker_StatusUpdateIncludesLatestEvaluationDetails(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	alertRule := &alertrulev1.AlertRule{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test-namespace", Name: "test-rule"},
		Spec: alertrulev1.AlertRuleSpec{
			Database:    "TestDB",
			Query:       "Table | take 1",
			Destination: "destination",
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	rule := &rules.Rule{Namespace: alertRule.Namespace, Name: alertRule.Name, Database: "TestDB"}
	w := NewWorker(&WorkerConfig{
		Rule:       rule,
		CtrlClient: ctrlCli,
	})

	w.updateAlertRuleStatus(context.Background(), time.Now().UTC(), 1500*time.Millisecond, 2, 2, "Success", "")

	updated := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, updated))
	require.Equal(t, "Success", updated.Status.Status)
	require.Equal(t, int64(1500), updated.Status.LastEvaluationDurationMilliseconds)
	require.Equal(t, int64(2), updated.Status.LastRowsReturned)
	require.Equal(t, int64(2), updated.Status.LastAlertsGenerated)
	require.False(t, updated.Status.LastQueryTime.IsZero())
	require.False(t, updated.Status.LastAlertTime.IsZero())
}
