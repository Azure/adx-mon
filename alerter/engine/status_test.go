package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/stretchr/testify/require"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
)

func TestWorker_StatusUpdate_NoKubernetesClient(t *testing.T) {
	// Test that status updates are gracefully skipped when no Kubernetes client is available
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
			// Simulate 1 row being processed
			fn(ctx, "fake.endpoint", qc, &table.Row{})
			return nil, 1
		},
	}

	rule := &rules.Rule{
		Namespace: "test-namespace",
		Name:      "test-rule",
		Database:  "TestDB",
	}

	w := &worker{
		rule:        rule,
		Region:      "eastus",
		kustoClient: kcli,
		AlertAddr:   "http://fake.alert.addr",
		AlertCli:    &fakeAlerter{},
		HandlerFn: func(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error {
			return nil
		},
		ctrlCli: nil, // No Kubernetes client
	}

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
				queryFn: func(ctx context.Context, qc *QueryContext, fn func(context.Context, string, *QueryContext, *table.Row) error) (error, int) {
					for i := 0; i < tt.numRows; i++ {
						fn(ctx, "fake.endpoint", qc, &table.Row{})
					}
					return nil, tt.numRows
				},
			}

			rule := &rules.Rule{
				Namespace: "test-namespace",
				Name:      "test-rule",
				Database:  "TestDB",
			}

			w := &worker{
				rule:        rule,
				Region:      "eastus",
				kustoClient: kcli,
				AlertAddr:   "http://fake.alert.addr",
				AlertCli:    &fakeAlerter{},
				HandlerFn: func(ctx context.Context, endpoint string, qc *QueryContext, row *table.Row) error {
					if tt.handlerSuccess {
						return nil // Success - alert generated
					}
					return fmt.Errorf("handler failed") // Failure - no alert generated
				},
				ctrlCli: nil, // No Kubernetes client
			}

			// Execute the query
			w.ExecuteQuery(context.Background())

			// Verify alert count
			require.Equal(t, tt.expectedAlerts, w.alertsGenerated)
		})
	}
}