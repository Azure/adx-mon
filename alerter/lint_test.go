package alerter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"github.com/stretchr/testify/require"
)

func TestRunLint_DuplicateReservedColumnsDifferentCase(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "alert_rule.yaml")
	rule := `---
apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: duplicate-columns
  namespace: namespace
spec:
  database: DB
  interval: 5m
  query: |
    Heartbeat
  autoMitigateAfter: 1h
  destination: "somewhere"
`
	require.NoError(t, os.WriteFile(filename, []byte(rule), 0o644))

	opts := &AlerterOpts{
		KustoEndpoints:   map[string]string{"DB": "http://fake.endpoint"},
		Region:           "test-region",
		Tags:             map[string]string{"env": "test"},
		MaxNotifications: 10,
	}

	err := runLint(context.Background(), opts, filename, func(opts *AlerterOpts) (engine.Client, error) {
		return fakeLintKustoClient{}, nil
	})
	require.ErrorContains(t, err, `query results include multiple columns for reserved alert field "Severity": Severity, severity`)
}

type fakeLintKustoClient struct{}

func (f fakeLintKustoClient) Endpoint(db string) string {
	return "https://fake.kusto.windows.net"
}

func (f fakeLintKustoClient) Query(ctx context.Context, qc *engine.QueryContext, fn func(context.Context, string, *engine.QueryContext, *table.Row) error) (error, int) {
	iter := &kusto.RowIterator{}
	rows, err := kusto.NewMockRows(table.Columns{
		{Name: "Title", Type: types.String},
		{Name: "Severity", Type: types.Long},
		{Name: "severity", Type: types.Long},
	})
	if err != nil {
		return err, 0
	}

	if err := rows.Row(value.Values{
		value.String{Value: "Title", Valid: true},
		value.Long{Value: 1, Valid: true},
		value.Long{Value: 2, Valid: true},
	}); err != nil {
		return err, 0
	}

	if err := iter.Mock(rows); err != nil {
		return err, 0
	}

	row, _, _ := iter.NextRowOrError()
	if err := fn(ctx, f.Endpoint(qc.Rule.Database), qc, row); err != nil {
		return err, 0
	}
	return nil, 1
}

func (f fakeLintKustoClient) AvailableDatabases() []string {
	return []string{"DB"}
}

func (f fakeLintKustoClient) FindCaseInsensitiveMatch(db string) string {
	return ""
}
