package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/metrics"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWorker_StatusUpdate_NoKubernetesClient(t *testing.T) {
	// A missing Kubernetes client must not prevent the query from executing.
	queryExecuted := false
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			queryExecuted = true
			require.NoError(t, fn(ctx, "fake.endpoint", qc, testRow(nil, nil)))
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

	w.ExecuteQuery(context.Background())

	require.True(t, queryExecuted)
}

func TestWorker_AlertsGeneratedMetric(t *testing.T) {
	// Only successful handler calls count as generated alerts.
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

			counterBefore := getCounterValue(t, metrics.AlertsGeneratedTotal)
			w.ExecuteQuery(context.Background())

			require.Equal(t, counterBefore+float64(tt.expectedAlerts), getCounterValue(t, metrics.AlertsGeneratedTotal))
		})
	}
}

func TestWorker_StatusUpdateIncludesLatestEvaluationDetails(t *testing.T) {
	// The status update persists the latest duration, row count, and alert count.
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

	evaluation := newAlertRuleEvaluation(rule)
	evaluation.startTime = time.Now().Add(-1500 * time.Millisecond)
	evaluation.rows = 2
	evaluation.alertsGenerated = 2
	w.updateAlertRuleStatus(context.Background(), evaluation, "Success", "")

	updated := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, updated))
	require.Equal(t, "Success", updated.Status.Status)
	require.GreaterOrEqual(t, updated.Status.LastEvaluationDurationMilliseconds, int64(1500))
	require.Equal(t, int64(2), updated.Status.LastRowsReturned)
	require.Equal(t, int64(2), updated.Status.LastAlertsGenerated)
	require.False(t, updated.Status.LastQueryTime.IsZero())
	require.False(t, updated.Status.LastAlertTime.IsZero())
}

func TestWorker_ThrottledStatusIncludesPartialAlerts(t *testing.T) {
	// Alerts generated before throttling must remain visible in AlertRule status.
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	alertRule := &alertrulev1.AlertRule{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test-namespace", Name: "throttled-rule"},
		Spec: alertrulev1.AlertRuleSpec{
			Database:    "TestDB",
			Query:       "Table | take 2",
			Destination: "destination",
		},
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&alertrulev1.AlertRule{}).
		WithObjects(alertRule).
		Build()

	w := NewWorker(&WorkerConfig{
		Rule:   &rules.Rule{Namespace: alertRule.Namespace, Name: alertRule.Name, Database: "TestDB", Destination: "destination"},
		Region: "eastus",
		KustoClient: &fakeKustoClient{queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			for range 2 {
				require.NoError(t, fn(ctx, "fake.endpoint", qc, testRow(nil, nil)))
			}
			return alert.ErrTooManyRequests, 2
		}},
		AlertClient: &fakeAlerter{},
		CtrlClient:  ctrlCli,
		HandlerFn: func(context.Context, string, *QueryContext, azquery.Row) error {
			return nil
		},
	})

	w.ExecuteQuery(context.Background())

	updated := &alertrulev1.AlertRule{}
	require.NoError(t, ctrlCli.Get(context.Background(), types.NamespacedName{Namespace: alertRule.Namespace, Name: alertRule.Name}, updated))
	require.Equal(t, "Throttled", updated.Status.Status)
	require.Equal(t, int64(2), updated.Status.LastRowsReturned)
	require.Equal(t, int64(2), updated.Status.LastAlertsGenerated)
	require.False(t, updated.Status.LastAlertTime.IsZero())
}
